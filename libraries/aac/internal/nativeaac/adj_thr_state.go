// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Allocation / init / top-level entry of the AAC encoder threshold-adjustment
// DRIVER tier, ported 1:1 from the vendored FDK-AAC reference
// libAACenc/src/adj_thr.cpp. FDKaacEnc_AdjThrNew allocates the ADJ_THR_STATE;
// FDKaacEnc_AdjThrInit seeds the per-bitrate bitres-control parameters, the per-
// element pe min/max, minSnr-adaptation params, avoid-hole params and the
// bits2PeFactor; FDKaacEnc_AdjustThresholds is the top entry that drives the CBR
// per-element / inter-element pe-dependent threshold adaptation and finally
// un-weights the thresholds.
//
// CBR/AAC-LC path. The VBR else-branch of AdjustThresholds (FDKaacEnc_AdaptThresholdsVBR)
// is excluded by design; only the CBRbitrateMode == TRUE path is ported, plus the
// shared final threshold un-weighting loop. The reduceThresholdsVBR-only state
// (chaosMeasureOld, vbrQualFactor) is initialised for struct fidelity.
//
// Pure fixed-point: every value is an int32 FIXP_DBL Q-format / INT with carried
// block exponents — bit-identical to the C, no float, no transcendental. fMultI/
// fMultNorm are the already-verified leaf kernels.

// AACENC_BIT_DISTRIBUTION_MODE (aacenc.h). Only the two CBR distribution modes are
// modelled.
const (
	aacencBdModeInterElement = 0 // AACENC_BD_MODE_INTER_ELEMENT
	aacencBdModeIntraElement = 1 // AACENC_BD_MODE_INTRA_ELEMENT
)

// adjThrNew is the 1:1 port of FDKaacEnc_AdjThrNew (adj_thr.cpp:2437-2457):
// allocate ADJ_THR_STATE and its per-element ATS_ELEMENT slots. In Go the state is
// a struct value the caller owns; this mirrors the C allocator by zero-allocating
// the element pointers. Returns err==0 always (no allocation failure path).
func adjThrNew(hAdjThr *adjThrState, nElements int) int {
	for i := 0; i < nElements; i++ {
		hAdjThr.adjThrStateElem[i] = new(atsElement)
	}
	return 0
}

// adjThrInit is the 1:1 port of FDKaacEnc_AdjThrInit (adj_thr.cpp:2463-2598):
// initialize ADJ_THR_STATE. CBR branch only — the reduceThresholdsVBR state
// (chaosMeasureOld/vbrQualFactor) is set for fidelity but unused on the CBR path.
func adjThrInit(hAdjThr *adjThrState, meanPe, invQuant int, channelMapping *ChannelMapping,
	sampleRate, totalBitrate, isLowDelay int, bitResMode AacencBitresMode,
	dZoneQuantEnable, bitDistributionMode int, vbrQualFactor int32) {

	// C: POINT8 = FL2FXCONST_DBL(0.8f), POINT6 = FL2FXCONST_DBL(0.6f) — the
	// f-suffixed (float32) literals round to single precision before the Q31
	// scale, so use fl2fxconstDBLf to match the genuine fixed-point constants.
	point8 := fl2fxconstDBLf(0.8)
	point6 := fl2fxconstDBLf(0.6)

	if bitDistributionMode == 1 {
		hAdjThr.bitDistributionMode = aacencBdModeIntraElement
	} else {
		hAdjThr.bitDistributionMode = aacencBdModeInterElement
	}

	// Max number of iterations in second guess
	if isLowDelay != 0 || channelMapping.NElements > 1 {
		hAdjThr.maxIter2ndGuess = 3
	} else {
		hAdjThr.maxIter2ndGuess = 1
	}

	// common for all elements: parameters for bitres control (raw FIXP_DBL hex)
	hAdjThr.bresParamLong.clipSaveLow = int32(0x1999999a)   // FL2FXCONST_DBL(0.2f)
	hAdjThr.bresParamLong.clipSaveHigh = int32(0x7999999a)  // FL2FXCONST_DBL(0.95f)
	hAdjThr.bresParamLong.minBitSave = int32(-0x06666666)   // 0xf999999a FL2FXCONST_DBL(-0.05f)
	hAdjThr.bresParamLong.maxBitSave = int32(0x26666666)    // FL2FXCONST_DBL(0.3f)
	hAdjThr.bresParamLong.clipSpendLow = int32(0x1999999a)  // FL2FXCONST_DBL(0.2f)
	hAdjThr.bresParamLong.clipSpendHigh = int32(0x7999999a) // FL2FXCONST_DBL(0.95f)
	hAdjThr.bresParamLong.minBitSpend = int32(-0x0ccccccd)  // 0xf3333333 FL2FXCONST_DBL(-0.10f)
	hAdjThr.bresParamLong.maxBitSpend = int32(0x33333333)   // FL2FXCONST_DBL(0.4f)

	hAdjThr.bresParamShort.clipSaveLow = int32(0x199999a0)   // FL2FXCONST_DBL(0.2f)
	hAdjThr.bresParamShort.clipSaveHigh = int32(0x5fffffff)  // FL2FXCONST_DBL(0.75f)
	hAdjThr.bresParamShort.minBitSave = int32(0x00000000)    // FL2FXCONST_DBL(0.0f)
	hAdjThr.bresParamShort.maxBitSave = int32(0x199999a0)    // FL2FXCONST_DBL(0.2f)
	hAdjThr.bresParamShort.clipSpendLow = int32(0x199999a0)  // FL2FXCONST_DBL(0.2f)
	hAdjThr.bresParamShort.clipSpendHigh = int32(0x5fffffff) // FL2FXCONST_DBL(0.75f)
	hAdjThr.bresParamShort.minBitSpend = int32(-0x06666668)  // 0xf9999998 FL2FXCONST_DBL(-0.05f)
	hAdjThr.bresParamShort.maxBitSpend = int32(0x40000000)   // FL2FXCONST_DBL(0.5f)

	// specific for each element:
	for i := 0; i < channelMapping.NElements; i++ {
		relativeBits := channelMapping.ElInfo[i].RelativeBits
		nChannelsInElement := channelMapping.ElInfo[i].NChannelsInEl
		var bitrateInElement int
		if relativeBits != maxvalDBL {
			prod, _ := fMultNorm(relativeBits, int32(totalBitrate))
			bitrateInElement = int(prod)
		} else {
			bitrateInElement = totalBitrate
		}
		chBitrate := bitrateInElement
		if nChannelsInElement != 1 {
			chBitrate >>= 1
		}

		atsElem := hAdjThr.adjThrStateElem[i]
		msaParam := &atsElem.minSnrAdaptParam

		// parameters for bitres control
		if isLowDelay != 0 {
			atsElem.peMin = int(fMultI(point8, int32(meanPe)))
			atsElem.peMax = int(fMultI(point6, int32(meanPe))) << 1
		} else {
			atsElem.peMin = int(fMultI(point8, int32(meanPe))) >> 1
			atsElem.peMax = int(fMultI(point6, int32(meanPe)))
		}

		// for use in FDKaacEnc_reduceThresholdsVBR. C: FL2FXCONST_DBL(0.3f) —
		// the float32 literal lands one ULP off the double path (644245094 vs
		// the correct 644245120).
		atsElem.chaosMeasureOld = fl2fxconstDBLf(0.3)

		// additional pe offset to correct pe2bits for low bitrates
		atsElem.peOffset = 0

		// vbr initialisation
		atsElem.vbrQualFactor = vbrQualFactor
		if chBitrate < 32000 {
			atsElem.peOffset = fixMax(50, 100-int(fMultI(int32(0x666667), int32(chBitrate))))
		}

		// avoid hole parameters
		if chBitrate >= 20000 {
			atsElem.ahParam.modifyMinSnr = 1 // TRUE
			atsElem.ahParam.startSfbL = 15
			atsElem.ahParam.startSfbS = 3
		} else {
			atsElem.ahParam.modifyMinSnr = 0 // FALSE
			atsElem.ahParam.startSfbL = 0
			atsElem.ahParam.startSfbS = 0
		}

		// minSnr adaptation. The C constants are FL2FXCONST_DBL of f-suffixed
		// (32-bit float) literals, so they round to single precision before the
		// Q31 scale — fl2fxconstDBLf reproduces that. (maxRed/redRatioFac/redOffs
		// are exact in float32 either way; startRatio = ld64(10.0f) is the one
		// that lands one ULP off under the double path.)
		msaParam.maxRed = fl2fxconstDBLf(0.00390625)        // 0.25f/64.0f
		msaParam.startRatio = fl2fxconstDBLf(0.05190512648) // ld64(10.0f)
		msaParam.redRatioFac = fl2fxconstDBLf(-0.375)       // -0.0375f * 10.0f
		msaParam.redOffs = fl2fxconstDBLf(0.021484375)      // 1.375f/64.0f

		// init pe correction
		atsElem.peCorrectionFactorM = fl2fxconstDBLf(0.5) // 1.0
		atsElem.peCorrectionFactorE = 1

		atsElem.dynBitsLast = -1
		atsElem.peLast = 0

		// init bits2PeFactor
		atsElem.bits2PeFactorM, atsElem.bits2PeFactorE = initBits2PeFactor(
			bitrateInElement, nChannelsInElement, sampleRate, isLowDelay, dZoneQuantEnable, invQuant)
	}
}

// adjustThresholds is the 1:1 port of FDKaacEnc_AdjustThresholds
// (adj_thr.cpp:2804-2900): adjust thresholds. CBRbitrateMode != 0 only — the VBR
// else-branch (FDKaacEnc_AdaptThresholdsVBR) is excluded. The final threshold
// un-weighting loop is shared and ported.
func adjustThresholds(hAdjThr *adjThrState, qcElement []*QcOutElement, qcOut *QcOut,
	psyOutElement []*PsyOutElement, cbrBitrateMode int, cm *ChannelMapping) {

	if cbrBitrateMode != 0 {
		if hAdjThr.bitDistributionMode == aacencBdModeIntraElement {
			// element-wise execution
			for i := 0; i < cm.NElements; i++ {
				elInfo := cm.ElInfo[i]
				if elInfo.ElType == IDSCE || elInfo.ElType == IDCPE || elInfo.ElType == IDLFE {
					if qcElement[i].GrantedPeCorr < int(qcElement[i].PeData.pe) {
						adaptThresholdsToPe(cm, hAdjThr.adjThrStateElem[:], qcElement, psyOutElement,
							qcElement[i].GrantedPeCorr, hAdjThr.maxIter2ndGuess,
							1, /* Process only 1 element */
							i, /* Process exactly THIS element */
						)
					}
				}
			}
		} else if hAdjThr.bitDistributionMode == aacencBdModeInterElement {
			// Use global Pe to obtain the thresholds?
			if qcOut.TotalGrantedPeCorr < qcOut.TotalNoRedPe {
				adaptThresholdsToPe(cm, hAdjThr.adjThrStateElem[:], qcElement, psyOutElement,
					qcOut.TotalGrantedPeCorr, hAdjThr.maxIter2ndGuess,
					cm.NElements, /* Process all elements */
					0)
			} else {
				for i := 0; i < cm.NElements; i++ {
					if cm.ElInfo[i].ElType == IDSCE || cm.ElInfo[i].ElType == IDCPE ||
						cm.ElInfo[i].ElType == IDLFE {
						// Element pe applies to dynamic bits of maximum element bitrate.
						maxElementPe := bits2pe2(
							(cm.ElInfo[i].NChannelsInEl*minBufsizePerEffChan)-
								qcElement[i].StaticBitsUsed-qcElement[i].ExtBitsUsed,
							hAdjThr.adjThrStateElem[i].bits2PeFactorM,
							hAdjThr.adjThrStateElem[i].bits2PeFactorE)

						if maxElementPe < int(qcElement[i].PeData.pe) {
							adaptThresholdsToPe(cm, hAdjThr.adjThrStateElem[:], qcElement, psyOutElement,
								maxElementPe, hAdjThr.maxIter2ndGuess, 1, i)
						}
					}
				}
			}
		}
	} else {
		// VBR-mode (adj_thr.cpp:2869-2883): per element, reduce thresholds by the
		// fixed quality-driven VBR reduction value.
		for i := 0; i < cm.NElements; i++ {
			elInfo := cm.ElInfo[i]
			if elInfo.ElType == IDSCE || elInfo.ElType == IDCPE || elInfo.ElType == IDLFE {
				adaptThresholdsVBR(
					qcElement[i].QcOutChannel[:], psyOutElement[i].PsyOutChannel[:],
					hAdjThr.adjThrStateElem[i], &psyOutElement[i].ToolsInfo,
					cm.ElInfo[i].NChannelsInEl)
			}
		}
	}

	// no weighting of thresholds and energies for mlout; weight thresholds back
	for i := 0; i < cm.NElements; i++ {
		for ch := 0; ch < cm.ElInfo[i].NChannelsInEl; ch++ {
			pQcOutCh := qcElement[i].QcOutChannel[ch]
			for sfbGrp := 0; sfbGrp < psyOutElement[i].PsyOutChannel[ch].SfbCnt; sfbGrp += psyOutElement[i].PsyOutChannel[ch].SfbPerGroup {
				for sfb := 0; sfb < psyOutElement[i].PsyOutChannel[ch].MaxSfbPerGroup; sfb++ {
					pQcOutCh.SfbThresholdLdData[sfb+sfbGrp] += pQcOutCh.SfbEnFacLd[sfb+sfbGrp]
				}
			}
		}
	}
}
