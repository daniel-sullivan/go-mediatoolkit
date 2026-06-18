// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && mp3_strict

package nativemp3

// Strict-mode double-precision helpers for LAME's bit-reservoir framing
// (ResvMaxBits, reservoir.c:174).
//
// reservoir.c is integer arithmetic except for two scalings in ResvMaxBits,
// both done in C `double` because the literals 0.9 / .1 promote the int operand
// to double before the result is truncated back to int:
//
//	ResvMax *= 0.9;          // ResvMax = (int)((double)ResvMax * 0.9)
//	targBits -= .1 * mean_bits; // targBits = (int)((double)targBits - 0.1*mean_bits)
//
// In the cgo oracle reservoir.c is compiled with -ffp-contract=off, so the
// `(double)targBits - 0.1*mean_bits` is a separately rounded multiply (0.1 *
// mean_bits) followed by a rounded subtract; Go's backend would otherwise fuse
// the multiply into a single-rounded FMS. Routing the multiply through a
// //go:noinline helper makes it an opaque function-call return the SSA cannot
// pattern-match back into a fused multiply-subtract, so the strict build rounds
// the product first, matching clang. The conversion back to int truncates
// toward zero in both C (assignment) and Go (int(float64)). The `resv` prefix
// keeps these distinct from the float32 `fe*` / psymodel `ps*` helpers.

// resvScale9 returns int(float64(x) * 0.9), matching C's `ResvMax *= 0.9`. The
// multiply is //go:noinline so it cannot fuse (it is a lone product here; the
// helper is gated for uniformity with the convention).
//
//go:noinline
func resvScale9(x int) int {
	return int(float64(x) * 0.9)
}

// resvMulTenth returns the separately rounded product 0.1 * float64(x); kept
// //go:noinline so resvSubTenth's subtraction cannot fuse it into an FMS.
//
//go:noinline
func resvMulTenth(x int) float64 {
	return 0.1 * float64(x)
}

// resvSubTenth returns int(float64(targBits) - 0.1*meanBits), matching C's
// `targBits -= .1 * mean_bits` (the whole RHS computed in double, then
// truncated). The product is rounded by resvMulTenth before the subtract so the
// two operations round separately, as under -ffp-contract=off.
func resvSubTenth(targBits, meanBits int) int {
	return int(float64(targBits) - resvMulTenth(meanBits))
}
