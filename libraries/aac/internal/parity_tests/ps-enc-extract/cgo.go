// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package psencextract pins the Go port of the Fraunhofer FDK-AAC parametric
// stereo parameter EXTRACTION + quantization + DPCM rate-decision
// (libSBRenc/src/ps_encode.cpp — FDKsbrEnc_PSEncode, in internal/nativeaac/sbr)
// against the vendored C via cgo. Identical left/right hybrid sub-band data is
// driven through both sides and the resulting PS_OUT (modes, enables, per-env
// quantized IID/ICC indices, DPCM directions) is compared for EXACT integer
// equality.
//
// Compiles its OWN copy of the needed vendored C (ps_encode + FDK_tools_rom +
// fixpoint_math + FDK_bitbuffer + genericStds), with malloc-backed RAM stubs in
// the bridge. NEVER imports libraries/aac. Fenced behind aacfdk. Fixed-point =>
// EXACT integer equality.
package psencextract

/*
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo LDFLAGS: -lm

#include <stdint.h>

extern void *psextract_new(int psEncMode, int iidQuantErrorThreshold);
extern void psextract_free(void *handle);
extern void psextract_run(
    void *handle, const int *hybridFlat, const unsigned char *dynBandScale,
    int maxEnvelopes, int frameSize, int sendHeader, int *enablePSHeader,
    int *enableIID, int *iidMode, int *enableICC, int *iccMode, int *frameClass,
    int *nEnvelopes, int *frameBorder, int *deltaIID, int *deltaICC,
    int *iidFlat, int *iccFlat, int *iidLast, int *iccLast);
*/
import "C"

import "unsafe"

const (
	hybFramesize = 32
	nbHybrid     = 71
	psMaxEnv     = 4
	psMaxBands   = 20
)

type psExtractResult struct {
	enablePSHeader, enableIID, iidMode, enableICC, iccMode int
	frameClass, nEnvelopes                                 int
	frameBorder                                            [4]int32
	deltaIID, deltaICC                                     [4]int32
	iidFlat, iccFlat                                       [80]int32
	iidLast, iccLast                                       [20]int32
}

type cPSExtract struct{ h unsafe.Pointer }

func cNewExtract(psEncMode int, iidQuantErrorThreshold int32) *cPSExtract {
	return &cPSExtract{h: C.psextract_new(C.int(psEncMode), C.int(iidQuantErrorThreshold))}
}

func (c *cPSExtract) free() { C.psextract_free(c.h) }

func (c *cPSExtract) run(hybridFlat []int32, dynBandScale []uint8, maxEnvelopes, frameSize, sendHeader int) psExtractResult {
	hyb := make([]C.int, len(hybridFlat))
	for i, v := range hybridFlat {
		hyb[i] = C.int(v)
	}
	dyn := make([]C.uchar, psMaxBands)
	for i := 0; i < psMaxBands && i < len(dynBandScale); i++ {
		dyn[i] = C.uchar(dynBandScale[i])
	}

	var enablePSHeader, enableIID, iidMode, enableICC, iccMode C.int
	var frameClass, nEnvelopes C.int
	frameBorder := make([]C.int, 4)
	deltaIID := make([]C.int, 4)
	deltaICC := make([]C.int, 4)
	iidFlat := make([]C.int, 80)
	iccFlat := make([]C.int, 80)
	iidLast := make([]C.int, 20)
	iccLast := make([]C.int, 20)

	C.psextract_run(c.h, &hyb[0], &dyn[0], C.int(maxEnvelopes), C.int(frameSize),
		C.int(sendHeader), &enablePSHeader, &enableIID, &iidMode, &enableICC,
		&iccMode, &frameClass, &nEnvelopes, &frameBorder[0], &deltaIID[0],
		&deltaICC[0], &iidFlat[0], &iccFlat[0], &iidLast[0], &iccLast[0])

	var r psExtractResult
	r.enablePSHeader = int(enablePSHeader)
	r.enableIID = int(enableIID)
	r.iidMode = int(iidMode)
	r.enableICC = int(enableICC)
	r.iccMode = int(iccMode)
	r.frameClass = int(frameClass)
	r.nEnvelopes = int(nEnvelopes)
	for i := 0; i < 4; i++ {
		r.frameBorder[i] = int32(frameBorder[i])
		r.deltaIID[i] = int32(deltaIID[i])
		r.deltaICC[i] = int32(deltaICC[i])
	}
	for i := 0; i < 80; i++ {
		r.iidFlat[i] = int32(iidFlat[i])
		r.iccFlat[i] = int32(iccFlat[i])
	}
	for i := 0; i < 20; i++ {
		r.iidLast[i] = int32(iidLast[i])
		r.iccLast[i] = int32(iccLast[i])
	}
	return r
}
