//go:build cgo

// Package bitreader pins the Go "bitreader" slice of the minimp3 port
// (libraries/mp3/internal/nativemp3: BitStream / BsInit / GetBits) against
// the vendored minimp3 C reference's MSB-first bit reader (bs_t / bs_init /
// get_bits, minimp3.h:206/241/248).
//
// The slice under test is integer-only: get_bits performs pure bitstream
// arithmetic and never touches floating point, so the cgo oracle and the Go
// port agree exactly in both build modes. The mp3_strict gate in
// parity_test.go therefore exists only to keep this suite's invariants
// consistent with the FP-bearing slices (IMDCT, synthesis) elsewhere in the
// port, and to document intent.
//
// bs_init / get_bits are `static` inside minimp3.h, so they cannot be
// referenced from a separate translation unit. The C side therefore compiles
// its OWN copy of minimp3 (bitreader_cgo_src.c includes minimp3.h with
// MINIMP3_IMPLEMENTATION) and surfaces each static via a mp3parity_*
// trampoline declared below — the same discipline the FLAC bitreader oracle
// and the MP3 main-bits oracle use. This package never imports libraries/mp3
// (which would compile minimp3 a second time and collide on its static
// symbols); it may import nativemp3.
//
// The scalar-baseline FP flags (-ffp-contract=off, -fno-vectorize, …) come
// from the mise task env (CGO_CFLAGS + CGO_CFLAGS_ALLOW), never from the
// in-source #cgo block below, because Go's cgo flag allowlist rejects them.
// This slice is integer-only so they do not affect its result either way.
package bitreader

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libminimp3
#cgo LDFLAGS: -lm
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable

#include <stdint.h>
#include <stdlib.h>

// bs_t (minimp3.h:206) is declared inside minimp3's MINIMP3_IMPLEMENTATION
// guard, so it is NOT visible from a plain #include of the header. Compiling
// the header with the implementation macro here would emit a second copy of
// minimp3 and collide with the one in bitreader_cgo_src.c. We therefore
// re-declare the struct layout-identically in this preamble — it is a tiny,
// stable public-shaped type (a borrowed byte pointer plus two ints) — and the
// trampolines in bitreader_cgo_src.c (which DOES include the implementation)
// take the real minimp3 bs_t; the two declarations are ABI-identical.
typedef struct
{
    const uint8_t *buf;
    int pos, limit;
} bs_t;

extern void     mp3parity_bs_init(bs_t *bs, const uint8_t *data, int bytes);
extern uint32_t mp3parity_get_bits(bs_t *bs, int n);
extern int      mp3parity_bs_pos(const bs_t *bs);
extern int      mp3parity_bs_limit(const bs_t *bs);
*/
import "C"

import "unsafe"

// cgoBitReader holds a C bs_t and the C-owned byte slab it reads from. The
// slab is allocated with C.malloc (not a Go slice) so that bs->buf — which
// bs_t borrows and the cgo pointer checker inspects on every call — is a C
// pointer rather than an unpinned Go pointer. free must be called to release
// it.
type cgoBitReader struct {
	bs   C.bs_t
	cbuf unsafe.Pointer
	n    int
}

// newCgoBitReader copies src into C memory and initializes a C bs_t over it
// via minimp3's bs_init.
func newCgoBitReader(src []byte) *cgoBitReader {
	r := &cgoBitReader{n: len(src)}
	var p *C.uint8_t
	if len(src) > 0 {
		r.cbuf = C.CBytes(src) // C.malloc'd copy; freed in free()
		p = (*C.uint8_t)(r.cbuf)
	}
	C.mp3parity_bs_init(&r.bs, p, C.int(len(src)))
	return r
}

// free releases the C-owned backing buffer.
func (r *cgoBitReader) free() {
	if r.cbuf != nil {
		C.free(r.cbuf)
		r.cbuf = nil
	}
}

// getBits reads n bits via minimp3's static get_bits.
func (r *cgoBitReader) getBits(n int) uint32 { return uint32(C.mp3parity_get_bits(&r.bs, C.int(n))) }

// pos returns the C reader's bit cursor (bs->pos).
func (r *cgoBitReader) pos() int { return int(C.mp3parity_bs_pos(&r.bs)) }

// limit returns the C reader's bit limit (bs->limit).
func (r *cgoBitReader) limit() int { return int(C.mp3parity_bs_limit(&r.bs)) }
