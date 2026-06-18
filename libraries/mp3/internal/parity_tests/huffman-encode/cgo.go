// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package huffmanencode holds the huffman-encode parity slice: it pins the
// pure-Go nativemp3 port of LAME 3.100's encoder-side bit writer
// (putbits2 / putbits_noheaders / putheader_bits, bitstream_encode.go)
// against the vendored LAME C reference compiled inline via cgo.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference
// (huffman_encode_cgo_src.c, which #includes the committed
// libraries/mp3/liblame bitstream.c + tables.c) so each go-test binary is
// symbol-self-contained, and it NEVER imports libraries/mp3 (only the
// internal nativemp3 port).
//
// LAME's bit writer is file-static; huffman_encode_cgo_src.c re-exports it
// through thin mp3parity_* trampolines in the same translation unit so the C
// side of every assertion is the genuine vendored code.
//
// SCOPE — bit writer only. The Huffman code emitters (Huffmancode /
// huffman_coder_count1 / Short/LongHuffmancodebits) that share this slice are
// NOT pinned yet: the Go ht[] codebook array is declared but still empty
// (huffman_encode.go:51) and the emitter methods are unexported, so there is
// nothing to compare. Extend this package once the tables.c port populates
// ht[] and the emitters are exported.
//
// This slice is integer-only and so is bit-identical regardless of build tag
// or vectorization; the strict-gated assertions (parity_test.go) merely make
// the bit-exact contract explicit and ride the same mp3_strict gate as the
// FP slices.
package huffmanencode

/*
#cgo CFLAGS: -DHAVE_CONFIG_H
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../liblame
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/libmp3lame
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable
#cgo CFLAGS: -Wno-shift-negative-value -Wno-absolute-value -Wno-tautological-pointer-compare

#include <stdint.h>
#include <stdlib.h>

// mp3parity_enc_t is an opaque handle over a heap lame_internal_flags; the Go
// side never inspects its layout, only passing the pointer back to the
// trampolines. The C TU casts it to the real lame_internal_flags.
typedef struct mp3parity_enc_t mp3parity_enc_t;

extern mp3parity_enc_t *mp3parity_enc_new(int buf_size);
extern void mp3parity_enc_free(mp3parity_enc_t *e);
extern void mp3parity_enc_set_sideinfo_len(mp3parity_enc_t *e, int len);
extern void mp3parity_enc_prime_header(mp3parity_enc_t *e, int slot, int write_timing,
                                       const unsigned char *buf, int len);
extern void mp3parity_enc_set_wptr(mp3parity_enc_t *e, int w_ptr);
extern void mp3parity_enc_disarm_headers(mp3parity_enc_t *e, int sentinel);

extern void mp3parity_putbits2(mp3parity_enc_t *e, int val, int j);
extern void mp3parity_putbits_noheaders(mp3parity_enc_t *e, int val, int j);
extern void mp3parity_putheader_bits(mp3parity_enc_t *e);

extern int  mp3parity_enc_totbit(const mp3parity_enc_t *e);
extern int  mp3parity_enc_buf_byte_idx(const mp3parity_enc_t *e);
extern int  mp3parity_enc_buf_bit_idx(const mp3parity_enc_t *e);
extern int  mp3parity_enc_wptr(const mp3parity_enc_t *e);
extern void mp3parity_enc_copy_buf(const mp3parity_enc_t *e, unsigned char *out, int n);
*/
import "C"

import "unsafe"

// cgoEnc drives the vendored LAME bit writer over a C-owned
// lame_internal_flags. The Go-visible state (totbit, byte/bit cursors, w_ptr,
// and the output bytes) is read back through the trampolines so the nativemp3
// EncFlags can be compared field-for-field.
type cgoEnc struct {
	e       *C.mp3parity_enc_t
	bufSize int
}

func newCgoEnc(bufSize int) *cgoEnc {
	return &cgoEnc{e: C.mp3parity_enc_new(C.int(bufSize)), bufSize: bufSize}
}

func (c *cgoEnc) free() { C.mp3parity_enc_free(c.e) }

func (c *cgoEnc) setSideinfoLen(n int) { C.mp3parity_enc_set_sideinfo_len(c.e, C.int(n)) }

func (c *cgoEnc) primeHeader(slot, writeTiming int, buf []byte) {
	var p *C.uchar
	if len(buf) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&buf[0]))
	}
	C.mp3parity_enc_prime_header(c.e, C.int(slot), C.int(writeTiming), p, C.int(len(buf)))
}

func (c *cgoEnc) setWPtr(w int)              { C.mp3parity_enc_set_wptr(c.e, C.int(w)) }
func (c *cgoEnc) disarmHeaders(sentinel int) { C.mp3parity_enc_disarm_headers(c.e, C.int(sentinel)) }

func (c *cgoEnc) putBits2(val, j int) { C.mp3parity_putbits2(c.e, C.int(val), C.int(j)) }
func (c *cgoEnc) putBitsNoHeaders(val, j int) {
	C.mp3parity_putbits_noheaders(c.e, C.int(val), C.int(j))
}
func (c *cgoEnc) putHeaderBits() { C.mp3parity_putheader_bits(c.e) }

func (c *cgoEnc) totbit() int     { return int(C.mp3parity_enc_totbit(c.e)) }
func (c *cgoEnc) bufByteIdx() int { return int(C.mp3parity_enc_buf_byte_idx(c.e)) }
func (c *cgoEnc) bufBitIdx() int  { return int(C.mp3parity_enc_buf_bit_idx(c.e)) }
func (c *cgoEnc) wPtr() int       { return int(C.mp3parity_enc_wptr(c.e)) }

// bytes copies n output bytes out of the C bit-stream buffer.
func (c *cgoEnc) bytes(n int) []byte {
	out := make([]byte, n)
	if n == 0 {
		return out
	}
	C.mp3parity_enc_copy_buf(c.e, (*C.uchar)(unsafe.Pointer(&out[0])), C.int(n))
	return out
}
