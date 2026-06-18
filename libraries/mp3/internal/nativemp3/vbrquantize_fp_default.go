// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && !mp3_strict

package nativemp3

import "math"

// Default-build counterparts of the VBR quantizer leaf float helpers
// (vbrquantize.c vec_max_c / find_lowest_scalefac / k_34_4 /
// calc_sfb_noise_x34 / calc_scalefac). Same float arithmetic as the strict
// build but WITHOUT //go:noinline, so Go's backend may fuse `a*b+c` into an FMA
// and vectorize the noise loops. The default build is within PSNR noise of the
// reference but is NOT a bit-exact target; the mp3_strict build
// (vbrquantize_fp_strict.go) is the only bit-exact claim. The
// TAKEHIRO_IEEE754_HACK in vqHackQuantize is reproduced identically here — its
// magic-number floor is integer-exact in both builds — but is left fusable for
// uniformity with the rest of the default path.

// magicFloat is LAME's MAGIC_FLOAT (vbrquantize.c:90): 2^23.
const magicFloat = float64(65536 * 128)

// magicInt is LAME's MAGIC_INT (vbrquantize.c:91): the float32 bits of 2^23.
const magicInt = int32(0x4b000000)

// vqMulF returns the float32 product a*b (fusable in the default build).
func vqMulF(a, b float32) float32 { return a * b }

// vqSubF returns the float32 difference a-b (fusable in the default build).
func vqSubF(a, b float32) float32 { return a - b }

// vqMulD returns the double product a*b (fusable in the default build).
func vqMulD(a, b float64) float64 { return a * b }

// vqAddD returns the double sum a+b (fusable in the default build).
func vqAddD(a, b float64) float64 { return a + b }

// vqHackQuantize reproduces one lane of k_34_4's TAKEHIRO_IEEE754_HACK
// (vbrquantize.c:176-191); see vbrquantize_fp_strict.go for the full derivation.
func vqHackQuantize(x float64) int {
	x = x + magicFloat
	f0 := float32(x)
	rx0 := int32(math.Float32bits(f0)) - magicInt
	f1 := float32(x + float64(adj43asm[rx0]))
	return int(int32(math.Float32bits(f1)) - magicInt)
}

// vqLog10f returns float32(log10(x)) (calc_scalefac's log10f).
func vqLog10f(x float32) float32 { return float32(math.Log10(float64(x))) }
