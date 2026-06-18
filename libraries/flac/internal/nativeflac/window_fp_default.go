//go:build !flac_strict

package nativeflac

import "math"

// Default-mode float32 helpers for window.c.
//
// The production build inlines the plain float32 operators and lets the
// backend fuse multiply-adds (FMADDS) where it can. Window coefficients
// are within PSNR noise of the reference but not guaranteed bit-exact
// in every ULP; the strict build (window_fp_strict.go) is what the
// parity suite asserts against.

func f32mul(a, b float32) float32 { return a * b }
func f32div(a, b float32) float32 { return a / b }
func f32add(a, b float32) float32 { return a + b }
func f32sub(a, b float32) float32 { return a - b }

func f32abs(a float32) float32 {
	if a < 0 {
		return -a
	}
	return a
}

func cosfStrict(angle float64) float32 {
	return float32(math.Cos(angle))
}

func expDouble(x float64) float64 { return math.Exp(x) }
