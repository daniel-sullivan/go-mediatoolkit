//go:build cgo

// Package bitallocation holds the bit-allocation parity slice: it pins the
// pure-Go nativemp3 port of minimp3's Layer III scalefactor decode
// (L3_decode_scalefactors and its helpers L3_read_scalefactors / L3_ldexp_q2 —
// the Layer III analog of per-band bit allocation) against the vendored C
// minimp3 reference compiled inline via cgo.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference (oracle.c, which includes
// the committed libraries/mp3/libminimp3/minimp3.h with
// MINIMP3_IMPLEMENTATION) so each go-test binary is symbol-self-contained, and
// it NEVER imports libraries/mp3 (only the internal nativemp3 port).
//
// minimp3's scalefactor routines are all file-static; oracle.c re-exports them
// through thin oracle_* wrappers in the same translation unit so the C side of
// every assertion is the genuine vendored code (see oracle.h). Inputs are
// fabricated through the library's own parser: oracle_read_side_info wraps the
// static L3_read_side_info to build a real L3_gr_info_t from a raw side-info
// byte buffer, and the Go side runs nativemp3.L3ReadSideInfo over the same
// bytes (see native.go).
//
// This slice IS floating-point-bearing: L3_ldexp_q2 expands the integer
// scalefactors into float32 band gains, so the scf[] comparison is only
// bit-exact under the mp3_strict build (FMA-free Go) against the
// -ffp-contract=off cgo oracle. The strict gate lives in parity_test.go.
package bitallocation

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libminimp3
#cgo LDFLAGS: -lm
#cgo CFLAGS: -DMINIMP3_ONLY_MP3
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable

#include <stdint.h>
#include <stdlib.h>
#include "oracle.h"
*/
import "C"

import "unsafe"

// cgoGrInfo mirrors the C oracle_gr_t (== minimp3's L3_gr_info_t). The Go side
// builds it via oracle_read_side_info and reads back the fields the decode
// step needs; it is an opaque C struct passed straight back into
// oracle_decode_scalefactors, so the parity test never reimplements side-info
// parsing on the C path.
type cgoGrInfo struct {
	gr [4]C.oracle_gr_t
}

func cHdrPtr(h []byte) *C.uint8_t { return (*C.uint8_t)(unsafe.Pointer(&h[0])) }

// cgoReadSideInfo runs the vendored static L3_read_side_info over side and
// reports whether it accepted the buffer; on success the granules are stored
// for cgoDecodeScalefactors. side is copied into C-owned storage for the call.
func cgoReadSideInfo(side []byte, hdr []byte) (*cgoGrInfo, bool) {
	buf := C.oracle_buf_new(cBytesPtr(side), C.int(len(side)))
	defer C.oracle_buf_free(buf)
	g := new(cgoGrInfo)
	ok := C.oracle_read_side_info(buf, C.int(len(side)), cHdrPtr(hdr), &g.gr[0]) != 0
	return g, ok
}

// cgoScfsi returns the scfsi field minimp3's L3_read_side_info stored for
// granule gi — the value that feeds L3_read_scalefactors. Exposed so the
// parity test can compare it against the Go port's gr.Scfsi directly (it is
// the field most sensitive to side-info parsing differences).
func (g *cgoGrInfo) scfsi(gi int) uint8 { return uint8(g.gr[gi].scfsi) }

// cgoDecodeScalefactors runs the vendored static L3_decode_scalefactors for
// granule gi / channel ch over main, seeding the ist_pos scratch with seed
// (39 bytes), and returns the resulting 40 float gains, 39 ist_pos bytes, and
// the final bit position.
func cgoDecodeScalefactors(g *cgoGrInfo, gi int, hdr, main []byte, ch int, seed []uint8) (scf []float32, istPos []uint8, bsPos int) {
	mbuf := C.oracle_buf_new(cBytesPtr(main), C.int(len(main)))
	defer C.oracle_buf_free(mbuf)

	var cScf [40]C.float
	var cIst [39]C.uint8_t
	var cPos C.int
	var cSeed [39]C.uint8_t
	for i := 0; i < 39; i++ {
		cSeed[i] = C.uint8_t(seed[i])
	}
	C.oracle_decode_scalefactors(cHdrPtr(hdr), mbuf, C.int(len(main)),
		&g.gr[gi], C.int(ch), &cSeed[0], &cScf[0], &cIst[0], &cPos)

	scf = make([]float32, 40)
	for i := 0; i < 40; i++ {
		scf[i] = float32(cScf[i])
	}
	istPos = make([]uint8, 39)
	for i := 0; i < 39; i++ {
		istPos[i] = uint8(cIst[i])
	}
	return scf, istPos, int(cPos)
}

// cBytesPtr returns a C pointer to the first byte of b, or nil for an empty
// slice. oracle_buf_new copies the bytes immediately, so passing a transient Go
// pointer here is safe (no Go pointer is retained in C memory across calls).
func cBytesPtr(b []byte) *C.uint8_t {
	if len(b) == 0 {
		return nil
	}
	return (*C.uint8_t)(unsafe.Pointer(&b[0]))
}
