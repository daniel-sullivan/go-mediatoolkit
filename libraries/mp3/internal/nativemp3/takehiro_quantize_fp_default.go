// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && !mp3_strict

package nativemp3

// Default-build counterparts of the quantizer-front-end float helpers
// (takehiro.c count_bits / quantize_xrpow / quantize_lines_xrpow). These are
// the same float32 mul/add as the strict build but WITHOUT //go:noinline, so
// Go's backend may fuse `a*b+c` into an FMA and vectorize the quantize loop.
// The default build is within PSNR noise of the reference but is NOT a
// bit-exact target; the mp3_strict build (takehiro_quantize_fp_strict.go) is
// the only bit-exact claim. See the "FP parity convention" note there.

// tqMul returns the float32 product a*b (fusable in the default build).
func tqMul(a, b float32) float32 { return a * b }

// tqAdd returns the float32 sum a+b (fusable in the default build).
func tqAdd(a, b float32) float32 { return a + b }

// tqDiv01 returns (1.0f - 0.4054f) / istep (quantize_lines_xrpow_01 threshold).
func tqDiv01(istep float32) float32 { return (float32(1.0) - float32(0.4054)) / istep }

// tqDivIxmax returns IXMAX_VAL / ipow20 (count_bits over-large-step guard).
func tqDivIxmax(ipow20 float32) float32 { return float32(IXMAXVAL) / ipow20 }
