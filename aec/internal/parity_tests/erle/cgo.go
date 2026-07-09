//go:build cgo && aec_oracle

// Package erle is the standalone bit-exact parity slice for
// aec/internal/aec3's ErleEstimator (and its constituents
// FullBandErleEstimator, SubbandErleEstimator and
// SignalDependentErleEstimator) against the fetched AEC3 C++ oracle,
// driven by a real Subtractor + RenderDelayBuffer pipeline (AecState
// itself is never Update()'d -- see shim.h/shim.cc's doc comment).
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package erle

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
#include <stdlib.h>
*/
import "C"

// erleOutputFloats mirrors AEC3_ERLE_OUTPUT_FLOATS (shim.h).
const erleOutputFloats = 65*4 + 3

// erleC wraps the oracle's webrtc::ErleEstimator plus the real
// webrtc::Subtractor/RenderDelayBuffer/RenderSignalAnalyzer pipeline
// that drives it.
type erleC struct {
	h *C.aec3_erle
}

func newErleC(numRenderChannels, numSections int) *erleC {
	return &erleC{h: C.aec3_erle_create(C.int(numRenderChannels), C.int(numSections))}
}

func (s *erleC) close() {
	C.aec3_erle_destroy(s.h)
	s.h = nil
}

func (s *erleC) insertRenderBlock(samples []float32) {
	C.aec3_erle_insert_render_block(s.h, (*C.float)(&samples[0]))
}

// cErleOutput mirrors the flattened layout documented in shim.h.
type cErleOutput struct {
	erleNoOnset, erleOnset, erleUnbounded, erleDuringOnsets [65]float32
	fullbandErleLog2                                        float32
	qualityValid                                            bool
	qualityValue                                            float32
}

func unpackErleOutput(flat []float32) cErleOutput {
	var o cErleOutput
	i := 0
	take65 := func(dst *[65]float32) {
		copy(dst[:], flat[i:i+65])
		i += 65
	}
	take65(&o.erleNoOnset)
	take65(&o.erleOnset)
	take65(&o.erleUnbounded)
	take65(&o.erleDuringOnsets)
	o.fullbandErleLog2 = flat[i]
	i++
	o.qualityValid = flat[i] != 0
	i++
	o.qualityValue = flat[i]
	i++
	return o
}

func (s *erleC) process(captureSamples []float32, delayBlocks int) cErleOutput {
	flat := make([]float32, erleOutputFloats)
	C.aec3_erle_process(
		s.h,
		(*C.float)(&captureSamples[0]),
		C.int(delayBlocks),
		(*C.float)(&flat[0]),
	)
	return unpackErleOutput(flat)
}
