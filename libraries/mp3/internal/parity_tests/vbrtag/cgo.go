// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package vbrtag holds the vbrtag parity slice: it pins the pure-Go nativemp3
// port of LAME 3.100's VbrTag.c (the Xing/Info/LAME VBR tag — AddVbrFrame /
// InitVbrTag / CRC_update_lookup / UpdateMusicCRC / Xing_seek_table /
// setLameTagFrameHeader / PutLameVBR / lame_get_lametag_frame, plus the
// crc16_lookup[256] table; all in nativemp3/vbrtag.go) against the vendored C
// LAME reference.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference. The tag bytes depend on the
// real encoded -V2 frames, so the oracle drives a genuine end-to-end
// vbr_default (== vbr_mtrh) encode through the full public LAME encoder (every
// vendored libmp3lame source, one per TU via the lame_*.c wrappers — the same
// per-TU split the public cgo backend uses, because LAME reuses Min/Max macros +
// per-TU file statics) and reads the genuine lame_get_lametag_frame output as the
// golden bytes. oracle.c then exports the gfc->VBR_seek_table / nMusicCRC / cfg /
// ov_enc / ov_rpg state so the Go side reconstructs an identical
// LameInternalFlags and produces its own tag frame to compare byte-for-byte. It
// NEVER imports libraries/mp3 (which would duplicate the LAME symbols at link
// time); only the pure-Go internal/nativemp3 port.
//
// VbrTag.c is mostly integer (the bag arithmetic, the CRC, the bit packing); the
// few FP expressions are computed once for the tag, but the real -V2 encode that
// feeds the bag/CRC is FP-bearing, so the byte-identical tag is asserted under
// the mp3_strict build (FMA-free Go) vs the -ffp-contract=off oracle. The scalar
// FP flags come from the mise task env, never the in-source #cgo block. The
// strict gate lives in parity_test.go.
package vbrtag

/*
#cgo CFLAGS: -I${SRCDIR}/../../../liblame
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/libmp3lame
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/mpglib
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/include
#cgo CFLAGS: -DHAVE_CONFIG_H
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable
#cgo CFLAGS: -Wno-shift-negative-value -Wno-absolute-value -Wno-tautological-pointer-compare
#cgo CFLAGS: -Wno-missing-field-initializers -Wno-parentheses

#include <stdlib.h>
#include "oracle.h"
*/
import "C"

import "unsafe"

// cgoHandle wraps a C oracle handle (the captured state of a real -V2 encode).
type cgoHandle struct{ h *C.mp3parity_vbrtag_t }

// cgoRun drives a genuine -V2 encode of nsamples synthetic stereo/mono samples
// per channel at the given samplerate and captures the genuine tag frame + gfc
// state. Returns nil on a LAME setup/encode failure.
func cgoRun(samplerate, channels, nsamplesPerCh int, seed uint32) *cgoHandle {
	h := C.mp3parity_vbrtag_run(C.int(samplerate), C.int(channels),
		C.int(nsamplesPerCh), C.uint(seed))
	if h == nil {
		return nil
	}
	return &cgoHandle{h: h}
}

func (c *cgoHandle) free() { C.mp3parity_vbrtag_free(c.h) }

// goldenFrame returns the genuine lame_get_lametag_frame bytes.
func (c *cgoHandle) goldenFrame() []byte {
	n := int(C.mp3parity_vbrtag_frame_len(c.h))
	if n <= 0 {
		return nil
	}
	ptr := C.mp3parity_vbrtag_frame_ptr(c.h)
	return C.GoBytes(unsafe.Pointer(ptr), C.int(n))
}

func (c *cgoHandle) cfg(which int) int   { return int(C.mp3parity_vbrtag_cfg(c.h, C.int(which))) }
func (c *cgoHandle) ovEnc(which int) int { return int(C.mp3parity_vbrtag_ovenc(c.h, C.int(which))) }
func (c *cgoHandle) radioGain() int      { return int(C.mp3parity_vbrtag_radio_gain(c.h)) }
func (c *cgoHandle) peakSample() float32 { return float32(C.mp3parity_vbrtag_peak_sample(c.h)) }
func (c *cgoHandle) musicCRC() uint16    { return uint16(C.mp3parity_vbrtag_music_crc(c.h)) }
func (c *cgoHandle) seekSum() int        { return int(C.mp3parity_vbrtag_seek_sum(c.h)) }
func (c *cgoHandle) seekSeen() int       { return int(C.mp3parity_vbrtag_seek_seen(c.h)) }
func (c *cgoHandle) seekWant() int       { return int(C.mp3parity_vbrtag_seek_want(c.h)) }
func (c *cgoHandle) seekPos() int        { return int(C.mp3parity_vbrtag_seek_pos(c.h)) }
func (c *cgoHandle) seekSize() int       { return int(C.mp3parity_vbrtag_seek_size(c.h)) }
func (c *cgoHandle) seekBag(i int) int   { return int(C.mp3parity_vbrtag_seek_bag(c.h, C.int(i))) }
func (c *cgoHandle) seekNFrames() uint   { return uint(C.mp3parity_vbrtag_seek_nframes(c.h)) }
func (c *cgoHandle) seekNBytes() uint64  { return uint64(C.mp3parity_vbrtag_seek_nbytes(c.h)) }
func (c *cgoHandle) seekTotalFrameSize() uint {
	return uint(C.mp3parity_vbrtag_seek_totalframesize(c.h))
}
func (c *cgoHandle) gfpVBRq() int       { return int(C.mp3parity_vbrtag_gfp_vbr_q(c.h)) }
func (c *cgoHandle) gfpQuality() int    { return int(C.mp3parity_vbrtag_gfp_quality(c.h)) }
func (c *cgoHandle) gfpNogapTotal() int { return int(C.mp3parity_vbrtag_gfp_nogap_total(c.h)) }
func (c *cgoHandle) gfpNogapCurrent() int {
	return int(C.mp3parity_vbrtag_gfp_nogap_current(c.h))
}

// crcStep folds one byte through the genuine static CRC_update_lookup.
func crcStep(value, crc uint16) uint16 {
	return uint16(C.mp3parity_vbrtag_crc_step(C.uint(value), C.uint(crc)))
}
