// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && mp3_strict

package nativemp3

// Strict-mode floating-point helpers for LAME's quantization iteration loop
// (quantize.c init_xrpow_core_c / ms_convert / amp_scalefac_bands /
// inc_scalefac_scale / inc_subblock_gain / penalties / get_klemm_noise /
// calc_target_bits).
//
// quantize.c mixes C `FLOAT` (== float32) and `double` arithmetic. The cgo
// oracle compiles it with -ffp-contract=off, so every multiply/add rounds
// separately; Go's backend would otherwise fuse `a*b+c` into an FMA. Each
// float32 mul/add on a bit-exact path is routed through a //go:noinline `qe`
// helper below so the strict build separately-rounds, matching clang. The
// double-precision sub-expressions (penalties / get_klemm_noise / the
// res_factor and add_bits in calc_target_bits, and the sqrt/pow/log10
// transcendentals) are reproduced as float64 expressions wrapped in //go:noinline
// helpers where a fusion is otherwise possible; lone transcendental calls
// (sqrt, pow, FAST_LOG10) round correctly on their own and are computed inline
// with math.* at the call sites. The `qe` prefix keeps these distinct from the
// psymodel `ps*`, frame `fe*`, takehiro `tq*` and bit-alloc `op*` helpers.

import "math"

// qeMul / qeAdd / qeSub are the separately rounded float32 primitives quantize.c
// uses on bit-exact paths (e.g. ms_convert's (l+r)*(SQRT2*0.5), the xrpow
// scalings in amp_scalefac_bands / inc_scalefac_scale / inc_subblock_gain).
//
//go:noinline
func qeMul(a, b float32) float32 { return a * b }

//go:noinline
func qeAdd(a, b float32) float32 { return a + b }

//go:noinline
func qeSub(a, b float32) float32 { return a - b }

// qeXrpowCore returns sqrt(tmp * sqrt(tmp)) narrowed to float32, the xrpow[i]
// value in init_xrpow_core_c (quantize.c:81). tmp is the FLOAT (float32)
// fabs(xr[i]); the inner sqrt promotes it to double, the product and outer sqrt
// are double, narrowed to the FLOAT xrpow[i] on store. //go:noinline for
// convention (a lone double chain).
//
//go:noinline
func qeXrpowCore(tmp float32) float32 {
	return float32(math.Sqrt(float64(tmp) * math.Sqrt(float64(tmp))))
}

// qeTrigger95 returns trigger * .95 (amp_scalefac_bands, quantize.c:760/769).
// .95 is a double literal, so the FLOAT trigger promotes to double, the product
// is double, narrowed back to FLOAT. //go:noinline keeps the narrow separate.
//
//go:noinline
func qeTrigger95(trigger float32) float32 {
	return float32(float64(trigger) * 0.95)
}

// qePenalties returns FAST_LOG10(0.368 + 0.632*noise^3), quant_compare's
// penalties() (quantize.c:571). All-double; FAST_LOG10 expands to log10 (USE_FAST_LOG
// undefined). The 0.632*noise*noise*noise + 0.368 sum is routed through the
// //go:noinline boundary so the multiplies and the add round separately under
// -ffp-contract=off.
//
//go:noinline
func qePenalties(noise float64) float64 {
	return math.Log10(0.368 + 0.632*noise*noise*noise)
}

// qeKlemmAcc returns klemm_noise + penalties(distort), get_klemm_noise's
// accumulation (quantize.c:580). Double add, separately rounded.
//
//go:noinline
func qeKlemmAcc(klemm, pen float64) float64 { return klemm + pen }

// qeResFactor returns calc_target_bits's res_factor =
// .93 + .07*(11.0-compression_ratio)/(11.0-5.5) (quantize.c:1813). All-double;
// compression_ratio (FLOAT) promotes. //go:noinline so the product/divide/add
// do not fuse.
//
//go:noinline
func qeResFactor(compressionRatio float32) float64 {
	return 0.93 + 0.07*(11.0-float64(compressionRatio))/(11.0-5.5)
}

// qeTargBits returns int(resFactor * float64(meanBits)), calc_target_bits's
// targ_bits assignment (quantize.c:1822/1828). Double product truncated to int.
//
//go:noinline
func qeTargBits(resFactor float64, meanBits int) int {
	return int(resFactor * float64(meanBits))
}

// qeAddBits returns int((pe - 700) / 1.4), calc_target_bits's add_bits
// (quantize.c:1825). pe is FLOAT promoted to double; the subtract and divide
// run in double, truncated to int.
//
//go:noinline
func qeAddBits(pe float32) int {
	return int((float64(pe) - 700) / 1.4)
}

// qeVbrAdjust returns VBR_old_prepare's perceptual-entropy noise adjust
// `num / (1 + exp(3.5 - pe/300.)) - sub`, narrowed to FLOAT (quantize.c:1419 for
// long blocks: num=1.28, sub=0.05; quantize.c:1423 for short: num=2.56,
// sub=0.14). pe is FLOAT promoted to double; the exp, the divide, the subtract
// all run in double under -ffp-contract=off, narrowed once to the FLOAT adjust.
// //go:noinline keeps the chain from fusing.
//
//go:noinline
func qeVbrAdjust(num float64, pe float32, sub float64) float32 {
	return float32(num/(1+math.Exp(3.5-float64(pe)/300.)) - sub)
}

// qeVbrMaskingLower returns pow(10.0, maskingLowerDb * 0.1), VBR_*_prepare's
// masking_lower (quantize.c:1426 / 1619). maskingLowerDb is FLOAT promoted to
// double, the product and pow run in double, narrowed to the FLOAT masking_lower.
//
//go:noinline
func qeVbrMaskingLower(maskingLowerDb float32) float32 {
	return float32(math.Pow(10.0, float64(maskingLowerDb)*0.1))
}

// qeBitpressureXmin returns pxmin * (1. + .029 * sfb * sfb / span / span),
// bitpressure_strategy's per-band xmin inflation (quantize.c:1464/1468). The
// 1.+... factor is double (sfb int promoted, span is SBMAX_l or SBMAX_s as a
// double divisor), the FLOAT pxmin promotes to double for the product and
// narrows on store. //go:noinline so the factor and product round separately.
//
//go:noinline
func qeBitpressureXmin(pxmin float32, sfb int, span float64) float32 {
	return float32(float64(pxmin) * (1. + .029*float64(sfb)*float64(sfb)/span/span))
}

// qeBitpressureMaxBits returns Max(minBits, 0.9*maxBits) truncated to int,
// bitpressure_strategy's max_bits update (quantize.c:1473). The C Max macro
// compares the int min_bits against the double 0.9*max_bits (int promoted to
// double); the larger, in double, is truncated toward zero on assignment to the
// int max_bits. //go:noinline so the product rounds before the compare.
//
//go:noinline
func qeBitpressureMaxBits(minBits, maxBits int) int {
	scaled := 0.9 * float64(maxBits)
	if float64(minBits) > scaled {
		return minBits
	}
	return int(scaled)
}
