// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && !mp3_strict

package nativemp3

import "math"

// Default-build counterparts of VBR_encode_frame's bit-distribution float
// helpers (vbrquantize.c:1388-1505). Same arithmetic as the strict build but
// WITHOUT //go:noinline, so Go's backend may fuse / vectorize the mixed
// int/float products. The default build is within PSNR noise of the reference
// but is NOT a bit-exact target; the mp3_strict build
// (vbrquantize_frame_fp_strict.go) is the only bit-exact claim. The integer
// clamping in VBR_encode_frame is bit-identical in both builds.

// vqfQuadRoot returns float32(sqrt(sqrt(n))), the per-channel weight f[ch].
func vqfQuadRoot(n int) float32 {
	return float32(math.Sqrt(math.Sqrt(float64(n))))
}

// vqfSqrt returns float32(sqrt(n)), the per-granule weight f[gr].
func vqfSqrt(n int) float32 {
	return float32(math.Sqrt(float64(n)))
}

// vqfAddF returns the float32 sum a+b, the weight accumulation `s += f[k]`.
func vqfAddF(a, b float32) float32 { return a + b }

// vqfDistribute returns int(budget * f / s), the redistributed bit cap.
func vqfDistribute(budget int, f, s float32) int {
	return int(float32(budget) * f / s)
}
