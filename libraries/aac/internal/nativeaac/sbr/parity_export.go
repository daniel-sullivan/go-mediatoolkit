// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// Thin exported drivers + ROM views for the sbr-qmf cgo parity oracle
// (internal/parity_tests/sbr-qmf). They add no logic: each mirrors exactly what
// the C bridge does (init a fresh 64-band bank over cleared states, then run the
// filtering), so the oracle can drive the Go port and the genuine C with
// identical inputs and assert EXACT int32 equality.

// Pfilt640 returns the narrowed FIXP_SGL qmf_pfilt640 ROM (330 entries) so the
// oracle can verify it against the genuine in-RAM C symbol.
func Pfilt640() []int16 { return qmfPfilt640[:] }

// Phaseshift64 returns the narrowed qmf_phaseshift_cos64 / _sin64 ROM (64 each).
func Phaseshift64() (cos, sin []int16) { return qmfPhaseshiftCos64[:], qmfPhaseshiftSin64[:] }

// SineWindow64Flat returns the narrowed SineWindow64 slope ROM as flat
// [re0,im0,...] int16 (64 == 32 pairs), the windowSlopes[0][0][4] twiddle the
// QMF L==64 DCT uses.
func SineWindow64Flat() []int16 { return sineWindow64Flat[:] }

// RunAnalysis is the exported driver mirroring the C bridge qparity_qmf_analysis:
// it inits a fresh 64-band HQ STD analysis bank over cleared FIXP_QAS states and
// runs AnalysisFiltering over noCol slots of timeIn (int32, FIXP_QAS), returning
// the per-slot complex subband matrices flat (noCol*64 each) and the lb_scale.
func RunAnalysis(timeIn []int32, noCol, lsb, usb, timeInE, stride int) (real, imag []int32, lbScale int) {
	var fb FilterBank
	// Analysis states are 10*no_channels (FDK_qmf_domain.cpp:127): the slot feed
	// reaches offset+no_channels == 10*64 before the FIR consumes it.
	states := make([]int32, 10*64)
	InitAnalysisFilterBank(&fb, states, noCol, lsb, usb, 64, 0)

	realFlat := make([]int32, noCol*64)
	imagFlat := make([]int32, noCol*64)
	qmfReal := make([][]int32, noCol)
	qmfImag := make([][]int32, noCol)
	for i := 0; i < noCol; i++ {
		qmfReal[i] = realFlat[i*64 : i*64+64]
		qmfImag[i] = imagFlat[i*64 : i*64+64]
	}

	var sf ScaleFactor
	workBuffer := make([]int32, 2*64)
	AnalysisFiltering(&fb, qmfReal, qmfImag, &sf, timeIn, timeInE, stride, workBuffer)
	return realFlat, imagFlat, sf.LbScale
}

// RunAnalysis32 is the exported driver for the 32-band analysis filter bank (the
// dual-rate SBR analysis), mirroring qparity_qmf_analysis32. timeIn has length
// noCol*32*stride; the complex output is noCol*32 each.
func RunAnalysis32(timeIn []int32, noCol, lsb, usb, timeInE, stride int) (real, imag []int32, lbScale int) {
	var fb FilterBank
	states := make([]int32, 10*32)
	InitAnalysisFilterBank(&fb, states, noCol, lsb, usb, 32, 0)

	realFlat := make([]int32, noCol*32)
	imagFlat := make([]int32, noCol*32)
	qmfReal := make([][]int32, noCol)
	qmfImag := make([][]int32, noCol)
	for i := 0; i < noCol; i++ {
		qmfReal[i] = realFlat[i*32 : i*32+32]
		qmfImag[i] = imagFlat[i*32 : i*32+32]
	}

	var sf ScaleFactor
	workBuffer := make([]int32, 2*64)
	AnalysisFiltering(&fb, qmfReal, qmfImag, &sf, timeIn, timeInE, stride, workBuffer)
	return realFlat, imagFlat, sf.LbScale
}

// RunSynthesis is the exported driver mirroring the C bridge
// qparity_qmf_synthesis: it inits a fresh 64-band HQ STD synthesis bank over
// cleared FIXP_QSS states, applies outScalefactor, seeds the QMF_SCALE_FACTOR,
// and runs SynthesisFiltering over noCol slots, returning noCol*64 int32 time
// samples (at the given stride).
func RunSynthesis(realFlat, imagFlat []int32, noCol, lsb, usb, outScalefactor, lbScale, hbScale, ovLbScale, ovHbScale, ovLen, stride int) []int32 {
	var fb FilterBank
	states := make([]int32, 9*64)
	InitSynthesisFilterBank(&fb, states, noCol, lsb, usb, 64, 0)
	ChangeOutScalefactor(&fb, outScalefactor)

	qmfReal := make([][]int32, noCol)
	qmfImag := make([][]int32, noCol)
	for i := 0; i < noCol; i++ {
		qmfReal[i] = realFlat[i*64 : i*64+64]
		qmfImag[i] = imagFlat[i*64 : i*64+64]
	}

	sf := ScaleFactor{LbScale: lbScale, HbScale: hbScale, OvLbScale: ovLbScale, OvHbScale: ovHbScale}
	timeOut := make([]int32, noCol*64*stride)
	workBuffer := make([]int32, 2*64)
	SynthesisFiltering(&fb, qmfReal, qmfImag, &sf, ovLen, timeOut, stride, workBuffer)
	return timeOut
}

// FFT32 runs the shared hard-coded fft_32 over a copy of x (interleaved complex,
// 64 int32) via the exported nativeaac dispatcher, returning the result and the
// accumulated scalefactor — so the oracle can pin the QMF's M==32 FFT leaf
// directly against the genuine fft(32, ...). It lives here (not in nativeaac)
// only because the QMF is its consumer; the kernel itself is the AAC-LC one.
func FFT32(x []int32) ([]int32, int) {
	out := append([]int32(nil), x...)
	sc := nativeaac.Fft(32, out)
	return out, sc
}

// FFTLen runs the shared fft() dispatcher for an arbitrary supported length over
// a copy of x, for cross-checking fft_16/fft_32 against the genuine kernels.
func FFTLen(length int, x []int32) ([]int32, int) {
	out := append([]int32(nil), x...)
	sc := nativeaac.Fft(length, out)
	return out, sc
}
