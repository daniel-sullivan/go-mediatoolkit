// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Hybrid analysis/synthesis filterbank, ported 1:1 from the vendored Fraunhofer
// FDK-AAC libFDK/src/FDK_hybrid.cpp. The PS tool further splits the lowest 3 QMF
// subbands into finer hybrid bands (8/2/2 -> 12 sub-subbands for the 20-band
// baseline config, THREE_TO_TEN) to gain the frequency resolution the stereo
// cues need, and recombines them on synthesis.
//
// FIXED-POINT / arch convention: the build target is treated as __ARM_ARCH_8__
// (the same convention fixmul.go documents), so ARCH_PREFER_MULT_32x16 is
// defined and the hybrid coefficients FIXP_HTB are FIXP_SGL (Q1.15), with
// HybFilterCoef8 a packed FIXP_SPK. fMult(FIXP_DBL, FIXP_SGL) widens the SGL.
// EXCLUDED: THREE_TO_TWELVE / THREE_TO_SIXTEEN are ported (setup tables present)
// but only THREE_TO_TEN is exercised by the baseline PS path.

// fdkHybridSetup mirrors struct FDK_HYBRID_SETUP (FDK_hybrid.cpp:144-153).
type fdkHybridSetup struct {
	nrQmfBands    uint8    // QMF bands converted to hybrid
	nHybBands     [3]uint8 // hybrid bands generated per QMF band
	synHybScale   [3]uint8 // headroom for hybrid synthesis
	kHybrid       [3]int8  // per-QMF-band filter config
	protoLen      uint8    // prototype filter length
	filterDelay   uint8    // delay caused by hybrid filter
	pReadIdxTable []int32  // ringbuffer access helper
}

// ringbuffIdxTab is the doubled read-index helper (FDK_hybrid.cpp:156).
var ringbuffIdxTab = [2 * 13]int32{
	0, 1, 2, 3, 4, 5, 6, 7, 8,
	9, 10, 11, 12, 0, 1, 2, 3, 4,
	5, 6, 7, 8, 9, 10, 11, 12,
}

// setup_3_16 / setup_3_12 / setup_3_10 (FDK_hybrid.cpp:160-165).
var (
	hybSetup316 = fdkHybridSetup{3, [3]uint8{8, 4, 4}, [3]uint8{4, 3, 3}, [3]int8{8, 4, 4}, 13, (13 - 1) / 2, ringbuffIdxTab[:]}
	hybSetup312 = fdkHybridSetup{3, [3]uint8{8, 2, 2}, [3]uint8{4, 2, 2}, [3]int8{8, 2, 2}, 13, (13 - 1) / 2, ringbuffIdxTab[:]}
	hybSetup310 = fdkHybridSetup{3, [3]uint8{6, 2, 2}, [3]uint8{3, 2, 2}, [3]int8{-8, -2, 2}, 13, (13 - 1) / 2, ringbuffIdxTab[:]}
)

// hybFilterCoef8 is HybFilterCoef8[] (FDK_hybrid.cpp:167), the packed FIXP_HTP
// (FIXP_SPK == [re,im] int16 under SINETABLE_16BIT/ARCH_PREFER_MULT_32x16). The
// hex values are FIXP_DBL Q31 source narrowed to Q15 by HTC == FX_DBL2FXCONST_SGL.
var hybFilterCoef8 = [13]fixHTP{
	htcp(0x10000000, 0x00000000), htcp(0x0df26407, 0xfa391882),
	htcp(0xff532109, 0x00acdef7), htcp(0x08f26d36, 0xf70d92ca),
	htcp(0xfee34b5f, 0x02af570f), htcp(0x038f276e, 0xf7684793),
	htcp(0x00000000, 0x05d1eac2), htcp(0x00000000, 0x05d1eac2),
	htcp(0x038f276e, 0x0897b86d), htcp(0xfee34b5f, 0xfd50a8f1),
	htcp(0x08f26d36, 0x08f26d36), htcp(0xff532109, 0xff532109),
	htcp(0x0df26407, 0x05c6e77e),
}

// hybFilterCoef2 is HybFilterCoef2[3] (FDK_hybrid.cpp:176), FIXP_SGL.
var hybFilterCoef2 = [3]int16{
	htcFromFloat(0.01899487526049),
	htcFromFloat(-0.07293139167538),
	htcFromFloat(0.30596630545168),
}

// hybFilterCoef4 is HybFilterCoef4[13] (FDK_hybrid.cpp:180), FIXP_SGL.
var hybFilterCoef4 = [13]int16{
	htcFromFloat(-0.00305151927305),
	htcFromFloat(-0.00794862316203),
	htcFromFloat(0.0),
	htcFromFloat(0.04318924038756),
	htcFromFloat(0.12542448210445),
	htcFromFloat(0.21227807049160),
	htcFromFloat(0.25),
	htcFromFloat(0.21227807049160),
	htcFromFloat(0.12542448210445),
	htcFromFloat(0.04318924038756),
	htcFromFloat(0.0),
	htcFromFloat(-0.00794862316203),
	htcFromFloat(-0.00305151927305),
}

// fourChanCr / fourChanCi are the pre-twiddle coefficients of fourChannelFiltering
// (FDK_hybrid.cpp:559-574), FIXP_DBL Q31.
var fourChanCr = [13]int32{
	0, fl2fxconstDBL(-0.70710678118655),
	fl2fxconstDBL(-1.0), fl2fxconstDBL(-0.70710678118655),
	0, fl2fxconstDBL(0.70710678118655),
	fl2fxconstDBL(1.0), fl2fxconstDBL(0.70710678118655),
	0, fl2fxconstDBL(-0.70710678118655),
	fl2fxconstDBL(-1.0), fl2fxconstDBL(-0.70710678118655),
	0,
}

var fourChanCi = [13]int32{
	fl2fxconstDBL(-1.0), fl2fxconstDBL(-0.70710678118655),
	0, fl2fxconstDBL(0.70710678118655),
	fl2fxconstDBL(1.0), fl2fxconstDBL(0.70710678118655),
	0, fl2fxconstDBL(-0.70710678118655),
	fl2fxconstDBL(-1.0), fl2fxconstDBL(-0.70710678118655),
	0, fl2fxconstDBL(0.70710678118655),
	fl2fxconstDBL(1.0),
}

// fixHTP is FIXP_HTP/FIXP_SPK: a packed pair of FIXP_SGL (Q1.15) coefficients.
type fixHTP struct {
	re int16
	im int16
}

// fdkHybridMode mirrors FDK_HYBRID_MODE (FDK_hybrid.h:113).
type fdkHybridMode int

const (
	threeToTen fdkHybridMode = iota
	threeToTwelve
	threeToSixteen
)

// fdkAnaHybFilter mirrors FDK_ANA_HYB_FILTER (FDK_hybrid.h:123-143). The LF/HF
// state buffers are flattened into pLFmemory/pHFmemory exactly as the C
// distributes them in FDKhybridAnalysisInit.
type fdkAnaHybFilter struct {
	bufferLFReal [3][]int32
	bufferLFImag [3][]int32
	bufferHFReal [13][]int32
	bufferHFImag [13][]int32

	bufferLFpos int
	bufferHFpos int
	nrBands     int
	cplxBands   int
	hfMode      uint8

	pLFmemory []int32
	pHFmemory []int32

	pSetup *fdkHybridSetup
}

// fdkSynHybFilter mirrors FDK_SYN_HYB_FILTER (FDK_hybrid.h:145-151).
type fdkSynHybFilter struct {
	nrBands   int
	cplxBands int
	pSetup    *fdkHybridSetup
}

// htcFromFloat is HTC(FL2FXCONST_HTB(x)) == FL2FXCONST_SGL(x): the Q1.15 round of
// a float in [-1,1).
func htcFromFloat(x float64) int16 {
	return fl2fxconstSGL(x)
}

// htcp is HTCP(real, imag): narrow two Q31 hex literals to Q15 via HTC ==
// FX_DBL2FXCONST_SGL (the stcNarrow rounding the SineTable uses).
func htcp(re, im uint32) fixHTP {
	return fixHTP{re: nativeaac.StcNarrow(int32(re)), im: nativeaac.StcNarrow(int32(im))}
}

// hybridSelectSetup ports the mode->setup switch (FDK_hybrid.cpp:227-240,445-458).
func hybridSelectSetup(mode fdkHybridMode) *fdkHybridSetup {
	switch mode {
	case threeToTen:
		return &hybSetup310
	case threeToTwelve:
		return &hybSetup312
	case threeToSixteen:
		return &hybSetup316
	}
	return nil
}

// fdkHybridAnalysisOpen ports FDKhybridAnalysisOpen (FDK_hybrid.cpp:204-217):
// just records the externally allocated LF/HF state memory.
func fdkHybridAnalysisOpen(h *fdkAnaHybFilter, lfMem, hfMem []int32) {
	h.pLFmemory = lfMem
	h.pHFmemory = hfMem
}

// fdkHybridAnalysisInit ports FDKhybridAnalysisInit (FDK_hybrid.cpp:219-311):
// selects the setup, distributes the LF (and optional HF) state buffers from
// pLFmemory/pHFmemory, and clears them on initStatesFlag.
func fdkHybridAnalysisInit(h *fdkAnaHybFilter, mode fdkHybridMode, qmfBands, cplxBands int, initStatesFlag bool) {
	setup := hybridSelectSetup(mode)
	h.pSetup = setup
	if initStatesFlag {
		h.bufferLFpos = int(setup.protoLen) - 1
		h.bufferHFpos = 0
	}
	h.nrBands = qmfBands
	h.cplxBands = cplxBands
	h.hfMode = 0

	// Distribute LF memory (FDK_hybrid.cpp:267-274).
	pos := 0
	pl := int(setup.protoLen)
	for k := 0; k < int(setup.nrQmfBands); k++ {
		h.bufferLFReal[k] = h.pLFmemory[pos : pos+pl : pos+pl]
		pos += pl
		h.bufferLFImag[k] = h.pLFmemory[pos : pos+pl : pos+pl]
		pos += pl
	}

	// Distribute HF memory (FDK_hybrid.cpp:276-285). Only when HF mem provided.
	if len(h.pHFmemory) != 0 {
		hpos := 0
		nr := qmfBands - int(setup.nrQmfBands)
		nc := cplxBands - int(setup.nrQmfBands)
		for k := 0; k < int(setup.filterDelay); k++ {
			h.bufferHFReal[k] = h.pHFmemory[hpos : hpos+nr : hpos+nr]
			hpos += nr
			h.bufferHFImag[k] = h.pHFmemory[hpos : hpos+nc : hpos+nc]
			hpos += nc
		}
	}

	if initStatesFlag {
		for k := 0; k < int(setup.nrQmfBands); k++ {
			clearInt32(h.bufferLFReal[k], pl)
			clearInt32(h.bufferLFImag[k], pl)
		}
		if len(h.pHFmemory) != 0 && qmfBands > int(setup.nrQmfBands) {
			for k := 0; k < int(setup.filterDelay); k++ {
				clearInt32(h.bufferHFReal[k], qmfBands-int(setup.nrQmfBands))
				clearInt32(h.bufferHFImag[k], cplxBands-int(setup.nrQmfBands))
			}
		}
	}
}

// fdkHybridAnalysisApply ports FDKhybridAnalysisApply (FDK_hybrid.cpp:345-424):
// for each LF QMF band push the new sample into its ringbuffer and run the
// per-band hybrid filter; then for the HF bands apply the filterDelay/2 delay
// compensation (or, if hfMode != 0, a straight copy).
func fdkHybridAnalysisApply(h *fdkAnaHybFilter, pQmfReal, pQmfImag, pHybridReal, pHybridImag []int32) {
	setup := h.pSetup
	nrQmfBandsLF := int(setup.nrQmfBands)

	writIndex := h.bufferLFpos
	readIndex := h.bufferLFpos
	readIndex++
	if readIndex >= int(setup.protoLen) {
		readIndex = 0
	}
	pBufferLFreadIdx := setup.pReadIdxTable[readIndex:]

	hybOffset := 0
	for k := 0; k < nrQmfBandsLF; k++ {
		h.bufferLFReal[k][writIndex] = pQmfReal[k]
		h.bufferLFImag[k][writIndex] = pQmfImag[k]

		kChannelFiltering(h.bufferLFReal[k], h.bufferLFImag[k], pBufferLFreadIdx,
			pHybridReal[hybOffset:], pHybridImag[hybOffset:], setup.kHybrid[k])

		hybOffset += int(setup.nHybBands[k])
	}

	h.bufferLFpos = readIndex

	if h.nrBands > nrQmfBandsLF {
		if h.hfMode != 0 {
			copy(pHybridReal[hybOffset:hybOffset+(h.nrBands-nrQmfBandsLF)], pQmfReal[nrQmfBandsLF:])
			copy(pHybridImag[hybOffset:hybOffset+(h.cplxBands-nrQmfBandsLF)], pQmfImag[nrQmfBandsLF:])
		} else {
			nr := h.nrBands - nrQmfBandsLF
			nc := h.cplxBands - nrQmfBandsLF
			copy(pHybridReal[hybOffset:hybOffset+nr], h.bufferHFReal[h.bufferHFpos][:nr])
			copy(pHybridImag[hybOffset:hybOffset+nc], h.bufferHFImag[h.bufferHFpos][:nc])

			copy(h.bufferHFReal[h.bufferHFpos][:nr], pQmfReal[nrQmfBandsLF:nrQmfBandsLF+nr])
			copy(h.bufferHFImag[h.bufferHFpos][:nc], pQmfImag[nrQmfBandsLF:nrQmfBandsLF+nc])

			h.bufferHFpos++
			if h.bufferHFpos >= int(setup.filterDelay) {
				h.bufferHFpos = 0
			}
		}
	}
}

// fdkHybridSynthesisInit ports FDKhybridSynthesisInit (FDK_hybrid.cpp:439-466).
func fdkHybridSynthesisInit(h *fdkSynHybFilter, mode fdkHybridMode, qmfBands, cplxBands int) {
	h.pSetup = hybridSelectSetup(mode)
	h.nrBands = qmfBands
	h.cplxBands = cplxBands
}

// fdkHybridSynthesisApply ports FDKhybridSynthesisApply (FDK_hybrid.cpp:468-509):
// recombine the hybrid sub-subbands of each LF QMF band (sum with synHybScale
// headroom, then SATURATE_LEFT_SHIFT back), and straight-copy the HF bands.
func fdkHybridSynthesisApply(h *fdkSynHybFilter, pHybridReal, pHybridImag, pQmfReal, pQmfImag []int32) {
	setup := h.pSetup
	nrQmfBandsLF := int(setup.nrQmfBands)

	hybOffset := 0
	for k := 0; k < nrQmfBandsLF; k++ {
		nHybBands := int(setup.nHybBands[k])
		scale := uint(setup.synHybScale[k])

		var accu1, accu2 int32
		for n := 0; n < nHybBands; n++ {
			accu1 += pHybridReal[hybOffset+n] >> scale
			accu2 += pHybridImag[hybOffset+n] >> scale
		}
		pQmfReal[k] = nativeaac.SaturateLeftShift(accu1, scale)
		pQmfImag[k] = nativeaac.SaturateLeftShift(accu2, scale)

		hybOffset += nHybBands
	}

	if h.nrBands > nrQmfBandsLF {
		copy(pQmfReal[nrQmfBandsLF:nrQmfBandsLF+(h.nrBands-nrQmfBandsLF)], pHybridReal[hybOffset:])
		copy(pQmfImag[nrQmfBandsLF:nrQmfBandsLF+(h.cplxBands-nrQmfBandsLF)], pHybridImag[hybOffset:])
	}
}

// kChannelFiltering ports kChannelFiltering (FDK_hybrid.cpp:786-815): dispatch by
// |config| with sign deciding invert.
func kChannelFiltering(pQmfReal, pQmfImag []int32, pReadIdx []int32, mHybridReal, mHybridImag []int32, hybridConfig int8) {
	switch hybridConfig {
	case 2, -2:
		dualChannelFiltering(pQmfReal, pQmfImag, pReadIdx, mHybridReal, mHybridImag, b2i(hybridConfig < 0))
	case 4, -4:
		fourChannelFiltering(pQmfReal, pQmfImag, pReadIdx, mHybridReal, mHybridImag, b2i(hybridConfig < 0))
	case 8, -8:
		eightChannelFiltering(pQmfReal, pQmfImag, pReadIdx, mHybridReal, mHybridImag, b2i(hybridConfig < 0))
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// dualChannelFiltering ports dualChannelFiltering (FDK_hybrid.cpp:511-547).
func dualChannelFiltering(pQmfReal, pQmfImag []int32, pReadIdx []int32, mHybridReal, mHybridImag []int32, invert int) {
	f0 := hybFilterCoef2[0] // p1 and p11
	f1 := hybFilterCoef2[1] // p3 and p9
	f2 := hybFilterCoef2[2] // p5 and p7

	r1 := fMultDiv2DS(pQmfReal[pReadIdx[1]], f0) + fMultDiv2DS(pQmfReal[pReadIdx[11]], f0)
	i1 := fMultDiv2DS(pQmfImag[pReadIdx[1]], f0) + fMultDiv2DS(pQmfImag[pReadIdx[11]], f0)
	r1 += fMultDiv2DS(pQmfReal[pReadIdx[3]], f1) + fMultDiv2DS(pQmfReal[pReadIdx[9]], f1)
	i1 += fMultDiv2DS(pQmfImag[pReadIdx[3]], f1) + fMultDiv2DS(pQmfImag[pReadIdx[9]], f1)
	r1 += fMultDiv2DS(pQmfReal[pReadIdx[5]], f2) + fMultDiv2DS(pQmfReal[pReadIdx[7]], f2)
	i1 += fMultDiv2DS(pQmfImag[pReadIdx[5]], f2) + fMultDiv2DS(pQmfImag[pReadIdx[7]], f2)

	r6 := pQmfReal[pReadIdx[6]] >> 2
	i6 := pQmfImag[pReadIdx[6]] >> 2

	mHybridReal[0+invert] = nativeaac.SaturateLeftShift(r6+r1, 1)
	mHybridImag[0+invert] = nativeaac.SaturateLeftShift(i6+i1, 1)
	mHybridReal[1-invert] = nativeaac.SaturateLeftShift(r6-r1, 1)
	mHybridImag[1-invert] = nativeaac.SaturateLeftShift(i6-i1, 1)
}

// fourChannelFiltering ports fourChannelFiltering (FDK_hybrid.cpp:549-687).
func fourChannelFiltering(pQmfReal, pQmfImag []int32, pReadIdx []int32, mHybridReal, mHybridImag []int32, invert int) {
	p := &hybFilterCoef4
	cr := &fourChanCr
	ci := &fourChanCi
	var fft [8]int32

	// preMac is fMult(p[pi], fMultSub(fMultDiv2(cr[ci2]*re), ci[ci2]*im)) for the
	// real branch; preMacI for the imag branch. fMultSub(s, a, b) == s - fMult(a,b).
	preR := func(pi, ti, idx int) int32 {
		s := fMultDiv2(cr[ti], pQmfReal[pReadIdx[idx]])
		s = s - fMult(ci[ti], pQmfImag[pReadIdx[idx]])
		return fMultDS(s, p[pi])
	}
	preI := func(pi, ti, idx int) int32 {
		s := fMultDiv2(ci[ti], pQmfReal[pReadIdx[idx]])
		s = s + fMult(cr[ti], pQmfImag[pReadIdx[idx]])
		return fMultDS(s, p[pi])
	}

	fft[idxR(0)] = preR(10, 2, 2) + preR(6, 6, 6) + preR(2, 10, 10)
	fft[idxI(0)] = preI(10, 2, 2) + preI(6, 6, 6) + preI(2, 10, 10)

	fft[idxR(1)] = preR(9, 3, 3) + preR(5, 7, 7) + preR(1, 11, 11)
	fft[idxI(1)] = preI(9, 3, 3) + preI(5, 7, 7) + preI(1, 11, 11)

	fft[idxR(2)] = preR(12, 0, 0) + preR(8, 4, 4) + preR(4, 8, 8) + preR(0, 12, 12)
	fft[idxI(2)] = preI(12, 0, 0) + preI(8, 4, 4) + preI(4, 8, 8) + preI(0, 12, 12)

	fft[idxR(3)] = preR(11, 1, 1) + preR(7, 5, 5) + preR(3, 9, 9)
	fft[idxI(3)] = preI(11, 1, 1) + preI(7, 5, 5) + preI(3, 9, 9)

	// fft modulation, manual length-4 (FDK_hybrid.cpp:656-686).
	mHybridReal[0] = fft[idxR(0)] + fft[idxR(1)] + fft[idxR(2)] + fft[idxR(3)]
	mHybridImag[0] = fft[idxI(0)] + fft[idxI(1)] + fft[idxI(2)] + fft[idxI(3)]

	mHybridReal[1] = fft[idxR(0)] + fft[idxI(1)] - fft[idxR(2)] - fft[idxI(3)]
	mHybridImag[1] = fft[idxI(0)] - fft[idxR(1)] - fft[idxI(2)] + fft[idxR(3)]

	mHybridReal[2] = fft[idxR(0)] - fft[idxR(1)] + fft[idxR(2)] - fft[idxR(3)]
	mHybridImag[2] = fft[idxI(0)] - fft[idxI(1)] + fft[idxI(2)] - fft[idxI(3)]

	mHybridReal[3] = fft[idxR(0)] - fft[idxI(1)] - fft[idxR(2)] + fft[idxI(3)]
	mHybridImag[3] = fft[idxI(0)] + fft[idxR(1)] - fft[idxI(2)] - fft[idxR(3)]
	_ = invert // invert is always 0 for the 4-channel config in the baseline path.
}

// eightChannelFiltering ports eightChannelFiltering (FDK_hybrid.cpp:689-784).
func eightChannelFiltering(pQmfReal, pQmfImag []int32, pReadIdx []int32, mHybridReal, mHybridImag []int32, invert int) {
	p := &hybFilterCoef8
	var pfft [16]int32

	// pre twiddeling (FDK_hybrid.cpp:704-752).
	pfft[idxR(0)] = pQmfReal[pReadIdx[6]] >> (3 + 1)
	pfft[idxI(0)] = pQmfImag[pReadIdx[6]] >> (3 + 1)

	a1, a2 := cplxMultDiv2(pQmfReal[pReadIdx[7]], pQmfImag[pReadIdx[7]], p[1])
	pfft[idxR(1)] = a1
	pfft[idxI(1)] = a2

	a1, a2 = cplxMultDiv2(pQmfReal[pReadIdx[0]], pQmfImag[pReadIdx[0]], p[2])
	a3, a4 := cplxMultDiv2(pQmfReal[pReadIdx[8]], pQmfImag[pReadIdx[8]], p[3])
	pfft[idxR(2)] = a1 + a3
	pfft[idxI(2)] = a2 + a4

	a1, a2 = cplxMultDiv2(pQmfReal[pReadIdx[1]], pQmfImag[pReadIdx[1]], p[4])
	a3, a4 = cplxMultDiv2(pQmfReal[pReadIdx[9]], pQmfImag[pReadIdx[9]], p[5])
	pfft[idxR(3)] = a1 + a3
	pfft[idxI(3)] = a2 + a4

	pfft[idxR(4)] = fMultDiv2DS(pQmfImag[pReadIdx[10]], p[7].im) - fMultDiv2DS(pQmfImag[pReadIdx[2]], p[6].im)
	pfft[idxI(4)] = fMultDiv2DS(pQmfReal[pReadIdx[2]], p[6].im) - fMultDiv2DS(pQmfReal[pReadIdx[10]], p[7].im)

	a1, a2 = cplxMultDiv2(pQmfReal[pReadIdx[3]], pQmfImag[pReadIdx[3]], p[8])
	a3, a4 = cplxMultDiv2(pQmfReal[pReadIdx[11]], pQmfImag[pReadIdx[11]], p[9])
	pfft[idxR(5)] = a1 + a3
	pfft[idxI(5)] = a2 + a4

	a1, a2 = cplxMultDiv2(pQmfReal[pReadIdx[4]], pQmfImag[pReadIdx[4]], p[10])
	a3, a4 = cplxMultDiv2(pQmfReal[pReadIdx[12]], pQmfImag[pReadIdx[12]], p[11])
	pfft[idxR(6)] = a1 + a3
	pfft[idxI(6)] = a2 + a4

	a1, a2 = cplxMultDiv2(pQmfReal[pReadIdx[5]], pQmfImag[pReadIdx[5]], p[12])
	pfft[idxR(7)] = a1
	pfft[idxI(7)] = a2

	// fft modulation.
	fft8(pfft[:])
	sc := uint(1 + 2)

	if invert != 0 {
		mHybridReal[0] = pfft[idxR(7)] << sc
		mHybridImag[0] = pfft[idxI(7)] << sc
		mHybridReal[1] = pfft[idxR(0)] << sc
		mHybridImag[1] = pfft[idxI(0)] << sc

		mHybridReal[2] = pfft[idxR(6)] << sc
		mHybridImag[2] = pfft[idxI(6)] << sc
		mHybridReal[3] = pfft[idxR(1)] << sc
		mHybridImag[3] = pfft[idxI(1)] << sc

		mHybridReal[4] = nativeaac.SaturateLeftShift(pfft[idxR(2)]+pfft[idxR(5)], sc)
		mHybridImag[4] = nativeaac.SaturateLeftShift(pfft[idxI(2)]+pfft[idxI(5)], sc)
		mHybridReal[5] = nativeaac.SaturateLeftShift(pfft[idxR(3)]+pfft[idxR(4)], sc)
		mHybridImag[5] = nativeaac.SaturateLeftShift(pfft[idxI(3)]+pfft[idxI(4)], sc)
	} else {
		for k := 0; k < 8; k++ {
			mHybridReal[k] = pfft[idxR(k)] << sc
			mHybridImag[k] = pfft[idxI(k)] << sc
		}
	}
}

func idxR(a int) int { return 2 * a }
func idxI(a int) int { return 2*a + 1 }

// Local aliases to the shared libFDK fixed-point primitives (exported from
// nativeaac) so the hybrid filterbank reads like the C. The arch-specific
// overload selection (which fixmul form the C picks on __ARM_ARCH_8__) is
// documented at each nativeaac export.
func fMultDiv2DS(a int32, b int16) int32 { return nativeaac.FMultDiv2DS(a, b) } // fMultDiv2(DBL,SGL)
func fMultDS(a int32, b int16) int32     { return nativeaac.FMultDS(a, b) }     // fMult(SGL,DBL)/(DBL,SGL)
func fMult(a, b int32) int32             { return nativeaac.FMultDD(a, b) }     // fMult(DBL,DBL)
func fMultDiv2(a, b int32) int32         { return nativeaac.FMultDiv2DD(a, b) } // fMultDiv2(DBL,DBL)
func fl2fxconstDBL(v float64) int32      { return nativeaac.Fl2fxconstDBL(v) }
func fl2fxconstSGL(v float64) int16      { return nativeaac.Fl2fxconstSGL(v) }
func cplxMultDiv2(aRe, aIm int32, p fixHTP) (int32, int32) {
	return nativeaac.CplxMultDiv2SGL(aRe, aIm, p.re, p.im)
}
func fft8(x []int32) { nativeaac.Fft8(x) }
