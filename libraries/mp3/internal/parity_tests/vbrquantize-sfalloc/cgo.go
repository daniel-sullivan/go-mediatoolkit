// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package vbrquantizesfalloc holds the vbrquantize-sfalloc parity slice: it pins
// the pure-Go nativemp3 port of LAME 3.100's VBR (vbr_mtrh / -V) quantizer
// scalefactor-ALLOCATION tier — block_sf (vbrquantize.c:394), quantize_x34
// (:500), set_subblock_gain (:595), set_scalefacs (:688), checkScalefactor
// (:732), short_block_constrain (:769) and long_block_constrain (:847) — against
// the vendored C LAME reference compiled inline via cgo.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference (oracle.c, which #includes
// the committed libmp3lame/vbrquantize.c) so each go-test binary is
// symbol-self-contained, and it NEVER imports libraries/mp3 (which would
// duplicate the LAME symbols at link time) — only the pure-Go
// internal/nativemp3 port.
//
// Every allocation routine is file-static in vbrquantize.c; oracle.c re-exports
// them through thin mp3parity_vbrsf_* trampolines in the same translation unit
// so the C side of every assertion is the genuine vendored code (see oracle.h).
//
// This slice IS floating-point-bearing: block_sf's find dispatch and
// quantize_x34's float32 product + magic-float quantize round separately, so the
// result is only bit-exact under the mp3_strict build (FMA-free Go) against the
// -ffp-contract=off cgo oracle. The strict gate lives in parity_test.go.
//
// Build tags: gated by `mp3lame` (in addition to `cgo`) because vbrquantize.c is
// LGPL LAME source and the Go port slice it pins is itself mp3lame-gated; the
// canonical strict run is `-tags='mp3lame mp3_strict'` with the FP CGO env (the
// //libraries/mp3:encode-parity mise task).
package vbrquantizesfalloc

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

// cgoFillTables drives the genuine table fill so the oracle's pow20 / ipow20 /
// pow43 / adj43asm globals are populated before any kernel call.
func cgoFillTables() { C.oracle_fill_tables() }

// cgoHandle wraps a C oracle handle (a zeroed lame_internal_flags with a gr_info
// at l3_side.tt[0][0]) so a single test can populate inputs, run a kernel and
// read the resolved side info from the same C-side state, exactly as the Go side
// drives one GrInfo through the parity hooks.
type cgoHandle struct{ h *C.mp3parity_vbrsf_t }

func cgoNewHandle() *cgoHandle { return &cgoHandle{h: C.mp3parity_vbrsf_new()} }
func (c *cgoHandle) free()     { C.mp3parity_vbrsf_free(c.h) }

func cints(xs []int) []C.int {
	out := make([]C.int, len(xs))
	for i, v := range xs {
		out[i] = C.int(v)
	}
	return out
}

func cfloats(xs []float32) []C.float {
	out := make([]C.float, len(xs))
	for i, v := range xs {
		out[i] = C.float(v)
	}
	return out
}

func (c *cgoHandle) setCfg(modeGr, noiseShaping int) {
	C.mp3parity_vbrsf_set_cfg(c.h, C.int(modeGr), C.int(noiseShaping))
}

func (c *cgoHandle) setXr(xr []float32) {
	cx := cfloats(xr)
	C.mp3parity_vbrsf_set_xr(c.h, &cx[0], C.int(len(xr)))
}
func (c *cgoHandle) setWidth(w []int) {
	cw := cints(w)
	C.mp3parity_vbrsf_set_width(c.h, &cw[0], C.int(len(w)))
}
func (c *cgoHandle) setWindow(win []int) {
	cw := cints(win)
	C.mp3parity_vbrsf_set_window(c.h, &cw[0], C.int(len(win)))
}
func (c *cgoHandle) setEac(eac []byte) {
	cb := make([]C.char, len(eac))
	for i, v := range eac {
		cb[i] = C.char(v)
	}
	C.mp3parity_vbrsf_set_eac(c.h, &cb[0], C.int(len(eac)))
}
func (c *cgoHandle) setScalefac(sf []int) {
	cs := cints(sf)
	C.mp3parity_vbrsf_set_scalefac(c.h, &cs[0], C.int(len(sf)))
}
func (c *cgoHandle) setSubblockGain(sbg []int) {
	cs := cints(sbg)
	C.mp3parity_vbrsf_set_subblock_gain(c.h, &cs[0], C.int(len(sbg)))
}
func (c *cgoHandle) setGeom(blockType, globalGain, scalefacScale, preflag, sfbmax, psymax, maxNonzeroCoeff int) {
	C.mp3parity_vbrsf_set_geom(c.h, C.int(blockType), C.int(globalGain), C.int(scalefacScale),
		C.int(preflag), C.int(sfbmax), C.int(psymax), C.int(maxNonzeroCoeff))
}

func (c *cgoHandle) globalGain() int      { return int(C.mp3parity_vbrsf_global_gain(c.h)) }
func (c *cgoHandle) scalefacScale() int   { return int(C.mp3parity_vbrsf_scalefac_scale(c.h)) }
func (c *cgoHandle) preflag() int         { return int(C.mp3parity_vbrsf_preflag(c.h)) }
func (c *cgoHandle) scalefac(sfb int) int { return int(C.mp3parity_vbrsf_scalefac(c.h, C.int(sfb))) }
func (c *cgoHandle) subblockGain(i int) int {
	return int(C.mp3parity_vbrsf_subblock_gain(c.h, C.int(i)))
}
func (c *cgoHandle) l3enc(i int) int { return int(C.mp3parity_vbrsf_l3enc(c.h, C.int(i))) }

// blockSf runs the genuine block_sf over the handle, filling vbrsf / vbrsfmin in
// place and returning (vbrmax, mingainL, mingainS).
func (c *cgoHandle) blockSf(xr34orig, l3Xmin []float32, findSel int, vbrsf, vbrsfmin []int) (int, int, [3]int) {
	cx := cfloats(xr34orig)
	cm := cfloats(l3Xmin)
	cvbrsf := cints(vbrsf)
	cvbrsfmin := cints(vbrsfmin)
	var mingainL C.int
	var mingainS [3]C.int
	vbrmax := int(C.mp3parity_vbrsf_block_sf(c.h, &cx[0], &cm[0], C.int(findSel),
		&cvbrsf[0], &cvbrsfmin[0], &mingainL, (*C.int)(unsafe.Pointer(&mingainS[0]))))
	for i := range vbrsf {
		vbrsf[i] = int(cvbrsf[i])
	}
	for i := range vbrsfmin {
		vbrsfmin[i] = int(cvbrsfmin[i])
	}
	return vbrmax, int(mingainL), [3]int{int(mingainS[0]), int(mingainS[1]), int(mingainS[2])}
}

func (c *cgoHandle) quantizeX34(xr34orig []float32) {
	cx := cfloats(xr34orig)
	C.mp3parity_vbrsf_quantize_x34(c.h, &cx[0])
}

func (c *cgoHandle) setSubblockGainK(mingainS [3]int, sf []int) {
	cm := cints(mingainS[:])
	cs := cints(sf)
	C.mp3parity_vbrsf_run_set_subblock_gain(c.h, &cm[0], &cs[0])
	for i := range sf {
		sf[i] = int(cs[i])
	}
}

func (c *cgoHandle) setScalefacsK(vbrsfmin, sf []int, maxRangeSel int) {
	cm := cints(vbrsfmin)
	cs := cints(sf)
	C.mp3parity_vbrsf_set_scalefacs(c.h, &cm[0], &cs[0], C.int(maxRangeSel))
	for i := range sf {
		sf[i] = int(cs[i])
	}
}

func (c *cgoHandle) checkScalefactor(vbrsfmin []int) bool {
	cm := cints(vbrsfmin)
	return C.mp3parity_vbrsf_check_scalefactor(c.h, &cm[0]) != 0
}

func (c *cgoHandle) shortConstrain(vbrsf, vbrsfmin []int, vbrmax, mingainL int, mingainS [3]int) {
	cvbrsf := cints(vbrsf)
	cvbrsfmin := cints(vbrsfmin)
	cm := cints(mingainS[:])
	C.mp3parity_vbrsf_short_constrain(c.h, &cvbrsf[0], &cvbrsfmin[0], C.int(vbrmax),
		C.int(mingainL), &cm[0])
}

func (c *cgoHandle) longConstrain(vbrsf, vbrsfmin []int, vbrmax, mingainL int, mingainS [3]int) {
	cvbrsf := cints(vbrsf)
	cvbrsfmin := cints(vbrsfmin)
	cm := cints(mingainS[:])
	C.mp3parity_vbrsf_long_constrain(c.h, &cvbrsf[0], &cvbrsfmin[0], C.int(vbrmax),
		C.int(mingainL), &cm[0])
}
