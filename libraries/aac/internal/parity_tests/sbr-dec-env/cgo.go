// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrdecenv pins the Go port of the Fraunhofer FDK-AAC SBR decode
// "envelope" batch foundation — the constant ROM tables (sbr_rom.cpp) and the
// master/hi/lo/noise frequency band-table builder (sbrdec_freq_sca.cpp) the
// HE-AAC v1 decoder derives from the parsed SBR header (internal/nativeaac/sbr)
// — against the vendored C, compiled into this test binary via cgo.
//
// This package compiles its OWN copy of the needed vendored C source (sbr_rom +
// sbrdec_freq_sca + the libFDK fixpoint_math/scale + genericStds sibling TUs)
// and NEVER imports libraries/aac — importing it would link a second copy of the
// FDK reference and clash on static symbols (the amalgamation-split reason the
// sibling parity packages document). It MAY, and does, import the pure-Go
// internal/nativeaac/sbr (and the shared internal/nativeaac primitives the SBR
// ROM/band-factor math reuses).
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the SBR decoder is a pure fixed-point subsystem (FIXP_DBL
// Q-format data, FIXP_SGL Q1.15 ROM). The FL2FXCONST narrowing, the band-factor
// fMult/fMultDiv2 MACs, the CalcLdInt log lookups and the UCHAR band-table
// arithmetic are bit-identical regardless of -ffp-contract / vectorization, with
// no transcendental. So this slice asserts EXACT integer equality
// unconditionally — no aac_strict gate is needed (every SBR kernel is fixed-point).
package sbrdecenv

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/sbr-dec-env).
// Mirrors the sibling sbr-qmf oracle, plus the SBRdec src roots the freq-scale /
// rom sources need.
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
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/src
#cgo LDFLAGS: -lm

#include <stdint.h>

extern void qparity_limGains(int16_t *mOut, uint8_t *eOut, int count);
extern void qparity_smoothFilter(int16_t *out, int count);
extern void qparity_limiterBandsPerOctaveDiv4(int16_t *sglOut, int32_t *dblOut, int count);
extern void qparity_randomPhase(int16_t *out, int pairCount);
extern void qparity_invTable(int16_t *out, int count);
extern int  qparity_resetFreqBandTables(
    unsigned int sbrProcSmplRate, int startFreq, int stopFreq, int freqScale,
    int alterScale, int noiseBands, int xoverBand, int numberOfAnalysisBands,
    unsigned int flags, unsigned char *numMaster, unsigned char *vKMaster,
    unsigned char *nSfb, unsigned char *nNfb, unsigned char *nInvfBands,
    unsigned char *lowSubband, unsigned char *highSubband,
    unsigned char *freqBandLo, unsigned char *freqBandHi,
    unsigned char *freqBandNoise);

#define MAX_ENVELOPES        8
#define MAX_NOISE_ENVELOPES  2
#define MAX_FREQ_COEFFS      56
#define MAX_NOISE_COEFFS     5
#define MAX_NUM_ENVELOPE_VALUES (MAX_ENVELOPES * MAX_FREQ_COEFFS)
#define MAX_NUM_NOISE_VALUES (MAX_NOISE_ENVELOPES * MAX_NOISE_COEFFS)

typedef struct {
  int nScaleFactors;
  uint8_t coupling;
  int16_t iEnvelope[MAX_NUM_ENVELOPE_VALUES];
  int16_t sbrNoiseFloorLevel[MAX_NUM_NOISE_VALUES];
  int16_t sfbNrgPrev[MAX_FREQ_COEFFS];
  int16_t prevNoiseLevel[MAX_NOISE_COEFFS];
  uint8_t frameError;
} decodeOut;

extern int qparity_buildPayload(const uint32_t *value, const uint8_t *nBits,
                                int nTok, uint8_t *out, int bufBytes);
extern int qparity_decodeChannelElement(
    unsigned int sbrProcSmplRate, int startFreq, int stopFreq, int freqScale,
    int alterScale, int noiseBands, int xoverBand, int numberOfAnalysisBands,
    int ampResolution, int numberTimeSlots, int timeStep, int nCh, int overlap,
    uint8_t *payload, int bufBytes, unsigned int validBits, unsigned int flags,
    decodeOut *outLeft, decodeOut *outRight);
*/
import "C"

import "unsafe"

// cLimGains returns the genuine in-RAM FDK_sbrDecoder_sbr_limGains_m / _e.
func cLimGains(count int) (m []int16, e []uint8) {
	m = make([]int16, count)
	e = make([]uint8, count)
	C.qparity_limGains((*C.int16_t)(unsafe.Pointer(&m[0])), (*C.uint8_t)(unsafe.Pointer(&e[0])), C.int(count))
	return m, e
}

// cSmoothFilter returns the genuine FDK_sbrDecoder_sbr_smoothFilter.
func cSmoothFilter(count int) []int16 {
	out := make([]int16, count)
	C.qparity_smoothFilter((*C.int16_t)(unsafe.Pointer(&out[0])), C.int(count))
	return out
}

// cLimiterBandsPerOctaveDiv4 returns the genuine FIXP_SGL + FIXP_DBL limiter ROM.
func cLimiterBandsPerOctaveDiv4(count int) (sgl []int16, dbl []int32) {
	sgl = make([]int16, count)
	dbl = make([]int32, count)
	C.qparity_limiterBandsPerOctaveDiv4((*C.int16_t)(unsafe.Pointer(&sgl[0])), (*C.int32_t)(unsafe.Pointer(&dbl[0])), C.int(count))
	return sgl, dbl
}

// cRandomPhase returns the genuine FDK_sbrDecoder_sbr_randomPhase flat re/im.
func cRandomPhase(pairCount int) []int16 {
	out := make([]int16, 2*pairCount)
	C.qparity_randomPhase((*C.int16_t)(unsafe.Pointer(&out[0])), C.int(pairCount))
	return out
}

// cInvTable returns the genuine FDK_sbrDecoder_invTable.
func cInvTable(count int) []int16 {
	out := make([]int16, count)
	C.qparity_invTable((*C.int16_t)(unsafe.Pointer(&out[0])), C.int(count))
	return out
}

// freqScaleC is the flat result the genuine resetFreqBandTables produces.
type freqScaleC struct {
	err           int
	numMaster     uint8
	vKMaster      []uint8
	nSfb          [2]uint8
	nNfb          uint8
	nInvfBands    uint8
	lowSubband    uint8
	highSubband   uint8
	freqBandLo    []uint8
	freqBandHi    []uint8
	freqBandNoise []uint8
}

// cResetFreqBandTables runs the genuine resetFreqBandTables over the flat header
// fields and returns the full band-table result.
func cResetFreqBandTables(sbrProcSmplRate uint, startFreq, stopFreq, freqScale, alterScale, noiseBands, xoverBand, numberOfAnalysisBands uint8, flags uint) freqScaleC {
	const maxFreqCoeffs = 56
	const maxNoiseCoeffs = 5
	var r freqScaleC
	r.vKMaster = make([]uint8, maxFreqCoeffs+1)
	r.freqBandLo = make([]uint8, maxFreqCoeffs/2+1)
	r.freqBandHi = make([]uint8, maxFreqCoeffs+1)
	r.freqBandNoise = make([]uint8, maxNoiseCoeffs+1)
	var nSfb [2]uint8

	r.err = int(C.qparity_resetFreqBandTables(
		C.uint(sbrProcSmplRate), C.int(startFreq), C.int(stopFreq), C.int(freqScale),
		C.int(alterScale), C.int(noiseBands), C.int(xoverBand), C.int(numberOfAnalysisBands),
		C.uint(flags),
		(*C.uchar)(unsafe.Pointer(&r.numMaster)),
		(*C.uchar)(unsafe.Pointer(&r.vKMaster[0])),
		(*C.uchar)(unsafe.Pointer(&nSfb[0])),
		(*C.uchar)(unsafe.Pointer(&r.nNfb)),
		(*C.uchar)(unsafe.Pointer(&r.nInvfBands)),
		(*C.uchar)(unsafe.Pointer(&r.lowSubband)),
		(*C.uchar)(unsafe.Pointer(&r.highSubband)),
		(*C.uchar)(unsafe.Pointer(&r.freqBandLo[0])),
		(*C.uchar)(unsafe.Pointer(&r.freqBandHi[0])),
		(*C.uchar)(unsafe.Pointer(&r.freqBandNoise[0]))))
	r.nSfb = nSfb
	return r
}

// cBuildPayload writes the (value,nBits) tokens into a fresh power-of-two-sized
// scratch buffer via the genuine FDK bit writer, returning the buffer (full
// bufBytes length, zero-padded) and the byte count actually written.
func cBuildPayload(values []uint32, nBits []uint8, bufBytes int) (buf []byte, nBytes int) {
	buf = make([]byte, bufBytes)
	nTok := len(values)
	var vp *C.uint32_t
	var np *C.uint8_t
	if nTok > 0 {
		vp = (*C.uint32_t)(unsafe.Pointer(&values[0]))
		np = (*C.uint8_t)(unsafe.Pointer(&nBits[0]))
	}
	n := C.qparity_buildPayload(vp, np, C.int(nTok),
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(bufBytes))
	return buf, int(n)
}

// decodeC is the genuine dequantized SBR_FRAME_DATA flat decodeSbrData produces,
// mirroring the Go sbr.DecodeResult fields.
type decodeC struct {
	nScaleFactors      int
	coupling           uint8
	iEnvelope          []int16
	sbrNoiseFloorLevel []int16
	sfbNrgPrev         []int16
	prevNoiseLevel     []int16
	frameError         uint8
}

func fromDecodeC(o *C.decodeOut) decodeC {
	iEnv := make([]int16, 8*56)
	for i := range iEnv {
		iEnv[i] = int16(o.iEnvelope[i])
	}
	noise := make([]int16, 2*5)
	for i := range noise {
		noise[i] = int16(o.sbrNoiseFloorLevel[i])
	}
	sfb := make([]int16, 56)
	for i := range sfb {
		sfb[i] = int16(o.sfbNrgPrev[i])
	}
	pn := make([]int16, 5)
	for i := range pn {
		pn[i] = int16(o.prevNoiseLevel[i])
	}
	return decodeC{
		nScaleFactors:      int(o.nScaleFactors),
		coupling:           uint8(o.coupling),
		iEnvelope:          iEnv,
		sbrNoiseFloorLevel: noise,
		sfbNrgPrev:         sfb,
		prevNoiseLevel:     pn,
		frameError:         uint8(o.frameError),
	}
}

// cDecodeChannelElement parses payload[:bufBytes] (validBits valid bits) with the
// genuine sbrGetChannelElement, then runs the genuine decodeSbrData, returning ok
// + the dequantized left (+right for CPE) frame data.
func cDecodeChannelElement(sbrProcSmplRate uint, startFreq, stopFreq, freqScale, alterScale, noiseBands, xoverBand, numberOfAnalysisBands, ampResolution, numberTimeSlots, timeStep uint8, nCh, overlap int, payload []byte, validBits uint32, flags uint) (ok int, left, right decodeC) {
	var outL, outR C.decodeOut
	r := C.qparity_decodeChannelElement(
		C.uint(sbrProcSmplRate), C.int(startFreq), C.int(stopFreq), C.int(freqScale),
		C.int(alterScale), C.int(noiseBands), C.int(xoverBand), C.int(numberOfAnalysisBands),
		C.int(ampResolution), C.int(numberTimeSlots), C.int(timeStep), C.int(nCh), C.int(overlap),
		(*C.uint8_t)(unsafe.Pointer(&payload[0])), C.int(len(payload)), C.uint(validBits), C.uint(flags),
		&outL, &outR)
	return int(r), fromDecodeC(&outL), fromDecodeC(&outR)
}
