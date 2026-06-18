// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Fixed-point LD-domain helpers ported 1:1 from the vendored FDK-AAC reference
// (libFDK/include/fixpoint_math.h + libFDK/src/fixpoint_math.cpp). These are the
// inverse-log2 (CalcInvLdData), integer-log2 (CalcLdInt), normalised multiply
// (fMultNorm) and fractional*integer multiply (fMultI) primitives the AAC
// encoder perceptual-entropy / threshold-adjustment driver (line_pe.cpp +
// adj_thr.cpp) computes upon. They are pure-integer Q-format kernels — ROM
// lookups, leading-bit normalisation and arithmetic shift/add — bit-identical
// regardless of vectorization, no float, no transcendental, so no aac_strict FP
// gating is required (cf. the integer-parity note in nativeaac.go).

// exp2TabLong is the 1:1 transcription of exp2_tab_long[32]
// (fixpoint_math.cpp:208-216): 2^x for x in [0..1.0[ in steps of 1/32, used by
// CalcInvLdData lookup1.
var exp2TabLong = [32]uint32{
	0x40000000, 0x4166C34C, 0x42D561B4, 0x444C0740, 0x45CAE0F2, 0x47521CC6,
	0x48E1E9BA, 0x4A7A77D4, 0x4C1BF829, 0x4DC69CDD, 0x4F7A9930, 0x51382182,
	0x52FF6B55, 0x54D0AD5A, 0x56AC1F75, 0x5891FAC1, 0x5A82799A, 0x5C7DD7A4,
	0x5E8451D0, 0x60962665, 0x62B39509, 0x64DCDEC3, 0x6712460B, 0x69540EC9,
	0x6BA27E65, 0x6DFDDBCC, 0x70666F76, 0x72DC8374, 0x75606374, 0x77F25CCE,
	0x7A92BE8B, 0x7D41D96E,
}

// exp2wTabLong is the 1:1 transcription of exp2w_tab_long[32]
// (fixpoint_math.cpp:221-228): 2^x for x in [0..1/32[ in steps of 1/1024, used
// by CalcInvLdData lookup2.
var exp2wTabLong = [32]uint32{
	0x40000000, 0x400B1818, 0x4016321B, 0x40214E0C, 0x402C6BE9, 0x40378BB4,
	0x4042AD6D, 0x404DD113, 0x4058F6A8, 0x40641E2B, 0x406F479E, 0x407A7300,
	0x4085A051, 0x4090CF92, 0x409C00C4, 0x40A733E6, 0x40B268FA, 0x40BD9FFF,
	0x40C8D8F5, 0x40D413DD, 0x40DF50B8, 0x40EA8F86, 0x40F5D046, 0x410112FA,
	0x410C57A2, 0x41179E3D, 0x4122E6CD, 0x412E3152, 0x41397DCC, 0x4144CC3B,
	0x41501CA0, 0x415B6EFB,
}

// exp2xTabLong is the 1:1 transcription of exp2x_tab_long[32]
// (fixpoint_math.cpp:233-240): 2^x for x in [0..1/1024[ in steps of 1/32768,
// used by CalcInvLdData lookup3.
var exp2xTabLong = [32]uint32{
	0x40000000, 0x400058B9, 0x4000B173, 0x40010A2D, 0x400162E8, 0x4001BBA3,
	0x4002145F, 0x40026D1B, 0x4002C5D8, 0x40031E95, 0x40037752, 0x4003D011,
	0x400428CF, 0x4004818E, 0x4004DA4E, 0x4005330E, 0x40058BCE, 0x4005E48F,
	0x40063D51, 0x40069613, 0x4006EED5, 0x40074798, 0x4007A05B, 0x4007F91F,
	0x400851E4, 0x4008AAA8, 0x4009036E, 0x40095C33, 0x4009B4FA, 0x400A0DC0,
	0x400A6688, 0x400ABF4F,
}

// calcInvLdData is the 1:1 port of CalcInvLdData(x) (fixpoint_math.h:228-255),
// the inverse of CalcLdData: it delivers 2^(x*LD_DATA_SCALING) for fractional
// -1.0 < x < 1.0. For x == 0 the result is MAXVAL_DBL; for negative x the result
// is a positive fraction; for positive x a positive integer; for x >= 31/64 it
// saturates to MAXVAL_DBL; for x < -31/64 it returns 0.
//
// The arm fixmul kernels used here (fMultDiv2(FIXP_DBL,FIXP_SGL) ==
// fMultDiv2DS, fMult(FIXP_DBL,FIXP_DBL) == fMultDD) are the same ones the rest
// of the port uses. C's `x >> 25` / `x >> 10` etc. are arithmetic shifts on the
// signed LONG, matching Go's `>>` on int32; the (UINT)(LONG) casts and the
// unsigned multiplies (lookup1/2/3 reinterpreted as FIXP_DBL) are reproduced
// with uint32<->int32 round-trips.
func calcInvLdData(x int32) int32 {
	setZero := int32(0)
	if x < fl2fxconstDBL(-31.0/64.0) {
		setZero = 0
	} else {
		setZero = 1
	}
	setMax := int32(0)
	if x >= fl2fxconstDBL(31.0/64.0) || x == fl2fxconstDBL(0.0) {
		setMax = 1
	}

	frac := int16(int32(x) & 0x3FF)
	index3 := uint32(x>>10) & 0x1F
	index2 := uint32(x>>15) & 0x1F
	index1 := uint32(x>>20) & 0x1F
	var exp int32
	if x > fl2fxconstDBL(0.0) {
		exp = 31 - (x >> 25)
	} else {
		exp = -(x >> 25)
	}
	exp = fMin(31, exp)

	lookup1 := exp2TabLong[index1] * uint32(setZero)
	lookup2 := exp2wTabLong[index2]
	lookup3 := exp2xTabLong[index3]
	lookup3f := lookup3 + uint32(fMultDiv2DS(int32(0x0016302F), frac))

	// fMult(LONG,LONG) on __ARM_ARCH_8__ == fixmul_DD == (a*b)>>31
	// (fixmulDDarm8), KEEPING bit 31 — not the generic fMultDD.
	lookup12 := uint32(fixmulDDarm8(int32(lookup1), int32(lookup2)))
	lookup := uint32(fixmulDDarm8(int32(lookup12), int32(lookup3f)))

	// C: `FIXP_DBL retVal = (lookup << 3) >> exp;` — lookup is UINT, so both
	// shifts are UNSIGNED (logical) and the result is reinterpreted as signed.
	retVal := int32((lookup << 3) >> uint(exp))

	if setMax != 0 {
		retVal = 0x7FFFFFFF
	}
	return retVal
}

// ldIntCoeff is the 1:1 transcription of ldIntCoeff[] for LD_INT_TAB_LEN == 193
// (fixpoint_math.cpp:299-364): a precomputed table of ld(i)/LD_DATA_SCALING for
// i in [0..192]. Entry 0 is the sentinel 0x80000001 (== -1.0+1ulp, FDK's
// "table initialised" marker).
var ldIntCoeff = [193]int32{
	-0x7FFFFFFF, 0x00000000, 0x02000000, // 0x80000001
	0x032b8034, 0x04000000, 0x04a4d3c2,
	0x052b8034, 0x059d5da0, 0x06000000,
	0x06570069, 0x06a4d3c2, 0x06eb3a9f,
	0x072b8034, 0x0766a009, 0x079d5da0,
	0x07d053f7, 0x08000000, 0x082cc7ee,
	0x08570069, 0x087ef05b, 0x08a4d3c2,
	0x08c8ddd4, 0x08eb3a9f, 0x090c1050,
	0x092b8034, 0x0949a785, 0x0966a009,
	0x0982809d, 0x099d5da0, 0x09b74949,
	0x09d053f7, 0x09e88c6b, 0x0a000000,
	0x0a16bad3, 0x0a2cc7ee, 0x0a423162,
	0x0a570069, 0x0a6b3d79, 0x0a7ef05b,
	0x0a92203d, 0x0aa4d3c2, 0x0ab7110e,
	0x0ac8ddd4, 0x0ada3f60, 0x0aeb3a9f,
	0x0afbd42b, 0x0b0c1050, 0x0b1bf312,
	0x0b2b8034, 0x0b3abb40, 0x0b49a785,
	0x0b584822, 0x0b66a009, 0x0b74b1fd,
	0x0b82809d, 0x0b900e61, 0x0b9d5da0,
	0x0baa708f, 0x0bb74949, 0x0bc3e9ca,
	0x0bd053f7, 0x0bdc899b, 0x0be88c6b,
	0x0bf45e09, 0x0c000000, 0x0c0b73cb,
	0x0c16bad3, 0x0c21d671, 0x0c2cc7ee,
	0x0c379085, 0x0c423162, 0x0c4caba8,
	0x0c570069, 0x0c6130af, 0x0c6b3d79,
	0x0c7527b9, 0x0c7ef05b, 0x0c88983f,
	0x0c92203d, 0x0c9b8926, 0x0ca4d3c2,
	0x0cae00d2, 0x0cb7110e, 0x0cc0052b,
	0x0cc8ddd4, 0x0cd19bb0, 0x0cda3f60,
	0x0ce2c97d, 0x0ceb3a9f, 0x0cf39355,
	0x0cfbd42b, 0x0d03fda9, 0x0d0c1050,
	0x0d140ca0, 0x0d1bf312, 0x0d23c41d,
	0x0d2b8034, 0x0d3327c7, 0x0d3abb40,
	0x0d423b08, 0x0d49a785, 0x0d510118,
	0x0d584822, 0x0d5f7cff, 0x0d66a009,
	0x0d6db197, 0x0d74b1fd, 0x0d7ba190,
	0x0d82809d, 0x0d894f75, 0x0d900e61,
	0x0d96bdad, 0x0d9d5da0, 0x0da3ee7f,
	0x0daa708f, 0x0db0e412, 0x0db74949,
	0x0dbda072, 0x0dc3e9ca, 0x0dca258e,
	0x0dd053f7, 0x0dd6753e, 0x0ddc899b,
	0x0de29143, 0x0de88c6b, 0x0dee7b47,
	0x0df45e09, 0x0dfa34e1, 0x0e000000,
	0x0e05bf94, 0x0e0b73cb, 0x0e111cd2,
	0x0e16bad3, 0x0e1c4dfb, 0x0e21d671,
	0x0e275460, 0x0e2cc7ee, 0x0e323143,
	0x0e379085, 0x0e3ce5d8, 0x0e423162,
	0x0e477346, 0x0e4caba8, 0x0e51daa8,
	0x0e570069, 0x0e5c1d0b, 0x0e6130af,
	0x0e663b74, 0x0e6b3d79, 0x0e7036db,
	0x0e7527b9, 0x0e7a1030, 0x0e7ef05b,
	0x0e83c857, 0x0e88983f, 0x0e8d602e,
	0x0e92203d, 0x0e96d888, 0x0e9b8926,
	0x0ea03232, 0x0ea4d3c2, 0x0ea96df0,
	0x0eae00d2, 0x0eb28c7f, 0x0eb7110e,
	0x0ebb8e96, 0x0ec0052b, 0x0ec474e4,
	0x0ec8ddd4, 0x0ecd4012, 0x0ed19bb0,
	0x0ed5f0c4, 0x0eda3f60, 0x0ede8797,
	0x0ee2c97d, 0x0ee70525, 0x0eeb3a9f,
	0x0eef69ff, 0x0ef39355, 0x0ef7b6b4,
	0x0efbd42b, 0x0effebcd, 0x0f03fda9,
	0x0f0809cf, 0x0f0c1050, 0x0f10113b,
	0x0f140ca0, 0x0f18028d, 0x0f1bf312,
	0x0f1fde3d, 0x0f23c41d, 0x0f27a4c0,
	0x0f2b8034,
}

// ldIntTabLen is LD_INT_TAB_LEN, the default 193 (fixpoint_math.cpp:250).
const ldIntTabLen = 193

// calcLdInt is the 1:1 port of CalcLdInt(i) (fixpoint_math.cpp:378-391):
// ld(i)/LD_DATA_SCALING via the ldIntCoeff ROM, for i in [1, LD_INT_TAB_LEN);
// returns 0 outside that range.
func calcLdInt(i int32) int32 {
	if i > 0 && i < ldIntTabLen {
		return ldIntCoeff[i]
	}
	return 0
}

// fMultNorm is the 1:1 port of fMultNorm(f1, f2, *result_e)
// (fixpoint_math.cpp:427-449): a normalised FIXP_DBL multiply returning the
// mantissa and (via the second return) the exponent, so the product carries no
// precision loss from small operands. CountLeadingBits == fNorm. fMult ==
// fMultDD on this target.
func fMultNorm(f1, f2 int32) (product, resultE int32) {
	if f1 == 0 || f2 == 0 {
		return 0, 0
	}
	normF1 := fNorm(f1)
	f1 = f1 << uint(normF1)
	normF2 := fNorm(f2)
	f2 = f2 << uint(normF2)

	if f1 == int32(-0x80000000) && f2 == int32(-0x80000000) {
		product = -(int32(-0x80000000) >> 1)
		resultE = -(normF1 + normF2 - 1)
	} else {
		// fMult(LONG,LONG) on __ARM_ARCH_8__ == fixmul_DD == (a*b)>>31
		// (fixmulDDarm8), KEEPING bit 31 — not the generic fMultDD.
		product = fixmulDDarm8(f1, f2)
		resultE = -(normF1 + normF2)
	}
	return product, resultE
}

// fMultI is the 1:1 port of fMultI(a, b) (fixpoint_math.h:507-525): multiply a
// FIXP_DBL fraction by an INT, rounding to the nearest integer. Uses fMultNorm
// then a round-half-up shift, or scaleValueSaturate for non-negative exponents.
func fMultI(a, b int32) int32 {
	m, mE := fMultNorm(a, b)
	var mi int32
	if mE < 0 {
		if mE > -dfractBits {
			m = m >> uint((-mE)-1)
			mi = (m + 1) >> 1
		} else {
			mi = 0
		}
	} else {
		mi = scaleValueSaturate(m, mE)
	}
	return mi
}
