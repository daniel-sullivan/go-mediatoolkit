// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package vbrquantizeleaf holds the vbrquantize-leaf parity slice: it pins the
// pure-Go nativemp3 port of LAME 3.100's VBR (vbr_mtrh / -V) quantizer leaf
// kernels — vec_max_c (vbrquantize.c:116), find_lowest_scalefac (:148), k_34_4
// (:169, the TAKEHIRO_IEEE754_HACK magic-float quantize), calc_sfb_noise_x34
// (:218), tri_calc_sfb_noise_x34 (:278), calc_scalefac (:317),
// guess_scalefac_x34 (:324) and find_scalefac_x34 (:347) — against the vendored
// C LAME reference compiled inline via cgo.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference (oracle.c, which #includes
// the committed libmp3lame/vbrquantize.c) so each go-test binary is
// symbol-self-contained, and it NEVER imports libraries/mp3 (which would
// duplicate the LAME symbols at link time) — only the pure-Go
// internal/nativemp3 port.
//
// Every leaf kernel is file-static in vbrquantize.c; oracle.c re-exports them
// through thin oracle_* trampolines in the same translation unit so the C side
// of every assertion is the genuine vendored code (see oracle.h).
//
// This slice IS floating-point-bearing: every band-noise multiply/add and the
// magic-float add of the IEEE754 hack is a separately rounded term, so the
// result is only bit-exact under the mp3_strict build (FMA-free Go) against the
// -ffp-contract=off cgo oracle. The strict gate lives in parity_test.go.
//
// Build tags: gated by `mp3lame` (in addition to `cgo`) because vbrquantize.c is
// LGPL LAME source and the Go port slice it pins is itself mp3lame-gated; the
// canonical strict run is `-tags='mp3lame mp3_strict'` with the FP CGO env (the
// //libraries/mp3:encode-parity mise task).
package vbrquantizeleaf

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

#include "oracle.h"
*/
import "C"

// cfloats copies a float32 slice to a fresh []C.float (C-owned) so passing
// &out[0] to a C trampoline satisfies cgo's pointer rules.
func cfloats(xs []float32) []C.float {
	out := make([]C.float, len(xs))
	for i, v := range xs {
		out[i] = C.float(v)
	}
	return out
}

// cgoFillTables drives the genuine table fill so the oracle's pow20 / ipow20 /
// pow43 / adj43asm globals are populated before any kernel call.
func cgoFillTables() { C.oracle_fill_tables() }

func cgoVecMaxC(xr34 []float32, bw int) float32 {
	c := cfloats(xr34)
	return float32(C.oracle_vec_max_c(&c[0], C.uint(bw)))
}

func cgoFindLowestScalefac(xr34 float32) uint8 {
	return uint8(C.oracle_find_lowest_scalefac(C.float(xr34)))
}

func cgoK344(x [4]float64) [4]int {
	var cx [4]C.double
	for i := range x {
		cx[i] = C.double(x[i])
	}
	var cl3 [4]C.int
	C.oracle_k_34_4(&cx[0], &cl3[0])
	var l3 [4]int
	for i := range l3 {
		l3[i] = int(cl3[i])
	}
	return l3
}

func cgoCalcSfbNoiseX34(xr, xr34 []float32, bw int, sf uint8) float32 {
	cxr := cfloats(xr)
	cxr34 := cfloats(xr34)
	return float32(C.oracle_calc_sfb_noise_x34(&cxr[0], &cxr34[0], C.uint(bw), C.uchar(sf)))
}

func cgoTriCalcSfbNoiseX34(xr, xr34 []float32, l3Xmin float32, bw int, sf uint8) uint8 {
	cxr := cfloats(xr)
	cxr34 := cfloats(xr34)
	return uint8(C.oracle_tri_calc_sfb_noise_x34(&cxr[0], &cxr34[0], C.float(l3Xmin), C.uint(bw), C.uchar(sf)))
}

func cgoCalcScalefac(l3Xmin float32, bw int) int {
	return int(C.oracle_calc_scalefac(C.float(l3Xmin), C.int(bw)))
}

func cgoGuessScalefacX34(xr, xr34 []float32, l3Xmin float32, bw int, sfMin uint8) uint8 {
	cxr := cfloats(xr)
	cxr34 := cfloats(xr34)
	return uint8(C.oracle_guess_scalefac_x34(&cxr[0], &cxr34[0], C.float(l3Xmin), C.uint(bw), C.uchar(sfMin)))
}

func cgoFindScalefacX34(xr, xr34 []float32, l3Xmin float32, bw int, sfMin uint8) uint8 {
	cxr := cfloats(xr)
	cxr34 := cfloats(xr34)
	return uint8(C.oracle_find_scalefac_x34(&cxr[0], &cxr34[0], C.float(l3Xmin), C.uint(bw), C.uchar(sfMin)))
}
