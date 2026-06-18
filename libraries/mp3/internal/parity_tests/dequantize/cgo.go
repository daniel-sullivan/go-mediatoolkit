//go:build cgo

// Package dequantize contains parity tests that pin the Go Layer III
// scalefactor-dequantization slice of the minimp3 port
// (libraries/mp3/internal/nativemp3: L3ReadScalefactors, L3Ldexp,
// L3DecodeScalefactors) against the vendored minimp3 C reference.
//
// The slice mixes a pure-integer scalefactor unpack (L3_read_scalefactors and
// the integer body of L3_decode_scalefactors — bit-identical in any build)
// with a floating-point gain expansion (L3_ldexp_q2's float32 multiplies). The
// FP step is what the mp3_strict gate exists for: the cgo oracle is built with
// -ffp-contract=off (plus the vectorization / unroll flags) from the mise task
// env so each float multiply is separately rounded, and the strict Go build
// routes its multiplies through //go:noinline helpers so they cannot fuse into
// an FMA. The FP parity assertions therefore require the gain table to match
// bit-for-bit, which only holds under -tags=mp3_strict; the tests skip
// otherwise (see requireStrict in parity_test.go).
//
// L3_read_scalefactors, L3_ldexp_q2 and L3_decode_scalefactors are all `static`
// inside minimp3.h, so they cannot be referenced from a separate translation
// unit. The C side therefore compiles its OWN copy of minimp3
// (dequantize_cgo_src.c includes minimp3.h with MINIMP3_IMPLEMENTATION) and
// surfaces each static via a mp3parity_* trampoline declared below — the same
// discipline the other mp3 parity slices (bitreader, main-bits,
// bitstream-format, huffman-decode) use. This package never imports
// libraries/mp3 (which would compile minimp3 a second time and collide on its
// static symbols); it may import nativemp3.
//
// The scalar-baseline FP flags (-ffp-contract=off, -fno-vectorize, …) come
// from the mise task env (CGO_CFLAGS + CGO_CFLAGS_ALLOW), never from the
// in-source #cgo block below, because Go's cgo flag allowlist rejects them.
package dequantize

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libminimp3
#cgo LDFLAGS: -lm
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable

#include <stdint.h>

extern float mp3parity_l3_ldexp_q2(float y, int exp_q2);

extern void mp3parity_l3_read_scalefactors(uint8_t *scf, uint8_t *ist_pos,
                                           const uint8_t *scf_size, const uint8_t *scf_count,
                                           const uint8_t *payload, int payload_bytes, int bs_pos,
                                           int scfsi, int *out_pos);

extern void mp3parity_l3_decode_scalefactors(const uint8_t hdr[4], uint8_t *ist_pos,
                                             const uint8_t *payload, int payload_bytes, int bs_pos,
                                             uint16_t scalefac_compress,
                                             uint8_t global_gain,
                                             uint8_t scalefac_scale,
                                             uint8_t n_long_sfb,
                                             uint8_t n_short_sfb,
                                             const uint8_t subblock_gain[3],
                                             uint8_t preflag,
                                             uint8_t scfsi,
                                             int ch,
                                             float *scf, int *out_pos);
*/
import "C"

import "unsafe"

// cgoL3Ldexp returns the C L3_ldexp_q2(y, expQ2), the quarter-step
// power-of-two gain accumulator.
func cgoL3Ldexp(y float32, expQ2 int) float32 {
	return float32(C.mp3parity_l3_ldexp_q2(C.float(y), C.int(expQ2)))
}

// cgoL3ReadScalefactors drives the C L3_read_scalefactors over identical
// inputs to the Go port and returns the written scf and ist_pos arrays plus the
// final bs.pos.
//
// scf and istPos are the writable outputs (the caller sizes them iscf[40] /
// ist_pos[39]); scfSize and scfCount are the const inputs L3_read_scalefactors
// walks. payload aliases the bit reader's backing buffer for the call.
func cgoL3ReadScalefactors(scf, istPos []byte, scfSize [4]uint8, scfCount [28]uint8,
	payload []byte, bsPos, scfsi int) (outPos int) {

	var cPos C.int
	var pPayload *C.uint8_t
	if len(payload) > 0 {
		pPayload = (*C.uint8_t)(unsafe.Pointer(&payload[0]))
	}
	cSize := [4]C.uint8_t{C.uint8_t(scfSize[0]), C.uint8_t(scfSize[1]), C.uint8_t(scfSize[2]), C.uint8_t(scfSize[3])}
	var cCount [28]C.uint8_t
	for i := range scfCount {
		cCount[i] = C.uint8_t(scfCount[i])
	}

	C.mp3parity_l3_read_scalefactors(
		(*C.uint8_t)(unsafe.Pointer(&scf[0])),
		(*C.uint8_t)(unsafe.Pointer(&istPos[0])),
		&cSize[0], &cCount[0],
		pPayload, C.int(len(payload)), C.int(bsPos),
		C.int(scfsi), &cPos,
	)
	return int(cPos)
}

// cgoL3DecodeScalefactors drives the C L3_decode_scalefactors over identical
// inputs to the Go port and returns the expanded float gain table (40 entries,
// of which the first nLongSfb+nShortSfb are written) plus the final bs.pos.
//
// hdr is the 4 header bytes; istPos is this channel's 39-byte intensity-stereo
// scratch (mutated in place by the inner L3_read_scalefactors). The remaining
// scalars are the L3_gr_info_t members the function consumes.
func cgoL3DecodeScalefactors(hdr []byte, istPos []byte, payload []byte, bsPos int,
	scalefacCompress uint16, globalGain, scalefacScale, nLongSfb, nShortSfb uint8,
	subblockGain [3]uint8, preflag, scfsi uint8, ch int) (scf [40]float32, outPos int) {

	var cPos C.int
	var pPayload *C.uint8_t
	if len(payload) > 0 {
		pPayload = (*C.uint8_t)(unsafe.Pointer(&payload[0]))
	}
	cSub := [3]C.uint8_t{C.uint8_t(subblockGain[0]), C.uint8_t(subblockGain[1]), C.uint8_t(subblockGain[2])}

	C.mp3parity_l3_decode_scalefactors(
		(*C.uint8_t)(unsafe.Pointer(&hdr[0])),
		(*C.uint8_t)(unsafe.Pointer(&istPos[0])),
		pPayload, C.int(len(payload)), C.int(bsPos),
		C.uint16_t(scalefacCompress),
		C.uint8_t(globalGain),
		C.uint8_t(scalefacScale),
		C.uint8_t(nLongSfb),
		C.uint8_t(nShortSfb),
		&cSub[0],
		C.uint8_t(preflag),
		C.uint8_t(scfsi),
		C.int(ch),
		(*C.float)(unsafe.Pointer(&scf[0])),
		&cPos,
	)
	return scf, int(cPos)
}
