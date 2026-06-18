// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && mp3_strict

package nativemp3

import "math"

// Strict-mode floating-point helper for PutLameVBR's peak-sample-amplitude field
// (VbrTag.c:725-726):
//
//	nPeakSignalAmplitude =
//	    abs((int) ((((FLOAT) gfc->ov_rpg.PeakSample) / 32767.0) * pow(2, 23) + .5));
//
// The (FLOAT)PeakSample promotes to double, then the expression
// `(peak/32767.0) * 2^23 + 0.5` is a genuine double multiply-then-add. Route it
// through a //go:noinline shim so Go's backend cannot fuse the `x*y + 0.5` into a
// single-rounded FMA; the mp3_strict build therefore separately-rounds, matching
// the cgo oracle compiled with -ffp-contract=off. The result is truncated to int
// (the C cast) and abs() taken by the caller. This field is only emitted when
// cfg.FindPeakSample is set; the public -V2 encoder leaves it off, but the bit
// path is kept faithful.
//
//go:noinline
func vbrPeakSignalAmplitude(peakSample float32) int {
	x := (float64(peakSample) / 32767.0) * math.Pow(2, 23)
	return int(x + 0.5)
}
