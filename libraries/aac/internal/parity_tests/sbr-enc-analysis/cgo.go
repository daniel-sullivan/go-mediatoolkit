// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrencanalysis pins the Go port of the Fraunhofer FDK-AAC SBR-ENCODER
// analysis tools — the transient detector (tran_det.cpp), envelope estimator
// (env_est.cpp), frame grid generator (fram_gen.cpp) and missing-harmonics
// detector (mh_det.cpp), all in internal/nativeaac/sbr — against the vendored C,
// compiled into this test binary via cgo. Each tool's decision/grid/threshold
// output is compared bit-for-bit against the genuine FDKsbrEnc_* symbol.
//
// This package compiles its OWN copy of the needed vendored C source (the four
// SBR-enc TUs plus their sbr_misc / fixpoint_math / FDK_tools_rom / genericStds
// libFDK siblings) and NEVER imports libraries/aac — importing it would link a
// second copy of the FDK reference and clash on static symbols (the
// amalgamation-split reason the sibling parity packages document). It MAY, and
// does, import the pure-Go internal/nativeaac/sbr (and the shared
// internal/nativeaac primitives the ports reuse).
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it.
//
// Integer parity: the SBR encoder is FIXED-POINT (FIXP_DBL == int32 Q-format,
// FIXP_SGL == int16 Q1.15 ROM). Every kernel here — the polyphase MACs, the
// fLog2 / sqrtFixp / fDivNorm fixed-point math, the saturating shifts — is
// bit-identical regardless of -ffp-contract / vectorization, with no
// transcendental. So this slice asserts EXACT int equality unconditionally — no
// aac_strict gate is needed.
package sbrencanalysis

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/sbr-enc-analysis).
// Mirrors the sibling sbr-qmf oracle, plus the libSBRenc src/include roots.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags come from the
// mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here. They are
// irrelevant to these integer kernels in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo LDFLAGS: -lm

#include <stdint.h>

// --- tran_det.cpp bridge ---
extern void tparity_init_fast_tran(int timeSlotsPerFrame, int bandwidthQmfSlot,
                                   int noQmfChannels, int sbrQmf1stBand,
                                   int32_t *dBfMOut, int *dBfEOut, int *startBand,
                                   int *stopBand);
extern void tparity_fast_tran(const int32_t *energyFlat, int rows, int noQmfChannels,
                              const int *scaleEnergies, int yBufferWriteOffset,
                              int timeSlotsPerFrame, int bandwidthQmfSlot,
                              int sbrQmf1stBand, unsigned char *tranVector);
extern void tparity_init_tran(int lowDelay, int frameSize, int sampleFreq,
                              int standardBitrate, int nChannels, int codecBitrate,
                              int tran_thr, int tran_det_mode, int tran_fc, int no_cols,
                              int no_rows, int frameShift, int tran_off,
                              int32_t *tranThrOut, int32_t *splitThrMOut, int *splitThrEOut);
extern void tparity_tran(const int32_t *energyFlat, int rows, int rowStride,
                         const int *scaleEnergies, int lowDelay, int frameSize,
                         int sampleFreq, int standardBitrate, int nChannels,
                         int codecBitrate, int tran_thr, int tran_det_mode, int tran_fc,
                         int no_cols, int no_rows, int frameShift, int tran_off,
                         int yBufferWriteOffset, int yBufferSzShift, int timeStep,
                         int frameMiddleBorder, unsigned char *transientInfo,
                         int32_t *thresholdsOut, int32_t *transientsOut);
extern void tparity_frame_splitter(const int32_t *energyFlat, int rows, int rowStride,
                                   const int *scaleEnergies, int lowDelay, int frameSize,
                                   int sampleFreq, int standardBitrate, int nChannels,
                                   int codecBitrate, int tran_thr, int tran_det_mode,
                                   int tran_fc, int no_cols, int no_rows, int frameShift,
                                   int tran_off, int32_t prevLowBandEnergy,
                                   const unsigned char *freqBandTable, unsigned char *tranVector,
                                   int yBufferWriteOffset, int yBufferSzShift, int nSfb,
                                   int timeStep, int32_t tonalityIn, int32_t *prevLowOut,
                                   int32_t *prevHighOut, int32_t *tonalityOut);
*/
import "C"

import "unsafe"

// cInitFastTran runs the genuine FDKsbrEnc_InitSbrFastTransientDetector and
// returns the dBf_m/dBf_e ROM + start/stop bands.
func cInitFastTran(timeSlotsPerFrame, bandwidthQmfSlot, noQmfChannels, sbrQmf1stBand int) (dBfM []int32, dBfE []int, startBand, stopBand int) {
	dBfM = make([]int32, 64)
	dBfE = make([]int, 64)
	cE := make([]C.int, 64)
	var sb, eb C.int
	C.tparity_init_fast_tran(C.int(timeSlotsPerFrame), C.int(bandwidthQmfSlot), C.int(noQmfChannels), C.int(sbrQmf1stBand),
		(*C.int32_t)(unsafe.Pointer(&dBfM[0])), &cE[0], &sb, &eb)
	for i := range cE {
		dBfE[i] = int(cE[i])
	}
	return dBfM, dBfE, int(sb), int(eb)
}

// cFastTran runs the genuine fast detector and returns tran_vector.
func cFastTran(energyFlat []int32, rows, noQmfChannels int, scaleEnergies []int, yBufferWriteOffset, timeSlotsPerFrame, bandwidthQmfSlot, sbrQmf1stBand int) []uint8 {
	sc := []C.int{C.int(scaleEnergies[0]), C.int(scaleEnergies[1])}
	tv := make([]C.uchar, 3)
	C.tparity_fast_tran((*C.int32_t)(unsafe.Pointer(&energyFlat[0])), C.int(rows), C.int(noQmfChannels),
		&sc[0], C.int(yBufferWriteOffset), C.int(timeSlotsPerFrame), C.int(bandwidthQmfSlot), C.int(sbrQmf1stBand),
		&tv[0])
	out := make([]uint8, 3)
	for i := range tv {
		out[i] = uint8(tv[i])
	}
	return out
}

// cInitTran runs the genuine standard-detector init and returns tran_thr,
// split_thr_m, split_thr_e.
func cInitTran(lowDelay int, frameSize, sampleFreq, standardBitrate, nChannels, codecBitrate, tranThr, tranDetMode, tranFc, noCols, noRows, frameShift, tranOff int) (int32, int32, int) {
	var tt, sm C.int32_t
	var se C.int
	C.tparity_init_tran(C.int(lowDelay), C.int(frameSize), C.int(sampleFreq), C.int(standardBitrate), C.int(nChannels),
		C.int(codecBitrate), C.int(tranThr), C.int(tranDetMode), C.int(tranFc), C.int(noCols), C.int(noRows),
		C.int(frameShift), C.int(tranOff), &tt, &sm, &se)
	return int32(tt), int32(sm), int(se)
}

// cTran runs the genuine standard detector, returning transient_info + the
// mutated thresholds[64] and transients ring.
func cTran(energyFlat []int32, rows, rowStride int, scaleEnergies []int, lowDelay, frameSize, sampleFreq, standardBitrate, nChannels, codecBitrate, tranThr, tranDetMode, tranFc, noCols, noRows, frameShift, tranOff, yBufferWriteOffset, yBufferSzShift, timeStep, frameMiddleBorder int) (transientInfo []uint8, thresholds, transients []int32) {
	sc := []C.int{C.int(scaleEnergies[0]), C.int(scaleEnergies[1])}
	ti := make([]C.uchar, 3)
	thresholds = make([]int32, 64)
	transients = make([]int32, 32+16)
	C.tparity_tran((*C.int32_t)(unsafe.Pointer(&energyFlat[0])), C.int(rows), C.int(rowStride), &sc[0],
		C.int(lowDelay), C.int(frameSize), C.int(sampleFreq), C.int(standardBitrate), C.int(nChannels),
		C.int(codecBitrate), C.int(tranThr), C.int(tranDetMode), C.int(tranFc), C.int(noCols), C.int(noRows),
		C.int(frameShift), C.int(tranOff), C.int(yBufferWriteOffset), C.int(yBufferSzShift), C.int(timeStep),
		C.int(frameMiddleBorder), &ti[0],
		(*C.int32_t)(unsafe.Pointer(&thresholds[0])), (*C.int32_t)(unsafe.Pointer(&transients[0])))
	transientInfo = make([]uint8, 3)
	for i := range ti {
		transientInfo[i] = uint8(ti[i])
	}
	return transientInfo, thresholds, transients
}

// cFrameSplitter runs the genuine FIXFIX splitter.
func cFrameSplitter(energyFlat []int32, rows, rowStride int, scaleEnergies []int, lowDelay, frameSize, sampleFreq, standardBitrate, nChannels, codecBitrate, tranThr, tranDetMode, tranFc, noCols, noRows, frameShift, tranOff int, prevLowBandEnergy int32, freqBandTable []uint8, tranVectorIn []uint8, yBufferWriteOffset, yBufferSzShift, nSfb, timeStep int, tonalityIn int32) (tranVector []uint8, prevLow, prevHigh, tonality int32) {
	sc := []C.int{C.int(scaleEnergies[0]), C.int(scaleEnergies[1])}
	tv := make([]C.uchar, 3)
	for i := range tranVectorIn {
		tv[i] = C.uchar(tranVectorIn[i])
	}
	fbt := make([]C.uchar, len(freqBandTable))
	for i := range freqBandTable {
		fbt[i] = C.uchar(freqBandTable[i])
	}
	var pl, ph, tn C.int32_t
	C.tparity_frame_splitter((*C.int32_t)(unsafe.Pointer(&energyFlat[0])), C.int(rows), C.int(rowStride), &sc[0],
		C.int(lowDelay), C.int(frameSize), C.int(sampleFreq), C.int(standardBitrate), C.int(nChannels),
		C.int(codecBitrate), C.int(tranThr), C.int(tranDetMode), C.int(tranFc), C.int(noCols), C.int(noRows),
		C.int(frameShift), C.int(tranOff), C.int32_t(prevLowBandEnergy), &fbt[0], &tv[0],
		C.int(yBufferWriteOffset), C.int(yBufferSzShift), C.int(nSfb), C.int(timeStep), C.int32_t(tonalityIn),
		&pl, &ph, &tn)
	tranVector = make([]uint8, 3)
	for i := range tv {
		tranVector[i] = uint8(tv[i])
	}
	return tranVector, int32(pl), int32(ph), int32(tn)
}
