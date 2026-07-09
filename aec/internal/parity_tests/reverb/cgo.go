//go:build cgo && aec_oracle

// Package reverb is the standalone bit-exact parity slice for
// aec/internal/aec3's ReverbModel, ReverbModelEstimator,
// ReverbDecayEstimator and ReverbFrequencyResponse against the fetched
// AEC3 C++ oracle -- see shim.h's doc comment for the two-part design
// (a direct ReverbModel unit test, and a Subtractor/FilterAnalyzer-
// driven ReverbModelEstimator pipeline test with an adaptive-decay
// config not otherwise exercised by any other slice).
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package reverb

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
#include <stdlib.h>
*/
import "C"

// ---- Part A: direct ReverbModel unit test bindings ----

type reverbModelC struct {
	h *C.aec3_reverbmodel
}

func newReverbModelC() *reverbModelC {
	return &reverbModelC{h: C.aec3_reverbmodel_create()}
}

func (m *reverbModelC) close() {
	C.aec3_reverbmodel_destroy(m.h)
	m.h = nil
}

func (m *reverbModelC) updateNoFreqShaping(powerSpectrum []float32, scaling, decay float32) [65]float32 {
	var out [65]float32
	C.aec3_reverbmodel_update_no_freq(m.h, (*C.float)(&powerSpectrum[0]), C.float(scaling), C.float(decay), (*C.float)(&out[0]))
	return out
}

func (m *reverbModelC) updateFreq(powerSpectrum []float32, scaling [65]float32, decay float32) [65]float32 {
	var out [65]float32
	C.aec3_reverbmodel_update_freq(m.h, (*C.float)(&powerSpectrum[0]), (*C.float)(&scaling[0]), C.float(decay), (*C.float)(&out[0]))
	return out
}

// ---- Part B: ReverbModelEstimator pipeline bindings ----

// reverbEstOutputFloats mirrors AEC3_REVERBEST_OUTPUT_FLOATS (shim.h).
const reverbEstOutputFloats = 65 + 2

type reverbEstC struct {
	h *C.aec3_reverbest
}

func newReverbEstC(numRenderChannels int, defaultLen, nearendLen float32) *reverbEstC {
	return &reverbEstC{h: C.aec3_reverbest_create(C.int(numRenderChannels), C.float(defaultLen), C.float(nearendLen))}
}

func (s *reverbEstC) close() {
	C.aec3_reverbest_destroy(s.h)
	s.h = nil
}

func (s *reverbEstC) insertRenderBlock(samples []float32) {
	C.aec3_reverbest_insert_render_block(s.h, (*C.float)(&samples[0]))
}

// reverbEstOutput mirrors the flattened layout documented in shim.h.
type reverbEstOutput struct {
	freqResponse [65]float32
	decayDefault float32
	decayMild    float32
}

func (s *reverbEstC) process(captureSamples []float32, quality *float32, stationaryBlock bool) reverbEstOutput {
	flat := make([]float32, reverbEstOutputFloats)
	hasQuality := C.int(0)
	var qv float32
	if quality != nil {
		hasQuality = 1
		qv = *quality
	}
	sb := C.int(0)
	if stationaryBlock {
		sb = 1
	}
	C.aec3_reverbest_process(
		s.h,
		(*C.float)(&captureSamples[0]),
		hasQuality, C.float(qv), sb,
		(*C.float)(&flat[0]),
	)
	var o reverbEstOutput
	copy(o.freqResponse[:], flat[:65])
	o.decayDefault = flat[65]
	o.decayMild = flat[66]
	return o
}
