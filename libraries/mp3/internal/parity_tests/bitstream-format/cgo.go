//go:build cgo

// Package bitstreamformat holds the bitstream-format parity slice: it pins
// the pure-Go nativemp3 port of minimp3's bit reader, MPEG audio frame
// header accessors, frame-sync scan, bit-reservoir reassembly, and Layer III
// side-info parser against the vendored C minimp3 reference compiled inline
// via cgo.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference (oracle.c, which includes
// the committed libraries/mp3/libminimp3/minimp3.h with
// MINIMP3_IMPLEMENTATION) so each go-test binary is symbol-self-contained,
// and it NEVER imports libraries/mp3 (only the internal nativemp3 port).
//
// minimp3's bitstream-format routines are all file-static; oracle.c re-exports
// them through thin oracle_* wrappers in the same translation unit so the C
// side of every assertion is the genuine vendored code (see oracle.h).
//
// This slice is integer-only and so is bit-identical regardless of build tag
// or vectorization; the strict-gated assertions (parity_test.go) merely make
// the bit-exact contract explicit and ride the same mp3_strict gate as the
// FP slices.
package bitstreamformat

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

// ── bit reader ───────────────────────────────────────────────────────────

// cgoBitStream wraps an oracle bs_t over a C-owned copy of the backing bytes.
// bs_t stashes a raw pointer to those bytes, and cgo forbids storing a Go
// pointer in C-visible memory across calls, so the buffer must be malloc'd C
// storage (freed via free); the Go slice is copied in once at construction.
type cgoBitStream struct {
	bs  C.oracle_bs_t
	buf *C.uint8_t
}

func newCgoBitStream(data []byte) *cgoBitStream {
	c := &cgoBitStream{}
	var p *C.uint8_t
	if len(data) > 0 {
		p = (*C.uint8_t)(unsafe.Pointer(&data[0]))
	}
	c.buf = C.oracle_buf_new(p, C.int(len(data)))
	C.oracle_bs_init(&c.bs, c.buf, C.int(len(data)))
	return c
}

func (c *cgoBitStream) free() { C.oracle_buf_free(c.buf) }

func (c *cgoBitStream) getBits(n int) uint32 {
	return uint32(C.oracle_get_bits(&c.bs, C.int(n)))
}

func (c *cgoBitStream) pos() int   { return int(c.bs.pos) }
func (c *cgoBitStream) limit() int { return int(c.bs.limit) }

// ── header accessors ─────────────────────────────────────────────────────

func cHdrPtr(h []byte) *C.uint8_t { return (*C.uint8_t)(unsafe.Pointer(&h[0])) }

func cgoHdrIsMono(h []byte) bool       { return C.oracle_hdr_is_mono(cHdrPtr(h)) != 0 }
func cgoHdrIsFreeFormat(h []byte) bool { return C.oracle_hdr_is_free_format(cHdrPtr(h)) != 0 }
func cgoHdrIsCRC(h []byte) bool        { return C.oracle_hdr_is_crc(cHdrPtr(h)) != 0 }
func cgoHdrTestPadding(h []byte) int   { return int(C.oracle_hdr_test_padding(cHdrPtr(h))) }
func cgoHdrTestMPEG1(h []byte) int     { return int(C.oracle_hdr_test_mpeg1(cHdrPtr(h))) }
func cgoHdrTestNotMPEG25(h []byte) int { return int(C.oracle_hdr_test_not_mpeg25(cHdrPtr(h))) }
func cgoHdrGetLayer(h []byte) int      { return int(C.oracle_hdr_get_layer(cHdrPtr(h))) }
func cgoHdrGetBitrate(h []byte) int    { return int(C.oracle_hdr_get_bitrate(cHdrPtr(h))) }
func cgoHdrGetSampleRate(h []byte) int { return int(C.oracle_hdr_get_sample_rate(cHdrPtr(h))) }
func cgoHdrGetMySampleRate(h []byte) int {
	return int(C.oracle_hdr_get_my_sample_rate(cHdrPtr(h)))
}
func cgoHdrIsFrame576(h []byte) bool { return C.oracle_hdr_is_frame_576(cHdrPtr(h)) != 0 }
func cgoHdrIsLayer1(h []byte) bool   { return C.oracle_hdr_is_layer_1(cHdrPtr(h)) != 0 }
func cgoHdrValid(h []byte) bool      { return C.oracle_hdr_valid(cHdrPtr(h)) != 0 }
func cgoHdrCompare(h1, h2 []byte) bool {
	return C.oracle_hdr_compare(cHdrPtr(h1), cHdrPtr(h2)) != 0
}
func cgoHdrBitrateKbps(h []byte) uint  { return uint(C.oracle_hdr_bitrate_kbps(cHdrPtr(h))) }
func cgoHdrSampleRateHz(h []byte) uint { return uint(C.oracle_hdr_sample_rate_hz(cHdrPtr(h))) }
func cgoHdrFrameSamples(h []byte) uint { return uint(C.oracle_hdr_frame_samples(cHdrPtr(h))) }
func cgoHdrPadding(h []byte) int       { return int(C.oracle_hdr_padding(cHdrPtr(h))) }
func cgoHdrFrameBytes(h []byte, ff int) int {
	return int(C.oracle_hdr_frame_bytes(cHdrPtr(h), C.int(ff)))
}

// ── frame sync ───────────────────────────────────────────────────────────

func cgoMatchFrame(hdr []byte, mp3Bytes, frameBytes int) bool {
	return C.oracle_mp3d_match_frame(cHdrPtr(hdr), C.int(mp3Bytes), C.int(frameBytes)) != 0
}

func cgoFindFrame(mp3 []byte, mp3Bytes int, freeFormatBytes *int) (off, frameBytes int) {
	cff := C.int(*freeFormatBytes)
	var cfb C.int
	// mp3d_find_frame's loop runs `i < mp3_bytes - HDR_SIZE`, so an empty or
	// sub-header buffer touches no bytes; a nil pointer is safe and matches
	// what an empty Go slice would yield natively.
	var p *C.uint8_t
	if len(mp3) > 0 {
		p = (*C.uint8_t)(unsafe.Pointer(&mp3[0]))
	}
	r := C.oracle_mp3d_find_frame(p, C.int(mp3Bytes), &cff, &cfb)
	*freeFormatBytes = int(cff)
	return int(r), int(cfb)
}

// ── reservoir ────────────────────────────────────────────────────────────

// cgoReservoir owns a C-side mp3dec_t + scratch so the reservoir memmoves land
// in real minimp3-sized storage.
type cgoReservoir struct {
	dec *C.oracle_mp3dec_t
	s   *C.oracle_scratch_t
}

func newCgoReservoir() *cgoReservoir {
	return &cgoReservoir{dec: C.oracle_dec_new(), s: C.oracle_scratch_new()}
}

func (c *cgoReservoir) free() {
	C.oracle_dec_free(c.dec)
	C.oracle_scratch_free(c.s)
}

func (c *cgoReservoir) setMaindata(data []byte) {
	if len(data) == 0 {
		return
	}
	C.oracle_scratch_set_maindata(c.s, (*C.uint8_t)(unsafe.Pointer(&data[0])), C.int(len(data)))
}

func (c *cgoReservoir) setBs(pos, limit int) {
	C.oracle_scratch_set_bs(c.s, C.int(pos), C.int(limit))
}

func (c *cgoReservoir) maindata(n int) []byte {
	out := make([]byte, n)
	if n == 0 {
		return out
	}
	C.oracle_scratch_get_maindata(c.s, (*C.uint8_t)(unsafe.Pointer(&out[0])), C.int(n))
	return out
}

func (c *cgoReservoir) bsPos() int   { return int(C.oracle_scratch_bs_pos(c.s)) }
func (c *cgoReservoir) bsLimit() int { return int(C.oracle_scratch_bs_limit(c.s)) }

// reserv reads back the decoder's reservoir byte count and buffer tail.
func (c *cgoReservoir) reserv() int { return int(c.dec.reserv) }

func (c *cgoReservoir) reservBuf(n int) []byte {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(c.dec.reserv_buf[i])
	}
	return out
}

func (c *cgoReservoir) saveReservoir() {
	C.oracle_L3_save_reservoir(c.dec, c.s)
}

// restoreReservoir runs L3_restore_reservoir against a payload bit reader.
func (c *cgoReservoir) restoreReservoir(payload []byte, mainDataBegin int) bool {
	bs := newCgoBitStream(payload)
	defer bs.free()
	return C.oracle_L3_restore_reservoir(c.dec, &bs.bs, c.s, C.int(mainDataBegin)) != 0
}

// L3_read_side_info is intentionally not wrapped — the committed minimp3 and
// the Go port disagree on the success return value (raw main_data_begin vs.
// bs.Pos/8); see oracle.h. The side-info parity slice is deferred.
