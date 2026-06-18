//go:build cgo

// Package stereodecoding contains parity tests that pin the Go Layer III
// stereo-reconstruction slice of the minimp3 port
// (libraries/mp3/internal/nativemp3: L3MidsideStereo, L3IntensityStereoBand,
// L3StereoTopBand, L3StereoProcess, L3IntensityStereo) against the vendored
// minimp3 C reference.
//
// The slice under test mixes integer/control-flow logic (the top-band scan
// and the trailing intensity-position fixup, bit-identical in any build) with
// floating-point arithmetic (the mid/side a+b / a-b sums and the kl / kr
// intensity weights derived from L3_ldexp_q2 and g_pan). The FP step is what
// the mp3_strict gate exists for: the cgo oracle is built with
// -ffp-contract=off (plus the vectorization / unroll flags) from the mise task
// env so each multiply/add is separately rounded, and the strict Go build
// routes the same multiplies/adds through //go:noinline helpers so they cannot
// fuse into an FMA. The parity assertions therefore require the reconstructed
// channels to match bit-for-bit, which only holds under -tags=mp3_strict; the
// tests skip otherwise (see requireStrict in parity_test.go).
//
// L3_midside_stereo, L3_intensity_stereo_band, L3_stereo_top_band,
// L3_stereo_process and L3_intensity_stereo are all `static` inside minimp3.h
// (minimp3.h:879..993), so they cannot be referenced from a separate
// translation unit. The C side therefore compiles its OWN copy of minimp3
// (stereo_decoding_cgo_src.c includes minimp3.h with MINIMP3_IMPLEMENTATION)
// and surfaces each static via a mp3parity_* trampoline declared below — the
// same discipline the other mp3 parity slices (bitreader, main-bits,
// huffman-decode, bitstream-format) use. This package never imports
// libraries/mp3 (which would compile minimp3 a second time and collide on its
// static symbols); it may import nativemp3.
//
// The scalar-baseline FP flags (-ffp-contract=off, -fno-vectorize, …) come
// from the mise task env (CGO_CFLAGS + CGO_CFLAGS_ALLOW), never from the
// in-source #cgo block below, because Go's cgo flag allowlist rejects them.
package stereodecoding

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libminimp3
#cgo LDFLAGS: -lm
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable

#include <stdint.h>

extern void mp3parity_l3_midside_stereo(float *left, int n);
extern void mp3parity_l3_intensity_stereo_band(float *left, int n, float kl, float kr);
extern void mp3parity_l3_stereo_top_band(const float *right, const uint8_t *sfb, int nbands, int max_band[3]);
extern void mp3parity_l3_stereo_process(float *left, const uint8_t *ist_pos, const uint8_t *sfb,
                                        const uint8_t *hdr, const int max_band[3], int mpeg2_sh);
extern void mp3parity_l3_intensity_stereo(float *left, uint8_t *ist_pos,
                                          const uint8_t *sfbtab,
                                          uint8_t n_long_sfb, uint8_t n_short_sfb,
                                          uint16_t gr1_scalefac_compress,
                                          const uint8_t *hdr);
*/
import "C"

import "unsafe"

// cgoL3MidsideStereo runs the C L3_midside_stereo over a copy of `left`
// (the 1152-sample granule buffer) for the first n samples and returns the
// mutated buffer.
func cgoL3MidsideStereo(left []float32, n int) []float32 {
	out := append([]float32(nil), left...)
	if len(out) > 0 {
		C.mp3parity_l3_midside_stereo((*C.float)(unsafe.Pointer(&out[0])), C.int(n))
	}
	return out
}

// cgoL3IntensityStereoBand runs the C L3_intensity_stereo_band over a copy of
// `left` for the first n samples with intensity weights kl / kr and returns
// the mutated buffer.
func cgoL3IntensityStereoBand(left []float32, n int, kl, kr float32) []float32 {
	out := append([]float32(nil), left...)
	if len(out) > 0 {
		C.mp3parity_l3_intensity_stereo_band((*C.float)(unsafe.Pointer(&out[0])), C.int(n), C.float(kl), C.float(kr))
	}
	return out
}

// cgoL3StereoTopBand runs the C L3_stereo_top_band over `right` (the right
// channel, i.e. grbuf+576) with band-width table sfb and returns the per-window
// max_band[3] result. right is not mutated.
func cgoL3StereoTopBand(right []float32, sfb []byte, nbands int) [3]int {
	var mb [3]C.int
	var rp *C.float
	if len(right) > 0 {
		rp = (*C.float)(unsafe.Pointer(&right[0]))
	}
	var sp *C.uint8_t
	if len(sfb) > 0 {
		sp = (*C.uint8_t)(unsafe.Pointer(&sfb[0]))
	}
	C.mp3parity_l3_stereo_top_band(rp, sp, C.int(nbands), &mb[0])
	return [3]int{int(mb[0]), int(mb[1]), int(mb[2])}
}

// cgoL3StereoProcess runs the C L3_stereo_process over a copy of `left` (the
// granule buffer) given the per-band intensity positions istPos, band-width
// table sfb, raw 4-byte header hdr, per-window maxBand, and the MPEG-2 shift
// mpeg2Sh, returning the mutated buffer. max_band is taken by value by the
// trampoline so it is not disturbed.
func cgoL3StereoProcess(left []float32, istPos, sfb, hdr []byte, maxBand [3]int, mpeg2Sh int) []float32 {
	out := append([]float32(nil), left...)
	mb := [3]C.int{C.int(maxBand[0]), C.int(maxBand[1]), C.int(maxBand[2])}
	var ip, sp *C.uint8_t
	if len(istPos) > 0 {
		ip = (*C.uint8_t)(unsafe.Pointer(&istPos[0]))
	}
	if len(sfb) > 0 {
		sp = (*C.uint8_t)(unsafe.Pointer(&sfb[0]))
	}
	hp := (*C.uint8_t)(unsafe.Pointer(&hdr[0]))
	if len(out) > 0 {
		C.mp3parity_l3_stereo_process((*C.float)(unsafe.Pointer(&out[0])), ip, sp, hp, &mb[0], C.int(mpeg2Sh))
	}
	return out
}

// cgoL3IntensityStereo runs the C L3_intensity_stereo over a copy of `left`
// (the granule buffer) and a copy of istPos (which the trailing-position fixup
// mutates), reassembling gr[0] from sfbtab / nLongSfb / nShortSfb and carrying
// gr1ScalefacCompress for the mpeg2_sh derivation. Returns the mutated buffer
// and the mutated istPos.
func cgoL3IntensityStereo(left []float32, istPos, sfbtab []byte,
	nLongSfb, nShortSfb uint8, gr1ScalefacCompress uint16, hdr []byte) (outLeft []float32, outIstPos []byte) {

	outLeft = append([]float32(nil), left...)
	outIstPos = append([]byte(nil), istPos...)
	var lp *C.float
	if len(outLeft) > 0 {
		lp = (*C.float)(unsafe.Pointer(&outLeft[0]))
	}
	var ip *C.uint8_t
	if len(outIstPos) > 0 {
		ip = (*C.uint8_t)(unsafe.Pointer(&outIstPos[0]))
	}
	var sbp *C.uint8_t
	if len(sfbtab) > 0 {
		sbp = (*C.uint8_t)(unsafe.Pointer(&sfbtab[0]))
	}
	hp := (*C.uint8_t)(unsafe.Pointer(&hdr[0]))
	C.mp3parity_l3_intensity_stereo(lp, ip, sbp,
		C.uint8_t(nLongSfb), C.uint8_t(nShortSfb), C.uint16_t(gr1ScalefacCompress), hp)
	return outLeft, outIstPos
}
