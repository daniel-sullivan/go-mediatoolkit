// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Exported AAC-LC CBR encoder entry point: NewEncoder runs the init path
// (AacInitDefaultConfig + Open + Initialize, all 1:1 ports in aacenc_init.go)
// once, then EncodeOneFrame drives FDKaacEnc_EncodeFrame (aacenc_frame.go) per
// frame. This binds the internal/nativeaac encode core to the aacfdk seam in
// libraries/aac (native_engine_encode_aacfdk.go).
//
// Scope: AAC-LC (AOT_AAC_LC), CBR, raw access units (TT_MP4_RAW), 1024-sample
// frames, 1..2 channels (SCE / CPE). The high-level aacEncoder_lib resolution of
// channelMode / framelength is reproduced here for the AAC-LC mono/stereo case.

package nativeaac

// Encoder is the stateful pure-Go AAC-LC CBR encoder handle. It owns the
// AAC_ENC state graph and a reusable raw-AU transport; one Encoder encodes a
// sequence of frames for a fixed (sampleRate, channels, bitrate) config.
type Encoder struct {
	enc          *AacEnc
	channels     int
	frameLength  int
	inputBufSize int
	input        []int16 // planar int16 scratch: channels * inputBufSize

	// inputDelay is the block-switching look-ahead offset (2*nTimeSamples -
	// blockSwitchingOffset, == 448 for AAC-LC 1024-sample frames). The genuine
	// aacEncEncode presents psyMain a one-frame inputBuffer in which the current
	// frame's PCM is shifted right by inputDelay samples, the leading inputDelay
	// samples carrying the previous frame's tail (psyMain's psyInputBuffer
	// rotation expects this offset). delayLine[ch] holds those carried
	// last-inputDelay samples per channel; on a cold start it is zero, so the
	// first frame is primed exactly as the genuine encoder.
	inputDelay int
	delayLine  []int16 // channels * inputDelay carried samples (planar)
}

// NewEncoder builds and initialises an AAC-LC CBR encoder for the given sample
// rate, channel count and bitrate (bits/s). It mirrors the high-level
// aacEncoder_lib AAC-LC setup: AOT_AAC_LC, 1024-sample frames, channelMode from
// the channel count (MODE_1 mono / MODE_2 stereo), one element.
func NewEncoder(sampleRate, channels, bitRate int) (*Encoder, EncoderError) {
	return newEncoderMode(sampleRate, channels, bitRate, AacBitrateModeCBR)
}

// NewEncoderVBR builds and initialises an AAC-LC VBR encoder driven by the given
// bitrate MODE (AacBitrateModeVBR1..5). It mirrors the high-level
// aacEncoder_lib VBR setup (aacenc_lib.cpp:1026-1042): the nominal bitRate is
// derived from the mode + channel mode via FDKaacEnc_GetVBRBitrate, and the core
// encoder is then driven by the bitrate mode (the rate-control selects the VBR
// threshold-reduction path instead of the CBR PE-based one). bitrateMode must be
// one of the five VBR modes; otherwise AacEncUnsupportedBitrateMode is returned.
func NewEncoderVBR(sampleRate, channels int, bitrateMode AacencBitrateMode) (*Encoder, EncoderError) {
	if !aacBrModeIsVBR(bitrateMode) {
		return nil, AacEncUnsupportedBitrateMode
	}
	return newEncoderMode(sampleRate, channels, 0, bitrateMode)
}

// newEncoderMode is the shared CBR/VBR encoder builder. For CBR it uses the
// supplied bitRate directly; for VBR it derives the nominal bitRate from the
// mode + channel mode (FDKaacEnc_GetVBRBitrate), matching the aacEncoder_lib
// SetupAfterConfig bitrate resolution.
func newEncoderMode(sampleRate, channels, bitRate int, bitrateMode AacencBitrateMode) (*Encoder, EncoderError) {
	if channels < 1 || channels > 2 {
		return nil, AacEncUnsupportedChannelconf
	}

	var config AacencConfig
	AacInitDefaultConfig(&config)
	config.AudioObjectType = int(aotAACLC)
	config.NChannels = channels
	if channels == 1 {
		config.ChannelMode = ChannelMode1
	} else {
		config.ChannelMode = ChannelMode2
	}
	config.SampleRate = sampleRate
	config.FrameLength = 1024
	config.BitrateMode = bitrateMode

	if aacBrModeIsVBR(bitrateMode) {
		// VBR: nominal bitrate from mode + channel mode (aacenc_lib.cpp:1041).
		// AacencBitrateMode shares identical integer values with BitrateMode.
		config.BitRate = GetVBRBitrate(BitrateMode(bitrateMode), config.ChannelMode)
		// VBR runs without PNS (aacenc_lib.cpp:1172-1177: "VBR without PNS").
		config.UsePns = false
	} else {
		config.BitRate = bitRate
	}

	nElements := 1
	hAacEnc, err := Open(nElements, channels, config.NSubFrames)
	if err != AacEncOK {
		return nil, err
	}

	// Initialize with a nil StaticBitsProvider (TT_MP4_RAW -> 0 static bits) and
	// a cold-start reset (initFlags=1).
	if err := Initialize(hAacEnc, &config, nil, 1); err != AacEncOK {
		return nil, err
	}

	frameLength := config.FrameLength

	// inputDelay = 2*nTimeSamples - blockSwitchingOffset, the AAC-LC FB_LC
	// block-switching look-ahead the genuine aacEncEncode applies to the input
	// fed to FDKaacEnc_psyMain (psy_main.cpp:457 blockSwitchingOffset =
	// nTimeSamples + 9*nTimeSamples/(2*TRANS_FAC)). For 1024-sample frames this
	// is 2*1024 - (1024 + 9*1024/16) == 448.
	blockSwitchingOffset := frameLength + (9 * frameLength / (2 * encTransFac))
	inputDelay := 2*frameLength - blockSwitchingOffset

	return &Encoder{
		enc:          hAacEnc,
		channels:     channels,
		frameLength:  frameLength,
		inputBufSize: frameLength,
		input:        make([]int16, channels*frameLength),
		inputDelay:   inputDelay,
		delayLine:    make([]int16, channels*inputDelay),
	}, AacEncOK
}

// FrameLength returns the per-channel samples one EncodeOneFrame call consumes.
func (e *Encoder) FrameLength() int { return e.frameLength }

// EncodeOneFrame encodes one frame of interleaved int16 PCM
// (len == channels*frameLength) into a raw AAC-LC access unit. The interleaved
// input is de-interleaved into the planar channel layout EncodeFrame's chIdx
// addressing expects (channel c at input[c*inputBufSize : (c+1)*inputBufSize]).
//
// The per-channel planar buffer reproduces the genuine aacEncEncode input
// layout: the current frame's PCM is shifted right by inputDelay samples (the
// block-switching look-ahead), the leading inputDelay samples carrying the
// previous frame's tail (delayLine). On a cold start delayLine is zero, so the
// first frame is primed with inputDelay leading zeros exactly as the genuine
// encoder, and FDKaacEnc_psyMain's psyInputBuffer rotation then carries the
// look-ahead identically frame-to-frame.
func (e *Encoder) EncodeOneFrame(interleaved []int16) ([]byte, EncoderError) {
	if len(interleaved) < e.channels*e.frameLength {
		return nil, AacEncInvalidHandle
	}
	lead := e.inputDelay
	keep := e.frameLength - lead // current samples that fit after the lead
	for ch := 0; ch < e.channels; ch++ {
		dst := e.input[ch*e.inputBufSize : ch*e.inputBufSize+e.frameLength]
		dl := e.delayLine[ch*lead : ch*lead+lead]

		// leading inputDelay samples = previous frame's carried tail
		copy(dst[:lead], dl)
		// remaining samples = this frame's first (frameLength - inputDelay)
		for i := 0; i < keep; i++ {
			dst[lead+i] = interleaved[i*e.channels+ch]
		}
		// carry this frame's last inputDelay samples for the next call
		for i := 0; i < lead; i++ {
			dl[i] = interleaved[(keep+i)*e.channels+ch]
		}
	}

	hTpEnc := newRawTransportEnc()
	return EncodeFrame(e.enc, hTpEnc, e.input, uint(e.inputBufSize), nil)
}
