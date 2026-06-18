// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package frameencodedispatch holds the frame-encode-dispatch parity slice: it
// pins the pure-Go nativemp3 port of LAME 3.100's per-frame encode driver
// (EncodeMP3Frame / lameEncodeFrameInit, frame_encode.go — a 1:1 translation of
// lame_encode_mp3_frame / lame_encode_frame_init, encoder.c) against the
// vendored LAME C reference compiled inline via cgo.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference (oracle.c, which #includes
// the committed libmp3lame/encoder.c) so each go-test binary is symbol-self-
// contained, and it NEVER imports libraries/mp3 (which would duplicate the LAME
// symbols at link time) — only the pure-Go internal/nativemp3 port.
//
// SCOPE — the dispatcher's OWN arithmetic only. lame_encode_mp3_frame sits at
// the top of the encode call tree and dispatches five heavy stages (psy model,
// MDCT, M/S decision, quantize loop, bitstream). Those callees are separate
// not-yet-pinned slices; oracle.c stubs them inert (the same way the Go test
// supplies a stub FrameEncodeStages), so the genuine vendored dispatcher runs
// its own padding accumulator, JOINT_STEREO M/S energy ratio, M/S-vs-L/R PE
// sums, mode_ext decision and CBR/ABR PE-smoothing FIR in isolation — exactly
// the control flow this slice translates. The fabricated psy-model outputs the
// dispatcher reads back are fed through the stub L3psycho_anal_vbr.
//
// FP: the M/S energy ratio, the PE sums and the smoothing FIR are
// single-precision (C FLOAT == float32). The oracle is compiled with
// -ffp-contract=off (mise env), so every a + b*c rounds separately; the
// nativemp3 mp3_strict build routes the same arithmetic through //go:noinline
// feMul/feAdd/feFma helpers so it rounds separately too. The bit-exact
// assertions in parity_test.go are gated behind nativemp3.StrictMode per the
// FP-parity convention, so a bare `go test` is clean and the strict run
// (mp3_strict + mp3lame + the FP CGO env) is the authoritative gate.
//
// LGPL: encoder.c is LGPL LAME source, so this package is gated by mp3lame (in
// addition to cgo) like the dispatcher slice it pins; a bare `go test` never
// compiles it.
package frameencodedispatch

/*
#cgo CFLAGS: -DHAVE_CONFIG_H
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../liblame
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/libmp3lame
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable
#cgo CFLAGS: -Wno-shift-negative-value -Wno-absolute-value -Wno-tautological-pointer-compare

#include "oracle.h"
*/
import "C"

import "unsafe"

// cgoFE drives the vendored LAME dispatcher over a C-owned lame_internal_flags.
// All state the dispatcher reads or writes is configured and read back through
// the oracle trampolines so the nativemp3 FELameInternalFlags can be compared
// field-for-field.
type cgoFE struct {
	h *C.mp3parity_fe_t
}

func newCgoFE() *cgoFE { return &cgoFE{h: C.mp3parity_fe_new()} }

func (c *cgoFE) free() { C.mp3parity_fe_free(c.h) }

func (c *cgoFE) setCfg(samplerateOut, channelsOut, modeGr, mode, forceMS, vbr, writeLameTag int) {
	C.mp3parity_fe_set_cfg(c.h, C.int(samplerateOut), C.int(channelsOut), C.int(modeGr),
		C.int(mode), C.int(forceMS), C.int(vbr), C.int(writeLameTag))
}

func (c *cgoFE) setPad(fracSpF, slotLag int) {
	C.mp3parity_fe_set_pad(c.h, C.int(fracSpF), C.int(slotLag))
}

func (c *cgoFE) setPefirbuf(buf [19]float32) {
	C.mp3parity_fe_set_pefirbuf(c.h, (*C.float)(unsafe.Pointer(&buf[0])))
}

func (c *cgoFE) setFrameInit(v int) { C.mp3parity_fe_set_frame_init(c.h, C.int(v)) }

func (c *cgoFE) setPsy(gr int, pe, peMS [2]float32, totEner [4]float32, blocktype [2]int) {
	bt := [2]C.int{C.int(blocktype[0]), C.int(blocktype[1])}
	C.mp3parity_fe_set_psy(C.int(gr),
		(*C.float)(unsafe.Pointer(&pe[0])),
		(*C.float)(unsafe.Pointer(&peMS[0])),
		(*C.float)(unsafe.Pointer(&totEner[0])),
		&bt[0])
}

func (c *cgoFE) setPsyRet(ret int) { C.mp3parity_fe_set_psy_ret(C.int(ret)) }
func (c *cgoFE) armCapture()       { C.mp3parity_fe_arm_capture() }

func (c *cgoFE) resetCapture()        { C.mp3parity_fe_reset_capture() }
func (c *cgoFE) allocInput(inlen int) { C.mp3parity_fe_alloc_input(c.h, C.int(inlen)) }

func (c *cgoFE) setInput(ch int, vals []float32) {
	if len(vals) == 0 {
		return
	}
	C.mp3parity_fe_set_input(c.h, C.int(ch), (*C.float)(unsafe.Pointer(&vals[0])), C.int(len(vals)))
}

func (c *cgoFE) encode() int { return int(C.mp3parity_fe_encode(c.h)) }

func (c *cgoFE) padding() int     { return int(C.mp3parity_fe_padding(c.h)) }
func (c *cgoFE) slotLag() int     { return int(C.mp3parity_fe_slot_lag(c.h)) }
func (c *cgoFE) modeExt() int     { return int(C.mp3parity_fe_mode_ext(c.h)) }
func (c *cgoFE) frameNumber() int { return int(C.mp3parity_fe_frame_number(c.h)) }
func (c *cgoFE) frameInit() int   { return int(C.mp3parity_fe_frame_init(c.h)) }

func (c *cgoFE) pefirbuf(i int) float32 { return float32(C.mp3parity_fe_pefirbuf(c.h, C.int(i))) }

func (c *cgoFE) blockType(gr, ch int) int {
	return int(C.mp3parity_fe_block_type(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoFE) mixedBlockFlag(gr, ch int) int {
	return int(C.mp3parity_fe_mixed_block_flag(c.h, C.int(gr), C.int(ch)))
}

func (c *cgoFE) mdctCalls() int       { return int(C.mp3parity_fe_mdct_calls()) }
func (c *cgoFE) prime0(i int) float32 { return float32(C.mp3parity_fe_prime0(C.int(i))) }
func (c *cgoFE) prime1(i int) float32 { return float32(C.mp3parity_fe_prime1(C.int(i))) }
