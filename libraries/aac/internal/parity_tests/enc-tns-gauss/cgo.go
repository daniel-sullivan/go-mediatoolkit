// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package enctnsgauss pins the Go port of the Fraunhofer FDK-AAC encoder TNS
// autocorrelation Gauss window — the static FDKaacEnc_CalcGaussWindow
// (libAACenc/src/aacenc_tns.cpp:1094-1139) — against the genuine vendored C,
// compiled into this test binary via cgo.
//
// CalcGaussWindow derives the Gaussian ACF window from a requested time
// resolution: a fixed-point window exponent (fDivNorm/fMult/fMultNorm/fPow2Div2)
// followed by fPow(EULER_M, EULER_E, ...) per coefficient. Every value is an
// int32 FIXP_DBL with carried block exponents; the whole win[] array is compared
// element-for-element, bit-for-bit.
//
// The static CalcGaussWindow is reached by #include "aacenc_tns.cpp" in
// bridge.cpp (the established same-TU pattern for vendored statics). This package
// compiles its OWN copy of the needed C TUs (aacenc_tns.cpp + FDK_lpc.cpp +
// aacEnc_rom.cpp + fixpoint_math.cpp + FDK_tools_rom.cpp + genericStds.cpp +
// scale.cpp) and NEVER imports libraries/aac. It MAY, and does, import the
// pure-Go internal/nativeaac.
//
// The whole AAC island is fenced behind the opt-in aacfdk build tag; a default
// `go build ./...` links none of it. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: libfdk-aac ENCODE is FIXED-POINT — CalcGaussWindow is an
// integer/ROM-table fPow chain (no transcendental, no float), bit-identical
// regardless of -ffp-contract / vectorization. So it asserts EXACT int equality.
// The oracle links the genuine static FDKaacEnc_CalcGaussWindow (oracle_kind ==
// real_vendored) through the thin extern shim in bridge.cpp — no hand-twin.
package enctnsgauss

/*
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo LDFLAGS: -lm

#include <stdint.h>

extern void gparity_calc_gauss_window(int32_t *win, int winSize, int samplingRate,
    int transformResolution, int32_t timeResolution, int timeResolutionE);
*/
import "C"

import "unsafe"

// cCalcGaussWindow runs the genuine static FDKaacEnc_CalcGaussWindow and returns
// the filled window (length winSize).
func cCalcGaussWindow(winSize, samplingRate, transformResolution int,
	timeResolution int32, timeResolutionE int) []int32 {
	win := make([]int32, winSize)
	var p *C.int32_t
	if winSize > 0 {
		p = (*C.int32_t)(unsafe.Pointer(&win[0]))
	}
	C.gparity_calc_gauss_window(p, C.int(winSize), C.int(samplingRate),
		C.int(transformResolution), C.int32_t(timeResolution), C.int(timeResolutionE))
	return win
}
