// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Parity-only export for the encoder init/config DRIVER tier (aacenc_init.go
// AacInitDefaultConfig / Open / Initialize + psy_main_init.go psyMainInit). The
// cgo parity slice under internal/parity_tests/enc-init/ runs ParityInit and
// compares the resulting flat dump field-for-field against the GENUINE vendored
// FDKaacEnc_Open + FDKaacEnc_Initialize state (reached via a TU that links
// aacenc.cpp et al.). Not part of the production API.
//
// The dump exposes every deterministic init-populated field of the unexported
// state graph (QcState / adjThrState / atsElement / PsyConfiguration / its
// TNSConfig+PNSConfig / ChannelMapping / AacEnc lifetime vars) as exported flat
// values, so the cross-package test (package encinit) can assert exact integer
// equality. The transport static-bit demand is pinned to 0 (nil
// StaticBitsProvider) to match the oracle's TT_MP4_RAW stub.

package nativeaac

// InitStateDump is the flat, exported mirror of the init-populated encoder state
// the parity oracle compares. Field groups mirror the C einit_State (bridge.cpp).
type InitStateDump struct {
	// channel mapping
	CmEncMode, CmNChannels, CmNChannelsEff, CmNElements       int
	CmElType, CmElInstanceTag, CmElNChannelsInEl, CmElChIndex [8]int

	// AAC_ENC lifetime / config-derived
	Aot, BitrateMode, Bandwidth90dB int
	MaxAncBytesPerAU                uint
	CfgBandWidth                    int

	// QC_STATE
	QcGlobHdrBits, QcMaxBitsPerFrame, QcMinBitsPerFrame, QcNElements int
	QcBitrateMode, QcBitResMode, QcBitResTot, QcBitResTotMax         int
	QcMaxIterations, QcInvQuant, QcVbrQualFactor, QcMaxBitFac        int
	QcPaddingRest, QcDZoneQuantEnable                                int
	QcEbChBitrateEl, QcEbMaxBitsEl, QcEbBitResLevelEl                [8]int
	QcEbMaxBitResBitsEl, QcEbRelativeBitsEl                          [8]int

	// ADJ_THR_STATE
	AtBitDistributionMode, AtMaxIter2ndGuess       int
	AtBpL, AtBpS                                   [8]int
	AtPeMin, AtPeMax, AtPeOffset                   [8]int
	AtBits2PeFactorM, AtBits2PeFactorE             [8]int
	AtAhModifyMinSnr, AtAhStartSfbL, AtAhStartSfbS [8]int
	AtMsaMaxRed, AtMsaStartRatio, AtMsaMaxRatio    [8]int
	AtMsaRedRatioFac, AtMsaRedOffs                 [8]int
	AtPeLast, AtDynBitsLast, AtPeCorrM, AtPeCorrE  [8]int
	AtVbrQualFactor, AtChaosMeasureOld             [8]int

	// PSY_CONFIGURATION[0]=LONG / [1]=SHORT
	PcSfbCnt, PcSfbActive, PcSfbActiveLFE, PcFilterbank          [2]int
	PcMaxAllowedIncreaseFactor, PcMinRemainingThresholdFactor    [2]int
	PcLowpassLine, PcLowpassLineLFE, PcClipEnergy                [2]int
	PcGranuleLength, PcAllowIS, PcAllowMS                        [2]int
	PcSfbOffset                                                  [2][52]int
	PcSfbPcmQuantThreshold                                       [2][51]int
	PcSfbMaskLowFactor, PcSfbMaskHighFactor                      [2][51]int
	PcSfbMaskLowFactorSprEn, PcSfbMaskHighFactorSprEn            [2][51]int
	PcSfbMinSnrLdData                                            [2][51]int
	PcPnsUsePns, PcPnsMinCorrelationEnergy, PcPnsNoiseCorrThresh [2]int
	PcTnsIsLowDelay, PcTnsTnsActive, PcTnsMaxOrder, PcTnsCoefRes [2]int
	PcTnsLpcStartBand, PcTnsLpcStartLine                         [2][2]int
	PcTnsLpcStopBand, PcTnsLpcStopLine                           [2]int
}

// ParityInit drives AacInitDefaultConfig + Open + Initialize for the given AAC-LC
// CBR config and returns the init error code (0 == AAC_ENC_OK) plus the flat
// dump. channelMode is the resolved ChannelMode; nElements/nChannels size the
// allocation. Mirrors the oracle's einit_run.
func ParityInit(channelMode, nChannels, sampleRate, bitRate, aot, nElements, frameLength int) (int, InitStateDump) {
	var d InitStateDump

	var config AacencConfig
	AacInitDefaultConfig(&config)
	config.AudioObjectType = aot
	config.NChannels = nChannels
	config.ChannelMode = ChannelMode(channelMode)
	config.SampleRate = sampleRate
	config.BitRate = bitRate
	config.FrameLength = frameLength

	hAacEnc, err := Open(nElements, nChannels, config.NSubFrames)
	if err != AacEncOK {
		return int(err), d
	}

	// hTpEnc == nil StaticBitsProvider -> 0 (matches the oracle's TT_MP4_RAW stub).
	err = Initialize(hAacEnc, &config, nil /*initFlags=*/, 1)
	if err != AacEncOK {
		return int(err), d
	}

	cm := &hAacEnc.ChannelMapping
	d.CmEncMode = int(cm.EncMode)
	d.CmNChannels = cm.NChannels
	d.CmNChannelsEff = cm.NChannelsEff
	d.CmNElements = cm.NElements
	for i := 0; i < cm.NElements && i < 8; i++ {
		d.CmElType[i] = cm.ElInfo[i].ElType
		d.CmElInstanceTag[i] = cm.ElInfo[i].InstanceTag
		d.CmElNChannelsInEl[i] = cm.ElInfo[i].NChannelsInEl
		d.CmElChIndex[i] = cm.ElInfo[i].ChannelIndex[0]
	}

	d.Aot = hAacEnc.Aot
	d.BitrateMode = int(hAacEnc.BitrateMode)
	d.Bandwidth90dB = hAacEnc.Bandwidth90dB
	d.MaxAncBytesPerAU = config.MaxAncBytesPerAU
	d.CfgBandWidth = config.BandWidth

	qc := hAacEnc.QcKernel
	d.QcGlobHdrBits = qc.GlobHdrBits
	d.QcMaxBitsPerFrame = qc.MaxBitsPerFrame
	d.QcMinBitsPerFrame = qc.MinBitsPerFrame
	d.QcNElements = qc.NElements
	d.QcBitrateMode = int(qc.BitrateMode)
	d.QcBitResMode = int(qc.BitResMode)
	d.QcBitResTot = qc.BitResTot
	d.QcBitResTotMax = qc.BitResTotMax
	d.QcMaxIterations = qc.MaxIterations
	d.QcInvQuant = qc.InvQuant
	d.QcVbrQualFactor = int(qc.VbrQualFactor)
	d.QcMaxBitFac = int(qc.MaxBitFac)
	d.QcPaddingRest = qc.Padding.PaddingRest
	d.QcDZoneQuantEnable = qc.DZoneQuantEnable
	for i := 0; i < cm.NElements && i < 8; i++ {
		eb := qc.ElementBits[i]
		d.QcEbChBitrateEl[i] = eb.ChBitrateEl
		d.QcEbMaxBitsEl[i] = eb.MaxBitsEl
		d.QcEbBitResLevelEl[i] = eb.BitResLevelEl
		d.QcEbMaxBitResBitsEl[i] = eb.MaxBitResBitsEl
		d.QcEbRelativeBitsEl[i] = int(eb.RelativeBitsEl)
	}

	at := qc.HAdjThr
	d.AtBitDistributionMode = at.bitDistributionMode
	d.AtMaxIter2ndGuess = at.maxIter2ndGuess
	dumpBres(&at.bresParamLong, &d.AtBpL)
	dumpBres(&at.bresParamShort, &d.AtBpS)
	for i := 0; i < cm.NElements && i < 8; i++ {
		e := at.adjThrStateElem[i]
		d.AtPeMin[i] = e.peMin
		d.AtPeMax[i] = e.peMax
		d.AtPeOffset[i] = e.peOffset
		d.AtBits2PeFactorM[i] = int(e.bits2PeFactorM)
		d.AtBits2PeFactorE[i] = e.bits2PeFactorE
		d.AtAhModifyMinSnr[i] = e.ahParam.modifyMinSnr
		d.AtAhStartSfbL[i] = e.ahParam.startSfbL
		d.AtAhStartSfbS[i] = e.ahParam.startSfbS
		d.AtMsaMaxRed[i] = int(e.minSnrAdaptParam.maxRed)
		d.AtMsaStartRatio[i] = int(e.minSnrAdaptParam.startRatio)
		d.AtMsaMaxRatio[i] = int(e.minSnrAdaptParam.maxRatio)
		d.AtMsaRedRatioFac[i] = int(e.minSnrAdaptParam.redRatioFac)
		d.AtMsaRedOffs[i] = int(e.minSnrAdaptParam.redOffs)
		d.AtPeLast[i] = e.peLast
		d.AtDynBitsLast[i] = e.dynBitsLast
		d.AtPeCorrM[i] = int(e.peCorrectionFactorM)
		d.AtPeCorrE[i] = e.peCorrectionFactorE
		d.AtVbrQualFactor[i] = int(e.vbrQualFactor)
		d.AtChaosMeasureOld[i] = int(e.chaosMeasureOld)
	}

	dumpPsyConf(&hAacEnc.PsyKernel.PsyConf[0], 0, &d)
	dumpPsyConf(&hAacEnc.PsyKernel.PsyConf[1], 1, &d)

	return int(AacEncOK), d
}

func dumpBres(bp *bresParam, out *[8]int) {
	out[0] = int(bp.clipSaveLow)
	out[1] = int(bp.clipSaveHigh)
	out[2] = int(bp.minBitSave)
	out[3] = int(bp.maxBitSave)
	out[4] = int(bp.clipSpendLow)
	out[5] = int(bp.clipSpendHigh)
	out[6] = int(bp.minBitSpend)
	out[7] = int(bp.maxBitSpend)
}

func dumpPsyConf(pc *PsyConfiguration, idx int, d *InitStateDump) {
	d.PcSfbCnt[idx] = pc.SfbCnt
	d.PcSfbActive[idx] = pc.SfbActive
	d.PcSfbActiveLFE[idx] = pc.SfbActiveLFE
	d.PcFilterbank[idx] = pc.Filterbank
	d.PcMaxAllowedIncreaseFactor[idx] = pc.MaxAllowedIncreaseFactor
	d.PcMinRemainingThresholdFactor[idx] = int(pc.MinRemainingThresholdFactor)
	d.PcLowpassLine[idx] = pc.LowpassLine
	d.PcLowpassLineLFE[idx] = pc.LowpassLineLFE
	d.PcClipEnergy[idx] = int(pc.ClipEnergy)
	d.PcGranuleLength[idx] = pc.GranuleLength
	d.PcAllowIS[idx] = pc.AllowIS
	d.PcAllowMS[idx] = pc.AllowMS
	for i := 0; i < 52; i++ {
		d.PcSfbOffset[idx][i] = int(pc.SfbOffset[i])
	}
	for i := 0; i < 51; i++ {
		d.PcSfbPcmQuantThreshold[idx][i] = int(pc.SfbPcmQuantThreshold[i])
		d.PcSfbMaskLowFactor[idx][i] = int(pc.SfbMaskLowFactor[i])
		d.PcSfbMaskHighFactor[idx][i] = int(pc.SfbMaskHighFactor[i])
		d.PcSfbMaskLowFactorSprEn[idx][i] = int(pc.SfbMaskLowFactorSprEn[i])
		d.PcSfbMaskHighFactorSprEn[idx][i] = int(pc.SfbMaskHighFactorSprEn[i])
		d.PcSfbMinSnrLdData[idx][i] = int(pc.SfbMinSnrLdData[i])
	}
	d.PcPnsUsePns[idx] = pc.PnsConf.UsePns
	d.PcPnsMinCorrelationEnergy[idx] = int(pc.PnsConf.MinCorrelationEnergy)
	d.PcPnsNoiseCorrThresh[idx] = int(pc.PnsConf.NoiseCorrelationThresh)
	d.PcTnsIsLowDelay[idx] = pc.TnsConf.IsLowDelay
	d.PcTnsTnsActive[idx] = pc.TnsConf.TnsActive
	d.PcTnsMaxOrder[idx] = pc.TnsConf.MaxOrder
	d.PcTnsCoefRes[idx] = pc.TnsConf.CoefRes
	for i := 0; i < 2; i++ {
		d.PcTnsLpcStartBand[idx][i] = pc.TnsConf.LpcStartBand[i]
		d.PcTnsLpcStartLine[idx][i] = pc.TnsConf.LpcStartLine[i]
	}
	d.PcTnsLpcStopBand[idx] = pc.TnsConf.LpcStopBand
	d.PcTnsLpcStopLine[idx] = pc.TnsConf.LpcStopLine
}
