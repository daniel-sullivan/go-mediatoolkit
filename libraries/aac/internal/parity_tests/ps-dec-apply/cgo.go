// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package psdecapply pins the Go port of the Fraunhofer FDK-AAC HE-AAC v2 PS
// apply / full mono->stereo synthesis (CreatePsDec/ApplyPsSlot, psdec.cpp, plus
// the hybrid filterbank, the decorrelator, and the rotation) —
// internal/nativeaac/sbr ps_apply.go / ps_decorr.go / ps_hybrid.go — against the
// vendored C, compiled into this test binary via cgo.
//
// This package compiles its OWN copy of the needed vendored C source (psdec +
// psbitdec + FDK_decorrelate + FDK_hybrid + FDK_tools_rom + sbr_rom + huff_dec +
// scale + fixpoint_math + FDK_bitbuffer + genericStds; inline_fixp_cos_sin and
// GetInvInt are inline in headers) and NEVER imports libraries/aac. It MAY, and
// does, import the pure-Go internal/nativeaac/sbr.
//
// Integer parity: the whole PS synthesis is a pure fixed-point subsystem, so the
// slice asserts EXACT integer equality unconditionally.
package psdecapply

/*
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/src
#cgo LDFLAGS: -lm

#include <stdint.h>

extern void qparity_cosSin(int32_t x1, int32_t x2, int scale, int32_t *out);
extern void qparity_decorr(int nSlots, const int32_t *inRe, const int32_t *inImg,
                           int32_t *leftRe, int32_t *leftImg, int32_t *rightRe, int32_t *rightImg);
extern int qparity_initRot(const uint8_t *payload, int payloadBytes, int validBits, int usb,
                           int32_t *H11, int32_t *H12, int32_t *H21, int32_t *H22,
                           int32_t *D11, int32_t *D12, int32_t *D21, int32_t *D22);
extern int qparity_psApply(int aacSamplesPerFrame, const uint8_t *payload, int payloadBytes,
                           int validBits, int noCol, int lsb, int usb,
                           int scaleFactorLowBandNoOv, int scaleFactorLowBand,
                           int scaleFactorHighBand, int highSubband,
                           int32_t *lowBandReal, int32_t *lowBandImag,
                           int32_t *outLeftRe, int32_t *outLeftImg,
                           int32_t *outRightRe, int32_t *outRightImg);
*/
import "C"

import "unsafe"

// cCosSin probes the genuine inline_fixp_cos_sin(x1,x2,scale,out).
func cCosSin(x1, x2 int32, scale int) [4]int32 {
	var out [4]int32
	C.qparity_cosSin(C.int32_t(x1), C.int32_t(x2), C.int(scale), (*C.int32_t)(unsafe.Pointer(&out[0])))
	return out
}

// cDecorr probes the genuine PS decorrelator over nSlots slots of 71 hybrid bands.
func cDecorr(nSlots int, inRe, inImg []int32) (leftRe, leftImg, rightRe, rightImg []int32) {
	leftRe = make([]int32, nSlots*71)
	leftImg = make([]int32, nSlots*71)
	rightRe = make([]int32, nSlots*71)
	rightImg = make([]int32, nSlots*71)
	C.qparity_decorr(C.int(nSlots),
		(*C.int32_t)(unsafe.Pointer(&inRe[0])), (*C.int32_t)(unsafe.Pointer(&inImg[0])),
		(*C.int32_t)(unsafe.Pointer(&leftRe[0])), (*C.int32_t)(unsafe.Pointer(&leftImg[0])),
		(*C.int32_t)(unsafe.Pointer(&rightRe[0])), (*C.int32_t)(unsafe.Pointer(&rightImg[0])))
	return
}

// cPsApply runs the genuine PS apply over the QMF input + payload and returns the
// in-place-modified left + synthesised right QMF channels (noCol*64 each) and the
// DecodePs process flag.
func cPsApply(aacSamplesPerFrame int, payload []byte, validBits, noCol, lsb, usb,
	scaleLowNoOv, scaleLow, scaleHigh, highSubband int,
	lowBandReal, lowBandImag []int32) (outLeftRe, outLeftImg, outRightRe, outRightImg []int32, psProcess int) {

	outLeftRe = make([]int32, noCol*64)
	outLeftImg = make([]int32, noCol*64)
	outRightRe = make([]int32, noCol*64)
	outRightImg = make([]int32, noCol*64)

	// Copy the input so the C in-place modification doesn't disturb the Go run.
	lr := make([]int32, len(lowBandReal))
	li := make([]int32, len(lowBandImag))
	copy(lr, lowBandReal)
	copy(li, lowBandImag)

	var pp *C.uint8_t
	if len(payload) > 0 {
		pp = (*C.uint8_t)(unsafe.Pointer(&payload[0]))
	}

	r := C.qparity_psApply(C.int(aacSamplesPerFrame), pp, C.int(len(payload)),
		C.int(validBits), C.int(noCol), C.int(lsb), C.int(usb),
		C.int(scaleLowNoOv), C.int(scaleLow), C.int(scaleHigh), C.int(highSubband),
		(*C.int32_t)(unsafe.Pointer(&lr[0])), (*C.int32_t)(unsafe.Pointer(&li[0])),
		(*C.int32_t)(unsafe.Pointer(&outLeftRe[0])), (*C.int32_t)(unsafe.Pointer(&outLeftImg[0])),
		(*C.int32_t)(unsafe.Pointer(&outRightRe[0])), (*C.int32_t)(unsafe.Pointer(&outRightImg[0])))

	return outLeftRe, outLeftImg, outRightRe, outRightImg, int(r)
}

// cInitRot probes the genuine initSlotBasedRotation coefficient computation.
func cInitRot(payload []byte, validBits, usb int) (h11, h12, h21, h22, d11, d12, d21, d22 []int32, flag int) {
	const g = 22
	h11 = make([]int32, g)
	h12 = make([]int32, g)
	h21 = make([]int32, g)
	h22 = make([]int32, g)
	d11 = make([]int32, g)
	d12 = make([]int32, g)
	d21 = make([]int32, g)
	d22 = make([]int32, g)
	var pp *C.uint8_t
	if len(payload) > 0 {
		pp = (*C.uint8_t)(unsafe.Pointer(&payload[0]))
	}
	r := C.qparity_initRot(pp, C.int(len(payload)), C.int(validBits), C.int(usb),
		(*C.int32_t)(unsafe.Pointer(&h11[0])), (*C.int32_t)(unsafe.Pointer(&h12[0])),
		(*C.int32_t)(unsafe.Pointer(&h21[0])), (*C.int32_t)(unsafe.Pointer(&h22[0])),
		(*C.int32_t)(unsafe.Pointer(&d11[0])), (*C.int32_t)(unsafe.Pointer(&d12[0])),
		(*C.int32_t)(unsafe.Pointer(&d21[0])), (*C.int32_t)(unsafe.Pointer(&d22[0])))
	return h11, h12, h21, h22, d11, d12, d21, d22, int(r)
}
