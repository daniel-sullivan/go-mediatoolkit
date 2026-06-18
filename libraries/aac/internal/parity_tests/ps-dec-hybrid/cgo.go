// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package psdechybrid pins the Go port of the Fraunhofer FDK-AAC HE-AAC v2 PS
// hybrid analysis/synthesis filterbank (FDK_hybrid.cpp) — internal/nativeaac/sbr
// ps_hybrid.go — against the vendored C, compiled into this test binary via cgo.
//
// This package compiles its OWN copy of the needed vendored C source
// (FDK_hybrid + genericStds; fft_8 is inline in fft.h) and NEVER imports
// libraries/aac. It MAY, and does, import the pure-Go internal/nativeaac/sbr.
//
// Integer parity: the hybrid filterbank is a pure fixed-point subsystem
// (FIXP_DBL data, FIXP_SGL/FIXP_SPK coefficients, the inline fft_8 with its
// w_PiFOURTH twiddle), bit-identical regardless of -ffp-contract / vectorization
// — so the slice asserts EXACT integer equality unconditionally.
package psdechybrid

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

extern void qparity_psHybrid(int nSlots, const int32_t *qmfRe, const int32_t *qmfImg,
                             int32_t *outRe, int32_t *outImg);
*/
import "C"

import "unsafe"

// cPsHybrid runs the genuine FDKhybridAnalysisApply -> FDKhybridSynthesisApply
// over nSlots timeslots (each with NO_QMF_BANDS_HYBRID20==3 complex QMF inputs)
// and returns the flattened 64-band-per-slot QMF output.
func cPsHybrid(nSlots int, qmfRe, qmfImg []int32) (outRe, outImg []int32) {
	outRe = make([]int32, nSlots*64)
	outImg = make([]int32, nSlots*64)
	C.qparity_psHybrid(C.int(nSlots),
		(*C.int32_t)(unsafe.Pointer(&qmfRe[0])),
		(*C.int32_t)(unsafe.Pointer(&qmfImg[0])),
		(*C.int32_t)(unsafe.Pointer(&outRe[0])),
		(*C.int32_t)(unsafe.Pointer(&outImg[0])))
	return outRe, outImg
}
