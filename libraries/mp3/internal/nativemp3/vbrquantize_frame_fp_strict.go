// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && mp3_strict

package nativemp3

import "math"

// Strict-mode floating-point helpers for VBR_encode_frame's two bit-distribution
// fallbacks (vbrquantize.c:1388-1505). When the 'as is' encode overflows the
// per-frame / per-granule / per-channel bit budget, LAME re-distributes the
// available bits proportionally to a power of each block's already-used bit
// count: the per-channel split weights by the FOURTH root (sqrt(sqrt(n))) and
// the per-granule split weights by the square root (sqrt(n)). The redistributed
// cap is `BUDGET * f[k] / s`, an int*float32/float32 product truncated to int.
//
// Each float multiply/add on this bit-exact path routes through a //go:noinline
// shim so Go's backend cannot fuse the int*float/float into a single-rounded
// expression; the mp3_strict build therefore separately-rounds, matching the
// cgo oracle compiled with -ffp-contract=off. The `vqf` prefix keeps these
// distinct from the leaf-kernel `vq*` helpers (vbrquantize_fp_strict.go).
//
// # Float types (vbrquantize.c:1388, 1433, 1470)
//
// C declares `float f[2], s` (float32). sqrt / sqrt(sqrt(...)) compute in
// double (the libm double sqrt of the int argument) and narrow to float32 on
// the store into f[]. The accumulation `s += f[ch]` is float32. The
// redistribution `BUDGET * f[k] / s` is int*float32 -> float32 (the int promotes
// to float), `/s` float32, then truncated to int on assignment.

// vqfQuadRoot returns float32(sqrt(sqrt(n))), the per-channel weight f[ch] in
// VBR_encode_frame (vbrquantize.c:1391). The double sqrt(sqrt(...)) of the int
// argument is narrowed to float32 on the C store into the float f[] array.
//
//go:noinline
func vqfQuadRoot(n int) float32 {
	return float32(math.Sqrt(math.Sqrt(float64(n))))
}

// vqfSqrt returns float32(sqrt(n)), the per-granule weight f[gr] in
// VBR_encode_frame (vbrquantize.c:1436). The double sqrt of the int argument is
// narrowed to float32 on the C store into the float f[] array.
//
//go:noinline
func vqfSqrt(n int) float32 {
	return float32(math.Sqrt(float64(n)))
}

// vqfAddF returns the separately rounded float32 sum a+b, the `s += f[k]`
// weight accumulation (vbrquantize.c:1392/1437/1474).
//
//go:noinline
func vqfAddF(a, b float32) float32 { return a + b }

// vqfDistribute returns int(budget * f / s), the redistributed bit cap
// `BUDGET * f[k] / s` (vbrquantize.c:1400/1445/1482). budget is an int promoted
// to float32, multiplied by f (float32), divided by s (float32), truncated to
// int. //go:noinline so the product and quotient round separately, matching the
// -ffp-contract=off oracle.
//
//go:noinline
func vqfDistribute(budget int, f, s float32) int {
	return int(float32(budget) * f / s)
}
