// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package vbriterationloop holds the vbr-iteration-loop parity slice: it pins the
// pure-Go nativemp3 port of LAME 3.100's per-frame VBR iteration DRIVERS —
// VBR_new_iteration_loop (quantize.c:1645, the vbr_mtrh / -V entry) and
// VBR_old_iteration_loop (quantize.c:1490, the vbr_rh entry), plus the static
// VBR_new_prepare / VBR_old_prepare / bitpressure_strategy / VBR_encode_granule
// they run (all in nativemp3/quantize_encode_vbr.go) — against the vendored C
// LAME reference compiled inline via cgo.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference across three TUs (oracle.c =
// quantize.c + quantize_pvt.c + reservoir.c; oracle_vbrquantize.c =
// vbrquantize.c; oracle_takehiro.c = takehiro.c + tables.c) so each go-test
// binary is symbol-self-contained, and it NEVER imports libraries/mp3 (which
// would duplicate the LAME symbols at link time) — only the pure-Go
// internal/nativemp3 port.
//
// Both sides receive byte-identical state — cfg (bitrate / reservoir / quant
// flags), sv_qnt (mask adjust, longfact/shortfact, sfb21_extra), the ATH masking
// floor, scalefac_band + huffman_init, the per-(gr,ch) MDCT lines (gr_info.xr)
// and psy ratio, the reservoir occupancy, and the per-(gr,ch) block type — and
// the iteration loop's frame output (per-(gr,ch) resolved side info + l3_enc, the
// chosen eov->bitrate_index, and the post-frame reservoir size) is pinned EXACT.
//
// This slice IS floating-point-bearing (the prepares' masking adjust /
// pow(10,.), bitpressure's xmin inflation, and the whole quantization the loop
// drives), so the result is bit-exact only under the mp3_strict build (FMA-free
// Go) against the -ffp-contract=off cgo oracle. The strict gate lives in
// parity_test.go.
package vbriterationloop

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
// pow43 / adj43asm globals are populated before the loop runs.
func cgoFillTables() { C.oracle_fill_tables() }

// cgoHandle wraps a C oracle handle (a zeroed lame_internal_flags + an ATH + a
// 2x2 III_psy_ratio grid) so one test can populate cfg / sv_qnt / ATH /
// reservoir / scalefac_band / geometry + inputs, drive an iteration loop and read
// the resolved frame output from the same C-side state.
type cgoHandle struct{ h *C.mp3parity_vbrit_t }

func cgoNewHandle() *cgoHandle { return &cgoHandle{h: C.mp3parity_vbrit_new()} }
func (c *cgoHandle) free()     { C.mp3parity_vbrit_free(c.h) }

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

func (c *cgoHandle) setCfg(modeGr, channelsOut, version, samplerateOut, avgBitrate,
	sideinfoLen, bufferConstraint, vbrMin, vbrMax, disableResv, freeFormat,
	enforceMin, modeExt int) {
	C.mp3parity_vbrit_set_cfg(c.h, C.int(modeGr), C.int(channelsOut), C.int(version),
		C.int(samplerateOut), C.int(avgBitrate), C.int(sideinfoLen), C.int(bufferConstraint),
		C.int(vbrMin), C.int(vbrMax), C.int(disableResv), C.int(freeFormat),
		C.int(enforceMin), C.int(modeExt))
}

func (c *cgoHandle) setCfgQuant(noiseShaping, fullOuterLoop, useBestHuffman int, athFixpoint, athCurve float32, athType int) {
	C.mp3parity_vbrit_set_cfg_quant(c.h, C.int(noiseShaping), C.int(fullOuterLoop),
		C.int(useBestHuffman), C.float(athFixpoint), C.float(athCurve), C.int(athType))
}

func (c *cgoHandle) setResv(resvSize, resvMax int) {
	C.mp3parity_vbrit_set_resv(c.h, C.int(resvSize), C.int(resvMax))
}

func (c *cgoHandle) setBinsearch(oldValue, currentStep int) {
	C.mp3parity_vbrit_set_binsearch(c.h, C.int(oldValue), C.int(currentStep))
}

func (c *cgoHandle) setSvQnt(maskAdjust, maskAdjustShort float32, substepShaping, sfb21Extra int) {
	C.mp3parity_vbrit_set_svqnt(c.h, C.float(maskAdjust), C.float(maskAdjustShort),
		C.int(substepShaping), C.int(sfb21Extra))
}

func (c *cgoHandle) setLongfact(lf []float32) {
	cl := cfloats(lf)
	C.mp3parity_vbrit_set_longfact(c.h, &cl[0], C.int(len(lf)))
}
func (c *cgoHandle) setShortfact(sf []float32) {
	cs := cfloats(sf)
	C.mp3parity_vbrit_set_shortfact(c.h, &cs[0], C.int(len(sf)))
}

func (c *cgoHandle) setATH(adjustFactor, floor float32, l, s []float32) {
	cl := cfloats(l)
	cs := cfloats(s)
	C.mp3parity_vbrit_set_ath(c.h, C.float(adjustFactor), C.float(floor),
		&cl[0], C.int(len(l)), &cs[0], C.int(len(s)))
}

func (c *cgoHandle) setSfbLong(l []int) {
	cl := cints(l)
	C.mp3parity_vbrit_set_sfb_long(c.h, &cl[0], C.int(len(l)))
}
func (c *cgoHandle) setSfbShort(s []int) {
	cs := cints(s)
	C.mp3parity_vbrit_set_sfb_short(c.h, &cs[0], C.int(len(s)))
}
func (c *cgoHandle) huffmanInit() { C.mp3parity_vbrit_huffman_init(c.h) }

func (c *cgoHandle) setXr(gr, ch int, xr []float32) {
	cx := cfloats(xr)
	C.mp3parity_vbrit_set_xr(c.h, C.int(gr), C.int(ch), &cx[0], C.int(len(xr)))
}
func (c *cgoHandle) setGeom(gr, ch, blockType, mixedBlockFlag int) {
	C.mp3parity_vbrit_set_geom(c.h, C.int(gr), C.int(ch), C.int(blockType), C.int(mixedBlockFlag))
}
func (c *cgoHandle) setRatioL(gr, ch int, enL, thmL []float32) {
	ce := cfloats(enL)
	ct := cfloats(thmL)
	C.mp3parity_vbrit_set_ratio_l(c.h, C.int(gr), C.int(ch), &ce[0], &ct[0], C.int(len(enL)))
}
func (c *cgoHandle) setRatioS(gr, ch int, enS, thmS []float32, n int) {
	ce := cfloats(enS)
	ct := cfloats(thmS)
	C.mp3parity_vbrit_set_ratio_s(c.h, C.int(gr), C.int(ch), &ce[0], &ct[0], C.int(n))
}

func (c *cgoHandle) runNew(pe []float32, mer []float32) {
	cp := cfloats(pe)
	cm := cfloats(mer)
	C.mp3parity_vbrit_run_new(c.h, &cp[0], &cm[0])
}
func (c *cgoHandle) runOld(pe []float32, mer []float32) {
	cp := cfloats(pe)
	cm := cfloats(mer)
	C.mp3parity_vbrit_run_old(c.h, &cp[0], &cm[0])
}

func (c *cgoHandle) bitrateIndex() int { return int(C.mp3parity_vbrit_bitrate_index(c.h)) }
func (c *cgoHandle) resvSize() int     { return int(C.mp3parity_vbrit_resv_size(c.h)) }
func (c *cgoHandle) modeExt() int      { return int(C.mp3parity_vbrit_mode_ext(c.h)) }

func (c *cgoHandle) globalGain(gr, ch int) int {
	return int(C.mp3parity_vbrit_global_gain(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) scalefacScale(gr, ch int) int {
	return int(C.mp3parity_vbrit_scalefac_scale(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) preflag(gr, ch int) int {
	return int(C.mp3parity_vbrit_preflag(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) scalefac(gr, ch, sfb int) int {
	return int(C.mp3parity_vbrit_scalefac(c.h, C.int(gr), C.int(ch), C.int(sfb)))
}
func (c *cgoHandle) subblockGain(gr, ch, i int) int {
	return int(C.mp3parity_vbrit_subblock_gain(c.h, C.int(gr), C.int(ch), C.int(i)))
}
func (c *cgoHandle) l3enc(gr, ch, i int) int {
	return int(C.mp3parity_vbrit_l3enc(c.h, C.int(gr), C.int(ch), C.int(i)))
}
func (c *cgoHandle) part23Length(gr, ch int) int {
	return int(C.mp3parity_vbrit_part2_3_length(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) part2Length(gr, ch int) int {
	return int(C.mp3parity_vbrit_part2_length(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) scalefacCompress(gr, ch int) int {
	return int(C.mp3parity_vbrit_scalefac_compress(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) bigValues(gr, ch int) int {
	return int(C.mp3parity_vbrit_big_values(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) tableSelect(gr, ch, i int) int {
	return int(C.mp3parity_vbrit_table_select(c.h, C.int(gr), C.int(ch), C.int(i)))
}
func (c *cgoHandle) region0Count(gr, ch int) int {
	return int(C.mp3parity_vbrit_region0_count(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) region1Count(gr, ch int) int {
	return int(C.mp3parity_vbrit_region1_count(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoHandle) blockType(gr, ch int) int {
	return int(C.mp3parity_vbrit_block_type(c.h, C.int(gr), C.int(ch)))
}
