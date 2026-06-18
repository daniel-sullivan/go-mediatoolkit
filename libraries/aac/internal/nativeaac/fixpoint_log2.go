// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Fixed-point base-2 logarithm helpers ported 1:1 from the vendored FDK-AAC
// reference (libFDK/include/fixpoint_math.h + libFDK/src/fixpoint_math.cpp).
// fLog2/CalcLdData/LdDataVector convert FIXP_DBL band energies to the "ldData"
// (log2/LD_DATA_SCALING) domain the AAC encoder psychoacoustic model carries
// throughout (sfbEnergyLdData, sfbThresholdLdData, …). They are pure-integer
// Q-format kernels: a normalisation, a Taylor polynomial over the ldCoeff ROM,
// and shift/add scaling — bit-identical regardless of vectorization, no float,
// no transcendental.

// LD-domain scaling constants (fixpoint_math.h:113-117).
//
//	#define LD_DATA_SCALING (64.0f)
//	#define LD_DATA_SHIFT 6   // pow(2, LD_DATA_SHIFT) = LD_DATA_SCALING
//	#define MAX_LD_PRECISION 10
//	#define LD_PRECISION 10
const (
	ldDataShift = 6
	ldPrecision = 10
)

// ldCoeff is the 1:1 transcription of ldCoeff[MAX_LD_PRECISION]
// (fixpoint_math.h:131-138): the Taylor coefficients -1/(i+1) of ln(1-x). On the
// build platform (aarch64 -> __arm__ + __ARM_ARCH_8__, FDK_archdef.h:182-186)
// LDCOEFF_16BIT is defined, so ldCoeff is the FIXP_SGL (Q1.15 int16) variant,
// NOT the FIXP_DBL one — and the Taylor accumulation below multiplies a FIXP_DBL
// by these FIXP_SGL coefficients (the fixmadddiv2_SD path). FL2FXCONST_SGL(-1.0)
// saturates to MAXVAL_SGL's negative, i.e. -32768.
//
//	static const FIXP_SGL ldCoeff[MAX_LD_PRECISION] = {
//	    FL2FXCONST_SGL(-1.0),       FL2FXCONST_SGL(-1.0 / 2.0),
//	    FL2FXCONST_SGL(-1.0 / 3.0), FL2FXCONST_SGL(-1.0 / 4.0),
//	    FL2FXCONST_SGL(-1.0 / 5.0), FL2FXCONST_SGL(-1.0 / 6.0),
//	    FL2FXCONST_SGL(-1.0 / 7.0), FL2FXCONST_SGL(-1.0 / 8.0),
//	    FL2FXCONST_SGL(-1.0 / 9.0), FL2FXCONST_SGL(-1.0 / 10.0)};
var ldCoeff = [maxLdPrecision]int16{
	fl2fxconstSGL(-1.0),
	fl2fxconstSGL(-1.0 / 2.0),
	fl2fxconstSGL(-1.0 / 3.0),
	fl2fxconstSGL(-1.0 / 4.0),
	fl2fxconstSGL(-1.0 / 5.0),
	fl2fxconstSGL(-1.0 / 6.0),
	fl2fxconstSGL(-1.0 / 7.0),
	fl2fxconstSGL(-1.0 / 8.0),
	fl2fxconstSGL(-1.0 / 9.0),
	fl2fxconstSGL(-1.0 / 10.0),
}

const maxLdPrecision = 10

// fNorm is the 1:1 port of fixnorm_D (libFDK/include/clz.h:193): count leading
// ones/zeros of a FIXP_DBL minus 1 (0 for a zero input). fNorm(x) (common_fix.h)
// and CountLeadingBits(x) (common_fix.h:309) resolve to this.
//
//	inline INT fixnorm_D(FIXP_DBL val) {
//	  INT leadingBits = 0;
//	  if (val != 0) {
//	    if (val < 0) val = ~val;
//	    leadingBits = fixnormz_D(val) - 1;
//	  }
//	  return leadingBits;
//	}
func fNorm(val int32) int32 {
	if val == 0 {
		return 0
	}
	if val < 0 {
		val = ^val
	}
	return fixnormzD(val) - 1
}

// fLog2WithExp is the 1:1 port of fLog2(x_m, x_e, *result_e)
// (fixpoint_math.h:807-865): log2(x_m * 2^x_e) returning the mantissa and (via
// the second return) the result exponent. Negative/zero input short-circuits to
// (-1.0, DFRACT_BITS-1).
//
// On the aarch64 target ldCoeff is FIXP_SGL, so
// fMultAddDiv2(result_m, ldCoeff[i], px2_m) takes the (FIXP_DBL, FIXP_SGL,
// FIXP_DBL) overload == fixmadddiv2_SD (common_fix.h:317). The arm header
// defines FUNCTION_fixmadddiv2_DS (not _SD), so the generic fixmadddiv2_SD
// (fixmadd.h:130) routes to fixmadddiv2_DS(x, b, a) (operands swapped); the arm
// fixmadddiv2_DS (fixmadd_arm.h:177) is `smlawb` == x + (px2_m * ldCoeff)>>16 ==
// result_m + fMultDiv2DS(px2_m, ldCoeff). px2_m = fMult(px2_m, x2_m) uses the
// arm fixmul_DD == (a*b)>>31 (fixmulDDarm8, KEEPING bit 31), not the generic
// (..>>32)<<1 — this LSB difference is load-bearing across the iterations.
func fLog2WithExp(xM, xE int32) (resultM, resultE int32) {
	// Short cut for zero and negative numbers.
	if xM <= 0 {
		return fl2fxconstDBL(-1.0), dfractBits - 1
	}

	// Move input value x_m * 2^x_e toward 1.0 (most accurate Taylor region).
	// C uses fNormz (== fixnormz_D == CntLeadingZeros), NOT fNorm.
	bNorm := fixnormzD(xM) - 1
	x2M := xM << uint(bNorm)
	xE = xE - bNorm

	// map x from log(x) domain to log(1-x) domain.
	x2M = -(x2M + fl2fxconstDBL(-1.0))

	// Taylor polynomial approximation of ln(1-x).
	resultM = 0
	px2M := x2M
	for i := 0; i < ldPrecision; i++ {
		resultM = resultM + fMultDiv2DS(px2M, ldCoeff[i])
		px2M = fixmulDDarm8(px2M, x2M)
	}

	// Multiply result with 1/ln(2) (get log2(x) from ln(x) result).
	resultM = fMultAddDiv2(resultM, resultM,
		fl2fxconstDBL(2.0*0.4426950408889634073599246810019))

	// Add exponent part. log2(x_m * 2^x_e) = log2(x_m) + x_e
	if xE != 0 {
		enorm := dfractBits - fNorm(xE)
		resultM = (resultM >> uint(enorm-1)) +
			(xE << uint(dfractBits-1-enorm))
		resultE = enorm
	} else {
		resultE = 1
	}
	return resultM, resultE
}

// fLog2 is the 1:1 port of fLog2(x_m, x_e) (fixpoint_math.h:876-887): the
// implicit-exponent form returning log2(x_m * 2^x_e) scaled to the LD_DATA_SHIFT
// domain. Negative/zero input yields -1.0.
func fLog2(xM, xE int32) int32 {
	if xM <= 0 {
		return fl2fxconstDBL(-1.0)
	}
	m, resultE := fLog2WithExp(xM, xE)
	return scaleValue(m, resultE-ldDataShift)
}

// calcLdData is the 1:1 port of the CalcLdData(op) macro (fixpoint_math.h:219):
// CalcLdData(op) == fLog2(op, 0).
func calcLdData(op int32) int32 { return fLog2(op, 0) }

// ldDataVector is the 1:1 port of LdDataVector (fixpoint_math.cpp:117): apply
// CalcLdData (fLog2(.,0)) element-wise over the first n entries of src into dst.
//
//	void LdDataVector(FIXP_DBL *srcVector, FIXP_DBL *destVector, INT n) {
//	  for (INT i = 0; i < n; i++) destVector[i] = fLog2(srcVector[i], 0);
//	}
func ldDataVector(src, dst []int32, n int) {
	for i := 0; i < n; i++ {
		dst[i] = fLog2(src[i], 0)
	}
}
