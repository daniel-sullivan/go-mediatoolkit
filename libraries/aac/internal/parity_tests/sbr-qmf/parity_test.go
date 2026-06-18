// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrqmf

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"

	"github.com/stretchr/testify/require"
)

// TestParityPfilt640 verifies the Go QFC-narrowed qmf_pfilt640 ROM (330 entries)
// matches the genuine in-RAM FIXP_SGL qmf_pfilt640 the QMF_COEFF_16BIT build
// links, entry for entry.
func TestParityPfilt640(t *testing.T) {
	const count = 330
	require.Equal(t, cPfilt640(count), sbr.Pfilt640())
}

// TestParityPhaseshift64 verifies the Go QTC-narrowed phaseshift cos64/sin64 ROM
// matches the genuine in-RAM FIXP_QTW tables entry for entry.
func TestParityPhaseshift64(t *testing.T) {
	cCos, cSin := cPhaseshift64(64)
	gCos, gSin := sbr.Phaseshift64()
	require.Equal(t, cCos, gCos, "cos64")
	require.Equal(t, cSin, gSin, "sin64")
}

// TestParitySineWindow64 verifies the Go WTCP-narrowed SineWindow64 slope ROM
// (the windowSlopes[0][0][4] twiddle the QMF L==64 DCT uses) matches the genuine
// in-RAM packed FIXP_SPK SineWindow64, flat re/im for all 32 pairs.
func TestParitySineWindow64(t *testing.T) {
	require.Equal(t, cSineWindow64(32), sbr.SineWindow64Flat())
}

// randSpectrum makes n interleaved complex Q1.31 samples, scaled down to leave
// headroom so the in-place fixed-point transforms don't overflow.
func randSpectrum(r *rand.Rand, n int) []int32 {
	x := make([]int32, 2*n)
	for i := range x {
		x[i] = int32(r.Uint32()) >> 4
	}
	return x
}

// TestParityFFT32 drives the hard-coded fft_32 — the leaf the QMF L==64 DCT-IV/
// DST-IV route through at M==32 — over random Q1.31 spectra and compares the
// in-place int32 output and the accumulated scalefactor bit-for-bit against the
// genuine fft(32, ...).
func TestParityFFT32(t *testing.T) {
	r := rand.New(rand.NewPCG(0x5B, 0x9D))
	for iter := 0; iter < 200; iter++ {
		x := randSpectrum(r, 32)
		gotC, scC := cFFT(32, x)
		gotN, scN := sbr.FFT32(x)
		require.Equal(t, scC, scN, "scalefactor iter %d", iter)
		require.Equal(t, gotC, gotN, "fft_32 output iter %d", iter)
	}
}

// TestParityFFT16 drives the hard-coded fft_16 (fft_32's mirror) over random
// Q1.31 spectra, cross-checking the kernel and scalefactor.
func TestParityFFT16(t *testing.T) {
	r := rand.New(rand.NewPCG(0x16, 0x16))
	for iter := 0; iter < 200; iter++ {
		x := randSpectrum(r, 16)
		gotC, scC := cFFT(16, x)
		gotN, scN := sbr.FFTLen(16, x)
		require.Equal(t, scC, scN, "scalefactor iter %d", iter)
		require.Equal(t, gotC, gotN, "fft_16 output iter %d", iter)
	}
}

// TestParityQMFAnalysis drives the full HQ STD 64-band analysis over random int32
// (FIXP_QAS) time input and compares the complex subband matrix (per slot) and
// the lb_scale bit-for-bit against the genuine qmfInitAnalysisFilterBank +
// qmfAnalysisFiltering. lsb/usb cover several SBR band crossover positions.
func TestParityQMFAnalysis(t *testing.T) {
	r := rand.New(rand.NewPCG(0xA1, 0x17))
	cases := []struct {
		noCol, lsb, usb, timeInE, stride int
	}{
		{noCol: 1, lsb: 0, usb: 64, timeInE: 0, stride: 1},
		{noCol: 2, lsb: 16, usb: 48, timeInE: 0, stride: 1},
		{noCol: 4, lsb: 32, usb: 64, timeInE: 1, stride: 1},
		{noCol: 6, lsb: 20, usb: 40, timeInE: 0, stride: 2},
		{noCol: 8, lsb: 0, usb: 32, timeInE: 2, stride: 1},
	}
	for ci, c := range cases {
		for iter := 0; iter < 25; iter++ {
			// noCol slots, no_channels==64 samples per slot, at the given stride.
			timeIn := make([]int32, c.noCol*64*c.stride)
			for i := range timeIn {
				timeIn[i] = int32(r.Uint32()) >> 5
			}

			cReal, cImag, cLb := cQMFAnalysis(timeIn, c.noCol, c.lsb, c.usb, c.timeInE, c.stride)
			nReal, nImag, nLb := sbr.RunAnalysis(timeIn, c.noCol, c.lsb, c.usb, c.timeInE, c.stride)

			require.Equal(t, cLb, nLb, "case %d iter %d lb_scale", ci, iter)
			require.Equal(t, cReal, nReal, "case %d iter %d real", ci, iter)
			require.Equal(t, cImag, nImag, "case %d iter %d imag", ci, iter)
		}
	}
}

// TestParityQMFAnalysis32 drives the 32-band analysis filter bank (the dual-rate
// SBR analysis) over random int32 time input and compares the complex subband
// matrix + lb_scale bit-for-bit against the genuine 32-band qmfAnalysisFiltering.
// This covers the L==32 forward-modulation else-branch + trailing complex
// rotation and the qmf_phaseshift_cos32/sin32 + SineWindow32 ROM.
func TestParityQMFAnalysis32(t *testing.T) {
	r := rand.New(rand.NewPCG(0xB2, 0x29))
	cases := []struct {
		noCol, lsb, usb, timeInE, stride int
	}{
		{noCol: 1, lsb: 0, usb: 32, timeInE: 0, stride: 1},
		{noCol: 2, lsb: 8, usb: 24, timeInE: 0, stride: 1},
		{noCol: 4, lsb: 16, usb: 32, timeInE: 1, stride: 1},
		{noCol: 6, lsb: 10, usb: 20, timeInE: 0, stride: 2},
		{noCol: 8, lsb: 0, usb: 16, timeInE: 2, stride: 1},
		{noCol: 32, lsb: 23, usb: 32, timeInE: 0, stride: 1},
	}
	for ci, c := range cases {
		for iter := 0; iter < 25; iter++ {
			timeIn := make([]int32, c.noCol*32*c.stride)
			for i := range timeIn {
				timeIn[i] = int32(r.Uint32()) >> 5
			}
			cReal, cImag, cLb := cQMFAnalysis32(timeIn, c.noCol, c.lsb, c.usb, c.timeInE, c.stride)
			nReal, nImag, nLb := sbr.RunAnalysis32(timeIn, c.noCol, c.lsb, c.usb, c.timeInE, c.stride)
			require.Equal(t, cLb, nLb, "case %d iter %d lb_scale", ci, iter)
			require.Equal(t, cReal, nReal, "case %d iter %d real", ci, iter)
			require.Equal(t, cImag, nImag, "case %d iter %d imag", ci, iter)
		}
	}
}

// TestParityQMFSynthesis drives the full HQ STD 64-band synthesis over random
// complex subband input and compares the int32 time output bit-for-bit against
// the genuine qmfInitSynthesisFilterBank + qmfChangeOutScalefactor +
// qmfSynthesisFiltering. The scale-factor seeds and out-scale exercise the
// per-area headroom logic.
func TestParityQMFSynthesis(t *testing.T) {
	r := rand.New(rand.NewPCG(0x53, 0x4E))
	cases := []struct {
		noCol, lsb, usb, outSc, lb, hb, ovLb, ovHb, ovLen, stride int
	}{
		{noCol: 1, lsb: 0, usb: 64, outSc: 0, lb: 0, hb: 0, ovLb: 0, ovHb: 0, ovLen: 0, stride: 1},
		{noCol: 2, lsb: 16, usb: 48, outSc: 1, lb: 1, hb: 2, ovLb: 1, ovHb: 2, ovLen: 1, stride: 1},
		{noCol: 4, lsb: 32, usb: 64, outSc: 2, lb: 2, hb: 1, ovLb: 3, ovHb: 0, ovLen: 2, stride: 1},
		{noCol: 6, lsb: 20, usb: 40, outSc: 0, lb: 3, hb: 3, ovLb: 2, ovHb: 1, ovLen: 3, stride: 2},
	}
	for ci, c := range cases {
		for iter := 0; iter < 25; iter++ {
			real := make([]int32, c.noCol*64)
			imag := make([]int32, c.noCol*64)
			for i := range real {
				real[i] = int32(r.Uint32()) >> 6
				imag[i] = int32(r.Uint32()) >> 6
			}

			cOut := cQMFSynthesis(real, imag, c.noCol, c.lsb, c.usb, c.outSc, c.lb, c.hb, c.ovLb, c.ovHb, c.ovLen, c.stride)
			nOut := sbr.RunSynthesis(real, imag, c.noCol, c.lsb, c.usb, c.outSc, c.lb, c.hb, c.ovLb, c.ovHb, c.ovLen, c.stride)

			require.Equal(t, cOut, nOut, "case %d iter %d", ci, iter)
		}
	}
}
