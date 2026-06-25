// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Parametric-stereo ENCODE wrapper + downmix — 1:1 port of
// libSBRenc/src/ps_main.cpp (M. Multrus). This is the per-frame driver that sits
// above the already-ported PS parameter extractor (enc_ps_encode.go,
// FDKsbrEnc_PSEncode) and ps_data writer (enc_ps_bitenc.go): it runs the stereo
// QMF analysis + hybrid analysis (THREE_TO_TEN) on the two input channels,
// extracts the PS parameters, then downmixes the stereo hybrid data to a mono
// QMF signal (DownmixPSQmfData) — applying the per-band stereo scale factor,
// hybrid synthesis, the half-rate QMF synthesis that produces the downsampled
// mono core signal, and the stereo qmfDelayLines swap that aligns the SBR-domain
// downmix the core encoder consumes.
//
// FDK-AAC-derived; see libfdk/COPYING. Fenced behind the aacfdk build tag.
// FIXED-POINT => byte-identical. HE-AAC v2 GA baseline (MAX_PS_CHANNELS == 2,
// THREE_TO_TEN, dual-rate, AOT_PS); DRM/LD/ELD/USAC-MPS212 PS excluded; IPD/OPD
// not transmitted.
package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// PSENC_NENV / PSENC_STEREO_BANDS config bounds (ps_main.h:115-129).
const (
	psencNEnv1       = 1
	psencNEnvDefault = 2
	psencNEnvMax     = 4
)

// maxHybridBands == MAX_HYBRID_BANDS (ps_const.h) == maxHybridBandsE (== 71),
// the per-(slot,channel,reim) hybrid vector length used throughout ps_main.
const maxHybridBands = maxHybridBandsE

// hybridFilterDelay == HYBRID_FILTER_DELAY (ps_const.h): psDelay basis.
const hybridFilterDelay = (hybridFilterLength - 1) / 2 // == hybridFilterDelayE == 6

// psEncConfig is the 1:1 port of T_PSENC_CONFIG (ps_main.h:173-181). frameSize,
// qmfFilterMode and sbrPsDelay are carried for fidelity; only nStereoBands,
// maxEnvelopes and iidQuantErrorThreshold drive the GA baseline.
type psEncConfig struct {
	frameSize              int
	qmfFilterMode          int
	sbrPsDelay             int
	nStereoBands           int // PSENC_STEREO_BANDS_CONFIG (10 or 20)
	maxEnvelopes           int // PSENC_NENV_CONFIG
	iidQuantErrorThreshold int32
}

// PSEncConfig is the exported PS tuning the heaac glue supplies to
// SbrEncoderInitPS: the per-bitrate nStereoBands / maxEnvelopes / IID quant-error
// threshold (psTuningTable, sbrenc_rom.cpp:899-908).
type PSEncConfig struct {
	NStereoBands           int
	MaxEnvelopes           int
	IidQuantErrorThreshold int32
}

// ParametricStereo is the 1:1 port of T_PARAMETRIC_STEREO (ps_main.h:143-171):
// the complete per-instance PS encode state — the parameter extractor, the
// double-buffered PS_OUT, the hybrid ring buffer (HYBRID_READ_OFFSET history +
// HYBRID_FRAMESIZE current slots), the stereo QMF delay lines, the scaling
// buffers, and the per-channel analysis hybrid banks + the synthesis hybrid bank.
type ParametricStereo struct {
	hPsEncode *psEncode
	psOut     [2]psOut

	// pHybridData[slot][ch][reim] -> a length-maxHybridBands vector. The first
	// HYBRID_READ_OFFSET slots are the saved history (staticHybridData), the
	// remaining HYBRID_FRAMESIZE slots are this frame's hybrid analysis output.
	pHybridData [hybridReadOffset + hybridFramesize][maxPsChannels][2][]int32

	// qmfDelayLines[reim][noQmfSlots>>1][noQmfBands] (ps_main.h:152). 32>>1 == 16
	// slots, 64 bands.
	qmfDelayLines [2][32 >> 1][64]int32
	qmfDelayScale int

	psDelay      int
	maxEnvelopes int
	dynBandScale [psMaxBandsE]uint8
	maxBandValue [psMaxBandsE]int32
	dmxScale     int
	initPS       int
	noQmfSlots   int
	noQmfBands   int

	fdkHybAnaFilter [maxPsChannels]fdkAnaHybFilter
	fdkHybSynFilter fdkSynHybFilter
}

// NoChannels returns the synthesis filterbank channel count (qmf.cpp accessor).
func (h *FilterBank) NoChannels() int { return h.noChannels }

// OutScalefactor returns the filterbank out-scale headroom (== outScalefactor),
// the value ps_main reads as hQmfAnalysis[psCh]->outScalefactor.
func (h *FilterBank) OutScalefactor() int { return h.outScalefactor }

// PSEncCreate is the 1:1 port of PSEnc_Create (ps_main.cpp:116-159): allocate the
// PS encode instance, create the parameter extractor and open the two analysis
// hybrid banks over their (zeroed-on-init) LF/HF state memory.
func PSEncCreate() *ParametricStereo {
	h := new(ParametricStereo)
	h.hPsEncode = createPSEncode()
	for i := 0; i < maxPsChannels; i++ {
		// __staticHybAnaStatesLF[2*HYBRID_FILTER_LENGTH*HYBRID_MAX_QMF_BANDS],
		// __staticHybAnaStatesHF[2*HYBRID_FILTER_DELAY*(64-HYBRID_MAX_QMF_BANDS)]
		// (ps_main.h:164-167). FDKhybridAnalysisInit re-distributes these.
		lf := make([]int32, 2*hybridFilterLength*hybridMaxQmfBands)
		hf := make([]int32, 2*hybridFilterDelay*(64-hybridMaxQmfBands))
		fdkHybridAnalysisOpen(&h.fdkHybAnaFilter[i], lf, hf)
	}
	return h
}

// PSEncInit is the 1:1 port of PSEnc_Init (ps_main.cpp:161-245): initialise the
// hybrid banks (THREE_TO_TEN, 64, 64), the synthesis hybrid bank, the parameter
// extractor, wire the hybrid ring-buffer slot vectors, clear the static/scaling
// buffers and force a PS header in the first frame.
func PSEncInit(h *ParametricStereo, cfg *psEncConfig, noQmfSlots, noQmfBands int) int {
	h.initPS = 1
	h.noQmfSlots = noQmfSlots
	h.noQmfBands = noQmfBands

	// clear delay lines.
	for r := 0; r < 2; r++ {
		for s := 0; s < (32 >> 1); s++ {
			for b := 0; b < 64; b++ {
				h.qmfDelayLines[r][s][b] = 0
			}
		}
	}
	h.qmfDelayScale = fractBits - 1

	for ch := 0; ch < maxPsChannels; ch++ {
		fdkHybridAnalysisInit(&h.fdkHybAnaFilter[ch], threeToTen, 64, 64, true)
	}
	fdkHybridSynthesisInit(&h.fdkHybSynFilter, threeToTen, 64, 64)

	// determine average delay.
	h.psDelay = hybridFilterDelay * h.noQmfBands

	if cfg.maxEnvelopes < psencNEnv1 || cfg.maxEnvelopes > psencNEnvMax {
		cfg.maxEnvelopes = psencNEnvDefault
	}
	h.maxEnvelopes = cfg.maxEnvelopes

	if e := initPSEncode(h.hPsEncode, cfg.nStereoBands, cfg.iidQuantErrorThreshold); e != psencOK {
		return e
	}

	// Wire the hybrid ring buffer. The current HYBRID_FRAMESIZE slots are this
	// frame's dynamic hybrid output; the first HYBRID_READ_OFFSET slots are saved
	// history. Each [ch][reim] entry is a length-maxHybridBands vector (the C uses
	// the env R/I dynamic scratch + the static history; in Go each gets its own
	// allocation, the layout is otherwise identical).
	for ch := 0; ch < maxPsChannels; ch++ {
		for i := 0; i < hybridFramesize; i++ {
			h.pHybridData[i+hybridReadOffset][ch][0] = make([]int32, maxHybridBands)
			h.pHybridData[i+hybridReadOffset][ch][1] = make([]int32, maxHybridBands)
		}
		for i := 0; i < hybridReadOffset; i++ {
			h.pHybridData[i][ch][0] = make([]int32, maxHybridBands)
			h.pHybridData[i][ch][1] = make([]int32, maxHybridBands)
		}
	}

	// clear bs buffer.
	h.psOut[0] = psOut{}
	h.psOut[1] = psOut{}
	h.psOut[0].enablePSHeader = 1 // write ps header in first frame.

	for i := range h.dynBandScale {
		h.dynBandScale[i] = 0
	}
	for i := range h.maxBandValue {
		h.maxBandValue[i] = 0
	}
	return psencOK
}

// extractPSParameters is the 1:1 port of ExtractPSParameters (ps_main.cpp:261-290).
// hybridData is &pHybridData[0] (the full ring, slot 0..noQmfSlots-1).
func extractPSParameters(h *ParametricStereo, sendHeader int,
	hybridData [hybridFramesize][maxPsChannels][2][]int32) int {

	if h.initPS != 0 {
		h.psOut[1] = h.psOut[0]
	}
	h.psOut[0] = h.psOut[1]

	if e := psEncodeRun(h.hPsEncode, &h.psOut[1], h.dynBandScale[:], uint(h.maxEnvelopes),
		hybridData, h.noQmfSlots, sendHeader); e != psencOK {
		return e
	}

	if h.initPS != 0 {
		h.psOut[0] = h.psOut[1]
		h.initPS = 0
	}
	return psencOK
}

// downmixPSQmfData is the 1:1 port of DownmixPSQmfData (ps_main.cpp:292-451).
// mixRealQmfData/mixImagQmfData are noQmfSlots slices of 64 int32 (the downmixed
// QMF the core's extractSbrEnvelope1 consumes). downsampledOutSignal receives the
// half-rate mono time signal (INT_PCM == int16). hybridData is the
// pHybridData[HYBRID_READ_OFFSET..] window. psQmfScale is the per-channel analysis
// out-scale. On return *qmfScale is the downmix scale.
func downmixPSQmfData(h *ParametricStereo, sbrSynthQmf *FilterBank,
	mixRealQmfData, mixImagQmfData [][]int32, downsampledOutSignal []int16,
	hybridData [hybridFramesize][maxPsChannels][2][]int32, noQmfSlots int,
	psQmfScale [maxPsChannels]int, qmfScale *int) {

	pWorkBuffer := make([]int32, 2*64)

	// define scalings.
	dynQmfScale := fixMaxI(0, h.dmxScale-1) // scale one bit more for L+R add.
	downmixScale := psQmfScale[0] - dynQmfScale
	const maxStereoScaleFactor = int32(0x7FFFFFFF) // MAXVAL_DBL (2.f/2.f)

	for n := 0; n < noQmfSlots; n++ {
		var tmpHybrid [2][maxHybridBands]int32

		for k := 0; k < 71; k++ {
			tmpLeftReal := hybridData[n][0][0][k]
			tmpLeftImag := hybridData[n][0][1][k]
			tmpRightReal := hybridData[n][1][0][k]
			tmpRightImag := hybridData[n][1][1][k]

			sc := fixMaxI(0, nativeaac.CntLeadingZeros(fixMaxI32(
				fixMaxI32(fAbs(tmpLeftReal), fAbs(tmpLeftImag)),
				fixMaxI32(fAbs(tmpRightReal), fAbs(tmpRightImag))))-2)

			tmpLeftReal <<= uint(sc)
			tmpLeftImag <<= uint(sc)
			tmpRightReal <<= uint(sc)
			tmpRightImag <<= uint(sc)
			dynScale := fixMinI(sc-dynQmfScale, dfractBits-1)

			// stereo scale factor (energy of left + right).
			stereoScaleFactor := fPow2Div2PS(tmpLeftReal) + fPow2Div2PS(tmpLeftImag) +
				fPow2Div2PS(tmpRightReal) + fPow2Div2PS(tmpRightImag)

			tmpScaleFactor := fAbs(stereoScaleFactor + fMult(tmpLeftReal, tmpRightReal) +
				fMult(tmpLeftImag, tmpRightImag))

			// min(2.0f, sqrt(stereoScaleFactor/(0.5f*tmpScaleFactor))).
			if (stereoScaleFactor >> 1) < fMult(maxStereoScaleFactor, tmpScaleFactor) {
				scNum := nativeaac.CountLeadingBits(stereoScaleFactor)
				scDenum := nativeaac.CountLeadingBits(tmpScaleFactor)
				sc = -(scNum - scDenum)

				tmpScaleFactor = schurDivPS((stereoScaleFactor<<uint(scNum))>>1,
					tmpScaleFactor<<uint(scDenum), 16)

				if sc&0x1 != 0 {
					sc++
					tmpScaleFactor >>= 1
				}
				stereoScaleFactor = sqrtFixpPS(tmpScaleFactor)
				// C: stereoScaleFactor <<= (sc >> 1). sc is even here (the sc&1
				// adjust above), so sc>>1 is exact; when negative this is a right
				// shift (Go's uint() of a negative would wrap, so branch on sign).
				if shift := sc >> 1; shift >= 0 {
					stereoScaleFactor <<= uint(shift)
				} else {
					stereoScaleFactor >>= uint(-shift)
				}
			} else {
				stereoScaleFactor = maxStereoScaleFactor
			}

			// write data to hybrid output.
			tmpHybrid[0][k] = fMultDiv2(stereoScaleFactor, tmpLeftReal+tmpRightReal) >> uint(dynScale)
			tmpHybrid[1][k] = fMultDiv2(stereoScaleFactor, tmpLeftImag+tmpRightImag) >> uint(dynScale)
		} // hybrid bands - k

		fdkHybridSynthesisApply(&h.fdkHybSynFilter, tmpHybrid[0][:], tmpHybrid[1][:],
			mixRealQmfData[n], mixImagQmfData[n])

		// half-rate QMF synthesis -> downsampled mono time signal (INT_PCM ==
		// int16). The SAMPLE_BITS == 16 synthesis slot writes int16 directly.
		nch := sbrSynthQmf.noChannels
		SynthesisFilteringSlotPCM16(sbrSynthQmf, mixRealQmfData[n], mixImagQmfData[n],
			downmixScale-7, downmixScale-7, downsampledOutSignal[n*nch:], 1, pWorkBuffer)
	} // slots

	*qmfScale = -downmixScale + 7

	// qmfDelayLines stereo-delay swap (ps_main.cpp:398-446).
	noQmfSlots2 := h.noQmfSlots >> 1
	noQmfBands := h.noQmfBands

	var tmp [2][64]int32
	for i := 0; i < noQmfSlots2; i++ {
		copy(tmp[0][:noQmfBands], h.qmfDelayLines[0][i][:noQmfBands])
		copy(tmp[1][:noQmfBands], h.qmfDelayLines[1][i][:noQmfBands])

		copy(h.qmfDelayLines[0][i][:noQmfBands], mixRealQmfData[i+noQmfSlots2][:noQmfBands])
		copy(h.qmfDelayLines[1][i][:noQmfBands], mixImagQmfData[i+noQmfSlots2][:noQmfBands])

		copy(mixRealQmfData[i+noQmfSlots2][:noQmfBands], mixRealQmfData[i][:noQmfBands])
		copy(mixImagQmfData[i+noQmfSlots2][:noQmfBands], mixImagQmfData[i][:noQmfBands])

		copy(mixRealQmfData[i][:noQmfBands], tmp[0][:noQmfBands])
		copy(mixImagQmfData[i][:noQmfBands], tmp[1][:noQmfBands])
	}

	var scale, slotOffset int
	if h.qmfDelayScale > *qmfScale {
		scale = h.qmfDelayScale - *qmfScale
		slotOffset = 0
	} else {
		scale = *qmfScale - h.qmfDelayScale
		slotOffset = noQmfSlots2
	}

	for i := 0; i < noQmfSlots2; i++ {
		for j := 0; j < noQmfBands; j++ {
			mixRealQmfData[i+slotOffset][j] >>= uint(scale)
			mixImagQmfData[i+slotOffset][j] >>= uint(scale)
		}
	}

	scale = *qmfScale
	if h.qmfDelayScale < *qmfScale {
		*qmfScale = h.qmfDelayScale
	}
	h.qmfDelayScale = scale
}

// PSEncWritePSData is the 1:1 port of FDKsbrEnc_PSEnc_WritePSData
// (ps_main.cpp:453-459): write the (one-frame-delayed) psOut[0] ps_data().
func PSEncWritePSData(h *ParametricStereo, bs *FdkBitStream) int {
	if h == nil {
		return 0
	}
	return writePSBitstream(&h.psOut[0], bs)
}

// PSEncParametricStereoProcessing is the 1:1 port of
// FDKsbrEnc_PSEnc_ParametricStereoProcessing (ps_main.cpp:461-542): per-channel
// QMF analysis (slot) -> hybrid analysis -> psFindBestScaling -> PS parameter
// extraction -> hybrid-history shift -> downmix. samples[ch] is the planar int16
// input for that channel (length >= noQmfSlots*noQmfBands). downmixedReal/Imag are
// noQmfSlots slices of 64 int32 receiving the downmixed QMF. downsampledOutSignal
// receives the half-rate mono time signal. On return *qmfScale is the downmix
// scale (the core's h_envChan->qmfScale).
func PSEncParametricStereoProcessing(h *ParametricStereo, samples [maxPsChannels][]int16,
	hQmfAnalysis [maxPsChannels]*FilterBank, downmixedRealQmfData, downmixedImagQmfData [][]int32,
	downsampledOutSignal []int16, sbrSynthQmf *FilterBank, qmfScale *int, sendHeader int) int {

	var psQmfScale [maxPsChannels]int
	pWorkBuffer := make([]int32, 4*64)
	qmfReal := pWorkBuffer[2*64 : 2*64+64]
	qmfImag := pWorkBuffer[3*64 : 3*64+64]
	timeIn := make([]int32, 64) // one slot of widened INT_PCM input.

	for psCh := 0; psCh < maxPsChannels; psCh++ {
		noCol := hQmfAnalysis[psCh].noCol
		nch := hQmfAnalysis[psCh].noChannels
		for i := 0; i < noCol; i++ {
			// qmfAnalysisFilteringSlot takes INT_PCM (int16); SAMPLE_BITS == 16,
			// so left-align each sample into the int32 FIXP_QAS word (<< 16),
			// exactly as analysisFilterFromPCM does for the v1 frame path.
			for j := 0; j < nch; j++ {
				timeIn[j] = int32(samples[psCh][i*nch+j]) << 16
			}
			AnalysisFilteringSlot(hQmfAnalysis[psCh], qmfReal, qmfImag,
				timeIn, 1, pWorkBuffer[0:2*64])

			fdkHybridAnalysisApply(&h.fdkHybAnaFilter[psCh], qmfReal, qmfImag,
				h.pHybridData[i+hybridReadOffset][psCh][0],
				h.pHybridData[i+hybridReadOffset][psCh][1])
		}
		psQmfScale[psCh] = hQmfAnalysis[psCh].outScalefactor
	}

	// find best scaling in new QMF and Hybrid data. The slice is
	// &pHybridData[HYBRID_READ_OFFSET] (the current-frame window).
	var hybWindow [hybridFramesize][maxPsChannels][2][]int32
	for s := 0; s < hybridFramesize; s++ {
		hybWindow[s] = h.pHybridData[hybridReadOffset+s]
	}
	psFindBestScaling(h, hybWindow, h.dynBandScale[:], h.maxBandValue[:], &h.dmxScale)

	// extract the ps parameters (over the FULL ring &pHybridData[0]).
	var hybFull [hybridFramesize][maxPsChannels][2][]int32
	for s := 0; s < hybridFramesize; s++ {
		hybFull[s] = h.pHybridData[s]
	}
	if e := extractPSParameters(h, sendHeader, hybFull); e != psencOK {
		return e
	}

	// save hybrid data for next frame (ps_main.cpp:510-527).
	for i := 0; i < hybridReadOffset; i++ {
		copy(h.pHybridData[i][0][0][:maxHybridBands], h.pHybridData[h.noQmfSlots+i][0][0][:maxHybridBands])
		copy(h.pHybridData[i][0][1][:maxHybridBands], h.pHybridData[h.noQmfSlots+i][0][1][:maxHybridBands])
		copy(h.pHybridData[i][1][0][:maxHybridBands], h.pHybridData[h.noQmfSlots+i][1][0][:maxHybridBands])
		copy(h.pHybridData[i][1][1][:maxHybridBands], h.pHybridData[h.noQmfSlots+i][1][1][:maxHybridBands])
	}

	// downmix and hybrid synthesis (over the current-frame window).
	downmixPSQmfData(h, sbrSynthQmf, downmixedRealQmfData, downmixedImagQmfData,
		downsampledOutSignal, hybWindow, h.noQmfSlots, psQmfScale, qmfScale)
	return psencOK
}

// psFindBestScaling is the 1:1 port of psFindBestScaling (ps_main.cpp:544-606):
// group-wise scan of the hybrid data to derive per-PS-band dynamic scales
// (dynBandScale) and the overall downmix scale (dmxScale). hybridData is the
// current-frame window (&pHybridData[HYBRID_READ_OFFSET]).
func psFindBestScaling(h *ParametricStereo, hybridData [hybridFramesize][maxPsChannels][2][]int32,
	dynBandScale []uint8, maxBandValue []int32, dmxScale *int) {

	hPsEncode := h.hPsEncode
	frameSize := h.noQmfSlots
	psBands := hPsEncode.psEncMode
	nIidGroups := hPsEncode.nQmfIidGroups + hPsEncode.nSubQmfIidGroups

	var maxVal [2][psMaxBandsE]int32
	maxValue := int32(0)

	for group := 0; group < nIidGroups; group++ {
		bin := int(hPsEncode.subband2parameterIndex[group])
		if hPsEncode.psEncMode == psBandsCoarse {
			bin >>= 1
		}

		for col := 0; col < frameSize; col++ {
			section := 0
			if col >= frameSize-hybridReadOffset {
				section = 1
			}
			tmp := maxVal[section][bin]
			for i := int(hPsEncode.iidGroupBorders[group]); i < int(hPsEncode.iidGroupBorders[group+1]); i++ {
				tmp = fixMaxI32(tmp, fAbs(hybridData[col][0][0][i]))
				tmp = fixMaxI32(tmp, fAbs(hybridData[col][0][1][i]))
				tmp = fixMaxI32(tmp, fAbs(hybridData[col][1][0][i]))
				tmp = fixMaxI32(tmp, fAbs(hybridData[col][1][1][i]))
			}
			maxVal[section][bin] = tmp
		}
	}

	for band := 0; band < psBands; band++ {
		dynBandScale[band] = uint8(nativeaac.CountLeadingBits(fixMaxI32(maxVal[0][band], maxBandValue[band])))
		maxValue = fixMaxI32(maxValue, fixMaxI32(maxVal[0][band], maxVal[1][band]))
		maxBandValue[band] = fixMaxI32(maxVal[0][band], maxVal[1][band])
	}

	*dmxScale = fixMinI(dfractBits, nativeaac.CountLeadingBits(maxValue))
}
