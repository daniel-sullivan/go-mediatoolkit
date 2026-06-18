// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && mp3_strict

package nativemp3

import "math"

// Strict-mode floating-point helpers for LAME's VBR quantizer leaf kernels
// (vbrquantize.c vec_max_c / find_lowest_scalefac / k_34_4 / calc_sfb_noise_x34
// / calc_scalefac). These route every float multiply/add on a bit-exact path
// through //go:noinline shims so Go's backend cannot fuse `a*b+c` into a
// single-rounded FMA; the mp3_strict build therefore separately-rounds each
// operation, matching the cgo oracle compiled with -ffp-contract=off. The `vq`
// prefix keeps them distinct from the psymodel `ps*`, frame `fe*` and takehiro
// `tq*` helpers.
//
// # The TAKEHIRO_IEEE754_HACK (vbrquantize.c:76-110, k_34_4 :169-207)
//
// The vendored config.h DOES define TAKEHIRO_IEEE754_HACK (config.h:89), so for
// vbrquantize.c DOUBLEX == double and k_34_4 takes the magic-number branch:
//
//	#define MAGIC_FLOAT (65536*128)   // 8388608.0 == 2^23
//	#define MAGIC_INT   0x4b000000    // the IEEE-754 bits of 2^23 as a float
//
// Adding MAGIC_FLOAT to a small non-negative x (x <= IXMAX_VAL = 8206) and
// reading the result's raw 32-bit pattern through a float/int union yields
// MAGIC_INT + trunc(x): the float's mantissa holds the integer part because the
// 2^23 bias forces the binade where the unit-in-last-place is exactly 1.0. So
// `fi.i - MAGIC_INT` is the floor of x, computed by the FPU's round-to-nearest
// add rather than a cast. The second pass adds adj43asm[rx] (the rounding
// adjust) and re-extracts, giving the final quantized integer. vqHackQuantize
// reproduces this bit-for-bit: the add is done in float32 (the union is `float`,
// not double — fi_union holds a 4-byte float), the bits are reinterpreted with
// math.Float32bits, and MAGIC_INT is subtracted as a signed int32.
//
// Note the C stores `x[k] += MAGIC_FLOAT` into the DOUBLE x[] but copies it into
// the FLOAT union member fi[k].f — the double->float narrowing on that store is
// what makes the trick land in the 23-bit mantissa. vqMulD/vqAddD below carry
// the double arithmetic of the x[] array (sfpow34*xr34 is double*float here
// because DOUBLEX==double), and vqHackQuantize performs the float-narrowing
// union store explicitly.

// magicFloat is LAME's MAGIC_FLOAT (vbrquantize.c:90): 65536*128 == 2^23, the
// IEEE-754 single-precision bias that floats the integer part into the mantissa.
const magicFloat = float64(65536 * 128)

// magicInt is LAME's MAGIC_INT (vbrquantize.c:91): 0x4b000000, the raw float32
// bit pattern of 2^23, subtracted from the reinterpreted union to recover the
// truncated integer.
const magicInt = int32(0x4b000000)

// vqMulF returns the separately rounded float32 product a*b, matching
// vbrquantize.c's float32 multiplies (sfpow34*xr34[i], ipow20[sf]*xr34,
// sfpow*pow43[l3]).
//
//go:noinline
func vqMulF(a, b float32) float32 { return a * b }

// vqSubF returns the separately rounded float32 difference a-b, matching
// vbrquantize.c's `fabsf(xr[i]) - sfpow*pow43[l3[i]]`.
//
//go:noinline
func vqSubF(a, b float32) float32 { return a - b }

// vqMulD returns the separately rounded double product a*b. In
// calc_sfb_noise_x34 the squared residuals x[k]*x[k] are double multiplies
// (x[] is the DOUBLEX array, == double under the hack; each residual is a
// FLOAT value promoted to double on store). NOTE the input quantize term
// `sfpow34 * xr34[k]` is a FLOAT*FLOAT product (float32) promoted to double on
// store, so it uses vqMulF then float64(...), NOT vqMulD.
//
//go:noinline
func vqMulD(a, b float64) float64 { return a * b }

// vqAddD returns the separately rounded double sum a+b, used for the noise
// accumulation `xfsf += (x0*x0 + x1*x1) + (x2*x2 + x3*x3)` (double squares and
// sums, accumulated into the FLOAT xfsf promoted to double).
//
//go:noinline
func vqAddD(a, b float64) float64 { return a + b }

// vqHackQuantize reproduces one lane of k_34_4's TAKEHIRO_IEEE754_HACK
// (vbrquantize.c:176-191). x is the DOUBLEX coefficient sfpow34*xr34. It:
//
//	x += MAGIC_FLOAT;            // double add
//	fi.f = (float) x;            // narrowing union store
//	fi.f = (float) x + adj43asm[fi.i - MAGIC_INT];   // re-store with adjust
//	return fi.i - MAGIC_INT;     // recovered quantized integer
//
// Critically, the C keeps x in DOUBLE for the second store's add: the source is
//
//	fi[k].f = x[k] + adj43asm[fi[k].i - MAGIC_INT];
//
// where x[k] is the DOUBLE 8388608+x and adj43asm[rx0] is FLOAT promoted to
// double, so the add is performed in DOUBLE and narrowed to float32 only on the
// union store. The first store `fi[k].f = x[k]` narrows the same double
// separately to recover the integer part. Narrowing x BEFORE the add (in
// float32) would lose the half-ULP that decides the round-to-even at the 2^23
// boundary, so the second add MUST be done in double. //go:noinline so the
// narrowings and the integer extraction stay opaque to the SSA pattern matcher,
// matching the -ffp-contract=off oracle.
//
//go:noinline
func vqHackQuantize(x float64) int {
	// x[k] += MAGIC_FLOAT (double).
	x = x + magicFloat
	// fi.f = x (double -> float narrowing store into the union's float member).
	f0 := float32(x)
	// rx0 = fi.i - MAGIC_INT: reinterpret the float bits as int32, subtract MAGIC_INT.
	rx0 := int32(math.Float32bits(f0)) - magicInt
	// fi.f = x + adj43asm[rx0]: x stays DOUBLE; adj43asm[] is FLOAT promoted to
	// double; the add is double, narrowed to float32 on the union store.
	f1 := float32(x + float64(adj43asm[rx0]))
	// l3 = fi.i - MAGIC_INT.
	return int(int32(math.Float32bits(f1)) - magicInt)
}

// vqLog10f returns float32(log10f(x)) computed as the platform log10 narrowed,
// matching the cgo oracle's `log10f`. calc_scalefac (vbrquantize.c:320) calls
// log10f on the float ratio l3_xmin/bw; LAME's machine.h does not redefine
// log10f, so it is the libm single-precision log10. To stay portable and match
// the -ffp-contract=off oracle (whose log10f the SKILL's transcendental rule
// pins to the double kernel narrowed), compute it the same way Go's strict.go
// FP-parity convention does for the rest of the suite. //go:noinline so the
// narrowing rounds before any caller arithmetic.
//
//go:noinline
func vqLog10f(x float32) float32 { return float32(math.Log10(float64(x))) }
