// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package takehiro holds the takehiro parity slice: it pins the pure-Go
// nativemp3 port of LAME 3.100's Huffman-table-selection / bit-counting stage
// (takehiro.go — a 1:1 translation of libmp3lame/takehiro.c's integer
// bit-counting core) against the vendored LAME C reference compiled inline via
// cgo (oracle.c, which #includes the committed libmp3lame/takehiro.c +
// tables.c).
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference so each go-test binary is
// symbol-self-contained, and it NEVER imports libraries/mp3 (which would
// duplicate the LAME symbols at link time) — only the pure-Go
// internal/nativemp3 port.
//
// SCOPE — the integer bit-counting core. huffman_init (bv_scf table),
// gfc->choose_table (choose_table_nonMMX over count_bit_ESC / noESC /
// from2 / from3), noquant_count_bits, scale_bitcount (mpeg1 + mpeg2-LSF),
// best_huffman_divide and best_scalefac_store are each driven on both sides
// over identical fabricated gr_info + scalefac_band input, and the full filled
// side information (region counts, table selects, count1, part2/part2_3
// lengths, scalefac_compress/scale/preflag, slen, scfsi) must be bit-for-bit
// equal. The FP quantizer front-end (count_bits / quantize_xrpow) is a
// separate slice and is not driven here.
//
// This slice is integer-only — no floating point anywhere on the exercised
// paths — so its results are independent of FMA/vectorization. The bit-exact
// assertions are nonetheless gated behind nativemp3.StrictMode per the
// FP-parity convention, so a bare `go test` is clean and the strict run
// (mp3lame + mp3_strict + the FP CGO env) is the authoritative bit-exact gate.
//
// LGPL: takehiro.c is LGPL LAME source, so this package is gated by mp3lame
// (in addition to cgo) like the stage it pins; a bare `go test` never compiles
// it.
package takehiro

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

// cgoTk drives the vendored LAME takehiro routines over a C-owned
// lame_internal_flags. Every field the routines read or write is configured
// and read back through the oracle trampolines so the nativemp3
// LameInternalFlags can be compared field-for-field.
type cgoTk struct {
	h *C.mp3parity_tk_t
}

func newCgoTk() *cgoTk { return &cgoTk{h: C.mp3parity_tk_new()} }

func (c *cgoTk) free() { C.mp3parity_tk_free(c.h) }

func (c *cgoTk) setCfg(modeGr, useBestHuffman int) {
	C.mp3parity_tk_set_cfg(c.h, C.int(modeGr), C.int(useBestHuffman))
}

func cIntSlice(v []int) (*C.int, C.int) {
	if len(v) == 0 {
		return nil, 0
	}
	buf := make([]C.int, len(v))
	for i, x := range v {
		buf[i] = C.int(x)
	}
	return &buf[0], C.int(len(v))
}

func (c *cgoTk) setSfbLong(l []int) {
	p, n := cIntSlice(l)
	C.mp3parity_tk_set_sfb_long(c.h, p, n)
}

func (c *cgoTk) setSfbShort(s []int) {
	p, n := cIntSlice(s)
	C.mp3parity_tk_set_sfb_short(c.h, p, n)
}

func (c *cgoTk) setL3Enc(gr, ch int, ix []int) {
	p, n := cIntSlice(ix)
	C.mp3parity_tk_set_l3enc(c.h, C.int(gr), C.int(ch), p, n)
}

func (c *cgoTk) setScalefac(gr, ch int, sf []int) {
	p, n := cIntSlice(sf)
	C.mp3parity_tk_set_scalefac(c.h, C.int(gr), C.int(ch), p, n)
}

func (c *cgoTk) setWidth(gr, ch int, w []int) {
	p, n := cIntSlice(w)
	C.mp3parity_tk_set_width(c.h, C.int(gr), C.int(ch), p, n)
}

func (c *cgoTk) setWindow(gr, ch int, win []int) {
	p, n := cIntSlice(win)
	C.mp3parity_tk_set_window(c.h, C.int(gr), C.int(ch), p, n)
}

func (c *cgoTk) setGeom(gr, ch, blockType, mixedBlockFlag, globalGain,
	scalefacScale, preflag, sfbmax, sfbdivide, maxNonzeroCoeff, part23Length int) {
	C.mp3parity_tk_set_geom(c.h, C.int(gr), C.int(ch), C.int(blockType),
		C.int(mixedBlockFlag), C.int(globalGain), C.int(scalefacScale),
		C.int(preflag), C.int(sfbmax), C.int(sfbdivide), C.int(maxNonzeroCoeff),
		C.int(part23Length))
}

func (c *cgoTk) huffmanInit() { C.mp3parity_tk_huffman_init(c.h) }

func (c *cgoTk) chooseTable(gr, ch, begin, end int, bits *int) int {
	var cb C.int = C.int(*bits)
	r := C.mp3parity_tk_choose_table(c.h, C.int(gr), C.int(ch), C.int(begin),
		C.int(end), (*C.int)(unsafe.Pointer(&cb)))
	*bits = int(cb)
	return int(r)
}

func (c *cgoTk) noquantCountBits(gr, ch int) int {
	return int(C.mp3parity_tk_noquant_count_bits(c.h, C.int(gr), C.int(ch)))
}

func (c *cgoTk) scaleBitcount(gr, ch int) int {
	return int(C.mp3parity_tk_scale_bitcount(c.h, C.int(gr), C.int(ch)))
}

func (c *cgoTk) bestHuffmanDivide(gr, ch int) {
	C.mp3parity_tk_best_huffman_divide(c.h, C.int(gr), C.int(ch))
}

func (c *cgoTk) bestScalefacStore(gr, ch int) {
	C.mp3parity_tk_best_scalefac_store(c.h, C.int(gr), C.int(ch))
}

func (c *cgoTk) bvScf(i int) int { return int(C.mp3parity_tk_bv_scf(c.h, C.int(i))) }
func (c *cgoTk) bigValues(gr, ch int) int {
	return int(C.mp3parity_tk_big_values(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoTk) count1(gr, ch int) int {
	return int(C.mp3parity_tk_count1(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoTk) count1bits(gr, ch int) int {
	return int(C.mp3parity_tk_count1bits(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoTk) count1tableSelect(gr, ch int) int {
	return int(C.mp3parity_tk_count1table_select(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoTk) region0Count(gr, ch int) int {
	return int(C.mp3parity_tk_region0_count(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoTk) region1Count(gr, ch int) int {
	return int(C.mp3parity_tk_region1_count(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoTk) tableSelect(gr, ch, i int) int {
	return int(C.mp3parity_tk_table_select(c.h, C.int(gr), C.int(ch), C.int(i)))
}
func (c *cgoTk) part23Length(gr, ch int) int {
	return int(C.mp3parity_tk_part2_3_length(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoTk) part2Length(gr, ch int) int {
	return int(C.mp3parity_tk_part2_length(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoTk) scalefacCompress(gr, ch int) int {
	return int(C.mp3parity_tk_scalefac_compress(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoTk) scalefacScale(gr, ch int) int {
	return int(C.mp3parity_tk_scalefac_scale(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoTk) preflag(gr, ch int) int {
	return int(C.mp3parity_tk_preflag(c.h, C.int(gr), C.int(ch)))
}
func (c *cgoTk) scalefac(gr, ch, sfb int) int {
	return int(C.mp3parity_tk_scalefac(c.h, C.int(gr), C.int(ch), C.int(sfb)))
}
func (c *cgoTk) slen(gr, ch, i int) int {
	return int(C.mp3parity_tk_slen(c.h, C.int(gr), C.int(ch), C.int(i)))
}
func (c *cgoTk) scfsi(ch, i int) int {
	return int(C.mp3parity_tk_scfsi(c.h, C.int(ch), C.int(i)))
}
