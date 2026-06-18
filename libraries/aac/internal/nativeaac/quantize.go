// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// This file ports the AAC-LC ENCODE quantizer 1:1 from the vendored Fraunhofer
// FDK reference libAACenc/src/quantize.cpp: the fixed-point spectral
// quantization / inverse-quantization / distortion kernels the scalefactor
// estimator (sf_estim.cpp) and the quant loop drive.
//
//   - quantizeLines        — FDKaacEnc_quantizeLines (quantize.cpp:116): quantize
//     a run of MDCT lines for one quantizer-step-size (gain == globalGain-scf):
//     spec^(3/4) * 2^(-3/16*QSS) + k, via the (-gain)&3 / (-gain)>>2 mantissa/
//     exponent split and the FDKaacEnc_mTab_3_4 ^3/4 mantissa table.
//   - invQuantizeLines     — FDKaacEnc_invQuantizeLines (quantize.cpp:180): the
//     inverse, iquaSpectrum^4/3 * 2^(0.25*gain), via FDKaacEnc_mTab_4_3Elc and
//     the specExp comb tables.
//   - QuantizeSpectrum     — FDKaacEnc_QuantizeSpectrum (quantize.cpp:278): drive
//     quantizeLines over every (group,sfb), with QSS == globalGain-scalefactor.
//   - calcSfbDist          — FDKaacEnc_calcSfbDist (quantize.cpp:312): the
//     ld-domain quantization distortion of one band for a trial gain (the cost
//     function the scalefactor search minimises); MAX_QUANT overflow returns 0.
//   - calcSfbQuantEnergyAndDist — FDKaacEnc_calcSfbQuantEnergyAndDist
//     (quantize.cpp:361): the ld-domain quantized energy AND distortion of one
//     band for already-quantized lines.
//
// These kernels are pure integer fixed-point: every spectral line is an int32
// FIXP_DBL, every quantized value a SHORT (int16), every quantizer table entry a
// FIXP_QTD == FIXP_SGL (int16, ARCH_PREFER_MULT_32x16) or FIXP_DBL (int32). The
// arithmetic is leading-bit normalisation, arithmetic shifts, int64-product
// fixmul kernels and a table-driven ^3/4 / ^4/3 mantissa lookup — bit-identical
// regardless of -ffp-contract / vectorization, with NO transcendental and NO
// float. They therefore carry no aac_strict FP gate (only the aacfdk license
// fence); the parity oracle asserts EXACT int32 equality against the genuine
// quantize.cpp symbols. Every ported function names its C counterpart as
// file:line and is translated faithfully, not "improved".

// maxQuant mirrors MAX_QUANT == 8191 (quantize.h:110) — the largest representable
// quantized magnitude; calcSfbDist / calcSfbQuantEnergyAndDist treat any line
// exceeding it as a quantization overflow.
const maxQuant = 8191

// fMultDiv2SS multiplies two FIXP_SGL fractions, returning the int64 product (no
// down-scale beyond the implicit fract*fract). C counterpart: fixmuldiv2_SS,
// libFDK/include/fixmul.h:159 (the arm header does NOT define
// FUNCTION_fixmuldiv2_SS, so the generic form is used; fMultDiv2(SHORT, SHORT)
// resolves here via common_fix.h):
//
//	inline LONG fixmuldiv2_SS(const SHORT a, const SHORT b) {
//	  return ((LONG)a * b);
//	}
func fMultDiv2SS(a, b int16) int32 { return int32(a) * int32(b) }

// fdkaacEncQuantizeLines is the 1:1 port of FDKaacEnc_quantizeLines
// (quantize.cpp:116). gain == QSS (quantizer step size, globalGain-scalefactor);
// it quantizes noOfLines MDCT lines into quaSpectrum.
//
//	static void FDKaacEnc_quantizeLines(INT gain, INT noOfLines,
//	                                    const FIXP_DBL *mdctSpectrum,
//	                                    SHORT *quaSpectrum, INT dZoneQuantEnable);
func fdkaacEncQuantizeLines(gain, noOfLines int, mdctSpectrum []int32, quaSpectrum []int16, dZoneQuantEnable bool) {
	var k int32 = 0 // FL2FXCONST_DBL(0.0f)
	// FIXP_QTD quantizer = FDKaacEnc_quantTableQ[(-gain) & 3];
	quantizer := fdkaacEncQuantTableQ[(-gain)&3]
	quantizershift := ((-gain) >> 2) + 1
	const kShift = 16

	if dZoneQuantEnable {
		k = fl2fxconstDBL(0.23) >> kShift
	} else {
		k = fl2fxconstDBL(-0.0946+0.5) >> kShift
	}

	for line := 0; line < noOfLines; line++ {
		// FIXP_DBL accu = fMultDiv2(mdctSpectrum[line], quantizer);
		accu := fMultDiv2DS(mdctSpectrum[line], quantizer)

		if accu < 0 {
			accu = -accu
			// INT accuShift = CntLeadingZeros(accu) - 1;
			accuShift := int(fixnormzD(accu)) - 1
			accu <<= uint(accuShift)
			// INT tabIndex = (INT)(accu >> (DFRACT_BITS - 2 - MANT_DIGITS)) & (~MANT_SIZE);
			tabIndex := int(accu>>(dfractBitsQuant-2-mantDigits)) & mantMask
			totalShift := quantizershift - accuShift + 1
			// accu = fMultDiv2(FDKaacEnc_mTab_3_4[tabIndex], FDKaacEnc_quantTableE[totalShift & 3]);
			accu = fMultDiv2SS(fdkaacEncMTab34[tabIndex], fdkaacEncQuantTableE[totalShift&3])
			totalShift = (16 - 4) - (3 * (totalShift >> 2))
			// FDK_ASSERT(totalShift >= 0);
			accu >>= uint(fixMin(totalShift, dfractBitsQuant-1))
			// quaSpectrum[line] = (SHORT)(-((LONG)(k + accu) >> (DFRACT_BITS - 1 - 16)));
			quaSpectrum[line] = int16(-((k + accu) >> (dfractBitsQuant - 1 - 16)))
		} else if accu > 0 {
			accuShift := int(fixnormzD(accu)) - 1
			accu <<= uint(accuShift)
			tabIndex := int(accu>>(dfractBitsQuant-2-mantDigits)) & mantMask
			totalShift := quantizershift - accuShift + 1
			accu = fMultDiv2SS(fdkaacEncMTab34[tabIndex], fdkaacEncQuantTableE[totalShift&3])
			totalShift = (16 - 4) - (3 * (totalShift >> 2))
			accu >>= uint(fixMin(totalShift, dfractBitsQuant-1))
			quaSpectrum[line] = int16((k + accu) >> (dfractBitsQuant - 1 - 16))
		} else {
			quaSpectrum[line] = 0
		}
	}
}

// fdkaacEncInvQuantizeLines is the 1:1 port of FDKaacEnc_invQuantizeLines
// (quantize.cpp:180): mdctSpectrum = iquaSpectrum^4/3 * 2^(0.25*gain).
//
//	static void FDKaacEnc_invQuantizeLines(INT gain, INT noOfLines,
//	                                       SHORT *quantSpectrum,
//	                                       FIXP_DBL *mdctSpectrum);
//
// Defined domain: the C applies an UNCLAMPED `accu <<= n` / `accu >>= n` on a
// 32-bit INT, with n == iquantizershift +/- specExp. The genuine kernel is only
// well-defined where n stays in [0, 31]: its own FDK_ASSERT(specExp < 14) bounds
// |q| <= MAX_QUANT (8191), and the encoder limits the scalefactor so the shift
// count never exceeds 31 (sf_estim.cpp:1133). A count >= 32 is C undefined
// behaviour (clang folds it differently at -O0 vs -O2), so there is no bit-exact
// target outside the domain and the real encoder never reaches it. Go's shift
// (`<< uint(n)`) yields 0 for n >= 32, mirroring neither -O0 nor -O2 — by design,
// since that input is out of the kernel's contract.
func fdkaacEncInvQuantizeLines(gain, noOfLines int, quantSpectrum []int16, mdctSpectrum []int32) {
	iquantizermod := gain & 3
	iquantizershift := gain >> 2

	for line := 0; line < noOfLines; line++ {
		if quantSpectrum[line] < 0 {
			accu := int32(-quantSpectrum[line])

			ex := int(fNorm(accu)) // CountLeadingBits
			accu <<= uint(ex)
			specExp := (dfractBitsQuant - 1) - ex
			// FDK_ASSERT(specExp < 14);

			tabIndex := int(accu>>(dfractBitsQuant-2-mantDigits)) & mantMask

			// s = FDKaacEnc_mTab_4_3Elc[tabIndex];
			s := fdkaacEncMTab43Elc[tabIndex]
			// t = FDKaacEnc_specExpMantTableCombElc[iquantizermod][specExp];
			t := fdkaacEncSpecExpMantTableCombElc[iquantizermod][specExp]

			// accu = fMult(s, t);  -- fMult(FIXP_DBL,FIXP_DBL) == fixmul_DD, which on
			// __ARM_ARCH_8__ (fixmul_arm.h:176-186) is `smull; asr #31` == int64
			// product>>31 (KEEPS bit 31), NOT the generic (fixmuldiv2_DD<<1) the
			// package fMultDD uses. Use the arm8-exact helper (block_switch.go).
			accu = fixmulDDarm8(s, t)

			// specExp = FDKaacEnc_specExpTableComb[iquantizermod][specExp] - 1;
			specExp = int(fdkaacEncSpecExpTableComb[iquantizermod][specExp]) - 1

			if (-iquantizershift - specExp) < 0 {
				accu <<= uint(-(-iquantizershift - specExp))
			} else {
				accu >>= uint(-iquantizershift - specExp)
			}

			mdctSpectrum[line] = -accu
		} else if quantSpectrum[line] > 0 {
			accu := int32(quantSpectrum[line])

			ex := int(fNorm(accu))
			accu <<= uint(ex)
			specExp := (dfractBitsQuant - 1) - ex

			tabIndex := int(accu>>(dfractBitsQuant-2-mantDigits)) & mantMask

			s := fdkaacEncMTab43Elc[tabIndex]
			t := fdkaacEncSpecExpMantTableCombElc[iquantizermod][specExp]

			// accu = fMult(s, t);  -- fixmul_DD == arm8 smull>>31 (see negative branch).
			accu = fixmulDDarm8(s, t)

			specExp = int(fdkaacEncSpecExpTableComb[iquantizermod][specExp]) - 1

			if (-iquantizershift - specExp) < 0 {
				accu <<= uint(-(-iquantizershift - specExp))
			} else {
				accu >>= uint(-iquantizershift - specExp)
			}

			mdctSpectrum[line] = accu
		} else {
			mdctSpectrum[line] = 0
		}
	}
}

// fdkaacEncQuantizeSpectrum is the 1:1 port of FDKaacEnc_QuantizeSpectrum
// (quantize.cpp:278): quantize the whole spectrum, driving quantizeLines over
// each (group,sfb) with QSS == globalGain-scalefactor.
//
//	void FDKaacEnc_QuantizeSpectrum(INT sfbCnt, INT maxSfbPerGroup, INT sfbPerGroup,
//	                                const INT *sfbOffset, const FIXP_DBL *mdctSpectrum,
//	                                INT globalGain, const INT *scalefactors,
//	                                SHORT *quantizedSpectrum, INT dZoneQuantEnable);
func fdkaacEncQuantizeSpectrum(sfbCnt, maxSfbPerGroup, sfbPerGroup int, sfbOffset []int,
	mdctSpectrum []int32, globalGain int, scalefactors []int, quantizedSpectrum []int16,
	dZoneQuantEnable bool) {
	for sfbOffs := 0; sfbOffs < sfbCnt; sfbOffs += sfbPerGroup {
		for sfb := 0; sfb < maxSfbPerGroup; sfb++ {
			scalefactor := scalefactors[sfbOffs+sfb]

			fdkaacEncQuantizeLines(
				globalGain-scalefactor, // QSS
				sfbOffset[sfbOffs+sfb+1]-sfbOffset[sfbOffs+sfb],
				mdctSpectrum[sfbOffset[sfbOffs+sfb]:],
				quantizedSpectrum[sfbOffset[sfbOffs+sfb]:],
				dZoneQuantEnable)
		}
	}
}

// fdkaacEncCalcSfbDist is the 1:1 port of FDKaacEnc_calcSfbDist
// (quantize.cpp:312): the ld-domain quantization distortion of one band for a
// trial gain. Quantizes each line, returns FL2FXCONST_DBL(0) on a MAX_QUANT
// overflow, otherwise accumulates (invQuant - mdct/2)^2 in block-floating-point
// and returns CalcLdData(sum). quantSpectrum is written with the trial
// quantization (the C reuses the caller's scratch buffer).
//
//	FIXP_DBL FDKaacEnc_calcSfbDist(const FIXP_DBL *mdctSpectrum, SHORT *quantSpectrum,
//	                               INT noOfLines, INT gain, INT dZoneQuantEnable);
func fdkaacEncCalcSfbDist(mdctSpectrum []int32, quantSpectrum []int16, noOfLines, gain int, dZoneQuantEnable bool) int32 {
	var xfsf int32 = 0
	var invQuantSpec [1]int32

	for i := 0; i < noOfLines; i++ {
		// quantization
		fdkaacEncQuantizeLines(gain, 1, mdctSpectrum[i:i+1], quantSpectrum[i:i+1], dZoneQuantEnable)

		if int(fixpAbsShort(quantSpectrum[i])) > maxQuant {
			return 0
		}
		// inverse quantization
		fdkaacEncInvQuantizeLines(gain, 1, quantSpectrum[i:i+1], invQuantSpec[:])

		// dist
		diff := fixabsD(fixabsD(invQuantSpec[0]) - fixabsD(mdctSpectrum[i]>>1))

		scale := int(fNorm(diff)) // CountLeadingBits
		diff = scaleValue(diff, int32(scale))
		diff = fMultDD(diff, diff) // fPow2
		scale = fixMin(2*(scale-1), dfractBitsQuant-1)

		diff = scaleValue(diff, int32(-scale))

		xfsf = xfsf + diff
	}

	xfsf = calcLdData(xfsf)

	return xfsf
}

// fdkaacEncCalcSfbQuantEnergyAndDist is the 1:1 port of
// FDKaacEnc_calcSfbQuantEnergyAndDist (quantize.cpp:361): the ld-domain
// quantized energy AND distortion of one band for already-quantized lines.
// Returns en, dist; on a MAX_QUANT overflow both are 0.
//
//	void FDKaacEnc_calcSfbQuantEnergyAndDist(FIXP_DBL *mdctSpectrum, SHORT *quantSpectrum,
//	                                         INT noOfLines, INT gain, FIXP_DBL *en, FIXP_DBL *dist);
func fdkaacEncCalcSfbQuantEnergyAndDist(mdctSpectrum []int32, quantSpectrum []int16, noOfLines, gain int) (en, dist int32) {
	var invQuantSpec [1]int32

	var energy int32 = 0
	var distortion int32 = 0

	for i := 0; i < noOfLines; i++ {
		if int(fixpAbsShort(quantSpectrum[i])) > maxQuant {
			return 0, 0
		}

		// inverse quantization
		fdkaacEncInvQuantizeLines(gain, 1, quantSpectrum[i:i+1], invQuantSpec[:])

		// energy
		energy += fMultDD(invQuantSpec[0], invQuantSpec[0]) // fPow2

		// dist
		diff := fixabsD(fixabsD(invQuantSpec[0]) - fixabsD(mdctSpectrum[i]>>1))

		scale := int(fNorm(diff))
		diff = scaleValue(diff, int32(scale))
		diff = fMultDD(diff, diff) // fPow2

		scale = fixMin(2*(scale-1), dfractBitsQuant-1)

		diff = scaleValue(diff, int32(-scale))

		distortion += diff
	}

	en = calcLdData(energy) + fl2fxconstDBL(0.03125)
	dist = calcLdData(distortion)
	return en, dist
}

// fixpAbsShort returns the absolute value of a SHORT (FIXP_SGL). C counterpart:
// fAbs(SHORT) == fixabs_S (common_fix.h:276 / abs.h). Used by the MAX_QUANT
// overflow checks above (fAbs(quantSpectrum[i])).
func fixpAbsShort(v int16) int16 {
	if v > 0 {
		return v
	}
	return -v
}

// dfractBitsQuant mirrors DFRACT_BITS == 32 (common_fix.h:113) for the quantizer
// shift expressions. Named locally to keep this file self-documenting against
// the C `DFRACT_BITS` spellings.
const dfractBitsQuant = 32
