// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package enc_pns pins the Go port of the Fraunhofer FDK-AAC encoder Perceptual
// Noise Substitution (PNS) detect/code chain (nativeaac.GetPnsParam /
// InitPnsConfiguration / PnsDetect / CodePnsChannel / PreProcessPnsChannelPair /
// PostProcessPnsChannelPair) against the genuine vendored libAACenc/src/
// aacenc_pns.cpp + noisedet.cpp + pnsparam.cpp kernels, compiled into this test
// binary via cgo. For a range of (bitrate, samplerate, sfb layout) configs and
// fabricated spectra/energies/tonalities, the C kernels fill PNS_CONFIG / pnsFlag
// / noiseNrg / noiseEnergyCorrelation / msMask and the bridge copies every field
// out flat; the Go port is compared bit-for-bit (raw int32 / int16) against it.
//
// This package compiles its OWN copy of the needed vendored C++ sources
// (aacenc_pns.cpp + noisedet.cpp + pnsparam.cpp + aacenc_tns.cpp for
// FreqToBandWidthRounding + FDK_lpc.cpp + aacEnc_rom.cpp + fixpoint_math.cpp +
// FDK_tools_rom.cpp + scale.cpp + genericStds.cpp, one go-test binary per package)
// and NEVER imports libraries/aac — importing it would link a second copy of the
// whole FDK reference and clash on static symbols. It MAY, and does, import the
// pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
package enc_pns

/*
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to these integer kernels in any case.
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

#define EP_MAX_GROUPED_SFB 60 // MAX_GROUPED_SFB (psy_const.h:151)

typedef struct {
  int32_t usePns;
  int32_t minCorrelationEnergy;
  int32_t noiseCorrelationThresh;
  int32_t startSfb;
  int32_t detectionAlgorithmFlags;
  int32_t refPower;
  int32_t refTonality;
  int32_t tnsGainThreshold;
  int32_t tnsPNSGainThreshold;
  int32_t minSfbWidth;
  int16_t powDistPSDcurve[EP_MAX_GROUPED_SFB];
  int16_t gapFillThr;
} EP_PNS_CONF;

extern int eparity_init_pns_configuration(int bitRate, int sampleRate, int usePns,
                                          int sfbCnt, const int *sfbOffset,
                                          int numChan, int isLC, EP_PNS_CONF *out);

extern void eparity_pns_detect(const EP_PNS_CONF *conf, int lastWindowSequence,
                               int sfbActive, int maxSfbPerGroup,
                               const int32_t *sfbThresholdLdData,
                               const int *sfbOffset, const int32_t *mdctSpectrum,
                               const int *sfbMaxScaleSpec,
                               const int16_t *sfbtonality, int tnsOrder,
                               int tnsPredictionGain, int tnsActive,
                               const int32_t *sfbEnergyLdData, int specLen,
                               int *pnsFlagOut, int *noiseNrgOut,
                               int16_t *noiseFuzzyOut);

extern void eparity_code_pns_channel(const EP_PNS_CONF *conf, int sfbActive,
                                     const int *pnsFlag,
                                     const int32_t *sfbEnergyLdData,
                                     const int *noiseNrgIn,
                                     const int32_t *sfbThresholdIn, int *noiseNrgOut,
                                     int32_t *sfbThresholdOut);

extern void eparity_pre_process(const EP_PNS_CONF *conf, int sfbActive,
                                const int32_t *sfbEnergyLeft,
                                const int32_t *sfbEnergyRight,
                                const int32_t *sfbEnergyLeftLD,
                                const int32_t *sfbEnergyRightLD,
                                const int32_t *sfbEnergyMid, int32_t *corrOut);

extern void eparity_post_process(const EP_PNS_CONF *conf, int sfbActive,
                                 const int *pnsFlagL, const int *pnsFlagR,
                                 const int32_t *corrL, const int32_t *corrR,
                                 const int *msMaskIn, int msDigestIn,
                                 int *pnsFlagLOut, int *pnsFlagROut, int *msMaskOut,
                                 int *msDigestOut);
*/
import "C"

import "unsafe"

const epMaxGroupedSfb = 60

// cPnsConf is the Go-side mirror of the bridge's EP_PNS_CONF.
type cPnsConf struct {
	usePns                  int32
	minCorrelationEnergy    int32
	noiseCorrelationThresh  int32
	startSfb                int32
	detectionAlgorithmFlags int32
	refPower                int32
	refTonality             int32
	tnsGainThreshold        int32
	tnsPNSGainThreshold     int32
	minSfbWidth             int32
	powDistPSDcurve         [epMaxGroupedSfb]int16
	gapFillThr              int16
}

func (c cPnsConf) toC() C.EP_PNS_CONF {
	var out C.EP_PNS_CONF
	out.usePns = C.int32_t(c.usePns)
	out.minCorrelationEnergy = C.int32_t(c.minCorrelationEnergy)
	out.noiseCorrelationThresh = C.int32_t(c.noiseCorrelationThresh)
	out.startSfb = C.int32_t(c.startSfb)
	out.detectionAlgorithmFlags = C.int32_t(c.detectionAlgorithmFlags)
	out.refPower = C.int32_t(c.refPower)
	out.refTonality = C.int32_t(c.refTonality)
	out.tnsGainThreshold = C.int32_t(c.tnsGainThreshold)
	out.tnsPNSGainThreshold = C.int32_t(c.tnsPNSGainThreshold)
	out.minSfbWidth = C.int32_t(c.minSfbWidth)
	for i := 0; i < epMaxGroupedSfb; i++ {
		out.powDistPSDcurve[i] = C.int16_t(c.powDistPSDcurve[i])
	}
	out.gapFillThr = C.int16_t(c.gapFillThr)
	return out
}

func cConfFrom(out C.EP_PNS_CONF) cPnsConf {
	var c cPnsConf
	c.usePns = int32(out.usePns)
	c.minCorrelationEnergy = int32(out.minCorrelationEnergy)
	c.noiseCorrelationThresh = int32(out.noiseCorrelationThresh)
	c.startSfb = int32(out.startSfb)
	c.detectionAlgorithmFlags = int32(out.detectionAlgorithmFlags)
	c.refPower = int32(out.refPower)
	c.refTonality = int32(out.refTonality)
	c.tnsGainThreshold = int32(out.tnsGainThreshold)
	c.tnsPNSGainThreshold = int32(out.tnsPNSGainThreshold)
	c.minSfbWidth = int32(out.minSfbWidth)
	for i := 0; i < epMaxGroupedSfb; i++ {
		c.powDistPSDcurve[i] = int16(out.powDistPSDcurve[i])
	}
	c.gapFillThr = int16(out.gapFillThr)
	return c
}

// cInitPnsConfiguration runs the genuine FDKaacEnc_InitPnsConfiguration and
// returns its filled config plus the AAC_ENCODER_ERROR code (0 == OK).
func cInitPnsConfiguration(bitRate, sampleRate, usePns, sfbCnt int, sfbOffset []int32,
	numChan, isLC int) (cPnsConf, int) {
	coff := make([]C.int, len(sfbOffset))
	for i, v := range sfbOffset {
		coff[i] = C.int(v)
	}
	var out C.EP_PNS_CONF
	err := C.eparity_init_pns_configuration(
		C.int(bitRate), C.int(sampleRate), C.int(usePns), C.int(sfbCnt),
		(*C.int)(unsafe.Pointer(&coff[0])), C.int(numChan), C.int(isLC), &out)
	return cConfFrom(out), int(err)
}

// cPnsDetect runs the genuine FDKaacEnc_PnsDetect and returns pnsFlag / noiseNrg
// / noiseFuzzyMeasure (each sized MAX_GROUPED_SFB).
func cPnsDetect(conf cPnsConf, lastWindowSequence, sfbActive, maxSfbPerGroup int,
	sfbThresholdLdData, sfbOffset []int32, mdctSpectrum []int32, sfbMaxScaleSpec []int32,
	sfbtonality []int16, tnsOrder, tnsPredictionGain, tnsActive int,
	sfbEnergyLdData []int32) (pnsFlag, noiseNrg []int32, noiseFuzzy []int16) {

	cconf := conf.toC()
	cThr := i32slice(sfbThresholdLdData)
	cOff := islice32(sfbOffset)
	cSpec := i32slice(mdctSpectrum)
	cMaxScale := islice32(sfbMaxScaleSpec)
	cTon := i16slice(sfbtonality)
	cEnLd := i32slice(sfbEnergyLdData)

	pnsFlagC := make([]C.int, epMaxGroupedSfb)
	noiseNrgC := make([]C.int, epMaxGroupedSfb)
	noiseFuzzyC := make([]C.int16_t, epMaxGroupedSfb)

	C.eparity_pns_detect(&cconf, C.int(lastWindowSequence), C.int(sfbActive),
		C.int(maxSfbPerGroup), ptr32(cThr), ptrInt(cOff), ptr32(cSpec),
		ptrInt(cMaxScale), ptr16(cTon), C.int(tnsOrder), C.int(tnsPredictionGain),
		C.int(tnsActive), ptr32(cEnLd), C.int(len(mdctSpectrum)),
		&pnsFlagC[0], &noiseNrgC[0], &noiseFuzzyC[0])

	pnsFlag = make([]int32, epMaxGroupedSfb)
	noiseNrg = make([]int32, epMaxGroupedSfb)
	noiseFuzzy = make([]int16, epMaxGroupedSfb)
	for i := 0; i < epMaxGroupedSfb; i++ {
		pnsFlag[i] = int32(pnsFlagC[i])
		noiseNrg[i] = int32(noiseNrgC[i])
		noiseFuzzy[i] = int16(noiseFuzzyC[i])
	}
	return pnsFlag, noiseNrg, noiseFuzzy
}

// cCodePnsChannel runs the genuine FDKaacEnc_CodePnsChannel and returns the
// modified noiseNrg + sfbThreshold (each sized MAX_GROUPED_SFB).
func cCodePnsChannel(conf cPnsConf, sfbActive int, pnsFlag []int32,
	sfbEnergyLdData []int32, noiseNrgIn []int32, sfbThresholdIn []int32) (noiseNrg []int32, sfbThreshold []int32) {

	cconf := conf.toC()
	cFlag := islice32(pnsFlag)
	cEnLd := i32slice(sfbEnergyLdData)
	cNrgIn := islice32(noiseNrgIn)
	cThrIn := i32slice(sfbThresholdIn)

	noiseNrgC := make([]C.int, epMaxGroupedSfb)
	sfbThrC := make([]C.int32_t, epMaxGroupedSfb)

	C.eparity_code_pns_channel(&cconf, C.int(sfbActive), ptrInt(cFlag), ptr32(cEnLd),
		ptrInt(cNrgIn), ptr32(cThrIn), &noiseNrgC[0], &sfbThrC[0])

	noiseNrg = make([]int32, epMaxGroupedSfb)
	sfbThreshold = make([]int32, epMaxGroupedSfb)
	for i := 0; i < epMaxGroupedSfb; i++ {
		noiseNrg[i] = int32(noiseNrgC[i])
		sfbThreshold[i] = int32(sfbThrC[i])
	}
	return noiseNrg, sfbThreshold
}

// cPreProcess runs the genuine FDKaacEnc_PreProcessPnsChannelPair and returns the
// L channel's noiseEnergyCorrelation (R is identical).
func cPreProcess(conf cPnsConf, sfbActive int, sfbEnergyLeft, sfbEnergyRight,
	sfbEnergyLeftLD, sfbEnergyRightLD, sfbEnergyMid []int32) []int32 {

	cconf := conf.toC()
	cL := i32slice(sfbEnergyLeft)
	cR := i32slice(sfbEnergyRight)
	cLLD := i32slice(sfbEnergyLeftLD)
	cRLD := i32slice(sfbEnergyRightLD)
	cMid := i32slice(sfbEnergyMid)

	corrC := make([]C.int32_t, epMaxGroupedSfb)
	C.eparity_pre_process(&cconf, C.int(sfbActive), ptr32(cL), ptr32(cR),
		ptr32(cLLD), ptr32(cRLD), ptr32(cMid), &corrC[0])

	corr := make([]int32, epMaxGroupedSfb)
	for i := 0; i < epMaxGroupedSfb; i++ {
		corr[i] = int32(corrC[i])
	}
	return corr
}

// cPostProcess runs the genuine FDKaacEnc_PostProcessPnsChannelPair and returns
// the modified pnsFlagL / pnsFlagR / msMask + msDigest.
func cPostProcess(conf cPnsConf, sfbActive int, pnsFlagL, pnsFlagR []int32,
	corrL, corrR []int32, msMaskIn []int32, msDigestIn int) (pnsFlagLOut, pnsFlagROut, msMaskOut []int32, msDigest int) {

	cconf := conf.toC()
	cFL := islice32(pnsFlagL)
	cFR := islice32(pnsFlagR)
	cCL := i32slice(corrL)
	cCR := i32slice(corrR)
	cMM := islice32(msMaskIn)

	flLC := make([]C.int, epMaxGroupedSfb)
	flRC := make([]C.int, epMaxGroupedSfb)
	mmC := make([]C.int, epMaxGroupedSfb)
	var dig C.int

	C.eparity_post_process(&cconf, C.int(sfbActive), ptrInt(cFL), ptrInt(cFR),
		ptr32(cCL), ptr32(cCR), ptrInt(cMM), C.int(msDigestIn),
		&flLC[0], &flRC[0], &mmC[0], &dig)

	pnsFlagLOut = make([]int32, epMaxGroupedSfb)
	pnsFlagROut = make([]int32, epMaxGroupedSfb)
	msMaskOut = make([]int32, epMaxGroupedSfb)
	for i := 0; i < epMaxGroupedSfb; i++ {
		pnsFlagLOut[i] = int32(flLC[i])
		pnsFlagROut[i] = int32(flRC[i])
		msMaskOut[i] = int32(mmC[i])
	}
	return pnsFlagLOut, pnsFlagROut, msMaskOut, int(dig)
}

// ---- small slice->C conversion helpers (kept local to this test package) ----

func i32slice(s []int32) []C.int32_t {
	out := make([]C.int32_t, len(s))
	for i, v := range s {
		out[i] = C.int32_t(v)
	}
	return out
}

func i16slice(s []int16) []C.int16_t {
	out := make([]C.int16_t, len(s))
	for i, v := range s {
		out[i] = C.int16_t(v)
	}
	return out
}

func islice32(s []int32) []C.int {
	out := make([]C.int, len(s))
	for i, v := range s {
		out[i] = C.int(v)
	}
	return out
}

func ptr32(s []C.int32_t) *C.int32_t {
	if len(s) == 0 {
		return nil
	}
	return &s[0]
}

func ptr16(s []C.int16_t) *C.int16_t {
	if len(s) == 0 {
		return nil
	}
	return &s[0]
}

func ptrInt(s []C.int) *C.int {
	if len(s) == 0 {
		return nil
	}
	return &s[0]
}
