// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encqcmainloop pins the Go port of the AAC-LC encoder rate-control
// DRIVER tier (libAACenc/src/qc_main.cpp: FDKaacEnc_QCNew / QCInit / QCOutNew /
// QCOutInit / QCMainPrepare / QCMain and the static prepareBitDistribution /
// reduceBitConsumption / crashRecovery convergence helpers) against the genuine
// vendored fdk reference, compiled into this test binary via cgo.
//
// FDKaacEnc_QCMain is the crux of the encoder: it turns psychoacoustic
// thresholds into a quantized spectrum + scalefactors, iterating the
// quantize / count-bits / adjust-thresholds loop until the access unit fits the
// CBR frame budget. The oracle drives the GENUINE QCMain end-to-end over the
// genuine struct graph (real_vendored, not a hand-twin) and the test asserts the
// Go port's quantized spectrum, scalefactors, global gain and full bit
// accounting are EXACT-integer-equal to it.
//
// This package compiles its OWN copy of the needed vendored C TUs (qc_main +
// sf_estim + adj_thr + quantize + dyn_bits + bit_cnt + bitenc + channel_map +
// line_pe + band_nrg + ms_stereo + intensity + aacEnc_ram + aacEnc_rom for the
// encoder, fixpoint_math + FDK_bitbuffer + FDK_tools_rom for libFDK, genericStds
// for libSYS) and NEVER imports libraries/aac (which would link a second copy of
// the FDK reference and clash on the encoder's file-local static symbols + RAM
// pools). It MAY, and does, import the pure-Go internal/nativeaac.
//
// fdk-aac encode is FIXED-POINT (int32 Q-format), so this asserts EXACT int32
// equality regardless of -ffp-contract / vectorization. The whole AAC island is
// fenced behind the opt-in aacfdk build tag; the cgo oracle additionally
// requires cgo. See libfdk/COPYING for the Fraunhofer FDK-AAC license.
package encqcmainloop

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

struct qcm_in;
struct qcm_out;
extern int qcmain_e2e(const struct qcm_in *in, struct qcm_out *out);

// Mirror of bridge.cpp's qcm_in / qcm_out so Go can allocate them.
struct qcm_in_go {
  int nChannels, bitrate, sampleRate, maxBits, minBits, bitRes, averageBits;
  int staticBits, meanPe, maxIterations, invQuant, maxBitFac, avgTotalBits;
  int sfbCnt, sfbPerGroup, maxSfbPerGroup, lastWindowSequence;
  int sfbOffsets[1025];
  int mdctSpectrum[2][1024];
  int sfbThresholdLdData[2][120];
  int sfbEnergyLdData[2][120];
  int sfbEnergy[2][120];
  int sfbMinSnrLdData[2][120];
  int sfbSpreadEnergy[2][120];
  int noiseNrg[2][120];
  int isBook[2][120];
  int isScale[2][120];
};

struct qcm_out_go {
  int errCode;
  int16_t quantSpec[2][1024];
  int scf[2][120];
  int globalGain[2];
  unsigned int maxValueInSfb[2][120];
  int staticBitsUsed, dynBitsUsed, grantedDynBits, grantedPe, grantedPeCorr;
  int usedDynBits, auGrantedDynBits, maxDynBits, totalGrantedPeCorr;
  int noOfSections0, huffmanBits0, sideInfoBits0, scalefacBits0;
};
*/
import "C"

import "unsafe"

// MaxGroupedSfb mirrors MAX_GROUPED_SFB (the per-channel grouped-sfb array
// bound); the cgo structs size their per-sfb arrays to it.
const MaxGroupedSfb = 120

// qcMainIn is the Go mirror of bridge.cpp's qcm_in.
type qcMainIn struct {
	nChannels, bitrate, sampleRate, maxBits, minBits, bitRes, averageBits int
	staticBits, meanPe, maxIterations, invQuant, maxBitFac, avgTotalBits  int
	sfbCnt, sfbPerGroup, maxSfbPerGroup, lastWindowSequence               int
	sfbOffsets                                                            []int
	mdctSpectrum                                                          [2][]int32
	sfbThresholdLdData, sfbEnergyLdData, sfbEnergy                        [2][]int32
	sfbMinSnrLdData, sfbSpreadEnergy                                      [2][]int32
	noiseNrg, isBook, isScale                                             [2][]int
}

// qcMainOut is the Go mirror of bridge.cpp's qcm_out.
type qcMainOut struct {
	errCode                                                               int
	quantSpec                                                             [2][1024]int16
	scf                                                                   [2][MaxGroupedSfb]int
	globalGain                                                            [2]int
	maxValueInSfb                                                         [2][MaxGroupedSfb]uint
	staticBitsUsed, dynBitsUsed, grantedDynBits, grantedPe, grantedPeCorr int
	usedDynBits, auGrantedDynBits, maxDynBits, totalGrantedPeCorr         int
	noOfSections0, huffmanBits0, sideInfoBits0, scalefacBits0             int
}

func setSfb(dst *[2][120]C.int, src [2][]int32) {
	for ch := 0; ch < 2; ch++ {
		for s := 0; s < len(src[ch]) && s < 120; s++ {
			dst[ch][s] = C.int(src[ch][s])
		}
	}
}

func setSfbInt(dst *[2][120]C.int, src [2][]int) {
	for ch := 0; ch < 2; ch++ {
		for s := 0; s < len(src[ch]) && s < 120; s++ {
			dst[ch][s] = C.int(src[ch][s])
		}
	}
}

// cQCMain marshals the input into the C struct, runs the genuine QCMain oracle
// and unmarshals the result.
func cQCMain(in *qcMainIn) qcMainOut {
	var ci C.struct_qcm_in_go
	var co C.struct_qcm_out_go

	ci.nChannels = C.int(in.nChannels)
	ci.bitrate = C.int(in.bitrate)
	ci.sampleRate = C.int(in.sampleRate)
	ci.maxBits = C.int(in.maxBits)
	ci.minBits = C.int(in.minBits)
	ci.bitRes = C.int(in.bitRes)
	ci.averageBits = C.int(in.averageBits)
	ci.staticBits = C.int(in.staticBits)
	ci.meanPe = C.int(in.meanPe)
	ci.maxIterations = C.int(in.maxIterations)
	ci.invQuant = C.int(in.invQuant)
	ci.maxBitFac = C.int(in.maxBitFac)
	ci.avgTotalBits = C.int(in.avgTotalBits)
	ci.sfbCnt = C.int(in.sfbCnt)
	ci.sfbPerGroup = C.int(in.sfbPerGroup)
	ci.maxSfbPerGroup = C.int(in.maxSfbPerGroup)
	ci.lastWindowSequence = C.int(in.lastWindowSequence)

	for i := 0; i < len(in.sfbOffsets) && i < 1025; i++ {
		ci.sfbOffsets[i] = C.int(in.sfbOffsets[i])
	}
	for ch := 0; ch < 2; ch++ {
		for i := 0; i < len(in.mdctSpectrum[ch]) && i < 1024; i++ {
			ci.mdctSpectrum[ch][i] = C.int(in.mdctSpectrum[ch][i])
		}
	}
	setSfb(&ci.sfbThresholdLdData, in.sfbThresholdLdData)
	setSfb(&ci.sfbEnergyLdData, in.sfbEnergyLdData)
	setSfb(&ci.sfbEnergy, in.sfbEnergy)
	setSfb(&ci.sfbMinSnrLdData, in.sfbMinSnrLdData)
	setSfb(&ci.sfbSpreadEnergy, in.sfbSpreadEnergy)
	setSfbInt(&ci.noiseNrg, in.noiseNrg)
	setSfbInt(&ci.isBook, in.isBook)
	setSfbInt(&ci.isScale, in.isScale)

	C.qcmain_e2e((*C.struct_qcm_in)(unsafe.Pointer(&ci)),
		(*C.struct_qcm_out)(unsafe.Pointer(&co)))

	var out qcMainOut
	out.errCode = int(co.errCode)
	for ch := 0; ch < 2; ch++ {
		for i := 0; i < 1024; i++ {
			out.quantSpec[ch][i] = int16(co.quantSpec[ch][i])
		}
		for s := 0; s < MaxGroupedSfb; s++ {
			out.scf[ch][s] = int(co.scf[ch][s])
			out.maxValueInSfb[ch][s] = uint(co.maxValueInSfb[ch][s])
		}
		out.globalGain[ch] = int(co.globalGain[ch])
	}
	out.staticBitsUsed = int(co.staticBitsUsed)
	out.dynBitsUsed = int(co.dynBitsUsed)
	out.grantedDynBits = int(co.grantedDynBits)
	out.grantedPe = int(co.grantedPe)
	out.grantedPeCorr = int(co.grantedPeCorr)
	out.usedDynBits = int(co.usedDynBits)
	out.auGrantedDynBits = int(co.auGrantedDynBits)
	out.maxDynBits = int(co.maxDynBits)
	out.totalGrantedPeCorr = int(co.totalGrantedPeCorr)
	out.noOfSections0 = int(co.noOfSections0)
	out.huffmanBits0 = int(co.huffmanBits0)
	out.sideInfoBits0 = int(co.sideInfoBits0)
	out.scalefacBits0 = int(co.scalefacBits0)
	return out
}
