// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encadjthr pins the Go port of the Fraunhofer FDK-AAC encoder
// perceptual-entropy DRIVER core — FDKaacEnc_prepareSfbPe / FDKaacEnc_calcSfbPe
// (libAACenc/src/line_pe.cpp) plus the LD-domain helper kernels CalcInvLdData /
// CalcLdInt / fMultNorm / fMultI (libFDK fixpoint_math) the whole
// threshold-adjustment driver (adj_thr.cpp) computes upon — against the genuine
// vendored C, compiled into this test binary via cgo.
//
// line_pe is the load-bearing PE engine of FDKaacEnc_AdjustThresholds: it maps
// the PsyOut sfb energies/thresholds (ld64 log domain) to a per-sfb perceptual
// entropy estimate, and the resulting pe/constPart/nActiveLines figures drive the
// reduction-value iteration that compresses thresholds to the granted bit budget.
// Every value is an int32 FIXP_DBL Q-format / INT with carried block exponents;
// the whole result (the three per-sfb arrays + the three channel sums + the
// per-sfb line estimate) is compared element-for-element, bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C source
// (line_pe.cpp + fixpoint_math.cpp for the LD math + aacEnc_rom.cpp for the
// FDKaacEnc_huff_ltabscf the intensity branch references) and NEVER imports
// libraries/aac — importing it would link a second copy of the FDK reference and
// clash on static symbols (the same amalgamation-split reason the sibling parity
// packages document). It MAY, and does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: libfdk-aac ENCODE is FIXED-POINT — these are pure INTEGER
// kernels (FIXP_DBL == int32, INT == int32). The leading-bit normalisation,
// arithmetic-shift block-floating-point accumulation, int64-product fixmul
// kernels and ROM-table inverse-log2 are bit-identical regardless of
// -ffp-contract / vectorization, with no transcendental and no float. So they
// assert EXACT int32 equality. The oracle links the genuine FDKaacEnc_prepareSfbPe
// / FDKaacEnc_calcSfbPe / CalcInvLdData / CalcLdInt / fMultNorm / fMultI symbols
// (oracle_kind == real_vendored), reached through the thin extern shims in
// bridge.cpp — no hand-twin re-derivation.
package encadjthr

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

extern void eparity_prepare_sfb_pe(const int32_t *sfbEnergyLdData,
    const int32_t *sfbThresholdLdData, const int32_t *sfbFormFactorLdData,
    const int32_t *sfbOffset, int sfbCnt, int sfbPerGroup, int maxSfbPerGroup,
    int32_t *sfbNLinesOut);
extern void eparity_calc_sfb_pe(const int32_t *sfbNLines,
    const int32_t *sfbEnergyLdData, const int32_t *sfbThresholdLdData,
    int sfbCnt, int sfbPerGroup, int maxSfbPerGroup, const int32_t *isBook,
    const int32_t *isScale, int32_t *sfbPeOut, int32_t *sfbConstPartOut,
    int32_t *sfbNActiveLinesOut, int32_t *peOut, int32_t *constPartOut,
    int32_t *nActiveLinesOut);
extern int32_t eparity_calc_inv_ld_data(int32_t x);
extern int32_t eparity_calc_ld_int(int32_t i);
extern int32_t eparity_fmult_norm(int32_t f1, int32_t f2, int32_t *result_e);
extern int32_t eparity_fmult_i(int32_t a, int32_t b);

// --- adj_thr.cpp driver statics (reached via adj_thr_oracle_cgo.cpp) ---------
extern void aparity_prepare_pe(const int32_t *sfbEnergyLdData,
    const int32_t *sfbThresholdLdData, const int32_t *sfbFormFactorLdData,
    const int *sfbOffset, int sfbCnt, int sfbPerGroup, int maxSfbPerGroup,
    int peOffset, int32_t *sfbNLinesOut, int32_t *offsetOut);
extern void aparity_calc_weighting(int nChannels, const int32_t *sfbEnergyLdData,
    const int32_t *sfbEnergy, const int32_t *sfbNLines, const int *sfbOffset,
    const int *lastWindowSequence, const int32_t *msMask, int sfbCnt,
    int sfbPerGroup, int maxSfbPerGroup, int32_t *chaosMeasureEnFac,
    int *lastEnFacPatch, int32_t *sfbEnFacLdOut);
extern void aparity_calc_pe(int nChannels, const int32_t *sfbWeightedEnergyLdData,
    const int32_t *sfbThresholdLdData, const int32_t *sfbNLines, const int *isBook,
    const int *isScale, int sfbCnt, int sfbPerGroup, int maxSfbPerGroup,
    int peOffset, int32_t *peOut, int32_t *constPartOut, int32_t *nActiveLinesOut);
extern void aparity_init_avoid_hole_flag(int nChannels,
    const int32_t *sfbSpreadEnergy, const int32_t *sfbEnergy,
    const int32_t *sfbEnergyLdData, const int32_t *sfbMinSnrLdData,
    const int *sfbOffset, const int *lastWindowSequence, const int32_t *msMask,
    int sfbCnt, int sfbPerGroup, int maxSfbPerGroup, int modifyMinSnr,
    uint8_t *ahFlagOut, int32_t *sfbSpreadEnergyOut, int32_t *sfbMinSnrLdDataOut);
extern void aparity_reduce_thresholds_cbr(int nChannels,
    const int32_t *sfbWeightedEnergyLdData, const int32_t *sfbThresholdLdData,
    const int32_t *sfbMinSnrLdData, const uint8_t *ahFlagIn, const int32_t *thrExp,
    int sfbCnt, int sfbPerGroup, int maxSfbPerGroup, int32_t redValM, int redValE,
    int32_t *sfbThresholdLdDataOut, uint8_t *ahFlagOut);
extern int32_t aparity_calc_chaos_measure(const int32_t *sfbEnergyLdData,
    const int32_t *sfbThresholdLdData, const int32_t *sfbEnergy,
    const int32_t *sfbFormFactorLdData, const int *sfbOffset, int sfbCnt,
    int sfbPerGroup, int maxSfbPerGroup);
extern void aparity_reduce_thresholds_vbr(int nChannels,
    const int32_t *sfbWeightedEnergyLdData, const int32_t *sfbThresholdLdData,
    const int32_t *sfbMinSnrLdData, const int32_t *sfbFormFactorLdData,
    const int32_t *sfbEnergy, const int32_t *sfbEnergyLdData,
    const uint8_t *ahFlagIn, const int32_t *thrExp,
    const int *sfbOffset, int sfbCnt, int sfbPerGroup, int maxSfbPerGroup,
    int lastWindowSequence, const int *groupLen, int32_t vbrQualFactor,
    int32_t *chaosMeasureOldInOut, int32_t *sfbThresholdLdDataOut,
    uint8_t *ahFlagOut);

// --- adj_thr.cpp A-leaf statics (reached via adj_thr_oracle_cgo.cpp) ---------
extern void aparity_calc_thresh_exp(int nChannels, const int32_t *sfbThresholdLdData,
    const int *sfbCnt, const int *sfbPerGroup, const int *maxSfbPerGroup,
    int32_t *thrExpOut);
extern void aparity_adapt_min_snr(int nChannels, const int32_t *sfbEnergy,
    const int32_t *sfbEnergyLdData, const int32_t *sfbMinSnrLdData,
    const int *sfbCnt, const int *sfbPerGroup, const int *maxSfbPerGroup,
    int32_t maxRed, int32_t startRatio, int32_t redRatioFac, int32_t redOffs,
    int32_t *sfbMinSnrLdDataOut);
extern void aparity_reset_ah_flags(int nChannels, const uint8_t *ahFlagIn,
    const int *sfbCnt, const int *sfbPerGroup, const int *maxSfbPerGroup,
    uint8_t *ahFlagOut);
extern void aparity_calc_pe_no_ah(int nChannels, int32_t offset, const int32_t *sfbPe,
    const int32_t *sfbConstPart, const int32_t *sfbNActiveLines,
    const uint8_t *ahFlagIn, const int *sfbCnt, const int *sfbPerGroup,
    const int *maxSfbPerGroup, int *peOut, int *constPartOut, int *nActiveLinesOut);
extern int32_t aparity_calc_bit_save(int32_t fillLevel, int32_t clipLow,
    int32_t clipHigh, int32_t minBitSave, int32_t maxBitSave, int32_t bitsaveSlope);
extern int32_t aparity_calc_bit_spend(int32_t fillLevel, int32_t clipLow,
    int32_t clipHigh, int32_t minBitSpend, int32_t maxBitSpend, int32_t bitspendSlope);
extern void aparity_adjust_pe_min_max(int currPe, int *peMin, int *peMax);

// --- adj_thr.cpp TOP ENTRY (FDKaacEnc_AdjustThresholds, genuine non-static) ---
extern void aparity_adjust_thresholds(int nChannels, int elType,
    const int32_t *sfbEnergy, const int32_t *sfbEnergyLdData,
    const int32_t *sfbThresholdLdData, const int32_t *sfbWeightedEnergyLdData,
    const int32_t *sfbSpreadEnergy, const int32_t *sfbMinSnrLdData,
    const int32_t *sfbFormFactorLdData, const int32_t *sfbEnFacLd,
    const int32_t *sfbPe, const int32_t *sfbConstPart,
    const int32_t *sfbNActiveLines, const int32_t *sfbNLines, const int *sfbOffset,
    const int *lastWindowSequence, const int32_t *msMask, int sfbCnt,
    int sfbPerGroup, int maxSfbPerGroup, int peOffset, int modifyMinSnr,
    int startSfbL, int startSfbS, int32_t maxRed, int32_t startRatio,
    int32_t redRatioFac, int32_t redOffs, int maxIter2ndGuess, int grantedPeCorr,
    int32_t pe, int32_t constPart, int32_t nActiveLines,
    int32_t *sfbThresholdLdDataOut);
*/
import "C"

import "unsafe"

const maxGroupedSFB = 60

// ip returns a *C.int over an int32 slice. Go's int is 64-bit on arm64 while C
// int is 32-bit, so every C INT array crossing the boundary is carried as int32.
func ip(s []int32) *C.int {
	if len(s) == 0 {
		return nil
	}
	return (*C.int)(unsafe.Pointer(&s[0]))
}

func u8p(s []uint8) *C.uint8_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.uint8_t)(unsafe.Pointer(&s[0]))
}

func i32p(s []int32) *C.int32_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.int32_t)(unsafe.Pointer(&s[0]))
}

// cPrepareSfbPe runs the genuine FDKaacEnc_prepareSfbPe and returns sfbNLines
// (length maxGroupedSFB).
func cPrepareSfbPe(sfbEnergyLdData, sfbThresholdLdData, sfbFormFactorLdData, sfbOffset []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int) []int32 {
	out := make([]int32, maxGroupedSFB)
	C.eparity_prepare_sfb_pe(i32p(sfbEnergyLdData), i32p(sfbThresholdLdData),
		i32p(sfbFormFactorLdData), i32p(sfbOffset), C.int(sfbCnt),
		C.int(sfbPerGroup), C.int(maxSfbPerGroup), i32p(out))
	return out
}

// cCalcSfbPe runs the genuine FDKaacEnc_calcSfbPe and returns the three per-sfb
// arrays (length maxGroupedSFB) plus the channel sums (pe, constPart,
// nActiveLines).
func cCalcSfbPe(sfbNLines, sfbEnergyLdData, sfbThresholdLdData []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int, isBook, isScale []int32) (
	sfbPe, sfbConstPart, sfbNActiveLines []int32, pe, constPart, nActiveLines int32) {
	sfbPe = make([]int32, maxGroupedSFB)
	sfbConstPart = make([]int32, maxGroupedSFB)
	sfbNActiveLines = make([]int32, maxGroupedSFB)
	var p, c, n C.int32_t
	C.eparity_calc_sfb_pe(i32p(sfbNLines), i32p(sfbEnergyLdData), i32p(sfbThresholdLdData),
		C.int(sfbCnt), C.int(sfbPerGroup), C.int(maxSfbPerGroup),
		i32p(isBook), i32p(isScale),
		i32p(sfbPe), i32p(sfbConstPart), i32p(sfbNActiveLines), &p, &c, &n)
	return sfbPe, sfbConstPart, sfbNActiveLines, int32(p), int32(c), int32(n)
}

// cCalcInvLdData runs the genuine CalcInvLdData.
func cCalcInvLdData(x int32) int32 { return int32(C.eparity_calc_inv_ld_data(C.int32_t(x))) }

// cCalcLdInt runs the genuine CalcLdInt.
func cCalcLdInt(i int32) int32 { return int32(C.eparity_calc_ld_int(C.int32_t(i))) }

// cFMultNorm runs the genuine fMultNorm, returning (mantissa, exponent).
func cFMultNorm(f1, f2 int32) (int32, int32) {
	var e C.int32_t
	m := C.eparity_fmult_norm(C.int32_t(f1), C.int32_t(f2), &e)
	return int32(m), int32(e)
}

// cFMultI runs the genuine fMultI.
func cFMultI(a, b int32) int32 { return int32(C.eparity_fmult_i(C.int32_t(a), C.int32_t(b))) }

// --- adj_thr.cpp driver-static oracles --------------------------------------

// cPreparePe runs the genuine FDKaacEnc_preparePe for one channel, returning the
// resulting sfbNLines (length maxGroupedSFB) and the stamped offset. sfbOffset is
// int32 (C INT == 32-bit).
func cPreparePe(sfbEnergyLdData, sfbThresholdLdData, sfbFormFactorLdData []int32,
	sfbOffset []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup, peOffset int) (sfbNLines []int32, offset int32) {
	sfbNLines = make([]int32, maxGroupedSFB)
	var off C.int32_t
	C.aparity_prepare_pe(i32p(sfbEnergyLdData), i32p(sfbThresholdLdData),
		i32p(sfbFormFactorLdData), ip(sfbOffset), C.int(sfbCnt), C.int(sfbPerGroup),
		C.int(maxSfbPerGroup), C.int(peOffset), i32p(sfbNLines), &off)
	return sfbNLines, int32(off)
}

// cCalcWeighting runs the genuine FDKaacEnc_calcWeighting for nChannels. The flat
// per-channel inputs/state mirror the Go ParityCalcWeighting layout.
func cCalcWeighting(nChannels int, sfbEnergyLdData, sfbEnergy, sfbNLines []int32,
	sfbOffset, lastWindowSequence []int32, msMask []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup int,
	chaosMeasureEnFac []int32, lastEnFacPatch []int32) (sfbEnFacLd []int32) {
	sfbEnFacLd = make([]int32, nChannels*maxGroupedSFB)
	C.aparity_calc_weighting(C.int(nChannels), i32p(sfbEnergyLdData), i32p(sfbEnergy),
		i32p(sfbNLines), ip(sfbOffset), ip(lastWindowSequence), i32p(msMask),
		C.int(sfbCnt), C.int(sfbPerGroup), C.int(maxSfbPerGroup),
		i32p(chaosMeasureEnFac), ip(lastEnFacPatch), i32p(sfbEnFacLd))
	return sfbEnFacLd
}

// cCalcPe runs the genuine FDKaacEnc_calcPe for nChannels.
func cCalcPe(nChannels int, sfbWeightedEnergyLdData, sfbThresholdLdData, sfbNLines []int32,
	isBook, isScale []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup, peOffset int) (pe, constPart, nActiveLines int32) {
	var p, c, n C.int32_t
	C.aparity_calc_pe(C.int(nChannels), i32p(sfbWeightedEnergyLdData), i32p(sfbThresholdLdData),
		i32p(sfbNLines), ip(isBook), ip(isScale), C.int(sfbCnt), C.int(sfbPerGroup),
		C.int(maxSfbPerGroup), C.int(peOffset), &p, &c, &n)
	return int32(p), int32(c), int32(n)
}

// cInitAvoidHoleFlag runs the genuine FDKaacEnc_initAvoidHoleFlag for nChannels.
func cInitAvoidHoleFlag(nChannels int, sfbSpreadEnergy, sfbEnergy, sfbEnergyLdData, sfbMinSnrLdData []int32,
	sfbOffset, lastWindowSequence []int32, msMask []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup, modifyMinSnr int) (
	ahFlag []uint8, sfbSpreadEnergyOut, sfbMinSnrLdDataOut []int32) {
	ahFlag = make([]uint8, nChannels*maxGroupedSFB)
	sfbSpreadEnergyOut = make([]int32, nChannels*maxGroupedSFB)
	sfbMinSnrLdDataOut = make([]int32, nChannels*maxGroupedSFB)
	C.aparity_init_avoid_hole_flag(C.int(nChannels), i32p(sfbSpreadEnergy), i32p(sfbEnergy),
		i32p(sfbEnergyLdData), i32p(sfbMinSnrLdData), ip(sfbOffset), ip(lastWindowSequence),
		i32p(msMask), C.int(sfbCnt), C.int(sfbPerGroup), C.int(maxSfbPerGroup), C.int(modifyMinSnr),
		u8p(ahFlag), i32p(sfbSpreadEnergyOut), i32p(sfbMinSnrLdDataOut))
	return ahFlag, sfbSpreadEnergyOut, sfbMinSnrLdDataOut
}

// cReduceThresholdsCBR runs the genuine FDKaacEnc_reduceThresholdsCBR.
func cReduceThresholdsCBR(nChannels int, sfbWeightedEnergyLdData, sfbThresholdLdData, sfbMinSnrLdData []int32,
	ahFlagIn []uint8, thrExp []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup int, redValM int32, redValE int) (
	sfbThresholdLdDataOut []int32, ahFlagOut []uint8) {
	sfbThresholdLdDataOut = make([]int32, nChannels*maxGroupedSFB)
	ahFlagOut = make([]uint8, nChannels*maxGroupedSFB)
	C.aparity_reduce_thresholds_cbr(C.int(nChannels), i32p(sfbWeightedEnergyLdData),
		i32p(sfbThresholdLdData), i32p(sfbMinSnrLdData), u8p(ahFlagIn), i32p(thrExp),
		C.int(sfbCnt), C.int(sfbPerGroup), C.int(maxSfbPerGroup), C.int32_t(redValM),
		C.int(redValE), i32p(sfbThresholdLdDataOut), u8p(ahFlagOut))
	return sfbThresholdLdDataOut, ahFlagOut
}

// cReduceThresholdsVBR runs the genuine FDKaacEnc_reduceThresholdsVBR. groupLen
// is the per-group window-count vector (len MAX_NO_OF_GROUPS); chaosMeasureOld is
// updated in place (input + output state).
func cReduceThresholdsVBR(nChannels, stride int, sfbWeightedEnergyLdData, sfbThresholdLdData,
	sfbMinSnrLdData, sfbFormFactorLdData, sfbEnergy, sfbEnergyLdData []int32, ahFlagIn []uint8, thrExp []int32,
	sfbOffset []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup, lastWindowSequence int,
	groupLen []int32, vbrQualFactor int32, chaosMeasureOld int32) (
	sfbThresholdLdDataOut []int32, ahFlagOut []uint8, chaosMeasureOldOut int32) {
	sfbThresholdLdDataOut = make([]int32, nChannels*stride)
	ahFlagOut = make([]uint8, nChannels*stride)
	cmo := C.int32_t(chaosMeasureOld)
	C.aparity_reduce_thresholds_vbr(C.int(nChannels), i32p(sfbWeightedEnergyLdData),
		i32p(sfbThresholdLdData), i32p(sfbMinSnrLdData), i32p(sfbFormFactorLdData),
		i32p(sfbEnergy), i32p(sfbEnergyLdData), u8p(ahFlagIn), i32p(thrExp), ip(sfbOffset),
		C.int(sfbCnt), C.int(sfbPerGroup), C.int(maxSfbPerGroup),
		C.int(lastWindowSequence), ip(groupLen), C.int32_t(vbrQualFactor),
		&cmo, i32p(sfbThresholdLdDataOut), u8p(ahFlagOut))
	return sfbThresholdLdDataOut, ahFlagOut, int32(cmo)
}

// cCalcChaosMeasure runs the genuine FDKaacEnc_calcChaosMeasure for one channel.
func cCalcChaosMeasure(sfbEnergyLdData, sfbThresholdLdData, sfbEnergy, sfbFormFactorLdData []int32,
	sfbOffset []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup int) int32 {
	return int32(C.aparity_calc_chaos_measure(i32p(sfbEnergyLdData), i32p(sfbThresholdLdData),
		i32p(sfbEnergy), i32p(sfbFormFactorLdData), ip(sfbOffset), C.int(sfbCnt),
		C.int(sfbPerGroup), C.int(maxSfbPerGroup)))
}

// --- A-leaves cgo wrappers ---------------------------------------------------

// ipi returns a *C.int over an int slice (each cell widened to C int == int32).
func ipi(s []int) []C.int {
	out := make([]C.int, len(s))
	for i, v := range s {
		out[i] = C.int(v)
	}
	return out
}

func cintp(s []C.int) *C.int {
	if len(s) == 0 {
		return nil
	}
	return &s[0]
}

// cCalcThreshExp runs the genuine static FDKaacEnc_calcThreshExp.
func cCalcThreshExp(nChannels int, sfbThresholdLdData []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup []int) []int32 {
	out := make([]int32, nChannels*maxGroupedSFB)
	c, p, m := ipi(sfbCnt), ipi(sfbPerGroup), ipi(maxSfbPerGroup)
	C.aparity_calc_thresh_exp(C.int(nChannels), i32p(sfbThresholdLdData),
		cintp(c), cintp(p), cintp(m), i32p(out))
	return out
}

// cAdaptMinSnr runs the genuine static FDKaacEnc_adaptMinSnr.
func cAdaptMinSnr(nChannels int, sfbEnergy, sfbEnergyLdData, sfbMinSnrLdData []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup []int, maxRed, startRatio, redRatioFac, redOffs int32) []int32 {
	out := make([]int32, nChannels*maxGroupedSFB)
	c, p, m := ipi(sfbCnt), ipi(sfbPerGroup), ipi(maxSfbPerGroup)
	C.aparity_adapt_min_snr(C.int(nChannels), i32p(sfbEnergy), i32p(sfbEnergyLdData),
		i32p(sfbMinSnrLdData), cintp(c), cintp(p), cintp(m),
		C.int32_t(maxRed), C.int32_t(startRatio), C.int32_t(redRatioFac), C.int32_t(redOffs),
		i32p(out))
	return out
}

// cResetAHFlags runs the genuine static FDKaacEnc_resetAHFlags.
func cResetAHFlags(nChannels int, ahFlagIn []uint8, sfbCnt, sfbPerGroup, maxSfbPerGroup []int) []uint8 {
	out := make([]uint8, nChannels*maxGroupedSFB)
	c, p, m := ipi(sfbCnt), ipi(sfbPerGroup), ipi(maxSfbPerGroup)
	C.aparity_reset_ah_flags(C.int(nChannels), u8p(ahFlagIn), cintp(c), cintp(p), cintp(m), u8p(out))
	return out
}

// cCalcPeNoAH runs the genuine static FDKaacEnc_FDKaacEnc_calcPeNoAH.
func cCalcPeNoAH(nChannels int, offset int32, sfbPe, sfbConstPart, sfbNActiveLines []int32,
	ahFlagIn []uint8, sfbCnt, sfbPerGroup, maxSfbPerGroup []int) (pe, constPart, nActiveLines int) {
	var p, cp, nal C.int
	c, pg, m := ipi(sfbCnt), ipi(sfbPerGroup), ipi(maxSfbPerGroup)
	C.aparity_calc_pe_no_ah(C.int(nChannels), C.int32_t(offset), i32p(sfbPe), i32p(sfbConstPart),
		i32p(sfbNActiveLines), u8p(ahFlagIn), cintp(c), cintp(pg), cintp(m), &p, &cp, &nal)
	return int(p), int(cp), int(nal)
}

// cCalcBitSave runs the genuine static FDKaacEnc_calcBitSave.
func cCalcBitSave(fillLevel, clipLow, clipHigh, minBitSave, maxBitSave, bitsaveSlope int32) int32 {
	return int32(C.aparity_calc_bit_save(C.int32_t(fillLevel), C.int32_t(clipLow), C.int32_t(clipHigh),
		C.int32_t(minBitSave), C.int32_t(maxBitSave), C.int32_t(bitsaveSlope)))
}

// cCalcBitSpend runs the genuine static FDKaacEnc_calcBitSpend.
func cCalcBitSpend(fillLevel, clipLow, clipHigh, minBitSpend, maxBitSpend, bitspendSlope int32) int32 {
	return int32(C.aparity_calc_bit_spend(C.int32_t(fillLevel), C.int32_t(clipLow), C.int32_t(clipHigh),
		C.int32_t(minBitSpend), C.int32_t(maxBitSpend), C.int32_t(bitspendSlope)))
}

// cAdjustPeMinMax runs the genuine static FDKaacEnc_adjustPeMinMax.
func cAdjustPeMinMax(currPe, peMin, peMax int) (int, int) {
	cmin, cmax := C.int(peMin), C.int(peMax)
	C.aparity_adjust_pe_min_max(C.int(currPe), &cmin, &cmax)
	return int(cmin), int(cmax)
}

// adjThrCParams mirrors nativeaac.AdjThrParams for the cgo oracle call.
type adjThrCParams struct {
	peOffset                    int
	modifyMinSnr                int
	startSfbL, startSfbS        int
	maxRed, startRatio          int32
	redRatioFac, redOffs        int32
	maxIter2ndGuess             int
	grantedPeCorr               int
	pe, constPart, nActiveLines int32
}

// cAdjustThresholds runs the genuine FDKaacEnc_AdjustThresholds top entry for a
// single CBR AAC-LC element, returning the mutated sfbThresholdLdData per channel.
func cAdjustThresholds(nChannels, elType int,
	sfbEnergy, sfbEnergyLdData, sfbThresholdLdData, sfbWeightedEnergyLdData,
	sfbSpreadEnergy, sfbMinSnrLdData, sfbFormFactorLdData, sfbEnFacLd,
	sfbPe, sfbConstPart, sfbNActiveLines, sfbNLines []int32,
	sfbOffset, lastWindowSequence []int32, msMask []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int, p adjThrCParams) []int32 {
	out := make([]int32, nChannels*maxGroupedSFB)
	C.aparity_adjust_thresholds(C.int(nChannels), C.int(elType),
		i32p(sfbEnergy), i32p(sfbEnergyLdData), i32p(sfbThresholdLdData),
		i32p(sfbWeightedEnergyLdData), i32p(sfbSpreadEnergy), i32p(sfbMinSnrLdData),
		i32p(sfbFormFactorLdData), i32p(sfbEnFacLd), i32p(sfbPe), i32p(sfbConstPart),
		i32p(sfbNActiveLines), i32p(sfbNLines), ip(sfbOffset), ip(lastWindowSequence),
		i32p(msMask), C.int(sfbCnt), C.int(sfbPerGroup), C.int(maxSfbPerGroup),
		C.int(p.peOffset), C.int(p.modifyMinSnr), C.int(p.startSfbL), C.int(p.startSfbS),
		C.int32_t(p.maxRed), C.int32_t(p.startRatio), C.int32_t(p.redRatioFac), C.int32_t(p.redOffs),
		C.int(p.maxIter2ndGuess), C.int(p.grantedPeCorr),
		C.int32_t(p.pe), C.int32_t(p.constPart), C.int32_t(p.nActiveLines), i32p(out))
	return out
}
