// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Layer III Huffman-table selection and bit counting — a 1:1 translation of
// the vendored LAME 3.100 encoder's libmp3lame/takehiro.c. This is the
// encoder's bit-cost oracle: given a granule's quantized coefficients
// (gi.L3Enc), it picks the cheapest Huffman codebooks for each big-value
// region and the count1 region, fills the region split / table_select side
// information, and returns the part2_3 bit length the quantizer's rate loop
// compares against the target. It also counts the scalefactor (part2) bits
// and chooses the optimal scalefac_compress.
//
// # Scope of this slice
//
// This file covers takehiro.c's INTEGER bit-counting core: ix_max,
// count_bit_ESC / count_bit_noESC / count_bit_noESC_from2 / from3 /
// count_bit_null, choose_table_nonMMX, noquant_count_bits, the
// best_huffman_divide region-split optimizer (recalc_divide_init /
// recalc_divide_sub), scfsi_calc, best_scalefac_store, the scalefactor bit
// counters mpeg1_scale_bitcount / mpeg2_scale_bitcount (scale_bitcount_lsf) /
// scale_bitcount, and huffman_init. Every one of these is pure integer
// arithmetic over the Huffman bit-length tables (takehiro_huffman_tables.go),
// so it is bit-identical regardless of FMA/vectorization — no *_fp_strict /
// *_fp_default split is needed. The strict parity gate verifies them anyway.
//
// The floating-point quantizer front-end of takehiro.c — count_bits (which
// quantizes xr via IPOW20 then calls noquant_count_bits) and its
// quantize_xrpow / quantize_lines_xrpow helpers — is NOT in this slice. Those
// depend on the TAKEHIRO_IEEE754_HACK adj43asm path whose precompute and
// dispatch are owned by the quantize_pvt / iteration-init slices
// (quantize_pvt.go); they join when that quantizer rate-loop slice lands.
// This slice's count_bits seam is therefore noquant_count_bits, the genuine
// integer bit counter the rate loop ultimately calls.
//
// # choose_table function pointer
//
// LAME selects gfc->choose_table at huffman_init time (choose_table_nonMMX,
// or choose_table_MMX when MMX is available). The vendored config.h builds
// the scalar baseline (no MMX), so the pointer is always choose_table_nonMMX;
// the port realizes the pointer as the chooseTable method below, which
// huffman_init documents it installs.

// chooseTable is LAME's gfc->choose_table function pointer, installed by
// huffman_init. The vendored config.h has no MMX, so it is always
// choose_table_nonMMX. Callers (noquant_count_bits, recalc_divide_*,
// best_huffman_divide) invoke it exactly where the C dereferences the pointer.
func (gfc *LameInternalFlags) chooseTable(ix []int, begin, end int, s *int) int {
	return chooseTableNonMMX(ix, begin, end, s)
}

// largeBits is takehiro/quantize_pvt.h:126's LARGE_BITS (100000): the
// sentinel "this coding is infeasible" bit count.
const largeBits = 100000

// ixMax returns the maximum coefficient magnitude in ix[begin:end]
// (takehiro.c:423, ix_max). The C unrolls two lanes (max1 / max2) over a
// do/while; the port keeps the two-lane reduction so the traversal order and
// pairwise compares match exactly. end is exclusive; the region has an even,
// non-zero length (the caller guarantees it).
func ixMax(ix []int, begin, end int) int {
	max1, max2 := 0, 0
	i := begin
	for {
		x1 := ix[i]
		i++
		x2 := ix[i]
		i++
		if max1 < x1 {
			max1 = x1
		}
		if max2 < x2 {
			max2 = x2
		}
		if i >= end {
			break
		}
	}
	if max1 < max2 {
		max1 = max2
	}
	return max1
}

// countBitESC counts the bits to code ix[begin:end] with one of the two
// ESC (linbits) Huffman tables t1 / t2, returning the cheaper table index and
// adding its bit count to *s (takehiro.c:449, count_bit_ESC). linbits packs
// both candidates' xlen into the high/low 16 bits, and largetbl packs both
// tables' base lengths, so each pair costs one table lookup that accumulates
// both candidates at once; the high/low halves of sum are then compared.
func countBitESC(ix []int, begin, end, t1, t2 int, s *int) int {
	// ESC-table is used
	linbits := ht[t1].Xlen*65536 + ht[t2].Xlen
	var sum uint = 0

	i := begin
	for {
		x := uint(ix[i])
		i++
		y := uint(ix[i])
		i++

		if x >= 15 {
			x = 15
			sum += linbits
		}
		if y >= 15 {
			y = 15
			sum += linbits
		}
		x <<= 4
		x += y
		sum += uint(largetbl[x])
		if i >= end {
			break
		}
	}

	sum2 := sum & 0xffff
	sum >>= 16

	if sum > sum2 {
		sum = sum2
		t1 = t2
	}

	*s += int(sum)
	return t1
}

// countBitNoESC counts the bits to code ix[begin:end] with table 1 (the only
// table whose max magnitude is 1), adding the count to *s and returning 1
// (takehiro.c:486, count_bit_noESC). It reads ht[1].hlen (t1l) indexed by the
// packed pair x0+x0 + x1 (xlen for table 1 is 2).
func countBitNoESC(ix []int, begin, end, mx int, s *int) int {
	// No ESC-words
	var sum1 uint = 0
	hlen1 := ht[1].Hlen
	_ = mx

	i := begin
	for {
		x0 := uint(ix[i])
		i++
		x1 := uint(ix[i])
		i++
		sum1 += uint(hlen1[x0+x0+x1])
		if i >= end {
			break
		}
	}

	*s += int(sum1)
	return 1
}

// countBitNoESCFrom2 counts the bits to code ix[begin:end] using the no-ESC
// table pair derived from max (table 2/3 or 5/6), via the packed table23 /
// table56 length table, returning the cheaper index and adding its count to
// *s (takehiro.c:510, count_bit_noESC_from2).
func countBitNoESCFrom2(ix []int, begin, end, max int, s *int) int {
	t1 := hufTblNoESC[max-1]
	// No ESC-words
	xlen := int(ht[t1].Xlen)
	var table []uint32
	if t1 == 2 {
		table = table23
	} else {
		table = table56
	}
	var sum uint = 0

	i := begin
	for {
		x0 := ix[i]
		i++
		x1 := ix[i]
		i++
		sum += uint(table[x0*xlen+x1])
		if i >= end {
			break
		}
	}

	sum2 := sum & 0xffff
	sum >>= 16

	if sum > sum2 {
		sum = sum2
		t1++
	}

	*s += int(sum)
	return t1
}

// countBitNoESCFrom3 counts the bits to code ix[begin:end] across the three
// consecutive no-ESC tables starting at the base table for max (7, 10 or 13),
// returning the cheapest of the three and adding its count to *s
// (takehiro.c:538, count_bit_noESC_from3). It sums all three candidate
// lengths in one pass over the packed pair index.
func countBitNoESCFrom3(ix []int, begin, end, max int, s *int) int {
	t1 := hufTblNoESC[max-1]
	// No ESC-words
	var sum1 uint = 0
	var sum2 uint = 0
	var sum3 uint = 0
	xlen := int(ht[t1].Xlen)
	hlen1 := ht[t1].Hlen
	hlen2 := ht[t1+1].Hlen
	hlen3 := ht[t1+2].Hlen

	i := begin
	for {
		x0 := ix[i]
		i++
		x1 := ix[i]
		i++
		x := x0*xlen + x1
		sum1 += uint(hlen1[x])
		sum2 += uint(hlen2[x])
		sum3 += uint(hlen3[x])
		if i >= end {
			break
		}
	}

	t := t1
	if sum1 > sum2 {
		sum1 = sum2
		t++
	}
	if sum1 > sum3 {
		sum1 = sum3
		t = t1 + 2
	}
	*s += int(sum1)

	return t
}

// countBitNull is takehiro.c:588's count_bit_null: the no-op counter for an
// all-zero region (max 0), the first entry of count_fncs.
func countBitNull(ix []int, begin, end, max int, s *int) int {
	_, _, _, _ = ix, begin, end, max
	_ = s
	return 0
}

// countFnc is takehiro.c:597's count_fnc typedef: the bit-counter signature
// count_fncs dispatches on for max <= 15. The port carries the (ix, begin,
// end) trio where the C passes (ix, end) pointers into the same array.
type countFnc func(ix []int, begin, end, max int, s *int) int

// countFncs is takehiro.c:599's count_fncs[]: the max-indexed dispatch table
// (max 0 -> null, 1 -> noESC, 2..3 -> from2, 4..15 -> from3).
var countFncs = [16]countFnc{
	countBitNull,
	countBitNoESC,
	countBitNoESCFrom2,
	countBitNoESCFrom2,
	countBitNoESCFrom3,
	countBitNoESCFrom3,
	countBitNoESCFrom3,
	countBitNoESCFrom3,
	countBitNoESCFrom3,
	countBitNoESCFrom3,
	countBitNoESCFrom3,
	countBitNoESCFrom3,
	countBitNoESCFrom3,
	countBitNoESCFrom3,
	countBitNoESCFrom3,
	countBitNoESCFrom3,
}

// chooseTableNonMMX picks the Huffman table that codes ix[begin:end] in the
// fewest bits, adding that bit count to *s and returning the table index (or
// -1 with *s = LARGE_BITS when a coefficient exceeds the max codeable value)
// (takehiro.c:618, choose_table_nonMMX). For max <= 15 it dispatches through
// count_fncs; otherwise it scans the two ESC-table families (24..31 then
// choice2-8..23) for the smallest linmax >= (max-15) and calls count_bit_ESC.
func chooseTableNonMMX(ix []int, begin, end int, s *int) int {
	max := ixMax(ix, begin, end)

	if max <= 15 {
		return countFncs[max](ix, begin, end, max, s)
	}
	// try tables with linbits
	if max > IXMAXVAL {
		*s = largeBits
		return -1
	}
	max -= 15
	choice2 := 24
	for ; choice2 < 32; choice2++ {
		if int(ht[choice2].Linmax) >= max {
			break
		}
	}

	choice := choice2 - 8
	for ; choice < 24; choice++ {
		if int(ht[choice].Linmax) >= max {
			break
		}
	}
	return countBitESC(ix, begin, end, choice, choice2, s)
}

// noquantCountBits counts the part2_3 bits for an already-quantized granule
// and fills its region split / table_select / count1 side information
// (takehiro.c:654, noquant_count_bits). It locates the count1 region, picks
// the count1 codebook (t32l vs t33l), then determines the big-value region
// boundaries (long blocks use bv_scf; short blocks use scalefac_band.s[3])
// and the cheapest big-value Huffman tables, optionally running
// best_huffman_divide. Returns the big-value + count1 bit total (the rate
// loop adds part2/scalefactor bits separately).
func (gfc *LameInternalFlags) noquantCountBits(gi *GrInfo, prevNoise *CalcNoiseData) int {
	cfg := &gfc.Cfg
	bits := 0
	ix := gi.L3Enc[:]

	i := iMin(576, ((gi.MaxNonzeroCoeff+2)>>1)<<1)

	if prevNoise != nil {
		prevNoise.SfbCount1 = 0
	}

	// Determine count1 region
	for ; i > 1; i -= 2 {
		if ix[i-1]|ix[i-2] != 0 {
			break
		}
	}
	gi.Count1 = i

	// Determines the number of bits to encode the quadruples.
	a1, a2 := 0, 0
	for ; i > 3; i -= 4 {
		x4 := ix[i-4]
		x3 := ix[i-3]
		x2 := ix[i-2]
		x1 := ix[i-1]

		// hack to check if all values <= 1
		if uint(x4|x3|x2|x1) > 1 {
			break
		}

		p := ((x4*2+x3)*2+x2)*2 + x1
		a1 += t32l[p]
		a2 += t33l[p]
	}

	bits = a1
	gi.Count1tableSelect = 0
	if a1 > a2 {
		bits = a2
		gi.Count1tableSelect = 1
	}

	gi.Count1bits = bits
	gi.BigValues = i
	if i == 0 {
		return bits
	}

	if gi.BlockType == ShortType {
		a1 = 3 * gfc.ScalefacBand.S[3]
		if a1 > gi.BigValues {
			a1 = gi.BigValues
		}
		a2 = gi.BigValues
	} else if gi.BlockType == NormType {
		// bv_scf is C `char`; promote to int at read (the values stored are
		// the small, non-negative region counts).
		a1 = int(gfc.SvQnt.BvScf[i-2])
		gi.Region0Count = a1
		a2 = int(gfc.SvQnt.BvScf[i-1])
		gi.Region1Count = a2

		a2 = gfc.ScalefacBand.L[a1+a2+2]
		a1 = gfc.ScalefacBand.L[a1+1]
		if a2 < i {
			gi.TableSelect[2] = gfc.chooseTable(ix, a2, i, &bits)
		}
	} else {
		gi.Region0Count = 7
		// gi.region1_count = SBPSY_l - 7 - 1;
		gi.Region1Count = SBMAXl - 1 - 7 - 1
		a1 = gfc.ScalefacBand.L[7+1]
		a2 = i
		if a1 > a2 {
			a1 = a2
		}
	}

	// have to allow for the case when bigvalues < region0 < region1
	// (and region0, region1 are ignored)
	a1 = iMin(a1, i)
	a2 = iMin(a2, i)

	// Count the number of bits necessary to code the bigvalues region.
	if 0 < a1 {
		gi.TableSelect[0] = gfc.chooseTable(ix, 0, a1, &bits)
	}
	if a1 < a2 {
		gi.TableSelect[1] = gfc.chooseTable(ix, a1, a2, &bits)
	}
	if cfg.UseBestHuffman == 2 {
		gi.Part23Length = bits
		gfc.bestHuffmanDivide(gi)
		bits = gi.Part23Length
	}

	if prevNoise != nil {
		if gi.BlockType == NormType {
			sfb := 0
			for gfc.ScalefacBand.L[sfb] < gi.BigValues {
				sfb++
			}
			prevNoise.SfbCount1 = sfb
		}
	}

	return bits
}

// recalcDivideInit precomputes, for every (region0, region1) big-value split,
// the cheapest combined region0+region1 bit count and the tables that achieve
// it (takehiro.c:809, recalc_divide_init). It fills r01_bits / r01_div /
// r0_tbl / r1_tbl indexed by (r0+r1) for best_huffman_divide's region2 scan.
func (gfc *LameInternalFlags) recalcDivideInit(codInfo *GrInfo, ix []int,
	r01Bits, r01Div, r0Tbl, r1Tbl []int) {
	bigv := codInfo.BigValues

	for r0 := 0; r0 <= 7+15; r0++ {
		r01Bits[r0] = largeBits
	}

	for r0 := 0; r0 < 16; r0++ {
		a1 := gfc.ScalefacBand.L[r0+1]
		if a1 >= bigv {
			break
		}
		r0bits := 0
		r0t := gfc.chooseTable(ix, 0, a1, &r0bits)

		for r1 := 0; r1 < 8; r1++ {
			a2 := gfc.ScalefacBand.L[r0+r1+2]
			if a2 >= bigv {
				break
			}

			bits := r0bits
			r1t := gfc.chooseTable(ix, a1, a2, &bits)
			if r01Bits[r0+r1] > bits {
				r01Bits[r0+r1] = bits
				r01Div[r0+r1] = r0
				r0Tbl[r0+r1] = r0t
				r1Tbl[r0+r1] = r1t
			}
		}
	}
}

// recalcDivideSub scans region2 split points to find a cheaper three-region
// big-value coding than gi currently holds, copying codInfo2 into gi and
// updating its region counts / table selects when it improves part2_3_length
// (takehiro.c:847, recalc_divide_sub).
func (gfc *LameInternalFlags) recalcDivideSub(codInfo2, gi *GrInfo, ix []int,
	r01Bits, r01Div, r0Tbl, r1Tbl []int) {
	bigv := codInfo2.BigValues

	for r2 := 2; r2 < SBMAXl+1; r2++ {
		a2 := gfc.ScalefacBand.L[r2]
		if a2 >= bigv {
			break
		}

		bits := r01Bits[r2-2] + codInfo2.Count1bits
		if gi.Part23Length <= bits {
			break
		}

		r2t := gfc.chooseTable(ix, a2, bigv, &bits)
		if gi.Part23Length <= bits {
			continue
		}

		*gi = *codInfo2
		gi.Part23Length = bits
		gi.Region0Count = r01Div[r2-2]
		gi.Region1Count = r2 - 2 - r01Div[r2-2]
		gi.TableSelect[0] = r0Tbl[r2-2]
		gi.TableSelect[1] = r1Tbl[r2-2]
		gi.TableSelect[2] = r2t
	}
}

// bestHuffmanDivide re-optimizes a granule's big-value region split and count1
// boundary to minimize part2_3_length, updating gi in place
// (takehiro.c:884, best_huffman_divide). Long blocks run the full region0/1/2
// search via recalc_divide_init/sub; it also tries pushing two more lines into
// the count1 region. SHORT_TYPE under MPEG-2 is skipped (fails for mode_gr 1).
func (gfc *LameInternalFlags) bestHuffmanDivide(gi *GrInfo) {
	cfg := &gfc.Cfg
	ix := gi.L3Enc[:]

	var r01Bits [7 + 15 + 1]int
	var r01Div [7 + 15 + 1]int
	var r0Tbl [7 + 15 + 1]int
	var r1Tbl [7 + 15 + 1]int

	// SHORT BLOCK stuff fails for MPEG2
	if gi.BlockType == ShortType && cfg.ModeGr == 1 {
		return
	}

	var codInfo2 GrInfo
	codInfo2 = *gi
	if gi.BlockType == NormType {
		gfc.recalcDivideInit(gi, ix, r01Bits[:], r01Div[:], r0Tbl[:], r1Tbl[:])
		gfc.recalcDivideSub(&codInfo2, gi, ix, r01Bits[:], r01Div[:], r0Tbl[:], r1Tbl[:])
	}

	i := codInfo2.BigValues
	if i == 0 || uint(ix[i-2]|ix[i-1]) > 1 {
		return
	}

	i = gi.Count1 + 2
	if i > 576 {
		return
	}

	// Determines the number of bits to encode the quadruples.
	codInfo2 = *gi
	codInfo2.Count1 = i
	a1, a2 := 0, 0

	for ; i > codInfo2.BigValues; i -= 4 {
		p := ((ix[i-4]*2+ix[i-3])*2+ix[i-2])*2 + ix[i-1]
		a1 += t32l[p]
		a2 += t33l[p]
	}
	codInfo2.BigValues = i

	codInfo2.Count1tableSelect = 0
	if a1 > a2 {
		a1 = a2
		codInfo2.Count1tableSelect = 1
	}

	codInfo2.Count1bits = a1

	if codInfo2.BlockType == NormType {
		gfc.recalcDivideSub(&codInfo2, gi, ix, r01Bits[:], r01Div[:], r0Tbl[:], r1Tbl[:])
	} else {
		// Count the number of bits necessary to code the bigvalues region.
		codInfo2.Part23Length = a1
		a1 = gfc.ScalefacBand.L[7+1]
		if a1 > i {
			a1 = i
		}
		if a1 > 0 {
			codInfo2.TableSelect[0] = gfc.chooseTable(ix, 0, a1, &codInfo2.Part23Length)
		}
		if i > a1 {
			codInfo2.TableSelect[1] = gfc.chooseTable(ix, a1, i, &codInfo2.Part23Length)
		}
		if gi.Part23Length > codInfo2.Part23Length {
			*gi = codInfo2
		}
	}
}

// scfsiCalc applies scalefactor selection information (scfsi) for granule 1
// against granule 0, marking bands whose scalefactors can be inherited and
// recomputing the cheapest scalefac_compress for the reduced set
// (takehiro.c:964, scfsi_calc).
func scfsiCalc(ch int, l3Side *IIISideInfo) {
	gi := &l3Side.Tt[1][ch]
	g0 := &l3Side.Tt[0][ch]

	for i := 0; i < len(scfsiBand)-1; i++ {
		var sfb int
		for sfb = scfsiBand[i]; sfb < scfsiBand[i+1]; sfb++ {
			if g0.Scalefac[sfb] != gi.Scalefac[sfb] && gi.Scalefac[sfb] >= 0 {
				break
			}
		}
		if sfb == scfsiBand[i+1] {
			for sfb = scfsiBand[i]; sfb < scfsiBand[i+1]; sfb++ {
				gi.Scalefac[sfb] = -1
			}
			l3Side.Scfsi[ch][i] = 1
		}
	}

	s1, c1 := 0, 0
	var sfb int
	for sfb = 0; sfb < 11; sfb++ {
		if gi.Scalefac[sfb] == -1 {
			continue
		}
		c1++
		if s1 < gi.Scalefac[sfb] {
			s1 = gi.Scalefac[sfb]
		}
	}

	s2, c2 := 0, 0
	for ; sfb < SBPSYl; sfb++ {
		if gi.Scalefac[sfb] == -1 {
			continue
		}
		c2++
		if s2 < gi.Scalefac[sfb] {
			s2 = gi.Scalefac[sfb]
		}
	}

	for i := 0; i < 16; i++ {
		if s1 < slen1N[i] && s2 < slen2N[i] {
			c := slen1Tab[i]*c1 + slen2Tab[i]*c2
			if gi.Part2Length > c {
				gi.Part2Length = c
				gi.ScalefacCompress = i
			}
		}
	}
}

// bestScalefacStore finds the optimal way to store a granule's scalefactors
// after the final scalefactors are chosen, by zeroing scalefacs of all-zero
// bands, halving when scalefac_scale can absorb it, applying preemphasis when
// it fits, and (for MPEG-1 granule 1) running scfsi against granule 0
// (takehiro.c:1021, best_scalefac_store). It re-runs scale_bitcount when it
// changed anything.
func (gfc *LameInternalFlags) bestScalefacStore(gr, ch int, l3Side *IIISideInfo) {
	cfg := &gfc.Cfg
	// use scalefac_scale if we can
	gi := &l3Side.Tt[gr][ch]
	recalc := 0

	// remove scalefacs from bands with ix=0.  This idea comes
	// from the AAC ISO docs.  added mt 3/00
	// check if l3_enc=0
	j := 0
	for sfb := 0; sfb < gi.Sfbmax; sfb++ {
		width := gi.Width[sfb]
		l := j
		j += width
		for ; l < j; l++ {
			if gi.L3Enc[l] != 0 {
				break
			}
		}
		if l == j {
			gi.Scalefac[sfb] = -2
			recalc = -2 // anything goes.
		}
		// only best_scalefac_store and calc_scfsi
		// know--and only they should know--about the magic number -2.
	}

	if gi.ScalefacScale == 0 && gi.Preflag == 0 {
		s := 0
		for sfb := 0; sfb < gi.Sfbmax; sfb++ {
			if gi.Scalefac[sfb] > 0 {
				s |= gi.Scalefac[sfb]
			}
		}

		if (s&1) == 0 && s != 0 {
			for sfb := 0; sfb < gi.Sfbmax; sfb++ {
				if gi.Scalefac[sfb] > 0 {
					gi.Scalefac[sfb] >>= 1
				}
			}

			gi.ScalefacScale = 1
			recalc = 1
		}
	}

	if gi.Preflag == 0 && gi.BlockType != ShortType && cfg.ModeGr == 2 {
		var sfb int
		for sfb = 11; sfb < SBPSYl; sfb++ {
			if gi.Scalefac[sfb] < pretab[sfb] && gi.Scalefac[sfb] != -2 {
				break
			}
		}
		if sfb == SBPSYl {
			for sfb = 11; sfb < SBPSYl; sfb++ {
				if gi.Scalefac[sfb] > 0 {
					gi.Scalefac[sfb] -= pretab[sfb]
				}
			}

			gi.Preflag = 1
			recalc = 1
		}
	}

	for i := 0; i < 4; i++ {
		l3Side.Scfsi[ch][i] = 0
	}

	if cfg.ModeGr == 2 && gr == 1 &&
		l3Side.Tt[0][ch].BlockType != ShortType &&
		l3Side.Tt[1][ch].BlockType != ShortType {
		scfsiCalc(ch, l3Side)
		recalc = 0
	}
	for sfb := 0; sfb < gi.Sfbmax; sfb++ {
		if gi.Scalefac[sfb] == -2 {
			gi.Scalefac[sfb] = 0 // if anything goes, then 0 is a good choice
		}
	}
	if recalc != 0 {
		_ = gfc.scaleBitcount(gi)
	}
}

// mpeg1ScaleBitcount counts the MPEG-1 scalefactor bits and chooses the
// scalefac_compress that minimizes them, over all 16 candidate values rather
// than stopping at the first valid one (takehiro.c:1135, mpeg1_scale_bitcount).
// Returns non-zero when no valid scalefac_compress exists (over-amplified).
func (gfc *LameInternalFlags) mpeg1ScaleBitcount(codInfo *GrInfo) int {
	maxSlen1, maxSlen2 := 0, 0
	_ = gfc

	var tab []int
	scalefac := codInfo.Scalefac[:]

	if codInfo.BlockType == ShortType {
		tab = scaleShort[:]
		if codInfo.MixedBlockFlag != 0 {
			tab = scaleMixed[:]
		}
	} else { // block_type == 1,2,or 3
		tab = scaleLong[:]
		if codInfo.Preflag == 0 {
			var sfb int
			for sfb = 11; sfb < SBPSYl; sfb++ {
				if scalefac[sfb] < pretab[sfb] {
					break
				}
			}

			if sfb == SBPSYl {
				codInfo.Preflag = 1
				for sfb = 11; sfb < SBPSYl; sfb++ {
					scalefac[sfb] -= pretab[sfb]
				}
			}
		}
	}

	var sfb int
	for sfb = 0; sfb < codInfo.Sfbdivide; sfb++ {
		if maxSlen1 < scalefac[sfb] {
			maxSlen1 = scalefac[sfb]
		}
	}

	for ; sfb < codInfo.Sfbmax; sfb++ {
		if maxSlen2 < scalefac[sfb] {
			maxSlen2 = scalefac[sfb]
		}
	}

	// from Takehiro TOMINAGA 10/99: loop over *all* possible values of
	// scalefac_compress to find the one which uses the smallest number of
	// bits.  ISO would stop at first valid index
	codInfo.Part2Length = largeBits
	for k := 0; k < 16; k++ {
		if maxSlen1 < slen1N[k] && maxSlen2 < slen2N[k] &&
			codInfo.Part2Length > tab[k] {
			codInfo.Part2Length = tab[k]
			codInfo.ScalefacCompress = k
		}
	}
	if codInfo.Part2Length == largeBits {
		return 1
	}
	return 0
}

// mpeg2ScaleBitcount counts the MPEG-2 LSF scalefactor bits and derives
// scalefac_compress / slen[] / sfb_partition_table from the per-partition
// maximum scalefactors (takehiro.c:1217, mpeg2_scale_bitcount,
// "scale_bitcount_lsf"). Returns the over-amplification count (non-zero ==
// some partition exceeds its range and no valid coding exists).
func (gfc *LameInternalFlags) mpeg2ScaleBitcount(codInfo *GrInfo) int {
	var maxSfac [4]int
	scalefac := codInfo.Scalefac[:]

	// Set partition table. Note that should try to use table one,
	// but do not yet...
	var tableNumber int
	if codInfo.Preflag != 0 {
		tableNumber = 2
	} else {
		tableNumber = 0
	}

	for i := 0; i < 4; i++ {
		maxSfac[i] = 0
	}

	var rowInTable int
	if codInfo.BlockType == ShortType {
		rowInTable = 1
		partitionTable := nrOfSfbBlock[tableNumber][rowInTable][:]
		sfb := 0
		for partition := 0; partition < 4; partition++ {
			nrSfb := partitionTable[partition] / 3
			for i := 0; i < nrSfb; i, sfb = i+1, sfb+1 {
				for window := 0; window < 3; window++ {
					if scalefac[sfb*3+window] > maxSfac[partition] {
						maxSfac[partition] = scalefac[sfb*3+window]
					}
				}
			}
		}
	} else {
		rowInTable = 0
		partitionTable := nrOfSfbBlock[tableNumber][rowInTable][:]
		sfb := 0
		for partition := 0; partition < 4; partition++ {
			nrSfb := partitionTable[partition]
			for i := 0; i < nrSfb; i, sfb = i+1, sfb+1 {
				if scalefac[sfb] > maxSfac[partition] {
					maxSfac[partition] = scalefac[sfb]
				}
			}
		}
	}

	over := 0
	for partition := 0; partition < 4; partition++ {
		if maxSfac[partition] > maxRangeSfacTab[tableNumber][partition] {
			over++
		}
	}
	if over == 0 {
		// Since no bands have been over-amplified, we can set
		// scalefac_compress and slen[] for the formatter

		codInfo.SfbPartitionTable = nrOfSfbBlock[tableNumber][rowInTable][:]
		for partition := 0; partition < 4; partition++ {
			codInfo.Slen[partition] = log2Tab[maxSfac[partition]]
		}

		// set scalefac_compress
		slen1 := codInfo.Slen[0]
		slen2 := codInfo.Slen[1]
		slen3 := codInfo.Slen[2]
		slen4 := codInfo.Slen[3]

		switch tableNumber {
		case 0:
			codInfo.ScalefacCompress = (((slen1 * 5) + slen2) << 4) +
				(slen3 << 2) +
				slen4
		case 1:
			codInfo.ScalefacCompress = 400 + (((slen1 * 5) + slen2) << 2) +
				slen3
		case 2:
			codInfo.ScalefacCompress = 500 + (slen1 * 3) + slen2
		default:
			// ERRORF: intensity stereo not implemented yet
		}
	}
	if over == 0 {
		codInfo.Part2Length = 0
		for partition := 0; partition < 4; partition++ {
			codInfo.Part2Length +=
				codInfo.Slen[partition] * codInfo.SfbPartitionTable[partition]
		}
	}
	return over
}

// scaleBitcount dispatches to the MPEG-1 or MPEG-2 LSF scalefactor bit counter
// by mode_gr (takehiro.c:1318, scale_bitcount).
func (gfc *LameInternalFlags) scaleBitcount(codInfo *GrInfo) int {
	if gfc.Cfg.ModeGr == 2 {
		return gfc.mpeg1ScaleBitcount(codInfo)
	}
	return gfc.mpeg2ScaleBitcount(codInfo)
}

// huffmanInit precomputes the bv_scf big-value region split table for every
// even big-value count and installs the choose_table function pointer
// (takehiro.c:1334, huffman_init). It also wires the Huffman bit-length tables
// into ht[] (populateHuffmanLengths) so the count routines find ht[t].Hlen /
// Xlen / Linmax populated; in the C those are static initializers of ht[].
func (gfc *LameInternalFlags) huffmanInit() {
	populateHuffmanLengths()
	populateHuffmanCodeWords()

	// gfc->choose_table = choose_table_nonMMX (realized as the chooseTable
	// method; no MMX in the vendored config).

	for i := 2; i <= 576; i += 2 {
		scfbAnz := 0
		for {
			scfbAnz++
			if gfc.ScalefacBand.L[scfbAnz] >= i {
				break
			}
		}

		bvIndex := subdvTable[scfbAnz][0] // region0_count
		for gfc.ScalefacBand.L[bvIndex+1] > i {
			bvIndex--
		}

		if bvIndex < 0 {
			// this is an indication that everything is going to
			// be encoded as region0:  bigvalues < region0 < region1
			// so lets set region0, region1 to some value larger
			// than bigvalues
			bvIndex = subdvTable[scfbAnz][0]
		}

		gfc.SvQnt.BvScf[i-2] = int8(bvIndex)

		bvIndex = subdvTable[scfbAnz][1] // region1_count
		for gfc.ScalefacBand.L[bvIndex+int(gfc.SvQnt.BvScf[i-2])+2] > i {
			bvIndex--
		}

		if bvIndex < 0 {
			bvIndex = subdvTable[scfbAnz][1]
		}

		gfc.SvQnt.BvScf[i-1] = int8(bvIndex)
	}
}

// iMin is the C Min macro (machine.h): the smaller of two ints. Inlined as a
// helper because takehiro.c uses Min in several bit-count expressions.
func iMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
