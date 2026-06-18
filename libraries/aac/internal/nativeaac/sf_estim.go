// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Scale-factor estimation ported 1:1 from the vendored FDK-AAC reference
// libAACenc/src/sf_estim.cpp + sf_estim.h. This is the AAC encoder
// rate-control/quantizer DRIVER stage FDKaacEnc_EstimateScaleFactors runs after
// the threshold adjustment (adj_thr.cpp): from the per-sfb adjusted thresholds
// and energies (ld64 log domain) and the MDCT spectrum it derives the initial
// integer scalefactor for every scalefactor band, then — when analysis-by-
// synthesis is enabled (invQuant > 0) — refines them via the
// assimilate*/improveScf passes and finally maps them to loop scalefactors and
// the global gain.
//
// Pure fixed-point: every value is an int32 FIXP_DBL Q-format / INT, with carried
// block exponents — bit-identical to the C, NO float, NO transcendental. The only
// arithmetic is leading-bit normalisation, arithmetic shift block-floating-point,
// int64-product fixmul kernels, the ld64-domain CalcLdData / CalcInvLdData ROM
// lookups, the sqrtFixp ROM kernel and the table-driven quantizer. So these
// carry no aac_strict FP gate (only the aacfdk license fence); the parity oracle
// asserts EXACT int32 equality against the genuine sf_estim.cpp symbols.
//
// fMult(FIXP_DBL,FIXP_DBL) on this build target (aarch64 == __ARM_ARCH_8__)
// resolves to fixmul_DD == fixmulDDarm8 (smull;asr#31, KEEPS bit 31) — NOT the
// generic (fixmuldiv2_DD<<1) fMultDD which drops it. Every fMult site below
// therefore uses fixmulDDarm8, matching the C oracle compiled on the same arch.
// The leaf kernels (quantizer FDKaacEnc_calcSfbDist / calcSfbQuantEnergyAndDist,
// the LD-domain CalcLdData / CalcInvLdData, sqrtFixp, fMultI, the scalefactor-
// delta bit counter) are the already-ported package helpers — not duplicated.

// sf_estim Q-format constants (sf_estim.cpp:111-115).
//
//	#define UPCOUNT_LIMIT 1
//	#define AS_PE_FAC_SHIFT 7
//	#define DIST_FAC_SHIFT 3
//	static const INT MAX_SCF_DELTA = 60;
const (
	upcountLimit = 1
	asPeFacShift = 7
	distFacShift = 3
	maxScfDelta  = 60
)

// fdkIntMin is FDK_INT_MIN == (INT)0x80000000 (genericStds.h:503), the sentinel
// the estimator marks zero-energy / thresh>energy scfs with. (fdkIntMax is the
// already-ported ratecontrol.go const.)
const fdkIntMin = -0x80000000

// PE breakpoint constants of sf_estim.cpp (sf_estim.cpp:117-121). These are
// fl2fxconstDBL of the documented reals, materialised exactly as the C compiler
// folds them; they are distinct from line_pe.go's c1/c2/c3 (different scalings).
//
//	PE_C1 = FL2FXCONST_DBL(3.0f / 128.0f)        // (log(8)/log(2)) >> AS_PE_FAC_SHIFT
//	PE_C2 = FL2FXCONST_DBL(1.3219281f / 128.0f)  // (log(2.5)/log(2)) >> AS_PE_FAC_SHIFT
//	PE_C3 = FL2FXCONST_DBL(0.5593573f)           // 1-C2/C1
var (
	sfePeC1 = fl2fxconstDBL(3.0 / 128.0)
	sfePeC2 = fl2fxconstDBL(1.3219281 / 128.0)
	sfePeC3 = fl2fxconstDBL(0.5593573)
)

// bitCountScalefactorDelta is the 1:1 port of the inline
// FDKaacEnc_bitCountScalefactorDelta(delta) (bit_cnt.h:192): the Huffman code
// length of a DPCM scalefactor delta, == FDKaacEnc_huff_ltabscf[delta +
// CODE_BOOK_SCF_LAV]. The C asserts 0 <= delta+CODE_BOOK_SCF_LAV < table-size.
func bitCountScalefactorDelta(delta int) int {
	return int(huffltabscf[delta+codeBookScfLav])
}

// calcFormFactorChannel is the 1:1 port of
// FDKaacEnc_FDKaacEnc_CalcFormFactorChannel (sf_estim.cpp:132-157): the per-sfb
// form factor == ld(sum of sqrt(|spec|) >> FORM_FAC_SHIFT) over the band, in the
// ld64 domain. sfbs beyond maxSfbPerGroup (within sfbPerGroup) are set to -1.0.
func calcFormFactorChannel(sfbFormFactorLdData []int32,
	mdctSpectrum []int32, sfbOffsets []int,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int) {
	tmp0 := sfbCnt
	tmp1 := maxSfbPerGroup
	step := sfbPerGroup
	for sfbGrp := 0; sfbGrp < tmp0; sfbGrp += step {
		var sfb int
		for sfb = 0; sfb < tmp1; sfb++ {
			var formFactor int32 // FL2FXCONST_DBL(0.0f)
			// calc sum of sqrt(spec)
			for j := sfbOffsets[sfbGrp+sfb]; j < sfbOffsets[sfbGrp+sfb+1]; j++ {
				formFactor += sqrtFixp(fixabsD(mdctSpectrum[j])) >> formFacShift
			}
			sfbFormFactorLdData[sfbGrp+sfb] = calcLdData(formFactor)
		}
		// set sfbFormFactor for sfbs with zero spec to zero (debugging)
		for ; sfb < sfbPerGroup; sfb++ {
			sfbFormFactorLdData[sfbGrp+sfb] = fl2fxconstDBL(-1.0)
		}
	}
}

// calcSfbRelevantLines is the 1:1 port of FDKaacEnc_calcSfbRelevantLines
// (sf_estim.cpp:183-224): sfbNRelevantLines[i] from the per-sfb form factor,
// energy and width (scaled by 1/((2^FORM_FAC_SHIFT)*2.0)). Only bands with
// energy > threshold get a non-zero value; the rest stay 0 (FDKmemclear).
func calcSfbRelevantLines(
	sfbFormFactorLdData, sfbEnergyLdData, sfbThresholdLdData []int32,
	sfbOffsets []int, sfbCnt, sfbPerGroup, maxSfbPerGroup int,
	sfbNRelevantLines []int32) {
	asPeFacLdData := fl2fxconstDBL(0.109375) // AS_PE_FAC_SHIFT*ld64(2)

	for i := 0; i < sfbCnt; i++ {
		sfbNRelevantLines[i] = 0
	}

	for sfbOffs := 0; sfbOffs < sfbCnt; sfbOffs += sfbPerGroup {
		for sfb := 0; sfb < maxSfbPerGroup; sfb++ {
			if sfbEnergyLdData[sfbOffs+sfb] > sfbThresholdLdData[sfbOffs+sfb] {
				sfbWidth := sfbOffsets[sfbOffs+sfb+1] - sfbOffsets[sfbOffs+sfb]

				// sfbWidthLdData = CalcLdData((FIXP_DBL)(sfbWidth << (DFRACT_BITS-1-AS_PE_FAC_SHIFT)));
				sfbWidthLdData := int32(sfbWidth << (dfractBits - 1 - asPeFacShift))
				sfbWidthLdData = calcLdData(sfbWidthLdData)

				accu := sfbEnergyLdData[sfbOffs+sfb] - sfbWidthLdData - asPeFacLdData
				accu = sfbFormFactorLdData[sfbOffs+sfb] - (accu >> 2)

				sfbNRelevantLines[sfbOffs+sfb] = calcInvLdData(accu) >> 1
			}
		}
	}
}

// countSingleScfBits is the 1:1 port of FDKaacEnc_countSingleScfBits
// (sf_estim.cpp:233-243): the ld-domain scalefactor-delta bit cost of scf
// against its left/right neighbours, scaled by 1/(2^(2*AS_PE_FAC_SHIFT)).
func countSingleScfBits(scf, scfLeft, scfRight int) int32 {
	scfBitsFract := int32(bitCountScalefactorDelta(scfLeft-scf) +
		bitCountScalefactorDelta(scf-scfRight))
	scfBitsFract = scfBitsFract << (dfractBits - 1 - (2 * asPeFacShift))
	return scfBitsFract
}

// calcSingleSpecPe is the 1:1 port of FDKaacEnc_calcSingleSpecPe
// (sf_estim.cpp:250-268): the spectral perceptual entropy of a band for a trial
// scf, scaled by 1/(2^(2*AS_PE_FAC_SHIFT)). fMult == fixmulDDarm8 (see file doc).
func calcSingleSpecPe(scf int, sfbConstPePart, nLines int32) int32 {
	var specPe int32 // FL2FXCONST_DBL(0.0f)

	scfFract := int32(scf << (dfractBits - 1 - asPeFacShift))

	ldRatio := sfbConstPePart - fixmulDDarm8(fl2fxconstDBL(0.375), scfFract)

	if ldRatio >= sfePeC1 {
		specPe = fixmulDDarm8(fl2fxconstDBL(0.7), fixmulDDarm8(nLines, ldRatio))
	} else {
		specPe = fixmulDDarm8(fl2fxconstDBL(0.7),
			fixmulDDarm8(nLines, sfePeC2+fixmulDDarm8(sfePeC3, ldRatio)))
	}

	return specPe
}

// countScfBitsDiff is the 1:1 port of FDKaacEnc_countScfBitsDiff
// (sf_estim.cpp:275-313): the change in scalefactor-delta bit demand when scfOld
// is replaced by scfNew over [startSfb, stopSfb), scaled by
// 1/(2^(2*AS_PE_FAC_SHIFT)). Bands marked FDK_INT_MIN are skipped, walking the
// previous/next relevant bands for the boundary deltas.
func countScfBitsDiff(scfOld, scfNew []int, sfbCnt, startSfb, stopSfb int) int32 {
	scfBitsDiff := 0
	var sfb, sfbLast int
	var sfbPrev, sfbNext int

	// search for first relevant sfb
	sfbLast = startSfb
	for (sfbLast < stopSfb) && (scfOld[sfbLast] == fdkIntMin) {
		sfbLast++
	}
	// search for previous relevant sfb and count diff
	sfbPrev = startSfb - 1
	for (sfbPrev >= 0) && (scfOld[sfbPrev] == fdkIntMin) {
		sfbPrev--
	}
	if sfbPrev >= 0 {
		scfBitsDiff +=
			bitCountScalefactorDelta(scfNew[sfbPrev]-scfNew[sfbLast]) -
				bitCountScalefactorDelta(scfOld[sfbPrev]-scfOld[sfbLast])
	}
	// now loop through all sfbs and count diffs of relevant sfbs
	for sfb = sfbLast + 1; sfb < stopSfb; sfb++ {
		if scfOld[sfb] != fdkIntMin {
			scfBitsDiff +=
				bitCountScalefactorDelta(scfNew[sfbLast]-scfNew[sfb]) -
					bitCountScalefactorDelta(scfOld[sfbLast]-scfOld[sfb])
			sfbLast = sfb
		}
	}
	// search for next relevant sfb and count diff
	sfbNext = stopSfb
	for (sfbNext < sfbCnt) && (scfOld[sfbNext] == fdkIntMin) {
		sfbNext++
	}
	if sfbNext < sfbCnt {
		scfBitsDiff +=
			bitCountScalefactorDelta(scfNew[sfbLast]-scfNew[sfbNext]) -
				bitCountScalefactorDelta(scfOld[sfbLast]-scfOld[sfbNext])
	}

	scfBitsFract := int32(scfBitsDiff << (dfractBits - 1 - (2 * asPeFacShift)))
	return scfBitsFract
}

// calcSpecPeDiff is the 1:1 port of FDKaacEnc_calcSpecPeDiff
// (sf_estim.cpp:320-369): the change in spectral PE when scfOld is replaced by
// scfNew over [startSfb, stopSfb), scaled by 1/(2^(2*AS_PE_FAC_SHIFT)). Lazily
// fills sfbConstPePart[sfb] (FDK_INT_MIN sentinel == not yet computed). fMult ==
// fixmulDDarm8.
func calcSpecPeDiff(
	sfbEnergyLdData []int32, scfOld, scfNew []int,
	sfbConstPePart, sfbFormFactorLdData, sfbNRelevantLines []int32,
	startSfb, stopSfb int) int32 {
	var specPeDiff int32 // FL2FXCONST_DBL(0.0f)

	for sfb := startSfb; sfb < stopSfb; sfb++ {
		if scfOld[sfb] != fdkIntMin {
			if sfbConstPePart[sfb] == int32(fdkIntMin) {
				sfbConstPePart[sfb] =
					((sfbEnergyLdData[sfb] - sfbFormFactorLdData[sfb] -
						fl2fxconstDBL(0.09375)) >> 1) +
						fl2fxconstDBL(0.02152255861)
			}

			scfFract := int32(scfOld[sfb] << (dfractBits - 1 - asPeFacShift))
			ldRatioOld := sfbConstPePart[sfb] - fixmulDDarm8(fl2fxconstDBL(0.375), scfFract)

			scfFract = int32(scfNew[sfb] << (dfractBits - 1 - asPeFacShift))
			ldRatioNew := sfbConstPePart[sfb] - fixmulDDarm8(fl2fxconstDBL(0.375), scfFract)

			var pOld, pNew int32
			if ldRatioOld >= sfePeC1 {
				pOld = ldRatioOld
			} else {
				pOld = sfePeC2 + fixmulDDarm8(sfePeC3, ldRatioOld)
			}

			if ldRatioNew >= sfePeC1 {
				pNew = ldRatioNew
			} else {
				pNew = sfePeC2 + fixmulDDarm8(sfePeC3, ldRatioNew)
			}

			specPeDiff += fixmulDDarm8(fl2fxconstDBL(0.7),
				fixmulDDarm8(sfbNRelevantLines[sfb], pNew-pOld))
		}
	}

	return specPeDiff
}

// improveScf is the 1:1 port of FDKaacEnc_improveScf (sf_estim.cpp:378-452):
// analysis-by-synthesis refinement of one band's scalefactor — quantize/inverse-
// quantize with neighbouring scf values and keep the one with the best
// distortion. Returns the best scf; *distLdData and *minScfCalculated are
// returned alongside. spec/quantSpec/quantSpecTmp are the band-local slices.
func improveScf(spec []int32, quantSpec, quantSpecTmp []int16, sfbWidth int,
	threshLdData int32, scf, minScf int, dZoneQuantEnable bool) (
	scfBest int, distLdData int32, minScfCalculated int) {
	distFactorLdData := fl2fxconstDBL(-0.0050301265) // ld64(1/1.25)

	scfBest = scf

	// calc real distortion
	sfbDistLdData := fdkaacEncCalcSfbDist(spec, quantSpec, sfbWidth, scf, dZoneQuantEnable)
	minScfCalculated = scf
	// nmr > 1.25 -> try to improve nmr
	if sfbDistLdData > (threshLdData - distFactorLdData) {
		scfEstimated := scf
		sfbDistBestLdData := sfbDistLdData
		var cnt int
		// improve by bigger scf ?
		cnt = 0
		for (sfbDistLdData > (threshLdData - distFactorLdData)) && (cnt < upcountLimit) {
			cnt++
			scf++
			sfbDistLdData = fdkaacEncCalcSfbDist(spec, quantSpecTmp, sfbWidth, scf, dZoneQuantEnable)

			if sfbDistLdData < sfbDistBestLdData {
				scfBest = scf
				sfbDistBestLdData = sfbDistLdData
				for k := 0; k < sfbWidth; k++ {
					quantSpec[k] = quantSpecTmp[k]
				}
			}
		}
		// improve by smaller scf ?
		cnt = 0
		scf = scfEstimated
		sfbDistLdData = sfbDistBestLdData
		for (sfbDistLdData > (threshLdData - distFactorLdData)) && (cnt < 1) && (scf > minScf) {
			cnt++
			scf--
			sfbDistLdData = fdkaacEncCalcSfbDist(spec, quantSpecTmp, sfbWidth, scf, dZoneQuantEnable)

			if sfbDistLdData < sfbDistBestLdData {
				scfBest = scf
				sfbDistBestLdData = sfbDistLdData
				for k := 0; k < sfbWidth; k++ {
					quantSpec[k] = quantSpecTmp[k]
				}
			}
			minScfCalculated = scf
		}
		distLdData = sfbDistBestLdData
	} else { // nmr <= 1.25 -> try to find bigger scf to use less bits
		sfbDistBestLdData := sfbDistLdData
		sfbDistAllowedLdData := fMin(sfbDistLdData-distFactorLdData, threshLdData)
		for cnt := 0; cnt < upcountLimit; cnt++ {
			scf++
			sfbDistLdData = fdkaacEncCalcSfbDist(spec, quantSpecTmp, sfbWidth, scf, dZoneQuantEnable)

			if sfbDistLdData < sfbDistAllowedLdData {
				minScfCalculated = scfBest + 1
				scfBest = scf
				sfbDistBestLdData = sfbDistLdData
				for k := 0; k < sfbWidth; k++ {
					quantSpec[k] = quantSpecTmp[k]
				}
			}
		}
		distLdData = sfbDistBestLdData
	}

	return scfBest, distLdData, minScfCalculated
}
