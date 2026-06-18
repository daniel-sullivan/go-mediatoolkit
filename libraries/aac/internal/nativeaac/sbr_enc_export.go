// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Exported views of the shared libFDK fixed-point kernels the SBR ENCODER
// analysis tools (internal/nativeaac/sbr: env_est / fram_gen / tran_det /
// mh_det) build on, BEYOND the surface the SBR QMF and HF-gen batches already
// export (sbr_qmf_export.go / sbr_hfgen_export.go / band_nrg_parity_export.go).
// Like those, these add no logic; they keep ONE coherent definition of each
// kernel in this package and let the sbr package reuse them bit-for-bit instead
// of re-porting. The three short helpers the AAC-LC core never needed
// (fAdjust / fAddNorm / fIsLessThan) are ported 1:1 here from
// libFDK/include/fixpoint_math.h.
//
// Reuses (do NOT redeclare): FixpAbs (==fAbs), FMultDD (==fMult), FMultDiv2DD
// (==fMultDiv2), ScaleValue, ScaleValueSaturate, ScaleValuesSaturate, FPow2,
// FPow2Div2, FDivNorm (3-arg form), CalcLdData (==fLog2(op,0)), CalcLog2
// (==fLog2 with exp), GetInvInt, CountLeadingBits (==fNorm, int), CntLeadingZeros
// (==fNormz, int), FMinI/FMaxI, Fl2fxconstDBL/SGL.
//
// All values are int32 FIXP_DBL (Q-format); every kernel is a pure integer
// kernel (cf. nativeaac.go), so EXACT-integer parity holds in any build.

// FMultAddDiv2 is fMultAddDiv2(FIXP_DBL x, FIXP_DBL a, FIXP_DBL b) == x + 0.5*a*b
// (common_fix.h). The DBL,DBL,DBL overload (calculateThresholds, addLowband
// accumulators) — distinct from the FMultAddDiv2SD the QMF exports.
func FMultAddDiv2(x, a, b int32) int32 { return fMultAddDiv2(x, a, b) }

// FMultI multiplies a FIXP_DBL by a plain INT (fMultI, fixpoint_math.h).
// FDKsbrEnc_frameSplitter uses it for sbrSlots = fMultI(GetInvInt(timeStep),
// no_cols).
func FMultI(a, b int32) int32 { return fMultI(a, b) }

// SchurDiv is schur_div(num, denum, count) (fixpoint_math.cpp): the bit-serial
// fixed-point division the MH detector's FDKsbrEnc_LSI_divide_scale_fract uses.
func SchurDiv(num, denum, count int32) int32 { return schurDiv(num, denum, count) }

// FPow2AddDiv2 is fPow2AddDiv2(x, a) == x + fMultDiv2(a, a) (band_nrg): the QMF
// energy accumulator the envelope estimator's getEnergyFromCplxQmfData uses
// (energy = fPow2AddDiv2(fPow2Div2(re), im)).
func FPow2AddDiv2(x, a int32) int32 { return fPow2AddDiv2(x, a) }

// FAddSaturate is the saturating FIXP_DBL add (fAddSaturate, fixpoint_math).
func FAddSaturate(a, b int32) int32 { return fAddSaturate(a, b) }

// SqrtFixp is the FIXP_DBL square root over [0,1) Q-format (sqrtFixp).
func SqrtFixp(op int32) int32 { return sqrtFixp(op) }

// InvSqrtNorm2 is invSqrtNorm2(op, &shift): 1/sqrt(op) with the result exponent
// returned as the second value.
func InvSqrtNorm2(op int32) (result, shift int32) { return invSqrtNorm2(op) }

// FLog2 is fLog2(x_m, x_e) (the 2-arg form returning a plain FIXP_DBL):
// log2 of x_m*2^x_e. spectralChange uses fLog2(accu, accu_e).
func FLog2(xM, xE int32) int32 { return fLog2(xM, xE) }

// CalcInvLdData is CalcInvLdData(op): 2^op (the inverse of CalcLdData), the
// fast-transient-detector dBf antilog uses it.
func CalcInvLdData(op int32) int32 { return calcInvLdData(op) }

// FDivNorm0 is the 2-arg fDivNorm(num, denum) (exponent forced to 0). Used by
// FDKsbrEnc_InitSbrTransientDetector's framedur_fix.
func FDivNorm0(num, denom int32) int32 { return fDivNorm2(num, denom) }

// FMultNorm is fMultNorm(f1, f2, &result_e): normalized product returning the
// mantissa and result exponent.
func FMultNorm(f1, f2 int32) (product, resultE int32) { return fMultNorm(f1, f2) }

// FMultNorm5 is fMultNorm(f1_m, f1_e, f2_m, f2_e, result_e): the fully-specified
// normalized product to a fixed result exponent (the fast-transient dBf chain).
func FMultNorm5(f1m, f1e, f2m, f2e, resultE int32) int32 {
	return fMultNorm5(f1m, f1e, f2m, f2e, resultE)
}

// --- The three short helpers the AAC-LC core never needed ------------------

// FAdjust is the 1:1 port of fAdjust(a_m, &a_e) (fixpoint_math.h:642): normalize
// a_m to one headroom bit and account the shift in the exponent.
//
//	inline FIXP_DBL fAdjust(FIXP_DBL a_m, INT *pA_e) {
//	  INT shift = fNorm(a_m) - 1;
//	  *pA_e -= shift;
//	  return scaleValue(a_m, shift);
//	}
func FAdjust(aM, aE int32) (mant, exp int32) {
	shift := fNorm(aM) - 1
	aE -= shift
	return scaleValue(aM, shift), aE
}

// FAddNorm is the 1:1 port of fAddNorm(a_m, a_e, b_m, b_e, &result_e)
// (fixpoint_math.h:662): add two scaled FIXP_DBL values, normalizing first, and
// return the sum mantissa and (second value) the chosen result exponent.
// addLowbandEnergies uses it.
func FAddNorm(aM, aE, bM, bE int32) (resultM, resultE int32) {
	if aM == 0 {
		return bM, bE
	}
	if bM == 0 {
		return aM, aE
	}
	aM, aE = FAdjust(aM, aE)
	bM, bE = FAdjust(bM, bE)
	if aE > bE {
		resultM = aM + (bM >> uint(fixMin(int(aE-bE), dfractBits-1)))
		resultE = aE
	} else {
		resultM = (aM >> uint(fixMin(int(bE-aE), dfractBits-1))) + bM
		resultE = bE
	}
	return resultM, resultE
}

// FIsLessThan is the 1:1 port of fIsLessThan(a_m, a_e, b_m, b_e)
// (fixpoint_math.h:173): true iff a_m*2^a_e < b_m*2^b_e. FDKsbrEnc_frameSplitter
// (split threshold) and the fast transient detector compare through it.
func FIsLessThan(aM, aE, bM, bE int32) bool {
	n := fNorm(aM)
	aM <<= uint(n)
	aE -= n
	n = fNorm(bM)
	bM <<= uint(n)
	bE -= n
	if aM == 0 {
		aE = bE
	}
	if bM == 0 {
		bE = aE
	}
	if aE > bE {
		return (bM >> uint(fixMin(int(aE-bE), dfractBits-1))) > aM
	}
	return (aM >> uint(fixMin(int(bE-aE), dfractBits-1))) < bM
}

// FPow is fPow(base_m, base_e, exp_m, exp_e) (fixpoint_pow.go:141): returns
// base^exp as a fixed-point mantissa + binary exponent. Exported for the SBR
// noise-floor estimator (nf_est.cpp init).
func FPow(baseM, baseE, expM, expE int32) (result, resultE int32) {
	return fPow(baseM, baseE, expM, expE)
}
