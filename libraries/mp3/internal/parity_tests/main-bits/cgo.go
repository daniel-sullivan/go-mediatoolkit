//go:build cgo

// Package mainbits contains parity tests that pin the Go "main-bits"
// slice of the minimp3 port (libraries/mp3/internal/nativemp3) against
// the vendored minimp3 C reference.
//
// The slice under test is integer-only: the bit reader (bs_init /
// get_bits), the frame-sync helpers (the hdr_* field accessors,
// hdr_valid, hdr_compare, mp3d_match_frame, mp3d_find_frame), and the
// bit-reservoir reassembly (L3_save_reservoir / L3_restore_reservoir).
// Because none of these touch floating point, the comparison is exact in
// both build modes; the strict gate in parity_test.go exists only to keep
// the suite's invariants consistent with the FP-bearing slices and to
// document intent.
//
// All of the functions exercised here are `static` inside minimp3.h, so
// they cannot be referenced from a separate translation unit. The C side
// therefore compiles its OWN copy of minimp3 (main_bits_cgo_src.c includes
// minimp3.h with MINIMP3_IMPLEMENTATION) and surfaces each static via a
// `mp3parity_*` trampoline declared below — the same discipline the FLAC
// foundation oracle uses for the static helpers in md5.c. This package
// never imports libraries/mp3 (which would compile minimp3 a second time
// and collide on its static symbols); it may import nativemp3.
//
// The C oracle is the vendored minimp3 under libraries/mp3/libminimp3,
// configured for a scalar baseline. The scalar FP flags
// (-ffp-contract=off, -fno-vectorize, …) come from the mise task env
// (CGO_CFLAGS + CGO_CFLAGS_ALLOW), never from the in-source #cgo block
// below, because Go's cgo flag allowlist rejects them.
package mainbits

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libminimp3
#cgo LDFLAGS: -lm
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// minimp3 public types we hand across the boundary. main_bits_cgo_src.c
// includes minimp3.h (with the implementation macro) and defines the
// trampolines; here we include the header WITHOUT the implementation macro
// so the public mp3dec_t (declared above the MINIMP3_IMPLEMENTATION guard)
// is visible and the Go side can allocate it.
#include "minimp3.h"

// bs_t and the bit-reservoir sizing macros live INSIDE minimp3's
// MINIMP3_IMPLEMENTATION guard (minimp3.h:206 / :54 / :56), so they are not
// visible from the plain #include above. Re-declare them here, layout- and
// value-identically — bs_t is a tiny stable public-shaped type (a borrowed
// byte pointer plus two ints); the trampolines in main_bits_cgo_src.c (which
// DOES include the implementation) take the real minimp3 bs_t, ABI-identical
// to this one. This is the same discipline the bitreader oracle uses.
typedef struct
{
    const uint8_t *buf;
    int pos, limit;
} bs_t;

#define MAX_L3_FRAME_PAYLOAD_BYTES  2304
#define MAX_BITRESERVOIR_BYTES      511

// bit reader (bs_t) trampolines.
extern void     mp3parity_bs_init(bs_t *bs, const uint8_t *data, int bytes);
extern uint32_t mp3parity_get_bits(bs_t *bs, int n);
extern int      mp3parity_bs_pos(const bs_t *bs);
extern int      mp3parity_bs_limit(const bs_t *bs);

// frame-header field accessor trampolines (HDR_* macros + hdr_* statics).
extern int      mp3parity_hdr_valid(const uint8_t *h);
extern int      mp3parity_hdr_compare(const uint8_t *h1, const uint8_t *h2);
extern unsigned mp3parity_hdr_bitrate_kbps(const uint8_t *h);
extern unsigned mp3parity_hdr_sample_rate_hz(const uint8_t *h);
extern unsigned mp3parity_hdr_frame_samples(const uint8_t *h);
extern int      mp3parity_hdr_frame_bytes(const uint8_t *h, int free_format_size);
extern int      mp3parity_hdr_padding(const uint8_t *h);

// frame-sync trampolines.
extern int      mp3parity_match_frame(const uint8_t *hdr, int mp3_bytes, int frame_bytes);
extern int      mp3parity_find_frame(const uint8_t *mp3, int mp3_bytes, int *free_format_bytes, int *ptr_frame_bytes);

// reservoir trampolines. These drive a minimp3 mp3dec_t + mp3dec_scratch_t
// the way the C decode loop does, exposing only the bytes the parity test
// inspects (the reassembled maindata and the resulting bs limit).
extern void     mp3parity_save_reservoir(mp3dec_t *h, uint8_t *scratch_maindata, int bs_pos, int bs_limit);
extern int      mp3parity_restore_reservoir(mp3dec_t *h, const uint8_t *payload, int payload_bytes,
                                            int main_data_begin, uint8_t *out_maindata, int *out_limit);
*/
import "C"

import "unsafe"

// cgoBsInit / cgoGetBits drive a C bs_t through the minimp3 static bit
// reader and return its observable state, so the Go BitStream port can be
// compared op-for-op.

// cgoBitReader holds a C bs_t and the C-owned byte slab it reads from. The
// slab is C.malloc'd (not a Go slice) so bs->buf — which bs_t borrows and the
// cgo pointer checker inspects on every call — is a C pointer rather than an
// unpinned Go pointer. free must be called to release it.
type cgoBitReader struct {
	bs   C.bs_t
	cbuf unsafe.Pointer
}

func newCgoBitReader(src []byte) *cgoBitReader {
	r := new(cgoBitReader)
	var p *C.uint8_t
	if len(src) > 0 {
		r.cbuf = C.CBytes(src) // C.malloc'd copy; freed in free()
		p = (*C.uint8_t)(r.cbuf)
	}
	C.mp3parity_bs_init(&r.bs, p, C.int(len(src)))
	return r
}

func (r *cgoBitReader) free() {
	if r.cbuf != nil {
		C.free(r.cbuf)
		r.cbuf = nil
	}
}

func (r *cgoBitReader) getBits(n int) uint32 { return uint32(C.mp3parity_get_bits(&r.bs, C.int(n))) }
func (r *cgoBitReader) pos() int             { return int(C.mp3parity_bs_pos(&r.bs)) }
func (r *cgoBitReader) limit() int           { return int(C.mp3parity_bs_limit(&r.bs)) }

func cHdrPtr(h []byte) *C.uint8_t { return (*C.uint8_t)(unsafe.Pointer(&h[0])) }

func cgoHdrValid(h []byte) bool { return C.mp3parity_hdr_valid(cHdrPtr(h)) != 0 }
func cgoHdrCompare(h1, h2 []byte) bool {
	return C.mp3parity_hdr_compare(cHdrPtr(h1), cHdrPtr(h2)) != 0
}
func cgoHdrBitrateKbps(h []byte) uint  { return uint(C.mp3parity_hdr_bitrate_kbps(cHdrPtr(h))) }
func cgoHdrSampleRateHz(h []byte) uint { return uint(C.mp3parity_hdr_sample_rate_hz(cHdrPtr(h))) }
func cgoHdrFrameSamples(h []byte) uint { return uint(C.mp3parity_hdr_frame_samples(cHdrPtr(h))) }
func cgoHdrFrameBytes(h []byte, freeFormatSize int) int {
	return int(C.mp3parity_hdr_frame_bytes(cHdrPtr(h), C.int(freeFormatSize)))
}
func cgoHdrPadding(h []byte) int { return int(C.mp3parity_hdr_padding(cHdrPtr(h))) }

func cgoMatchFrame(hdr []byte, mp3Bytes, frameBytes int) bool {
	return C.mp3parity_match_frame(cHdrPtr(hdr), C.int(mp3Bytes), C.int(frameBytes)) != 0
}

func cgoFindFrame(mp3 []byte, mp3Bytes int, freeFormatBytes *int) (off, frameBytes int) {
	cff := C.int(*freeFormatBytes)
	var cfb C.int
	off = int(C.mp3parity_find_frame((*C.uint8_t)(unsafe.Pointer(&mp3[0])), C.int(mp3Bytes), &cff, &cfb))
	*freeFormatBytes = int(cff)
	return off, int(cfb)
}

// cgoReservoir mirrors the minimp3 decode loop's reservoir handling. It
// keeps a C mp3dec_t (whose reserv / reserv_buf the parity test reads) and
// a scratch maindata buffer.
type cgoReservoir struct {
	h        C.mp3dec_t
	maindata [C.MAX_BITRESERVOIR_BYTES + C.MAX_L3_FRAME_PAYLOAD_BYTES]C.uint8_t
}

func newCgoReservoir() *cgoReservoir { return new(cgoReservoir) }

// save mirrors L3_save_reservoir: load maindata + a bs (pos,limit) into the
// scratch and copy the unconsumed tail into h->reserv_buf.
func (r *cgoReservoir) save(maindata []byte, bsPos, bsLimit int) {
	for i := range r.maindata {
		r.maindata[i] = 0
	}
	for i := 0; i < len(maindata) && i < len(r.maindata); i++ {
		r.maindata[i] = C.uint8_t(maindata[i])
	}
	C.mp3parity_save_reservoir(&r.h, &r.maindata[0], C.int(bsPos), C.int(bsLimit))
}

func (r *cgoReservoir) reserv() int { return int(r.h.reserv) }
func (r *cgoReservoir) reservBuf(n int) []byte {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(r.h.reserv_buf[i])
	}
	return out
}

// restore mirrors L3_restore_reservoir: reassemble main_data_begin reservoir
// bytes ahead of payload into the scratch maindata and return the result.
func (r *cgoReservoir) restore(payload []byte, mainDataBegin int) (maindata []byte, limit int, ok bool) {
	var out [C.MAX_BITRESERVOIR_BYTES + C.MAX_L3_FRAME_PAYLOAD_BYTES]C.uint8_t
	var clim C.int
	var pp *C.uint8_t
	if len(payload) > 0 {
		pp = (*C.uint8_t)(unsafe.Pointer(&payload[0]))
	}
	rc := C.mp3parity_restore_reservoir(&r.h, pp, C.int(len(payload)), C.int(mainDataBegin), &out[0], &clim)
	limit = int(clim)
	n := limit / 8
	maindata = make([]byte, n)
	for i := 0; i < n; i++ {
		maindata[i] = byte(out[i])
	}
	return maindata, limit, rc != 0
}
