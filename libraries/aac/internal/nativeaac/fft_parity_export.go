// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Thin exported wrappers around the unexported fixed-point FFT kernels (fft.go)
// so the cgo parity oracle in internal/parity_tests/fft can drive them without
// being in-package. These add no logic — they forward 1:1. The production decode
// path uses the unexported forms.

// DitFFT runs the in-place decimation-in-time FFT of length 1<<ldn over the
// interleaved complex buffer x, with the 512-point sine ROM. Wraps ditFFT
// (dit_fft, fft_rad2.cpp:131). The parity oracle passes the same Q1.15 trig ROM
// the production path uses.
func DitFFT(x []int32, ldn int) {
	ditFFT(x, ldn, sineTable512Q15[:], 512)
}

// FFT runs the DIT-FFT dispatcher slice (fft.cpp:1800) for length in
// {64,128,256,512}, mutating pInput in place and returning the SCALEFACTOR<n>
// added to the block exponent. Wraps fft.
func FFT(length int, pInput []int32) int {
	sf := 0
	fft(length, pInput, &sf)
	return sf
}

// SineTable512Q15 exposes the narrowed Q1.15 trig ROM (the genuine in-RAM
// SineTable512 under SINETABLE_16BIT) as (re,im) int16 pairs so the parity
// oracle can verify the ROM narrowing itself against the C table.
func SineTable512Q15() [][2]int16 {
	out := make([][2]int16, len(sineTable512Q15))
	for i, e := range sineTable512Q15 {
		out[i] = [2]int16{e.re, e.im}
	}
	return out
}
