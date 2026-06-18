// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Mantissa/exponent fixed-point helpers the SBR envelope-gain calculation
// (env_calc.cpp) consumes but that were not yet ported by the symbol-decode
// (env_dec.cpp) batch. The env_dec batch ported the FIXP_SGL FDK_add_MantExp /
// FDK_divide_MantExp pair (addMantExpSGL/divideMantExpSGL); env_calc needs the
// FIXP_DBL overloads plus FDK_sqrt_MantExp and the table-based sqrtFixp_lookup.
// All are integer kernels (no transcendental), so bit-exact in any build.

// sqrtTab is the genuine const USHORT sqrt_tab[49] (FDK_tools_rom.cpp:6513), the
// linear-interpolation square-root lookup table sqrtFixp_lookup indexes.
var sqrtTab = [49]uint16{
	0x5a82, 0x5d4b, 0x6000, 0x62a1, 0x6531, 0x67b1, 0x6a21, 0x6c84, 0x6ed9,
	0x7123, 0x7360, 0x7593, 0x77bb, 0x79da, 0x7bef, 0x7dfb, 0x8000, 0x81fc,
	0x83f0, 0x85dd, 0x87c3, 0x89a3, 0x8b7c, 0x8d4e, 0x8f1b, 0x90e2, 0x92a4,
	0x9460, 0x9617, 0x97ca, 0x9977, 0x9b20, 0x9cc4, 0x9e64, 0xa000, 0xa197,
	0xa32b, 0xa4ba, 0xa646, 0xa7cf, 0xa953, 0xaad5, 0xac53, 0xadcd, 0xaf45,
	0xb0b9, 0xb22b, 0xb399, 0xb504,
}

// fMultSD is the 1:1 port of fMult(FIXP_SGL a, FIXP_DBL b) == fixmul_SD
// (common_fix.h:239). On the build target (__ARM_ARCH_8__) fixmul_SD takes the
// GENERIC path fixmuldiv2_SD(a,b)<<1 == fixmuldiv2_DD(a<<16, b)<<1 (the ARMv8
// override only covers fixmul_DD, not fixmul_SD) — i.e. it DROPS the low product
// bit, unlike fMult(DBL,DBL) == fixmulDDarm8 which keeps it. Every
// fMult(FIXP_SGL,FIXP_DBL) in env_calc (smoothing ratios, randomPhase noise,
// limiter gains, invWidth) must use this form, NOT nativeaac.FMultDD.
//
// C counterpart: fixmul_SD / fixmuldiv2_SD (fixmul.h:171,225, arm/fixmul_arm.h:157).
func fMultSD(a int16, b int32) int32 {
	return nativeaac.FMultDiv2DD(int32(a)<<16, b) << 1
}

// fMultDiv2SD is the 1:1 port of fMultDiv2(FIXP_SGL a, FIXP_DBL b) ==
// fixmuldiv2_SD (common_fix.h:246) == fixmuldiv2_DD(a<<16, b) (arm/fixmul_arm.h:158).
//
// C counterpart: fixmuldiv2_SD (fixmul.h:171).
func fMultDiv2SD(a int16, b int32) int32 {
	return nativeaac.FMultDiv2DD(int32(a)<<16, b)
}

// sqrtFixpLookupE is the 1:1 port of sqrtFixp_lookup(FIXP_DBL x, INT *x_e)
// (fixpoint_math.h:275-302): a table-based square root that normalizes its input
// and writes back the halved exponent. fixnormz_D == CntLeadingZeros.
//
// C counterpart: sqrtFixp_lookup (fixpoint_math.h:275).
func sqrtFixpLookupE(x int32, xe int) (int32, int) {
	if x == 0 {
		return x, xe
	}
	y := uint32(x)

	// Normalize.
	e := nativeaac.CntLeadingZeros(int32(y))
	y <<= uint(e)
	e = xe - e + 2

	// Correct odd exponent.
	if e&1 != 0 {
		y >>= 1
		e++
	}
	// Get square root.
	idx := (y >> 26) - 16
	frac := uint16((y >> 10) & 0xffff)
	nfrac := uint16(0xffff) ^ frac
	t := uint32(nfrac)*uint32(sqrtTab[idx]) + uint32(frac)*uint32(sqrtTab[idx+1])

	// Write back exponent.
	xe = e >> 1
	return int32(t >> 1), xe
}

// sqrtFixpLookup is the 1:1 port of sqrtFixp_lookup(FIXP_DBL x)
// (fixpoint_math.h:262-273): the no-exponent table square root apply_inter_tes
// uses for the gain[i] / gain_adj computation.
//
// C counterpart: sqrtFixp_lookup (fixpoint_math.h:262).
func sqrtFixpLookup(x int32) int32 {
	y := uint32(x)
	isZero := y == 0
	zeros := nativeaac.CntLeadingZeros(int32(y)) & 0x1e
	y <<= uint(zeros)
	idx := (y >> 26) - 16
	frac := uint16((y >> 10) & 0xffff)
	nfrac := uint16(0xffff) ^ frac
	t := uint32(nfrac)*uint32(sqrtTab[idx]) + uint32(frac)*uint32(sqrtTab[idx+1])
	t = t >> uint(zeros>>1)
	if isZero {
		return 0
	}
	return int32(t)
}

// fdkAddMantExp is the 1:1 port of the FIXP_DBL FDK_add_MantExp
// (transcendent.h:177-213): add two mantissa/exponent FIXP_DBL values, keeping
// 1 bit of headroom to avoid overflow.
//
// C counterpart: FDK_add_MantExp (transcendent.h:177).
func fdkAddMantExp(a int32, ae int8, b int32, be int8) (sum int32, sumE int8) {
	shift := int(ae) - int(be)

	shiftAbs := shift
	if shiftAbs < 0 {
		shiftAbs = -shiftAbs
	}
	if shiftAbs >= dfractBits-1 {
		shiftAbs = dfractBits - 1
	}
	var shiftedMantissa, otherMantissa int32
	if shift > 0 {
		shiftedMantissa = b >> uint(shiftAbs)
		otherMantissa = a
		sumE = ae
	} else {
		shiftedMantissa = a >> uint(shiftAbs)
		otherMantissa = b
		sumE = be
	}

	accu := (shiftedMantissa >> 1) + (otherMantissa >> 1)
	// shift by 1 bit to avoid overflow
	const half = int32(0x40000000) // FL2FXCONST_DBL(0.5f)
	if accu >= (half-1) || accu <= -half {
		sumE++
	} else {
		accu = shiftedMantissa + otherMantissa
	}
	return accu, sumE
}

// fdkDivideMantExp is the 1:1 port of the FIXP_DBL FDK_divide_MantExp
// (transcendent.h:282-336): table-based mantissa/exponent division a/b through
// the SBR 1/x lookup table (sbrInvTable).
//
// C counterpart: FDK_divide_MantExp (transcendent.h:282).
func fdkDivideMantExp(aM int32, aE int8, bM int32, bE int8) (resultM int32, resultE int8) {
	preShift := nativeaac.CntLeadingZeros(bM)

	shift := dfractBits - 2 - invTableBits - preShift

	var index int
	if shift < 0 {
		index = int(int64(bM) << uint(-shift))
	} else {
		index = int(int64(bM) >> uint(shift))
	}

	// The index has INV_TABLE_BITS +1 valid bits here. Clear the other bits.
	index &= (1 << (invTableBits + 1)) - 1

	// Remove offset of half an interval.
	index--

	// Now the lowest bit is shifted out.
	index = index >> 1

	var bInvM int16 // FL2FXCONST_SGL(0.0f)
	if index >= 0 {
		bInvM = sbrInvTable[index]
	}

	var ratioM int32
	if index < 0 {
		ratioM = aM >> 1
	} else {
		// fMultDiv2(FIXP_SGL, FIXP_DBL) == fMultDiv2(FX_SGL2FX_DBL(bInv_m), a_m).
		ratioM = nativeaac.FMultDiv2DD(int32(bInvM)<<16, aM)
	}

	postShift := nativeaac.CntLeadingZeros(ratioM) - 1

	resultM = ratioM << uint(postShift)
	resultE = int8(int(aE) - int(bE) + 1 + preShift - postShift)
	return resultM, resultE
}

// fdkSqrtMantExp is the 1:1 port of FDK_sqrt_MantExp (transcendent.h:348-370):
// the square root of (mantissa,exponent) rescaled to destScale (or, when
// exponent==destScale i.e. dest is nil-equivalent, leaving the natural result
// exponent). destScalePtr being nil mirrors the C call where the exponent
// pointer and destScale pointer are the same address (&pNrgs->nrgGain_e[k]).
//
// C counterpart: FDK_sqrt_MantExp (transcendent.h:348).
func fdkSqrtMantExp(mantissa int32, exponent int8, destScale *int8) (int32, int8) {
	inputM := mantissa
	inputE := int(exponent)

	// Call lookup square root, which does internally normalization.
	result, resultE := sqrtFixpLookupE(inputM, inputE)

	if destScale == nil {
		// exponent == destScale: write the natural result exponent.
		return result, int8(resultE)
	}
	shift := resultE - int(*destScale)
	var outM int32
	if shift >= 0 {
		outM = result << uint(nativeaac.FMinI(dfractBits-1, shift))
	} else {
		outM = result >> uint(nativeaac.FMinI(dfractBits-1, -shift))
	}
	return outM, *destScale
}

// maxSubbandSample is the 1:1 port of maxSubbandSample (env_calc.cpp:1909-1953):
// determine the headroom (biggest |value|) in a time/frequency range of the QMF
// buffer. re/im are slices of per-timeslot rows; im may be nil (real-only LP).
//
// C counterpart: maxSubbandSample (env_calc.cpp:1909).
func maxSubbandSample(re, im [][]int32, lowSubband, highSubband, startPos, nextPos int) int32 {
	var maxVal int32
	width := highSubband - lowSubband

	if width > 0 {
		if im != nil {
			for l := startPos; l < nextPos; l++ {
				reTmp := re[l][lowSubband:]
				imTmp := im[l][lowSubband:]
				for k := 0; k < width; k++ {
					tmp1 := reTmp[k]
					tmp2 := imTmp[k]
					maxVal |= tmp1 ^ (tmp1 >> (dfractBits - 1))
					maxVal |= tmp2 ^ (tmp2 >> (dfractBits - 1))
				}
			}
		} else {
			for l := startPos; l < nextPos; l++ {
				row := re[l][lowSubband:]
				for k := 0; k < width; k++ {
					tmp := row[k]
					maxVal |= tmp ^ (tmp >> (dfractBits - 1))
				}
			}
		}
	}

	if maxVal > 0 {
		// For negative input values, maxVal is too small by 1. Add 1 only when
		// necessary: if maxVal is a power of 2.
		lowerPow2 := int32(1) << uint(dfractBits-1-nativeaac.CntLeadingZeros(maxVal))
		if maxVal == lowerPow2 {
			maxVal++
		}
	}
	return maxVal
}

// rescaleSubbandSamples is the 1:1 port of rescaleSubbandSamples
// (env_calc.cpp:1864-1887): shift the mantissas of a time/frequency range of the
// QMF buffer left by `shift` bits (right if negative). im may be nil.
//
// C counterpart: rescaleSubbandSamples (env_calc.cpp:1864).
func rescaleSubbandSamples(re, im [][]int32, lowSubband, highSubband, startPos, nextPos, shift int) {
	width := highSubband - lowSubband
	if width > 0 && shift != 0 {
		if im != nil {
			for l := startPos; l < nextPos; l++ {
				nativeaac.ScaleValues(re[l][lowSubband:lowSubband+width], width, int32(shift))
				nativeaac.ScaleValues(im[l][lowSubband:lowSubband+width], width, int32(shift))
			}
		} else {
			for l := startPos; l < nextPos; l++ {
				nativeaac.ScaleValues(re[l][lowSubband:lowSubband+width], width, int32(shift))
			}
		}
	}
}
