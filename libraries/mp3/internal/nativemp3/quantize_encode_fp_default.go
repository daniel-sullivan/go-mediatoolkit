// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && !mp3_strict

package nativemp3

// Default-build counterparts of the quantization-iteration float helpers
// (quantize.c). Same arithmetic as the strict build but WITHOUT //go:noinline,
// so Go's backend may fuse / vectorize. The default build is within PSNR noise
// of the reference but is NOT a bit-exact target; the mp3_strict build
// (quantize_encode_fp_strict.go) is the only bit-exact claim. The integer
// control flow of the iteration loop is bit-identical in both builds.

import "math"

func qeMul(a, b float32) float32 { return a * b }
func qeAdd(a, b float32) float32 { return a + b }
func qeSub(a, b float32) float32 { return a - b }

// qeXrpowCore returns sqrt(tmp*sqrt(tmp)) (init_xrpow_core_c).
func qeXrpowCore(tmp float32) float32 {
	return float32(math.Sqrt(float64(tmp) * math.Sqrt(float64(tmp))))
}

// qeTrigger95 returns trigger * .95 (amp_scalefac_bands).
func qeTrigger95(trigger float32) float32 { return float32(float64(trigger) * 0.95) }

// qePenalties returns FAST_LOG10(0.368 + 0.632*noise^3) (quant_compare).
func qePenalties(noise float64) float64 { return math.Log10(0.368 + 0.632*noise*noise*noise) }

// qeKlemmAcc returns klemm + pen (get_klemm_noise).
func qeKlemmAcc(klemm, pen float64) float64 { return klemm + pen }

// qeResFactor returns calc_target_bits's res_factor.
func qeResFactor(compressionRatio float32) float64 {
	return 0.93 + 0.07*(11.0-float64(compressionRatio))/(11.0-5.5)
}

// qeTargBits returns int(resFactor * meanBits) (calc_target_bits).
func qeTargBits(resFactor float64, meanBits int) int { return int(resFactor * float64(meanBits)) }

// qeAddBits returns int((pe - 700) / 1.4) (calc_target_bits).
func qeAddBits(pe float32) int { return int((float64(pe) - 700) / 1.4) }

// qeVbrAdjust returns num/(1+exp(3.5-pe/300.)) - sub (VBR_old_prepare adjust).
func qeVbrAdjust(num float64, pe float32, sub float64) float32 {
	return float32(num/(1+math.Exp(3.5-float64(pe)/300.)) - sub)
}

// qeVbrMaskingLower returns pow(10.0, maskingLowerDb*0.1) (VBR_*_prepare).
func qeVbrMaskingLower(maskingLowerDb float32) float32 {
	return float32(math.Pow(10.0, float64(maskingLowerDb)*0.1))
}

// qeBitpressureXmin returns pxmin*(1.+.029*sfb*sfb/span/span) (bitpressure_strategy).
func qeBitpressureXmin(pxmin float32, sfb int, span float64) float32 {
	return float32(float64(pxmin) * (1. + .029*float64(sfb)*float64(sfb)/span/span))
}

// qeBitpressureMaxBits returns Max(minBits, 0.9*maxBits) truncated (bitpressure_strategy).
func qeBitpressureMaxBits(minBits, maxBits int) int {
	scaled := 0.9 * float64(maxBits)
	if float64(minBits) > scaled {
		return minBits
	}
	return int(scaled)
}
