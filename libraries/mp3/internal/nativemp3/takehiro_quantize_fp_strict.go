// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && mp3_strict

package nativemp3

// Strict-mode single-precision helpers for LAME's quantizer front-end
// (takehiro.c count_bits / quantize_xrpow / quantize_lines_xrpow).
//
// takehiro.c's float quantizer (the non-TAKEHIRO_IEEE754_HACK branch the
// vendored config.h selects) multiplies each xr^(3/4) coefficient by the
// inverse step size istep and adds the rounding-adjust adj43[rx], all in C
// `FLOAT` (= float32). The cgo oracle compiles this with -ffp-contract=off, so
// `x = xr * istep` and `x += adj43[rx]` round separately; Go's backend would
// otherwise fuse a later `a*b+c`. Routing every float32 mul/add through these
// //go:noinline helpers makes them opaque to the SSA pattern matcher, so the
// strict build separately-rounds each operation, matching clang. The `tq`
// prefix keeps them distinct from the psymodel `ps*` and frame `fe*` helpers.
//
// The double-precision narrowings (count_bits's `IXMAX_VAL / IPOW20(gg)` and
// quantize_xrpow's `0.634521682242439 / IPOW20(gain)`) reproduce the C mixed
// float/double semantics directly with float32(... float64 ...) at the call
// sites — they are one-off scalars, not the FMA-sensitive inner loop, so they
// do not need a noinline helper.

// tqMul returns the separately rounded float32 product a*b, matching takehiro.c's
// `xr * istep`.
//
//go:noinline
func tqMul(a, b float32) float32 { return a * b }

// tqAdd returns the separately rounded float32 sum a+b, matching takehiro.c's
// `x += QUANTFAC(rx)` (x += adj43[rx]).
//
//go:noinline
func tqAdd(a, b float32) float32 { return a + b }

// tqDiv01 returns the float32 threshold (1.0f - 0.4054f) / istep used by
// quantize_lines_xrpow_01 (takehiro.c:116). The (1.0f - 0.4054f) numerator is a
// single-rounded float32 constant; the divide is float32. //go:noinline so it
// rounds before any caller arithmetic, matching the -ffp-contract=off oracle.
//
//go:noinline
func tqDiv01(istep float32) float32 { return (float32(1.0) - float32(0.4054)) / istep }

// tqDivIxmax returns the float32 quotient IXMAX_VAL / ipow20, count_bits's
// over-large-step guard (takehiro.c:774). IXMAX_VAL is an int literal C promotes
// to FLOAT for the FLOAT/FLOAT divide, so the numerator is float32(IXMAXVAL).
//
//go:noinline
func tqDivIxmax(ipow20 float32) float32 { return float32(IXMAXVAL) / ipow20 }
