// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Layer III VBR quantizer scalefactor-allocation tier — a 1:1 translation of
// the vendored LAME 3.100 encoder's libmp3lame/vbrquantize.c functions that turn
// the per-band leaf-kernel scalefactor estimates (the vbrquantize-leaf slice)
// into a granule's final global_gain / scalefac[] / subblock_gain[] /
// scalefac_scale / preflag side information and count the resulting bits:
//
//	algo_t struct + alloc_sf_f / find_sf_f dispatch (vbrquantize.c:43-54),
//	block_sf            (vbrquantize.c:394) — per-band sf survey,
//	quantize_x34        (vbrquantize.c:500) — quantize xr34 by the chosen sf,
//	set_subblock_gain   (vbrquantize.c:595) — short-block subblock_gain,
//	set_scalefacs       (vbrquantize.c:688) — scalefac[] from the sf deltas,
//	checkScalefactor    (vbrquantize.c:732) — the NDEBUG assert predicate,
//	short_block_constrain (vbrquantize.c:769) — short-block allocator,
//	long_block_constrain  (vbrquantize.c:847) — long-block allocator,
//	bitcount            (vbrquantize.c:984) — scale_bitcount wrapper,
//	quantizeAndCountBits (vbrquantize.c:999) — quantize + noquant_count_bits.
//
// Every ported function names its vbrquantize.c counterpart as file:line. The
// VBR_encode_frame driver and the global-stepsize / out-of-bits search that sit
// above these (tryGlobalStepsize .. outOfBitsStrategy, VBR_encode_frame) are a
// later slice; this slice owns the allocation + counting tier and the algo_t
// dispatch model the driver fills in.
//
// # The algo_t dispatch (vbrquantize.c:43-54)
//
// LAME's algo_t carries two function pointers:
//
//	typedef void    (*alloc_sf_f)(const algo_t *, const int *, const int *, int);
//	typedef uint8_t (*find_sf_f) (const FLOAT *, const FLOAT *, FLOAT, unsigned int, uint8_t);
//	struct algo_s { alloc_sf_f alloc; find_sf_f find; ... };
//
// VBR_encode_frame (the later driver slice) selects alloc = short/long_block_-
// constrain by block type and find = guess/find_scalefac_x34 by full_outer_loop.
// The Go port models them as the allocSfFunc / findSfFunc func-field types on
// algoT so the call sites (`that.find(...)`, `that.alloc(...)`) stay 1:1 with
// the C indirect calls. block_sf calls that.find; the driver calls that.alloc.
//
// # Floating-point parity
//
// This tier is mostly integer (the scalefactor algebra and the bit counts are
// integer-exact), but block_sf reads the leaf kernels' float results and
// quantize_x34 performs the same float32 sfpow34*xr34 product + k_34_4
// magic-float quantize as calc_sfb_noise_x34. Those float multiplies route
// through the //go:noinline vq* helpers (vbrquantize_fp_strict.go) so the
// mp3_strict build separately-rounds like the -ffp-contract=off cgo oracle. The
// integer allocators (set_subblock_gain / set_scalefacs / short/long_block_-
// constrain) are bit-identical in both builds.

// allocSfFunc is LAME's alloc_sf_f (vbrquantize.c:43): the per-block-type
// scalefactor allocator (short_block_constrain / long_block_constrain) that
// converts the block_sf survey (vbrsf / vbrsfmin / vbrmax) into the granule's
// global_gain, subblock_gain, scalefac, scalefac_scale and preflag side
// information. It is a method-shaped closure over the algoT so it mirrors the C
// `that->alloc(that, sfwork, vbrsfmin, vbrmax)` indirect call.
type allocSfFunc func(that *algoT, vbrsf []int, vbrsfmin []int, vbrmax int)

// findSfFunc is LAME's find_sf_f (vbrquantize.c:44): the per-band scalefactor
// finder (guess_scalefac_x34 / find_scalefac_x34) block_sf invokes for each
// scalefactor band via `that->find(...)`. Signature mirrors the C pointer type
// (xr, xr34, l3_xmin, bw, sf_min) -> uint8.
type findSfFunc func(xr, xr34 []float32, l3Xmin float32, bw uint, sfMin uint8) uint8

// algoT is LAME's struct algo_s / algo_t (vbrquantize.c:46-54): the per-granule
// VBR allocation context threaded through block_sf and the constrain
// allocators. It carries the alloc / find dispatch func fields, the original
// xr34 magnitudes, the encoder context, the granule being quantized, and the
// minimum-gain accumulators block_sf fills and the allocators read.
//
//	struct algo_s {
//	    alloc_sf_f alloc;
//	    find_sf_f  find;
//	    const FLOAT *xr34orig;
//	    lame_internal_flags *gfc;
//	    gr_info *cod_info;
//	    int     mingain_l;
//	    int     mingain_s[3];
//	};
//
// Xr34orig is the granule's |xr|^(3/4) line array (576 entries) the leaf
// kernels operate on; Gfc / CodInfo are the encoder context and the granule's
// gr_info. MingainL / MingainS[3] are the smallest allowable global_gain for
// the long block and the three short-block windows, set by block_sf from the
// per-band find_lowest_scalefac results and read by the allocators as the
// global_gain floor.
type algoT struct {
	alloc    allocSfFunc
	find     findSfFunc
	Xr34orig []float32 // const FLOAT *xr34orig
	Gfc      *LameInternalFlags
	CodInfo  *GrInfo
	MingainL int    // mingain_l
	MingainS [3]int // mingain_s[3]
}

// maxRangeShort is LAME's max_range_short[SBMAX_s*3] (vbrquantize.c:569): the
// per-band maximum scalefactor value for short blocks (15 for the first 18
// bands, 7 for the next 18, 0 for the last 3), used by short_block_constrain's
// overage computation and set_scalefacs' clamp.
var maxRangeShort = [SBMAXs * 3]uint8{
	15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	0, 0, 0,
}

// maxRangeLong is LAME's max_range_long[SBMAX_l] (vbrquantize.c:575): the
// per-band maximum scalefactor for MPEG-1 long blocks.
var maxRangeLong = [SBMAXl]uint8{
	15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 0,
}

// maxRangeLongLsfPretab is LAME's max_range_long_lsf_pretab[SBMAX_l]
// (vbrquantize.c:579): the per-band maximum scalefactor for MPEG-2/2.5 (LSF)
// long blocks with preemphasis, used when cfg->mode_gr != 2.
var maxRangeLongLsfPretab = [SBMAXl]uint8{
	7, 7, 7, 7, 7, 7, 3, 3, 3, 3, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
}

// blockSf surveys every scalefactor band of the granule, calling the find
// dispatch (guess/find_scalefac_x34) for each non-zero-energy band to pick a
// per-band scalefactor, recording the band minimum scalefactor (find_lowest_-
// scalefac) into vbrsfmin and the per-block minimum-gain floors into
// that.MingainL / MingainS, and returns the overall maximum scalefactor vbrmax
// (block_sf, vbrquantize.c:394). vbrsf / vbrsfmin must have length SFBMAX.
//
// 255 is the sentinel for a band with energy_above_cutoff false; if any in-range
// band reached < 255 (m_o), the 255 sentinels are rewritten to that maximum so
// the allocator does not see the out-of-band sentinel as a real scalefactor.
func blockSf(that *algoT, l3Xmin []float32, vbrsf []int, vbrsfmin []int) int {
	codInfo := that.CodInfo
	xr := codInfo.Xr[:]
	xr34Orig := that.Xr34orig
	width := codInfo.Width[:]
	energyAboveCutoff := codInfo.EnergyAboveCutoff[:]
	maxNonzeroCoeff := uint(codInfo.MaxNonzeroCoeff)
	var maxsf uint8 = 0
	sfb := 0
	mO := -1
	var j, i uint = 0, 0
	psymax := codInfo.Psymax

	that.MingainL = 0
	that.MingainS[0] = 0
	that.MingainS[1] = 0
	that.MingainS[2] = 0
	for j <= maxNonzeroCoeff {
		w := uint(width[sfb])
		m := maxNonzeroCoeff - j + 1
		l := w
		var m1, m2 uint8
		if l > m {
			l = m
		}
		maxXr34 := vecMaxC(xr34Orig[j:], l)

		m1 = findLowestScalefac(maxXr34)
		vbrsfmin[sfb] = int(m1)
		if that.MingainL < int(m1) {
			that.MingainL = int(m1)
		}
		if that.MingainS[i] < int(m1) {
			that.MingainS[i] = int(m1)
		}
		i++
		if i > 2 {
			i = 0
		}
		// mpeg2.5 at 8 kHz doesn't use all scalefactors, unused have width 2
		if sfb < psymax && w > 2 {
			if energyAboveCutoff[sfb] != 0 {
				m2 = that.find(xr[j:], xr34Orig[j:], l3Xmin[sfb], l, m1)
				if maxsf < m2 {
					maxsf = m2
				}
				if mO < int(m2) && m2 < 255 {
					mO = int(m2)
				}
			} else {
				m2 = 255
				maxsf = 255
			}
		} else {
			if maxsf < m1 {
				maxsf = m1
			}
			m2 = maxsf
		}
		vbrsf[sfb] = int(m2)
		sfb++
		j += w
	}
	for ; sfb < SFBMAX; sfb++ {
		vbrsf[sfb] = int(maxsf)
		vbrsfmin[sfb] = 0
	}
	if mO > -1 {
		maxsf = uint8(mO)
		for sfb = 0; sfb < SFBMAX; sfb++ {
			if vbrsf[sfb] == 255 {
				vbrsf[sfb] = mO
			}
		}
	}
	return int(maxsf)
}

// quantizeX34 quantizes the granule's xr34 magnitudes into cod_info.l3_enc
// using the scalefactors chosen by the allocator (the resolved per-band
// step-size sfac = global_gain - s drives sfpow34 = ipow20[sfac]), via the same
// k_34_4 TAKEHIRO_IEEE754_HACK magic-float quantize calc_sfb_noise_x34 used
// (quantize_x34, vbrquantize.c:500). The 4-wide body + switch tail match the C
// so the float32 sfpow34*xr34 product order is identical.
func quantizeX34(that *algoT) {
	var x [4]float64
	codInfo := that.CodInfo
	xr34Orig := that.Xr34orig
	var ifqstep int
	if codInfo.ScalefacScale == 0 {
		ifqstep = 2
	} else {
		ifqstep = 4
	}
	l3 := codInfo.L3Enc[:]
	var j, sfb uint = 0, 0
	maxNonzeroCoeff := uint(codInfo.MaxNonzeroCoeff)

	xrp := 0 // cursor into xr34Orig (the C advances xr34_orig)
	l3p := 0 // cursor into l3 (the C advances l3)

	for j <= maxNonzeroCoeff {
		var pre int
		if codInfo.Preflag != 0 {
			pre = pretab[sfb]
		}
		s := (codInfo.Scalefac[sfb]+pre)*ifqstep +
			codInfo.SubblockGain[codInfo.Window[sfb]]*8
		sfac := uint8(codInfo.GlobalGain - s)
		sfpow34 := ipow20[sfac]
		w := uint(codInfo.Width[sfb])
		m := maxNonzeroCoeff - j + 1

		j += w
		sfb++

		var ii uint
		if w <= m {
			ii = w
		} else {
			ii = m
		}
		remaining := ii & 0x03
		ii >>= 2

		for ii > 0 {
			ii--
			x[0] = float64(vqMulF(sfpow34, xr34Orig[xrp+0]))
			x[1] = float64(vqMulF(sfpow34, xr34Orig[xrp+1]))
			x[2] = float64(vqMulF(sfpow34, xr34Orig[xrp+2]))
			x[3] = float64(vqMulF(sfpow34, xr34Orig[xrp+3]))

			var ll [4]int
			k344(&x, &ll)
			l3[l3p+0] = ll[0]
			l3[l3p+1] = ll[1]
			l3[l3p+2] = ll[2]
			l3[l3p+3] = ll[3]

			l3p += 4
			xrp += 4
		}
		if remaining != 0 {
			var tmpL3 [4]int
			x[0], x[1], x[2], x[3] = 0, 0, 0, 0
			switch remaining {
			case 3:
				x[2] = float64(vqMulF(sfpow34, xr34Orig[xrp+2]))
				fallthrough
			case 2:
				x[1] = float64(vqMulF(sfpow34, xr34Orig[xrp+1]))
				fallthrough
			case 1:
				x[0] = float64(vqMulF(sfpow34, xr34Orig[xrp+0]))
			}

			k344(&x, &tmpL3)

			switch remaining {
			case 3:
				l3[l3p+2] = tmpL3[2]
				fallthrough
			case 2:
				l3[l3p+1] = tmpL3[1]
				fallthrough
			case 1:
				l3[l3p+0] = tmpL3[0]
			}

			l3p += int(remaining)
			xrp += int(remaining)
		}
	}
}

// setSubblockGain sets cod_info.subblock_gain[0..2] for a short block and
// rebases sf[] / global_gain so the per-window gains absorb as much of the
// scalefactor magnitude as the 3-bit subblock_gain field allows
// (set_subblock_gain, vbrquantize.c:595). sf[] (length SFBMAX) is the per-band
// `vbrsf - vbrmax` delta the allocator passes; it is updated in place. The
// computation is pure integer.
func setSubblockGain(codInfo *GrInfo, mingainS *[3]int, sf []int) {
	const maxrange1, maxrange2 = 15, 7
	var ifqstepShift int
	if codInfo.ScalefacScale == 0 {
		ifqstepShift = 1
	} else {
		ifqstepShift = 2
	}
	sbg := codInfo.SubblockGain[:]
	psymax := uint(codInfo.Psymax)
	psydiv := uint(18)
	var sbg0, sbg1, sbg2 int
	minSbg := 7

	if psydiv > psymax {
		psydiv = psymax
	}
	for i := uint(0); i < 3; i++ {
		maxsf1, maxsf2, minsf := 0, 0, 1000
		// see if we should use subblock gain
		var sfb uint
		for sfb = i; sfb < psydiv; sfb += 3 { // part 1
			v := -sf[sfb]
			if maxsf1 < v {
				maxsf1 = v
			}
			if minsf > v {
				minsf = v
			}
		}
		for ; sfb < SFBMAX; sfb += 3 { // part 2
			v := -sf[sfb]
			if maxsf2 < v {
				maxsf2 = v
			}
			if minsf > v {
				minsf = v
			}
		}

		// boost subblock gain as little as possible so we can reach maxsf1 with
		// scalefactors: 8*sbg >= maxsf1
		m1 := maxsf1 - (maxrange1 << ifqstepShift)
		m2 := maxsf2 - (maxrange2 << ifqstepShift)
		maxsf1 = imaxInt(m1, m2)

		if minsf > 0 {
			sbg[i] = minsf >> 3
		} else {
			sbg[i] = 0
		}
		if maxsf1 > 0 {
			a := sbg[i]
			b := (maxsf1 + 7) >> 3
			sbg[i] = imaxInt(a, b)
		}
		if sbg[i] > 0 && mingainS[i] > (codInfo.GlobalGain-sbg[i]*8) {
			sbg[i] = (codInfo.GlobalGain - mingainS[i]) >> 3
		}
		if sbg[i] > 7 {
			sbg[i] = 7
		}
		if minSbg > sbg[i] {
			minSbg = sbg[i]
		}
	}
	sbg0 = sbg[0] * 8
	sbg1 = sbg[1] * 8
	sbg2 = sbg[2] * 8
	for sfb := 0; sfb < SFBMAX; sfb += 3 {
		sf[sfb+0] += sbg0
		sf[sfb+1] += sbg1
		sf[sfb+2] += sbg2
	}
	if minSbg > 0 {
		for i := 0; i < 3; i++ {
			sbg[i] -= minSbg
		}
		codInfo.GlobalGain -= minSbg * 8
	}
}

// setScalefacs fills cod_info.scalefac[0..sfbmax-1] from the per-band sf[]
// deltas, rounding each up to the nearest representable scalefactor, clamping to
// the per-band max_range and to the minimum gain, and applying the preemphasis
// table when preflag is set (set_scalefacs, vbrquantize.c:688). sf[] (length
// SFBMAX) is updated in place by the preflag pass. Pure integer.
func setScalefacs(codInfo *GrInfo, vbrsfmin []int, sf []int, maxRange []uint8) {
	var ifqstep, ifqstepShift int
	if codInfo.ScalefacScale == 0 {
		ifqstep, ifqstepShift = 2, 1
	} else {
		ifqstep, ifqstepShift = 4, 2
	}
	scalefac := codInfo.Scalefac[:]
	sfbmax := codInfo.Sfbmax
	sbg := codInfo.SubblockGain[:]
	window := codInfo.Window[:]
	preflag := codInfo.Preflag

	var sfb int
	if preflag != 0 {
		for sfb = 11; sfb < sfbmax; sfb++ {
			sf[sfb] += pretab[sfb] * ifqstep
		}
	}
	for sfb = 0; sfb < sfbmax; sfb++ {
		var pre int
		if preflag != 0 {
			pre = pretab[sfb]
		}
		gain := codInfo.GlobalGain - (sbg[window[sfb]] * 8) - (pre * ifqstep)

		if sf[sfb] < 0 {
			m := gain - vbrsfmin[sfb]
			// ifqstep*scalefac >= -sf[sfb], so round UP
			scalefac[sfb] = (ifqstep - 1 - sf[sfb]) >> ifqstepShift

			if scalefac[sfb] > int(maxRange[sfb]) {
				scalefac[sfb] = int(maxRange[sfb])
			}
			if scalefac[sfb] > 0 && (scalefac[sfb]<<ifqstepShift) > m {
				scalefac[sfb] = m >> ifqstepShift
			}
		} else {
			scalefac[sfb] = 0
		}
	}
	for ; sfb < SFBMAX; sfb++ {
		scalefac[sfb] = 0 // sfb21
	}
}

// checkScalefactor is LAME's NDEBUG-guarded checkScalefactor assert predicate
// (vbrquantize.c:732): it returns true iff every band's resolved step-size
// global_gain - s stays at or above the band minimum vbrsfmin (i.e. no band was
// over-amplified). The allocators assert(checkScalefactor(...)); the Go port
// runs the predicate in checkScalefactorOrPanic (parityhooks) so the parity
// suite can assert the C and Go agree, but the production path mirrors the C's
// NDEBUG behaviour (the assert is compiled out — the allocators do not call it).
func checkScalefactor(codInfo *GrInfo, vbrsfmin []int) bool {
	var ifqstep int
	if codInfo.ScalefacScale == 0 {
		ifqstep = 2
	} else {
		ifqstep = 4
	}
	for sfb := 0; sfb < codInfo.Psymax; sfb++ {
		var pre int
		if codInfo.Preflag != 0 {
			pre = pretab[sfb]
		}
		s := (codInfo.Scalefac[sfb]+pre)*ifqstep +
			codInfo.SubblockGain[codInfo.Window[sfb]]*8
		if (codInfo.GlobalGain - s) < vbrsfmin[sfb] {
			return false
		}
	}
	return true
}

// shortBlockConstrain converts the block_sf survey (vbrsf / vbrsfmin / vbrmax)
// into a short-block granule's side information: it computes the global_gain by
// minimising scalefactor overage, decides scalefac_scale, then calls
// setSubblockGain + setScalefacs to fill subblock_gain[] and scalefac[]
// (short_block_constrain, vbrquantize.c:769). It is the alloc dispatch for
// SHORT_TYPE granules. Pure integer.
func shortBlockConstrain(that *algoT, vbrsf []int, vbrsfmin []int, vbrmax int) {
	codInfo := that.CodInfo
	cfg := &that.Gfc.Cfg
	maxminsfb := that.MingainL
	var mover, maxover0, maxover1, delta int
	psymax := codInfo.Psymax

	for sfb := 0; sfb < psymax; sfb++ {
		v := vbrmax - vbrsf[sfb]
		if delta < v {
			delta = v
		}
		v0 := v - (4*14 + 2*int(maxRangeShort[sfb]))
		v1 := v - (4*14 + 4*int(maxRangeShort[sfb]))
		if maxover0 < v0 {
			maxover0 = v0
		}
		if maxover1 < v1 {
			maxover1 = v1
		}
	}
	if cfg.NoiseShaping == 2 {
		// allow scalefac_scale=1
		mover = iminInt(maxover0, maxover1)
	} else {
		mover = maxover0
	}
	if delta > mover {
		delta = mover
	}
	vbrmax -= delta
	maxover0 -= mover
	maxover1 -= mover

	if maxover0 == 0 {
		codInfo.ScalefacScale = 0
	} else if maxover1 == 0 {
		codInfo.ScalefacScale = 1
	}
	if vbrmax < maxminsfb {
		vbrmax = maxminsfb
	}
	codInfo.GlobalGain = vbrmax

	if codInfo.GlobalGain < 0 {
		codInfo.GlobalGain = 0
	} else if codInfo.GlobalGain > 255 {
		codInfo.GlobalGain = 255
	}
	var sfTemp [SFBMAX]int
	for sfb := 0; sfb < SFBMAX; sfb++ {
		sfTemp[sfb] = vbrsf[sfb] - vbrmax
	}
	setSubblockGain(codInfo, &that.MingainS, sfTemp[:])
	setScalefacs(codInfo, vbrsfmin, sfTemp[:], maxRangeShort[:])
	// assert(checkScalefactor(cod_info, vbrsfmin)) — NDEBUG-compiled-out in C.
}

// longBlockConstrain converts the block_sf survey into a long-block granule's
// side information, deciding among the four (scalefac_scale, preflag)
// combinations by minimising overage and applying preemphasis where it keeps the
// step-size above the band minimum, then calls setScalefacs
// (long_block_constrain, vbrquantize.c:847). It is the alloc dispatch for
// non-SHORT_TYPE granules. Pure integer.
func longBlockConstrain(that *algoT, vbrsf []int, vbrsfmin []int, vbrmax int) {
	codInfo := that.CodInfo
	cfg := &that.Gfc.Cfg
	maxminsfb := that.MingainL
	var maxover0, maxover1, maxover0p, maxover1p, mover, delta int
	vm0p, vm1p := 1, 1
	psymax := codInfo.Psymax

	var maxRangep []uint8
	if cfg.ModeGr == 2 {
		maxRangep = maxRangeLong[:]
	} else {
		maxRangep = maxRangeLongLsfPretab[:]
	}

	maxover0 = 0
	maxover1 = 0
	maxover0p = 0 // pretab
	maxover1p = 0 // pretab

	for sfb := 0; sfb < psymax; sfb++ {
		v := vbrmax - vbrsf[sfb]
		if delta < v {
			delta = v
		}
		v0 := v - 2*int(maxRangeLong[sfb])
		v1 := v - 4*int(maxRangeLong[sfb])
		v0p := v - 2*(int(maxRangep[sfb])+pretab[sfb])
		v1p := v - 4*(int(maxRangep[sfb])+pretab[sfb])
		if maxover0 < v0 {
			maxover0 = v0
		}
		if maxover1 < v1 {
			maxover1 = v1
		}
		if maxover0p < v0p {
			maxover0p = v0p
		}
		if maxover1p < v1p {
			maxover1p = v1p
		}
	}
	if vm0p == 1 {
		gain := vbrmax - maxover0p
		if gain < maxminsfb {
			gain = maxminsfb
		}
		for sfb := 0; sfb < psymax; sfb++ {
			a := (gain - vbrsfmin[sfb]) - 2*pretab[sfb]
			if a <= 0 {
				vm0p = 0
				vm1p = 0
				break
			}
		}
	}
	if vm1p == 1 {
		gain := vbrmax - maxover1p
		if gain < maxminsfb {
			gain = maxminsfb
		}
		for sfb := 0; sfb < psymax; sfb++ {
			b := (gain - vbrsfmin[sfb]) - 4*pretab[sfb]
			if b <= 0 {
				vm1p = 0
				break
			}
		}
	}
	if vm0p == 0 {
		maxover0p = maxover0
	}
	if vm1p == 0 {
		maxover1p = maxover1
	}
	if cfg.NoiseShaping != 2 {
		maxover1 = maxover0
		maxover1p = maxover0p
	}
	mover = iminInt(maxover0, maxover0p)
	mover = iminInt(mover, maxover1)
	mover = iminInt(mover, maxover1p)

	if delta > mover {
		delta = mover
	}
	vbrmax -= delta
	if vbrmax < maxminsfb {
		vbrmax = maxminsfb
	}
	maxover0 -= mover
	maxover0p -= mover
	maxover1 -= mover
	maxover1p -= mover

	if maxover0 == 0 {
		codInfo.ScalefacScale = 0
		codInfo.Preflag = 0
		maxRangep = maxRangeLong[:]
	} else if maxover0p == 0 {
		codInfo.ScalefacScale = 0
		codInfo.Preflag = 1
	} else if maxover1 == 0 {
		codInfo.ScalefacScale = 1
		codInfo.Preflag = 0
		maxRangep = maxRangeLong[:]
	} else if maxover1p == 0 {
		codInfo.ScalefacScale = 1
		codInfo.Preflag = 1
	} else {
		// assert(0): this should not happen
		panic("nativemp3: long_block_constrain: no valid (scalefac_scale, preflag) combination")
	}
	codInfo.GlobalGain = vbrmax
	if codInfo.GlobalGain < 0 {
		codInfo.GlobalGain = 0
	} else if codInfo.GlobalGain > 255 {
		codInfo.GlobalGain = 255
	}
	var sfTemp [SFBMAX]int
	for sfb := 0; sfb < SFBMAX; sfb++ {
		sfTemp[sfb] = vbrsf[sfb] - vbrmax
	}
	setScalefacs(codInfo, vbrsfmin, sfTemp[:], maxRangep)
	// assert(checkScalefactor(cod_info, vbrsfmin)) — NDEBUG-compiled-out in C.
}

// bitcount runs scale_bitcount on the granule and panics if it reports
// over-amplification (bitcount, vbrquantize.c:984). The C ERRORFs + exit(-1)s in
// that "should not happen" case; the Go port panics with the mp3:-prefixed
// message so the failure is visible rather than silently miscounting.
func bitcount(that *algoT) {
	rc := that.Gfc.scaleBitcount(that.CodInfo)
	if rc == 0 {
		return
	}
	panic("mp3: internal error in VBR scalefactor selection (scale_bitcount reported over-amplification)")
}

// quantizeAndCountBits quantizes the granule (quantizeX34) and counts the
// resulting Huffman + scalefactor bits via noquant_count_bits, storing the total
// into cod_info.part2_3_length and returning it (quantizeAndCountBits,
// vbrquantize.c:999).
func quantizeAndCountBits(that *algoT) int {
	quantizeX34(that)
	that.CodInfo.Part23Length = that.Gfc.noquantCountBits(that.CodInfo, nil)
	return that.CodInfo.Part23Length
}

// imaxInt / iminInt are the LAME Max / Min int macros (util.h:91-92). Inlined as
// the per-call-site ternaries the C uses; named helpers here only because the
// allocators use them several times.
func imaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func iminInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
