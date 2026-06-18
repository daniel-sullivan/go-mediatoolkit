// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package psencdownmix is the stateful HE-AAC v2 parametric-stereo DOWNMIX parity
// slice. It drives the genuine Fraunhofer FDK-AAC per-frame PS processing
// (libSBRenc/src/ps_main.cpp: FDKsbrEnc_PSEnc_ParametricStereoProcessing ->
// DownmixPSQmfData) across multiple frames over a persistent PS instance +
// analysis QMF banks + half-rate synthesis QMF bank, and asserts the downmixed
// mono QMF core (real/imag per slot) + the per-frame downmix qmfScale are
// EXACTLY equal to the pure-Go internal/nativeaac/sbr port
// (PSEncParametricStereoProcessing) fed the same planar stereo int16 input.
//
// This isolates the DownmixPSQmfData numerics (stereo scale factor, hybrid
// synthesis, half-rate QMF synthesis, qmfDelayLines swap) from the full encoder
// so a divergence pinpoints the exact frame / QMF band / value. fdk-aac SBR+PS is
// fixed-point => byte-identical / exact-integer.
//
// Compiles its OWN copy of the needed fdk encoder C TUs (libSBRenc + libFDK +
// libSYS shared) and never imports libraries/aac; it MAY import
// internal/nativeaac/sbr. Build with `-tags aacfdk`.
package psencdownmix

/*
#cgo CXXFLAGS: -std=c++11 -O2 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACenc/src
#cgo LDFLAGS: -lm

#include <stdlib.h>

extern int psdmx_run(const short *pcmInterleaved, int nFrames, int noQmfSlots,
                     int noQmfBands, int nStereoBands, int maxEnvelopes,
                     int iidQuantErrorThreshold, int *mixRealFlat,
                     int *mixImagFlat, short *downFlat, int *qmfScales);
*/
import "C"

import "unsafe"

// cPSDownmix runs the genuine fdk PS downmix over nFrames frames of interleaved
// stereo int16 PCM and returns per-frame flat real/imag mono QMF
// (noQmfSlots*noQmfBands each), the downsampled MONO time signal
// (noQmfSlots*(noQmfBands>>1) int16 each) + qmfScale.
func cPSDownmix(pcm []int16, nFrames, noQmfSlots, noQmfBands, nStereoBands, maxEnvelopes int,
	iidQuantErrorThreshold int32) (mixReal, mixImag [][]int32, down [][]int16, qmfScales []int, ok bool) {

	per := noQmfSlots * noQmfBands
	perDown := noQmfSlots * (noQmfBands >> 1)
	realFlat := make([]C.int, nFrames*per)
	imagFlat := make([]C.int, nFrames*per)
	downFlat := make([]C.short, nFrames*perDown)
	scales := make([]C.int, nFrames)

	rc := C.psdmx_run(
		(*C.short)(unsafe.Pointer(&pcm[0])), C.int(nFrames), C.int(noQmfSlots),
		C.int(noQmfBands), C.int(nStereoBands), C.int(maxEnvelopes),
		C.int(iidQuantErrorThreshold),
		&realFlat[0], &imagFlat[0], &downFlat[0], &scales[0])
	if rc != 0 {
		return nil, nil, nil, nil, false
	}

	mixReal = make([][]int32, nFrames)
	mixImag = make([][]int32, nFrames)
	down = make([][]int16, nFrames)
	qmfScales = make([]int, nFrames)
	for f := 0; f < nFrames; f++ {
		r := make([]int32, per)
		im := make([]int32, per)
		for i := 0; i < per; i++ {
			r[i] = int32(realFlat[f*per+i])
			im[i] = int32(imagFlat[f*per+i])
		}
		d := make([]int16, perDown)
		for i := 0; i < perDown; i++ {
			d[i] = int16(downFlat[f*perDown+i])
		}
		mixReal[f] = r
		mixImag[f] = im
		down[f] = d
		qmfScales[f] = int(scales[f])
	}
	return mixReal, mixImag, down, qmfScales, true
}
