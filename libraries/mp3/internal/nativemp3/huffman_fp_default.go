//go:build !mp3_strict

package nativemp3

// Default-mode float32 helpers for minimp3's L3_huffman dequantization.
//
// The production build inlines the plain float32 operators and lets the
// backend fuse multiply-adds (FMADDS) where it can. Dequantized values are
// within PSNR noise of the reference but not guaranteed bit-exact in every
// ULP; the strict build (huffman_fp_strict.go) is what the parity suite
// asserts against.

func f32mul(a, b float32) float32 { return a * b }

func f32add(a, b float32) float32 { return a + b }

func f32sub(a, b float32) float32 { return a - b }

func f32div(a, b float32) float32 { return a / b }
