// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && !mp3_strict

package nativemp3

import "math"

// Default-build sibling of vbrPeakSignalAmplitude (vbrtag_fp_strict.go). The
// default build may fuse the `x*y + 0.5` into a single-rounded FMA and is NOT a
// bit-exact target; see the strict file for the contract.
func vbrPeakSignalAmplitude(peakSample float32) int {
	return int((float64(peakSample)/32767.0)*math.Pow(2, 23) + 0.5)
}
