// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

import "math"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// inline_fixp_cos_sin (FDK_trigFcts.h:228) and its helper
// fixp_sin_cos_residual_inline (:155), ported 1:1. The HE-AAC v2 PS upmix calls
// inline_fixp_cos_sin(Beta+Alpha, Beta-Alpha, 2, trigData) to turn the IID/ICC
// rotation angles into the h11/h12/h21/h22 mixing coefficients. Lives in
// nativeaac because it indexes the package-private sineTable512Q15 ROM (the same
// 512-point packed Q1.15 SineTable the FFT uses), the genuine in-RAM SINETABLE
// under the SINETABLE_16BIT (__ARM_ARCH_8__) build.

// fl2fxconst1OverPi / fl2fxconstPiOver4 are FL2FXCONST_DBL(1.0/M_PI) and
// FL2FXCONST_DBL(M_PI/4.0), the residual-stage constants. M_PI is the C double
// 3.14159265358979323846.
var (
	fl2fxconst1OverPi = fl2fxconstDBL(1.0 / math.Pi)
	fl2fxconstPiOver4 = fl2fxconstDBL(math.Pi / 4.0)
)

const psLD = 9 // LD (FDK_trigFcts.h:145)

// fixpSinCosResidualInline ports fixp_sin_cos_residual_inline (FDK_trigFcts.h:
// 155-218) for the SINETABLE_16BIT path. It computes sine(x) and cosine(x) at
// FIXP_DBL precision (downscaled by 1 for overflow prevention, undone by the
// caller) plus the linear-interpolation residual.
func fixpSinCosResidualInline(x int32, scale int) (residual, sine, cosine int32) {
	shift := uint(31 - scale - psLD - 1)
	ssign := int32(1)
	csign := int32(1)

	residual = fMult(x, fl2fxconst1OverPi)
	s := int(int32(residual) >> shift)

	residual &= (1 << shift) - 1
	residual = fMult(residual, fl2fxconstPiOver4) << 2
	residual <<= uint(scale)

	// Sine sign symmetry.
	if s&((1<<psLD)<<1) != 0 {
		ssign = -ssign
	}
	// Cosine sign symmetry.
	if (s+(1<<psLD))&((1<<psLD)<<1) != 0 {
		csign = -csign
	}

	if s < 0 {
		s = -s
	}
	s &= ((1 << psLD) << 1) - 1 // modulo PI

	if s > (1 << psLD) {
		s = ((1 << psLD) << 1) - s
	}

	var sl, cl int32
	if s > (1 << (psLD - 1)) {
		// Cosine/Sine symmetry for angles greater than PI/4.
		s = (1 << psLD) - s
		tmp := sineTable512Q15[s]
		sl = int32(tmp.re)
		cl = int32(tmp.im)
	} else {
		tmp := sineTable512Q15[s]
		sl = int32(tmp.im)
		cl = int32(tmp.re)
	}

	// SINETABLE_16BIT: *sine = (sl*ssign) << (DFRACT_BITS - FRACT_BITS), where
	// DFRACT_BITS-FRACT_BITS == 32-16 == 16.
	sine = (sl * ssign) << (32 - 16)
	cosine = (cl * csign) << (32 - 16)
	return residual, sine, cosine
}

// InlineFixpCosSin ports inline_fixp_cos_sin (FDK_trigFcts.h:228-255) for the
// SINETABLE_16BIT path: writes cos(x1), sin(x1), cos(x2), sin(x2) into out[0..3].
// Under SINETABLE_16BIT the residual error correction is applied WITHOUT the
// final <<1 undo (that undo is only in the non-16BIT branch).
func InlineFixpCosSin(x1, x2 int32, scale int, out []int32) {
	residual, sine, cosine := fixpSinCosResidualInline(x1, scale)
	error0 := fMultDiv2DD(sine, residual)
	error1 := fMultDiv2DD(cosine, residual)
	out[0] = cosine - (error0 << 1)
	out[1] = sine + (error1 << 1)

	residual, sine, cosine = fixpSinCosResidualInline(x2, scale)
	error0 = fMultDiv2DD(sine, residual)
	error1 = fMultDiv2DD(cosine, residual)
	out[2] = cosine - (error0 << 1)
	out[3] = sine + (error1 << 1)
}
