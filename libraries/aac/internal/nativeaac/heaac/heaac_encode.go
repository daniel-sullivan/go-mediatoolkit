// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// HE-AAC v1 (AOT_SBR) encoder glue: wires the SBR encoder
// (internal/nativeaac/sbr) to the AAC-LC core encoder (internal/nativeaac) the
// way aacenc_lib.cpp does — the SBR encoder runs on the full-rate input, emits
// the EXT_SBR_DATA payload and downsamples the core signal in place; the core
// then encodes the downsampled signal at the SBR-reduced bandwidth with the SBR
// fill element injected. It mirrors the SBR branch of aacEncEncode
// (aacenc_lib.cpp:1778-2010) and FDKaacEnc_Initialize (aacenc_lib.cpp:1303-1359)
// for the single SCE/CPE element a GA HE-AAC v1 stream carries.
//
// FDK-AAC-derived; see libfdk/COPYING. Fenced behind the aacfdk build tag.
// HE-AAC v1 ONLY (mono SCE / stereo CPE); PS / DRC / MPS / ancillary excluded.
package heaac

import (
	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// Encoder-side constants (aacenc_lib.cpp).
const (
	maxDSDelay          = 100                        // MAX_DS_DELAY
	inputBufferSize     = 2*1024 + maxDSDelay + 1537 // INPUTBUFFER_SIZE per channel
	transFac            = 8                          // TRANS_FAC
	defaultHeaderPeriod = 10                         // DEFAULT_HEADER_PERIOD_REPETITION_RATE
	extSbrData          = 0x0d                       // EXT_SBR_DATA (EXT_PAYLOAD_TYPE)
)

// delayAAC == DELAY_AAC(fl) = fl + BSLA(fl); BSLA(fl) = 4*SBL(fl)+SBL(fl)/2,
// SBL(fl) = fl/8 (aacenc_lib.cpp:138-143).
func delayAAC(fl int) int {
	sbl := fl / 8
	bsla := 4*sbl + sbl/2
	return fl + bsla
}

// Encoder is a complete HE-AAC v1 encoder: an SBR encoder over the full-rate
// input plus the AAC-LC core over the downsampled signal. One Encoder encodes a
// sequence of 2048-sample (per-channel) frames for a fixed (sampleRate,
// channels, bitrate) config, emitting one raw AAC-LC+SBR access unit per frame.
type Encoder struct {
	sbrEnc *sbr.SbrEncoder
	core   *nativeaac.SbrCoreEncoder

	channels        int
	sbrFrameLength  int // SBR output frame (== samples read per call) = 2048
	inputBufferSize int // per-channel stride

	inputBuffer    []int16 // planar channels*inputBufferSize
	inputBufferOff int     // aacBufferOffset (== max(sbrPathOffset, corePathOffset))
	nSamplesRead   int
	nSamplesToRead int

	// Metadata-encoder audio delay line. The genuine encoder always
	// instantiates the metadata encoder for a GA AAC stream (metaDataAllowed),
	// and even with metadataMode == 0 it runs FDK_MetadataEnc_Process ->
	// CompensateAudioDelay, which delays the core+SBR input by nAudioDataDelay
	// samples per channel before either path consumes it (metadata_main.cpp:
	// 670-808). This is the one-core-frame input pipeline delay: without it the
	// core reads the SBR-domain signal one frame too early. metadataMode == 0
	// emits no payload, so only this audio delay is observable.
	nAudioDataDelay int     // metadata_main.cpp:462-464,580 (-delay)
	audioDelayBuf   []int16 // pAudioDelayBuffer, per channel: [c*nAudioDataDelay + i]

	asc []byte // the AOT-5 AudioSpecificConfig
}

// NewEncoder builds an HE-AAC v1 encoder. sampleRate is the INPUT (SBR-output)
// sample rate; channels is 1 or 2; bitRate is the total bits/s. It runs the
// genuine init order: SBR init (which halves the rate, doubles the frame length,
// sets the core bandwidth + input-buffer offset + delay), then the AAC-LC core
// init at the core rate. Dual-rate SBR (downSampleFactor == 2) only.
func NewEncoder(sampleRate, channels, bitRate int) (*Encoder, error) {
	if channels < 1 || channels > 2 {
		return nil, errUnsupportedConfig
	}

	const coreFrameLength = 1024
	const downSampleFactor = 2

	enc := &Encoder{
		channels:        channels,
		inputBufferSize: inputBufferSize,
		inputBuffer:     make([]int16, channels*inputBufferSize),
	}

	// SBR element info: single SCE (mono) / CPE (stereo), full bitrate (single
	// element => relativeBits == 1.0 => sbrElInfo[0].bitRate == bitRate).
	var elInfo sbr.SbrElementInfo
	if channels == 1 {
		elInfo.ElType = 0 // ID_SCE
		elInfo.NChannelsInEl = 1
		elInfo.ChannelIndex[0] = 0
	} else {
		elInfo.ElType = 1 // ID_CPE
		elInfo.NChannelsInEl = 2
		elInfo.ChannelIndex[0] = 0
		elInfo.ChannelIndex[1] = 1
	}
	elInfo.BitRate = bitRate
	elInfo.InstanceTag = 0

	sbrEnc := new(sbr.SbrEncoder)
	nDelay := delayAAC(coreFrameLength) // DELAY_AAC(1024), the AAC core delay

	coreSampleRate, coreBandwidth, inputBufferOffset, _, errStatus := sbr.SbrEncoderInit(
		sbrEnc, &elInfo, sampleRate, coreFrameLength, channels, downSampleFactor,
		defaultHeaderPeriod, transFac, nDelay, true)
	if errStatus != 0 {
		return nil, errUnsupportedConfig
	}
	enc.sbrEnc = sbrEnc
	enc.inputBufferOff = inputBufferOffset

	// frameLength becomes the SBR output length (2*coreFrameLength); the encoder
	// reads frameLength*channels samples per call.
	enc.sbrFrameLength = coreFrameLength * downSampleFactor
	enc.nSamplesToRead = enc.sbrFrameLength * channels
	enc.nSamplesRead = 0

	// Metadata-encoder input-delay derivation, 1:1 with aacenc_lib.cpp:1431-1436
	// (inputDataDelay) + FDK_MetadataEnc_Init (metadata_main.cpp:462-464,580).
	// For SBR: inputDataDelay = sbrRatio*DELAY_AAC(coreFrameLength) +
	// sbrEncoder_GetInputDataDelay. sbrRatio == downSampleFactor == 2 for
	// HE-AAC v1. frameLength is the post-SBR output length (sbrFrameLength).
	audioDelay := downSampleFactor*delayAAC(coreFrameLength) + sbrEnc.InputDataDelay
	delay := audioDelay - enc.sbrFrameLength
	for delay > 0 {
		delay -= enc.sbrFrameLength
	}
	enc.nAudioDataDelay = -delay
	if enc.nAudioDataDelay > 0 {
		enc.audioDelayBuf = make([]int16, channels*enc.nAudioDataDelay)
	}

	// estimated bitrate consumed by SBR (ancDataBitRate). aacenc_lib.cpp:1358.
	ancDataBitRate := sbrEnc.EstimateBitrate

	core, cerr := nativeaac.NewSbrCoreEncoder(coreSampleRate, channels, bitRate,
		coreBandwidth, ancDataBitRate, inputBufferSize)
	if cerr != nativeaac.AacEncOK {
		return nil, errUnsupportedConfig
	}
	enc.core = core

	// Build the explicit AOT-5 AudioSpecificConfig.
	enc.asc = buildHEAACASC(sampleRate, coreSampleRate, channels)

	return enc, nil
}

// FrameSamples returns the per-channel samples one EncodeAccessUnit consumes
// (== 2*coreFrameLength == 2048).
func (e *Encoder) FrameSamples() int { return e.sbrFrameLength }

// Channels returns the channel count.
func (e *Encoder) Channels() int { return e.channels }

// ASC returns the AOT-5 AudioSpecificConfig describing the stream.
func (e *Encoder) ASC() []byte { return e.asc }

// EncodeAccessUnit encodes one HE-AAC v1 frame: interleaved int16 PCM
// (len == channels*FrameSamples()) at the input (SBR output) rate into one raw
// AAC-LC raw_data_block carrying the SBR fill element. It mirrors the SBR branch
// of aacEncEncode (aacenc_lib.cpp:1778-2010) for a full frame's worth of input.
func (e *Encoder) EncodeAccessUnit(interleaved []int16) ([]byte, error) {
	if len(interleaved) < e.channels*e.sbrFrameLength {
		return nil, errUnsupportedConfig
	}

	// Deinterleave new full-rate samples into the input buffer at
	// inputBufferOffset/nChannels + nSamplesRead/nChannels (aacenc_lib.cpp:1785).
	pInBase := e.inputBufferOff/e.channels + e.nSamplesRead/e.channels
	nFrames := e.sbrFrameLength
	for ch := 0; ch < e.channels; ch++ {
		dst := e.inputBuffer[ch*e.inputBufferSize+pInBase:]
		for i := 0; i < nFrames; i++ {
			dst[i] = interleaved[i*e.channels+ch]
		}
	}
	e.nSamplesRead += e.nSamplesToRead

	// Metadata-encoder audio delay: delay the core+SBR input by nAudioDataDelay
	// samples per channel, exactly as FDK_MetadataEnc_Process ->
	// CompensateAudioDelay (metadata_main.cpp:770-808) does before the SBR/core
	// paths run. Operates on inputBuffer + inputBufferOffset/nChannels (the same
	// base aacenc_lib.cpp:1909-1911 passes), nSamplesRead/nChannels samples per
	// channel.
	e.compensateAudioDelay()

	// Encode SBR data (sbrEncoder_EncodeFrame): produces the SBR payload and
	// downsamples the core signal into the channel base in place.
	sbrData := make([]byte, 256) // MAX_PAYLOAD_SIZE
	sbrDataBits, serr := sbr.SbrEncoderEncodeFrame(e.sbrEnc, e.inputBuffer, e.inputBufferSize, sbrData)
	if serr != 0 {
		return nil, errUnsupportedConfig
	}

	// Add the SBR extension payload (EXT_SBR_DATA, associated to channel element
	// 0). aacenc_lib.cpp:1943-1966.
	var extPayload []nativeaac.AacEncExtPayload
	if sbrDataBits > 0 {
		extPayload = []nativeaac.AacEncExtPayload{{
			Payload:             sbrData[:(sbrDataBits+7)>>3],
			DataSize:            sbrDataBits,
			DataType:            extSbrData,
			AssociatedChElement: 0,
		}}
	}

	// Encode the AAC core over the downsampled signal with the SBR fill element.
	au, cerr := e.core.EncodeFramePlanar(e.inputBuffer, extPayload)
	if cerr != nativeaac.AacEncOK {
		return nil, errUnsupportedConfig
	}

	// nSamplesRead -= nSamplesToRead; shift delay buffers (aacenc_lib.cpp:2000-2007).
	e.nSamplesRead -= e.nSamplesToRead
	sbr.SbrEncoderUpdateBuffers(e.sbrEnc, e.inputBuffer, e.inputBufferSize)

	return au, nil
}

// compensateAudioDelay is the 1:1 port of CompensateAudioDelay
// (metadata_main.cpp:770-808) for the steady-state nAudioDataDelay <= 1024
// case (the only case HE-AAC v1 produces: nAudioDataDelay == 892). For each
// channel it delays the per-frame window by nAudioDataDelay samples through
// audioDelayBuf: it saves the frame's last M samples, shifts the rest right by
// M, prepends the previous frame's saved tail, and stores the new tail. The
// fdk inner do/while collapses to a single M == nAudioDataDelay iteration for
// nAudioDataDelay <= 1024 (M = min(1024, delayIdx)). The window base matches
// aacenc_lib.cpp:1909-1911 (inputBuffer + inputBufferOffset/nChannels), with
// nAudioSamples = nSamplesRead/nChannels per channel.
func (e *Encoder) compensateAudioDelay() {
	if e.nAudioDataDelay == 0 {
		return
	}
	nAudioSamples := e.nSamplesRead / e.channels // == sbrFrameLength
	base := e.inputBufferOff / e.channels
	scratch := make([]int16, e.nAudioDataDelay)
	for c := 0; c < e.channels; c++ {
		w := e.inputBuffer[c*e.inputBufferSize+base:]
		d := e.audioDelayBuf[c*e.nAudioDataDelay:]
		m := e.nAudioDataDelay // M = min(1024, nAudioDataDelay) == nAudioDataDelay
		copy(scratch[:m], w[nAudioSamples-m:nAudioSamples])
		copy(w[m:nAudioSamples], w[:nAudioSamples-m]) // memmove, dst > src
		copy(w[:m], d[:m])
		copy(d[:m], scratch[:m])
	}
}
