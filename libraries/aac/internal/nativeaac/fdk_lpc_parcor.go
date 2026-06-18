// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// LeRoux-Gueguen/Schur ParCor (reflection-coefficient) analysis, ported 1:1
// from the vendored FDK-AAC libFDK module. This is the LPC analysis the TNS
// encode decision (FDKaacEnc_TnsDetect) drives: given a windowed
// autocorrelation it produces the lattice (reflection) coefficients plus a
// prediction gain (mantissa/exponent). It pulls in two fixed-point division
// primitives (schur_div / fDivNormSigned) from libFDK/fixpoint_math.
//
// Every value is an int32 FIXP_DBL / int16 FIXP_SGL Q-format quantity; the
// whole computation is pure integer (count-leading-bits, arithmetic shifts,
// schur restoring division, the arm8 fixmul_DD) with no float and no
// transcendental, so it is bit-identical regardless of vectorization and is
// fenced only by aacfdk (no aac_strict split).
//
// Type mapping on the aarch64 build target (FDK_archdef.h:166 ->
// __ARM_ARCH_8__): FIXP_DBL == int32, FIXP_LPC == FIXP_SGL == int16
// (FDK_lpc.h:128), FRACT_BITS == 16, DFRACT_BITS == 32, and fMult(LONG,LONG) ==
// fixmul_DD == the arm8 (a*b)>>31 form (fixmulDDarm8, keeping bit 31).

// fxDbl2FxLpc ports FX_DBL2FX_LPC(x) (FDK_lpc.h:129) ==
// FX_DBL2FX_SGL((FIXP_DBL)(x)) == (FIXP_SGL)((x) >> (DFRACT_BITS - FRACT_BITS))
// (common_fix.h:220): a plain truncating narrowing of an int32 Q1.31 to an
// int16 Q1.15 (NOT the rounding FX_DBL2FXCONST_SGL).
func fxDbl2FxLpc(x int32) int16 {
	return int16(x >> 16)
}

// fAbsDBL ports fAbs(FIXP_DBL) == fixabs_D (abs.h:121): x>0 ? x : -x.
func fAbsDBL(x int32) int32 {
	if x > 0 {
		return x
	}
	return -x
}

// schurDiv ports schur_div(FIXP_DBL num, FIXP_DBL denum, INT count)
// (fixpoint_math.cpp:402-422): restoring binary division delivering num/denum
// with `count`-bit accuracy. Preconditions (asserted in C): num >= 0,
// denum > 0, num <= denum.
//
//	INT L_num = (LONG)num >> 1;
//	INT L_denum = (LONG)denum >> 1;
//	INT div = 0, k = count;
//	if (L_num != 0)
//	  while (--k) {
//	    div <<= 1; L_num <<= 1;
//	    if (L_num >= L_denum) { L_num -= L_denum; div++; }
//	  }
//	return (FIXP_DBL)(div << (DFRACT_BITS - count));
func schurDiv(num, denum, count int32) int32 {
	lNum := num >> 1
	lDenum := denum >> 1
	div := int32(0)
	k := count

	if lNum != 0 {
		for {
			k--
			if k == 0 {
				break
			}
			div <<= 1
			lNum <<= 1
			if lNum >= lDenum {
				lNum -= lDenum
				div++
			}
		}
	}
	return div << uint(dfractBits-count)
}

// fDivNormSigned ports fDivNormSigned(FIXP_DBL L_num, FIXP_DBL L_denum,
// INT *result_e) (fixpoint_math.cpp:527-562): normalised signed division
// returning the mantissa and (via the second return) the result exponent.
// CountLeadingBits == fNorm; FRACT_BITS == 16.
func fDivNormSigned(lNum, lDenum int32) (div, resultE int32) {
	sign := (lNum >= 0) != (lDenum >= 0)

	if lNum == 0 {
		return 0, 0
	}
	if lDenum == 0 {
		return int32(0x7FFFFFFF), 14 // MAXVAL_DBL
	}

	normNum := fNorm(lNum)
	lNum = lNum << uint(normNum)
	lNum = lNum >> 2
	lNum = fAbsDBL(lNum)
	resultE = -normNum + 1

	normDen := fNorm(lDenum)
	lDenum = lDenum << uint(normDen)
	lDenum = lDenum >> 1
	lDenum = fAbsDBL(lDenum)
	resultE -= -normDen

	div = schurDiv(lNum, lDenum, fractBits) // FRACT_BITS == 16

	if sign {
		div = -div
	}
	return div, resultE
}

// clpcMaxOrder mirrors LPC_MAX_ORDER (FDK_lpc.h:108).
const clpcMaxOrder = 24

// fractBits mirrors FRACT_BITS (common_fix.h:112).
const fractBits = 16

// clpcAutoToParcor ports CLpc_AutoToParcor(FIXP_DBL acorr[], const int acorr_e,
// FIXP_LPC reflCoeff[], const int numOfCoeff, FIXP_DBL *pPredictionGain_m,
// INT *pPredictionGain_e) (FDK_lpc.cpp:431-487): the LeRoux-Gueguen/Schur
// lattice recursion turning an autocorrelation into reflection coefficients
// plus the signal-power/error-power prediction gain.
//
// acorr is consumed/modified in place (matching the C, which mutates acorr[]).
// acorr_e is unused by the reference (the gain is computed from autoCorr_0 and
// the in-place-updated acorr[0], both implicitly in the same exponent). fMult ==
// fixmul_DD == fixmulDDarm8 on the target.
func clpcAutoToParcor(acorr []int32, reflCoeff []int16, numOfCoeff int) (predictionGainM, predictionGainE int32) {
	var i, j, scale int32

	var parcorWorkBuffer [clpcMaxOrder]int32
	wbBase := parcorWorkBuffer[:]
	wbOff := 0 // C advances workBuffer++; track as an offset into wbBase.

	autoCorr0 := acorr[0]

	for k := 0; k < numOfCoeff; k++ {
		reflCoeff[k] = 0
	}

	if autoCorr0 == 0 {
		// *pPredictionGain_m = FL2FXCONST_DBL(0.5f); *pPredictionGain_e = 1;
		return fl2fxconstDBL(0.5), 1
	}

	// FDKmemcpy(workBuffer, acorr + 1, numOfCoeff * sizeof(FIXP_DBL));
	for k := 0; k < numOfCoeff; k++ {
		wbBase[k] = acorr[1+k]
	}

	for i = 0; i < int32(numOfCoeff); i++ {
		w0 := wbBase[wbOff]
		// LONG sign = ((LONG)workBuffer[0] >> (DFRACT_BITS - 1));
		sign := w0 >> 31
		// FIXP_DBL tmp = (FIXP_DBL)((LONG)workBuffer[0] ^ sign);
		tmp := w0 ^ sign

		// if (acorr[0] < tmp) break;
		if acorr[0] < tmp {
			break
		}

		// tmp = (FIXP_DBL)((LONG)schur_div(tmp, acorr[0], FRACT_BITS) ^ (~sign));
		tmp = schurDiv(tmp, acorr[0], fractBits) ^ (^sign)

		reflCoeff[i] = fxDbl2FxLpc(tmp)

		for j = int32(numOfCoeff) - i - 1; j >= 0; j-- {
			accu1 := fixmulDDarm8(tmp, acorr[j])
			accu2 := fixmulDDarm8(tmp, wbBase[wbOff+int(j)])
			wbBase[wbOff+int(j)] += accu1
			acorr[j] += accu2
		}

		// if (acorr[0] == 0) break;
		if acorr[0] == 0 {
			break
		}

		wbOff++ // workBuffer++
	}

	if acorr[0] > 0 {
		// prediction gain = signal power / error (residual) power
		predictionGainM, scale = fDivNormSigned(autoCorr0, acorr[0])
		predictionGainE = scale
	} else {
		predictionGainM = 0
		predictionGainE = 0
	}
	return predictionGainM, predictionGainE
}
