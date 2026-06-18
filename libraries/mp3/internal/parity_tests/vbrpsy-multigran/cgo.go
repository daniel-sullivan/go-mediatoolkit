// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package vbrpsymg holds the vbrpsy-multigran parity slice: it isolates the
// multi-granule psychoacoustic-analysis driver (psymodel.c L3psycho_anal_vbr) and
// pins the pure-Go nativemp3.L3psychoAnalVbr against the genuine vendored static
// function, granule-by-granule, over a SHARED first-frame mfbuf. It was built to
// localize the -V2 byte-identical divergence the vbr-encode-e2e slice flagged
// (granule 1 read near-silence in the Go vbrpsy while the C saw real audio).
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference, one vendored source per TU
// (the lame_*.c / mpglib_*.c wrappers — the same per-TU split the public cgo
// backend and the vbr-encode-e2e / vbrtag slices use). oracle.c is its own TU AND
// the psymodel TU (it #includes libmp3lame/psymodel.c directly so it can call the
// static L3psycho_anal_vbr, so unlike the other slices there is no separate
// lame_psymodel.c wrapper here). It NEVER imports libraries/mp3 (which would
// duplicate the LAME symbols at link time); only the pure-Go internal/nativemp3
// port is imported on the Go side (native.go).
//
// The energy/FHT/masking math is FP-bearing, so the bit-exact match holds only
// under the mp3_strict build (FMA-free Go) vs the -ffp-contract=off oracle. The
// scalar FP flags come from the mise task env (CGO_CFLAGS), never the in-source
// #cgo block. The strict gate lives in parity_test.go.
package vbrpsymg

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

// cgoHandle wraps a C oracle handle (the shared mfbuf + per-granule genuine
// L3psycho_anal_vbr outputs).
type cgoHandle struct{ h *C.mp3parity_mg_t }

// cgoRun builds the first-frame mfbuf and drives the genuine per-granule
// L3psycho_anal_vbr. Returns nil on a LAME setup failure.
func cgoRun(samplerate, channels, nsamplesPerCh int, seed uint32) *cgoHandle {
	h := C.mp3parity_mg_run(C.int(samplerate), C.int(channels),
		C.int(nsamplesPerCh), C.uint(seed))
	if h == nil {
		return nil
	}
	return &cgoHandle{h: h}
}

func (c *cgoHandle) free() { C.mp3parity_mg_free(c.h) }

func (c *cgoHandle) mfSize() int      { return int(C.mp3parity_mg_mf_size(c.h)) }
func (c *cgoHandle) channelsOut() int { return int(C.mp3parity_mg_channels_out(c.h)) }
func (c *cgoHandle) nChnPsy() int     { return int(C.mp3parity_mg_n_chn_psy(c.h)) }

// mfbuf returns the shared mfbuf for channel ch (the Go side reuses these exact
// floats).
func (c *cgoHandle) mfbuf(ch int) []float32 {
	n := int(C.mp3parity_mg_mfbuf_len(c.h))
	if n <= 0 {
		return nil
	}
	ptr := C.mp3parity_mg_mfbuf_ptr(c.h, C.int(ch))
	out := make([]float32, n)
	copy(out, unsafe.Slice((*float32)(unsafe.Pointer(ptr)), n))
	return out
}

func cFloats(ptr *C.float, n int) []float32 {
	out := make([]float32, n)
	copy(out, unsafe.Slice((*float32)(unsafe.Pointer(ptr)), n))
	return out
}

func (c *cgoHandle) energy(gr int) []float32 {
	return cFloats(C.mp3parity_mg_energy(c.h, C.int(gr)), 4)
}
func (c *cgoHandle) pe(gr int) []float32   { return cFloats(C.mp3parity_mg_pe(c.h, C.int(gr)), 2) }
func (c *cgoHandle) peMS(gr int) []float32 { return cFloats(C.mp3parity_mg_pe_ms(c.h, C.int(gr)), 2) }

func (c *cgoHandle) enL(which, gr, ch int) []float32 {
	return cFloats(C.mp3parity_mg_en_l(c.h, C.int(which), C.int(gr), C.int(ch)), 22)
}
func (c *cgoHandle) thmL(which, gr, ch int) []float32 {
	return cFloats(C.mp3parity_mg_thm_l(c.h, C.int(which), C.int(gr), C.int(ch)), 22)
}
func (c *cgoHandle) enS(which, gr, ch int) []float32 {
	return cFloats(C.mp3parity_mg_en_s(c.h, C.int(which), C.int(gr), C.int(ch)), 13*3)
}
func (c *cgoHandle) thmS(which, gr, ch int) []float32 {
	return cFloats(C.mp3parity_mg_thm_s(c.h, C.int(which), C.int(gr), C.int(ch)), 13*3)
}

func (c *cgoHandle) gdlS3() []float32 {
	n := int(C.mp3parity_mg_gdl_s3_len(c.h))
	if n <= 0 {
		return nil
	}
	return cFloats(C.mp3parity_mg_gdl_s3(c.h), n)
}
func (c *cgoHandle) gdlMaskingLower() []float32 {
	return cFloats(C.mp3parity_mg_gdl_masking_lower(c.h), 64)
}
func (c *cgoHandle) gdlMinval() []float32    { return cFloats(C.mp3parity_mg_gdl_minval(c.h), 64) }
func (c *cgoHandle) gdlRnumlines() []float32 { return cFloats(C.mp3parity_mg_gdl_rnumlines(c.h), 64) }
func (c *cgoHandle) gdlBoWeight() []float32  { return cFloats(C.mp3parity_mg_gdl_bo_weight(c.h), 22) }
func (c *cgoHandle) gdlMld() []float32       { return cFloats(C.mp3parity_mg_gdl_mld(c.h), 22) }

func (c *cgoHandle) gdsS3() []float32 {
	n := int(C.mp3parity_mg_gds_s3_len(c.h))
	if n <= 0 {
		return nil
	}
	return cFloats(C.mp3parity_mg_gds_s3(c.h), n)
}
func (c *cgoHandle) gdsMaskingLower() []float32 {
	return cFloats(C.mp3parity_mg_gds_masking_lower(c.h), 64)
}
func (c *cgoHandle) gdsMinval() []float32    { return cFloats(C.mp3parity_mg_gds_minval(c.h), 64) }
func (c *cgoHandle) gdsRnumlines() []float32 { return cFloats(C.mp3parity_mg_gds_rnumlines(c.h), 64) }
func (c *cgoHandle) gdsMld() []float32       { return cFloats(C.mp3parity_mg_gds_mld(c.h), 13) }
func (c *cgoHandle) gdsMldCb() []float32     { return cFloats(C.mp3parity_mg_gds_mld_cb(c.h), 64) }
func (c *cgoHandle) athCbS() []float32       { return cFloats(C.mp3parity_mg_ath_cb_s(c.h), 64) }
func (c *cgoHandle) athCbL() []float32       { return cFloats(C.mp3parity_mg_ath_cb_l(c.h), 64) }
