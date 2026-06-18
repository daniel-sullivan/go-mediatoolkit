// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Exported C-API-shaped views of the shared libFDK fixed-point primitives the
// SBR high-frequency-generation batch (lpp_tran.cpp / HFgen_preFlat.cpp /
// hbe.cpp, ported in internal/nativeaac/sbr) calls. They add NO logic: each is a
// thin adapter onto the already-ported AAC-LC kernel, reshaped to the C
// out-parameter calling convention (e.g. fDivNorm(num, denom, &scale)) the SBR
// code uses verbatim. This keeps ONE coherent definition of each kernel in this
// package — the SBR port never re-ports a libFDK math primitive.
//
// All values are int32 FIXP_DBL (Q-format) / int16 FIXP_SGL (Q1.15); every
// kernel here is a pure integer kernel, so EXACT-integer parity holds in any
// build (cf. the integer-kernel note in nativeaac.go).

// --- min / max / abs / norm -----------------------------------------------

// FMaxI is fMax over plain int (fixMax in the SBR sources operates on INT).
func FMaxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// FMinI is fMin over plain int (fixMin in the SBR sources operates on INT).
func FMinI(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// FMaxDBL is fMax over FIXP_DBL (common_fix.h:236).
func FMaxDBL(a, b int32) int32 { return fMax(a, b) }

// FMinDBL is fMin over FIXP_DBL (common_fix.h:235).
func FMinDBL(a, b int32) int32 { return fMin(a, b) }

// FixpAbs is fixp_abs / fAbs over FIXP_DBL (common_fix.h:200): saturating
// absolute value (MINVAL_DBL -> MAXVAL_DBL).
func FixpAbs(x int32) int32 { return fAbsDBL(x) }

// CntLeadingZeros is CntLeadingZeros / fNormz over FIXP_DBL (common_fix.h:292):
// the number of leading zero bits, 0 for the zero input.
func CntLeadingZeros(x int32) int { return fNormzPos(x) }

// CountLeadingBits is CountLeadingBits / fNorm over FIXP_DBL: leading sign bits
// minus one (the shift needed to normalise |x| to bit 30).
func CountLeadingBits(x int32) int { return int(fNorm(x)) }

// GetScalefactor is getScalefactor(FIXP_DBL*, INT) (scale.cpp): the common
// headroom (min CountLeadingBits over the vector), 0 for an all-zero vector.
func GetScalefactor(vector []int32, length int) int {
	return int(getScalefactor(vector, int32(length)))
}

// GetInvInt is GetInvInt(int) (fixpoint_math.h): 1/intValue in Q1.31 from the
// invCount ROM. The HFgen pre-flattening (sbrDecoder_calculateGainVec) uses it
// for the per-band / per-slot mean.
func GetInvInt(intValue int) int32 { return getInvInt(intValue) }

// --- multiply -------------------------------------------------------------

// FMultDD is the full-scale FIXP_DBL*FIXP_DBL product (fixmul_DD == fMult).
func FMultDD(a, b int32) int32 { return fMult(a, b) }

// FMultDiv2DD is the FIXP_DBL*FIXP_DBL product scaled down by 2 (fixmuldiv2_DD).
func FMultDiv2DD(a, b int32) int32 { return fMultDiv2DD(a, b) }

// CalcLdInt is CalcLdInt(INT i) (fixpoint_math.cpp:378): ld(i) in Q-format, the
// log-dualis the SBR freq-scale FDK_getNumOctavesDiv8 builds on.
func CalcLdInt(i int32) int32 { return calcLdInt(i) }

// FPow2 is fPow2 / fixpow2_D(x) == x*x full scale (common_fix via fPow2Div2<<1).
func FPow2(a int32) int32 { return fPow2(a) }

// FPow2Div2 is fPow2Div2 / fixpow2div2_D(x): (x*x) scaled down by 2.
func FPow2Div2(a int32) int32 { return fPow2Div2(a) }

// --- scale / saturate ------------------------------------------------------

// ScaleValueSaturate is scaleValueSaturate(FIXP_DBL, INT) (scale.h): scale by
// 2^scalefactor saturating to ±MAXVAL_DBL.
func ScaleValueSaturate(value, scalefactor int32) int32 {
	return scaleValueSaturate(value, scalefactor)
}

// ScaleValues is scaleValues(FIXP_DBL*, INT, INT) (scale.cpp): scale a vector by
// 2^scalefactor with a logical-by-sign shift (no saturation). The lpp transposer
// temporal-buffer rescale uses it.
func ScaleValues(vector []int32, length int, scalefactor int32) {
	for i := 0; i < length; i++ {
		vector[i] = scaleValue(vector[i], scalefactor)
	}
}

// ScaleValuesSaturate is scaleValuesSaturate(FIXP_DBL*, INT, INT) (scale.cpp):
// scale a vector by 2^scalefactor with saturation. hbe.cpp's overlap shifting
// uses it.
func ScaleValuesSaturate(vector []int32, length int, scalefactor int32) {
	scaleValuesSaturateInPlace(vector, length, scalefactor)
}

// --- divide / log / pow (C out-parameter forms) ----------------------------

// FDivNorm is fDivNorm(FIXP_DBL num, FIXP_DBL denom, INT *result_e)
// (fixpoint_math.cpp): the normalised division, returning the mantissa and
// writing the exponent through the return. The lpp transposer's reflection /
// filter-coefficient quick-checks use it.
func FDivNorm(num, denom int32) (mantissa, resultE int32) { return fDivNorm(num, denom) }

// F2Pow is f2Pow(FIXP_DBL exp_m, INT exp_e, INT *result_e)
// (fixpoint_math.cpp:638): 2^exp returning mantissa + exponent. HFgen's gain-vec
// antilog uses this exponent-carrying form.
func F2Pow(expM, expE int32) (mantissa, resultE int32) { return f2PowWithExp(expM, expE) }

// CalcLog2 is CalcLog2(FIXP_DBL arg, INT arg_e, INT *result_e)
// (fixpoint_math.cpp:763) == fLog2(arg, arg_e, result_e): base-2 logarithm,
// returning mantissa + exponent. HFgen converts band energy to dB through it.
func CalcLog2(argM, argE int32) (mantissa, resultE int32) { return fLog2WithExp(argM, argE) }

// Fl2fxconstDBL / Fl2fxconstSGL (FL2FXCONST_DBL / _SGL constant narrowing) are
// already exported by qmf_export.go; the SBR ROM here reuses those — no second
// definition. The whitening / hbe twiddle ROM materialises through them.

// FxDbl2FxSgl is the runtime FX_DBL2FX_SGL macro (common_fix.h:220):
// (FIXP_SGL)(val >> (DFRACT_BITS - FRACT_BITS)) == a plain TRUNCATING right shift
// by 16. This is distinct from StcNarrow / FX_DBL2FXCONST_SGL (the rounding,
// saturating compile-time narrow); the LPP transposer's alpha/a0/a1 coefficient
// narrowing uses the truncating runtime form.
func FxDbl2FxSgl(val int32) int16 { return int16(val >> 16) }

// FMultSS is the full-scale FIXP_SGL*FIXP_SGL product fixmul_SS(a,b) == (a*b)<<1
// (fixmul.h:166), returning FIXP_DBL. The LPP transposer's bandwidth-expansion
// fMult(bw, alpha) (both FIXP_SGL) routes through this.
func FMultSS(a, b int16) int32 { return (int32(a) * int32(b)) << 1 }

// FMultDiv2SS is the FIXP_SGL*FIXP_SGL product scaled down by 2 fixmuldiv2_SS ==
// (a*b) (fixmul.h:159). The LPP quadratic criterion fMultDiv2(alpha, alpha) and
// the a0/a1 * lowBand MAC chain (FIXP_SGL coeff × FIXP_DBL data) build on the
// SS / SD forms.
func FMultDiv2SS(a, b int16) int32 { return int32(a) * int32(b) }

// FPow2S is fPow2(FIXP_SGL) == fixpow2_S(a) == fixmuldiv2_SS(a,a)<<1
// (fixmul.h:287-292), returning FIXP_DBL. The transposer squares bw (FIXP_SGL).
func FPow2S(a int16) int32 { return (int32(a) * int32(a)) << 1 }

// fMultDiv2(FIXP_SGL a, FIXP_DBL b) == fixmuldiv2_SD commutes to fixmuldiv2_DS(b,
// a) (fixmul.h:172-174); the LPP a0r*lowBandReal MAC term uses the existing
// FMultDiv2DS(data, coeff) with the args in that order — no separate SD export.
