// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && !mp3_strict

package nativemp3

// Default-mode float32 helpers for LAME's frame-encode dispatcher
// (lame_encode_mp3_frame, encoder.c).
//
// The production build inlines the plain float32 operators and lets the
// backend fuse multiply-adds (FMADDS) and vectorize where it can. The
// dispatcher's perceptual-entropy smoothing and M/S energy/PE arithmetic is
// within PSNR noise of the reference but not guaranteed bit-exact in every
// ULP; the strict build (frame_encode_fp_strict.go) is the bit-exact target
// the parity suite asserts against. (FMA fusion here can change which
// stereo mode or which iteration loop the encoder *selects*, never the
// well-formedness of the emitted frame.)

func feMul(a, b float32) float32 { return a * b }

func feAdd(a, b float32) float32 { return a + b }

func feSub(a, b float32) float32 { return a - b }

func feDiv(a, b float32) float32 { return a / b }

func feFma(a, b, c float32) float32 { return a + b*c }
