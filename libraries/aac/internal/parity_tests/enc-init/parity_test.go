// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Exact-integer parity for the AAC-LC CBR encoder init/config DRIVER tier: the
// pure-Go FDKaacEnc_AacInitDefaultConfig / FDKaacEnc_Open / FDKaacEnc_Initialize
// (which drive psyMainInit -> InitPsyConfiguration / InitTnsConfiguration /
// InitPnsConfiguration / InitPreEchoControl, QCOutInit, QCInit -> AdjThrInit,
// InitChannelMapping, DetermineBandWidth) vs the GENUINE vendored
// FDKaacEnc_Open + FDKaacEnc_Initialize. Both are driven for the same default
// stereo AAC-LC CBR config; every deterministic init-populated field of the
// PSY_CONFIGURATION / QC_STATE / ADJ_THR_STATE / CHANNEL_MAPPING / AAC_ENC state
// is compared with require.Equal (exact int, no tolerance) — fdk-aac encode is
// fixed-point so the init arithmetic is bit-identical.

package encinit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// MODE_2 (stereo CPE), AOT_AAC_LC.
const (
	modeStereo = 2
	aotAACLC   = 2
)

func TestEncInitParityStereoCBR(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-init parity asserts under -tags aac_strict")
	}

	cases := []struct {
		name                                  string
		channelMode, nChannels, nElements     int
		sampleRate, bitRate, aot, frameLength int
	}{
		{"stereo-44k1-128k", modeStereo, 2, 1, 44100, 128000, aotAACLC, 1024},
		{"stereo-48k-128k", modeStereo, 2, 1, 48000, 128000, aotAACLC, 1024},
		{"stereo-44k1-192k", modeStereo, 2, 1, 44100, 192000, aotAACLC, 1024},
		{"stereo-48k-256k", modeStereo, 2, 1, 48000, 256000, aotAACLC, 1024},
		{"stereo-32k-96k", modeStereo, 2, 1, 32000, 96000, aotAACLC, 1024},
		{"stereo-44k1-64k", modeStereo, 2, 1, 44100, 64000, aotAACLC, 1024},
		{"stereo-22k05-64k", modeStereo, 2, 1, 22050, 64000, aotAACLC, 1024},
		{"stereo-24k-48k", modeStereo, 2, 1, 24000, 48000, aotAACLC, 1024},
		{"mono-44k1-64k", 1 /*MODE_1*/, 1, 1, 44100, 64000, aotAACLC, 1024},
		{"mono-48k-96k", 1, 1, 1, 48000, 96000, aotAACLC, 1024},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			goRC, g := nativeaac.ParityInit(tc.channelMode, tc.nChannels, tc.sampleRate,
				tc.bitRate, tc.aot, tc.nElements, tc.frameLength)
			cRC, c := cEinitRun(tc.channelMode, tc.nChannels, tc.sampleRate, tc.bitRate,
				tc.aot, tc.nElements, tc.frameLength)

			require.Equal(t, cRC, goRC, "init return code")
			require.Equal(t, 0, goRC, "init must succeed")

			// --- channel mapping ---
			require.Equal(t, int(c.cm_encMode), g.CmEncMode, "cm.encMode")
			require.Equal(t, int(c.cm_nChannels), g.CmNChannels, "cm.nChannels")
			require.Equal(t, int(c.cm_nChannelsEff), g.CmNChannelsEff, "cm.nChannelsEff")
			require.Equal(t, int(c.cm_nElements), g.CmNElements, "cm.nElements")
			for i := 0; i < g.CmNElements; i++ {
				require.Equal(t, int(c.cm_elType[i]), g.CmElType[i], "elInfo[%d].elType", i)
				require.Equal(t, int(c.cm_elInstanceTag[i]), g.CmElInstanceTag[i], "elInfo[%d].instanceTag", i)
				require.Equal(t, int(c.cm_elNChannelsInEl[i]), g.CmElNChannelsInEl[i], "elInfo[%d].nChannelsInEl", i)
				require.Equal(t, int(c.cm_elChIndex[i]), g.CmElChIndex[i], "elInfo[%d].ChannelIndex[0]", i)
			}

			// --- AAC_ENC lifetime / config-derived ---
			require.Equal(t, int(c.aot), g.Aot, "aot")
			require.Equal(t, int(c.bitrateMode), g.BitrateMode, "bitrateMode")
			require.Equal(t, int(c.bandwidth90dB), g.Bandwidth90dB, "bandwidth90dB")
			require.Equal(t, uint(c.maxAncBytesPerAU), g.MaxAncBytesPerAU, "maxAncBytesPerAU")
			require.Equal(t, int(c.cfg_bandWidth), g.CfgBandWidth, "config.bandWidth")

			// --- QC_STATE ---
			require.Equal(t, int(c.qc_globHdrBits), g.QcGlobHdrBits, "qc.globHdrBits")
			require.Equal(t, int(c.qc_maxBitsPerFrame), g.QcMaxBitsPerFrame, "qc.maxBitsPerFrame")
			require.Equal(t, int(c.qc_minBitsPerFrame), g.QcMinBitsPerFrame, "qc.minBitsPerFrame")
			require.Equal(t, int(c.qc_nElements), g.QcNElements, "qc.nElements")
			require.Equal(t, int(c.qc_bitrateMode), g.QcBitrateMode, "qc.bitrateMode")
			require.Equal(t, int(c.qc_bitResMode), g.QcBitResMode, "qc.bitResMode")
			require.Equal(t, int(c.qc_bitResTot), g.QcBitResTot, "qc.bitResTot")
			require.Equal(t, int(c.qc_bitResTotMax), g.QcBitResTotMax, "qc.bitResTotMax")
			require.Equal(t, int(c.qc_maxIterations), g.QcMaxIterations, "qc.maxIterations")
			require.Equal(t, int(c.qc_invQuant), g.QcInvQuant, "qc.invQuant")
			require.Equal(t, int(c.qc_vbrQualFactor), g.QcVbrQualFactor, "qc.vbrQualFactor")
			require.Equal(t, int(c.qc_maxBitFac), g.QcMaxBitFac, "qc.maxBitFac")
			require.Equal(t, int(c.qc_paddingRest), g.QcPaddingRest, "qc.padding.paddingRest")
			require.Equal(t, int(c.qc_dZoneQuantEnable), g.QcDZoneQuantEnable, "qc.dZoneQuantEnable")
			for i := 0; i < g.CmNElements; i++ {
				require.Equal(t, int(c.qc_eb_chBitrateEl[i]), g.QcEbChBitrateEl[i], "elementBits[%d].chBitrateEl", i)
				require.Equal(t, int(c.qc_eb_maxBitsEl[i]), g.QcEbMaxBitsEl[i], "elementBits[%d].maxBitsEl", i)
				require.Equal(t, int(c.qc_eb_bitResLevelEl[i]), g.QcEbBitResLevelEl[i], "elementBits[%d].bitResLevelEl", i)
				require.Equal(t, int(c.qc_eb_maxBitResBitsEl[i]), g.QcEbMaxBitResBitsEl[i], "elementBits[%d].maxBitResBitsEl", i)
				require.Equal(t, int(c.qc_eb_relativeBitsEl[i]), g.QcEbRelativeBitsEl[i], "elementBits[%d].relativeBitsEl", i)
			}

			// --- ADJ_THR_STATE ---
			require.Equal(t, int(c.at_bitDistributionMode), g.AtBitDistributionMode, "adjThr.bitDistributionMode")
			require.Equal(t, int(c.at_maxIter2ndGuess), g.AtMaxIter2ndGuess, "adjThr.maxIter2ndGuess")
			for i := 0; i < 8; i++ {
				require.Equal(t, int(c.at_bpL[i]), g.AtBpL[i], "bresParamLong[%d]", i)
				require.Equal(t, int(c.at_bpS[i]), g.AtBpS[i], "bresParamShort[%d]", i)
			}
			for i := 0; i < g.CmNElements; i++ {
				require.Equal(t, int(c.at_peMin[i]), g.AtPeMin[i], "ats[%d].peMin", i)
				require.Equal(t, int(c.at_peMax[i]), g.AtPeMax[i], "ats[%d].peMax", i)
				require.Equal(t, int(c.at_peOffset[i]), g.AtPeOffset[i], "ats[%d].peOffset", i)
				require.Equal(t, int(c.at_bits2PeFactor_m[i]), g.AtBits2PeFactorM[i], "ats[%d].bits2PeFactor_m", i)
				require.Equal(t, int(c.at_bits2PeFactor_e[i]), g.AtBits2PeFactorE[i], "ats[%d].bits2PeFactor_e", i)
				require.Equal(t, int(c.at_ah_modifyMinSnr[i]), g.AtAhModifyMinSnr[i], "ats[%d].ahParam.modifyMinSnr", i)
				require.Equal(t, int(c.at_ah_startSfbL[i]), g.AtAhStartSfbL[i], "ats[%d].ahParam.startSfbL", i)
				require.Equal(t, int(c.at_ah_startSfbS[i]), g.AtAhStartSfbS[i], "ats[%d].ahParam.startSfbS", i)
				require.Equal(t, int(c.at_msa_maxRed[i]), g.AtMsaMaxRed[i], "ats[%d].minSnrAdaptParam.maxRed", i)
				require.Equal(t, int(c.at_msa_startRatio[i]), g.AtMsaStartRatio[i], "ats[%d].minSnrAdaptParam.startRatio", i)
				require.Equal(t, int(c.at_msa_maxRatio[i]), g.AtMsaMaxRatio[i], "ats[%d].minSnrAdaptParam.maxRatio", i)
				require.Equal(t, int(c.at_msa_redRatioFac[i]), g.AtMsaRedRatioFac[i], "ats[%d].minSnrAdaptParam.redRatioFac", i)
				require.Equal(t, int(c.at_msa_redOffs[i]), g.AtMsaRedOffs[i], "ats[%d].minSnrAdaptParam.redOffs", i)
				require.Equal(t, int(c.at_peLast[i]), g.AtPeLast[i], "ats[%d].peLast", i)
				require.Equal(t, int(c.at_dynBitsLast[i]), g.AtDynBitsLast[i], "ats[%d].dynBitsLast", i)
				require.Equal(t, int(c.at_peCorr_m[i]), g.AtPeCorrM[i], "ats[%d].peCorrectionFactor_m", i)
				require.Equal(t, int(c.at_peCorr_e[i]), g.AtPeCorrE[i], "ats[%d].peCorrectionFactor_e", i)
				require.Equal(t, int(c.at_vbrQualFactor[i]), g.AtVbrQualFactor[i], "ats[%d].vbrQualFactor", i)
				require.Equal(t, int(c.at_chaosMeasureOld[i]), g.AtChaosMeasureOld[i], "ats[%d].chaosMeasureOld", i)
			}

			// --- PSY_CONFIGURATION[0]=LONG, [1]=SHORT ---
			for w := 0; w < 2; w++ {
				require.Equal(t, int(c.pc_sfbCnt[w]), g.PcSfbCnt[w], "psyConf[%d].sfbCnt", w)
				require.Equal(t, int(c.pc_sfbActive[w]), g.PcSfbActive[w], "psyConf[%d].sfbActive", w)
				require.Equal(t, int(c.pc_sfbActiveLFE[w]), g.PcSfbActiveLFE[w], "psyConf[%d].sfbActiveLFE", w)
				require.Equal(t, int(c.pc_filterbank[w]), g.PcFilterbank[w], "psyConf[%d].filterbank", w)
				require.Equal(t, int(c.pc_maxAllowedIncreaseFactor[w]), g.PcMaxAllowedIncreaseFactor[w], "psyConf[%d].maxAllowedIncreaseFactor", w)
				require.Equal(t, int(c.pc_minRemainingThresholdFactor[w]), g.PcMinRemainingThresholdFactor[w], "psyConf[%d].minRemainingThresholdFactor", w)
				require.Equal(t, int(c.pc_lowpassLine[w]), g.PcLowpassLine[w], "psyConf[%d].lowpassLine", w)
				require.Equal(t, int(c.pc_lowpassLineLFE[w]), g.PcLowpassLineLFE[w], "psyConf[%d].lowpassLineLFE", w)
				require.Equal(t, int(c.pc_clipEnergy[w]), g.PcClipEnergy[w], "psyConf[%d].clipEnergy", w)
				require.Equal(t, int(c.pc_granuleLength[w]), g.PcGranuleLength[w], "psyConf[%d].granuleLength", w)
				require.Equal(t, int(c.pc_allowIS[w]), g.PcAllowIS[w], "psyConf[%d].allowIS", w)
				require.Equal(t, int(c.pc_allowMS[w]), g.PcAllowMS[w], "psyConf[%d].allowMS", w)
				for i := 0; i < 52; i++ {
					require.Equal(t, int(c.pc_sfbOffset[w][i]), g.PcSfbOffset[w][i], "psyConf[%d].sfbOffset[%d]", w, i)
				}
				for i := 0; i < 51; i++ {
					require.Equal(t, int(c.pc_sfbPcmQuantThreshold[w][i]), g.PcSfbPcmQuantThreshold[w][i], "psyConf[%d].sfbPcmQuantThreshold[%d]", w, i)
					require.Equal(t, int(c.pc_sfbMaskLowFactor[w][i]), g.PcSfbMaskLowFactor[w][i], "psyConf[%d].sfbMaskLowFactor[%d]", w, i)
					require.Equal(t, int(c.pc_sfbMaskHighFactor[w][i]), g.PcSfbMaskHighFactor[w][i], "psyConf[%d].sfbMaskHighFactor[%d]", w, i)
					require.Equal(t, int(c.pc_sfbMaskLowFactorSprEn[w][i]), g.PcSfbMaskLowFactorSprEn[w][i], "psyConf[%d].sfbMaskLowFactorSprEn[%d]", w, i)
					require.Equal(t, int(c.pc_sfbMaskHighFactorSprEn[w][i]), g.PcSfbMaskHighFactorSprEn[w][i], "psyConf[%d].sfbMaskHighFactorSprEn[%d]", w, i)
					require.Equal(t, int(c.pc_sfbMinSnrLdData[w][i]), g.PcSfbMinSnrLdData[w][i], "psyConf[%d].sfbMinSnrLdData[%d]", w, i)
				}
				require.Equal(t, int(c.pc_pns_usePns[w]), g.PcPnsUsePns[w], "psyConf[%d].pnsConf.usePns", w)
				require.Equal(t, int(c.pc_pns_minCorrelationEnergy[w]), g.PcPnsMinCorrelationEnergy[w], "psyConf[%d].pnsConf.minCorrelationEnergy", w)
				require.Equal(t, int(c.pc_pns_noiseCorrelationThresh[w]), g.PcPnsNoiseCorrThresh[w], "psyConf[%d].pnsConf.noiseCorrelationThresh", w)
				require.Equal(t, int(c.pc_tns_isLowDelay[w]), g.PcTnsIsLowDelay[w], "psyConf[%d].tnsConf.isLowDelay", w)
				require.Equal(t, int(c.pc_tns_tnsActive[w]), g.PcTnsTnsActive[w], "psyConf[%d].tnsConf.tnsActive", w)
				require.Equal(t, int(c.pc_tns_maxOrder[w]), g.PcTnsMaxOrder[w], "psyConf[%d].tnsConf.maxOrder", w)
				require.Equal(t, int(c.pc_tns_coefRes[w]), g.PcTnsCoefRes[w], "psyConf[%d].tnsConf.coefRes", w)
				for i := 0; i < 2; i++ {
					require.Equal(t, int(c.pc_tns_lpcStartBand[w][i]), g.PcTnsLpcStartBand[w][i], "psyConf[%d].tnsConf.lpcStartBand[%d]", w, i)
					require.Equal(t, int(c.pc_tns_lpcStartLine[w][i]), g.PcTnsLpcStartLine[w][i], "psyConf[%d].tnsConf.lpcStartLine[%d]", w, i)
				}
				require.Equal(t, int(c.pc_tns_lpcStopBand[w]), g.PcTnsLpcStopBand[w], "psyConf[%d].tnsConf.lpcStopBand", w)
				require.Equal(t, int(c.pc_tns_lpcStopLine[w]), g.PcTnsLpcStopLine[w], "psyConf[%d].tnsConf.lpcStopLine", w)
			}
		})
	}
}
