// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// MP3 quantization iteration loop — the control core of the vendored LAME 3.100
// encoder (libmp3lame/quantize.c, copyright Mark Taylor / Takehiro Tominaga /
// Robert Hegemann / Gabriel Bouvigne). This is a 1:1 translation of the
// constant-bitrate (CBR) and average-bitrate (ABR) iteration loops and the
// outer/inner search machinery they drive: from a granule's MDCT lines
// (gr_info.xr) and its psychoacoustic distortion budget (calc_xmin's l3_xmin),
// it finds the global_gain / scalefactors / Huffman tables that pack the audio
// into the target bit budget with the least audible noise, then formats the
// frame through the bitstream slice.
//
// # Scope of this slice ("quantize-iteration", CBR + ABR)
//
// ms_convert (quantize.c:48), init_xrpow_core_c / init_xrpow (quantize.c:72/110),
// psfb21_analogsilence (quantize.c:159), init_outer_loop (quantize.c:226),
// bin_search_StepSize (quantize.c:367), trancate_smallspectrums (quantize.c:454),
// loop_break (quantize.c:540), penalties / get_klemm_noise / quant_compare
// (quantize.c:568/574/585), amp_scalefac_bands (quantize.c:720),
// inc_scalefac_scale (quantize.c:808), inc_subblock_gain (quantize.c:847),
// balance_noise (quantize.c:940), outer_loop (quantize.c:1010),
// iteration_finish_one (quantize.c:1213), calc_target_bits (quantize.c:1767),
// ABR_iteration_loop (quantize.c:1900) and CBR_iteration_loop (quantize.c:1988).
//
// get_framebits is ported here too (called by the VBR prepare routines and by
// calc_target_bits via ResvFrameBegin). The VBR loops themselves —
// VBR_old_prepare / VBR_old_iteration_loop / VBR_new_prepare /
// VBR_new_iteration_loop / VBR_encode_granule / bitpressure_strategy — live in
// the sibling quantize_encode_vbr.go (the new-VBR path drives VBR_encode_frame,
// the vbrquantize.c orchestrator already ported in vbrquantize_frame.go). The
// dispatcher (frame_encode.go) routes cfg.vbr==vbr_off to CBR, vbr_abr to ABR,
// vbr_mtrh/vbr_mt to VBR-new and vbr_rh to VBR-old.
//
// # Floating-point parity
//
// LAME's FLOAT is float32 (machine.h). quantize.c mixes float32 and double
// arithmetic; every bit-exact-path float32 mul/add goes through the //go:noinline
// qe* helpers (quantize_encode_fp_strict.go), and the double sub-expressions
// (penalties, res_factor, …) through their qe* double helpers, so the mp3_strict
// build separately-rounds, matching the -ffp-contract=off cgo oracle. The
// ifqstep34 / IPOW20 scalings of xrpow are float32 *= double-const, routed the
// same way. EQ(a,b) is the machine.h relative-epsilon macro (fabs, double).
// Every ported function names its quantize.c counterpart as file:line.

import "math"

// sqrt2HalfConst is `(FLOAT)(SQRT2 * 0.5)`, the float32 constant ms_convert
// scales the L/R sum/difference by (quantize.c:56). SQRT2 (util.h:79,
// 1.41421356237309504880) and 0.5 are double; the product is folded in double
// and narrowed once to float32 by the (FLOAT) cast, so it is written as a
// single-rounded float32 constant expression (matching the C's folded cast),
// NOT a runtime float32 multiply.
const sqrt2HalfConst = float32(1.41421356237309504880 * 0.5)

// ifqstep34Scale0 / ifqstep34Scale1 are amp_scalefac_bands /
// inc_scalefac_scale's ifqstep34 = 2**(.75*.5) / 2**(.75*1) (quantize.c:730/733).
// The C writes them as decimal double literals; the FLOAT *= ifqstep34 product
// promotes the FLOAT xrpow to double, multiplies in double and narrows on store,
// so the constants are carried at full double precision and the narrow happens
// in qeMulDConst.
const (
	ifqstep34Scale0 = 1.29683955465100964055 // 2**(.75*.5)
	ifqstep34Scale1 = 1.68179283050742922612 // 2**(.75*1)
)

// eqFloat is LAME's EQ(a,b) macro (machine.h:169): a relative-epsilon float
// comparison using the C double fabs. trancate_smallspectrums uses EQ/NEQ on
// FLOAT operands (promoted to double for fabs).
func eqFloat(a, b float32) bool {
	fa := math.Abs(float64(a))
	fb := math.Abs(float64(b))
	if fa > fb {
		return math.Abs(float64(a)-float64(b)) <= fa*float64(float32(1e-6))
	}
	return math.Abs(float64(a)-float64(b)) <= fb*float64(float32(1e-6))
}

// qeMulDConst returns the FLOAT product x * d where d is a double-precision
// constant (the ifqstep34 / amp scalings). C promotes the FLOAT x to double,
// multiplies in double and narrows on store. Defined here (not in the fp_strict
// file) because it is a fixed double-const multiply; the strict/default
// distinction is irrelevant — a FLOAT*=double-const narrows once regardless of
// FMA (there is no third addend to fuse).
func qeMulDConst(x float32, d float64) float32 { return float32(float64(x) * d) }

// msConvert converts a granule's two channels from L/R to mid/side in place
// (ms_convert, quantize.c:48): mid = (l+r)*SQRT2*0.5, side = (l-r)*SQRT2*0.5.
func msConvert(l3Side *IIISideInfo, gr int) {
	for i := 0; i < 576; i++ {
		l := l3Side.Tt[gr][0].Xr[i]
		r := l3Side.Tt[gr][1].Xr[i]
		l3Side.Tt[gr][0].Xr[i] = qeMul(qeAdd(l, r), sqrt2HalfConst)
		l3Side.Tt[gr][1].Xr[i] = qeMul(qeSub(l, r), sqrt2HalfConst)
	}
}

// initXrpowCoreC computes xrpow[i] = |xr[i]|^(3/4) for i in [0,upper], tracking
// the sum of |xr| and the per-granule xrpow_max (init_xrpow_core_c,
// quantize.c:72). tmp = fabs(xr[i]) narrows to FLOAT; *sum += tmp is float32;
// xrpow[i] = sqrt(tmp*sqrt(tmp)) is the double chain narrowed to FLOAT.
func initXrpowCoreC(codInfo *GrInfo, xrpow []float32, upper int) float32 {
	var sum float32
	for i := 0; i <= upper; i++ {
		tmp := float32(math.Abs(float64(codInfo.Xr[i]))) // FLOAT tmp = fabs(xr[i])
		sum = qeAdd(sum, tmp)
		xrpow[i] = qeXrpowCore(tmp)
		if xrpow[i] > codInfo.XrpowMax {
			codInfo.XrpowMax = xrpow[i]
		}
	}
	return sum
}

// initXrpow initialises xrpow from the granule's MDCT lines and returns 1 if
// there is energy to quantize, else 0 (init_xrpow, quantize.c:110). It zeroes
// the tail past max_nonzero_coeff, fills xrpow via init_xrpow_core, and (when
// non-silent) seeds the substep-shaping pseudohalf flags; a silent granule
// zeroes l3_enc and returns 0.
func (gfc *LameInternalFlags) initXrpow(codInfo *GrInfo, xrpow []float32) int {
	upper := codInfo.MaxNonzeroCoeff
	codInfo.XrpowMax = 0

	// memset(&xrpow[upper], 0, (576 - upper) * sizeof(xrpow[0]))
	for i := upper; i < 576; i++ {
		xrpow[i] = 0
	}

	sum := initXrpowCoreC(codInfo, xrpow, upper)

	// return 1 if we have something to quantize, else 0
	if sum > float32(1e-20) {
		j := 0
		if gfc.SvQnt.SubstepShaping&2 != 0 {
			j = 1
		}
		for i := 0; i < codInfo.Psymax; i++ {
			gfc.SvQnt.Pseudohalf[i] = j
		}
		return 1
	}

	for i := 0; i < 576; i++ {
		codInfo.L3Enc[i] = 0
	}
	return 0
}

// psfb21Analogsilence zeroes sub-ATH coefficients in the partitioned sfb21 (long
// blocks) or sfb12 (short blocks) from top to bottom, stopping at the first
// coefficient above the ATH (psfb21_analogsilence, quantize.c:159). Gabriel
// Bouvigne feb/apr 2003.
func (gfc *LameInternalFlags) psfb21Analogsilence(codInfo *GrInfo) {
	ath := gfc.ATH
	xr := codInfo.Xr[:]

	if codInfo.BlockType != ShortType {
		// NORM, START or STOP type, but not SHORT blocks
		stop := false
		for gsfb := PSFB21 - 1; gsfb >= 0 && !stop; gsfb-- {
			start := gfc.ScalefacBand.Psfb21[gsfb]
			end := gfc.ScalefacBand.Psfb21[gsfb+1]
			ath21 := athAdjust(ath.AdjustFactor, ath.Psfb21[gsfb], ath.Floor, 0)

			if gfc.SvQnt.Longfact[21] > float32(1e-12) {
				ath21 = qeMul(ath21, gfc.SvQnt.Longfact[21])
			}

			for j := end - 1; j >= start; j-- {
				if math.Abs(float64(xr[j])) < float64(ath21) {
					xr[j] = 0
				} else {
					stop = true
					break
				}
			}
		}
	} else {
		// note: short blocks coeffs are reordered
		for block := 0; block < 3; block++ {
			stop := false
			for gsfb := PSFB12 - 1; gsfb >= 0 && !stop; gsfb-- {
				start := gfc.ScalefacBand.S[12]*3 +
					(gfc.ScalefacBand.S[13]-gfc.ScalefacBand.S[12])*block +
					(gfc.ScalefacBand.Psfb12[gsfb] - gfc.ScalefacBand.Psfb12[0])
				end := start + (gfc.ScalefacBand.Psfb12[gsfb+1] - gfc.ScalefacBand.Psfb12[gsfb])
				ath12 := athAdjust(ath.AdjustFactor, ath.Psfb12[gsfb], ath.Floor, 0)

				if gfc.SvQnt.Shortfact[12] > float32(1e-12) {
					ath12 = qeMul(ath12, gfc.SvQnt.Shortfact[12])
				}

				for j := end - 1; j >= start; j-- {
					if math.Abs(float64(xr[j])) < float64(ath12) {
						xr[j] = 0
					} else {
						stop = true
						break
					}
				}
			}
		}
	}
}

// initOuterLoop resets a granule's cod_info to fresh scalefactors / block
// geometry before the iteration loop, reordering short-block coefficients and
// computing the per-band widths / window indices (init_outer_loop,
// quantize.c:226). mt 6/99.
func (gfc *LameInternalFlags) initOuterLoop(codInfo *GrInfo) {
	cfg := &gfc.Cfg

	// initialize fresh cod_info
	codInfo.Part23Length = 0
	codInfo.BigValues = 0
	codInfo.Count1 = 0
	codInfo.GlobalGain = 210
	codInfo.ScalefacCompress = 0
	// mixed_block_flag, block_type was set in psymodel.c
	codInfo.TableSelect[0] = 0
	codInfo.TableSelect[1] = 0
	codInfo.TableSelect[2] = 0
	codInfo.SubblockGain[0] = 0
	codInfo.SubblockGain[1] = 0
	codInfo.SubblockGain[2] = 0
	codInfo.SubblockGain[3] = 0 // this one is always 0
	codInfo.Region0Count = 0
	codInfo.Region1Count = 0
	codInfo.Preflag = 0
	codInfo.ScalefacScale = 0
	codInfo.Count1tableSelect = 0
	codInfo.Part2Length = 0
	if cfg.SamplerateOut <= 8000 {
		codInfo.SfbLmax = 17
		codInfo.SfbSmin = 9
		codInfo.PsyLmax = 17
	} else {
		codInfo.SfbLmax = SBPSYl
		codInfo.SfbSmin = SBPSYs
		if gfc.SvQnt.Sfb21Extra != 0 {
			codInfo.PsyLmax = SBMAXl
		} else {
			codInfo.PsyLmax = SBPSYl
		}
	}
	codInfo.Psymax = codInfo.PsyLmax
	codInfo.Sfbmax = codInfo.SfbLmax
	codInfo.Sfbdivide = 11
	for sfb := 0; sfb < SBMAXl; sfb++ {
		codInfo.Width[sfb] = gfc.ScalefacBand.L[sfb+1] - gfc.ScalefacBand.L[sfb]
		codInfo.Window[sfb] = 3 // which is always 0.
	}
	if codInfo.BlockType == ShortType {
		var ixwork [576]float32

		codInfo.SfbSmin = 0
		codInfo.SfbLmax = 0
		if codInfo.MixedBlockFlag != 0 {
			// MPEG-1:     sfbs 0-7 long block, 3-12 short blocks
			// MPEG-2(.5): sfbs 0-5 long block, 3-12 short blocks
			codInfo.SfbSmin = 3
			codInfo.SfbLmax = cfg.ModeGr*2 + 4
		}
		if cfg.SamplerateOut <= 8000 {
			codInfo.Psymax = codInfo.SfbLmax + 3*(9-codInfo.SfbSmin)
			codInfo.Sfbmax = codInfo.SfbLmax + 3*(9-codInfo.SfbSmin)
		} else {
			sbmaxsOrPsy := SBPSYs
			if gfc.SvQnt.Sfb21Extra != 0 {
				sbmaxsOrPsy = SBMAXs
			}
			codInfo.Psymax = codInfo.SfbLmax + 3*(sbmaxsOrPsy-codInfo.SfbSmin)
			codInfo.Sfbmax = codInfo.SfbLmax + 3*(SBPSYs-codInfo.SfbSmin)
		}
		codInfo.Sfbdivide = codInfo.Sfbmax - 18
		codInfo.PsyLmax = codInfo.SfbLmax

		// re-order the short blocks, for more efficient encoding below.
		// By Takehiro TOMINAGA. ix = &cod_info->xr[scalefac_band.l[sfb_lmax]].
		ixBase := gfc.ScalefacBand.L[codInfo.SfbLmax]
		copy(ixwork[:], codInfo.Xr[:576])
		ixPos := ixBase
		for sfb := codInfo.SfbSmin; sfb < SBMAXs; sfb++ {
			start := gfc.ScalefacBand.S[sfb]
			end := gfc.ScalefacBand.S[sfb+1]
			for window := 0; window < 3; window++ {
				for l := start; l < end; l++ {
					codInfo.Xr[ixPos] = ixwork[3*l+window]
					ixPos++
				}
			}
		}

		j := codInfo.SfbLmax
		for sfb := codInfo.SfbSmin; sfb < SBMAXs; sfb++ {
			w := gfc.ScalefacBand.S[sfb+1] - gfc.ScalefacBand.S[sfb]
			codInfo.Width[j] = w
			codInfo.Width[j+1] = w
			codInfo.Width[j+2] = w
			codInfo.Window[j] = 0
			codInfo.Window[j+1] = 1
			codInfo.Window[j+2] = 2
			j += 3
		}
	}

	codInfo.Count1bits = 0
	codInfo.SfbPartitionTable = nrOfSfbBlock[0][0][:]
	codInfo.Slen[0] = 0
	codInfo.Slen[1] = 0
	codInfo.Slen[2] = 0
	codInfo.Slen[3] = 0

	codInfo.MaxNonzeroCoeff = 575

	// fresh scalefactors are all zero
	for i := range codInfo.Scalefac {
		codInfo.Scalefac[i] = 0
	}

	if cfg.Vbr != vbrMt && cfg.Vbr != vbrMtrh && cfg.Vbr != vbrAbr && cfg.Vbr != vbrOff {
		gfc.psfb21Analogsilence(codInfo)
	}
}

// binSearchStepSize finds a starting quantizer step size (global_gain) for
// outer_loop by binary search so count_bits is near desired_rate
// (bin_search_StepSize, quantize.c:367). It seeds from the channel's previous
// OldValue / CurrentStep, narrows the step on each direction reversal, then
// nudges global_gain up while the bit count exceeds the target.
func (gfc *LameInternalFlags) binSearchStepSize(codInfo *GrInfo, desiredRate, ch int, xrpow []float32) int {
	// binsearch directions
	const (
		binsearchNone = 0
		binsearchUp   = 1
		binsearchDown = 2
	)

	currentStep := gfc.SvQnt.CurrentStep[ch]
	flagGoneOver := 0
	start := gfc.SvQnt.OldValue[ch]
	direction := binsearchNone
	codInfo.GlobalGain = start
	desiredRate -= codInfo.Part2Length

	var nBits int
	for {
		nBits = gfc.countBits(xrpow, codInfo, nil)

		if currentStep == 1 || nBits == desiredRate {
			break // nothing to adjust anymore
		}

		var step int
		if nBits > desiredRate {
			// increase Quantize_StepSize
			if direction == binsearchDown {
				flagGoneOver = 1
			}
			if flagGoneOver != 0 {
				currentStep /= 2
			}
			direction = binsearchUp
			step = currentStep
		} else {
			// decrease Quantize_StepSize
			if direction == binsearchUp {
				flagGoneOver = 1
			}
			if flagGoneOver != 0 {
				currentStep /= 2
			}
			direction = binsearchDown
			step = -currentStep
		}
		codInfo.GlobalGain += step
		if codInfo.GlobalGain < 0 {
			codInfo.GlobalGain = 0
			flagGoneOver = 1
		}
		if codInfo.GlobalGain > 255 {
			codInfo.GlobalGain = 255
			flagGoneOver = 1
		}
	}

	for nBits > desiredRate && codInfo.GlobalGain < 255 {
		codInfo.GlobalGain++
		nBits = gfc.countBits(xrpow, codInfo, nil)
	}
	if start-codInfo.GlobalGain >= 4 {
		gfc.SvQnt.CurrentStep[ch] = 4
	} else {
		gfc.SvQnt.CurrentStep[ch] = 2
	}
	gfc.SvQnt.OldValue[ch] = codInfo.GlobalGain
	codInfo.Part23Length = nBits
	return nBits
}

// trancateSmallspectrums truncates the smallest spectral coefficients to zero
// where the noise threshold allows, then re-counts the granule's bits
// (trancate_smallspectrums, quantize.c:454). Takehiro TOMINAGA 2002-07-21. It is
// only active when substep_shaping requests it.
func (gfc *LameInternalFlags) trancateSmallspectrums(gi *GrInfo, l3Xmin []float32, work []float32) {
	var distort [SFBMAX]float32
	var dummy CalcNoiseResult

	if (gfc.SvQnt.SubstepShaping&4 == 0 && gi.BlockType == ShortType) ||
		gfc.SvQnt.SubstepShaping&0x80 != 0 {
		return
	}
	calcNoise(gi, l3Xmin, distort[:], &dummy, nil)
	for j := 0; j < 576; j++ {
		var xr float32
		if gi.L3Enc[j] != 0 {
			xr = float32(math.Abs(float64(gi.Xr[j])))
		}
		work[j] = xr
	}

	j := 0
	sfb := 8
	if gi.BlockType == ShortType {
		sfb = 6
	}
	for {
		width := gi.Width[sfb]
		j += width
		if distort[sfb] >= 1.0 {
			if sfb++; sfb < gi.Psymax {
				continue
			}
			break
		}

		// qsort(&work[j-width], width, ...) ascending.
		sortFloat32Asc(work[j-width : j])
		if eqFloat(work[j-1], 0.0) {
			if sfb++; sfb < gi.Psymax {
				continue // all zero sfb
			}
			break
		}

		// allowedNoise = (1.0 - distort[sfb]) * l3_xmin[sfb] — FLOAT.
		allowedNoise := qeMul(qeSub(1.0, distort[sfb]), l3Xmin[sfb])
		trancateThreshold := float32(0.0)
		start := 0
		for {
			var nsame int
			for nsame = 1; start+nsame < width; nsame++ {
				if neqFloat(work[start+j-width], work[start+j+nsame-width]) {
					break
				}
			}

			// noise = work[..]^2 * nsame — FLOAT.
			noise := qeMul(qeMul(work[start+j-width], work[start+j-width]), float32(nsame))
			if allowedNoise < noise {
				if start != 0 {
					trancateThreshold = work[start+j-width-1]
				}
				break
			}
			allowedNoise = qeSub(allowedNoise, noise)
			start += nsame
			if start >= width {
				break
			}
		}
		if eqFloat(trancateThreshold, 0.0) {
			if sfb++; sfb < gi.Psymax {
				continue
			}
			break
		}

		for {
			if math.Abs(float64(gi.Xr[j-width])) <= float64(trancateThreshold) {
				gi.L3Enc[j-width] = 0
			}
			width--
			if width <= 0 {
				break
			}
		}

		if sfb++; sfb >= gi.Psymax {
			break
		}
	}

	gi.Part23Length = gfc.noquantCountBits(gi, nil)
}

// loopBreak returns 0 if some scalefactor band has not been amplified, else 1
// (loop_break, quantize.c:540).
func loopBreak(codInfo *GrInfo) int {
	for sfb := 0; sfb < codInfo.Sfbmax; sfb++ {
		if codInfo.Scalefac[sfb]+codInfo.SubblockGain[codInfo.Window[sfb]] == 0 {
			return 0
		}
	}
	return 1
}

// getKlemmNoise returns the Klemm perceptual-noise sum over a granule's bands
// (get_klemm_noise, quantize.c:574), used by quant_compare mode 8.
func getKlemmNoise(distort []float32, gi *GrInfo) float64 {
	klemmNoise := 1e-37
	for sfb := 0; sfb < gi.Psymax; sfb++ {
		klemmNoise = qeKlemmAcc(klemmNoise, qePenalties(float64(distort[sfb])))
	}
	if 1e-20 > klemmNoise {
		return 1e-20
	}
	return klemmNoise
}

// quantCompare decides whether the candidate quantization (calc) is better than
// the best so far (best), under the selected quant_comp comparison mode
// (quant_compare, quantize.c:585). distort / gi feed mode 8's Klemm noise.
func quantCompare(quantComp int, best *CalcNoiseResult, calc *CalcNoiseResult, gi *GrInfo, distort []float32) int {
	var better bool

	switch quantComp {
	default:
		fallthrough
	case 9:
		if best.OverCount > 0 {
			// there are distorted sfb
			better = calc.OverSSD <= best.OverSSD
			if calc.OverSSD == best.OverSSD {
				better = calc.Bits < best.Bits
			}
		} else {
			// no distorted sfb
			better = calc.MaxNoise < 0 &&
				(calc.MaxNoise*10+float32(calc.Bits)) <= (best.MaxNoise*10+float32(best.Bits))
		}
	case 0:
		better = calc.OverCount < best.OverCount ||
			(calc.OverCount == best.OverCount && calc.OverNoise < best.OverNoise) ||
			(calc.OverCount == best.OverCount &&
				eqFloat(calc.OverNoise, best.OverNoise) && calc.TotNoise < best.TotNoise)
	case 8:
		calc.MaxNoise = float32(getKlemmNoise(distort, gi))
		fallthrough
	case 1:
		better = calc.MaxNoise < best.MaxNoise
	case 2:
		better = calc.TotNoise < best.TotNoise
	case 3:
		better = (calc.TotNoise < best.TotNoise) && (calc.MaxNoise < best.MaxNoise)
	case 4:
		better = (calc.MaxNoise <= 0.0 && best.MaxNoise > 0.2) ||
			(calc.MaxNoise <= 0.0 &&
				best.MaxNoise < 0.0 &&
				best.MaxNoise > calc.MaxNoise-0.2 && calc.TotNoise < best.TotNoise) ||
			(calc.MaxNoise <= 0.0 &&
				best.MaxNoise > 0.0 &&
				best.MaxNoise > calc.MaxNoise-0.2 &&
				calc.TotNoise < best.TotNoise+best.OverNoise) ||
			(calc.MaxNoise > 0.0 &&
				best.MaxNoise > -0.05 &&
				best.MaxNoise > calc.MaxNoise-0.1 &&
				calc.TotNoise+calc.OverNoise < best.TotNoise+best.OverNoise) ||
			(calc.MaxNoise > 0.0 &&
				best.MaxNoise > -0.1 &&
				best.MaxNoise > calc.MaxNoise-0.15 &&
				calc.TotNoise+calc.OverNoise+calc.OverNoise <
					best.TotNoise+best.OverNoise+best.OverNoise)
	case 5:
		better = calc.OverNoise < best.OverNoise ||
			(eqFloat(calc.OverNoise, best.OverNoise) && calc.TotNoise < best.TotNoise)
	case 6:
		better = calc.OverNoise < best.OverNoise ||
			(eqFloat(calc.OverNoise, best.OverNoise) &&
				(calc.MaxNoise < best.MaxNoise ||
					(eqFloat(calc.MaxNoise, best.MaxNoise) && calc.TotNoise <= best.TotNoise)))
	case 7:
		better = calc.OverCount < best.OverCount || calc.OverNoise < best.OverNoise
	}

	if best.OverCount == 0 {
		// If no distorted bands, only use this quantization if it is better and
		// uses fewer bits.
		better = better && calc.Bits < best.Bits
	}

	if better {
		return 1
	}
	return 0
}

// ampScalefacBands amplifies the scalefactor bands whose noise exceeds the
// trigger threshold (amp_scalefac_bands, quantize.c:720). The trigger depends on
// noise_shaping_amp (0/default: ISO all distort>1; 1: within 50% of max in dB;
// 2: exactly one band; 3: refine pass selects 1 then 2).
func (gfc *LameInternalFlags) ampScalefacBands(codInfo *GrInfo, distort []float32, xrpow []float32, bRefine int) {
	cfg := &gfc.Cfg

	var ifqstep34 float64
	if codInfo.ScalefacScale == 0 {
		ifqstep34 = ifqstep34Scale0
	} else {
		ifqstep34 = ifqstep34Scale1
	}

	// compute maximum value of distort[]
	trigger := float32(0)
	for sfb := 0; sfb < codInfo.Sfbmax; sfb++ {
		if trigger < distort[sfb] {
			trigger = distort[sfb]
		}
	}

	noiseShapingAmp := cfg.NoiseShapingAmp
	if noiseShapingAmp == 3 {
		if bRefine == 1 {
			noiseShapingAmp = 2
		} else {
			noiseShapingAmp = 1
		}
	}
	switch noiseShapingAmp {
	case 2:
		// amplify exactly 1 band
	case 1:
		// amplify bands within 50% of max (on db scale)
		if trigger > 1.0 {
			trigger = float32(math.Pow(float64(trigger), 0.5))
		} else {
			trigger = qeTrigger95(trigger)
		}
	default: // case 0
		// ISO algorithm.  amplify all bands with distort>1
		if trigger > 1.0 {
			trigger = 1.0
		} else {
			trigger = qeTrigger95(trigger)
		}
	}

	j := 0
	for sfb := 0; sfb < codInfo.Sfbmax; sfb++ {
		width := codInfo.Width[sfb]
		j += width
		if distort[sfb] < trigger {
			continue
		}

		if gfc.SvQnt.SubstepShaping&2 != 0 {
			if gfc.SvQnt.Pseudohalf[sfb] != 0 {
				gfc.SvQnt.Pseudohalf[sfb] = 0
			} else {
				gfc.SvQnt.Pseudohalf[sfb] = 1
			}
			if gfc.SvQnt.Pseudohalf[sfb] == 0 && cfg.NoiseShapingAmp == 2 {
				return
			}
		}
		codInfo.Scalefac[sfb]++
		for l := -width; l < 0; l++ {
			xrpow[j+l] = qeMulDConst(xrpow[j+l], ifqstep34)
			if xrpow[j+l] > codInfo.XrpowMax {
				codInfo.XrpowMax = xrpow[j+l]
			}
		}

		if cfg.NoiseShapingAmp == 2 {
			return
		}
	}
}

// incScalefacScale turns on scalefac_scale and halves the scalefactors,
// re-scaling xrpow for the bands whose scalefactor was odd (inc_scalefac_scale,
// quantize.c:808). Takehiro Tominaga.
func incScalefacScale(codInfo *GrInfo, xrpow []float32) {
	const ifqstep34 = ifqstep34Scale0

	j := 0
	for sfb := 0; sfb < codInfo.Sfbmax; sfb++ {
		width := codInfo.Width[sfb]
		s := codInfo.Scalefac[sfb]
		if codInfo.Preflag != 0 {
			s += pretab[sfb]
		}
		j += width
		if s&1 != 0 {
			s++
			for l := -width; l < 0; l++ {
				xrpow[j+l] = qeMulDConst(xrpow[j+l], ifqstep34)
				if xrpow[j+l] > codInfo.XrpowMax {
					codInfo.XrpowMax = xrpow[j+l]
				}
			}
		}
		codInfo.Scalefac[sfb] = s >> 1
	}
	codInfo.Preflag = 0
	codInfo.ScalefacScale = 1
}

// incSubblockGain increases the per-window subblock gain for short blocks and
// adjusts the affected scalefactors / xrpow, returning 1 if it cannot proceed
// (a long-block scalefac >= 16 or a subblock_gain at its max) (inc_subblock_gain,
// quantize.c:847). Takehiro Tominaga.
func (gfc *LameInternalFlags) incSubblockGain(codInfo *GrInfo, xrpow []float32) int {
	scalefac := codInfo.Scalefac[:]

	// subbloc_gain can't do anything in the long block region
	for sfb := 0; sfb < codInfo.SfbLmax; sfb++ {
		if scalefac[sfb] >= 16 {
			return 1
		}
	}

	for window := 0; window < 3; window++ {
		s1, s2 := 0, 0

		sfb := codInfo.SfbLmax + window
		for ; sfb < codInfo.Sfbdivide; sfb += 3 {
			if s1 < scalefac[sfb] {
				s1 = scalefac[sfb]
			}
		}
		for ; sfb < codInfo.Sfbmax; sfb += 3 {
			if s2 < scalefac[sfb] {
				s2 = scalefac[sfb]
			}
		}

		if s1 < 16 && s2 < 8 {
			continue
		}

		if codInfo.SubblockGain[window] >= 7 {
			return 1
		}

		// even though there is no scalefactor for sfb12 subblock gain affects
		// upper frequencies too, that's why we go up to SBMAX_s.
		codInfo.SubblockGain[window]++
		j := gfc.ScalefacBand.L[codInfo.SfbLmax]
		for sfb = codInfo.SfbLmax + window; sfb < codInfo.Sfbmax; sfb += 3 {
			width := codInfo.Width[sfb]
			s := scalefac[sfb]
			s = s - (4 >> codInfo.ScalefacScale)
			if s >= 0 {
				scalefac[sfb] = s
				j += width * 3
				continue
			}

			scalefac[sfb] = 0
			gain := 210 + (s << (codInfo.ScalefacScale + 1))
			amp := ipow20Idx(gain)
			j += width * (window + 1)
			for l := -width; l < 0; l++ {
				xrpow[j+l] = qeMul(xrpow[j+l], amp)
				if xrpow[j+l] > codInfo.XrpowMax {
					codInfo.XrpowMax = xrpow[j+l]
				}
			}
			j += width * (3 - window - 1)
		}

		amp := ipow20Idx(202)
		j += codInfo.Width[sfb] * (window + 1)
		for l := -codInfo.Width[sfb]; l < 0; l++ {
			xrpow[j+l] = qeMul(xrpow[j+l], amp)
			if xrpow[j+l] > codInfo.XrpowMax {
				codInfo.XrpowMax = xrpow[j+l]
			}
		}
	}
	return 0
}

// balanceNoise amplifies scalefactor bands and decides whether the resulting
// scalefactors are encodable, escalating to scalefac_scale or subblock_gain when
// some bands are over-amplified (balance_noise, quantize.c:940). Returns 0 when
// all bands are amplified (search done) or the amplification is invalid, 1 when
// a valid new amplification was produced. Takehiro Tominaga / Robert Hegemann.
func (gfc *LameInternalFlags) balanceNoise(codInfo *GrInfo, distort []float32, xrpow []float32, bRefine int) int {
	cfg := &gfc.Cfg

	gfc.ampScalefacBands(codInfo, distort, xrpow, bRefine)

	// loop_break returns 0 if there is an unamplified scalefac.
	status := loopBreak(codInfo)
	if status != 0 {
		return 0 // all bands amplified
	}

	// not all scalefactors amplified; encode them.
	status = gfc.scaleBitcount(codInfo)
	if status == 0 {
		return 1 // amplified some bands not exceeding limits
	}

	// some scalefactors are too large; try scalefac_scale=1.
	if cfg.NoiseShaping > 1 {
		for i := range gfc.SvQnt.Pseudohalf {
			gfc.SvQnt.Pseudohalf[i] = 0
		}
		if codInfo.ScalefacScale == 0 {
			incScalefacScale(codInfo, xrpow)
			status = 0
		} else {
			if codInfo.BlockType == ShortType && cfg.SubblockGain > 0 {
				status = gfc.incSubblockGain(codInfo, xrpow)
				if status == 0 {
					status = loopBreak(codInfo)
				}
			}
		}
	}

	if status == 0 {
		status = gfc.scaleBitcount(codInfo)
	}
	if status != 0 {
		return 0
	}
	return 1
}

// outerLoop is the outer iteration loop: it starts from bin_search_StepSize's
// step, then repeatedly amplifies bands (balance_noise) and increases the
// quantizer step (count_bits) keeping the best quantization under the chosen
// quant_compare metric, until the search ends (outer_loop, quantize.c:1010).
// Returns best_noise_info.over_count. mt 5/99.
func (gfc *LameInternalFlags) outerLoop(codInfo *GrInfo, l3Xmin []float32, xrpow []float32, ch, targBits int) int {
	cfg := &gfc.Cfg
	var codInfoW GrInfo
	var saveXrpow [576]float32
	var distort [SFBMAX]float32
	var bestNoiseInfo CalcNoiseResult
	var prevNoise CalcNoiseData
	bestPart23Length := 9999999
	bEndOfSearch := 0
	bRefine := 0
	bestGgainPass1 := 0

	gfc.binSearchStepSize(codInfo, targBits, ch, xrpow)

	if cfg.NoiseShaping == 0 {
		// fast mode, no noise shaping, we are ready
		return 100 // default noise_info.over_count
	}

	// prev_noise is memset to zero (Go zero value of CalcNoiseData).

	// compute the distortion in this quantization
	calcNoise(codInfo, l3Xmin, distort[:], &bestNoiseInfo, &prevNoise)
	bestNoiseInfo.Bits = codInfo.Part23Length

	codInfoW = *codInfo
	age := 0
	copy(saveXrpow[:], xrpow[:576])

	for bEndOfSearch == 0 {
		// BEGIN MAIN LOOP
		for {
			var noiseInfo CalcNoiseResult
			var searchLimit int
			maxggain := 255

			if gfc.SvQnt.SubstepShaping&2 != 0 {
				searchLimit = 20
			} else {
				searchLimit = 3
			}

			// In VBR we can't get rid of distortion in the last sfb; quit now.
			if gfc.SvQnt.Sfb21Extra != 0 {
				if distort[codInfoW.Sfbmax] > 1.0 {
					break
				}
				if codInfoW.BlockType == ShortType &&
					(distort[codInfoW.Sfbmax+1] > 1.0 || distort[codInfoW.Sfbmax+2] > 1.0) {
					break
				}
			}

			// try a new scalefactor combination on cod_info_w
			if gfc.balanceNoise(&codInfoW, distort[:], xrpow, bRefine) == 0 {
				break
			}
			if codInfoW.ScalefacScale != 0 {
				maxggain = 254
			}

			huffBits := targBits - codInfoW.Part2Length
			if huffBits <= 0 {
				break
			}

			// increase quantizer stepsize until needed bits are below maximum
			for {
				codInfoW.Part23Length = gfc.countBits(xrpow, &codInfoW, &prevNoise)
				if !(codInfoW.Part23Length > huffBits && codInfoW.GlobalGain <= maxggain) {
					break
				}
				codInfoW.GlobalGain++
			}

			if codInfoW.GlobalGain > maxggain {
				break
			}

			if bestNoiseInfo.OverCount == 0 {
				for {
					codInfoW.Part23Length = gfc.countBits(xrpow, &codInfoW, &prevNoise)
					if !(codInfoW.Part23Length > bestPart23Length && codInfoW.GlobalGain <= maxggain) {
						break
					}
					codInfoW.GlobalGain++
				}
				if codInfoW.GlobalGain > maxggain {
					break
				}
			}

			// compute the distortion in this quantization
			calcNoise(&codInfoW, l3Xmin, distort[:], &noiseInfo, &prevNoise)
			noiseInfo.Bits = codInfoW.Part23Length

			// check if this quantization is better than our saved quantization
			var better int
			if codInfo.BlockType != ShortType {
				better = cfg.QuantComp
			} else {
				better = cfg.QuantCompShort
			}

			better = quantCompare(better, &bestNoiseInfo, &noiseInfo, &codInfoW, distort[:])

			if better != 0 {
				bestPart23Length = codInfo.Part23Length
				bestNoiseInfo = noiseInfo
				*codInfo = codInfoW
				age = 0
				// store for later reuse
				copy(saveXrpow[:], xrpow[:576])
			} else {
				// early stop?
				if cfg.FullOuterLoop == 0 {
					age++
					if age > searchLimit && bestNoiseInfo.OverCount == 0 {
						break
					}
					if cfg.NoiseShapingAmp == 3 && bRefine != 0 && age > 30 {
						break
					}
					if cfg.NoiseShapingAmp == 3 && bRefine != 0 &&
						(codInfoW.GlobalGain-bestGgainPass1) > 15 {
						break
					}
				}
			}

			if !((codInfoW.GlobalGain + codInfoW.ScalefacScale) < 255) {
				break
			}
		}

		if cfg.NoiseShapingAmp == 3 {
			if bRefine == 0 {
				// refine search
				codInfoW = *codInfo
				copy(xrpow[:576], saveXrpow[:])
				age = 0
				bestGgainPass1 = codInfoW.GlobalGain
				bRefine = 1
			} else {
				// search already refined, stop
				bEndOfSearch = 1
			}
		} else {
			bEndOfSearch = 1
		}
	}

	// finish up
	if cfg.Vbr == vbrRh || cfg.Vbr == vbrMtrh || cfg.Vbr == vbrMt {
		// restore for reuse on next try
		copy(xrpow[:576], saveXrpow[:])
	} else if gfc.SvQnt.SubstepShaping&1 != 0 {
		gfc.trancateSmallspectrums(codInfo, l3Xmin, xrpow)
	}

	return bestNoiseInfo.OverCount
}

// iterationFinishOne finalises a granule after its quantization: better
// scalefactor storage, optional best_huffman_divide, and the reservoir debit
// (iteration_finish_one, quantize.c:1213). Robert Hegemann.
func (gfc *LameInternalFlags) iterationFinishOne(gr, ch int) {
	cfg := &gfc.Cfg
	l3Side := &gfc.L3Side
	codInfo := &l3Side.Tt[gr][ch]

	// try some better scalefac storage
	gfc.bestScalefacStore(gr, ch, l3Side)

	// best huffman_divide may save some bits too
	if cfg.UseBestHuffman == 1 {
		gfc.bestHuffmanDivide(codInfo)
	}

	// update reservoir status after FINAL quantization/bitrate
	gfc.ResvAdjust(codInfo)
}

// getFramebits fills frameBits[i] with the per-bitrate ResvFrameBegin budgets
// used by the ABR/VBR loops (get_framebits, quantize.c:1340). Robert Hegemann.
func (gfc *LameInternalFlags) getFramebits(frameBits []int) {
	cfg := &gfc.Cfg
	eov := &gfc.OvEnc

	// always use at least this many bits per granule per channel unless analog
	// silence is detected.
	eov.BitrateIndex = cfg.VbrMinBitrateIndex
	_ = gfc.getframebits()

	// bits for analog silence
	eov.BitrateIndex = 1
	bitsPerFrame := gfc.getframebits()

	for i := 1; i <= cfg.VbrMaxBitrateIndex; i++ {
		eov.BitrateIndex = i
		frameBits[i] = gfc.ResvFrameBegin(&bitsPerFrame)
	}
}

// calcTargetBits computes the per-(gr,ch) target bit counts for ABR encoding
// from the mean bits, perceptual entropy and the res_factor reservoir policy
// (calc_target_bits, quantize.c:1767). mt 2000/05/31. It returns the
// analog-silence target and max-frame-bits via the pointer parameters.
func (gfc *LameInternalFlags) calcTargetBits(pe *[2][2]float32, msEnerRatio *[2]float32,
	targBits *[2][2]int, analogSilenceBits, maxFrameBits *int) {
	cfg := &gfc.Cfg
	eov := &gfc.OvEnc
	l3Side := &gfc.L3Side
	framesize := 576 * cfg.ModeGr

	var meanBits int
	eov.BitrateIndex = cfg.VbrMaxBitrateIndex
	*maxFrameBits = gfc.ResvFrameBegin(&meanBits)

	eov.BitrateIndex = 1
	meanBits = gfc.getframebits() - cfg.SideinfoLen*8
	*analogSilenceBits = meanBits / (cfg.ModeGr * cfg.ChannelsOut)

	// mean_bits = vbr_avg_bitrate_kbps * framesize * 1000 (int), then optionally
	// *1.09 (double) and /samplerate_out, all in C int except the 1.09 scaling.
	mb := cfg.VbrAvgBitrateKbps * framesize * 1000
	if gfc.SvQnt.SubstepShaping&1 != 0 {
		mb = int(float64(mb) * 1.09)
	}
	mb /= cfg.SamplerateOut
	mb -= cfg.SideinfoLen * 8
	mb /= cfg.ModeGr * cfg.ChannelsOut
	meanBits = mb

	resFactor := qeResFactor(cfg.CompressionRatio)
	if resFactor < 0.90 {
		resFactor = 0.90
	}
	if resFactor > 1.00 {
		resFactor = 1.00
	}

	for gr := 0; gr < cfg.ModeGr; gr++ {
		sum := 0
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			targBits[gr][ch] = qeTargBits(resFactor, meanBits)

			if pe[gr][ch] > 700 {
				addBits := qeAddBits(pe[gr][ch])

				codInfo := &l3Side.Tt[gr][ch]
				targBits[gr][ch] = qeTargBits(resFactor, meanBits)

				// short blocks use a little extra
				if codInfo.BlockType == ShortType {
					if addBits < meanBits/2 {
						addBits = meanBits / 2
					}
				}
				// at most increase bits by 1.5*average
				if addBits > meanBits*3/2 {
					addBits = meanBits * 3 / 2
				} else if addBits < 0 {
					addBits = 0
				}

				targBits[gr][ch] += addBits
			}
			if targBits[gr][ch] > maxBitsPerChannel {
				targBits[gr][ch] = maxBitsPerChannel
			}
			sum += targBits[gr][ch]
		}
		if sum > maxBitsPerGranule {
			for ch := 0; ch < cfg.ChannelsOut; ch++ {
				targBits[gr][ch] *= maxBitsPerGranule
				targBits[gr][ch] /= sum
			}
		}
	}

	if gfc.OvEnc.ModeExt == mpgMdMSLR {
		for gr := 0; gr < cfg.ModeGr; gr++ {
			reduceSide(&targBits[gr], msEnerRatio[gr], meanBits*cfg.ChannelsOut, maxBitsPerGranule)
		}
	}

	// sum target bits
	totbits := 0
	for gr := 0; gr < cfg.ModeGr; gr++ {
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			if targBits[gr][ch] > maxBitsPerChannel {
				targBits[gr][ch] = maxBitsPerChannel
			}
			totbits += targBits[gr][ch]
		}
	}

	// repartition target bits if needed
	if totbits > *maxFrameBits && totbits > 0 {
		for gr := 0; gr < cfg.ModeGr; gr++ {
			for ch := 0; ch < cfg.ChannelsOut; ch++ {
				targBits[gr][ch] *= *maxFrameBits
				targBits[gr][ch] /= totbits
			}
		}
	}
}

// ABRIterationLoop encodes one frame at a desired average bitrate
// (ABR_iteration_loop, quantize.c:1900). mt 2000/05/31. It computes per-granule
// target bits, quantizes each granule via outer_loop, then picks the bitrate
// that refills the reservoir to non-negative size.
func (gfc *LameInternalFlags) abrIterationLoop(pe *[2][2]float32, msEnerRatio *[2]float32, ratio *[2][2]III_psy_ratio) {
	cfg := &gfc.Cfg
	eov := &gfc.OvEnc
	var l3Xmin [SFBMAX]float32
	var xrpow [576]float32
	var targBits [2][2]int
	var meanBits, maxFrameBits int
	var analogSilenceBits int
	l3Side := &gfc.L3Side

	gfc.calcTargetBits(pe, msEnerRatio, &targBits, &analogSilenceBits, &maxFrameBits)

	// encode granules
	for gr := 0; gr < cfg.ModeGr; gr++ {
		if gfc.OvEnc.ModeExt == mpgMdMSLR {
			msConvert(&gfc.L3Side, gr)
		}
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			codInfo := &l3Side.Tt[gr][ch]

			// adjust = 0 in ABR (the pe-based adjust is commented out in the C).
			var maskingLowerDb float32
			if codInfo.BlockType != ShortType {
				maskingLowerDb = gfc.SvQnt.MaskAdjust
			} else {
				maskingLowerDb = gfc.SvQnt.MaskAdjustShort
			}
			gfc.SvQnt.MaskingLower = float32(math.Pow(10.0, float64(maskingLowerDb)*0.1))

			gfc.initOuterLoop(codInfo)
			if gfc.initXrpow(codInfo, xrpow[:]) != 0 {
				athOver := calcXmin(gfc, &ratio[gr][ch], codInfo, l3Xmin[:])
				if athOver == 0 { // analog silence
					targBits[gr][ch] = analogSilenceBits
				}
				gfc.outerLoop(codInfo, l3Xmin[:], xrpow[:], ch, targBits[gr][ch])
			}
			gfc.iterationFinishOne(gr, ch)
		}
	}

	// find a bitrate which can refill the reservoir to positive size.
	for eov.BitrateIndex = cfg.VbrMinBitrateIndex; eov.BitrateIndex <= cfg.VbrMaxBitrateIndex; eov.BitrateIndex++ {
		if gfc.ResvFrameBegin(&meanBits) >= 0 {
			break
		}
	}

	gfc.ResvFrameEnd(meanBits)
}

// CBRIterationLoop encodes one frame at a constant bitrate (CBR_iteration_loop,
// quantize.c:1988): per granule it allocates target bits (on_pe), converts to
// M/S if chosen (ms_convert + reduce_side), then quantizes each channel via
// outer_loop and finalises the reservoir.
func (gfc *LameInternalFlags) cbrIterationLoop(pe *[2][2]float32, msEnerRatio *[2]float32, ratio *[2][2]III_psy_ratio) {
	cfg := &gfc.Cfg
	var l3Xmin [SFBMAX]float32
	var xrpow [576]float32
	var targBits [2]int
	var meanBits int
	l3Side := &gfc.L3Side

	gfc.ResvFrameBegin(&meanBits)

	// quantize!
	for gr := 0; gr < cfg.ModeGr; gr++ {
		// calculate needed bits
		maxBits := gfc.onPe(pe, &targBits, meanBits, gr, gr)

		if gfc.OvEnc.ModeExt == mpgMdMSLR {
			msConvert(&gfc.L3Side, gr)
			reduceSide(&targBits, msEnerRatio[gr], meanBits, maxBits)
		}

		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			codInfo := &l3Side.Tt[gr][ch]

			// adjust = 0 in CBR (the pe-based adjust is commented out in the C).
			var maskingLowerDb float32
			if codInfo.BlockType != ShortType {
				maskingLowerDb = gfc.SvQnt.MaskAdjust
			} else {
				maskingLowerDb = gfc.SvQnt.MaskAdjustShort
			}
			gfc.SvQnt.MaskingLower = float32(math.Pow(10.0, float64(maskingLowerDb)*0.1))

			gfc.initOuterLoop(codInfo)
			if gfc.initXrpow(codInfo, xrpow[:]) != 0 {
				calcXmin(gfc, &ratio[gr][ch], codInfo, l3Xmin[:])
				gfc.outerLoop(codInfo, l3Xmin[:], xrpow[:], ch, targBits[ch])
			}

			gfc.iterationFinishOne(gr, ch)
		}
	}

	gfc.ResvFrameEnd(meanBits)
}

// neqFloat is LAME's NEQ(a,b) macro (machine.h:177): !EQ(a,b).
func neqFloat(a, b float32) bool { return !eqFloat(a, b) }

// sortFloat32Asc sorts s ascending, matching the C qsort(..., floatcompare)
// (quantize.c:444 floatcompare returns +1 if a>b, -1 if a<b, 0 otherwise — a
// total order on float32, no NaNs in the xrpow magnitudes). trancate_smallspectrums
// is the sole caller.
func sortFloat32Asc(s []float32) {
	// insertion sort keeps the comparison order identical to floatcompare and
	// avoids pulling in sort.Slice's interface overhead for these tiny (<= 192
	// element) per-band runs.
	for i := 1; i < len(s); i++ {
		v := s[i]
		j := i - 1
		for j >= 0 && s[j] > v {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = v
	}
}
