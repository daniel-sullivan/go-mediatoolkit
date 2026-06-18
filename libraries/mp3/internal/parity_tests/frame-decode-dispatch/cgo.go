//go:build cgo

// Package framedecodedispatch contains parity tests that pin the Go
// "frame-decode-dispatch" slice of the minimp3 port
// (libraries/mp3/internal/nativemp3, framedecode.go) against the vendored
// minimp3 C reference.
//
// # What this slice is, and what is exercised here
//
// The slice under test is minimp3's top-level driver: mp3dec_init
// (nativemp3.Mp3decInit) and mp3dec_decode_frame (nativemp3.DecodeFrame).
// mp3dec_decode_frame has two modes. In PROBE mode (pcm == nil) it runs the
// whole frame-detect → header-parse → mp3dec_frame_info_t-fill control flow
// and returns the per-channel sample count WITHOUT decoding audio; this path
// is integer-only and self-contained. In FULL mode (pcm != nil) it goes on to
// dispatch each granule through the L3_decode / Layer I-II seams and the
// synthesis filterbank.
//
// This oracle drives mp3dec_init and the PROBE path. The FULL-audio path is
// deliberately NOT driven, because in the current state of the port the two
// cross-slice seams the dispatch calls — nativemp3's package-level l3Decode /
// l12DecodeFrame function variables — are unassigned (the L3-granule-decode
// and Layer I/II slices that would populate them in their package init have
// not landed). Invoking nativemp3.DecodeFrame with a non-nil pcm on a real
// frame would call a nil seam and panic by construction, which framedecode.go
// documents as a build-ordering condition rather than a runtime input. The
// PROBE path is the entire dispatch control flow that is bit-exactly runnable
// today: frame sync, the fast-resync cached-header compare, the not-found
// early return, header field projection into FrameInfo, and the free-format
// size round-trip. When the downstream decode slices land and wire the seams,
// extend this oracle with a FULL-mode end-to-end comparison.
//
// # Discipline
//
// mp3dec_decode_frame is a public minimp3 entry point, but compiling it drags
// in the whole static minimp3 implementation, so the C side compiles its OWN
// copy (frame_decode_dispatch_cgo_src.c defines MINIMP3_IMPLEMENTATION) and
// surfaces each entry point via an mp3parity_* trampoline — the same one-copy-
// per-package discipline the other minimp3 oracle slices use. This package
// never imports libraries/mp3 (which would compile minimp3 a second time and
// collide on its static symbols); it may import nativemp3.
//
// The C oracle is the vendored minimp3 under libraries/mp3/libminimp3,
// configured for a scalar baseline. The scalar FP flags (-ffp-contract=off,
// -fno-vectorize, …) come from the mise task env (CGO_CFLAGS +
// CGO_CFLAGS_ALLOW), never from the in-source #cgo block below, because Go's
// cgo flag allowlist rejects them. They do not affect the integer-only PROBE
// path this oracle drives.
package framedecodedispatch

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libminimp3
#cgo LDFLAGS: -lm
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// Include minimp3.h WITHOUT the implementation macro so the public mp3dec_t
// (declared above the MINIMP3_IMPLEMENTATION guard) is visible and the Go side
// can allocate it; frame_decode_dispatch_cgo_src.c includes it WITH the macro
// and defines the trampolines, ABI-compatibly.
#include "minimp3.h"

extern void mp3parity_init(mp3dec_t *dec);
extern int  mp3parity_header0(const mp3dec_t *dec);
extern int  mp3parity_decode_probe(mp3dec_t *dec, const uint8_t *mp3, int mp3_bytes,
                                   int *io_free_format_bytes, int *out_header0,
                                   int *out_frame_bytes, int *out_frame_offset,
                                   int *out_channels, int *out_hz, int *out_layer,
                                   int *out_bitrate_kbps);
extern int  mp3parity_decode_twice(const uint8_t *mp3, int mp3_bytes, int advance,
                                   int *out_header0, int *out_frame_bytes,
                                   int *out_frame_offset, int *out_channels, int *out_hz,
                                   int *out_layer, int *out_bitrate_kbps);
*/
import "C"

import "unsafe"

// cgoFrameInfo mirrors the mp3dec_frame_info_t fields the dispatch fills,
// plus the cached header[0] byte and the round-tripped free-format size, so
// the parity test can compare every observable the C driver produces.
type cgoFrameInfo struct {
	samples         int
	header0         int
	freeFormatBytes int
	frameBytes      int
	frameOffset     int
	channels        int
	hz              int
	layer           int
	bitrateKbps     int
}

// cgoInit drives mp3dec_init on a freshly zeroed C mp3dec_t and reports the
// cached header[0] byte it leaves behind (the only state mp3dec_init touches).
func cgoInit(header0Seed byte) int {
	var dec C.mp3dec_t
	// Seed header[0] so the reset is observable (mp3dec_init clears it to 0).
	dec.header[0] = C.uint8_t(header0Seed)
	C.mp3parity_init(&dec)
	return int(C.mp3parity_header0(&dec))
}

// cgoDecodeProbe runs mp3dec_decode_frame in PROBE mode (pcm == NULL) over a
// fresh decoder seeded with the given free-format size, and returns the filled
// frame info plus the decoder's resulting cached header[0] and free-format
// size. mp3 is copied into C-owned memory so the cgo pointer checker sees a C
// pointer for the duration of the call.
func cgoDecodeProbe(mp3 []byte, mp3Bytes, freeFormatSeed int) cgoFrameInfo {
	var dec C.mp3dec_t
	var cbuf unsafe.Pointer
	var p *C.uint8_t
	if len(mp3) > 0 {
		cbuf = C.CBytes(mp3)
		defer C.free(cbuf)
		p = (*C.uint8_t)(cbuf)
	}
	cff := C.int(freeFormatSeed)
	var h0, fb, foff, ch, hz, layer, br C.int
	samples := C.mp3parity_decode_probe(&dec, p, C.int(mp3Bytes), &cff,
		&h0, &fb, &foff, &ch, &hz, &layer, &br)
	return cgoFrameInfo{
		samples:         int(samples),
		header0:         int(h0),
		freeFormatBytes: int(cff),
		frameBytes:      int(fb),
		frameOffset:     int(foff),
		channels:        int(ch),
		hz:              int(hz),
		layer:           int(layer),
		bitrateKbps:     int(br),
	}
}

// cgoDecodeTwice probes the stream once cold, then probes the tail starting at
// `advance` bytes with the SAME C mp3dec_t, returning the second call's
// observables — so the cached-header fast-resync branch is hit on call two.
func cgoDecodeTwice(stream []byte, advance int) cgoFrameInfo {
	var cbuf unsafe.Pointer
	var p *C.uint8_t
	if len(stream) > 0 {
		cbuf = C.CBytes(stream)
		defer C.free(cbuf)
		p = (*C.uint8_t)(cbuf)
	}
	var h0, fb, foff, ch, hz, layer, br C.int
	samples := C.mp3parity_decode_twice(p, C.int(len(stream)), C.int(advance),
		&h0, &fb, &foff, &ch, &hz, &layer, &br)
	return cgoFrameInfo{
		samples:     int(samples),
		header0:     int(h0),
		frameBytes:  int(fb),
		frameOffset: int(foff),
		channels:    int(ch),
		hz:          int(hz),
		layer:       int(layer),
		bitrateKbps: int(br),
	}
}
