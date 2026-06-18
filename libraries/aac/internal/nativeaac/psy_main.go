// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Pure-Go 1:1 port of the psychoacoustic DRIVER FDKaacEnc_psyMain
// (libAACenc/src/psy_main.cpp:407-1298). It threads the already-ported,
// parity-verified leaf kernels — block switching (block_switch.go), the forward
// MDCT (enc_transform.go), the SFB energy/headroom kernels (band_nrg.go),
// tonality (psy_tonality.go), TNS detect/sync/encode (enc_tns_*.go), spreading
// (psy_spreading.go), pre-echo control (psy_preecho.go), short-block grouping
// (psy_grpdata.go), PNS detect/pre/post/code (aacenc_pns.go), intensity
// (intensity.go) and M/S stereo (enc_ms_stereo.go) — into a populated PSY_OUT.
//
// Scope: AAC-LC only (filterbank FB_LC). The FB_LD / FB_ELD branches
// (Transform_Real_Eld, blockSwitchingOffset == nTimeSamples, the ELD blending)
// are EXCLUDED — this driver takes the FB_LC path FDKaacEnc_psyMain runs for
// AOT_AAC_LC. Every value is a fixed-point INT/FIXP_DBL (int32) in the integer
// domain, so the driver is bit-exact and asserts EXACT integer equality against
// the genuine vendored FDKaacEnc_psyMain.
//
// 1:1 fidelity: the channel/window loop structure, the headroom-rescaling
// block (minSpecShift / nrgShift / finalShift), the TNS scaling, the threshold
// spreading / PCM-quant / pre-echo chain, the short-block grouping + LdData
// vectorisation, the PNS/intensity/MS stereo orchestration and the output
// build all match the C exactly. C counterparts are cited as file:line.
//
// C-pointer-aliasing note: in C, psyData[ch]->mdctSpectrum points at
// psyOutChannel[ch]->mdctSpectrum, and psyOutChannel[ch]->sfb*LdData /
// sfbEnergy / sfbSpreadEnergy point into QC_OUT_CHANNEL memory (assigned by
// FDKaacEnc_EncodeFrame). The Go port keeps PsyData's own MdctSpectrum slice and
// PsyOutChannel's own arrays; psyMain writes them and FDKaacEnc_EncodeFrame
// copies PsyOutChannel -> QcOutChannel to reproduce the aliasing exactly.

package nativeaac

// FADE_OUT_LEN (psy_main.cpp:123) and the fadeOutFactor[] gibbs-blending table
// (psy_main.cpp:124-125). Used only on the FB_LD/FB_ELD path (lowpassLine >=
// FADE_OUT_LEN && filterbank != FB_LC), which AAC-LC never takes; retained 1:1.
const fadeOutLen = 6

var fadeOutFactor = [fadeOutLen]int32{
	1840644096, 1533870080, 1227096064, 920322048, 613548032, 306774016,
}

// cRatio is C_RATIO (psy_configuration.h:117-118): 0x02940a10,
// FL2FXCONST_DBL(0.001258925f) << THR_SHIFTBITS == pow(10, -2.9). The
// threshold = energy * C_RATIO product in the headroom-correction loop.
const cRatio int32 = 0x02940a10

// thrShiftBits is THR_SHIFTBITS (psy_configuration.h:113) == 4.
const thrShiftBits = 4

// PCM_QUANT_THR_SCALE (psy_configuration.h:114) == 16 is the existing
// pcmQuantThrScale (psy_preecho.go); reused here.

// maxShiftDBL is MAX_SHIFT_DBL (common_fix.h:141) == DFRACT_BITS-1 == 31.
const maxShiftDBL = dfractBits - 1

// TRANS_FAC (psy_const.h:109) == 8 (long/short window ratio) is the existing
// encTransFac (enc_tns_detect.go); reused throughout this file.

// psyMain is the 1:1 port of FDKaacEnc_psyMain (psy_main.cpp:407-1298). It runs
// the full psychoacoustic analysis for one element (1 or 2 channels) and fills
// psyOutElement. It assumes the FB_LC filterbank (AAC-LC); other filterbanks
// return AacEncUnsupportedFilterbank.
func psyMain(channels int, psyElement *PsyElement, psyDynamic *PsyDynamic,
	psyConf []PsyConfiguration, psyOutElement *PsyOutElement,
	pInput []int16, inputBufSize uint, chIdx []int, totalChannels int) EncoderError {

	const commonWindow = 1
	var maxSfbPerGroup [2]int
	var mdctSpectrumE int

	hPsyConfLong := &psyConf[0]
	hPsyConfShort := &psyConf[1]
	psyOutChannel := psyOutElement.PsyOutChannel[:]
	var sfbTonality [2][maxSfbLong]int16

	psyStatic := psyElement.PsyStatic[:]

	psyData := make([]*PsyData, channels)
	tnsData := make([]*TNSData, channels)
	pnsData := make([]*PNSData, channels)

	// psyMain's TNS detect/sync/encode work on the encoder-side TNSInfo (INT
	// coefs); the bitstream-side psyOutChannel.TnsInfo is the int16 TnsInfo. The
	// driver runs TNS into these locals then converts into psyOutChannel.TnsInfo
	// in the output-build step (mirroring C, where both are the one TNS_INFO).
	var tnsInfo [2]TNSInfo

	zeroSpec := true // means all spectral lines are zero

	var hThisPsyConf [2]*PsyConfiguration
	var windowLength [2]int
	var nWindows [2]int
	var wOffset int

	var maxSfb [2]int
	// Per-channel sfbOffset as []int for the band_nrg/tonality/grpdata leaves
	// (which take INT* offsets); intensity/pns take the int32 SfbOffset[:].
	var sfbOffInt [2][]int

	// number of incoming time samples to be processed
	nTimeSamples := psyConf[0].GranuleLength

	var blockSwitchingOffset int
	switch hPsyConfLong.Filterbank {
	case fbLC:
		blockSwitchingOffset = nTimeSamples + (9 * nTimeSamples / (2 * encTransFac))
	case fbLD, fbELD:
		blockSwitchingOffset = nTimeSamples
	default:
		return AacEncUnsupportedFilterbank
	}

	for ch := 0; ch < channels; ch++ {
		psyData[ch] = &psyDynamic.PsyData[ch]
		tnsData[ch] = &psyDynamic.TnsData[ch]
		pnsData[ch] = &psyDynamic.PnsData[ch]

		// psyData[ch]->mdctSpectrum = psyOutChannel[ch]->mdctSpectrum
		psyData[ch].MdctSpectrum = psyOutChannel[ch].MdctSpectrum[:]
	}

	// block switching (FB_LC / FB_LD path; FB_ELD excluded)
	if hPsyConfLong.Filterbank != fbELD {
		for ch := 0; ch < channels; ch++ {
			// copy input data and use for block switching
			var pTimeSignal [1024]int16
			copy(pTimeSignal[:nTimeSamples],
				pInput[chIdx[ch]*int(inputBufSize):chIdx[ch]*int(inputBufSize)+nTimeSamples])

			BlockSwitching(&psyStatic[ch].BlockSwitchingControl, nTimeSamples,
				psyStatic[ch].IsLFE, pTimeSignal[:])

			// fill up internal input buffer, to 2xframelength samples
			copy(psyStatic[ch].PsyInputBuffer[blockSwitchingOffset:blockSwitchingOffset+(2*nTimeSamples-blockSwitchingOffset)],
				pTimeSignal[:2*nTimeSamples-blockSwitchingOffset])
		}

		// synch left and right block type
		var right *BlockSwitchingControl
		if channels > 1 {
			right = &psyStatic[1].BlockSwitchingControl
		}
		if SyncBlockSwitching(&psyStatic[0].BlockSwitchingControl, right, channels, commonWindow) != 0 {
			return AacEncUnsupportedAOT // mixed up LC and LD
		}
	} else {
		for ch := 0; ch < channels; ch++ {
			copy(psyStatic[ch].PsyInputBuffer[blockSwitchingOffset:blockSwitchingOffset+nTimeSamples],
				pInput[chIdx[ch]*int(inputBufSize):chIdx[ch]*int(inputBufSize)+nTimeSamples])
		}
	}

	isShortWindow := [2]bool{}
	for ch := 0; ch < channels; ch++ {
		isShortWindow[ch] = psyStatic[ch].BlockSwitchingControl.LastWindowSequence == shortWindowEnc
	}

	// set parameters according to window length
	for ch := 0; ch < channels; ch++ {
		if isShortWindow[ch] {
			hThisPsyConf[ch] = hPsyConfShort
			windowLength[ch] = psyConf[0].GranuleLength / encTransFac
			nWindows[ch] = encTransFac
			maxSfb[ch] = maxSfbShort
		} else {
			hThisPsyConf[ch] = hPsyConfLong
			windowLength[ch] = psyConf[0].GranuleLength
			nWindows[ch] = 1
			maxSfb[ch] = maxGroupedSfb
		}
		sfbOffInt[ch] = int32SliceToInt(hThisPsyConf[ch].SfbOffset[:])
	}

	// Transform and get mdctScaling for all channels and windows.
	for ch := 0; ch < channels; ch++ {
		// update number of active bands
		if psyStatic[ch].IsLFE != 0 {
			psyData[ch].SfbActive = hThisPsyConf[ch].SfbActiveLFE
			psyData[ch].LowpassLine = hThisPsyConf[ch].LowpassLineLFE
		} else {
			psyData[ch].SfbActive = hThisPsyConf[ch].SfbActive
			psyData[ch].LowpassLine = hThisPsyConf[ch].LowpassLine
		}

		// FB_ELD path (Transform_Real_Eld) excluded — AAC-LC uses FB_LC.
		// transformReal (enc_transform.go) is the FB_LC FDKaacEnc_Transform_Real;
		// it carries no explicit filterbank arg (AAC-LC is its only target).
		if transformReal(psyStatic[ch].PsyInputBuffer[:], psyData[ch].MdctSpectrum,
			psyStatic[ch].BlockSwitchingControl.LastWindowSequence,
			psyStatic[ch].BlockSwitchingControl.WindowShape,
			&psyStatic[ch].BlockSwitchingControl.LastWindowShape,
			&psyStatic[ch].MdctPers, nTimeSamples, &mdctSpectrumE) != 0 {
			return AacEncUnsupportedFilterbank
		}

		for w := 0; w < nWindows[ch]; w++ {
			wOffset = w * windowLength[ch]

			// Low pass / highest sfb
			for i := psyData[ch].LowpassLine + wOffset; i < windowLength[ch]+wOffset; i++ {
				psyData[ch].MdctSpectrum[i] = 0
			}

			if (hPsyConfLong.Filterbank != fbLC) && (psyData[ch].LowpassLine >= fadeOutLen) {
				// Do blending to reduce gibbs artifacts
				for i := 0; i < fadeOutLen; i++ {
					idx := psyData[ch].LowpassLine + wOffset - fadeOutLen + i
					psyData[ch].MdctSpectrum[idx] = fMult(psyData[ch].MdctSpectrum[idx], fadeOutFactor[i])
				}
			}

			// Check for zero spectrum.
			for line := 0; (line < psyData[ch].LowpassLine) && zeroSpec; line++ {
				if psyData[ch].MdctSpectrum[line+wOffset] != 0 {
					zeroSpec = false
					break
				}
			}
		} // w loop

		psyData[ch].MdctScale = mdctSpectrumE

		// rotate internal time samples
		copy(psyStatic[ch].PsyInputBuffer[:nTimeSamples],
			psyStatic[ch].PsyInputBuffer[nTimeSamples:2*nTimeSamples])

		// ... and get remaining samples from input buffer
		src := (2*nTimeSamples - blockSwitchingOffset) + chIdx[ch]*int(inputBufSize)
		copy(psyStatic[ch].PsyInputBuffer[nTimeSamples:nTimeSamples+(blockSwitchingOffset-nTimeSamples)],
			pInput[src:src+(blockSwitchingOffset-nTimeSamples)])
	} // ch

	// Do some rescaling to get maximum possible accuracy for energies
	if !zeroSpec {
		minSpecShift := maxShiftDBL
		nrgShift := maxShiftDBL
		finalShift := maxShiftDBL
		var currNrg int32
		var maxNrg int32

		for ch := 0; ch < channels; ch++ {
			for w := 0; w < nWindows[ch]; w++ {
				wOffset = w * windowLength[ch]
				fdkaacEncCalcSfbMaxScaleSpec(
					psyData[ch].MdctSpectrum[wOffset:], sfbOffInt[ch],
					psyData[ch].SfbMaxScaleSpec.Slice(w*maxSfb[ch]), psyData[ch].SfbActive)

				for sfb := 0; sfb < psyData[ch].SfbActive; sfb++ {
					minSpecShift = fixMin(minSpecShift, psyData[ch].SfbMaxScaleSpec.Slice(w * maxSfb[ch])[sfb])
				}
			}
		}

		for ch := 0; ch < channels; ch++ {
			for w := 0; w < nWindows[ch]; w++ {
				wOffset = w * windowLength[ch]
				currNrg = fdkaacEncCheckBandEnergyOptim(
					psyData[ch].MdctSpectrum[wOffset:],
					psyData[ch].SfbEnergy.Slice(w*maxSfb[ch]),
					psyData[ch].SfbEnergyLdData.Slice(w*maxSfb[ch]),
					psyData[ch].SfbMaxScaleSpec.Slice(w*maxSfb[ch]),
					sfbOffInt[ch], psyData[ch].SfbActive, minSpecShift-4)

				maxNrg = fMax(maxNrg, currNrg)
			}
		}

		if maxNrg != 0 {
			nrgShift = (int(fNorm(maxNrg)) >> 1) + (minSpecShift - 4)
		}

		// For short windows 1 additional bit headroom is necessary.
		if isShortWindow[0] {
			nrgShift--
		}

		// both spectrum and energies mustn't overflow
		finalShift = fixMin(minSpecShift, nrgShift)

		// do not shift more than 3 bits more to the left than the unscaled signal
		if finalShift > psyData[0].MdctScale+3 {
			finalShift = psyData[0].MdctScale + 3
		}

		// correct sfbEnergy and sfbEnergyLdData with new finalShift
		ldShift := int32(finalShift) * fl2fxconstDBL(2.0/64)
		for ch := 0; ch < channels; ch++ {
			maxSfbCh := maxSfb[ch]
			wMaxSfbCh := 0
			for w := 0; w < nWindows[ch]; w++ {
				eng := psyData[ch].SfbEnergy.Slice(wMaxSfbCh)
				thr := psyData[ch].SfbThreshold.Slice(wMaxSfbCh)
				engLd := psyData[ch].SfbEnergyLdData.Slice(wMaxSfbCh)
				mss := psyData[ch].SfbMaxScaleSpec.Slice(wMaxSfbCh)
				for sfb := 0; sfb < psyData[ch].SfbActive; sfb++ {
					scale := fixMax(0, mss[sfb]-4)
					scale = fixMin((scale-finalShift)<<1, dfractBits-1)
					if scale >= 0 {
						eng[sfb] >>= uint(scale)
					} else {
						eng[sfb] <<= uint(-scale)
					}
					thr[sfb] = fMult(eng[sfb], cRatio)
					engLd[sfb] += ldShift
				}
				wMaxSfbCh += maxSfbCh
			}
		}

		if finalShift != 0 {
			for ch := 0; ch < channels; ch++ {
				wLen := windowLength[ch]
				lowpassLine := psyData[ch].LowpassLine
				wOffset = 0
				mdctSpectrum := psyData[ch].MdctSpectrum
				for w := 0; w < nWindows[ch]; w++ {
					spectrum := mdctSpectrum[wOffset:]
					for line := 0; line < lowpassLine; line++ {
						spectrum[line] <<= uint(finalShift)
					}
					wOffset += wLen

					// update sfbMaxScaleSpec
					mss := psyData[ch].SfbMaxScaleSpec.Slice(w * maxSfb[ch])
					for sfb := 0; sfb < psyData[ch].SfbActive; sfb++ {
						mss[sfb] -= finalShift
					}
				}
				psyData[ch].MdctScale -= finalShift
			}
		}
	} else {
		// all spectral lines are zero
		for ch := 0; ch < channels; ch++ {
			psyData[ch].MdctScale = 0
			for w := 0; w < nWindows[ch]; w++ {
				mss := psyData[ch].SfbMaxScaleSpec.Slice(w * maxSfb[ch])
				eng := psyData[ch].SfbEnergy.Slice(w * maxSfb[ch])
				engLd := psyData[ch].SfbEnergyLdData.Slice(w * maxSfb[ch])
				thr := psyData[ch].SfbThreshold.Slice(w * maxSfb[ch])
				for sfb := 0; sfb < psyData[ch].SfbActive; sfb++ {
					mss[sfb] = 0
					eng[sfb] = 0
					engLd[sfb] = fl2fxconstDBL(-1.0)
					thr[sfb] = 0
				}
			}
		}
	}

	// Advance psychoacoustics: Tonality and TNS
	if (channels >= 1) && (psyStatic[0].IsLFE != 0) {
		tnsData[0].LongSubBlock.TnsActive[hifilt] = 0
		tnsData[0].LongSubBlock.TnsActive[lofilt] = 0
	} else {
		for ch := 0; ch < channels; ch++ {
			if !isShortWindow[ch] {
				// tonality
				calculateFullTonality(
					psyData[ch].MdctSpectrum, psyData[ch].SfbMaxScaleSpec.Cells(),
					psyData[ch].SfbEnergyLdData.Cells(), sfbTonality[ch][:],
					psyData[ch].SfbActive, sfbOffInt[ch], hThisPsyConf[ch].PnsConf.UsePns)
			}
		}

		if hPsyConfLong.TnsConf.TnsActive != 0 || hPsyConfShort.TnsConf.TnsActive != 0 {
			var tnsActive [encTransFac]int
			var nrgScaling [2]int
			tnsSpecShift := 0

			var tnsScratch [1024]int32

			for ch := 0; ch < channels; ch++ {
				for w := 0; w < nWindows[ch]; w++ {
					wOffset = w * windowLength[ch]
					fdkaacEncTnsDetect(
						tnsData[ch], &hThisPsyConf[ch].TnsConf, &tnsInfo[ch],
						hThisPsyConf[ch].SfbCnt, psyData[ch].MdctSpectrum[wOffset:], w,
						psyStatic[ch].BlockSwitchingControl.LastWindowSequence, tnsScratch[:])
				}
			}

			if channels == 2 {
				tnsSync(tnsData[1], tnsData[0], &tnsInfo[1], &tnsInfo[0],
					psyStatic[1].BlockSwitchingControl.LastWindowSequence,
					psyStatic[0].BlockSwitchingControl.LastWindowSequence,
					&hThisPsyConf[1].TnsConf)
			}

			if channels >= 1 {
				for w := 0; w < nWindows[0]; w++ {
					if isShortWindow[0] {
						tnsActive[w] = boolToInt(
							tnsData[0].ShortSubBlock[w].TnsActive[hifilt] != 0 ||
								tnsData[0].ShortSubBlock[w].TnsActive[lofilt] != 0 ||
								tnsData[channels-1].ShortSubBlock[w].TnsActive[hifilt] != 0 ||
								tnsData[channels-1].ShortSubBlock[w].TnsActive[lofilt] != 0)
					} else {
						tnsActive[w] = boolToInt(
							tnsData[0].LongSubBlock.TnsActive[hifilt] != 0 ||
								tnsData[0].LongSubBlock.TnsActive[lofilt] != 0 ||
								tnsData[channels-1].LongSubBlock.TnsActive[hifilt] != 0 ||
								tnsData[channels-1].LongSubBlock.TnsActive[lofilt] != 0)
					}
				}
			}

			for ch := 0; ch < channels; ch++ {
				if tnsActive[0] != 0 && !isShortWindow[ch] {
					// Scale down spectrum if tns is active.
					shift := 1
					for sfb := 0; sfb < hThisPsyConf[ch].LowpassLine; sfb++ {
						psyData[ch].MdctSpectrum[sfb] = psyData[ch].MdctSpectrum[sfb] >> uint(shift)
					}
					thr := psyData[ch].SfbThreshold.Cells()
					for sfb := 0; sfb < psyData[ch].SfbActive; sfb++ {
						thr[sfb] >>= uint(2 * shift)
					}
					psyData[ch].MdctScale += shift
				}
			}

			for ch := 0; ch < channels; ch++ {
				for w := 0; w < nWindows[ch]; w++ {
					wOffset = w * windowLength[ch]
					tnsEncode(&tnsInfo[ch], tnsData[ch], hThisPsyConf[ch].SfbCnt,
						&hThisPsyConf[ch].TnsConf,
						int(hThisPsyConf[ch].SfbOffset[psyData[ch].SfbActive]),
						psyData[ch].MdctSpectrum[wOffset:], w,
						psyStatic[ch].BlockSwitchingControl.LastWindowSequence)

					if tnsActive[w] != 0 {
						fdkaacEncCalcSfbMaxScaleSpec(psyData[ch].MdctSpectrum[wOffset:],
							sfbOffInt[ch], psyData[ch].SfbMaxScaleSpec.Slice(w*maxSfb[ch]),
							psyData[ch].SfbActive)
					}
				}
			}

			for ch := 0; ch < channels; ch++ {
				for w := 0; w < nWindows[ch]; w++ {
					if tnsActive[w] != 0 {
						if isShortWindow[ch] {
							fdkaacEncCalcBandEnergyOptimShort(
								psyData[ch].MdctSpectrum[w*windowLength[ch]:],
								psyData[ch].SfbEnergy.Slice(w*maxSfb[ch]),
								psyData[ch].SfbMaxScaleSpec.Slice(w*maxSfb[ch]),
								sfbOffInt[ch], psyData[ch].SfbActive)
						} else {
							nrgScaling[ch] = fdkaacEncCalcBandEnergyOptimLong(
								psyData[ch].MdctSpectrum,
								psyData[ch].SfbEnergy.Cells(),
								psyData[ch].SfbEnergyLdData.Cells(),
								psyData[ch].SfbMaxScaleSpec.Cells(),
								sfbOffInt[ch], psyData[ch].SfbActive)
							tnsSpecShift = fixMax(tnsSpecShift, nrgScaling[ch])
						}
					}
				}
			}

			// adapt scaling to prevent nrg overflow, only for long blocks
			for ch := 0; ch < channels; ch++ {
				if tnsSpecShift != 0 && !isShortWindow[ch] {
					for line := 0; line < hThisPsyConf[ch].LowpassLine; line++ {
						psyData[ch].MdctSpectrum[line] >>= uint(tnsSpecShift)
					}
					scale := (tnsSpecShift - nrgScaling[ch]) << 1
					engLd := psyData[ch].SfbEnergyLdData.Cells()
					eng := psyData[ch].SfbEnergy.Cells()
					thr := psyData[ch].SfbThreshold.Cells()
					for sfb := 0; sfb < psyData[ch].SfbActive; sfb++ {
						engLd[sfb] -= int32(scale) * fl2fxconstDBL(1.0/ldDataScaling)
						eng[sfb] >>= uint(scale)
						thr[sfb] >>= uint(tnsSpecShift << 1)
					}
					psyData[ch].MdctScale += tnsSpecShift
				}
			}
		} else {
			// disable TNS: reset dynamic data (needed by PNS detection)
			for ch := 0; ch < channels; ch++ {
				psyDynamic.TnsData[ch] = TNSData{}
			}
		}
	} // !isLFE

	// Advance thresholds
	for ch := 0; ch < channels; ch++ {
		var headroom int
		var clipEnergy int32
		energyShift := psyData[ch].MdctScale * 2
		clipNrgShift := energyShift - thrShiftBits
		if isShortWindow[ch] {
			headroom = 6
		} else {
			headroom = 0
		}

		if clipNrgShift >= 0 {
			clipEnergy = hThisPsyConf[ch].ClipEnergy >> uint(clipNrgShift)
		} else if clipNrgShift >= -headroom {
			clipEnergy = hThisPsyConf[ch].ClipEnergy << uint(-clipNrgShift)
		} else {
			clipEnergy = maxvalDBL
		}

		for w := 0; w < nWindows[ch]; w++ {
			thr := psyData[ch].SfbThreshold.Slice(w * maxSfb[ch])
			// limit threshold to avoid clipping
			for i := 0; i < psyData[ch].SfbActive; i++ {
				thr[i] = fMin(thr[i], clipEnergy)
			}

			// spreading
			spreadingMax(psyData[ch].SfbActive,
				hThisPsyConf[ch].SfbMaskLowFactor[:], hThisPsyConf[ch].SfbMaskHighFactor[:], thr)

			// PCM quantization threshold
			energyShift += pcmQuantThrScale
			if energyShift >= 0 {
				energyShift = fixMin(dfractBits-1, energyShift)
				for i := 0; i < psyData[ch].SfbActive; i++ {
					thr[i] = fMax(thr[i]>>thrShiftBits,
						hThisPsyConf[ch].SfbPcmQuantThreshold[i]>>uint(energyShift))
				}
			} else {
				energyShift = fixMin(dfractBits-1, -energyShift)
				for i := 0; i < psyData[ch].SfbActive; i++ {
					thr[i] = fMax(thr[i]>>thrShiftBits,
						hThisPsyConf[ch].SfbPcmQuantThreshold[i]<<uint(energyShift))
				}
			}

			if psyStatic[ch].IsLFE == 0 {
				// preecho control
				if psyStatic[ch].BlockSwitchingControl.LastWindowSequence == encStopWindow {
					for i := 0; i < psyData[ch].SfbActive; i++ {
						psyStatic[ch].SfbThresholdNm1[i] = maxvalDBL
					}
					psyStatic[ch].MdctScaleNm1 = 0
					psyStatic[ch].CalcPreEcho = 0
				}

				psyStatic[ch].MdctScaleNm1 = preEchoControl(
					psyStatic[ch].SfbThresholdNm1[:], psyStatic[ch].CalcPreEcho,
					psyData[ch].SfbActive, hThisPsyConf[ch].MaxAllowedIncreaseFactor,
					hThisPsyConf[ch].MinRemainingThresholdFactor, thr,
					psyData[ch].MdctScale, psyStatic[ch].MdctScaleNm1)

				psyStatic[ch].CalcPreEcho = 1

				if psyStatic[ch].BlockSwitchingControl.LastWindowSequence == encStartWindow {
					for i := 0; i < psyData[ch].SfbActive; i++ {
						psyStatic[ch].SfbThresholdNm1[i] = maxvalDBL
					}
					psyStatic[ch].MdctScaleNm1 = 0
					psyStatic[ch].CalcPreEcho = 0
				}
			}

			// spread energy to avoid hole detection
			sprEn := psyData[ch].SfbSpreadEnergy.Slice(w * maxSfb[ch])
			copy(sprEn[:psyData[ch].SfbActive], psyData[ch].SfbEnergy.Slice(w * maxSfb[ch])[:psyData[ch].SfbActive])

			spreadingMax(psyData[ch].SfbActive,
				hThisPsyConf[ch].SfbMaskLowFactorSprEn[:], hThisPsyConf[ch].SfbMaskHighFactorSprEn[:], sprEn)
		}
	}

	// Calc bandwise energies for mid and side channel. Only if 2 channels exist.
	if channels == 2 {
		for w := 0; w < nWindows[1]; w++ {
			wOffset = w * windowLength[1]
			fdkaacEncCalcBandNrgMSOpt(
				psyData[0].MdctSpectrum[wOffset:], psyData[1].MdctSpectrum[wOffset:],
				psyData[0].SfbMaxScaleSpec.Slice(w*maxSfb[0]),
				psyData[1].SfbMaxScaleSpec.Slice(w*maxSfb[1]),
				int32SliceToInt(hThisPsyConf[1].SfbOffset[:]), psyData[0].SfbActive,
				psyData[0].SfbEnergyMS.Slice(w*maxSfb[0]),
				psyData[1].SfbEnergyMS.Slice(w*maxSfb[1]),
				boolToInt(psyStatic[1].BlockSwitchingControl.LastWindowSequence != shortWindowEnc),
				psyData[0].SfbEnergyMSLdData[:], psyData[1].SfbEnergyMSLdData[:])
		}
	}

	// group short data (maxSfb[ch] for short blocks is determined here)
	for ch := 0; ch < channels; ch++ {
		if isShortWindow[ch] {
			noSfb := psyStatic[ch].BlockSwitchingControl.NoOfGroups * hPsyConfShort.SfbCnt

			maxSfbPerGroup[ch] = groupShortData(
				psyData[ch].MdctSpectrum,
				&psyData[ch].SfbThreshold, &psyData[ch].SfbEnergy,
				&psyData[ch].SfbEnergyMS, &psyData[ch].SfbSpreadEnergy,
				hPsyConfShort.SfbCnt, psyData[ch].SfbActive,
				int32SliceToInt(hPsyConfShort.SfbOffset[:]),
				hPsyConfShort.SfbMinSnrLdData[:], psyData[ch].GroupedSfbOffset[:],
				psyOutChannel[ch].SfbMinSnrLdData[:],
				psyStatic[ch].BlockSwitchingControl.NoOfGroups,
				psyStatic[ch].BlockSwitchingControl.GroupLen[:],
				psyConf[1].GranuleLength)

			// calculate ldData arrays (short values are in .Long arrays now)
			for sfbGrp := 0; sfbGrp < noSfb; sfbGrp += hPsyConfShort.SfbCnt {
				ldDataVector(psyData[ch].SfbEnergy.Slice(sfbGrp),
					psyOutChannel[ch].SfbEnergyLdData[sfbGrp:], psyData[ch].SfbActive)
			}

			// calc sfbThrld and set Values smaller 2^-31 to 2^-33
			for sfbGrp := 0; sfbGrp < noSfb; sfbGrp += hPsyConfShort.SfbCnt {
				ldDataVector(psyData[ch].SfbThreshold.Slice(sfbGrp),
					psyOutChannel[ch].SfbThresholdLdData[sfbGrp:], psyData[ch].SfbActive)
				for sfb := 0; sfb < psyData[ch].SfbActive; sfb++ {
					psyOutChannel[ch].SfbThresholdLdData[sfbGrp+sfb] = fMax(
						psyOutChannel[ch].SfbThresholdLdData[sfbGrp+sfb], fl2fxconstDBL(-0.515625))
				}
			}

			if channels == 2 {
				for sfbGrp := 0; sfbGrp < noSfb; sfbGrp += hPsyConfShort.SfbCnt {
					ldDataVector(psyData[ch].SfbEnergyMS.Slice(sfbGrp),
						psyData[ch].SfbEnergyMSLdData[sfbGrp:], psyData[ch].SfbActive)
				}
			}

			for i := 0; i < maxGroupedSfb+1; i++ {
				psyOutChannel[ch].SfbOffsets[i] = psyData[ch].GroupedSfbOffset[i]
			}
		} else {
			// maxSfb[ch] for long blocks
			var sfb int
			for sfb = psyData[ch].SfbActive - 1; sfb >= 0; sfb-- {
				var line int
				for line = int(hPsyConfLong.SfbOffset[sfb+1]) - 1; line >= int(hPsyConfLong.SfbOffset[sfb]); line-- {
					if psyData[ch].MdctSpectrum[line] != 0 {
						break
					}
				}
				if line > int(hPsyConfLong.SfbOffset[sfb]) {
					break
				}
			}
			maxSfbPerGroup[ch] = sfb + 1
			maxSfbPerGroup[ch] = fixMax(fixMin(5, psyData[ch].SfbActive), maxSfbPerGroup[ch])

			// sfbNrgLdData copy
			copy(psyOutChannel[ch].SfbEnergyLdData[:psyData[ch].SfbActive],
				psyData[ch].SfbEnergyLdData.Cells()[:psyData[ch].SfbActive])

			// C copies (MAX_GROUPED_SFB+1)==61 INTs from hPsyConfLong->sfbOffset,
			// whose declared length is MAX_SFB+1==52 — a benign C struct over-read
			// (psy_main.cpp:1109). The over-read cells (sfbOffsets[52..60]) are
			// never consulted for long blocks (sfbCnt == sfbActive <= 51), so the
			// port copies the 52 valid source entries; downstream behaviour is
			// identical. (A byte-exact PSY_OUT oracle would model the over-read.)
			for i := 0; i < encMaxSfb+1; i++ {
				psyOutChannel[ch].SfbOffsets[i] = int(hPsyConfLong.SfbOffset[i])
			}

			// sfbMinSnrLdData modified in adjust threshold, copy necessary
			copy(psyOutChannel[ch].SfbMinSnrLdData[:psyData[ch].SfbActive],
				hPsyConfLong.SfbMinSnrLdData[:psyData[ch].SfbActive])

			// calc sfbThrld and set Values smaller 2^-31 to 2^-33
			ldDataVector(psyData[ch].SfbThreshold.Cells(),
				psyOutChannel[ch].SfbThresholdLdData[:], psyData[ch].SfbActive)
			for i := 0; i < psyData[ch].SfbActive; i++ {
				psyOutChannel[ch].SfbThresholdLdData[i] = fMax(
					psyOutChannel[ch].SfbThresholdLdData[i], fl2fxconstDBL(-0.515625))
			}
		}
	}

	// Intensity parameter initialization.
	for ch := 0; ch < channels; ch++ {
		for i := 0; i < maxGroupedSfb; i++ {
			psyOutChannel[ch].IsBook[i] = 0
			psyOutChannel[ch].IsScale[i] = 0
		}
	}

	for ch := 0; ch < channels; ch++ {
		win := 0
		if isShortWindow[ch] {
			win = 1
		}
		if psyStatic[ch].IsLFE == 0 {
			// PNS Decision
			PnsDetect(
				&psyConf[0].PnsConf, pnsData[ch],
				psyStatic[ch].BlockSwitchingControl.LastWindowSequence,
				psyData[ch].SfbActive, maxSfbPerGroup[ch],
				psyOutChannel[ch].SfbThresholdLdData[:], psyConf[win].SfbOffset[:],
				psyData[ch].MdctSpectrum, psyData[ch].SfbMaxScaleSpec.intToInt32Long(),
				sfbTonality[ch][:], tnsInfo[ch].Order[0][0],
				tnsData[ch].LongSubBlock.PredictionGain[hifilt],
				tnsData[ch].LongSubBlock.TnsActive[hifilt],
				psyOutChannel[ch].SfbEnergyLdData[:], psyOutChannel[ch].NoiseNrg[:])
		}
	}

	// stereo Processing
	if channels == 2 {
		psyOutElement.ToolsInfo.MsDigest = MsNone
		psyOutElement.CommonWindow = commonWindow
		if psyOutElement.CommonWindow != 0 {
			m := fixMax(maxSfbPerGroup[0], maxSfbPerGroup[1])
			maxSfbPerGroup[0] = m
			maxSfbPerGroup[1] = m
		}
		pnsPair := [2]*PNSData{pnsData[0], pnsData[1]}
		if psyStatic[0].BlockSwitchingControl.LastWindowSequence != shortWindowEnc {
			// PNS preprocessing depending on ms processing.
			PreProcessPnsChannelPair(
				psyData[0].SfbActive, psyData[0].SfbEnergy.Cells(), psyData[1].SfbEnergy.Cells(),
				psyOutChannel[0].SfbEnergyLdData[:], psyOutChannel[1].SfbEnergyLdData[:],
				psyData[0].SfbEnergyMS.Cells(), &psyConf[0].PnsConf, pnsData[0], pnsData[1])

			IntensityStereoProcessing(
				psyData[0].SfbEnergy.Cells(), psyData[1].SfbEnergy.Cells(),
				psyData[0].MdctSpectrum, psyData[1].MdctSpectrum,
				psyData[0].SfbThreshold.Cells(), psyData[1].SfbThreshold.Cells(),
				psyOutChannel[1].SfbThresholdLdData[:],
				psyData[0].SfbSpreadEnergy.Cells(), psyData[1].SfbSpreadEnergy.Cells(),
				psyOutChannel[0].SfbEnergyLdData[:], psyOutChannel[1].SfbEnergyLdData[:],
				&psyOutElement.ToolsInfo.MsDigest, psyOutElement.ToolsInfo.MsMask[:],
				psyConf[0].SfbCnt, psyConf[0].SfbCnt, maxSfbPerGroup[0],
				psyConf[0].SfbOffset[:],
				boolToInt(psyConf[0].AllowIS != 0 && psyOutElement.CommonWindow != 0),
				psyOutChannel[1].IsBook[:], psyOutChannel[1].IsScale[:], pnsPair)

			runMsStereo(psyData, psyOutChannel, psyOutElement,
				psyConf[0].AllowMS, psyData[0].SfbActive, psyData[0].SfbActive,
				maxSfbPerGroup[0], intSliceToInt32(psyOutChannel[0].SfbOffsets[:]))

			// PNS postprocessing
			PostProcessPnsChannelPair(
				psyData[0].SfbActive, &psyConf[0].PnsConf, pnsData[0], pnsData[1],
				psyOutElement.ToolsInfo.MsMask[:], &psyOutElement.ToolsInfo.MsDigest)
		} else {
			noGrpSfb := psyStatic[0].BlockSwitchingControl.NoOfGroups * hPsyConfShort.SfbCnt
			IntensityStereoProcessing(
				psyData[0].SfbEnergy.Cells(), psyData[1].SfbEnergy.Cells(),
				psyData[0].MdctSpectrum, psyData[1].MdctSpectrum,
				psyData[0].SfbThreshold.Cells(), psyData[1].SfbThreshold.Cells(),
				psyOutChannel[1].SfbThresholdLdData[:],
				psyData[0].SfbSpreadEnergy.Cells(), psyData[1].SfbSpreadEnergy.Cells(),
				psyOutChannel[0].SfbEnergyLdData[:], psyOutChannel[1].SfbEnergyLdData[:],
				&psyOutElement.ToolsInfo.MsDigest, psyOutElement.ToolsInfo.MsMask[:],
				noGrpSfb, psyConf[1].SfbCnt, maxSfbPerGroup[0],
				intSliceToInt32(psyData[0].GroupedSfbOffset[:]),
				boolToInt(psyConf[0].AllowIS != 0 && psyOutElement.CommonWindow != 0),
				psyOutChannel[1].IsBook[:], psyOutChannel[1].IsScale[:], pnsPair)

			// it's OK to pass the ".Long" arrays here. They contain grouped short
			// data since groupShortData(). The MS sfbOffset uses
			// psyOutChannel[0]->sfbOffsets (the grouped offsets copied above).
			runMsStereo(psyData, psyOutChannel, psyOutElement,
				psyConf[1].AllowMS, noGrpSfb, hPsyConfShort.SfbCnt, maxSfbPerGroup[0],
				intSliceToInt32(psyOutChannel[0].SfbOffsets[:]))
		}
	}

	// PNS Coding
	for ch := 0; ch < channels; ch++ {
		if psyStatic[ch].IsLFE != 0 {
			for sfb := 0; sfb < psyData[ch].SfbActive; sfb++ {
				psyOutChannel[ch].NoiseNrg[sfb] = noNoisePns
			}
		} else {
			CodePnsChannel(
				psyData[ch].SfbActive, &hThisPsyConf[ch].PnsConf, pnsData[ch].PnsFlag[:],
				psyData[ch].SfbEnergyLdData.Cells(), psyOutChannel[ch].NoiseNrg[:],
				psyOutChannel[ch].SfbThresholdLdData[:])
		}
	}

	// build output
	for ch := 0; ch < channels; ch++ {
		psyOutChannel[ch].MaxSfbPerGroup = maxSfbPerGroup[ch]
		psyOutChannel[ch].MdctScale = psyData[ch].MdctScale
		if !isShortWindow[ch] {
			psyOutChannel[ch].SfbCnt = hPsyConfLong.SfbActive
			psyOutChannel[ch].SfbPerGroup = hPsyConfLong.SfbActive
			psyOutChannel[ch].LastWindowSequence = psyStatic[ch].BlockSwitchingControl.LastWindowSequence
			psyOutChannel[ch].WindowShape = psyStatic[ch].BlockSwitchingControl.WindowShape
		} else {
			sfbCnt := psyStatic[ch].BlockSwitchingControl.NoOfGroups * hPsyConfShort.SfbCnt
			psyOutChannel[ch].SfbCnt = sfbCnt
			psyOutChannel[ch].SfbPerGroup = hPsyConfShort.SfbCnt
			psyOutChannel[ch].LastWindowSequence = shortWindowEnc
			psyOutChannel[ch].WindowShape = sineWindowEnc
		}

		// generate grouping mask
		mask := 0
		for grp := 0; grp < psyStatic[ch].BlockSwitchingControl.NoOfGroups; grp++ {
			mask <<= 1
			for j := 1; j < psyStatic[ch].BlockSwitchingControl.GroupLen[grp]; j++ {
				mask = (mask << 1) | 1
			}
		}
		psyOutChannel[ch].GroupingMask = mask

		// build interface
		copy(psyOutChannel[ch].GroupLen[:], psyStatic[ch].BlockSwitchingControl.GroupLen[:maxNoOfGroups])
		copy(psyOutChannel[ch].SfbEnergy[:maxGroupedSfb], psyData[ch].SfbEnergy.Cells()[:maxGroupedSfb])
		copy(psyOutChannel[ch].SfbSpreadEnergy[:maxGroupedSfb], psyData[ch].SfbSpreadEnergy.Cells()[:maxGroupedSfb])

		// Convert the encoder-side TNSInfo (INT coefs) into the bitstream-side
		// TnsInfo (int16 coefs) the writer reads. In C both are the one TNS_INFO;
		// the Go port's two representations alias the same small (coefRes-bounded)
		// values, so the int->int16 copy is lossless.
		copyTnsInfo(&psyOutChannel[ch].TnsInfo, &tnsInfo[ch])
	}

	return AacEncOK
}

// sineWindowEnc is SINE_WINDOW (psy_const.h:128) == 0, the window shape psyMain
// stamps on short blocks. The existing sineWindowShapeEnc (enc_transform.go) is
// the same value; a local alias keeps this file's citation self-contained.
const sineWindowEnc = 0

// int32SliceToInt converts an INT-valued int32 offset table to the []int the
// band_nrg / tonality / grp_data leaf kernels take (their FIXP-agnostic INT*
// offset arguments).
func int32SliceToInt(s []int32) []int {
	out := make([]int, len(s))
	for i, v := range s {
		out[i] = int(v)
	}
	return out
}

// intToInt32Long views the sfbGroupedInt union's Long[] storage as a fresh
// []int32 for PnsDetect's sfbMaxScaleSpec argument (PnsDetect reads it as
// FIXP_DBL* in C, where the INT and FIXP_DBL share the union footprint; PNS only
// reads, never writes, so a copy preserves the values bit-for-bit).
func (s *sfbGroupedInt) intToInt32Long() []int32 {
	out := make([]int32, len(s.cells))
	for i, v := range s.cells {
		out[i] = int32(v)
	}
	return out
}

// copyTnsInfo copies the encoder-side TNSInfo (INT coefs) into the bitstream
// TnsInfo (int16 coefs). Mirrors C, where psyOutChannel->tnsInfo is the single
// TNS_INFO the encode chain fills and the writer reads.
func copyTnsInfo(dst *TnsInfo, src *TNSInfo) {
	for w := 0; w < encTransFac; w++ {
		dst.NumOfFilters[w] = src.NumOfFilters[w]
		dst.CoefRes[w] = src.CoefRes[w]
		for f := 0; f < maxNumOfFilters; f++ {
			dst.Length[w][f] = src.Length[w][f]
			dst.Order[w][f] = src.Order[w][f]
			dst.Direction[w][f] = src.Direction[w][f]
			dst.CoefCompress[w][f] = src.CoefCompress[w][f]
			for k := 0; k < tnsMaxOrder; k++ {
				dst.Coef[w][f][k] = int16(src.Coef[w][f][k])
			}
		}
	}
}

// runMsStereo is the 1:1 port of the FDKaacEnc_MsStereoProcessing call in
// FDKaacEnc_psyMain (psy_main.cpp:1190-1194 / 1218-1225). It builds the
// MS_STEREO_DATA aliasing the per-channel psy arrays, bridges the int<->int32
// isBook/msMask representation (psyOutChannel.IsBook / toolsInfo.MsMask are []int
// in the Go port while MsStereoProcessing takes []int32 and mutates in place),
// runs the kernel and folds the returned msDigest into toolsInfo. The C
// MsStereoProcessing OR-accumulates msDigest via the msMask it just set; the Go
// kernel returns that digest, which we OR into toolsInfo.msDigest (the C kernel
// itself writes *msDigest, so a plain assign of the combined value matches —
// MsStereoProcessing only ever raises the digest above the IS-set value).
func runMsStereo(psyData []*PsyData, psyOutChannel []*PsyOutChannel,
	psyOutElement *PsyOutElement, allowMS, sfbCnt, sfbPerGroup, maxSfbPerGroup int,
	sfbOffset []int32) {

	d := MsStereoData{
		SfbEnergyLeft:       psyData[0].SfbEnergy.Cells(),
		SfbEnergyRight:      psyData[1].SfbEnergy.Cells(),
		SfbEnergyMid:        psyData[0].SfbEnergyMS.Cells(),
		SfbEnergySide:       psyData[1].SfbEnergyMS.Cells(),
		SfbThresholdLeft:    psyData[0].SfbThreshold.Cells(),
		SfbThresholdRight:   psyData[1].SfbThreshold.Cells(),
		SfbSpreadEnLeft:     psyData[0].SfbSpreadEnergy.Cells(),
		SfbSpreadEnRight:    psyData[1].SfbSpreadEnergy.Cells(),
		SfbEnergyLeftLd:     psyOutChannel[0].SfbEnergyLdData[:],
		SfbEnergyRightLd:    psyOutChannel[1].SfbEnergyLdData[:],
		SfbEnergyMidLd:      psyData[0].SfbEnergyMSLdData[:],
		SfbEnergySideLd:     psyData[1].SfbEnergyMSLdData[:],
		SfbThresholdLeftLd:  psyOutChannel[0].SfbThresholdLdData[:],
		SfbThresholdRightLd: psyOutChannel[1].SfbThresholdLdData[:],
		MdctSpectrumLeft:    psyData[0].MdctSpectrum,
		MdctSpectrumRight:   psyData[1].MdctSpectrum,
	}

	isBook := intSliceToInt32(psyOutChannel[1].IsBook[:])
	msMask := intSliceToInt32(psyOutElement.ToolsInfo.MsMask[:])

	psyOutElement.ToolsInfo.MsDigest = MsStereoProcessing(&d, isBook, msMask,
		allowMS, sfbCnt, sfbPerGroup, maxSfbPerGroup, sfbOffset)

	// write the int32 msMask back into the []int toolsInfo.msMask
	for i := range msMask {
		psyOutElement.ToolsInfo.MsMask[i] = int(msMask[i])
	}
}

// intSliceToInt32 returns an []int32 copy of an []int slice.
func intSliceToInt32(s []int) []int32 {
	out := make([]int32, len(s))
	for i, v := range s {
		out[i] = int32(v)
	}
	return out
}
