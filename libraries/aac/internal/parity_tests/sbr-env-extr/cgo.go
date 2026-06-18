// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrenvextr pins the Go port of the Fraunhofer FDK-AAC SBR decode
// bitstream-extraction batch "sbr-env-extr" — env_extr.cpp's sbrGetHeaderData /
// sbrGetChannelElement / extractFrameInfo / sbrGetEnvelope /
// sbrGetNoiseFloorData / sbrGetSyntheticCodedData / checkFrameInfo
// (internal/nativeaac/sbr) — against the vendored C, compiled into this test
// binary via cgo.
//
// This package compiles its OWN copy of the needed vendored C source (env_extr +
// huff_dec + sbr_rom + sbrdec_freq_sca + the libFDK fixpoint_math/scale +
// genericStds sibling TUs, plus a link-only ReadPsData stub) and NEVER imports
// libraries/aac — importing it would link a second copy of the FDK reference and
// clash on static symbols. It MAY, and does, import the pure-Go
// internal/nativeaac/sbr (and the shared internal/nativeaac primitives the SBR
// port reuses).
//
// The whole AAC island is fenced behind the opt-in aacfdk build tag, so a
// default `go build ./...` links none of it. The cgo oracle additionally
// requires cgo. See libfdk/COPYING for the Fraunhofer FDK-AAC license.
//
// Integer parity: the SBR bitstream extraction is a pure integer subsystem
// (UCHAR/SCHAR grid + FIXP_SGL Q1.15 envelope/noise scale-factor indices read
// from the bitstream — no transcendental, no FP). Both sides parse the SAME
// bytes (built with the genuine FDK bit writer) and the test asserts EXACT
// integer equality unconditionally — no aac_strict gate is needed.
package sbrenvextr

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

#define MAX_ENVELOPES        8
#define MAX_NOISE_ENVELOPES  2
#define MAX_INVF_BANDS       5
#define MAX_FREQ_COEFFS      56
#define ADD_HARMONICS_FLAGS_SIZE 2
#define MAX_NUM_ENVELOPE_VALUES (MAX_ENVELOPES * MAX_FREQ_COEFFS)
#define MAX_NOISE_COEFFS     5
#define MAX_NUM_NOISE_VALUES (MAX_NOISE_ENVELOPES * MAX_NOISE_COEFFS)

typedef struct {
  int nScaleFactors;
  uint8_t frameClass;
  uint8_t nEnvelopes;
  uint8_t borders[MAX_ENVELOPES + 1];
  uint8_t freqRes[MAX_ENVELOPES];
  int8_t  tranEnv;
  uint8_t nNoiseEnvelopes;
  uint8_t bordersNoise[MAX_NOISE_ENVELOPES + 1];
  uint8_t noisePosition;
  uint8_t varLength;
  uint8_t domainVec[MAX_ENVELOPES];
  uint8_t domainVecNoise[MAX_NOISE_ENVELOPES];
  int32_t sbrInvfMode[MAX_INVF_BANDS];
  int     coupling;
  int     ampResolutionCurrentFrame;
  uint32_t addHarmonics[ADD_HARMONICS_FLAGS_SIZE];
  int16_t iEnvelope[MAX_NUM_ENVELOPE_VALUES];
  int16_t sbrNoiseFloorLevel[MAX_NUM_NOISE_VALUES];
} frameDataOut;

extern int qparity_buildPayload(const uint32_t *value, const uint8_t *nBits, int nTok,
                                uint8_t *out, int bufBytes);
extern int qparity_parseChannelElement(
    unsigned int sbrProcSmplRate, int startFreq, int stopFreq, int freqScale,
    int alterScale, int noiseBands, int xoverBand, int numberOfAnalysisBands,
    int ampResolution, int numberTimeSlots, int timeStep, int nCh, int overlap,
    uint8_t *payload, int bufBytes, unsigned int validBits, unsigned int flags,
    frameDataOut *outLeft, frameDataOut *outRight);
extern int qparity_parseHeaderData(uint8_t *payload, int bufBytes,
                                   unsigned int validBits, int preSyncState,
                                   unsigned int flags, int fIsSbrData, int configMode,
                                   uint8_t *fields);
*/
import "C"

import "unsafe"

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

// frameDataC is the genuine parsed SBR_FRAME_DATA flat, mirroring the Go
// sbr.FrameDataResult fields.
type frameDataC struct {
	nScaleFactors          int
	frameClass             uint8
	nEnvelopes             uint8
	borders                []uint8
	freqRes                []uint8
	tranEnv                int8
	nNoiseEnvelopes        uint8
	bordersNoise           []uint8
	noisePosition          uint8
	varLength              uint8
	domainVec              []uint8
	domainVecNoise         []uint8
	sbrInvfMode            []int
	coupling               int
	ampResolutionCurrFrame int
	addHarmonics           []uint32
	iEnvelope              []int16
	sbrNoiseFloorLevel     []int16
}

func fromC(o *C.frameDataOut) frameDataC {
	cp8 := func(p *C.uint8_t, n int) []uint8 {
		out := make([]uint8, n)
		s := unsafe.Slice((*uint8)(unsafe.Pointer(p)), n)
		copy(out, s)
		return out
	}
	invf := make([]int, 5)
	for i := 0; i < 5; i++ {
		invf[i] = int(o.sbrInvfMode[i])
	}
	addH := make([]uint32, 2)
	for i := 0; i < 2; i++ {
		addH[i] = uint32(o.addHarmonics[i])
	}
	iEnv := make([]int16, 8*56)
	for i := range iEnv {
		iEnv[i] = int16(o.iEnvelope[i])
	}
	noise := make([]int16, 2*5)
	for i := range noise {
		noise[i] = int16(o.sbrNoiseFloorLevel[i])
	}
	return frameDataC{
		nScaleFactors:          int(o.nScaleFactors),
		frameClass:             uint8(o.frameClass),
		nEnvelopes:             uint8(o.nEnvelopes),
		borders:                cp8(&o.borders[0], 9),
		freqRes:                cp8(&o.freqRes[0], 8),
		tranEnv:                int8(o.tranEnv),
		nNoiseEnvelopes:        uint8(o.nNoiseEnvelopes),
		bordersNoise:           cp8(&o.bordersNoise[0], 3),
		noisePosition:          uint8(o.noisePosition),
		varLength:              uint8(o.varLength),
		domainVec:              cp8(&o.domainVec[0], 8),
		domainVecNoise:         cp8(&o.domainVecNoise[0], 2),
		sbrInvfMode:            invf,
		coupling:               int(o.coupling),
		ampResolutionCurrFrame: int(o.ampResolutionCurrentFrame),
		addHarmonics:           addH,
		iEnvelope:              iEnv,
		sbrNoiseFloorLevel:     noise,
	}
}

// cParseChannelElement parses payload[:bufBytes] (validBits valid bits) with the
// genuine sbrGetChannelElement and returns ok + the left (+right for CPE)
// frame-data flat.
func cParseChannelElement(sbrProcSmplRate uint, startFreq, stopFreq, freqScale, alterScale, noiseBands, xoverBand, numberOfAnalysisBands, ampResolution, numberTimeSlots, timeStep uint8, nCh, overlap int, payload []byte, validBits uint32, flags uint) (ok int, left, right frameDataC) {
	var outL, outR C.frameDataOut
	r := C.qparity_parseChannelElement(
		C.uint(sbrProcSmplRate), C.int(startFreq), C.int(stopFreq), C.int(freqScale),
		C.int(alterScale), C.int(noiseBands), C.int(xoverBand), C.int(numberOfAnalysisBands),
		C.int(ampResolution), C.int(numberTimeSlots), C.int(timeStep), C.int(nCh), C.int(overlap),
		(*C.uint8_t)(unsafe.Pointer(&payload[0])), C.int(len(payload)), C.uint(validBits), C.uint(flags),
		&outL, &outR)
	return int(r), fromC(&outL), fromC(&outR)
}

// headerFieldsC is the genuine sbrGetHeaderData status + the 11 bs_data/bs_info
// fields in the fixed order.
type headerFieldsC struct {
	status int
	fields [11]uint8
}

// cParseHeaderData parses payload[:bufBytes] with the genuine sbrGetHeaderData.
func cParseHeaderData(payload []byte, validBits uint32, preSyncState int, flags uint, fIsSbrData, configMode int) headerFieldsC {
	var fields [11]uint8
	st := C.qparity_parseHeaderData(
		(*C.uint8_t)(unsafe.Pointer(&payload[0])), C.int(len(payload)), C.uint(validBits),
		C.int(preSyncState), C.uint(flags), C.int(fIsSbrData), C.int(configMode),
		(*C.uint8_t)(unsafe.Pointer(&fields[0])))
	return headerFieldsC{status: int(st), fields: fields}
}
