// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of the SBR-encoder INIT + PER-FRAME DRIVER half of
// libSBRenc/src/sbr_encoder.cpp (the struct/config tier is in
// enc_sbr_encoder.go, the tuning ROM in enc_sbr_encoder_config.go):
//   - FDKsbrEnc_EnvInit       (sbr_encoder.cpp:1658-1852)
//   - sbrEncoder_Init_delay   (sbr_encoder.cpp:1950-2056)
//   - FDKsbrEnc_DelayCompensation (sbr_encoder.cpp:1867-1883)
//   - FDKsbrEnc_bsBufInit      (sbr_encoder.cpp:1637-1647)
//   - SbrEncoderInit (== sbrEncoder_Open + sbrEncoder_Init)  (sbr_encoder.cpp:2065-2381)
//   - FDKsbrEnc_EnvEncodeFrame (sbr_encoder.cpp:941-1193)
//   - FDKsbrEnc_Downsample     (sbr_encoder.cpp:1204-1275)
//   - sbrEncoder_EncodeFrame   (sbr_encoder.cpp:2383-2406)
//   - sbrEncoder_UpdateBuffers (sbr_encoder.cpp:2408-2447)
//
// HE-AAC v1 only — see the exclusion list atop enc_sbr_encoder.go. fdk-aac SBR
// is FIXED-POINT — byte-identical bitstream.
package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// Delay-balancing macros for the HE-AAC v1 (non-LD, non-PS) path
// (sbr_encoder.cpp:132-191). dwnsmp == downSampleFactor.
func sbrSFB(dwnsmp int) int { return 32 << (dwnsmp - 1) }

// stsSlots == STS(fl): 32 for fl==1024, else 30.
func stsSlots(fl int) int {
	if fl == 1024 {
		return 32
	}
	return 30
}

func delayQmfAna(dwnsmp int) int { return (320 << (dwnsmp - 1)) - (32 << (dwnsmp - 1)) }
func delayDecQmf(dwnsmp int) int { return 6 * sbrSFB(dwnsmp) }
func delayQmfSyn(dwnsmp int) int { return 1 << (dwnsmp - 1) }

// delaySBR == DELAY_SBR(fl,dwnsmp).
func delaySBR(fl, dwnsmp int) int {
	return delayQmfAna(dwnsmp) + (sbrSFB(dwnsmp)*stsSlots(fl) - 1) + delayQmfSyn(dwnsmp)
}

// delayCorepathSBR == DELAY_COREPATH_SBR(fl,dwnsmp).
func delayCorepathSBR(dwnsmp int) int {
	return delayQmfAna(dwnsmp) + delayDecQmf(dwnsmp) + delayQmfSyn(dwnsmp)
}

// HE-AAC v2 (PS) delay macros (sbr_encoder.cpp:142-175).
const (
	delayHybAna = 10 * 64   // DELAY_HYB_ANA
	delayHybSyn = 6*64 - 32 // DELAY_HYB_SYN
	delayQmfDS  = 32        // DELAY_QMF_DS
)

// delayPS == DELAY_PS(fl,dwnsmp).
func delayPS(fl, dwnsmp int) int {
	return delayQmfAna(dwnsmp) + delayHybAna + delayDecQmf(dwnsmp) +
		(sbrSFB(dwnsmp)*stsSlots(fl) - 1) + delayHybSyn + delayQmfSyn(dwnsmp)
}

// delayCorepathPS == DELAY_COREPATH_PS(fl,dwnsmp).
func delayCorepathPS(dwnsmp int) int {
	return delayQmfAna(dwnsmp) + delayQmfDS + delayQmfAna(dwnsmp) +
		delayDecQmf(dwnsmp) + delayHybSyn + delayQmfSyn(dwnsmp)
}

const maxDSFilterDelay = 5 // MAX_DS_FILTER_DELAY

// delayParam is the 1:1 port of struct DELAY_PARAM (sbr_encoder.cpp:196-207).
type delayParam struct {
	dsDelay         int
	delay           int
	sbrDecDelay     int
	corePathOffset  int
	sbrPathOffset   int
	bitstrDelay     int
	delayInput2Core int
}

// bsBufInit is the 1:1 port of FDKsbrEnc_bsBufInit (sbr_encoder.cpp:1637-1647):
// points the SBR bit-writer at payloadDelayLine[nBitstrDelay].
func bsBufInit(hSbrElement *SbrElement, nBitstrDelay int) {
	hSbrElement.CmonData.SbrBitbuf = NewFdkWriteBitStream(hSbrElement.PayloadDelayLine[nBitstrDelay])
}

// EnvInit is the 1:1 port of FDKsbrEnc_EnvInit (sbr_encoder.cpp:1658-1852).
// HE-AAC v1: aot == AOT_SBR (no SBR_SYNTAX_LOW_DELAY / SBR_SYNTAX_CRC),
// fParametricStereo == 0, headerPeriod is the AAC header repetition rate.
// Returns the core bandwidth and an error status (0 == ok).
func EnvInit(hSbrElement *SbrElement, params *SbrConfiguration, headerPeriod int, statesInitFlag bool) (coreBandWidth, errStatus int) {
	if params.NChannels < 1 || params.NChannels > maxNumChannels {
		return 0, 1
	}

	cfg := &hSbrElement.SbrConfigData
	hdr := &hSbrElement.SbrHeaderData
	bsd := &hSbrElement.SbrBitstrData

	// init and set syntax flags. AOT_SBR: no LD, no CRC.
	cfg.SbrSyntaxFlags = 0

	cfg.NoQmfBands = 64 >> (2 - params.DownSampleFactor)
	switch cfg.NoQmfBands {
	case 64:
		cfg.NoQmfSlots = params.SbrFrameSize >> 6
	case 32:
		cfg.NoQmfSlots = params.SbrFrameSize >> 5
	default:
		cfg.NoQmfSlots = params.SbrFrameSize >> 6
		return 0, 2
	}

	cfg.NChannels = params.NChannels

	if params.NChannels == 2 {
		if hSbrElement.ElInfo.ElType == idCPE && hSbrElement.ElInfo.FDualMono == 1 {
			cfg.StereoMode = SbrLeftRight
		} else {
			cfg.StereoMode = params.StereoMode
		}
	} else {
		cfg.StereoMode = SbrMono
	}

	cfg.FrameSize = params.SbrFrameSize
	cfg.SampleFreq = params.DownSampleFactor * params.SampleFreq

	bsd.CountSendHeaderData = 0
	if params.SendHeaderDataTime > 0 {
		if headerPeriod == -1 {
			bsd.NrSendHeaderData = params.SendHeaderDataTime * cfg.SampleFreq / (1000 * cfg.FrameSize)
			bsd.NrSendHeaderData = nativeaac.FMaxI(bsd.NrSendHeaderData, 1)
		} else {
			bsd.NrSendHeaderData = nativeaac.FMinI(nativeaac.FMaxI(headerPeriod, 1), cfg.SampleFreq/cfg.FrameSize)
		}
	} else {
		bsd.NrSendHeaderData = 0
	}

	hdr.SbrDataExtra = params.SbrDataExtra
	bsd.HeaderActive = 0
	bsd.RightBorderFIX = 0
	hdr.SbrStartFrequency = params.StartFreq
	hdr.SbrStopFrequency = params.StopFreq
	hdr.SbrXoverBand = 0
	hdr.SbrLcStereoMode = 0

	if params.SbrXposCtrl != sbrXposCtrlDefault {
		hdr.SbrDataExtra = 1
	}

	hdr.SbrAmpRes = AmpRes(params.AmpRes)
	cfg.InitAmpResFF = params.InitAmpResFF

	hdr.FreqScale = int(params.FreqScale)
	hdr.AlterScale = params.AlterScale
	hdr.SbrNoiseBands = params.SbrNoiseBands
	hdr.HeaderExtra1 = 0

	if params.FreqScale != sbrFreqScaleDefault || params.AlterScale != sbrAlterScaleDefault ||
		params.SbrNoiseBands != sbrNoiseBandsDefault {
		hdr.HeaderExtra1 = 1
	}

	hdr.SbrLimiterBands = params.SbrLimiterBands
	hdr.SbrLimiterGains = params.SbrLimiterGains

	if cfg.SampleFreq > 48000 && hdr.SbrStartFrequency >= 9 {
		hdr.SbrLimiterGains = sbrLimiterGainsInfinite
	}

	hdr.SbrInterpolFreq = params.SbrInterpolFreq
	hdr.SbrSmoothingLength = params.SbrSmoothingLength
	hdr.HeaderExtra2 = 0

	if params.SbrLimiterBands != sbrLimiterBandsDefault ||
		params.SbrLimiterGains != sbrLimiterGainsDefault ||
		params.SbrInterpolFreq != sbrInterpolFreqDefault ||
		params.SbrSmoothingLength != sbrSmoothingLengthDefault {
		hdr.HeaderExtra2 = 1
	}

	cfg.UseWaveCoding = params.UseWaveCoding
	cfg.UseParametricCoding = params.ParametricCoding
	cfg.ThresholdAmpResFFm = params.ThresholdAmpResFFm
	cfg.ThresholdAmpResFFe = params.ThresholdAmpResFFe

	// Allocate the band tables (sbrenc_ram: LO = MAX_FREQ_COEFFS/2+1, HI/master
	// = MAX_FREQ_COEFFS+1). v_k_master is shared by InitTonCorr/ResetTonCorr.
	cfg.FreqBandTable[loRes] = make([]uint8, encMaxFreqCoeffs/2+1)
	cfg.FreqBandTable[hiRes] = make([]uint8, encMaxFreqCoeffs+1)
	cfg.VKMaster = make([]uint8, encMaxFreqCoeffs+1)

	if updateFreqBandTable(cfg, hdr, params.DownSampleFactor) != 0 {
		return 0, 1
	}

	// Create + init envelope extractors and QMF analysis for each channel.
	for ch := 0; ch < cfg.NChannels; ch++ {
		if hSbrElement.SbrChannel[ch] == nil {
			hSbrElement.SbrChannel[ch] = new(SbrEncChannel)
		}
		createEnvChannel(&hSbrElement.SbrChannel[ch].HEnvChannel, ch)
		if initEnvChannel(cfg, hdr, &hSbrElement.SbrChannel[ch].HEnvChannel, params, statesInitFlag) != 0 {
			return 0, 1
		}
	}

	// Reset + initialise analysis QMF. HE-AAC v2 (fParametricStereo) needs TWO
	// analysis banks (the stereo input channels) even though the element is a mono
	// SCE; HE-AAC v1 allocates one per core channel (sbr_encoder.cpp:1610-1616).
	nQmf := cfg.NChannels
	if hSbrElement.ElInfo.FParametricStereo != 0 {
		nQmf = 2
	}
	for ch := 0; ch < nQmf; ch++ {
		if hSbrElement.HQmfAnalysis[ch] == nil {
			hSbrElement.HQmfAnalysis[ch] = new(FilterBank)
		}
		if statesInitFlag || hSbrElement.qmfStates[ch] == nil {
			// Analysis polyphase delay line: 10*no_channels (FDK_qmf_domain).
			hSbrElement.qmfStates[ch] = make([]int32, 10*cfg.NoQmfBands)
		}
		// !(SBR_SYNTAX_LOW_DELAY): qmfFlags = 0; KEEP_STATES toggled by init flag
		// but the Go bank reinitialises from the (retained) states slice either way.
		if InitAnalysisFilterBank(hSbrElement.HQmfAnalysis[ch], hSbrElement.qmfStates[ch],
			cfg.NoQmfSlots, cfg.NoQmfBands, cfg.NoQmfBands, cfg.NoQmfBands, 0) != 0 {
			return 0, 1
		}
	}

	// dynamic bandwidth excluded (dynBwEnabled defaults off): CmonData.xOverFreq /
	// dynXOverFreqEnc / dynXOverFreqDelay tracking is not referenced in v1.
	cfg.DynXOverFreq = cfg.XOverFreq

	// Bandwidth to be passed to the core encoder.
	coreBandWidth = cfg.XOverFreq
	return coreBandWidth, 0
}

// sbrEncoderInitDelay is the 1:1 port of sbrEncoder_Init_delay
// (sbr_encoder.cpp:1950-2056). lowDelay == is212 == false (HE-AAC GA only). usePs
// selects the PS (HE-AAC v2) delay path; downsamplingMethod is SBRENC_DS_TIME for
// v1 and SBRENC_DS_QMF for PS.
func sbrEncoderInitDelay(coreFrameLength, numChannels, downSampleFactor, usePs, downsamplingMethod int, dp *delayParam) {
	delayCore := dp.delay

	flCore := coreFrameLength

	var delayCorePath, delaySbrPath, delaySbrDec int
	if usePs != 0 {
		delayCorePath = delayCorepathPS(downSampleFactor)
		delaySbrPath = delayPS(flCore, downSampleFactor)
		delaySbrDec = delayCorepathSBR(downSampleFactor)
	} else {
		delayCorePath = delayCorepathSBR(downSampleFactor)
		delaySbrPath = delaySBR(flCore, downSampleFactor)
		delaySbrDec = delayCorepathSBR(downSampleFactor)
	}

	delayCorePath += delayCore * downSampleFactor
	if downsamplingMethod == sbrencDSTime {
		delayCorePath += dp.dsDelay
	}

	corePathOffset := 0
	sbrPathOffset := 0
	bitstreamDelay := 0

	// QMF-downsampling path coupling (sbr_encoder.cpp:1998-2012).
	if downsamplingMethod == sbrencDSQMF && delayCorePath > delaySbrPath {
		for delayCorePath > delaySbrPath {
			delaySbrPath += flCore * downSampleFactor
			bitstreamDelay++
		}
	}

	if delayCorePath > delaySbrPath {
		for delayCorePath > delaySbrPath+flCore*downSampleFactor {
			delaySbrPath += flCore * downSampleFactor
			bitstreamDelay++
		}
		corePathOffset = 0
		sbrPathOffset = (delayCorePath - delaySbrPath) * numChannels
	} else {
		corePathOffset = ((delaySbrPath - delayCorePath) * numChannels) >> (downSampleFactor - 1)
		sbrPathOffset = 0
	}

	var delayInput2Core int
	switch {
	case usePs != 0:
		// DELAY_QMF_ANA + DELAY_QMF_DS + DELAY_HYB_SYN + dwnsmp*corePathOffset + 1.
		delayInput2Core = (delayQmfAna(downSampleFactor) + delayQmfDS + delayHybSyn) +
			(downSampleFactor * corePathOffset) + 1
	case downsamplingMethod == sbrencDSTime:
		delayInput2Core = corePathOffset + dp.dsDelay
	default:
		delayInput2Core = corePathOffset
	}

	dp.delay = nativeaac.FMaxI(delayCorePath, delaySbrPath)
	dp.sbrDecDelay = delaySbrDec
	dp.delayInput2Core = delayInput2Core
	dp.bitstrDelay = bitstreamDelay
	dp.corePathOffset = corePathOffset
	dp.sbrPathOffset = sbrPathOffset
}
