//go:build cgo

// Package huffmandecode contains parity tests that pin the Go Layer III
// Huffman-unpacking slice of the minimp3 port
// (libraries/mp3/internal/nativemp3: L3Huffman, L3Pow43, gPow43) against the
// vendored minimp3 C reference.
//
// The slice under test mixes an integer Huffman tree traversal (bit-identical
// in any build) with a floating-point dequantization step (the g_pow43 lookups
// and L3_pow_43 polynomial). The FP step is what the mp3_strict gate exists
// for: the cgo oracle is built with -ffp-contract=off (plus the
// vectorization / unroll flags) from the mise task env so each `a + b*c` is two
// separately rounded float operations, and the strict Go build routes the same
// multiplies/adds through //go:noinline helpers so they cannot fuse into an
// FMA. The parity assertions therefore require the dequantized lines to match
// bit-for-bit, which only holds under -tags=mp3_strict; the tests skip
// otherwise (see requireStrict in parity_test.go).
//
// L3_huffman, L3_pow_43 and g_pow43 are all `static` inside minimp3.h, so they
// cannot be referenced from a separate translation unit. The C side therefore
// compiles its OWN copy of minimp3 (huffman_decode_cgo_src.c includes
// minimp3.h with MINIMP3_IMPLEMENTATION) and surfaces each static via a
// mp3parity_* trampoline declared below — the same discipline the other mp3
// parity slices (bitreader, main-bits, bitstream-format) use. This package
// never imports libraries/mp3 (which would compile minimp3 a second time and
// collide on its static symbols); it may import nativemp3.
//
// The scalar-baseline FP flags (-ffp-contract=off, -fno-vectorize, …) come
// from the mise task env (CGO_CFLAGS + CGO_CFLAGS_ALLOW), never from the
// in-source #cgo block below, because Go's cgo flag allowlist rejects them.
package huffmandecode

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libminimp3
#cgo LDFLAGS: -lm
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable

#include <stdint.h>

extern float mp3parity_l3_pow_43(int x);
extern void  mp3parity_g_pow43(float *out);
extern void  mp3parity_l3_huffman(float *dst,
                                  const uint8_t *payload, int payload_bytes, int bs_pos,
                                  const uint8_t *sfbtab,
                                  uint16_t big_values,
                                  const uint8_t table_select[3],
                                  const uint8_t region_count[3],
                                  uint8_t count1_table,
                                  const float *scf,
                                  int layer3gr_limit,
                                  int *out_pos);
*/
import "C"

import "unsafe"

// cgoL3Pow43 returns the C L3_pow_43(x), the dequantization power function.
func cgoL3Pow43(x int) float32 { return float32(C.mp3parity_l3_pow_43(C.int(x))) }

// cgoGPow43 copies the C g_pow43[129+16] table out for an entry-for-entry
// transcription check against the Go gPow43.
func cgoGPow43() [129 + 16]float32 {
	var out [129 + 16]float32
	C.mp3parity_g_pow43((*C.float)(unsafe.Pointer(&out[0])))
	return out
}

// cgoL3Huffman drives the C L3_huffman over identical inputs to the Go port
// and returns the 576 dequantized frequency lines plus the final bs.pos.
//
// payload aliases the reassembled main-data buffer the bit reader walks;
// sfbtab and scf are the const arrays L3_huffman consumes with *sfb++ / *scf++.
// All three are kept alive across the call by the caller's slices.
func cgoL3Huffman(payload []byte, bsPos int, sfbtab []byte, bigValues uint16,
	tableSelect, regionCount [3]uint8, count1Table uint8, scf []float32,
	layer3grLimit int) (dst [576]float32, outPos int) {

	var cPos C.int
	var pPayload *C.uint8_t
	if len(payload) > 0 {
		pPayload = (*C.uint8_t)(unsafe.Pointer(&payload[0]))
	}
	var pScf *C.float
	if len(scf) > 0 {
		pScf = (*C.float)(unsafe.Pointer(&scf[0]))
	}
	cTab := [3]C.uint8_t{C.uint8_t(tableSelect[0]), C.uint8_t(tableSelect[1]), C.uint8_t(tableSelect[2])}
	cReg := [3]C.uint8_t{C.uint8_t(regionCount[0]), C.uint8_t(regionCount[1]), C.uint8_t(regionCount[2])}

	C.mp3parity_l3_huffman(
		(*C.float)(unsafe.Pointer(&dst[0])),
		pPayload, C.int(len(payload)), C.int(bsPos),
		(*C.uint8_t)(unsafe.Pointer(&sfbtab[0])),
		C.uint16_t(bigValues),
		&cTab[0], &cReg[0],
		C.uint8_t(count1Table),
		pScf,
		C.int(layer3grLimit),
		&cPos,
	)
	return dst, int(cPos)
}
