//go:build cgo

// Package imdctsynthesisfilterbank contains parity tests that pin the Go
// IMDCT + polyphase-synthesis-filterbank slice of the minimp3 port
// (libraries/mp3/internal/nativemp3: l3IMDCTGr, l3ChangeSign, mp3dDCTII,
// mp3dSynthGranule and the helpers they drive) against the vendored minimp3 C
// reference.
//
// The slice is floating point throughout (the inverse MDCT windowing, the
// 32-point DCT-II, and the synthesis overlap-add), so its output only matches
// the cgo oracle bit-for-bit when the FMA-free strict Go build is paired with
// the scalar (-ffp-contract=off, -fno-vectorize, …) cgo oracle the mise
// `parity` task configures. The parity assertions therefore require bit-exact
// matches, which only hold under -tags=mp3_strict; the tests skip otherwise
// (see requireStrict in parity_test.go).
//
// L3_imdct_gr, L3_change_sign, mp3d_DCT_II and mp3d_synth_granule (with all
// their helpers) are `static` inside minimp3.h, so they cannot be referenced
// from a separate translation unit. The C side therefore compiles its OWN copy
// of minimp3 (imdct_synthesis_cgo_src.c includes minimp3.h with
// MINIMP3_IMPLEMENTATION and MINIMP3_NO_SIMD) and surfaces each driver via a
// mp3parity_* trampoline declared below — the same discipline the other mp3
// parity slices (bitreader, main-bits, bitstream-format, huffman-decode) use.
// This package never imports libraries/mp3 (which would compile minimp3 a
// second time and collide on its static symbols); it may import nativemp3.
//
// The scalar-baseline FP flags (-ffp-contract=off, -fno-vectorize, …) come
// from the mise task env (CGO_CFLAGS + CGO_CFLAGS_ALLOW), never from the
// in-source #cgo block below, because Go's cgo flag allowlist rejects them.
package imdctsynthesisfilterbank

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libminimp3
#cgo LDFLAGS: -lm
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable

#include <stdint.h>

extern void mp3parity_l3_imdct_gr(float *grbuf, float *overlap, unsigned block_type, unsigned n_long_bands);
extern void mp3parity_l3_change_sign(float *grbuf);
extern void mp3parity_mp3d_dct_ii(float *grbuf, int n);
extern void mp3parity_mp3d_synth_granule(float *qmf_state, float *grbuf, int nbands, int nch,
                                         int16_t *pcm, float *lins);
*/
import "C"

import "unsafe"

// cgoL3IMDCTGr runs the C L3_imdct_gr followed by L3_change_sign over a copy of
// the granule buffer and overlap history, returning the mutated grbuf (576
// floats) and overlap (9*32 floats). It mirrors the decoder's
// L3_imdct_gr + L3_change_sign pair (minimp3.h:1269-1270).
func cgoL3IMDCTGr(grbuf [576]float32, overlap [9 * 32]float32, blockType uint8, nLongBands uint) (outGrbuf [576]float32, outOverlap [9 * 32]float32) {
	outGrbuf = grbuf
	outOverlap = overlap
	C.mp3parity_l3_imdct_gr(
		(*C.float)(unsafe.Pointer(&outGrbuf[0])),
		(*C.float)(unsafe.Pointer(&outOverlap[0])),
		C.unsigned(blockType), C.unsigned(nLongBands),
	)
	return outGrbuf, outOverlap
}

// cgoL3ChangeSign runs the C L3_change_sign over a copy of grbuf and returns
// the mutated buffer.
func cgoL3ChangeSign(grbuf [576]float32) [576]float32 {
	out := grbuf
	C.mp3parity_l3_change_sign((*C.float)(unsafe.Pointer(&out[0])))
	return out
}

// cgoMp3dDCTII runs the C mp3d_DCT_II in place over a copy of the n-wide column
// block and returns the result.
func cgoMp3dDCTII(grbuf [576]float32, n int) [576]float32 {
	out := grbuf
	C.mp3parity_mp3d_dct_ii((*C.float)(unsafe.Pointer(&out[0])), C.int(n))
	return out
}

// cgoMp3dSynthGranule runs the C mp3d_synth_granule end-to-end over copies of
// the qmf history and granule buffer, returning the interleaved int16 PCM block
// it emits and the updated qmf_state. nch channels of 576 floats live flat in
// grbuf; the syn scratch lins is allocated here and sized (18+15)*64.
func cgoMp3dSynthGranule(qmfState [15 * 2 * 32]float32, grbuf []float32, nbands, nch int) (pcm []int16, outQmf [15 * 2 * 32]float32) {
	outQmf = qmfState
	gr := append([]float32(nil), grbuf...)
	lins := make([]float32, (18+15)*64)
	pcm = make([]int16, 32*nbands*nch)

	C.mp3parity_mp3d_synth_granule(
		(*C.float)(unsafe.Pointer(&outQmf[0])),
		(*C.float)(unsafe.Pointer(&gr[0])),
		C.int(nbands), C.int(nch),
		(*C.int16_t)(unsafe.Pointer(&pcm[0])),
		(*C.float)(unsafe.Pointer(&lins[0])),
	)
	return pcm, outQmf
}
