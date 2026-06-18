// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encinit pins the Go port of the Fraunhofer FDK-AAC encoder
// init/config tier — FDKaacEnc_AacInitDefaultConfig / FDKaacEnc_Open /
// FDKaacEnc_Initialize (libAACenc/src/aacenc.cpp) and, transitively,
// FDKaacEnc_psyMainInit (psy_main.cpp), FDKaacEnc_QCInit / FDKaacEnc_QCOutInit
// (qc_main.cpp), FDKaacEnc_AdjThrInit (adj_thr.cpp), FDKaacEnc_InitChannelMapping
// (channel_map.cpp), FDKaacEnc_DetermineBandWidth (bandwidth.cpp),
// FDKaacEnc_InitPsyConfiguration (psy_configuration.cpp),
// FDKaacEnc_InitTnsConfiguration (aacenc_tns.cpp) and FDKaacEnc_InitPnsConfiguration
// (aacenc_pns.cpp) — against the genuine vendored C, compiled into this test
// binary via cgo.
//
// This package compiles its OWN copy of the needed vendored FDK-AAC encoder TUs
// (the enc_*_cgo.cpp / fdk_*_cgo.cpp / sys_*_cgo.cpp sibling files, each a
// single-TU #include so file-static helpers stay file-local) and NEVER imports
// libraries/aac — importing it would link a second copy of the FDK reference and
// clash on static symbols. It MAY, and does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag; the cgo oracle additionally requires cgo. See
// libfdk/COPYING for the Fraunhofer FDK-AAC license.
//
// Integer parity: libfdk-aac ENCODE is FIXED-POINT. The init/config tier is pure
// integer / fixed-point (Q-format) arithmetic — bit-reservoir sizing, bandwidth
// table lookup, SFB-table accumulation, fDivNorm/scaleValue scaling — so it
// asserts EXACT int32 equality. The oracle links the genuine FDKaacEnc_Open /
// FDKaacEnc_Initialize symbols (oracle_kind == real_vendored) through the
// einit_run shim in bridge.cpp — no hand-twin re-derivation. The only external
// the bridge stubs is transportEnc_GetStaticBits, pinned to the deterministic
// TT_MP4_RAW value 0 (matching the Go port's nil StaticBitsProvider).
package encinit

/*
// The scalar FP flags (-ffp-contract=off etc.) come from the mise task env
// (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"); irrelevant to these integer kernels
// but kept to mirror the oracle convention.
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

// einit_State mirrors the bridge struct. Kept in sync with bridge.cpp.
typedef struct {
  int cm_encMode, cm_nChannels, cm_nChannelsEff, cm_nElements;
  int cm_elType[8], cm_elInstanceTag[8], cm_elNChannelsInEl[8], cm_elChIndex[8];

  int aot, bitrateMode, bandwidth90dB;
  unsigned int maxAncBytesPerAU;
  int cfg_bandWidth;

  int qc_globHdrBits, qc_maxBitsPerFrame, qc_minBitsPerFrame, qc_nElements;
  int qc_bitrateMode, qc_bitResMode, qc_bitResTot, qc_bitResTotMax;
  int qc_maxIterations, qc_invQuant, qc_vbrQualFactor, qc_maxBitFac;
  int qc_paddingRest, qc_dZoneQuantEnable;
  int qc_eb_chBitrateEl[8], qc_eb_maxBitsEl[8], qc_eb_bitResLevelEl[8];
  int qc_eb_maxBitResBitsEl[8], qc_eb_relativeBitsEl[8];

  int at_bitDistributionMode, at_maxIter2ndGuess;
  int at_bpL[8], at_bpS[8];
  int at_peMin[8], at_peMax[8], at_peOffset[8];
  int at_bits2PeFactor_m[8], at_bits2PeFactor_e[8];
  int at_ah_modifyMinSnr[8], at_ah_startSfbL[8], at_ah_startSfbS[8];
  int at_msa_maxRed[8], at_msa_startRatio[8], at_msa_maxRatio[8];
  int at_msa_redRatioFac[8], at_msa_redOffs[8];
  int at_peLast[8], at_dynBitsLast[8], at_peCorr_m[8], at_peCorr_e[8];
  int at_vbrQualFactor[8], at_chaosMeasureOld[8];

  int pc_sfbCnt[2], pc_sfbActive[2], pc_sfbActiveLFE[2], pc_filterbank[2];
  int pc_maxAllowedIncreaseFactor[2], pc_minRemainingThresholdFactor[2];
  int pc_lowpassLine[2], pc_lowpassLineLFE[2], pc_clipEnergy[2];
  int pc_granuleLength[2], pc_allowIS[2], pc_allowMS[2];
  int pc_sfbOffset[2][52];
  int pc_sfbPcmQuantThreshold[2][51];
  int pc_sfbMaskLowFactor[2][51], pc_sfbMaskHighFactor[2][51];
  int pc_sfbMaskLowFactorSprEn[2][51], pc_sfbMaskHighFactorSprEn[2][51];
  int pc_sfbMinSnrLdData[2][51];
  int pc_pns_usePns[2], pc_pns_minCorrelationEnergy[2], pc_pns_noiseCorrelationThresh[2];
  int pc_tns_isLowDelay[2], pc_tns_tnsActive[2], pc_tns_maxOrder[2], pc_tns_coefRes[2];
  int pc_tns_lpcStartBand[2][2], pc_tns_lpcStartLine[2][2];
  int pc_tns_lpcStopBand[2], pc_tns_lpcStopLine[2];
} einit_State;

extern int einit_run(int channelMode, int nChannels, int sampleRate, int bitRate,
                     int audioObjectType, int nElements, int frameLength,
                     einit_State *out);

// psymain_State mirrors the bridge_psymain.cpp struct (PSY_OUT_CHANNEL dump
// from the genuine FDKaacEnc_psyMain). MAX_GROUPED_SFB==60, MAX_NO_OF_GROUPS==4,
// TRANS_FAC==8, MAX_NUM_OF_FILTERS==2 (the genuine macros, hard-coded here so the
// Go preamble does not need the FDK headers).
typedef struct {
  int errCode;
  int commonWindow;
  int msDigest;
  int msMask[60];
  int sfbCnt[2];
  int sfbPerGroup[2];
  int maxSfbPerGroup[2];
  int windowShape[2];
  int lastWindowSequence[2];
  int groupingMask[2];
  int mdctScale[2];
  int groupLen[2][4];
  int sfbOffsets[2][60 + 1];
  int noiseNrg[2][60];
  int isBook[2][60];
  int isScale[2][60];
  int sfbEnergy[2][60];
  int sfbSpreadEnergy[2][60];
  int sfbEnergyLdData[2][60];
  int sfbThresholdLdData[2][60];
  int sfbMinSnrLdData[2][60];
  int tnsNumOfFilters[2][8];
  int tnsCoefRes[2][8];
  int tnsOrder[2][8][2];
} psymain_State;

extern int epsymain_run(int channelMode, int nChannels, int sampleRate, int bitRate,
                        int audioObjectType, int nElements, int frameLength,
                        const short *input, int inputBufSize, psymain_State *out);
*/
import "C"

import "unsafe"

// cInitState is the Go mirror of einit_State holding the genuine-init dump.
type cInitState = C.einit_State

// cEinitRun drives the genuine FDKaacEnc_Open + FDKaacEnc_Initialize and returns
// the init error code plus the populated state.
func cEinitRun(channelMode, nChannels, sampleRate, bitRate, aot, nElements, frameLength int) (int, cInitState) {
	var st C.einit_State
	rc := C.einit_run(C.int(channelMode), C.int(nChannels), C.int(sampleRate),
		C.int(bitRate), C.int(aot), C.int(nElements), C.int(frameLength), &st)
	return int(rc), st
}

// cPsyMainState is the Go mirror of the genuine psyMain PSY_OUT dump.
type cPsyMainState = C.psymain_State

// cEpsyMainRun drives the genuine FDKaacEnc_Open + FDKaacEnc_Initialize +
// FDKaacEnc_psyMain over the planar int16 input and returns the error code plus
// the populated PSY_OUT dump.
func cEpsyMainRun(channelMode, nChannels, sampleRate, bitRate, aot, nElements, frameLength int,
	input []int16, inputBufSize int) (int, cPsyMainState) {
	var st C.psymain_State
	var p *C.short
	if len(input) > 0 {
		p = (*C.short)(unsafe.Pointer(&input[0]))
	}
	rc := C.epsymain_run(C.int(channelMode), C.int(nChannels), C.int(sampleRate),
		C.int(bitRate), C.int(aot), C.int(nElements), C.int(frameLength),
		p, C.int(inputBufSize), &st)
	return int(rc), st
}
