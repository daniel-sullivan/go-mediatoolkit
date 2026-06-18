// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrenvcalc pins the Go port of the Fraunhofer FDK-AAC SBR
// envelope-gain calculation (env_calc.cpp: CalculateSbrEnvelope + ResetLimiterBands
// + the gain/noise/limiter/smoothing/adjustTimeSlot math in
// internal/nativeaac/sbr) against the vendored C, compiled into this test binary
// via cgo.
//
// This package compiles its OWN copy of the needed vendored C source (env_calc +
// env_extr/env_dec/huff_dec for the freq-table setup, sbr_rom, sbrdec_freq_sca,
// the libFDK fixpoint_math/FDK_tools_rom/scale + genericStds siblings) and NEVER
// imports libraries/aac — that would link a second copy of the FDK reference and
// clash on static symbols. It MAY, and does, import the pure-Go
// internal/nativeaac/sbr (and the shared internal/nativeaac primitives).
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the SBR decoder is a pure fixed-point subsystem (FIXP_DBL
// Q-format mantissa, FIXP_SGL Q1.15, SCHAR exponents). The gain/noise/limiter
// MACs, the table sqrtFixp_lookup, the mantissa/exponent divides, and the QMF
// rescaling are bit-identical regardless of -ffp-contract / vectorization, with
// no floating point at all. So this slice asserts EXACT integer equality
// unconditionally — no aac_strict gate is needed.
package sbrenvcalc

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

#define MAX_FREQ_COEFFS  56
#define MAX_NOISE_COEFFS 5
#define MAX_NUM_LIMITERS 12
#define ADD_HARMONICS_FLAGS_SIZE 2

typedef struct {
  int err;
  uint8_t numMaster;
  uint8_t vKMaster[MAX_FREQ_COEFFS + 1];
  uint8_t nSfb[2];
  uint8_t nNfb;
  uint8_t nInvfBands;
  uint8_t lowSubband;
  uint8_t highSubband;
  uint8_t freqBandLo[MAX_FREQ_COEFFS / 2 + 1];
  uint8_t freqBandHi[MAX_FREQ_COEFFS + 1];
  uint8_t freqBandNoise[MAX_NOISE_COEFFS + 1];
  uint8_t limiterBandTable[MAX_NUM_LIMITERS + 1];
  uint8_t noLimiterBands;
  int hbScale;
  int ovHbScale;
  int prevTranEnv;
  uint8_t harmIndex;
  int phaseIndex;
  uint32_t harmFlagsPrev[ADD_HARMONICS_FLAGS_SIZE];
  uint32_t harmFlagsPrevActive[ADD_HARMONICS_FLAGS_SIZE];
} envCalcOut;

extern int qparity_calculateSbrEnvelope(
    unsigned int sbrProcSmplRate, int startFreq, int stopFreq, int freqScale,
    int alterScale, int noiseBands, int xoverBand, int numberOfAnalysisBands,
    int ampResolution, int numberTimeSlots, int timeStep, int interpolFreq,
    int smoothingLength, int limiterBands, int limiterGains, unsigned int flags,
    int nEnvelopes, int tranEnv, int iTESactive, int interTempShapeMode0,
    const uint8_t *borders, const uint8_t *freqRes, int nNoiseEnvelopes,
    const uint8_t *bordersNoise,
    const int16_t *iEnvelope, int nIEnv, const int16_t *sbrNoiseFloorLevel,
    int nNoise, const uint32_t *addHarmonics,
    int hbScale, int ovHbScale, int ovLbScale, int lbScale, int useLP,
    int frameErrorFlag, int nSlots,
    int32_t *realFlat, int32_t *imagFlat, int32_t *degreeAlias,
    envCalcOut *out);
*/
import "C"

import "unsafe"

// envCalcConfig is the flat input fed to both sides.
type envCalcConfig struct {
	sbrProcSmplRate                                           uint
	startFreq, stopFreq, freqScale, alterScale                int
	noiseBands, xoverBand, numberOfAnalysisBands              int
	ampResolution, numberTimeSlots, timeStep                  int
	interpolFreq, smoothingLength, limiterBands, limiterGains int
	flags                                                     uint

	nEnvelopes, tranEnv, iTESactive, interTempShapeMode0 int
	borders, freqRes, bordersNoise                       []uint8
	nNoiseEnvelopes                                      int
	iEnvelope, sbrNoiseFloorLevel                        []int16
	addHarmonics                                         [2]uint32

	hbScale, ovHbScale, ovLbScale, lbScale int
	useLP, frameErrorFlag                  int

	nSlots int
}

// envCalcResult mirrors the C envCalcOut + the mutated QMF buffers.
type envCalcResult struct {
	err                 int
	numMaster           uint8
	vKMaster            []uint8
	nSfb                [2]uint8
	nNfb                uint8
	nInvfBands          uint8
	lowSubband          uint8
	highSubband         uint8
	freqBandLo          []uint8
	freqBandHi          []uint8
	freqBandNoise       []uint8
	limiterBandTab      []uint8
	noLimiterBands      uint8
	hbScale             int
	ovHbScale           int
	prevTranEnv         int
	harmIndex           uint8
	phaseIndex          int
	harmFlagsPrev       []uint32
	harmFlagsPrevActive []uint32

	realFlat []int32
	imagFlat []int32
}

// cCalculateSbrEnvelope runs the genuine setup + calculateSbrEnvelope over a copy
// of the QMF buffers and returns the resolved fixtures + mutated buffers.
func cCalculateSbrEnvelope(cfg envCalcConfig, realFlat, imagFlat, degreeAlias []int32) envCalcResult {
	re := append([]int32(nil), realFlat...)
	im := append([]int32(nil), imagFlat...)
	dg := append([]int32(nil), degreeAlias...)

	var out C.envCalcOut

	C.qparity_calculateSbrEnvelope(
		C.uint(cfg.sbrProcSmplRate), C.int(cfg.startFreq), C.int(cfg.stopFreq),
		C.int(cfg.freqScale), C.int(cfg.alterScale), C.int(cfg.noiseBands),
		C.int(cfg.xoverBand), C.int(cfg.numberOfAnalysisBands),
		C.int(cfg.ampResolution), C.int(cfg.numberTimeSlots), C.int(cfg.timeStep),
		C.int(cfg.interpolFreq), C.int(cfg.smoothingLength), C.int(cfg.limiterBands),
		C.int(cfg.limiterGains), C.uint(cfg.flags),
		C.int(cfg.nEnvelopes), C.int(cfg.tranEnv), C.int(cfg.iTESactive),
		C.int(cfg.interTempShapeMode0),
		(*C.uint8_t)(unsafe.Pointer(&cfg.borders[0])),
		(*C.uint8_t)(unsafe.Pointer(&cfg.freqRes[0])),
		C.int(cfg.nNoiseEnvelopes),
		(*C.uint8_t)(unsafe.Pointer(&cfg.bordersNoise[0])),
		(*C.int16_t)(unsafe.Pointer(&cfg.iEnvelope[0])), C.int(len(cfg.iEnvelope)),
		(*C.int16_t)(unsafe.Pointer(&cfg.sbrNoiseFloorLevel[0])), C.int(len(cfg.sbrNoiseFloorLevel)),
		(*C.uint32_t)(unsafe.Pointer(&cfg.addHarmonics[0])),
		C.int(cfg.hbScale), C.int(cfg.ovHbScale), C.int(cfg.ovLbScale), C.int(cfg.lbScale),
		C.int(cfg.useLP), C.int(cfg.frameErrorFlag), C.int(cfg.nSlots),
		(*C.int32_t)(unsafe.Pointer(&re[0])),
		(*C.int32_t)(unsafe.Pointer(&im[0])),
		(*C.int32_t)(unsafe.Pointer(&dg[0])),
		&out)

	r := envCalcResult{
		err:            int(out.err),
		numMaster:      uint8(out.numMaster),
		nNfb:           uint8(out.nNfb),
		nInvfBands:     uint8(out.nInvfBands),
		lowSubband:     uint8(out.lowSubband),
		highSubband:    uint8(out.highSubband),
		noLimiterBands: uint8(out.noLimiterBands),
		hbScale:        int(out.hbScale),
		ovHbScale:      int(out.ovHbScale),
		prevTranEnv:    int(out.prevTranEnv),
		harmIndex:      uint8(out.harmIndex),
		phaseIndex:     int(out.phaseIndex),
		realFlat:       re,
		imagFlat:       im,
	}
	r.nSfb[0] = uint8(out.nSfb[0])
	r.nSfb[1] = uint8(out.nSfb[1])
	r.vKMaster = make([]uint8, 57)
	for i := range r.vKMaster {
		r.vKMaster[i] = uint8(out.vKMaster[i])
	}
	r.freqBandLo = make([]uint8, 29)
	for i := range r.freqBandLo {
		r.freqBandLo[i] = uint8(out.freqBandLo[i])
	}
	r.freqBandHi = make([]uint8, 57)
	for i := range r.freqBandHi {
		r.freqBandHi[i] = uint8(out.freqBandHi[i])
	}
	r.freqBandNoise = make([]uint8, 6)
	for i := range r.freqBandNoise {
		r.freqBandNoise[i] = uint8(out.freqBandNoise[i])
	}
	r.limiterBandTab = make([]uint8, 13)
	for i := range r.limiterBandTab {
		r.limiterBandTab[i] = uint8(out.limiterBandTable[i])
	}
	r.harmFlagsPrev = []uint32{uint32(out.harmFlagsPrev[0]), uint32(out.harmFlagsPrev[1])}
	r.harmFlagsPrevActive = []uint32{uint32(out.harmFlagsPrevActive[0]), uint32(out.harmFlagsPrevActive[1])}
	return r
}
