// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Parity-only export for the psychoacoustic DRIVER FDKaacEnc_psyMain
// (psy_main.go). The cgo parity slice under internal/parity_tests/enc-psy-main/
// runs ParityPsyMain (Go init + Go psyMain on a deterministic input) and
// compares the resulting PSY_OUT_CHANNEL dump field-for-field against the
// GENUINE vendored FDKaacEnc_psyMain (driven through the real
// FDKaacEnc_Open/Initialize + the same input in the oracle bridge). fdk-aac
// encode is fixed-point, so every compared value is EXACT int32/int.

package nativeaac

// PsyMainDump is the flat, exported mirror of the per-channel PSY_OUT_CHANNEL
// state FDKaacEnc_psyMain fills, plus the per-element common-window / MS digest.
// Only the deterministic, downstream-consumed fields are dumped (the over-read
// sfbOffsets[52..60] long-block tail is excluded — see psy_main.go).
type PsyMainDump struct {
	ErrCode int

	CommonWindow int
	MsDigest     int
	MsMask       [maxGroupedSfb]int

	// per channel [2]
	SfbCnt             [2]int
	SfbPerGroup        [2]int
	MaxSfbPerGroup     [2]int
	WindowShape        [2]int
	LastWindowSequence [2]int
	GroupingMask       [2]int
	MdctScale          [2]int
	GroupLen           [2][maxNoOfGroups]int
	SfbOffsets         [2][maxGroupedSfb + 1]int
	NoiseNrg           [2][maxGroupedSfb]int
	IsBook             [2][maxGroupedSfb]int
	IsScale            [2][maxGroupedSfb]int

	SfbEnergy          [2][maxGroupedSfb]int32
	SfbSpreadEnergy    [2][maxGroupedSfb]int32
	SfbEnergyLdData    [2][maxGroupedSfb]int32
	SfbThresholdLdData [2][maxGroupedSfb]int32
	SfbMinSnrLdData    [2][maxGroupedSfb]int32

	// TNS bitstream-side info (numOfFilters / coefRes / order / coefs) the
	// writer reads; the full per-window/filter shape is flattened.
	TnsNumOfFilters [2][encTransFac]int
	TnsCoefRes      [2][encTransFac]int
	TnsOrder        [2][encTransFac][maxNumOfFilters]int
}

// ParityPsyMain drives Go Open + Initialize for the given AAC-LC CBR config,
// runs FDKaacEnc_psyMain (the Go port) over the supplied planar int16 input
// (channel ch at input[ch*inputBufSize : (ch+1)*inputBufSize]) for one element,
// and returns the PSY_OUT dump. It reproduces the EncodeFrame pointer aliasing
// (psyOutChan->X = qcOutChan->X) before psyMain so the run matches the genuine
// FDKaacEnc_EncodeFrame call path the oracle exercises.
func ParityPsyMain(channelMode, nChannels, sampleRate, bitRate, aot, nElements, frameLength int,
	input []int16, inputBufSize int) PsyMainDump {

	var d PsyMainDump

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
		d.ErrCode = int(err)
		return d
	}
	if err := Initialize(hAacEnc, &config, nil, 1); err != AacEncOK {
		d.ErrCode = int(err)
		return d
	}

	cm := &hAacEnc.ChannelMapping
	el := 0
	elInfo := cm.ElInfo[el]
	psyOutEl := hAacEnc.PsyOut[0].PsyOutElement[el]

	ec := psyMain(elInfo.NChannelsInEl, hAacEnc.PsyKernel.PsyElement[el],
		hAacEnc.PsyKernel.PsyDynamic, hAacEnc.PsyKernel.PsyConf[:],
		psyOutEl, input, uint(inputBufSize),
		cm.ElInfo[el].ChannelIndex[:], cm.NChannels)
	d.ErrCode = int(ec)
	if ec != AacEncOK {
		return d
	}

	d.CommonWindow = psyOutEl.CommonWindow
	d.MsDigest = psyOutEl.ToolsInfo.MsDigest
	for i := 0; i < maxGroupedSfb; i++ {
		d.MsMask[i] = psyOutEl.ToolsInfo.MsMask[i]
	}

	for ch := 0; ch < elInfo.NChannelsInEl; ch++ {
		poc := psyOutEl.PsyOutChannel[ch]
		d.SfbCnt[ch] = poc.SfbCnt
		d.SfbPerGroup[ch] = poc.SfbPerGroup
		d.MaxSfbPerGroup[ch] = poc.MaxSfbPerGroup
		d.WindowShape[ch] = poc.WindowShape
		d.LastWindowSequence[ch] = poc.LastWindowSequence
		d.GroupingMask[ch] = poc.GroupingMask
		d.MdctScale[ch] = poc.MdctScale
		for i := 0; i < maxNoOfGroups; i++ {
			d.GroupLen[ch][i] = poc.GroupLen[i]
		}
		for i := 0; i < maxGroupedSfb+1; i++ {
			d.SfbOffsets[ch][i] = poc.SfbOffsets[i]
		}
		for i := 0; i < maxGroupedSfb; i++ {
			d.NoiseNrg[ch][i] = poc.NoiseNrg[i]
			d.IsBook[ch][i] = poc.IsBook[i]
			d.IsScale[ch][i] = poc.IsScale[i]
			d.SfbEnergy[ch][i] = poc.SfbEnergy[i]
			d.SfbSpreadEnergy[ch][i] = poc.SfbSpreadEnergy[i]
			d.SfbEnergyLdData[ch][i] = poc.SfbEnergyLdData[i]
			d.SfbThresholdLdData[ch][i] = poc.SfbThresholdLdData[i]
			d.SfbMinSnrLdData[ch][i] = poc.SfbMinSnrLdData[i]
		}
		for w := 0; w < encTransFac; w++ {
			d.TnsNumOfFilters[ch][w] = poc.TnsInfo.NumOfFilters[w]
			d.TnsCoefRes[ch][w] = poc.TnsInfo.CoefRes[w]
			for f := 0; f < maxNumOfFilters; f++ {
				d.TnsOrder[ch][w][f] = poc.TnsInfo.Order[w][f]
			}
		}
	}

	return d
}
