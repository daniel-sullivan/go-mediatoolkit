// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && !mp3_strict

package nativemp3

// Default-mode double-precision helpers for LAME's bit-reservoir framing
// (ResvMaxBits, reservoir.c:174).
//
// The production build inlines the plain double operators and lets the backend
// fuse the multiply-subtract / vectorize where it can. ResvMaxBits' two scalings
// only steer the per-granule bit target and reservoir build-up rate; an FMA
// fusion here can shift which target the encoder picks by at most a bit, never
// the well-formedness of the frame. The strict build
// (reservoir_encode_fp_strict.go) is the bit-exact target the parity suite
// asserts against.

// resvScale9 returns int(float64(x) * 0.9), matching C's `ResvMax *= 0.9`.
func resvScale9(x int) int {
	return int(float64(x) * 0.9)
}

// resvSubTenth returns int(float64(targBits) - 0.1*meanBits), matching C's
// `targBits -= .1 * mean_bits` (the whole RHS computed in double, then
// truncated).
func resvSubTenth(targBits, meanBits int) int {
	return int(float64(targBits) - 0.1*float64(meanBits))
}
