//go:build cgo

// Package fvad_sp is the parity slice for the libfvad signal-processing
// layer: the SPL inline helpers (NormW32/NormU32/GetSizeInBits), the
// division, scaling-square and energy primitives, the whole
// resample-by-2 / 48→32 / 48→8 kHz resampler family, and vad_sp.c's
// Downsampling and FindMinimum. It checks the cgo-free Go port in
// vad/internal/fvad against the vendored C oracle bit-for-bit — libfvad
// is pure integer arithmetic, so every assertion here is exact equality
// with no strict-mode tag and no CGO_CFLAGS requirements (unlike the
// floating-point loudness/flac slices, this package runs identically
// under a plain `go test ./...`).
//
// Like every parity slice in this repo it compiles its OWN copy of the
// vendored C (see fvad_sp_cgo_src.c): libfvad's WebRtc* symbols are
// ordinary non-static C, so two packages compiling them into one test
// binary would collide at link time — each slice is therefore a
// self-contained package/binary that imports only the cgo-FREE
// vad/internal/fvad port. cgo call sites live in this file because Go
// does not allow `import "C"` in _test.go files; parity_test.go calls
// these wrappers.
package fvad_sp

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libfvad/src

#include <stdint.h>
#include <stddef.h>
#include "vad/vad_sp.h"
#include "signal_processing/resample_by_2_internal.h"
*/
import "C"

import "unsafe"

// --- SPL inline helpers (spl_inl.h; static inline, so this TU carries
// its own copies backed by the lookup table in the src TU) ---

func cNormW32(a int32) int16  { return int16(C.WebRtcSpl_NormW32(C.int32_t(a))) }
func cNormU32(a uint32) int16 { return int16(C.WebRtcSpl_NormU32(C.uint32_t(a))) }
func cGetSizeInBits(n uint32) int16 {
	return int16(C.WebRtcSpl_GetSizeInBits(C.uint32_t(n)))
}
func cCountLeadingZeros32NotBuiltin(n uint32) int {
	return int(C.WebRtcSpl_CountLeadingZeros32_NotBuiltin(C.uint32_t(n)))
}

// --- division_operations.c ---

func cDivW32W16(num int32, den int16) int32 {
	return int32(C.WebRtcSpl_DivW32W16(C.int32_t(num), C.int16_t(den)))
}

// --- get_scaling_square.c / energy.c ---

func cGetScalingSquare(in []int16, times int) int16 {
	return int16(C.WebRtcSpl_GetScalingSquare(
		(*C.int16_t)(unsafe.Pointer(&in[0])), C.size_t(len(in)), C.size_t(times)))
}

func cEnergy(vector []int16) (int32, int) {
	var scale C.int
	en := C.WebRtcSpl_Energy((*C.int16_t)(unsafe.Pointer(&vector[0])),
		C.size_t(len(vector)), &scale)
	return int32(en), int(scale)
}

// --- resample_by_2_internal.c ---

func cDownBy2ShortToInt(in []int16, n int, out []int32, state []int32) {
	C.WebRtcSpl_DownBy2ShortToInt(
		(*C.int16_t)(unsafe.Pointer(&in[0])), C.int32_t(n),
		(*C.int32_t)(unsafe.Pointer(&out[0])),
		(*C.int32_t)(unsafe.Pointer(&state[0])))
}

func cDownBy2IntToShort(in []int32, n int, out []int16, state []int32) {
	C.WebRtcSpl_DownBy2IntToShort(
		(*C.int32_t)(unsafe.Pointer(&in[0])), C.int32_t(n),
		(*C.int16_t)(unsafe.Pointer(&out[0])),
		(*C.int32_t)(unsafe.Pointer(&state[0])))
}

func cLPBy2IntToInt(in []int32, n int, out []int32, state []int32) {
	C.WebRtcSpl_LPBy2IntToInt(
		(*C.int32_t)(unsafe.Pointer(&in[0])), C.int32_t(n),
		(*C.int32_t)(unsafe.Pointer(&out[0])),
		(*C.int32_t)(unsafe.Pointer(&state[0])))
}

// --- resample_fractional.c ---

func cResample48khzTo32khz(in []int32, out []int32, k int) {
	C.WebRtcSpl_Resample48khzTo32khz(
		(*C.int32_t)(unsafe.Pointer(&in[0])),
		(*C.int32_t)(unsafe.Pointer(&out[0])), C.size_t(k))
}

// --- resample_48khz.c ---

// cState48To8 wraps the C resampler state so the parity test can
// compare the carried state after every block.
type cState48To8 struct {
	st C.WebRtcSpl_State48khzTo8khz
}

func newCState48To8() *cState48To8 {
	s := new(cState48To8)
	C.WebRtcSpl_ResetResample48khzTo8khz(&s.st)
	return s
}

func (s *cState48To8) resample(in []int16, out []int16, tmpmem []int32) {
	C.WebRtcSpl_Resample48khzTo8khz(
		(*C.int16_t)(unsafe.Pointer(&in[0])),
		(*C.int16_t)(unsafe.Pointer(&out[0])),
		&s.st,
		(*C.int32_t)(unsafe.Pointer(&tmpmem[0])))
}

// snapshot returns the four state arrays as Go slices (copied).
func (s *cState48To8) snapshot() (s4824, s2424, s2416, s168 []int32) {
	cp := func(p *C.int32_t, n int) []int32 {
		out := make([]int32, n)
		src := unsafe.Slice((*int32)(unsafe.Pointer(p)), n)
		copy(out, src)
		return out
	}
	return cp(&s.st.S_48_24[0], 8), cp(&s.st.S_24_24[0], 16),
		cp(&s.st.S_24_16[0], 8), cp(&s.st.S_16_8[0], 8)
}

// --- vad_sp.c ---

func cDownsampling(signalIn, signalOut []int16, filterState []int32, inLength int) {
	C.WebRtcVad_Downsampling(
		(*C.int16_t)(unsafe.Pointer(&signalIn[0])),
		(*C.int16_t)(unsafe.Pointer(&signalOut[0])),
		(*C.int32_t)(unsafe.Pointer(&filterState[0])),
		C.size_t(inLength))
}

// cFindMinimumInst wraps a C VadInstT with only the fields FindMinimum
// touches initialized (mirroring what WebRtcVad_InitCore sets for
// them): low_value_vector = 10000, index_vector = 0, mean_value = 1600,
// frame_counter = 0.
type cFindMinimumInst struct {
	inst C.VadInstT
}

func newCFindMinimumInst() *cFindMinimumInst {
	h := new(cFindMinimumInst)
	for i := range h.inst.low_value_vector {
		h.inst.low_value_vector[i] = 10000
		h.inst.index_vector[i] = 0
	}
	for i := range h.inst.mean_value {
		h.inst.mean_value[i] = 1600
	}
	h.inst.frame_counter = 0
	return h
}

func (h *cFindMinimumInst) setFrameCounter(n int32) { h.inst.frame_counter = C.int32_t(n) }

func (h *cFindMinimumInst) findMinimum(value int16, channel int) int16 {
	return int16(C.WebRtcVad_FindMinimum(&h.inst, C.int16_t(value), C.int(channel)))
}

// snapshot copies the per-channel minimum-tracking state.
func (h *cFindMinimumInst) snapshot() (lowValues, ages [96]int16, means [6]int16) {
	for i := 0; i < 96; i++ {
		lowValues[i] = int16(h.inst.low_value_vector[i])
		ages[i] = int16(h.inst.index_vector[i])
	}
	for i := 0; i < 6; i++ {
		means[i] = int16(h.inst.mean_value[i])
	}
	return
}
