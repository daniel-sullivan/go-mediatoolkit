// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Core-encoder seam for the HE-AAC v1 (AOT_SBR) integration: a constructor that
// builds the AAC-LC core for the *downsampled* signal + SBR-reduced bandwidth +
// PNS-off (the parameter changes aacenc_lib.cpp:1303-1359 applies once the SBR
// encoder takes a look at the configuration), and a planar encode entry that
// runs FDKaacEnc_EncodeFrame over an externally-managed input buffer with the
// SBR EXT_SBR_DATA extension payload injected (aacenc_lib.cpp:1928-1991).
//
// The big input-buffer ring (INPUTBUFFER_SIZE) and the SBR encode/downsample/
// delay flow live in the heaac package; this seam only exposes the core pieces.
package nativeaac

// SbrCoreEncoder is a thin AAC-LC core configured for the SBR core path: it owns
// the AAC_ENC graph at the (halved) core sample rate, with bandwidth limited to
// the SBR crossover and PNS disabled, and runs one raw core access unit per
// frame directly over an externally-supplied planar int16 input buffer.
type SbrCoreEncoder struct {
	enc          *AacEnc
	channels     int
	frameLength  int
	inputBufSize int
}

// NewSbrCoreEncoder builds the AAC-LC core for the HE-AAC v1 path. coreSampleRate
// is the downsampled (half) rate; bandWidth is the SBR crossover frequency (the
// core lowpass); bitRate is the total element bitrate; inputBufSize is the SBR
// input-buffer per-channel stride (INPUTBUFFER_SIZE) the planar encode reads
// from. PNS is forced off (aacenc_lib.cpp:1353-1356). frameLength is the core
// frame (1024).
func NewSbrCoreEncoder(coreSampleRate, channels, bitRate, bandWidth, ancDataBitRate, inputBufSize int) (*SbrCoreEncoder, EncoderError) {
	if channels < 1 || channels > 2 {
		return nil, AacEncUnsupportedChannelconf
	}

	var config AacencConfig
	AacInitDefaultConfig(&config)
	config.AudioObjectType = int(aotAACLC) // the SBR core is an AAC-LC core
	config.NChannels = channels
	if channels == 1 {
		config.ChannelMode = ChannelMode1
	} else {
		config.ChannelMode = ChannelMode2
	}
	config.SampleRate = coreSampleRate
	config.FrameLength = 1024
	config.BitrateMode = AacBitrateModeCBR
	config.BitRate = bitRate
	config.BandWidth = bandWidth           // SBR crossover -> core lowpass
	config.AncDataBitRate = ancDataBitRate // SBR estimated bitrate
	config.UsePns = false                  // "Never use PNS if SBR is active"
	// Afterburner / analysis-by-synthesis requantisation. The genuine HE-AAC
	// encode the oracle drives sets AACENC_AFTERBURNER=1, so useRequant=1 and
	// FDKaacEnc_QCInit derives invQuant=2 (aacenc.cpp:716,
	// aacenc_lib.cpp:896). Mirror it so the core's sf_estim runs the same
	// assimilateScf refinement passes (invQuant>0); without it the core picks a
	// different globalGain on signal frames. The plain AAC-LC NewEncoder leaves
	// this at its default-off (matching the afterburner-off encode-e2e oracle).
	config.UseRequant = true

	hAacEnc, err := Open(1, channels, config.NSubFrames)
	if err != AacEncOK {
		return nil, err
	}
	if err := Initialize(hAacEnc, &config, nil, 1); err != AacEncOK {
		return nil, err
	}

	return &SbrCoreEncoder{
		enc:          hAacEnc,
		channels:     channels,
		frameLength:  config.FrameLength,
		inputBufSize: inputBufSize,
	}, AacEncOK
}

// FrameLength returns the core frame length (1024).
func (e *SbrCoreEncoder) FrameLength() int { return e.frameLength }

// EncodeFramePlanar runs one core access unit over the planar int16 input buffer
// (channel c at input[c*inputBufSize:]; the downsampled core signal lives at the
// channel base after the SBR downsampler) with the supplied extension payloads
// (the SBR EXT_SBR_DATA fill element). It is the 1:1 equivalent of the
// FDKaacEnc_EncodeFrame call at aacenc_lib.cpp:1985-1991.
func (e *SbrCoreEncoder) EncodeFramePlanar(input []int16, extPayload []AacEncExtPayload) ([]byte, EncoderError) {
	hTpEnc := newRawTransportEnc()
	return EncodeFrame(e.enc, hTpEnc, input, uint(e.inputBufSize), extPayload)
}
