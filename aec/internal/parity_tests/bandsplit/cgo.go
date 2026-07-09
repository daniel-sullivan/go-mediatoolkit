//go:build cgo && aec_oracle

// Package bandsplit is the bit-exact parity slice for
// aec/internal/aec3's bandsplit.go (ThreeBandFilterBank, two-band QMF,
// FloatS16 conversions) against the fetched C++ oracle. Env/link setup
// is shared with the other slices via ../run.sh — see the fft slice's
// cgo.go and the smoke slice for the full rationale.
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package bandsplit

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
*/
import "C"

// threeBandC wraps the oracle's webrtc::ThreeBandFilterBank.
type threeBandC struct {
	h *C.aec3_three_band
}

func newThreeBandC() *threeBandC {
	return &threeBandC{h: C.aec3_three_band_create()}
}

func (t *threeBandC) close() {
	C.aec3_three_band_destroy(t.h)
	t.h = nil
}

// analysis splits in (480 samples) into 3x160, flat [band][160].
func (t *threeBandC) analysis(in *[480]float32) []float32 {
	out := make([]float32, 3*160)
	C.aec3_three_band_analysis(t.h, (*C.float)(&in[0]), (*C.float)(&out[0]))
	return out
}

// synthesis merges 3x160 (flat [band][160]) into 480 samples.
func (t *threeBandC) synthesis(in []float32) []float32 {
	out := make([]float32, 480)
	C.aec3_three_band_synthesis(t.h, (*C.float)(&in[0]), (*C.float)(&out[0]))
	return out
}

// analysisQMFC runs the oracle's WebRtcSpl_AnalysisQMF with
// caller-carried state (6 int32 words per branch).
func analysisQMFC(in []int16, state1, state2 *[6]int32) (low, high []int16) {
	low = make([]int16, len(in)/2)
	high = make([]int16, len(in)/2)
	C.aec3_analysis_qmf((*C.int16_t)(&in[0]), C.int(len(in)),
		(*C.int16_t)(&low[0]), (*C.int16_t)(&high[0]),
		(*C.int32_t)(&state1[0]), (*C.int32_t)(&state2[0]))
	return low, high
}

// synthesisQMFC runs the oracle's WebRtcSpl_SynthesisQMF.
func synthesisQMFC(low, high []int16, state1, state2 *[6]int32) []int16 {
	out := make([]int16, 2*len(low))
	C.aec3_synthesis_qmf((*C.int16_t)(&low[0]), (*C.int16_t)(&high[0]),
		C.int(len(low)), (*C.int16_t)(&out[0]),
		(*C.int32_t)(&state1[0]), (*C.int32_t)(&state2[0]))
	return out
}

// floatS16ToS16C applies webrtc::FloatS16ToS16 element-wise.
func floatS16ToS16C(src []float32) []int16 {
	dest := make([]int16, len(src))
	C.aec3_float_s16_to_s16((*C.float)(&src[0]), C.int(len(src)),
		(*C.int16_t)(&dest[0]))
	return dest
}

// s16ToFloatS16C applies webrtc::S16ToFloatS16 element-wise.
func s16ToFloatS16C(src []int16) []float32 {
	dest := make([]float32, len(src))
	C.aec3_s16_to_float_s16((*C.int16_t)(&src[0]), C.int(len(src)),
		(*C.float)(&dest[0]))
	return dest
}
