//go:build cgo && aec_oracle

// Package subtractor is the integrated bit-exact parity slice for
// aec/internal/aec3's subtractor.go, subtractor_output.go and
// subtractor_output_analyzer.go (the full Subtractor: both adaptive
// filters, both update gains, and the misadjustment/analyzer
// bookkeeping around them) against the fetched AEC3 C++ oracle. Env/
// link setup is shared with the other slices via ../run.sh.
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package subtractor

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
#include <stdlib.h>
*/
import "C"
import "unsafe"

// outputFloats mirrors AEC3_SUBTRACTOR_OUTPUT_FLOATS (shim.h): the
// flattened per-channel float layout aec3_subtractor_process writes.
const outputFloats = 64*4 + 65*4 + 7

// subtractorC wraps the oracle's webrtc::Subtractor plus the
// webrtc::RenderDelayBuffer/RenderSignalAnalyzer/AecState it's fed
// through.
type subtractorC struct {
	h                  *C.aec3_subtractor
	numCaptureChannels int
}

func newSubtractorC(numRenderChannels, numCaptureChannels int) *subtractorC {
	return &subtractorC{
		h:                  C.aec3_subtractor_create(C.int(numRenderChannels), C.int(numCaptureChannels)),
		numCaptureChannels: numCaptureChannels,
	}
}

func (s *subtractorC) close() {
	C.aec3_subtractor_destroy(s.h)
	s.h = nil
}

func (s *subtractorC) insertRenderBlock(samples []float32) {
	C.aec3_subtractor_insert_render_block(s.h, (*C.float)(&samples[0]))
}

func (s *subtractorC) updateRenderSignalAnalyzer() {
	C.aec3_subtractor_update_render_signal_analyzer(s.h)
}

func (s *subtractorC) handleEchoPathChange(gainChange bool, delayChange int, clockDrift bool) {
	C.aec3_subtractor_handle_echo_path_change(s.h, boolToC(gainChange), C.int(delayChange), boolToC(clockDrift))
}

func (s *subtractorC) exitInitialState() {
	C.aec3_subtractor_exit_initial_state(s.h)
}

func (s *subtractorC) setSaturatedCapture(v bool) {
	C.aec3_subtractor_set_saturated_capture(s.h, boolToC(v))
}

// cOutput mirrors the flattened per-channel layout documented in
// shim.h.
type cOutput struct {
	sRefined, sCoarse, eRefined, eCoarse [64]float32
	eRefinedRe, eRefinedIm               [65]float32
	e2Refined, e2Coarse                  [65]float32
	s2Refined, s2Coarse                  float32
	e2RefinedScalar, e2CoarseScalar      float32
	y2                                   float32
	sRefinedMaxAbs, sCoarseMaxAbs        float32
}

func unpackOutput(flat []float32) cOutput {
	var o cOutput
	i := 0
	take64 := func(dst *[64]float32) {
		copy(dst[:], flat[i:i+64])
		i += 64
	}
	take65 := func(dst *[65]float32) {
		copy(dst[:], flat[i:i+65])
		i += 65
	}
	take64(&o.sRefined)
	take64(&o.sCoarse)
	take64(&o.eRefined)
	take64(&o.eCoarse)
	take65(&o.eRefinedRe)
	take65(&o.eRefinedIm)
	take65(&o.e2Refined)
	take65(&o.e2Coarse)
	o.s2Refined = flat[i]
	i++
	o.s2Coarse = flat[i]
	i++
	o.e2RefinedScalar = flat[i]
	i++
	o.e2CoarseScalar = flat[i]
	i++
	o.y2 = flat[i]
	i++
	o.sRefinedMaxAbs = flat[i]
	i++
	o.sCoarseMaxAbs = flat[i]
	i++
	return o
}

func (s *subtractorC) process(captureSamples []float32) []cOutput {
	flat := make([]float32, s.numCaptureChannels*outputFloats)
	C.aec3_subtractor_process(s.h, (*C.float)(&captureSamples[0]), (*C.float)(unsafe.Pointer(&flat[0])))
	out := make([]cOutput, s.numCaptureChannels)
	for ch := 0; ch < s.numCaptureChannels; ch++ {
		out[ch] = unpackOutput(flat[ch*outputFloats : (ch+1)*outputFloats])
	}
	return out
}

func (s *subtractorC) impulseResponse(channel int) []float32 {
	n := int(C.aec3_subtractor_impulse_response_len(s.h, C.int(channel)))
	if n == 0 {
		return nil
	}
	buf := make([]float32, n)
	C.aec3_subtractor_get_impulse_response(s.h, C.int(channel), (*C.float)(unsafe.Pointer(&buf[0])))
	return buf
}

func (s *subtractorC) frequencyResponse(channel int) [][65]float32 {
	n := int(C.aec3_subtractor_frequency_response_partitions(s.h, C.int(channel)))
	if n == 0 {
		return nil
	}
	buf := make([]float32, n*65)
	C.aec3_subtractor_get_frequency_response(s.h, C.int(channel), (*C.float)(unsafe.Pointer(&buf[0])))
	out := make([][65]float32, n)
	for p := 0; p < n; p++ {
		copy(out[p][:], buf[p*65:(p+1)*65])
	}
	return out
}

func boolToC(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
