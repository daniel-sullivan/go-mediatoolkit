//go:build cgo && aec_oracle

// Package suppressor is the component-level bit-exact parity slice
// for aec/internal/aec3's suppression_gain.go, comfort_noise_generator.go
// and suppression_filter.go against the fetched AEC3 C++ oracle. See
// shim.h for why a full render/subtractor/AecState pipeline is not
// needed here.
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package suppressor

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
#include <stdlib.h>
*/
import "C"
import "unsafe"

// spectrumFloats mirrors AEC3_SUPPRESSOR_SPECTRUM_FLOATS.
const spectrumFloats = 65

// blockFloats mirrors AEC3_SUPPRESSOR_BLOCK_FLOATS.
const blockFloats = 64

type suppressorC struct {
	h *C.aec3_suppressor
}

func newSuppressorC() *suppressorC {
	return &suppressorC{h: C.aec3_suppressor_create()}
}

func (s *suppressorC) close() {
	C.aec3_suppressor_destroy(s.h)
	s.h = nil
}

// stepResult bundles every flattened output of a single
// aec3_suppressor_step call.
type stepResult struct {
	lowerNoiseRe, lowerNoiseIm [spectrumFloats]float32
	upperNoiseRe, upperNoiseIm [spectrumFloats]float32
	noiseSpectrum              [spectrumFloats]float32
	gain                       [spectrumFloats]float32
	filterOut                  [blockFloats]float32
	highBandsGain              float32
	isDominantNearend          bool
}

func (s *suppressorC) step(render, captureSpectrum []float32, saturatedCapture bool, nearend, echo, residual, residualUnbounded []float32, clockDrift bool, eLowestRe, eLowestIm []float32) stepResult {
	var lowerRe, lowerIm, upperRe, upperIm, noiseSpectrum, gain [spectrumFloats]float32
	var filterOut [blockFloats]float32
	var scalars [2]float32

	C.aec3_suppressor_step(
		s.h,
		(*C.float)(&render[0]),
		(*C.float)(&captureSpectrum[0]),
		boolToC(saturatedCapture),
		(*C.float)(&nearend[0]),
		(*C.float)(&echo[0]),
		(*C.float)(&residual[0]),
		(*C.float)(&residualUnbounded[0]),
		boolToC(clockDrift),
		(*C.float)(&eLowestRe[0]),
		(*C.float)(&eLowestIm[0]),
		(*C.float)(unsafe.Pointer(&lowerRe[0])),
		(*C.float)(unsafe.Pointer(&lowerIm[0])),
		(*C.float)(unsafe.Pointer(&upperRe[0])),
		(*C.float)(unsafe.Pointer(&upperIm[0])),
		(*C.float)(unsafe.Pointer(&noiseSpectrum[0])),
		(*C.float)(unsafe.Pointer(&gain[0])),
		(*C.float)(unsafe.Pointer(&filterOut[0])),
		(*C.float)(unsafe.Pointer(&scalars[0])),
	)

	return stepResult{
		lowerNoiseRe: lowerRe, lowerNoiseIm: lowerIm,
		upperNoiseRe: upperRe, upperNoiseIm: upperIm,
		noiseSpectrum:     noiseSpectrum,
		gain:              gain,
		filterOut:         filterOut,
		highBandsGain:     scalars[0],
		isDominantNearend: scalars[1] != 0,
	}
}

func boolToC(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
