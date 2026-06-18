// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Fixed-point square-root helpers ported 1:1 from the vendored FDK-AAC reference
// (libFDK/include/fixpoint_math.h invSqrtNorm2 / sqrtFixp + the invSqrtTab ROM in
// libFDK/src/FDK_tools_rom.cpp). sqrtFixp is the kernel the AAC encoder
// scale-factor estimator's form-factor calculation (sf_estim.cpp
// FDKaacEnc_CalcFormFactorChannel) sums sqrt(|spec|) with. These are pure integer
// Q-format kernels — leading-bit normalisation, a ROM lookup with linear
// interpolation and int64-product fixmul — bit-identical regardless of
// vectorization, no float, no transcendental. They therefore carry no aac_strict
// FP gate (only the aacfdk license fence). fMult/fMultDiv2 on this build target
// (aarch64 == __ARM_ARCH_8__) resolve to the arm8 smull kernels: fMultDiv2 ==
// fMultDiv2DD (smull;asr#32), fMult(FIXP_DBL,FIXP_DBL) == fixmulDDarm8
// (smull;asr#31, keeps bit 31).

// Square-root ROM/normalisation constants (fixpoint_math.h:146-149).
//
//	#define SQRT_BITS 7
//	#define SQRT_VALUES (128 + 2)
//	#define SQRT_BITS_MASK 0x7f
//	#define SQRT_FRACT_BITS_MASK 0x007FFFFF
const (
	sqrtBits          = 7
	sqrtValues        = 128 + 2
	sqrtBitsMask      = 0x7f
	sqrtFractBitsMask = 0x007FFFFF
)

// invSqrtTab is the 1:1 transcription of the vendored invSqrtTab[SQRT_VALUES]
// ROM (FDK_tools_rom.cpp:7222): a precomputed 1/sqrt lookup over [0.5, 1.0) for
// invSqrtNorm2's linear-interpolation path. The last commented entry
// (0x3FC05F61) is intentionally absent in the C (130 active values), so this
// table is 130 entries.
var invSqrtTab = [sqrtValues]int32{
	0x5A827999, 0x5A287E03, 0x59CF8CBC, 0x5977A0AC, 0x5920B4DF, 0x58CAC480,
	0x5875CADE, 0x5821C364, 0x57CEA99D, 0x577C7930, 0x572B2DE0, 0x56DAC38E,
	0x568B3632, 0x563C81E0, 0x55EEA2C4, 0x55A19522, 0x55555555, 0x5509DFD0,
	0x54BF311A, 0x547545D0, 0x542C1AA4, 0x53E3AC5B, 0x539BF7CD, 0x5354F9E7,
	0x530EAFA5, 0x52C91618, 0x52842A5F, 0x523FE9AC, 0x51FC5140, 0x51B95E6B,
	0x51770E8F, 0x51355F1A, 0x50F44D89, 0x50B3D768, 0x5073FA50, 0x5034B3E7,
	0x4FF601E0, 0x4FB7E1FA, 0x4F7A5202, 0x4F3D4FCF, 0x4F00D944, 0x4EC4EC4F,
	0x4E8986EA, 0x4E4EA718, 0x4E144AE9, 0x4DDA7073, 0x4DA115DA, 0x4D683948,
	0x4D2FD8F4, 0x4CF7F31B, 0x4CC08605, 0x4C899000, 0x4C530F65, 0x4C1D0294,
	0x4BE767F5, 0x4BB23DF9, 0x4B7D8317, 0x4B4935CF, 0x4B1554A6, 0x4AE1DE2A,
	0x4AAED0F0, 0x4A7C2B93, 0x4A49ECB3, 0x4A1812FA, 0x49E69D16, 0x49B589BB,
	0x4984D7A4, 0x49548592, 0x49249249, 0x48F4FC97, 0x48C5C34B, 0x4896E53D,
	0x48686148, 0x483A364D, 0x480C6332, 0x47DEE6E1, 0x47B1C049, 0x4784EE60,
	0x4758701C, 0x472C447C, 0x47006A81, 0x46D4E130, 0x46A9A794, 0x467EBCBA,
	0x46541FB4, 0x4629CF98, 0x45FFCB80, 0x45D6128A, 0x45ACA3D5, 0x45837E88,
	0x455AA1CB, 0x45320CC8, 0x4509BEB0, 0x44E1B6B4, 0x44B9F40B, 0x449275ED,
	0x446B3B96, 0x44444444, 0x441D8F3B, 0x43F71BBF, 0x43D0E917, 0x43AAF68F,
	0x43854374, 0x435FCF15, 0x433A98C6, 0x43159FDC, 0x42F0E3AE, 0x42CC6398,
	0x42A81EF6, 0x42841527, 0x4260458E, 0x423CAF8D, 0x4219528B, 0x41F62DF2,
	0x41D3412A, 0x41B08BA2, 0x418E0CC8, 0x416BC40D, 0x4149B0E5, 0x4127D2C3,
	0x41062920, 0x40E4B374, 0x40C3713B, 0x40A261EF, 0x40818512, 0x4060DA22,
	0x404060A1, 0x40201814, 0x40000000, 0x3FE017EC, // , 0x3FC05F61
}

// invSqrtNorm2 is the 1:1 port of invSqrtNorm2(op, *shift)
// (fixpoint_math.h:325-374): 1.0/sqrt(op) normalised to [0.5, 1.0) with the
// output shift, via leading-bit normalisation and the invSqrtTab linear
// interpolation (the INVSQRTNORM2_LINEAR_INTERPOLATE + _HQ path the default
// config selects). op must be > 0 except for the op == 0 special case (returns
// MAXVAL_DBL, shift 16). fNormz on a positive value == fNormzPos; fMultDiv2 ==
// fMultDiv2DD; fMultAddDiv2 == x + fMultDiv2(a,b).
func invSqrtNorm2(op int32) (result, shift int32) {
	val := op

	if val == 0 { // FL2FXCONST_DBL(0.0)
		return 0x7FFFFFFF, 16 // MAXVAL_DBL
	}

	// normalize input, calculate shift value (fNormz(val)-1; val > 0 so the
	// CountLeadingBits/fNormz distinction is irrelevant — fNormzPos).
	shift = int32(fNormzPos(val)) - 1
	val <<= uint(shift) // normalized input V
	shift += 2          // bias for exponent

	index := int((val >> (dfractBits - 1 - (sqrtBits + 1))) & sqrtBitsMask)
	// FIXP_DBL Fract = (FIXP_DBL)(((INT)val & SQRT_FRACT_BITS_MASK) << (SQRT_BITS+1));
	fract := int32((val & sqrtFractBitsMask) << (sqrtBits + 1))
	diff := invSqrtTab[index+1] - invSqrtTab[index]
	// reg1 = invSqrtTab[index] + (fMultDiv2(diff, Fract) << 1);
	reg1 := invSqrtTab[index] + (fMultDiv2DD(diff, fract) << 1)

	if fract != 0 {
		// Fract = fMultDiv2(Fract, (0x80000000 - Fract)) << 1;
		fract = fMultDiv2DD(fract, int32(uint32(0x80000000)-uint32(fract))) << 1
		diff = diff - (invSqrtTab[index+2] - invSqrtTab[index+1])
		reg1 = fMultAddDiv2(reg1, fract, diff)
	}

	if shift&0x00000001 != 0 { // odd shift values ?
		reg2 := int32(0x5A827999) // 1/sqrt(2) unrounded
		reg1 = fMultDiv2DD(reg1, reg2) << 2
	}

	shift = shift >> 1

	return reg1, shift
}

// sqrtFixp is the 1:1 port of sqrtFixp(op) (fixpoint_math.h:377-383):
// sqrt(op) via invSqrtNorm2. The C asserts tmp_exp > 0 (op > 0); the encoder
// calls it on fixp_abs(spec) so the value is always >= 0, and the op == 0 case
// is handled inside invSqrtNorm2 (returns 0 here since op << (shift-1) is 0).
//
//	return ((FIXP_DBL)(fMultDiv2((op << (tmp_exp - 1)), tmp_inv) << 2));
func sqrtFixp(op int32) int32 {
	tmpInv, tmpExp := invSqrtNorm2(op)
	return fMultDiv2DD(op<<uint(tmpExp-1), tmpInv) << 2
}
