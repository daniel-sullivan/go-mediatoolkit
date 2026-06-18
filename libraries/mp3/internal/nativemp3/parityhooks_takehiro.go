// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Exported test hooks for the takehiro parity oracle.
//
// The takehiro bit-counting routines (takehiro.go) are unexported methods on
// LameInternalFlags. The internal/parity_tests/takehiro slice drives them
// against the vendored LAME C, so the verbatim pass-throughs below surface
// each one (and the bv_scf readback) without widening the public API. They add
// no behaviour beyond calling the unexported method / field they shadow.

// HuffmanInit exposes huffmanInit (takehiro.c huffman_init) for the parity
// oracle. It fills SvQnt.BvScf from ScalefacBand.L and wires ht[].
func (gfc *LameInternalFlags) HuffmanInit() { gfc.huffmanInit() }

// ChooseTable exposes the gfc->choose_table function pointer
// (choose_table_nonMMX). begin/end are coefficient indices into gi.L3Enc;
// *bits accumulates the chosen table's bit count.
func (gfc *LameInternalFlags) ChooseTable(gi *GrInfo, begin, end int, bits *int) int {
	return gfc.chooseTable(gi.L3Enc[:], begin, end, bits)
}

// NoquantCountBits exposes noquantCountBits (takehiro.c noquant_count_bits).
func (gfc *LameInternalFlags) NoquantCountBits(gi *GrInfo, prevNoise *CalcNoiseData) int {
	return gfc.noquantCountBits(gi, prevNoise)
}

// ScaleBitcount exposes scaleBitcount (takehiro.c scale_bitcount).
func (gfc *LameInternalFlags) ScaleBitcount(gi *GrInfo) int { return gfc.scaleBitcount(gi) }

// BestHuffmanDivide exposes bestHuffmanDivide (takehiro.c best_huffman_divide).
func (gfc *LameInternalFlags) BestHuffmanDivide(gi *GrInfo) { gfc.bestHuffmanDivide(gi) }

// BestScalefacStore exposes bestScalefacStore (takehiro.c best_scalefac_store).
func (gfc *LameInternalFlags) BestScalefacStore(gr, ch int, l3Side *IIISideInfo) {
	gfc.bestScalefacStore(gr, ch, l3Side)
}
