// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Psychoacoustics-encoder chaos (tonality) measure — a 1:1 port of the
// "peak filter" chaos estimator in the Fraunhofer FDK-AAC encoder
// (libAACenc/src/chaosmeasure.cpp). The chaos measure classifies every MDCT
// spectral line as tonal (near 0) or noise-like (near 1); the encoder's
// scalefactor/threshold path consumes it.
//
// All arithmetic here is fixed-point integer: FIXP_DBL == int32, a signed
// Q1.31 fraction (common_fix.h). The kernel is therefore bit-identical
// regardless of vectorization or FMA fusing — it is NOT FP-gated by
// aac_strict (cf. the integer-kernel note in nativeaac.go). The parity gate
// (internal/parity_tests/psychoacoustics-encoder) compares this against the
// vendored chaosmeasure.cpp compiled via cgo.
//
// Go's `>>` on a signed integer is an arithmetic shift, matching the C `>>`
// on the signed LONG used throughout.

// chaosDfractBits is DFRACT_BITS — the FIXP_DBL width (common_fix.h:113).
const chaosDfractBits = 32

// chaosMaxvalDBL is MAXVAL_DBL == (signed)0x7FFFFFFF (common_fix.h:155), the
// saturated maximum of a FIXP_DBL.
const chaosMaxvalDBL int32 = 0x7FFFFFFF

// chaosMeasureHalf is FL2FXCONST_DBL(0.5) == (LONG)(0.5*2^31 + 0.5) ==
// 0x40000000 (common_fix.h:191, DFRACT_FIX_SCALE == 2^31), the constant the
// reference writes into the trailing lines.
const chaosMeasureHalf int32 = 0x40000000

// chaosCntLeadingZeros counts the leading zero bits of x interpreted as a
// 32-bit pattern. C counterpart: CntLeadingZeros(x) == fixnormz_D(x), the
// generic fallback in libFDK/include/clz.h:152.
//
//	inline INT fixnormz_D(LONG a) {
//	  INT leadingBits = 0;
//	  a = ~a;
//	  while (a & 0x80000000) { leadingBits++; a <<= 1; }
//	  return (leadingBits);
//	}
//
// The bit-twiddling (operate on ~a, test the top bit) is reproduced exactly so
// the count matches the reference for every input.
func chaosCntLeadingZeros(x int32) int {
	a := uint32(^x)
	leadingBits := 0
	for a&0x80000000 != 0 {
		leadingBits++
		a <<= 1
	}
	return leadingBits
}

// chaosSchurDiv delivers num/denum with count-bit accuracy. C counterpart: the
// generic schur_div fallback in libFDK/src/fixpoint_math.cpp:402 (the x86
// header provides an inline variant; the generic one is what compiles on
// arm64/ppc and is the bit-exact reference here). Preconditions (FDK asserts):
// num >= 0, denum > 0, num <= denum.
//
//	FIXP_DBL schur_div(FIXP_DBL num, FIXP_DBL denum, INT count) {
//	  INT L_num = (LONG)num >> 1;
//	  INT L_denum = (LONG)denum >> 1;
//	  INT div = 0;
//	  INT k = count;
//	  if (L_num != 0)
//	    while (--k) {
//	      div <<= 1; L_num <<= 1;
//	      if (L_num >= L_denum) { L_num -= L_denum; div++; }
//	    }
//	  return (FIXP_DBL)(div << (DFRACT_BITS - count));
//	}
func chaosSchurDiv(num, denum int32, count int) int32 {
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
	return div << (chaosDfractBits - count)
}

// chaosFMultDD multiplies two FIXP_DBL fractions at full scale, a local copy
// of fixmul_DD (libFDK/include/fixmul.h:145) kept self-contained so this slice
// does not depend on the multiply helper landing in a sibling file. The
// int64-intermediate arithmetic shift makes it bit-identical to the reference.
//
//	inline LONG fixmul_DD(const LONG a, const LONG b) {
//	  return ((LONG)((((INT64)a) * b) >> 32)) << 1;
//	}
func chaosFMultDD(a, b int32) int32 {
	return int32((int64(a)*int64(b))>>32) << 1
}

// calculateChaosMeasure fills chaosMeasure[0:numberOfLines] from the MDCT
// magnitudes in paMDCTDataNM0[0:numberOfLines]. C counterpart:
// FDKaacEnc_CalculateChaosMeasure (chaosmeasure.cpp:185), which forwards to
// the static FDKaacEnc_FDKaacEnc_CalculateChaosMeasurePeakFast
// (chaosmeasure.cpp:112). 0 means tonal, 1 means noise-like.
//
// The `(x ^ (x >> (DFRACT_BITS-1)))` idiom in the C is a branch-free absolute
// value (XOR with the sign-extended sign bit); it is reproduced exactly.
func calculateChaosMeasure(paMDCTDataNM0 []int32, numberOfLines int, chaosMeasure []int32) {
	// abs via XOR with arithmetic-shifted sign bit, matching the C.
	abs := func(v int32) int32 { return v ^ (v >> (chaosDfractBits - 1)) }

	// left, center taps of the peak filter; even- and odd-numbered passes.
	left0Div2 := abs(paMDCTDataNM0[0]) >> 1
	left1Div2 := abs(paMDCTDataNM0[1]) >> 1
	center0 := abs(paMDCTDataNM0[2])
	center1 := abs(paMDCTDataNM0[3])

	for j := 2; j < numberOfLines-2; j += 2 {
		right0 := abs(paMDCTDataNM0[j+2])
		tmp0 := left0Div2 + (right0 >> 1)
		right1 := abs(paMDCTDataNM0[j+3])
		tmp1 := left1Div2 + (right1 >> 1)

		if tmp0 < center0 {
			leadingBits := chaosCntLeadingZeros(center0) - 1
			tmp0 = chaosSchurDiv(tmp0<<uint(leadingBits), center0<<uint(leadingBits), 8)
			tmp0 = chaosFMultDD(tmp0, tmp0)
		} else {
			tmp0 = chaosMaxvalDBL
		}
		chaosMeasure[j+0] = tmp0
		left0Div2 = center0 >> 1
		center0 = right0

		if tmp1 < center1 {
			leadingBits := chaosCntLeadingZeros(center1) - 1
			tmp1 = chaosSchurDiv(tmp1<<uint(leadingBits), center1<<uint(leadingBits), 8)
			tmp1 = chaosFMultDD(tmp1, tmp1)
		} else {
			tmp1 = chaosMaxvalDBL
		}
		left1Div2 = center1 >> 1
		center1 = right1
		chaosMeasure[j+1] = tmp1
	}

	// first few lines copy line 2.
	chaosMeasure[0] = chaosMeasure[2]
	chaosMeasure[1] = chaosMeasure[2]

	// last few lines get the 0.5 constant.
	for i := numberOfLines - 3; i < numberOfLines; i++ {
		chaosMeasure[i] = chaosMeasureHalf
	}
}

// CalculateChaosMeasure is the exported entry point to the chaos-measure
// kernel, used by the parity oracle and benchmarks (which cannot reach the
// unexported calculateChaosMeasure across the package boundary). It fills
// chaosMeasure[0:numberOfLines] from paMDCTDataNM0[0:numberOfLines]; both
// slices must hold at least numberOfLines entries and numberOfLines must be at
// least 4 (the peak filter primes four taps).
func CalculateChaosMeasure(paMDCTDataNM0 []int32, numberOfLines int, chaosMeasure []int32) {
	calculateChaosMeasure(paMDCTDataNM0, numberOfLines, chaosMeasure)
}
