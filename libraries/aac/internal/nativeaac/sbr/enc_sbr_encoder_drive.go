// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of the SBR-encoder PER-FRAME DRIVER + top-level Init of
// libSBRenc/src/sbr_encoder.cpp (see the per-func citations below). HE-AAC v1
// only. fdk-aac SBR is FIXED-POINT — byte-identical bitstream.
package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// SbrEncoderInit is the 1:1 port of sbrEncoder_Init (sbr_encoder.cpp:2065-2381)
// fused with the single-element allocation that sbrEncoder_Open performs, for
// the HE-AAC v1 single SCE/CPE case (usePs == lowDelay == is212 == false,
// downSampleFactor == 2 always — sbrEncoder_IsSingleRatePossible(AOT_SBR) is
// true so downsampling is forced to 2 only when not single-rate-possible; for
// AOT_SBR single-rate IS possible, but HE-AAC v1 uses dual-rate, so the caller
// passes downSampleFactor == 2). The genuine encoder picks 2 via aacenc_lib's
// config; here the caller supplies it.
//
// elInfo carries the element type + per-channel bitrate + channel indices.
// coreSampleRate is the INPUT sample rate; on return it is halved (the core
// rate). Returns (coreBandwidth, inputBufferOffset, delay, error).
func SbrEncoderInit(enc *SbrEncoder, elInfo *SbrElementInfo, inputSampleRate, coreFrameLength,
	numChannels, downSampleFactor, headerPeriod, transformFactor, inDelay int, statesInitFlag bool) (
	coreSampleRate, coreBandwidth, inputBufferOffset, delay, errStatus int) {
	return sbrEncoderInitInternal(enc, elInfo, inputSampleRate, coreFrameLength,
		numChannels, downSampleFactor, headerPeriod, transformFactor, inDelay, 0,
		psEncConfig{}, statesInitFlag)
}

// SbrEncoderInitPS is the HE-AAC v2 entry point: same as SbrEncoderInit but with
// usePs == 1 — the element is overridden to a single mono SCE, the core gets the
// stereo->mono downmix, the QMF-domain downsampler is selected, and the
// parametric-stereo wrapper (PSEnc_Create/Init + qmfSynthesisPS) is set up
// (sbr_encoder.cpp:2087-2350). numChannels is the INPUT channel count (2); on
// return the core is mono.
func SbrEncoderInitPS(enc *SbrEncoder, elInfo *SbrElementInfo, inputSampleRate, coreFrameLength,
	numChannels, downSampleFactor, headerPeriod, transformFactor, inDelay int, psCfg PSEncConfig,
	statesInitFlag bool) (coreSampleRate, coreBandwidth, inputBufferOffset, delay, errStatus int) {
	internal := psEncConfig{
		nStereoBands:           psCfg.NStereoBands,
		maxEnvelopes:           psCfg.MaxEnvelopes,
		iidQuantErrorThreshold: psCfg.IidQuantErrorThreshold,
	}
	return sbrEncoderInitInternal(enc, elInfo, inputSampleRate, coreFrameLength,
		numChannels, downSampleFactor, headerPeriod, transformFactor, inDelay, 1,
		internal, statesInitFlag)
}

func sbrEncoderInitInternal(enc *SbrEncoder, elInfo *SbrElementInfo, inputSampleRate, coreFrameLength,
	numChannels, downSampleFactor, headerPeriod, transformFactor, inDelay, usePs int, psCfg psEncConfig,
	statesInitFlag bool) (coreSampleRate, coreBandwidth, inputBufferOffset, delay, errStatus int) {

	// Parametric Stereo: override the element to a single mono SCE; the core
	// encoder gets the downmixed mono signal (sbr_encoder.cpp:2108-2121).
	if usePs != 0 {
		if numChannels != 2 {
			return inputSampleRate >> 1, 0, 0, inDelay, 1
		}
		elInfo.ElType = idSCE
		elInfo.FParametricStereo = 1
		elInfo.NChannelsInEl = 1
		numChannels = 1
	}

	// set the core's sample rate (downSampleFactor == 2 for HE-AAC v1/v2).
	downsamplingMethod := sbrencDSTime
	switch downSampleFactor {
	case 1:
		coreSampleRate = inputSampleRate
		downsamplingMethod = sbrencDSNone
	case 2:
		coreSampleRate = inputSampleRate >> 1
		if usePs != 0 {
			downsamplingMethod = sbrencDSQMF
		} else {
			downsamplingMethod = sbrencDSTime
		}
	default:
		return inputSampleRate >> 1, 0, 0, inDelay, 1
	}

	// Element config feasibility (AOT_SBR => isELD == false).
	if elInfo.ElType != idSCE && elInfo.ElType != idCPE {
		return coreSampleRate, 0, 0, inDelay, 1
	}
	if !isSbrSettingAvail(uint(elInfo.BitRate), 0, uint(elInfo.NChannelsInEl),
		uint(inputSampleRate), uint(coreSampleRate), false) {
		return coreSampleRate, 0, 0, inDelay, 1
	}

	enc.NChannels = numChannels
	enc.FrameSize = coreFrameLength * downSampleFactor
	enc.DownsamplingMethod = downsamplingMethod
	enc.DownSampleFactor = downSampleFactor
	enc.EstimateBitrate = 0
	enc.InputDataDelay = 0
	enc.LfeChIdx = -1
	enc.NoElements = 1

	var sbrConfig SbrConfiguration
	if !initializeSbrDefaults(&sbrConfig, downSampleFactor, coreFrameLength, false) {
		return coreSampleRate, 0, 0, inDelay, 1
	}
	// bParametricStereo (sbr_encoder.cpp:2205) is set on config but never read in
	// the SBR encode path — PS is driven via elInfo.fParametricStereo +
	// hParametricStereo, so adjustSbrSettings is identical for v1/v2 here (the Go
	// port hardcodes config.BParametricStereo == 0; vbrMode stays 0 == CBR).
	if !adjustSbrSettings(&sbrConfig, uint(elInfo.BitRate), uint(elInfo.NChannelsInEl),
		uint(coreSampleRate), uint(inputSampleRate), uint(transformFactor), 24000, 0, false) {
		return coreSampleRate, 0, 0, inDelay, 1
	}

	highestSbrStartFreq := sbrConfig.StartFreq
	highestSbrStopFreq := sbrConfig.StopFreq

	// Allocate the single element.
	if enc.SbrElement[0] == nil {
		enc.SbrElement[0] = new(SbrElement)
	}
	hEl := enc.SbrElement[0]
	hEl.ElInfo = *elInfo
	for i := range hEl.PayloadDelayLine {
		hEl.PayloadDelayLine[i] = make([]byte, maxPayloadSize)
		hEl.PayloadDelayLineSize[i] = 0
	}

	// Use lowest common bandwidth (single element).
	sbrConfig.StartFreq = highestSbrStartFreq
	sbrConfig.StopFreq = highestSbrStopFreq

	bw, e := EnvInit(hEl, &sbrConfig, headerPeriod, statesInitFlag)
	if e != 0 {
		return coreSampleRate, 0, 0, inDelay, 2
	}
	lowestBandwidth := bw

	// Time-domain downsampler per channel — only for SBRENC_DS_TIME (HE-AAC v1);
	// the PS path uses QMF-domain downsampling (sbr_encoder.cpp:2270-2272).
	if downsamplingMethod == sbrencDSTime {
		for ch := 0; ch < int(hEl.ElInfo.NChannelsInEl); ch++ {
			InitDownsampler(&hEl.SbrChannel[ch].DownSampler, 500, downSampleFactor)
		}
	}

	// Delay information.
	var dp delayParam
	dp.dsDelay = hEl.SbrChannel[0].DownSampler.Delay()
	dp.delay = inDelay
	sbrEncoderInitDelay(coreFrameLength, numChannels, downSampleFactor, usePs, downsamplingMethod, &dp)

	enc.NBitstrDelay = dp.bitstrDelay
	enc.SbrDecDelay = dp.sbrDecDelay
	enc.InputDataDelay = dp.delayInput2Core

	coreBandwidth = lowestBandwidth
	enc.EstimateBitrate += 2500 * numChannels

	// Initialise the bitstream buffer for the element.
	bsBufInit(hEl, dp.bitstrDelay)

	// Initialise parametric stereo (sbr_encoder.cpp:2296-2349).
	if usePs != 0 {
		// qmfSynthesisPS: a half-band (noQmfBands>>1) synthesis bank.
		noSlots := hEl.SbrConfigData.NoQmfSlots
		halfBands := hEl.SbrConfigData.NoQmfBands >> 1
		enc.QmfSynthesisPS = new(FilterBank)
		enc.qmfSynthesisPSMem = make([]int32, (2*qmfNoPoly-1)*halfBands)
		flag := uint(0)
		if !statesInitFlag {
			flag = flagKeepStates
		}
		InitSynthesisFilterBank(enc.QmfSynthesisPS, enc.qmfSynthesisPSMem, noSlots,
			halfBands, halfBands, halfBands, flag)

		enc.HParametricStereo = PSEncCreate()
		// psEncConfig.sbrPsDelay = GetEnvEstDelay (carried, not byte-affecting on
		// this GA path); frameSize/qmfFilterMode from the caller-supplied psCfg.
		cfg := psCfg
		cfg.frameSize = coreFrameLength
		if PSEncInit(enc.HParametricStereo, &cfg, noSlots, hEl.SbrConfigData.NoQmfBands) != psencOK {
			return coreSampleRate, 0, 0, inDelay, 4
		}
		// PS bitrate estimate (sbr_encoder.cpp:2315-2319).
		enc.EstimateBitrate += (coreSampleRate * 5 * cfg.nStereoBands * cfg.maxEnvelopes) / enc.FrameSize
	}

	enc.DownsampledOffset = dp.corePathOffset
	enc.BufferOffset = dp.sbrPathOffset
	delay = dp.delay
	enc.DownmixSize = coreFrameLength * numChannels

	enc.SbrElement[0] = hEl
	inputBufferOffset = nativeaac.FMaxI(dp.sbrPathOffset, dp.corePathOffset)
	return coreSampleRate, coreBandwidth, inputBufferOffset, delay, 0
}

// EnvEncodeFrame is the 1:1 port of FDKsbrEnc_EnvEncodeFrame
// (sbr_encoder.cpp:941-1193). samples is the planar int16 input buffer (channel
// stride == samplesBufSize); baseOffset is downsampledOffset/nChannels. On
// return, sbrData (if non-nil) receives the bytes of payloadDelayLine[0] and
// sbrDataBits its bit length. clearOutput drives the delay-fill passes.
func EnvEncodeFrame(enc *SbrEncoder, iElement int, samples []int16, baseOffset, samplesBufSize int,
	sbrData []byte, clearOutput int) (sbrDataBits int, errStatus int) {

	hSbrElement := enc.SbrElement[iElement]
	if hSbrElement == nil {
		return 0, -1
	}
	cfg := &hSbrElement.SbrConfigData
	hdr := &hSbrElement.SbrHeaderData
	bsd := &hSbrElement.SbrBitstrData
	cmon := &hSbrElement.CmonData

	psHeaderActive := 0
	bsd.HeaderActive = 0

	// Anticipate PS header because of internal PS bitstream delay in order to be
	// in sync with the SBR header (sbr_encoder.cpp:970-973).
	if bsd.CountSendHeaderData == bsd.NrSendHeaderData-1 {
		psHeaderActive = 1
	}

	// Signal SBR header to be written into bitstream.
	if bsd.CountSendHeaderData == 0 {
		bsd.HeaderActive = 1
	}
	if bsd.NrSendHeaderData == 0 {
		bsd.CountSendHeaderData = 1
	} else {
		if bsd.CountSendHeaderData >= 0 {
			bsd.CountSendHeaderData++
			bsd.CountSendHeaderData %= bsd.NrSendHeaderData
		}
	}

	// dynamic bandwidth excluded (CmonData.dynBwEnabled == 0).

	// Allocate space for dummy header and crc (HE-AAC v1: InitSbrBitstream just
	// resets the bit-buffer and returns 0).
	cmon.SbrBitbuf = NewFdkWriteBitStream(hSbrElement.PayloadDelayLine[enc.NBitstrDelay])
	InitSbrBitstream(cmon, cfg.SbrSyntaxFlags)

	var fData SbrFrameTempData
	eData := make([]SbrEnvTempData, maxNumChannels)
	for i := 0; i < encMaxNumNoiseValues; i++ {
		fData.Res[i] = FreqResHigh
	}

	ps := enc.HParametricStereo
	if clearOutput == 0 {
		for ch := 0; ch < cfg.NChannels; ch++ {
			hEnvChan := &hSbrElement.SbrChannel[ch].HEnvChannel
			sbrExtrEnv := &hEnvChan.SbrExtractEnvelope

			if hSbrElement.ElInfo.FParametricStereo == 0 {
				// QMF analysis (fParametricStereo == 0).
				off := baseOffset + int(hSbrElement.ElInfo.ChannelIndex[ch])*samplesBufSize
				lbScale := analysisFilterFromPCM(hSbrElement.HQmfAnalysis[ch], sbrExtrEnv,
					samples, off, cfg.NoQmfSlots, cfg.NoQmfBands)
				hEnvChan.QmfScale = lbScale + 7
			} else {
				// Parametric stereo processing (sbr_encoder.cpp:1096-1128): stereo
				// QMF + hybrid analysis, PS parameter extraction, downmix + hybrid
				// synthesis. Output (the downmixed QMF) is written into
				// sbrExtrEnv.rBuffer/iBuffer; the downsampled mono time signal is
				// written back into the core's channel base (DS_QMF). ch == 0 only.
				ch0 := int(hSbrElement.ElInfo.ChannelIndex[0]) * samplesBufSize
				ch1 := int(hSbrElement.ElInfo.ChannelIndex[1]) * samplesBufSize
				var pSamples [maxPsChannels][]int16
				pSamples[0] = samples[baseOffset+ch0:]
				pSamples[1] = samples[baseOffset+ch1:]
				var hQmf [maxPsChannels]*FilterBank
				hQmf[0] = hSbrElement.HQmfAnalysis[0]
				hQmf[1] = hSbrElement.HQmfAnalysis[1]
				down := samples[baseOffset+int(hSbrElement.ElInfo.ChannelIndex[ch])*samplesBufSize:]
				var qmfScale int
				PSEncParametricStereoProcessing(ps, pSamples, hQmf,
					sbrExtrEnv.RBuffer[:], sbrExtrEnv.IBuffer[:], down,
					enc.QmfSynthesisPS, &qmfScale, psHeaderActive)
				hEnvChan.QmfScale = qmfScale
			}

			ExtractSbrEnvelope1(cfg, hEnvChan, &eData[ch])
		}
	}

	var hEnvChan0, hEnvChan1 *EnvChannel
	hEnvChan0 = &hSbrElement.SbrChannel[0].HEnvChannel
	if cfg.StereoMode != SbrMono {
		hEnvChan1 = &hSbrElement.SbrChannel[1].HEnvChannel
	}

	// HE-AAC v2: pass the PS handle only when this element carries PS, mirroring
	// the (fParametricStereo) ? hParametricStereo : NULL gate (sbr_encoder.cpp:1149).
	var psForWrite *ParametricStereo
	if hSbrElement.ElInfo.FParametricStereo != 0 {
		psForWrite = ps
	}
	ExtractSbrEnvelope2(cfg, hdr, psForWrite, bsd, hEnvChan0, hEnvChan1, cmon, eData, &fData, clearOutput)

	bsd.RightBorderFIX = 0

	// Format payload (HE-AAC v1: GA byte-alignment, no CRC).
	AssembleSbrBitstream(cmon, cfg.SbrSyntaxFlags)

	// Save new payload; zero-length if greater than MAX_PAYLOAD_SIZE.
	validBits := int(cmon.SbrBitbuf.GetValidBits())
	hSbrElement.PayloadDelayLineSize[enc.NBitstrDelay] = validBits
	if hSbrElement.PayloadDelayLineSize[enc.NBitstrDelay] > maxPayloadSize<<3 {
		hSbrElement.PayloadDelayLineSize[enc.NBitstrDelay] = 0
	}

	if sbrData != nil {
		sbrDataBits = hSbrElement.PayloadDelayLineSize[0]
		copy(sbrData, hSbrElement.PayloadDelayLine[0][:(hSbrElement.PayloadDelayLineSize[0]+7)>>3])
	}

	// delay header active flag (HeaderActiveDelay) — unused downstream in v1
	// (single-frame bitstream delay), but tracked for fidelity.
	if bsd.HeaderActive == 1 {
		bsd.HeaderActiveDelay = 1 + enc.NBitstrDelay
	} else if bsd.HeaderActiveDelay > 0 {
		bsd.HeaderActiveDelay--
	}

	return sbrDataBits, 0
}

// analysisFilterFromPCM runs the per-channel QMF analysis over one frame of
// planar int16 PCM, writing the complex subband matrices into sbrExtrEnv.RBuffer
// /IBuffer and returning lb_scale (== tmpScale.lb_scale). The int16 input is
// widened to int32 FIXP_QAS (timeInE == 0, stride == 1) exactly as the SBR
// encoder calls qmfAnalysisFiltering (sbr_encoder.cpp:1082-1085).
func analysisFilterFromPCM(fb *FilterBank, sbrExtrEnv *SbrExtractEnvelope, samples []int16,
	off, noCols, noChannels int) int {

	timeIn := make([]int32, noCols*noChannels)
	for i := 0; i < noCols*noChannels; i++ {
		// SAMPLE_BITS == 16: the genuine SBR encoder feeds INT_PCM (int16) into the
		// 16-bit FIXP_QAS analysis QMF (qmf_pcm.h:543, "Place INT_PCM value left
		// aligned"). The native AnalysisFiltering runs the 32-bit FIXP_QAS path,
		// so the int16 sample must be left-aligned into the int32 word (<< 16) to
		// carry the same magnitude; without it the QMF output mantissa is 2^16
		// smaller and lb_scale/energyScale land 16/32 higher, which clamps the
		// transient-detector energies to ~0 and suppresses onset transients.
		timeIn[i] = int32(samples[off+i]) << 16
	}

	qmfReal := make([][]int32, noCols)
	qmfImag := make([][]int32, noCols)
	for i := 0; i < noCols; i++ {
		qmfReal[i] = sbrExtrEnv.RBuffer[i]
		qmfImag[i] = sbrExtrEnv.IBuffer[i]
	}

	var sf ScaleFactor
	workBuffer := make([]int32, 2*noChannels)
	AnalysisFiltering(fb, qmfReal, qmfImag, &sf, timeIn, 0, 1, workBuffer)
	return sf.LbScale
}

// sbrEncoderDownsample is the 1:1 port of FDKsbrEnc_Downsample
// (sbr_encoder.cpp:1204-1275) for downSampleFactor > 1, SBRENC_DS_TIME. It
// downsamples the SBR-domain input back into the start of the same planar buffer
// (the core's signal). The input window starts bufferOffset/numChannels past the
// channel base; the output (the downsampled core signal) overwrites from the
// channel base. The resampler reads sequentially and writes behind, so the
// in-place overlap is safe (same as the C memmove-free path).
func sbrEncoderDownsample(enc *SbrEncoder, samples []int16, samplesBufSize, numChannels int) {
	if enc.DownSampleFactor <= 1 {
		return
	}
	// HE-AAC v2 (DS_QMF): the downsampled mono core is produced by the PS downmix
	// (DownmixPSQmfData's QMF synthesis), so the time-domain downsampler does not
	// run (sbr_encoder.cpp:2270-2272 only inits/runs it for SBRENC_DS_TIME).
	if enc.DownsamplingMethod != sbrencDSTime {
		return
	}
	for el := 0; el < enc.NoElements; el++ {
		hSbrElement := enc.SbrElement[el]
		if hSbrElement == nil {
			continue
		}
		nChannels := hSbrElement.SbrConfigData.NChannels
		frameSize := hSbrElement.SbrConfigData.FrameSize
		for ch := 0; ch < nChannels; ch++ {
			chBase := int(hSbrElement.ElInfo.ChannelIndex[ch]) * samplesBufSize
			in := samples[chBase+enc.BufferOffset/numChannels:]
			out := samples[chBase:]
			Downsample(&hSbrElement.SbrChannel[ch].DownSampler, in, frameSize, out)
		}
	}
}

// SbrEncoderEncodeFrame is the 1:1 port of sbrEncoder_EncodeFrame
// (sbr_encoder.cpp:2383-2406). For the single HE-AAC v1 element it runs the
// envelope encoder then the time-domain downsampler. samples is the planar int16
// input buffer; on return sbrData carries the element's payload bytes.
func SbrEncoderEncodeFrame(enc *SbrEncoder, samples []int16, samplesBufSize int, sbrData []byte) (sbrDataBits int, errStatus int) {
	baseOffset := enc.DownsampledOffset / enc.NChannels
	for el := 0; el < enc.NoElements; el++ {
		if enc.SbrElement[el] != nil {
			bits, e := EnvEncodeFrame(enc, el, samples, baseOffset, samplesBufSize, sbrData, 0)
			if e != 0 {
				return 0, e
			}
			sbrDataBits = bits
		}
	}
	sbrEncoderDownsample(enc, samples, samplesBufSize, enc.NChannels)
	return sbrDataBits, 0
}

// SbrEncoderUpdateBuffers is the 1:1 port of sbrEncoder_UpdateBuffers
// (sbr_encoder.cpp:2408-2447): shifts the delayed input/downsampled data and
// advances the per-element payload delay lines.
func SbrEncoderUpdateBuffers(enc *SbrEncoder, samples []int16, samplesBufSize int) {
	if enc.DownsampledOffset > 0 {
		nd := enc.DownmixSize / enc.NChannels
		for c := 0; c < enc.NChannels; c++ {
			n := enc.DownsampledOffset / enc.NChannels
			copy(samples[samplesBufSize*c:samplesBufSize*c+n], samples[samplesBufSize*c+nd:samplesBufSize*c+nd+n])
		}
	} else {
		for c := 0; c < enc.NChannels; c++ {
			n := enc.BufferOffset / enc.NChannels
			copy(samples[samplesBufSize*c:samplesBufSize*c+n], samples[samplesBufSize*c+enc.FrameSize:samplesBufSize*c+enc.FrameSize+n])
		}
	}
	if enc.NBitstrDelay > 0 {
		for el := 0; el < enc.NoElements; el++ {
			hEl := enc.SbrElement[el]
			// FDKmemmove payloadDelayLine[0] <- payloadDelayLine[1..nBitstrDelay].
			for i := 0; i < enc.NBitstrDelay; i++ {
				copy(hEl.PayloadDelayLine[i], hEl.PayloadDelayLine[i+1])
				hEl.PayloadDelayLineSize[i] = hEl.PayloadDelayLineSize[i+1]
			}
		}
	}
}

// DelayCompensation is the 1:1 port of FDKsbrEnc_DelayCompensation
// (sbr_encoder.cpp:1867-1883): runs nBitstrDelay dummy (clearOutput) frames to
// fill the bitstream delay lines with the zero input signal.
func DelayCompensation(enc *SbrEncoder, samples []int16, samplesBufSize int) int {
	baseOffset := enc.DownsampledOffset / enc.NChannels
	for n := enc.NBitstrDelay; n > 0; n-- {
		for el := 0; el < enc.NoElements; el++ {
			if _, e := EnvEncodeFrame(enc, el, samples, baseOffset, samplesBufSize, nil, 1); e != 0 {
				return -1
			}
		}
		SbrEncoderUpdateBuffers(enc, samples, samplesBufSize)
	}
	return 0
}
