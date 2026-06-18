// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of the SBR-encoder envelope-extraction DRIVER half of
// libSBRenc/src/env_est.cpp — the part that orchestrates the already-ported
// leaf kernels (getEnergyFromCplxQmfData*, GetTonality, getEnvSfbEnergy,
// mhLoweringEnergy/nmhLoweringEnergy, mapPanorama, sbrNoiseFloorLevels-
// Quantisation, coupleNoiseFloor — all in enc_env_est.go), the tonality
// correction (enc_ton_corr.go), the frame grid (enc_fram_gen*.go), the envelope
// coder (enc_code_env.go) and the bitstream writer (enc_bit_sbr.go):
//
//   - calculateSbrEnvelope            (env_est.cpp:723-1010)
//   - FDKsbrEnc_extractSbrEnvelope1   (env_est.cpp:1028-1110)
//   - FDKsbrEnc_extractSbrEnvelope2   (env_est.cpp:1142-1855)
//
// HE-AAC v1 only: the SBR_SYNTAX_LOW_DELAY (ELD/LD) branches (fast transient
// detector, ld-grid amp-res decision, ELD tuning) and the PARAMETRIC_STEREO
// path are excluded (taken-false / not referenced). fdk-aac SBR is FIXED-POINT:
// EXACT integer parity.
package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// EnvChannel is the 1:1 port of struct ENV_CHANNEL (env_est.h:144-158): the
// per-channel SBR-encoder envelope state. FastTranDetector is retained for the
// LD path (HE-AAC v1 uses the regular SbrTransientDetector only).
type EnvChannel struct {
	SbrFastTransientDetector FastTranDetector
	SbrTransientDetector     SbrTransientDetector
	SbrCodeEnvelope          SbrCodeEnvelope
	SbrCodeNoiseFloor        SbrCodeEnvelope
	SbrExtractEnvelope       SbrExtractEnvelope

	SbrEnvFrame SbrEnvelopeFrame
	TonCorr     SbrTonCorrEst

	EncEnvData SbrEnvData

	QmfScale      int
	FLevelProtect uint8
}

// v_tuning for the frame-grid generator (env_est.cpp:1155): HE-AAC AAC path.
var vTuningHEAAC = []int{0, 2, 4, 0, 0, 0}

// EncCommonData is the SBR-relevant slice of struct COMMON_DATA (cmondata.h):
// the SBR payload bit-buffer plus the running header/data bit counts the
// envelope writers accumulate. (The decode/PS members are out of scope.)
type EncCommonData struct {
	SbrBitbuf   *FdkBitStream
	SbrHdrBits  int
	SbrDataBits int
	SbrFillBits int
}

// ExtractSbrEnvelope1 is the 1:1 port of FDKsbrEnc_extractSbrEnvelope1
// (env_est.cpp:1028-1110): the QMF-energy + tonality-quota + transient +
// frame-split pre-pass that fills eData->transient_info and the YBuffer. HE-AAC
// v1: the SBR_SYNTAX_LOW_DELAY (fast transient / GetTonality / global_tonality)
// branches are taken-false and excluded.
func ExtractSbrEnvelope1(hCon *SbrConfigData, hEnvChan *EnvChannel, eData *SbrEnvTempData) {
	sbrExtrEnv := &hEnvChan.SbrExtractEnvelope

	// QMF energy extraction (rescales the QMF data in place).
	yWrite := sbrExtrEnv.YBuffer[sbrExtrEnv.YBufferWriteOffset:]
	rRead := sbrExtrEnv.RBuffer[sbrExtrEnv.RBufferReadOffset:]
	iRead := sbrExtrEnv.IBuffer[sbrExtrEnv.RBufferReadOffset:]
	if sbrExtrEnv.YBufferSzShift == 0 {
		GetEnergyFromCplxQmfDataFull(yWrite, rRead, iRead, hCon.NoQmfBands,
			sbrExtrEnv.NoCols, &hEnvChan.QmfScale, &sbrExtrEnv.YBufferScale[1])
	} else {
		GetEnergyFromCplxQmfData(yWrite, rRead, iRead, hCon.NoQmfBands,
			sbrExtrEnv.NoCols, &hEnvChan.QmfScale, &sbrExtrEnv.YBufferScale[1])
	}

	// Tonality quotas (LPC).
	usb := int(hCon.FreqBandTable[hiRes][hCon.NSfb[hiRes]])
	hEnvChan.TonCorr.CalculateTonalityQuotas(sbrExtrEnv.RBuffer[:], sbrExtrEnv.IBuffer[:], usb, hEnvChan.QmfScale)

	// HE-AAC v1: the SBR_SYNTAX_LOW_DELAY GetTonality/global_tonality block is
	// excluded (taken false; global_tonality stays 0).

	// Transient detection (regular detector; the LD fast detector is excluded).
	TransientDetect(&hEnvChan.SbrTransientDetector, sbrExtrEnv.YBuffer[:], sbrExtrEnv.YBufferScale[:],
		eData.TransientInfo[:], sbrExtrEnv.YBufferWriteOffset, sbrExtrEnv.YBufferSzShift,
		sbrExtrEnv.TimeStep, hEnvChan.SbrEnvFrame.FrameMiddleSlot)

	// Frame splitter (FIXFIX 2-env decision).
	FrameSplitter(sbrExtrEnv.YBuffer[:], sbrExtrEnv.YBufferScale[:], &hEnvChan.SbrTransientDetector,
		hCon.FreqBandTable[hiRes], eData.TransientInfo[:], sbrExtrEnv.YBufferWriteOffset,
		sbrExtrEnv.YBufferSzShift, hCon.NSfb[hiRes], sbrExtrEnv.TimeStep, sbrExtrEnv.NoCols,
		&hEnvChan.EncEnvData.GlobalTonality)
}

// ExtractSbrEnvelope2 is the 1:1 port of FDKsbrEnc_extractSbrEnvelope2
// (env_est.cpp:1142-1847): the envelope/noise calculation, coding and bitstream
// assembly. HE-AAC v1: hParametricStereo is always nil and the LD branches are
// excluded. The four stereo modes (MONO / L-R / COUPLING / SWITCH_LRC) are all
// ported 1:1.
func ExtractSbrEnvelope2(hCon *SbrConfigData, sbrHeaderData *EncSbrHeaderData,
	ps *ParametricStereo, sbrBitstreamData *SbrBitstreamData, hEnvChan0, hEnvChan1 *EnvChannel,
	hCmonData *EncCommonData, eData []SbrEnvTempData, fData *SbrFrameTempData, clearOutput int) {

	hEnvChan := [maxNumChannels]*EnvChannel{hEnvChan0, hEnvChan1}
	ySzShift := hEnvChan[0].SbrExtractEnvelope.YBufferSzShift
	stereoMode := hCon.StereoMode
	nChannels := hCon.NChannels

	// Select stereo mode (coupling transient sync).
	if stereoMode == SbrCoupling {
		if eData[0].TransientInfo[1] != 0 && eData[1].TransientInfo[1] != 0 {
			mn := nativeaac.FMinI(int(eData[1].TransientInfo[0]), int(eData[0].TransientInfo[0]))
			eData[0].TransientInfo[0] = uint8(mn)
			eData[1].TransientInfo[0] = uint8(mn)
		} else if eData[0].TransientInfo[1] != 0 && eData[1].TransientInfo[1] == 0 {
			eData[1].TransientInfo[0] = eData[0].TransientInfo[0]
		} else if eData[0].TransientInfo[1] == 0 && eData[1].TransientInfo[1] != 0 {
			eData[0].TransientInfo[0] = eData[1].TransientInfo[0]
		} else {
			mx := nativeaac.FMaxI(int(eData[1].TransientInfo[0]), int(eData[0].TransientInfo[0]))
			eData[0].TransientInfo[0] = uint8(mx)
			eData[1].TransientInfo[0] = uint8(mx)
		}
	}

	// Determine time/frequency division of current granule.
	eData[0].FrameInfo = FrameInfoGenerator(&hEnvChan[0].SbrEnvFrame, eData[0].TransientInfo[:],
		sbrBitstreamData.RightBorderFIX, hEnvChan[0].SbrExtractEnvelope.PreTransientInfo[:],
		int(hEnvChan[0].EncEnvData.LdGrid), vTuningHEAAC)
	hEnvChan[0].EncEnvData.HSbrBSGrid = &hEnvChan[0].SbrEnvFrame.SbrGrid

	switch stereoMode {
	case SbrLeftRight, SbrSwitchLrc:
		eData[1].FrameInfo = FrameInfoGenerator(&hEnvChan[1].SbrEnvFrame, eData[1].TransientInfo[:],
			sbrBitstreamData.RightBorderFIX, hEnvChan[1].SbrExtractEnvelope.PreTransientInfo[:],
			int(hEnvChan[1].EncEnvData.LdGrid), vTuningHEAAC)
		hEnvChan[1].EncEnvData.HSbrBSGrid = &hEnvChan[1].SbrEnvFrame.SbrGrid

		// Compare left and right frame_infos.
		if eData[0].FrameInfo.NEnvelopes != eData[1].FrameInfo.NEnvelopes {
			stereoMode = SbrLeftRight
		} else {
			for i := 0; i < eData[0].FrameInfo.NEnvelopes+1; i++ {
				if eData[0].FrameInfo.Borders[i] != eData[1].FrameInfo.Borders[i] {
					stereoMode = SbrLeftRight
					break
				}
			}
			for i := 0; i < eData[0].FrameInfo.NEnvelopes; i++ {
				if eData[0].FrameInfo.FreqRes[i] != eData[1].FrameInfo.FreqRes[i] {
					stereoMode = SbrLeftRight
					break
				}
			}
			if eData[0].FrameInfo.ShortEnv != eData[1].FrameInfo.ShortEnv {
				stereoMode = SbrLeftRight
			}
		}
	case SbrCoupling:
		eData[1].FrameInfo = eData[0].FrameInfo
		hEnvChan[1].EncEnvData.HSbrBSGrid = &hEnvChan[0].SbrEnvFrame.SbrGrid
	case SbrMono:
		// nothing to do
	}

	for ch := 0; ch < nChannels; ch++ {
		hEC := hEnvChan[ch]
		sbrExtrEnv := &hEC.SbrExtractEnvelope
		ed := &eData[ch]

		sbrExtrEnv.PreTransientInfo[0] = ed.TransientInfo[0]
		sbrExtrEnv.PreTransientInfo[1] = ed.TransientInfo[1]
		ed.NEnvelopes = uint8(ed.FrameInfo.NEnvelopes)
		hEC.EncEnvData.NoOfEnvelopes = ed.FrameInfo.NEnvelopes
		hEC.EncEnvData.CurrentAmpResFF = AmpRes(hCon.InitAmpResFF)

		// amp_res 1.5 dB for single-envelope FIXFIX frames.
		if hEC.EncEnvData.HSbrBSGrid.FrameClass == Fixfix && ed.NEnvelopes == 1 {
			currentAmpResFF := SbrAmpRes15
			// HE-AAC v1: the LD global_tonality amp-res decision is excluded.
			if currentAmpResFF != hEC.EncEnvData.InitSbrAmpRes {
				InitSbrHuffmanTables(&hEC.EncEnvData, &hEC.SbrCodeEnvelope, &hEC.SbrCodeNoiseFloor, currentAmpResFF)
			}
		} else {
			if sbrHeaderData.SbrAmpRes != hEC.EncEnvData.InitSbrAmpRes {
				InitSbrHuffmanTables(&hEC.EncEnvData, &hEC.SbrCodeEnvelope, &hEC.SbrCodeNoiseFloor, sbrHeaderData.SbrAmpRes)
			}
		}

		if clearOutput == 0 {
			hEC.TonCorr.TonCorrParamExtr(hEC.EncEnvData.SbrInvfModeVec[:], ed.NoiseFloor[:],
				&hEC.EncEnvData.AddHarmonicFlag, hEC.EncEnvData.AddHarmonic[:],
				sbrExtrEnv.EnvelopeCompensation[:], ed.FrameInfo, ed.TransientInfo[:],
				hCon.FreqBandTable[hiRes], hCon.NSfb[hiRes], hEC.EncEnvData.SbrXposMode,
				hCon.SbrSyntaxFlags)
		}

		// Low energy in low band fix (non-LD only).
		if hEC.SbrTransientDetector.PrevLowBandEnergy < hEC.SbrTransientDetector.PrevHighBandEnergy &&
			hEC.SbrTransientDetector.PrevHighBandEnergy > fl2f(0.03) &&
			hCon.SbrSyntaxFlags&sbrSyntaxLowDelay == 0 {
			hEC.FLevelProtect = 1
			for i := 0; i < encMaxNumNoiseValues; i++ {
				hEC.EncEnvData.SbrInvfModeVec[i] = InvfHighLevel
			}
		} else {
			hEC.FLevelProtect = 0
		}

		hEC.EncEnvData.SbrInvfMode = hEC.EncEnvData.SbrInvfModeVec[0]
		hEC.EncEnvData.NoOfnoisebands = hEC.TonCorr.SbrNoiseFloorEstimate.NoNoiseBands
	}

	// Save number of scf bands per envelope.
	for ch := 0; ch < nChannels; ch++ {
		for i := 0; i < int(eData[ch].NEnvelopes); i++ {
			if eData[ch].FrameInfo.FreqRes[i] == FreqResHigh {
				hEnvChan[ch].EncEnvData.NoScfBands[i] = hCon.NSfb[FreqResHigh]
			} else {
				hEnvChan[ch].EncEnvData.NoScfBands[i] = hCon.NSfb[FreqResLow]
			}
		}
	}

	// Extract envelope of current frame.
	switch stereoMode {
	case SbrMono:
		calculateSbrEnvelope(hEnvChan[0].SbrExtractEnvelope.YBuffer[:], nil,
			hEnvChan[0].SbrExtractEnvelope.YBufferScale[:], nil, eData[0].FrameInfo,
			eData[0].SfbNrg[:], nil, hCon, hEnvChan[0], SbrMono, nil, ySzShift)
	case SbrLeftRight:
		calculateSbrEnvelope(hEnvChan[0].SbrExtractEnvelope.YBuffer[:], nil,
			hEnvChan[0].SbrExtractEnvelope.YBufferScale[:], nil, eData[0].FrameInfo,
			eData[0].SfbNrg[:], nil, hCon, hEnvChan[0], SbrMono, nil, ySzShift)
		calculateSbrEnvelope(hEnvChan[1].SbrExtractEnvelope.YBuffer[:], nil,
			hEnvChan[1].SbrExtractEnvelope.YBufferScale[:], nil, eData[1].FrameInfo,
			eData[1].SfbNrg[:], nil, hCon, hEnvChan[1], SbrMono, nil, ySzShift)
	case SbrCoupling:
		calculateSbrEnvelope(hEnvChan[0].SbrExtractEnvelope.YBuffer[:], hEnvChan[1].SbrExtractEnvelope.YBuffer[:],
			hEnvChan[0].SbrExtractEnvelope.YBufferScale[:], hEnvChan[1].SbrExtractEnvelope.YBufferScale[:],
			eData[0].FrameInfo, eData[0].SfbNrg[:], eData[1].SfbNrg[:], hCon, hEnvChan[0],
			SbrCoupling, &fData.MaxQuantError, ySzShift)
	case SbrSwitchLrc:
		calculateSbrEnvelope(hEnvChan[0].SbrExtractEnvelope.YBuffer[:], nil,
			hEnvChan[0].SbrExtractEnvelope.YBufferScale[:], nil, eData[0].FrameInfo,
			eData[0].SfbNrg[:], nil, hCon, hEnvChan[0], SbrMono, nil, ySzShift)
		calculateSbrEnvelope(hEnvChan[1].SbrExtractEnvelope.YBuffer[:], nil,
			hEnvChan[1].SbrExtractEnvelope.YBufferScale[:], nil, eData[1].FrameInfo,
			eData[1].SfbNrg[:], nil, hCon, hEnvChan[1], SbrMono, nil, ySzShift)
		calculateSbrEnvelope(hEnvChan[0].SbrExtractEnvelope.YBuffer[:], hEnvChan[1].SbrExtractEnvelope.YBuffer[:],
			hEnvChan[0].SbrExtractEnvelope.YBufferScale[:], hEnvChan[1].SbrExtractEnvelope.YBufferScale[:],
			eData[0].FrameInfo, eData[0].SfbNrgCoupling[:], eData[1].SfbNrgCoupling[:], hCon, hEnvChan[0],
			SbrCoupling, &fData.MaxQuantError, ySzShift)
	}

	// Noise floor quantisation and coding.
	switch stereoMode {
	case SbrMono:
		SbrNoiseFloorLevelsQuantisation(eData[0].NoiseLevel[:], eData[0].NoiseFloor[:], 0)
		CodeEnvelope(eData[0].NoiseLevel[:], fData.Res[:], &hEnvChan[0].SbrCodeNoiseFloor,
			hEnvChan[0].EncEnvData.DomainVecNoise[:], 0, boolToInt(eData[0].FrameInfo.NEnvelopes > 1)+1, 0,
			sbrBitstreamData.HeaderActive)
	case SbrLeftRight:
		SbrNoiseFloorLevelsQuantisation(eData[0].NoiseLevel[:], eData[0].NoiseFloor[:], 0)
		CodeEnvelope(eData[0].NoiseLevel[:], fData.Res[:], &hEnvChan[0].SbrCodeNoiseFloor,
			hEnvChan[0].EncEnvData.DomainVecNoise[:], 0, boolToInt(eData[0].FrameInfo.NEnvelopes > 1)+1, 0,
			sbrBitstreamData.HeaderActive)
		SbrNoiseFloorLevelsQuantisation(eData[1].NoiseLevel[:], eData[1].NoiseFloor[:], 0)
		CodeEnvelope(eData[1].NoiseLevel[:], fData.Res[:], &hEnvChan[1].SbrCodeNoiseFloor,
			hEnvChan[1].EncEnvData.DomainVecNoise[:], 0, boolToInt(eData[1].FrameInfo.NEnvelopes > 1)+1, 0,
			sbrBitstreamData.HeaderActive)
	case SbrCoupling:
		CoupleNoiseFloor(eData[0].NoiseFloor[:], eData[1].NoiseFloor[:])
		SbrNoiseFloorLevelsQuantisation(eData[0].NoiseLevel[:], eData[0].NoiseFloor[:], 0)
		CodeEnvelope(eData[0].NoiseLevel[:], fData.Res[:], &hEnvChan[0].SbrCodeNoiseFloor,
			hEnvChan[0].EncEnvData.DomainVecNoise[:], 1, boolToInt(eData[0].FrameInfo.NEnvelopes > 1)+1, 0,
			sbrBitstreamData.HeaderActive)
		SbrNoiseFloorLevelsQuantisation(eData[1].NoiseLevel[:], eData[1].NoiseFloor[:], 1)
		CodeEnvelope(eData[1].NoiseLevel[:], fData.Res[:], &hEnvChan[1].SbrCodeNoiseFloor,
			hEnvChan[1].EncEnvData.DomainVecNoise[:], 1, boolToInt(eData[1].FrameInfo.NEnvelopes > 1)+1, 1,
			sbrBitstreamData.HeaderActive)
	case SbrSwitchLrc:
		SbrNoiseFloorLevelsQuantisation(eData[0].NoiseLevel[:], eData[0].NoiseFloor[:], 0)
		SbrNoiseFloorLevelsQuantisation(eData[1].NoiseLevel[:], eData[1].NoiseFloor[:], 0)
		CoupleNoiseFloor(eData[0].NoiseFloor[:], eData[1].NoiseFloor[:])
		SbrNoiseFloorLevelsQuantisation(eData[0].NoiseLevelCoupling[:], eData[0].NoiseFloor[:], 0)
		SbrNoiseFloorLevelsQuantisation(eData[1].NoiseLevelCoupling[:], eData[1].NoiseFloor[:], 1)
	}

	// Encode envelope of current frame + bitstream write.
	extractSbrEnvelope2EncodeAndWrite(hCon, sbrHeaderData, ps, sbrBitstreamData, hEnvChan[:],
		hCmonData, eData, fData, stereoMode, nChannels)

	// Update buffers.
	for ch := 0; ch < nChannels; ch++ {
		yBufferLength := hEnvChan[ch].SbrExtractEnvelope.NoCols >> hEnvChan[ch].SbrExtractEnvelope.YBufferSzShift
		for i := 0; i < hEnvChan[ch].SbrExtractEnvelope.YBufferWriteOffset; i++ {
			copy(hEnvChan[ch].SbrExtractEnvelope.YBuffer[i][:64],
				hEnvChan[ch].SbrExtractEnvelope.YBuffer[i+yBufferLength][:64])
		}
		hEnvChan[ch].SbrExtractEnvelope.YBufferScale[0] = hEnvChan[ch].SbrExtractEnvelope.YBufferScale[1]
	}

	sbrHeaderData.PrevCoupling = sbrHeaderData.Coupling
}

// timeDomain is the TIME domain_vec value (sbr_def.h: TIME == 1, FREQ == 0).
const timeDomain = 1

// extractSbrEnvelope2EncodeAndWrite is the 1:1 port of the envelope-coding +
// bitstream-write tail of FDKsbrEnc_extractSbrEnvelope2 (env_est.cpp:1486-1846).
func extractSbrEnvelope2EncodeAndWrite(hCon *SbrConfigData, sbrHeaderData *EncSbrHeaderData,
	ps *ParametricStereo, sbrBitstreamData *SbrBitstreamData, hEnvChan []*EnvChannel, hCmonData *EncCommonData,
	eData []SbrEnvTempData, fData *SbrFrameTempData, stereoMode SbrStereoMode, nChannels int) {

	switch stereoMode {
	case SbrMono:
		sbrHeaderData.Coupling = 0
		hEnvChan[0].EncEnvData.Balance = 0
		CodeEnvelope(eData[0].SfbNrg[:], eData[0].FrameInfo.FreqRes[:], &hEnvChan[0].SbrCodeEnvelope,
			hEnvChan[0].EncEnvData.DomainVec[:], sbrHeaderData.Coupling, eData[0].FrameInfo.NEnvelopes, 0,
			sbrBitstreamData.HeaderActive)
	case SbrLeftRight:
		sbrHeaderData.Coupling = 0
		hEnvChan[0].EncEnvData.Balance = 0
		hEnvChan[1].EncEnvData.Balance = 0
		CodeEnvelope(eData[0].SfbNrg[:], eData[0].FrameInfo.FreqRes[:], &hEnvChan[0].SbrCodeEnvelope,
			hEnvChan[0].EncEnvData.DomainVec[:], sbrHeaderData.Coupling, eData[0].FrameInfo.NEnvelopes, 0,
			sbrBitstreamData.HeaderActive)
		CodeEnvelope(eData[1].SfbNrg[:], eData[1].FrameInfo.FreqRes[:], &hEnvChan[1].SbrCodeEnvelope,
			hEnvChan[1].EncEnvData.DomainVec[:], sbrHeaderData.Coupling, eData[1].FrameInfo.NEnvelopes, 0,
			sbrBitstreamData.HeaderActive)
	case SbrCoupling:
		sbrHeaderData.Coupling = 1
		hEnvChan[0].EncEnvData.Balance = 0
		hEnvChan[1].EncEnvData.Balance = 1
		CodeEnvelope(eData[0].SfbNrg[:], eData[0].FrameInfo.FreqRes[:], &hEnvChan[0].SbrCodeEnvelope,
			hEnvChan[0].EncEnvData.DomainVec[:], sbrHeaderData.Coupling, eData[0].FrameInfo.NEnvelopes, 0,
			sbrBitstreamData.HeaderActive)
		CodeEnvelope(eData[1].SfbNrg[:], eData[1].FrameInfo.FreqRes[:], &hEnvChan[1].SbrCodeEnvelope,
			hEnvChan[1].EncEnvData.DomainVec[:], sbrHeaderData.Coupling, eData[1].FrameInfo.NEnvelopes, 1,
			sbrBitstreamData.HeaderActive)
	case SbrSwitchLrc:
		extractSbrEnvelope2SwitchLrc(hCon, sbrHeaderData, sbrBitstreamData, hEnvChan, hCmonData,
			eData, fData, nChannels)
	}

	// dF-edge tracking.
	if stereoMode == SbrMono {
		if hEnvChan[0].EncEnvData.DomainVec[0] == timeDomain {
			hEnvChan[0].SbrCodeEnvelope.DFEdgeIncrFac++
		} else {
			hEnvChan[0].SbrCodeEnvelope.DFEdgeIncrFac = 0
		}
	} else {
		if hEnvChan[0].EncEnvData.DomainVec[0] == timeDomain || hEnvChan[1].EncEnvData.DomainVec[0] == timeDomain {
			hEnvChan[0].SbrCodeEnvelope.DFEdgeIncrFac++
			hEnvChan[1].SbrCodeEnvelope.DFEdgeIncrFac++
		} else {
			hEnvChan[0].SbrCodeEnvelope.DFEdgeIncrFac = 0
			hEnvChan[1].SbrCodeEnvelope.DFEdgeIncrFac = 0
		}
	}

	// Send the encoded data to encEnvData (ienvelope + noise levels).
	for ch := 0; ch < nChannels; ch++ {
		ed := &eData[ch]
		c := 0
		for i := 0; i < int(ed.NEnvelopes); i++ {
			for j := 0; j < hEnvChan[ch].EncEnvData.NoScfBands[i]; j++ {
				hEnvChan[ch].EncEnvData.Ienvelope[i][j] = int(ed.SfbNrg[c])
				c++
			}
		}
		for i := 0; i < encMaxNumNoiseValues; i++ {
			hEnvChan[ch].EncEnvData.SbrNoiseLevels[i] = ed.NoiseLevel[i]
		}
	}

	// Write bitstream.
	bs := hCmonData.SbrBitbuf
	if nChannels == 2 {
		_, hdr, data := WriteEnvChannelPairElement(sbrHeaderData, sbrBitstreamData,
			&hEnvChan[0].EncEnvData, &hEnvChan[1].EncEnvData, bs, hCon.SbrSyntaxFlags)
		hCmonData.SbrHdrBits = hdr
		hCmonData.SbrDataBits = data
	} else {
		// HE-AAC v2: thread the one-frame-delayed psOut[0] into the SCE writer so
		// ps_data() lands in the SBR extension (EXTENSION_ID_PS). A nil ps
		// reproduces the HE-AAC v1 path byte-for-byte (env_est.cpp:1826-1832).
		var psh *PSOutHandle
		if ps != nil {
			psh = &PSOutHandle{p: &ps.psOut[0]}
		}
		_, hdr, data := WriteEnvSingleChannelElementPS(sbrHeaderData, sbrBitstreamData,
			&hEnvChan[0].EncEnvData, psh, bs, hCon.SbrSyntaxFlags)
		hCmonData.SbrHdrBits = hdr
		hCmonData.SbrDataBits = data
	}
}

// extractSbrEnvelope2SwitchLrc is the 1:1 port of the SBR_SWITCH_LRC case
// (env_est.cpp:1532-1778): trial-codes both L/R and coupling and keeps whichever
// costs fewer payload bits.
func extractSbrEnvelope2SwitchLrc(hCon *SbrConfigData, sbrHeaderData *EncSbrHeaderData,
	sbrBitstreamData *SbrBitstreamData, hEnvChan []*EnvChannel, hCmonData *EncCommonData,
	eData []SbrEnvTempData, fData *SbrFrameTempData, nChannels int) {

	var sfbNrgPrevTemp [maxNumChannels][encMaxFreqCoeffs]int8
	var noisePrevTemp [maxNumChannels][encMaxNumNoiseValues]int8
	var upDateNrgTemp, upDateNoiseTemp [maxNumChannels]int
	var domainVecTemp, domainVecNoiseTemp [maxNumChannels][encMaxEnvelopes]int

	bs := hCmonData.SbrBitbuf

	for ch := 0; ch < nChannels; ch++ {
		copy(sfbNrgPrevTemp[ch][:], hEnvChan[ch].SbrCodeEnvelope.SfbNrgPrev[:])
		copy(noisePrevTemp[ch][:], hEnvChan[ch].SbrCodeNoiseFloor.SfbNrgPrev[:encMaxNumNoiseValues])
		upDateNrgTemp[ch] = hEnvChan[ch].SbrCodeEnvelope.UpDate
		upDateNoiseTemp[ch] = hEnvChan[ch].SbrCodeNoiseFloor.UpDate
		if sbrHeaderData.PrevCoupling != 0 {
			hEnvChan[ch].SbrCodeEnvelope.UpDate = 0
			hEnvChan[ch].SbrCodeNoiseFloor.UpDate = 0
		}
	}

	// Code ordinary Left/Right stereo.
	CodeEnvelope(eData[0].SfbNrg[:], eData[0].FrameInfo.FreqRes[:], &hEnvChan[0].SbrCodeEnvelope,
		hEnvChan[0].EncEnvData.DomainVec[:], 0, eData[0].FrameInfo.NEnvelopes, 0, sbrBitstreamData.HeaderActive)
	CodeEnvelope(eData[1].SfbNrg[:], eData[1].FrameInfo.FreqRes[:], &hEnvChan[1].SbrCodeEnvelope,
		hEnvChan[1].EncEnvData.DomainVec[:], 0, eData[1].FrameInfo.NEnvelopes, 0, sbrBitstreamData.HeaderActive)

	c := 0
	for i := 0; i < int(eData[0].NEnvelopes); i++ {
		for j := 0; j < hEnvChan[0].EncEnvData.NoScfBands[i]; j++ {
			hEnvChan[0].EncEnvData.Ienvelope[i][j] = int(eData[0].SfbNrg[c])
			hEnvChan[1].EncEnvData.Ienvelope[i][j] = int(eData[1].SfbNrg[c])
			c++
		}
	}

	CodeEnvelope(eData[0].NoiseLevel[:], fData.Res[:], &hEnvChan[0].SbrCodeNoiseFloor,
		hEnvChan[0].EncEnvData.DomainVecNoise[:], 0, boolToInt(eData[0].FrameInfo.NEnvelopes > 1)+1, 0,
		sbrBitstreamData.HeaderActive)
	for i := 0; i < encMaxNumNoiseValues; i++ {
		hEnvChan[0].EncEnvData.SbrNoiseLevels[i] = eData[0].NoiseLevel[i]
	}
	CodeEnvelope(eData[1].NoiseLevel[:], fData.Res[:], &hEnvChan[1].SbrCodeNoiseFloor,
		hEnvChan[1].EncEnvData.DomainVecNoise[:], 0, boolToInt(eData[1].FrameInfo.NEnvelopes > 1)+1, 0,
		sbrBitstreamData.HeaderActive)
	for i := 0; i < encMaxNumNoiseValues; i++ {
		hEnvChan[1].EncEnvData.SbrNoiseLevels[i] = eData[1].NoiseLevel[i]
	}

	sbrHeaderData.Coupling = 0
	hEnvChan[0].EncEnvData.Balance = 0
	hEnvChan[1].EncEnvData.Balance = 0

	payloadbitsLR := CountSbrChannelPairElement(sbrHeaderData, sbrBitstreamData,
		&hEnvChan[0].EncEnvData, &hEnvChan[1].EncEnvData, bs, hCon.SbrSyntaxFlags)

	// Swap saved/stored with current values.
	for ch := 0; ch < nChannels; ch++ {
		for i := 0; i < encMaxFreqCoeffs; i++ {
			hEnvChan[ch].SbrCodeEnvelope.SfbNrgPrev[i], sfbNrgPrevTemp[ch][i] =
				sfbNrgPrevTemp[ch][i], hEnvChan[ch].SbrCodeEnvelope.SfbNrgPrev[i]
		}
		for i := 0; i < encMaxNumNoiseValues; i++ {
			hEnvChan[ch].SbrCodeNoiseFloor.SfbNrgPrev[i], noisePrevTemp[ch][i] =
				noisePrevTemp[ch][i], hEnvChan[ch].SbrCodeNoiseFloor.SfbNrgPrev[i]
		}
		hEnvChan[ch].SbrCodeEnvelope.UpDate, upDateNrgTemp[ch] = upDateNrgTemp[ch], hEnvChan[ch].SbrCodeEnvelope.UpDate
		hEnvChan[ch].SbrCodeNoiseFloor.UpDate, upDateNoiseTemp[ch] = upDateNoiseTemp[ch], hEnvChan[ch].SbrCodeNoiseFloor.UpDate

		copy(domainVecTemp[ch][:], hEnvChan[ch].EncEnvData.DomainVec[:])
		copy(domainVecNoiseTemp[ch][:], hEnvChan[ch].EncEnvData.DomainVecNoise[:])

		if sbrHeaderData.PrevCoupling == 0 {
			hEnvChan[ch].SbrCodeEnvelope.UpDate = 0
			hEnvChan[ch].SbrCodeNoiseFloor.UpDate = 0
		}
	}

	// Coupling.
	CodeEnvelope(eData[0].SfbNrgCoupling[:], eData[0].FrameInfo.FreqRes[:], &hEnvChan[0].SbrCodeEnvelope,
		hEnvChan[0].EncEnvData.DomainVec[:], 1, eData[0].FrameInfo.NEnvelopes, 0, sbrBitstreamData.HeaderActive)
	CodeEnvelope(eData[1].SfbNrgCoupling[:], eData[1].FrameInfo.FreqRes[:], &hEnvChan[1].SbrCodeEnvelope,
		hEnvChan[1].EncEnvData.DomainVec[:], 1, eData[1].FrameInfo.NEnvelopes, 1, sbrBitstreamData.HeaderActive)

	c = 0
	for i := 0; i < int(eData[0].NEnvelopes); i++ {
		for j := 0; j < hEnvChan[0].EncEnvData.NoScfBands[i]; j++ {
			hEnvChan[0].EncEnvData.Ienvelope[i][j] = int(eData[0].SfbNrgCoupling[c])
			hEnvChan[1].EncEnvData.Ienvelope[i][j] = int(eData[1].SfbNrgCoupling[c])
			c++
		}
	}

	CodeEnvelope(eData[0].NoiseLevelCoupling[:], fData.Res[:], &hEnvChan[0].SbrCodeNoiseFloor,
		hEnvChan[0].EncEnvData.DomainVecNoise[:], 1, boolToInt(eData[0].FrameInfo.NEnvelopes > 1)+1, 0,
		sbrBitstreamData.HeaderActive)
	for i := 0; i < encMaxNumNoiseValues; i++ {
		hEnvChan[0].EncEnvData.SbrNoiseLevels[i] = eData[0].NoiseLevelCoupling[i]
	}
	CodeEnvelope(eData[1].NoiseLevelCoupling[:], fData.Res[:], &hEnvChan[1].SbrCodeNoiseFloor,
		hEnvChan[1].EncEnvData.DomainVecNoise[:], 1, boolToInt(eData[1].FrameInfo.NEnvelopes > 1)+1, 1,
		sbrBitstreamData.HeaderActive)
	for i := 0; i < encMaxNumNoiseValues; i++ {
		hEnvChan[1].EncEnvData.SbrNoiseLevels[i] = eData[1].NoiseLevelCoupling[i]
	}

	sbrHeaderData.Coupling = 1
	hEnvChan[0].EncEnvData.Balance = 0
	hEnvChan[1].EncEnvData.Balance = 1

	tempFlagLeft := hEnvChan[0].EncEnvData.AddHarmonicFlag
	tempFlagRight := hEnvChan[1].EncEnvData.AddHarmonicFlag

	payloadbitsCOUPLING := CountSbrChannelPairElement(sbrHeaderData, sbrBitstreamData,
		&hEnvChan[0].EncEnvData, &hEnvChan[1].EncEnvData, bs, hCon.SbrSyntaxFlags)

	hEnvChan[0].EncEnvData.AddHarmonicFlag = tempFlagLeft
	hEnvChan[1].EncEnvData.AddHarmonicFlag = tempFlagRight

	if payloadbitsCOUPLING < payloadbitsLR {
		for ch := 0; ch < nChannels; ch++ {
			ed := &eData[ch]
			copy(ed.SfbNrg[:], ed.SfbNrgCoupling[:])
			copy(ed.NoiseLevel[:], ed.NoiseLevelCoupling[:])
		}
		sbrHeaderData.Coupling = 1
		hEnvChan[0].EncEnvData.Balance = 0
		hEnvChan[1].EncEnvData.Balance = 1
	} else {
		for ch := 0; ch < nChannels; ch++ {
			copy(hEnvChan[ch].SbrCodeEnvelope.SfbNrgPrev[:], sfbNrgPrevTemp[ch][:])
			hEnvChan[ch].SbrCodeEnvelope.UpDate = upDateNrgTemp[ch]
			copy(hEnvChan[ch].SbrCodeNoiseFloor.SfbNrgPrev[:encMaxNumNoiseValues], noisePrevTemp[ch][:])
			copy(hEnvChan[ch].EncEnvData.DomainVec[:], domainVecTemp[ch][:])
			copy(hEnvChan[ch].EncEnvData.DomainVecNoise[:], domainVecNoiseTemp[ch][:])
			hEnvChan[ch].SbrCodeNoiseFloor.UpDate = upDateNoiseTemp[ch]
		}
		sbrHeaderData.Coupling = 0
		hEnvChan[0].EncEnvData.Balance = 0
		hEnvChan[1].EncEnvData.Balance = 0
	}
}

// calculateSbrEnvelope is the 1:1 port of calculateSbrEnvelope
// (env_est.cpp:723-1010): turns the per-slot QMF energies into the quantised
// per-SFB envelope scale factors (and, for SBR_COUPLING, the panorama balance).
func calculateSbrEnvelope(yBufferLeft, yBufferRight [][]int32, yBufferScaleLeft, yBufferScaleRight []int,
	frameInfo *SbrFrameInfo, sfbNrgLeft, sfbNrgRight []int8, hCon *SbrConfigData, hSbr *EnvChannel,
	stereoMode SbrStereoMode, maxQuantError *int, yBufferSzShift int) {

	m := 0
	ca := 2 - int(hSbr.EncEnvData.InitSbrAmpRes)
	oneBitLess := 0
	if ca == 2 {
		oneBitLess = 1
	}

	nEnvelopes := frameInfo.NEnvelopes
	shortEnv := frameInfo.ShortEnv - 1
	timeStep := hSbr.SbrExtractEnvelope.TimeStep

	commonScale := nativeaac.FMinI(yBufferScaleLeft[0], yBufferScaleLeft[1])
	if stereoMode == SbrCoupling {
		commonScale = nativeaac.FMinI(commonScale, yBufferScaleRight[0])
		commonScale = nativeaac.FMinI(commonScale, yBufferScaleRight[1])
	}
	commonScale -= 7

	scaleLeft0 := yBufferScaleLeft[0] - commonScale
	scaleLeft1 := yBufferScaleLeft[1] - commonScale
	scaleRight0, scaleRight1 := 0, 0
	if stereoMode == SbrCoupling {
		scaleRight0 = yBufferScaleRight[0] - commonScale
		scaleRight1 = yBufferScaleRight[1] - commonScale
		*maxQuantError = 0
	}

	for env := 0; env < nEnvelopes; env++ {
		var pNrgLeft, pNrgRight [32]int32
		var missingHarmonic, count [32]int
		envNrgLeft := int32(0)
		envNrgRight := int32(0)

		startPos := timeStep * frameInfo.Borders[env]
		stopPos := timeStep * frameInfo.Borders[env+1]
		freqRes := frameInfo.FreqRes[env]
		noOfBands := hCon.NSfb[freqRes]
		envNrgScale := dfractBits - int(nativeaac.CntLeadingZeros(int32(noOfBands)))
		if env == shortEnv {
			j := nativeaac.FMaxI(2, timeStep)
			if (stopPos - startPos - j) > 0 {
				stopPos -= j
			}
		}

		for j := 0; j < noOfBands; j++ {
			var nrgLeft, nrgRight int32

			li := int(hCon.FreqBandTable[freqRes][j])
			ui := int(hCon.FreqBandTable[freqRes][j+1])

			if freqRes == FreqResHigh {
				if j == 0 && ui-li > 1 {
					li++
				}
			} else {
				if j == 0 && ui-li > 2 {
					li++
				}
			}

			missingHarmonic[j] = 0
			if hSbr.EncEnvData.AddHarmonicFlag != 0 {
				if freqRes == FreqResHigh {
					if hSbr.EncEnvData.AddHarmonic[j] != 0 {
						missingHarmonic[j] = 1
					}
				} else {
					startBandHigh := 0
					stopBandHigh := 0
					for int(hCon.FreqBandTable[FreqResHigh][startBandHigh]) < int(hCon.FreqBandTable[FreqResLow][j]) {
						startBandHigh++
					}
					for int(hCon.FreqBandTable[FreqResHigh][stopBandHigh]) < int(hCon.FreqBandTable[FreqResLow][j+1]) {
						stopBandHigh++
					}
					for i := startBandHigh; i < stopBandHigh; i++ {
						if hSbr.EncEnvData.AddHarmonic[i] != 0 {
							missingHarmonic[j] = 1
						}
					}
				}
			}

			borderPos := nativeaac.FMinI(stopPos, hSbr.SbrExtractEnvelope.YBufferWriteOffset<<yBufferSzShift)

			if missingHarmonic[j] != 0 {
				count[j] = stopPos - startPos
				nrgLeft = 0
				for k := li; k < ui; k++ {
					tmpNrg := GetEnvSfbEnergy(k, k+1, startPos, stopPos, borderPos,
						yBufferLeft, yBufferSzShift, scaleLeft0, scaleLeft1)
					nrgLeft = nativeaac.FMaxDBL(nrgLeft, tmpNrg)
				}
				nrgLeft = MhLoweringEnergy(nrgLeft, ui-li)

				if stereoMode == SbrCoupling {
					nrgRight = 0
					for k := li; k < ui; k++ {
						tmpNrg := GetEnvSfbEnergy(k, k+1, startPos, stopPos, borderPos,
							yBufferRight, yBufferSzShift, scaleRight0, scaleRight1)
						nrgRight = nativeaac.FMaxDBL(nrgRight, tmpNrg)
					}
					nrgRight = MhLoweringEnergy(nrgRight, ui-li)
				}
			} else {
				count[j] = (stopPos - startPos) * (ui - li)
				nrgLeft = GetEnvSfbEnergy(li, ui, startPos, stopPos, borderPos,
					yBufferLeft, yBufferSzShift, scaleLeft0, scaleLeft1)
				if stereoMode == SbrCoupling {
					nrgRight = GetEnvSfbEnergy(li, ui, startPos, stopPos, borderPos,
						yBufferRight, yBufferSzShift, scaleRight0, scaleRight1)
				}
			}

			pNrgLeft[j] = nrgLeft
			pNrgRight[j] = nrgRight
			envNrgLeft += nrgLeft >> uint(envNrgScale)
			envNrgRight += nrgRight >> uint(envNrgScale)
		}

		for j := 0; j < noOfBands; j++ {
			nrgLeft2 := int32(0)
			nrgLeft := pNrgLeft[j]
			nrgRight := pNrgRight[j]

			if missingHarmonic[j] == 0 && hSbr.FLevelProtect != 0 {
				nrgLeft = NmhLoweringEnergy(nrgLeft, envNrgLeft, envNrgScale, noOfBands)
				if stereoMode == SbrCoupling {
					nrgRight = NmhLoweringEnergy(nrgRight, envNrgRight, envNrgScale, noOfBands)
				}
			}

			if stereoMode == SbrCoupling {
				nrgLeft2 = nrgLeft
				nrgLeft = (nrgRight + nrgLeft) >> 1
			}

			if nrgLeft > 0 {
				tmpScale := int(nativeaac.CountLeadingBits(nrgLeft))
				nrgLeft = nrgLeft << uint(tmpScale)

				tmp0 := nativeaac.CalcLdData(nrgLeft)
				tmp1 := int32(commonScale+tmpScale) << (dfractBits - 1 - encLdDataShift - 1)
				tmp2 := int32(count[j]*64) << (dfractBits - 1 - 14 - 1)
				tmp2 = nativeaac.CalcLdData(tmp2)
				tmp3 := fl2f(0.6875-0.21875-0.015625) >> 1

				nrgLeft = ((tmp0 - tmp2) >> 1) + (tmp3 - tmp1)
			} else {
				nrgLeft = fl2f(-1.0)
			}

			nrgLeft = nativeaac.FMinDBL(nativeaac.FMaxDBL(nrgLeft, 0), fl2f(0.5)>>uint(oneBitLess))
			nrgLeft = nrgLeft >> uint(dfractBits-1-encLdDataShift-1-oneBitLess-1)
			sfbNrgLeft[m] = int8((int(nrgLeft) + 1) >> 1) // rounding

			if stereoMode == SbrCoupling {
				nrgLeft2 = nativeaac.FMaxDBL(0x1, nrgLeft2)
				nrgRight = nativeaac.FMaxDBL(0x1, nrgRight)

				sc0 := int(nativeaac.CountLeadingBits(nrgLeft2))
				sc1 := int(nativeaac.CountLeadingBits(nrgRight))

				scaleFract := int32(sc0-sc1) << (dfractBits - 1 - encLdDataShift)
				nrgRight = nativeaac.CalcLdData(nrgLeft2<<uint(sc0)) - nativeaac.CalcLdData(nrgRight<<uint(sc1)) - scaleFract

				nrgRight = nrgRight >> uint(dfractBits-1-encLdDataShift-1-oneBitLess)
				nrgRight = (nrgRight + 1) >> 1

				pan, quantError := mapPanorama(int(nrgRight), int(hSbr.EncEnvData.InitSbrAmpRes))
				sfbNrgRight[m] = int8(pan)
				*maxQuantError = nativeaac.FMaxI(quantError, *maxQuantError)
			}

			m++
		}

		// Energy compensation for synthetic-sine coding (parametric coding).
		if hCon.UseParametricCoding != 0 {
			m -= noOfBands
			for j := 0; j < noOfBands; j++ {
				if freqRes == FreqResHigh && hSbr.SbrExtractEnvelope.EnvelopeCompensation[j] != 0 {
					sfbNrgLeft[m] -= int8(ca * int(nativeaac.FixpAbs(int32(int8(hSbr.SbrExtractEnvelope.EnvelopeCompensation[j])))))
				}
				if sfbNrgLeft[m] < 0 {
					sfbNrgLeft[m] = 0
				}
				m++
			}
		}
	}
}
