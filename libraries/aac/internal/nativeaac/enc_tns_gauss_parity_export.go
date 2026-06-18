// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Parity-only export for the TNS Gauss-window (aacenc_tns.cpp) port. Lets the
// cgo parity slice under internal/parity_tests/enc-tns-gauss/ drive the
// unexported calcGaussWindow against the genuine vendored
// FDKaacEnc_CalcGaussWindow and compare the win[] array bit-for-bit. Not part of
// the production API.

// ParityCalcGaussWindow runs calcGaussWindow and returns the filled window
// (length winSize).
func ParityCalcGaussWindow(winSize, samplingRate, transformResolution int,
	timeResolution int32, timeResolutionE int) []int32 {
	win := make([]int32, winSize)
	calcGaussWindow(win, winSize, samplingRate, transformResolution, timeResolution, timeResolutionE)
	return win
}
