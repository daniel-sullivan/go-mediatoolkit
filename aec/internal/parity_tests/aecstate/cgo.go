//go:build cgo && aec_oracle

// Package aecstate is the integrated bit-exact parity slice for
// aec/internal/aec3's aec_state.go (AecState and its nested
// InitialState/FilterDelay/FilteringQualityAnalyzer/
// SaturationDetector helpers) against the fetched AEC3 C++ oracle.
// AecState::Update is driven with the same orchestration order
// EchoRemoverImpl::ProcessCapture uses (see shim.h/shim.cc), so this
// slice also re-exercises the already-verified subtractor, erl and
// erle estimators as inputs -- but the state compared here is
// AecState's own readable outputs.
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package aecstate

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
#include <stdlib.h>
*/
import "C"
import "unsafe"

// subtractorOutputFloats mirrors AEC3_AECSTATE_SUBTRACTOR_OUTPUT_FLOATS
// (shim.h).
const subtractorOutputFloats = 64*4 + 65*4 + 7

// aecStateOutputFloats mirrors AEC3_AECSTATE_OUTPUT_FLOATS (shim.h).
const aecStateOutputFloats = 65*5 + 13

// aecStateC wraps the oracle's webrtc::AecState plus the
// webrtc::Subtractor/RenderDelayBuffer/RenderSignalAnalyzer pipeline
// that drives it.
type aecStateC struct {
	h *C.aec3_aecstate
}

func newAecStateC(numRenderChannels int) *aecStateC {
	return &aecStateC{h: C.aec3_aecstate_create(C.int(numRenderChannels))}
}

func (s *aecStateC) close() {
	C.aec3_aecstate_destroy(s.h)
	s.h = nil
}

func (s *aecStateC) insertRenderBlock(samples []float32) {
	C.aec3_aecstate_insert_render_block(s.h, (*C.float)(&samples[0]))
}

// cSubtractorOutput mirrors the flattened per-channel layout
// documented in shim.h.
type cSubtractorOutput struct {
	sRefined, sCoarse, eRefined, eCoarse [64]float32
	eRefinedRe, eRefinedIm               [65]float32
	e2Refined, e2Coarse                  [65]float32
	s2Refined, s2Coarse                  float32
	e2RefinedScalar, e2CoarseScalar      float32
	y2                                   float32
	sRefinedMaxAbs, sCoarseMaxAbs        float32
}

func unpackSubtractorOutput(flat []float32) cSubtractorOutput {
	var o cSubtractorOutput
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

// cAecState mirrors the flattened AecState-readable-state layout
// documented in shim.h.
type cAecState struct {
	erleNoOnset, erleOnset, erleUnbounded, erl, reverbFreqResponse [65]float32
	fullbandErleLog2                                               float32
	erlTimeDomain                                                  float32
	minDirectPathFilterDelay                                       float32
	saturatedCapture                                               float32
	saturatedEcho                                                  float32
	transparentModeActive                                          float32
	reverbDecayDefault                                             float32
	reverbDecayMild                                                float32
	transitionTriggered                                            float32
	filterLengthBlocks                                             float32
	usableLinearEstimate                                           float32
	useLinearFilterOutput                                          float32
	activeRender                                                   float32
}

func unpackAecState(flat []float32) cAecState {
	var o cAecState
	i := 0
	take65 := func(dst *[65]float32) {
		copy(dst[:], flat[i:i+65])
		i += 65
	}
	take65(&o.erleNoOnset)
	take65(&o.erleOnset)
	take65(&o.erleUnbounded)
	take65(&o.erl)
	take65(&o.reverbFreqResponse)
	takeScalar := func() float32 {
		v := flat[i]
		i++
		return v
	}
	o.fullbandErleLog2 = takeScalar()
	o.erlTimeDomain = takeScalar()
	o.minDirectPathFilterDelay = takeScalar()
	o.saturatedCapture = takeScalar()
	o.saturatedEcho = takeScalar()
	o.transparentModeActive = takeScalar()
	o.reverbDecayDefault = takeScalar()
	o.reverbDecayMild = takeScalar()
	o.transitionTriggered = takeScalar()
	o.filterLengthBlocks = takeScalar()
	o.usableLinearEstimate = takeScalar()
	o.useLinearFilterOutput = takeScalar()
	o.activeRender = takeScalar()
	return o
}

// processResult bundles both flattened outputs from a single
// aec3_aecstate_process call.
type processResult struct {
	subtractor cSubtractorOutput
	aecState   cAecState
}

// externalDelayArgs mirrors shim.h's has_external_delay/quality/value
// trio (0/1, 0=kCoarse/1=kRefined, delay-in-blocks).
type externalDelayArgs struct {
	has     bool
	quality int
	value   int
}

func (s *aecStateC) process(captureSamples []float32, captureSaturation bool, gainChange bool, delayChange int, clockDrift bool, ext externalDelayArgs) processResult {
	subFlat := make([]float32, subtractorOutputFloats)
	stateFlat := make([]float32, aecStateOutputFloats)
	C.aec3_aecstate_process(
		s.h,
		(*C.float)(&captureSamples[0]),
		boolToC(captureSaturation),
		boolToC(gainChange),
		C.int(delayChange),
		boolToC(clockDrift),
		boolToC(ext.has),
		C.int(ext.quality),
		C.int(ext.value),
		(*C.float)(unsafe.Pointer(&subFlat[0])),
		(*C.float)(unsafe.Pointer(&stateFlat[0])),
	)
	return processResult{
		subtractor: unpackSubtractorOutput(subFlat),
		aecState:   unpackAecState(stateFlat),
	}
}

func boolToC(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
