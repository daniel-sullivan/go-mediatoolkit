// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Exported test hooks for the vbrquantize-sfalloc parity oracle
// (internal/parity_tests/vbrquantize-sfalloc).
//
// The VBR scalefactor-allocation tier (vbrquantize_sfalloc.go: blockSf,
// quantizeX34, setSubblockGain, setScalefacs, checkScalefactor,
// shortBlockConstrain, longBlockConstrain, bitcount, quantizeAndCountBits, and
// the algoT struct + alloc/find dispatch) is a 1:1 translation of LAME's
// `static` vbrquantize.c functions and has no place in the public surface. The
// wrappers below exist solely so the parity suite — which lives in its own
// package because it compiles the vendored vbrquantize.c oracle — can assert the
// Go port matches the genuine C bit-for-bit. They are mp3lame-gated like the
// slice they expose.
//
// The hooks operate on a fabricated GrInfo (the per-granule gr_info) inside a
// LameInternalFlags context, mirroring the oracle's mp3parity_vbrsf_t handle
// (which calloc's a gfc and touches the same gr_info fields). Both sides receive
// byte-identical inputs and the Go side returns the resolved side-info fields
// (global_gain / scalefac / subblock_gain / scalefac_scale / preflag /
// part2_3_length) for the assertion.

// VbrSfFindGuess / VbrSfFindFull select which find_sf dispatch blockSf uses,
// matching VBR_encode_frame's `find = (full_outer_loop < 0) ? guess : find`.
const (
	VbrSfFindGuess = 0 // guess_scalefac_x34
	VbrSfFindFull  = 1 // find_scalefac_x34
)

// newVbrSfAlgo builds an algoT over gfc/gi with the find dispatch selected by
// findSel, mirroring the oracle handle setup. It is the shared constructor the
// hooks below use; not exported (the hooks pass the selector through).
func newVbrSfAlgo(gfc *LameInternalFlags, gi *GrInfo, xr34orig []float32, findSel int) *algoT {
	that := &algoT{
		Xr34orig: xr34orig,
		Gfc:      gfc,
		CodInfo:  gi,
	}
	if findSel == VbrSfFindFull {
		that.find = func(xr, xr34 []float32, l3Xmin float32, bw uint, sfMin uint8) uint8 {
			return findScalefacX34(xr, xr34, l3Xmin, bw, sfMin)
		}
	} else {
		that.find = func(xr, xr34 []float32, l3Xmin float32, bw uint, sfMin uint8) uint8 {
			return guessScalefacX34(xr, xr34, l3Xmin, bw, sfMin)
		}
	}
	return that
}

// BlockSf exposes block_sf (vbrquantize.c:394). It builds an algoT over gi with
// the findSel dispatch, runs blockSf, and returns (vbrmax, mingainL, mingainS).
// vbrsf and vbrsfmin (length SFBMAX) are filled in place. gi.Xr / Width /
// EnergyAboveCutoff / MaxNonzeroCoeff / Psymax must be populated by the caller.
func (gfc *LameInternalFlags) BlockSf(gi *GrInfo, xr34orig []float32, l3Xmin []float32,
	findSel int, vbrsf, vbrsfmin []int) (vbrmax, mingainL int, mingainS [3]int) {
	that := newVbrSfAlgo(gfc, gi, xr34orig, findSel)
	vbrmax = blockSf(that, l3Xmin, vbrsf, vbrsfmin)
	return vbrmax, that.MingainL, that.MingainS
}

// QuantizeX34 exposes quantize_x34 (vbrquantize.c:500). It quantizes xr34orig
// into gi.L3Enc using gi's resolved scalefactors. The caller must have set
// gi.GlobalGain / Scalefac / Preflag / SubblockGain / Window / Width /
// ScalefacScale / MaxNonzeroCoeff.
func (gfc *LameInternalFlags) QuantizeX34(gi *GrInfo, xr34orig []float32) {
	that := newVbrSfAlgo(gfc, gi, xr34orig, VbrSfFindFull)
	quantizeX34(that)
}

// SetSubblockGain exposes set_subblock_gain (vbrquantize.c:595). sf (length
// SFBMAX) is updated in place; gi.SubblockGain / GlobalGain are mutated.
func (gfc *LameInternalFlags) SetSubblockGain(gi *GrInfo, mingainS [3]int, sf []int) {
	ms := mingainS
	setSubblockGain(gi, &ms, sf)
}

// SetScalefacs exposes set_scalefacs (vbrquantize.c:688). maxRangeSel picks the
// table: 0 -> max_range_short, 1 -> max_range_long, 2 -> max_range_long_lsf_-
// pretab. sf (length SFBMAX) is updated in place; gi.Scalefac is filled.
func (gfc *LameInternalFlags) SetScalefacs(gi *GrInfo, vbrsfmin []int, sf []int, maxRangeSel int) {
	var mr []uint8
	switch maxRangeSel {
	case 1:
		mr = maxRangeLong[:]
	case 2:
		mr = maxRangeLongLsfPretab[:]
	default:
		mr = maxRangeShort[:]
	}
	setScalefacs(gi, vbrsfmin, sf, mr)
}

// CheckScalefactor exposes checkScalefactor (vbrquantize.c:732): true iff no
// band is over-amplified relative to vbrsfmin.
func (gfc *LameInternalFlags) CheckScalefactor(gi *GrInfo, vbrsfmin []int) bool {
	return checkScalefactor(gi, vbrsfmin)
}

// ShortBlockConstrain exposes short_block_constrain (vbrquantize.c:769). It
// resolves gi's side info from the block_sf survey (vbrsf / vbrsfmin / vbrmax).
// mingainL / mingainS seed the algoT's minimum-gain floors (set by block_sf in
// the full pipeline; supplied directly here so the allocator can be pinned in
// isolation).
func (gfc *LameInternalFlags) ShortBlockConstrain(gi *GrInfo, vbrsf, vbrsfmin []int, vbrmax int,
	mingainL int, mingainS [3]int) {
	that := newVbrSfAlgo(gfc, gi, nil, VbrSfFindFull)
	that.MingainL = mingainL
	that.MingainS = mingainS
	shortBlockConstrain(that, vbrsf, vbrsfmin, vbrmax)
}

// LongBlockConstrain exposes long_block_constrain (vbrquantize.c:847).
func (gfc *LameInternalFlags) LongBlockConstrain(gi *GrInfo, vbrsf, vbrsfmin []int, vbrmax int,
	mingainL int, mingainS [3]int) {
	that := newVbrSfAlgo(gfc, gi, nil, VbrSfFindFull)
	that.MingainL = mingainL
	that.MingainS = mingainS
	longBlockConstrain(that, vbrsf, vbrsfmin, vbrmax)
}

// QuantizeAndCountBits exposes quantizeAndCountBits (vbrquantize.c:999): quantize
// then noquant_count_bits, returning part2_3_length (also stored in gi).
func (gfc *LameInternalFlags) QuantizeAndCountBits(gi *GrInfo, xr34orig []float32) int {
	that := newVbrSfAlgo(gfc, gi, xr34orig, VbrSfFindFull)
	return quantizeAndCountBits(that)
}
