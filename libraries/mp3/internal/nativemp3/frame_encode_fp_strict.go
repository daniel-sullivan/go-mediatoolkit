// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && mp3_strict

package nativemp3

// Strict-mode float32 helpers for LAME's frame-encode dispatcher
// (lame_encode_mp3_frame, encoder.c).
//
// The dispatcher's own floating-point arithmetic — the JOINT_STEREO M/S
// energy ratio, the JOINT_STEREO M/S-vs-L/R perceptual-entropy sums, and the
// CBR/ABR perceptual-entropy smoothing FIR (the `fircoef` convolution and the
// `670*5*...` normalisation) — is single-precision (C `FLOAT` == float32). In
// the parity oracle encoder.c is compiled with -ffp-contract=off, so every
// `a + b*c` is two separately rounded float32 operations: a rounded product,
// then a rounded add. Go's arm64 backend auto-fuses `a + b*c` into a
// single-rounded FMA, which diverges in the last ULP. Routing each multiply
// through a //go:noinline helper makes the product an opaque function-call
// return that Go's SSA cannot pattern-match back into a fused multiply-add;
// the +/-/÷ helpers are likewise //go:noinline so each individual operation is
// a single round-to-nearest-even float32 step, matching clang under
// -ffp-contract=off -fno-vectorize. Same technique as the psymodel slice (the
// names here carry an `fe` prefix to coexist with psymodel's `ps*` and
// huffman's `f32*` helpers). See the SKILL "FP-parity convention" rule.

//go:noinline
func feMul(a, b float32) float32 { return a * b }

//go:noinline
func feAdd(a, b float32) float32 { return a + b }

//go:noinline
func feSub(a, b float32) float32 { return a - b }

//go:noinline
func feDiv(a, b float32) float32 { return a / b }

// feFma computes a + b*c as two separately rounded float32 operations,
// matching -ffp-contract=off. The multiply goes through feMul (opaque to the
// fuser) and the add through feAdd; the strict build therefore rounds the
// product before adding, never emitting a fused FMADDS.
func feFma(a, b, c float32) float32 { return feAdd(a, feMul(b, c)) }
