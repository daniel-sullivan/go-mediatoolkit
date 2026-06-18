// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrhfgen pins the Go port of the Fraunhofer FDK-AAC SBR high-frequency-
// generation tools — the HE-AAC v1 (LPP) transposer (lpp_tran.cpp), its 2nd-order
// autocorrelation (autocorr2nd.cpp) and the pre-flattening gain vector
// (HFgen_preFlat.cpp), in internal/nativeaac/sbr — against the vendored C,
// compiled into this test binary via cgo, asserting EXACT int32 equality.
//
// This package compiles its OWN copy of the needed vendored C source (lpp_tran /
// HFgen_preFlat / autocorr2nd + the sbr_rom / sbr_ram / fixpoint_math / scale /
// genericStds sibling TUs) and NEVER imports libraries/aac — importing it would
// link a second copy of the FDK reference and clash on static symbols (the
// amalgamation-split reason the sibling parity packages document). It MAY, and
// does, import the pure-Go internal/nativeaac/sbr (and the shared
// internal/nativeaac primitives the HF-gen reuses).
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the license.
//
// Integer parity: the HF-gen subsystem is pure INTEGER fixed-point (FIXP_DBL
// Q-format data, FIXP_SGL Q1.15 ROM) — the autocorrelation MACs, the LPC
// fDivNorm/scaleValueSaturate, the patch copy/whitening filter, and the
// polynomial-fit Cholesky are bit-identical regardless of -ffp-contract /
// vectorization, with no transcendental. So this slice asserts EXACT int32
// equality unconditionally — no aac_strict gate is needed.
//
// HBE (hbe.cpp / harmonic SBR) is USAC-only, out of HE-AAC v1 scope — not linked.
package sbrhfgen

/*
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

extern void qparity_whFactorsIndex(uint16_t *out, int count);
extern void qparity_whFactorsTable(int32_t *out, int rows);

extern int qparity_autoCorr2nd_real(const int32_t *buf, int base, int length,
                                    int32_t *r11r, int32_t *r22r, int32_t *r01r,
                                    int32_t *r12r, int32_t *r02r, int32_t *det,
                                    int *det_scale);
extern int qparity_autoCorr2nd_cplx(const int32_t *re, const int32_t *im, int base,
                                    int length, int32_t *r00r, int32_t *r11r,
                                    int32_t *r22r, int32_t *r01r, int32_t *r12r,
                                    int32_t *r01i, int32_t *r12i, int32_t *r02r,
                                    int32_t *r02i, int32_t *det, int *det_scale);

extern void qparity_calculateGainVec(const int32_t *realFlat, const int32_t *imagFlat,
                                     int nSlots, int sourceBuf_e_overlap,
                                     int sourceBuf_e_current, int overlap, int numBands,
                                     int startSample, int stopSample, int32_t *gain,
                                     int *gainExp);

extern int qparity_resetLppTransposer(int highBandStartSb, const uint8_t *vKMaster,
                                      int numMaster, int usb, int timeSlots, int nCols,
                                      const uint8_t *noiseBandTable, int noNoiseBands,
                                      unsigned int fs, int overlap, int *noOfPatches,
                                      int *lbStartPatching, int *lbStopPatching,
                                      uint8_t *srcStart, uint8_t *srcStop,
                                      uint8_t *tgtStart, uint8_t *tgtOffs,
                                      uint8_t *guardStart, uint8_t *numBandsArr,
                                      uint8_t *bwBorders, int32_t *whFactors);

extern void qparity_lppTransposer(int32_t *realFlat, int32_t *imagFlat, int nSlots,
                                  int highBandStartSb, const uint8_t *vKMaster,
                                  int numMaster, int usb, int timeSlots, int nCols,
                                  const uint8_t *noiseBandTable, int noNoiseBands,
                                  unsigned int fs, int overlap, int lbScale,
                                  int ovLbScale, int useLP, int fPreWhitening,
                                  int vKMaster0, int timeStep, int firstSlotOffs,
                                  int lastSlotOffs, int nInvfBands, const int *invfMod,
                                  const int *invfModPrev, int32_t *degreeAlias,
                                  int *hbScale);
*/
import "C"

import "unsafe"

func cWhFactorsIndex(count int) []uint16 {
	out := make([]uint16, count)
	C.qparity_whFactorsIndex((*C.uint16_t)(unsafe.Pointer(&out[0])), C.int(count))
	return out
}

func cWhFactorsTable(rows int) []int32 {
	out := make([]int32, rows*5)
	C.qparity_whFactorsTable((*C.int32_t)(unsafe.Pointer(&out[0])), C.int(rows))
	return out
}

func cAutoCorr2ndReal(buf []int32, base, length int) (r11r, r22r, r01r, r12r, r02r, det int32, detScale, scaling int) {
	var cr11r, cr22r, cr01r, cr12r, cr02r, cdet C.int32_t
	var cds C.int
	sc := C.qparity_autoCorr2nd_real((*C.int32_t)(unsafe.Pointer(&buf[0])), C.int(base), C.int(length),
		&cr11r, &cr22r, &cr01r, &cr12r, &cr02r, &cdet, &cds)
	return int32(cr11r), int32(cr22r), int32(cr01r), int32(cr12r), int32(cr02r), int32(cdet), int(cds), int(sc)
}

func cAutoCorr2ndCplx(re, im []int32, base, length int) (r00r, r11r, r22r, r01r, r12r, r01i, r12i, r02r, r02i, det int32, detScale, scaling int) {
	var c0, c11, c22, c01r, c12r, c01i, c12i, c02r, c02i, cdet C.int32_t
	var cds C.int
	sc := C.qparity_autoCorr2nd_cplx((*C.int32_t)(unsafe.Pointer(&re[0])), (*C.int32_t)(unsafe.Pointer(&im[0])),
		C.int(base), C.int(length), &c0, &c11, &c22, &c01r, &c12r, &c01i, &c12i, &c02r, &c02i, &cdet, &cds)
	return int32(c0), int32(c11), int32(c22), int32(c01r), int32(c12r), int32(c01i), int32(c12i), int32(c02r), int32(c02i), int32(cdet), int(cds), int(sc)
}

func cCalculateGainVec(realFlat, imagFlat []int32, nSlots, sourceBufEOverlap, sourceBufECurrent, overlap, numBands, startSample, stopSample int) (gain []int32, gainExp []int) {
	gain = make([]int32, numBands)
	gainExpC := make([]C.int, numBands)
	C.qparity_calculateGainVec((*C.int32_t)(unsafe.Pointer(&realFlat[0])), (*C.int32_t)(unsafe.Pointer(&imagFlat[0])),
		C.int(nSlots), C.int(sourceBufEOverlap), C.int(sourceBufECurrent), C.int(overlap), C.int(numBands),
		C.int(startSample), C.int(stopSample), (*C.int32_t)(unsafe.Pointer(&gain[0])), &gainExpC[0])
	gainExp = make([]int, numBands)
	for i := range gainExp {
		gainExp[i] = int(gainExpC[i])
	}
	return gain, gainExp
}

func cResetLppTransposer(highBandStartSb int, vKMaster []uint8, numMaster, usb, timeSlots, nCols int,
	noiseBandTable []uint8, noNoiseBands int, fs uint, overlap int) (rc, noOfPatches, lbStart, lbStop int,
	srcStart, srcStop, tgtStart, tgtOffs, guardStart, numBands, bwBorders []uint8, whFactors []int32) {

	var cnp, clbStart, clbStop C.int
	srcStart = make([]uint8, maxNumPatches+1)
	srcStop = make([]uint8, maxNumPatches+1)
	tgtStart = make([]uint8, maxNumPatches+1)
	tgtOffs = make([]uint8, maxNumPatches+1)
	guardStart = make([]uint8, maxNumPatches+1)
	numBands = make([]uint8, maxNumPatches+1)
	bwBorders = make([]uint8, maxNumNoiseValues)
	whFactors = make([]int32, 5)

	rcC := C.qparity_resetLppTransposer(C.int(highBandStartSb), (*C.uint8_t)(unsafe.Pointer(&vKMaster[0])),
		C.int(numMaster), C.int(usb), C.int(timeSlots), C.int(nCols),
		(*C.uint8_t)(unsafe.Pointer(&noiseBandTable[0])), C.int(noNoiseBands), C.uint(fs), C.int(overlap),
		&cnp, &clbStart, &clbStop,
		(*C.uint8_t)(unsafe.Pointer(&srcStart[0])), (*C.uint8_t)(unsafe.Pointer(&srcStop[0])),
		(*C.uint8_t)(unsafe.Pointer(&tgtStart[0])), (*C.uint8_t)(unsafe.Pointer(&tgtOffs[0])),
		(*C.uint8_t)(unsafe.Pointer(&guardStart[0])), (*C.uint8_t)(unsafe.Pointer(&numBands[0])),
		(*C.uint8_t)(unsafe.Pointer(&bwBorders[0])), (*C.int32_t)(unsafe.Pointer(&whFactors[0])))

	return int(rcC), int(cnp), int(clbStart), int(clbStop),
		srcStart, srcStop, tgtStart, tgtOffs, guardStart, numBands, bwBorders, whFactors
}

func cLppTransposer(realFlat, imagFlat []int32, nSlots, highBandStartSb int, vKMaster []uint8,
	numMaster, usb, timeSlots, nCols int, noiseBandTable []uint8, noNoiseBands int, fs uint, overlap,
	lbScale, ovLbScale int, useLP, fPreWhitening bool, vKMaster0, timeStep, firstSlotOffs, lastSlotOffs, nInvfBands int,
	invfMod, invfModPrev []int) (degreeAlias []int32, hbScale int) {

	useLPi := 0
	if useLP {
		useLPi = 1
	}
	fpw := 0
	if fPreWhitening {
		fpw = 1
	}
	imC := make([]C.int, nInvfBands)
	imPrevC := make([]C.int, nInvfBands)
	for i := 0; i < nInvfBands; i++ {
		imC[i] = C.int(invfMod[i])
		imPrevC[i] = C.int(invfModPrev[i])
	}
	degreeAlias = make([]int32, 64)
	var chb C.int

	C.qparity_lppTransposer((*C.int32_t)(unsafe.Pointer(&realFlat[0])), (*C.int32_t)(unsafe.Pointer(&imagFlat[0])),
		C.int(nSlots), C.int(highBandStartSb), (*C.uint8_t)(unsafe.Pointer(&vKMaster[0])), C.int(numMaster),
		C.int(usb), C.int(timeSlots), C.int(nCols), (*C.uint8_t)(unsafe.Pointer(&noiseBandTable[0])),
		C.int(noNoiseBands), C.uint(fs), C.int(overlap), C.int(lbScale), C.int(ovLbScale),
		C.int(useLPi), C.int(fpw), C.int(vKMaster0), C.int(timeStep), C.int(firstSlotOffs), C.int(lastSlotOffs),
		C.int(nInvfBands), &imC[0], &imPrevC[0], (*C.int32_t)(unsafe.Pointer(&degreeAlias[0])), &chb)

	return degreeAlias, int(chb)
}

// maxNumPatches / maxNumNoiseValues mirror the C MAX_NUM_PATCHES /
// MAX_NUM_NOISE_VALUES (lpp_tran.h) for sizing the patch-layout return slices.
const (
	maxNumPatches     = 6
	maxNumNoiseValues = 10
)
