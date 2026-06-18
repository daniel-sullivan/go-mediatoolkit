// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package vbrquantizeframe holds the vbrquantize-frame parity slice: it pins the
// pure-Go nativemp3 port of LAME 3.100's VBR (vbr_mtrh / -V) quantizer bit-search
// orchestration tier — tryGlobalStepsize (vbrquantize.c:1011), searchGlobal-
// StepsizeMax (:1040), sfDepth (:1074), cutDistribution (:1093), flatten-
// Distribution (:1104), tryThatOne (:1140), outOfBitsStrategy (:1154),
// reduce_bit_usage (:1231) and the entry point VBR_encode_frame (:1254) —
// against the vendored C LAME reference compiled inline via cgo.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference (oracle.c, which #includes the
// committed libmp3lame/vbrquantize.c + takehiro.c + tables.c) so each go-test
// binary is symbol-self-contained, and it NEVER imports libraries/mp3 (which
// would duplicate the LAME symbols at link time) — only the pure-Go
// internal/nativemp3 port.
//
// VBR_encode_frame drives the full pipeline (block_sf, the allocator,
// quantize_x34 + noquant_count_bits, scale_bitcount, best_scalefac_store +
// best_huffman_divide), so the oracle compiles the genuine bit-counting TU
// (takehiro.c + tables.c) into the same unit rather than stubbing it; the
// per-granule scalefactors / quantized spectrum / bit usage are pinned EXACT.
//
// This slice IS floating-point-bearing (the 'as is' quantize + the out-of-budget
// redistribution round float32 products separately), so the result is only
// bit-exact under the mp3_strict build (FMA-free Go) against the
// -ffp-contract=off cgo oracle. The strict gate lives in parity_test.go.
package vbrquantizeframe

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

// cgoFillTables drives the genuine table fill so the oracle's pow20 / ipow20 /
// pow43 / adj43asm globals are populated before VBR_encode_frame runs.
func cgoFillTables() { C.oracle_fill_tables() }

// cgoHandle wraps a C oracle handle (a zeroed lame_internal_flags with the 2x2
// l3_side.tt gr_info grid) so one test can populate cfg + per-granule geometry +
// inputs, drive VBR_encode_frame and read the resolved side info from the same
// C-side state, exactly as the Go side drives one LameInternalFlags.
type cgoHandle struct{ h *C.mp3parity_vbrfr_t }

func cgoNewHandle() *cgoHandle { return &cgoHandle{h: C.mp3parity_vbrfr_new()} }
func (c *cgoHandle) free()     { C.mp3parity_vbrfr_free(c.h) }

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

func (c *cgoHandle) setCfg(modeGr, channelsOut, noiseShaping, fullOuterLoop, useBestHuffman int) {
	C.mp3parity_vbrfr_set_cfg(c.h, C.int(modeGr), C.int(channelsOut), C.int(noiseShaping),
		C.int(fullOuterLoop), C.int(useBestHuffman))
}

func (c *cgoHandle) setSfbLong(l []int) {
	cl := cints(l)
	C.mp3parity_vbrfr_set_sfb_long(c.h, &cl[0], C.int(len(l)))
}
func (c *cgoHandle) setSfbShort(s []int) {
	cs := cints(s)
	C.mp3parity_vbrfr_set_sfb_short(c.h, &cs[0], C.int(len(s)))
}
func (c *cgoHandle) huffmanInit() { C.mp3parity_vbrfr_huffman_init(c.h) }

func (c *cgoHandle) setXr(gr, ch int, xr []float32) {
	cx := cfloats(xr)
	C.mp3parity_vbrfr_set_xr(c.h, C.int(gr), C.int(ch), &cx[0], C.int(len(xr)))
}
func (c *cgoHandle) setWidth(gr, ch int, w []int) {
	cw := cints(w)
	C.mp3parity_vbrfr_set_width(c.h, C.int(gr), C.int(ch), &cw[0], C.int(len(w)))
}
func (c *cgoHandle) setWindow(gr, ch int, win []int) {
	cw := cints(win)
	C.mp3parity_vbrfr_set_window(c.h, C.int(gr), C.int(ch), &cw[0], C.int(len(win)))
}
func (c *cgoHandle) setEac(gr, ch int, eac []byte) {
	cb := make([]C.char, len(eac))
	for i, v := range eac {
		cb[i] = C.char(v)
	}
	C.mp3parity_vbrfr_set_eac(c.h, C.int(gr), C.int(ch), &cb[0], C.int(len(eac)))
}
func (c *cgoHandle) setGeom(gr, ch, blockType, mixedBlockFlag, sfbmax, sfbdivide, psymax, maxNonzeroCoeff int, xrpowMax float32) {
	C.mp3parity_vbrfr_set_geom(c.h, C.int(gr), C.int(ch), C.int(blockType), C.int(mixedBlockFlag),
		C.int(sfbmax), C.int(sfbdivide), C.int(psymax), C.int(maxNonzeroCoeff), C.float(xrpowMax))
}

// encode drives the genuine VBR_encode_frame over the handle and returns its
// bit-usage result. xr34orig is the flat 2*2*576 grid, l3Xmin the flat
// 2*2*SFBMAX grid, maxBits the flat 2*2 budget.
func (c *cgoHandle) encode(xr34orig, l3Xmin []float32, maxBits []int) int {
	cx := cfloats(xr34orig)
	cm := cfloats(l3Xmin)
	cb := cints(maxBits)
	return int(C.mp3parity_vbrfr_encode(c.h, &cx[0], &cm[0], &cb[0]))
}

func (c *cgoHandle) globalGain(gr, ch int) int {
	return int(C.mp3parity_vbrfr_global_gain(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) scalefacScale(gr, ch int) int {
	return int(C.mp3parity_vbrfr_scalefac_scale(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) preflag(gr, ch int) int {
	return int(C.mp3parity_vbrfr_preflag(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) scalefac(gr, ch, sfb int) int {
	return int(C.mp3parity_vbrfr_scalefac(c.h, C.int(gr), C.int(ch), C.int(sfb)))
}
func (c *cgoHandle) subblockGain(gr, ch, i int) int {
	return int(C.mp3parity_vbrfr_subblock_gain(c.h, C.int(gr), C.int(ch), C.int(i)))
}
func (c *cgoHandle) l3enc(gr, ch, i int) int {
	return int(C.mp3parity_vbrfr_l3enc(c.h, C.int(gr), C.int(ch), C.int(i)))
}
func (c *cgoHandle) part23Length(gr, ch int) int {
	return int(C.mp3parity_vbrfr_part2_3_length(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) part2Length(gr, ch int) int {
	return int(C.mp3parity_vbrfr_part2_length(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) scalefacCompress(gr, ch int) int {
	return int(C.mp3parity_vbrfr_scalefac_compress(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) bigValues(gr, ch int) int {
	return int(C.mp3parity_vbrfr_big_values(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) tableSelect(gr, ch, i int) int {
	return int(C.mp3parity_vbrfr_table_select(c.h, C.int(gr), C.int(ch), C.int(i)))
}
func (c *cgoHandle) region0Count(gr, ch int) int {
	return int(C.mp3parity_vbrfr_region0_count(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) region1Count(gr, ch int) int {
	return int(C.mp3parity_vbrfr_region1_count(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) scfsi(ch, band int) int {
	return int(C.mp3parity_vbrfr_scfsi(c.h, C.int(ch), C.int(band)))
}
