// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrqmf pins the Go port of the Fraunhofer FDK-AAC SBR QMF filterbank —
// the complex exponential-modulated polyphase analysis (time -> 64-band complex
// subband matrix) and synthesis (subband matrix -> time) the HE-AAC v1 decoder
// runs at no_channels==64 in STD/HQ mode (internal/nativeaac/sbr) — against the
// vendored C, compiled into this test binary via cgo. The DCT-IV/DST-IV that
// drive the modulation, the hard-coded fft_32 they route through at M==32, and
// the prototype/phaseshift/SineWindow64 ROM are all exercised end-to-end and the
// in-place int32 (FIXP_DBL) output compared bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C source (qmf.cpp +
// the dct/fft/rom/scale/genericStds/trig sibling TUs) and NEVER imports
// libraries/aac — importing it would link a second copy of the FDK reference and
// clash on static symbols (the amalgamation-split reason the sibling parity
// packages document). It MAY, and does, import the pure-Go
// internal/nativeaac/sbr (and the shared internal/nativeaac primitives the QMF
// reuses).
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the QMF is a pure INTEGER fixed-point subsystem (FIXP_DBL
// Q-format data, FIXP_SGL Q1.15 ROM). The polyphase FIR (fMultDiv2 MACs), the
// DCT/DST modulation (int64-product>>32 fixmul/cplxMul kernels) and the
// saturating shifts are bit-identical regardless of -ffp-contract / vectorization,
// with no transcendental. So this slice asserts EXACT int32 equality
// unconditionally — no aac_strict gate is needed (every AAC kernel is fixed-point).
package sbrqmf

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/sbr-qmf). Mirrors
// the sibling dct / fft oracles, plus the SBR header roots the QMF needs.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to these integer kernels in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/src
#cgo LDFLAGS: -lm

#include <stdint.h>

extern int  qparity_fft(int length, int32_t *x);
extern void qparity_qmf_analysis(const int32_t *timeIn, int noCol, int lsb, int usb,
                                 int timeInE, int stride, int32_t *qmfRealFlat,
                                 int32_t *qmfImagFlat, int *pLbScale);
extern void qparity_qmf_analysis32(const int32_t *timeIn, int noCol, int lsb, int usb,
                                   int timeInE, int stride, int32_t *qmfRealFlat,
                                   int32_t *qmfImagFlat, int *pLbScale);
extern void qparity_qmf_synthesis(const int32_t *qmfRealFlat, const int32_t *qmfImagFlat,
                                  int noCol, int lsb, int usb, int outScalefactor,
                                  int lbScale, int hbScale, int ovLbScale, int ovHbScale,
                                  int ovLen, int stride, int32_t *timeOut);
extern void qparity_pfilt640(int16_t *out, int count);
extern void qparity_phaseshift64(int16_t *cosOut, int16_t *sinOut, int count);
extern void qparity_sinewindow64(int16_t *out, int pairCount);
*/
import "C"

import "unsafe"

// cFFT runs the genuine fft() dispatcher over a copy of x (interleaved complex,
// length 2*length) and returns the in-place result plus the accumulated
// scalefactor.
func cFFT(length int, x []int32) ([]int32, int) {
	out := append([]int32(nil), x...)
	sc := int(C.qparity_fft(C.int(length), (*C.int32_t)(unsafe.Pointer(&out[0]))))
	return out, sc
}

// cQMFAnalysis runs the genuine HQ STD 64-band analysis over noCol slots of
// timeIn (int32, length noCol*64*stride), returning per-slot real/imag subband
// matrices (each noCol*64, flat) and the lb_scale the C wrote.
func cQMFAnalysis(timeIn []int32, noCol, lsb, usb, timeInE, stride int) (real, imag []int32, lbScale int) {
	real = make([]int32, noCol*64)
	imag = make([]int32, noCol*64)
	var lb C.int
	C.qparity_qmf_analysis(
		(*C.int32_t)(unsafe.Pointer(&timeIn[0])),
		C.int(noCol), C.int(lsb), C.int(usb), C.int(timeInE), C.int(stride),
		(*C.int32_t)(unsafe.Pointer(&real[0])),
		(*C.int32_t)(unsafe.Pointer(&imag[0])),
		&lb)
	return real, imag, int(lb)
}

// cQMFAnalysis32 runs the genuine HQ STD 32-band analysis (dual-rate SBR) over
// noCol slots of timeIn (int32, length noCol*32*stride), returning per-slot
// real/imag subband matrices (each noCol*32, flat) and lb_scale.
func cQMFAnalysis32(timeIn []int32, noCol, lsb, usb, timeInE, stride int) (real, imag []int32, lbScale int) {
	real = make([]int32, noCol*32)
	imag = make([]int32, noCol*32)
	var lb C.int
	C.qparity_qmf_analysis32(
		(*C.int32_t)(unsafe.Pointer(&timeIn[0])),
		C.int(noCol), C.int(lsb), C.int(usb), C.int(timeInE), C.int(stride),
		(*C.int32_t)(unsafe.Pointer(&real[0])),
		(*C.int32_t)(unsafe.Pointer(&imag[0])),
		&lb)
	return real, imag, int(lb)
}

// cQMFSynthesis runs the genuine HQ STD 64-band synthesis over noCol slots of the
// complex subband input (real/imag, each noCol*64 flat), returning noCol*64 int32
// time samples.
func cQMFSynthesis(real, imag []int32, noCol, lsb, usb, outScalefactor, lbScale, hbScale, ovLbScale, ovHbScale, ovLen, stride int) []int32 {
	timeOut := make([]int32, noCol*64*stride)
	C.qparity_qmf_synthesis(
		(*C.int32_t)(unsafe.Pointer(&real[0])),
		(*C.int32_t)(unsafe.Pointer(&imag[0])),
		C.int(noCol), C.int(lsb), C.int(usb), C.int(outScalefactor),
		C.int(lbScale), C.int(hbScale), C.int(ovLbScale), C.int(ovHbScale),
		C.int(ovLen), C.int(stride),
		(*C.int32_t)(unsafe.Pointer(&timeOut[0])))
	return timeOut
}

// cPfilt640 returns the first count entries of the genuine in-RAM qmf_pfilt640
// (narrowed FIXP_SGL).
func cPfilt640(count int) []int16 {
	out := make([]int16, count)
	C.qparity_pfilt640((*C.int16_t)(unsafe.Pointer(&out[0])), C.int(count))
	return out
}

// cPhaseshift64 returns the genuine qmf_phaseshift_cos64 / _sin64 (FIXP_SGL).
func cPhaseshift64(count int) (cos, sin []int16) {
	cos = make([]int16, count)
	sin = make([]int16, count)
	C.qparity_phaseshift64(
		(*C.int16_t)(unsafe.Pointer(&cos[0])),
		(*C.int16_t)(unsafe.Pointer(&sin[0])),
		C.int(count))
	return cos, sin
}

// cSineWindow64 returns the genuine SineWindow64 as flat [re0,im0,...] int16
// (pairCount packed pairs).
func cSineWindow64(pairCount int) []int16 {
	out := make([]int16, 2*pairCount)
	C.qparity_sinewindow64((*C.int16_t)(unsafe.Pointer(&out[0])), C.int(pairCount))
	return out
}
