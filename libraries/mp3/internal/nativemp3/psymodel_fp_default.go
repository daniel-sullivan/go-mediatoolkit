// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && !mp3_strict

package nativemp3

import "math"

// Default-mode float32 helpers for LAME's psychoacoustic model.
//
// The production build inlines the plain float32 operators and lets the
// backend fuse multiply-adds (FMADDS) and vectorize where it can. The model's
// outputs (per-band energies, masking thresholds, perceptual entropy, the
// block-type decision) are within PSNR noise of the reference but are NOT a
// bit-exact target — FMA fusion can even change which block type the encoder
// selects (never correctness). The strict build (psymodel_fp_strict.go) is
// what the parity suite asserts against.

func psMul(a, b float32) float32 { return a * b }

func psAdd(a, b float32) float32 { return a + b }

func psSub(a, b float32) float32 { return a - b }

func psDiv(a, b float32) float32 { return a / b }

// psMulD computes d*x in double precision narrowed to float32, matching C's
// `SQRT2 * gi[k]` (double literal × float). The default build keeps the same
// double-rounded semantics — there is no float32 fast path to take here, the
// double multiply is the meaning of the C expression, not a parity contrivance.
func psMulD(d float64, x float32) float32 { return float32(d * float64(x)) }

// psFma computes a + b*c; the default build may fuse this into a single FMADDS.
func psFma(a, b, c float32) float32 { return a + b*c }

// psFmaSub computes a - b*c; the default build may fuse this into an FMSUBS.
func psFmaSub(a, b, c float32) float32 { return a - b*c }

// psCosf is the single-precision cosine shim, computed as the double kernel
// narrowed to float32 to match the oracle's cosf #define on every platform.
func psCosf(x float32) float32 { return float32(math.Cos(float64(x))) }

// psPowf is the single-precision power shim used by quantize_pvt.c's ATHmdct /
// athAdjust, computed as the double kernel narrowed to float32 to match the
// oracle's powf #define on every platform.
func psPowf(x, y float32) float32 { return float32(math.Pow(float64(x), float64(y))) }

// psMulD64 / psAddD64 / psSubD64 are the DOUBLE-precision arithmetic helpers;
// the default build inlines the plain operators and may fuse them into an FMA.
func psMulD64(a, b float64) float64 { return a * b }
func psAddD64(a, b float64) float64 { return a + b }
func psSubD64(a, b float64) float64 { return a - b }
