// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// HE-AAC v2 (AOT_PS) encoder glue: wires the SBR encoder's parametric-stereo path
// (internal/nativeaac/sbr, SbrEncoderInitPS + the PS branch of EnvEncodeFrame) to
// the AAC-LC core encoder, the way aacenc_lib.cpp does for a stereo input encoded
// to a mono AAC-LC core + SBR + ps_data. The SBR/PS processing runs on the
// full-rate STEREO input, extracts the PS parameters, downmixes to a mono QMF
// signal, QMF-synthesises the downsampled MONO core signal in place, and emits the
// EXT_SBR_DATA payload (carrying ps_data in its EXTENSION_ID_PS extension); the
// core then encodes the downsampled MONO signal with that SBR fill element. It
// mirrors the AOT_PS branch of aacEncEncode (aacenc_lib.cpp:1778-2010) +
// FDKaacEnc_Initialize (aacenc_lib.cpp:1303-1468) for the single SCE a GA HE-AAC
// v2 stream carries.
//
// FDK-AAC-derived; see libfdk/COPYING. Fenced behind the aacfdk build tag.
// HE-AAC v2 GA baseline (stereo input -> mono SCE core + SBR + PS); IPD/OPD not
// transmitted; DRM/LD/ELD/USAC-MPS212 excluded.
package heaac

import (
	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// PSEncoder is a complete HE-AAC v2 encoder: the SBR+PS encoder over the full-rate
// STEREO input plus the AAC-LC core over the downsampled MONO signal. It encodes a
// sequence of 2048-sample (per-channel) STEREO frames for a fixed (sampleRate,
// bitrate) config, emitting one raw AAC-LC+SBR access unit (carrying ps_data) per
// frame.
type PSEncoder struct {
	sbrEnc *sbr.SbrEncoder
	core   *nativeaac.SbrCoreEncoder

	inChannels     int // input channels (2) — drives deinterleave + metadata delay
	coreChannels   int // core channels (1, the mono downmix)
	sbrFrameLength int // SBR output frame (== samples read per channel per call) = 2048
	inputBufSize   int // per-channel stride (inputBufferSizePerChannel)

	inputBuffer    []int16 // planar inChannels*inputBufSize
	inputBufferOff int     // aacBufferOffset (== max(sbrPathOffset, corePathOffset))
	nSamplesRead   int     // interleaved samples in the buffer (stereo)
	nSamplesToRead int     // interleaved samples consumed per frame (stereo)

	// Metadata-encoder audio delay line — operates on the STEREO input (the
	// metadata encoder's nChannels == the input channel count, 2), exactly as
	// FDK_MetadataEnc_Process -> CompensateAudioDelay runs before the SBR/PS path.
	nAudioDataDelay int
	audioDelayBuf   []int16 // pAudioDelayBuffer, per channel: [c*nAudioDataDelay + i]

	asc []byte // the AOT-29 AudioSpecificConfig
}

// NewPSEncoder builds an HE-AAC v2 encoder. sampleRate is the INPUT (SBR-output)
// sample rate; the input is stereo (2 channels); bitRate is the total bits/s. The
// genuine init order: SBR+PS init (halves the rate, doubles the frame length,
// overrides the element to a mono SCE, selects QMF-domain downsampling, sets up
// the PS wrapper), then the AAC-LC core init at the (mono) core rate. Dual-rate
// only.
func NewPSEncoder(sampleRate, bitRate int) (*PSEncoder, error) {
	const coreFrameLength = 1024
	const downSampleFactor = 2

	enc := &PSEncoder{
		inChannels:   2,
		coreChannels: 1,
		inputBufSize: inputBufferSize,
		inputBuffer:  make([]int16, 2*inputBufferSize),
	}

	// SBR element info: a single SCE (PS overrides the element to mono).
	var elInfo sbr.SbrElementInfo
	elInfo.ElType = 0 // ID_SCE (PS override)
	elInfo.NChannelsInEl = 1
	elInfo.ChannelIndex[0] = 0
	elInfo.ChannelIndex[1] = 1
	elInfo.BitRate = bitRate
	elInfo.InstanceTag = 0

	// PS tuning (sbr_encoder.cpp:2297-2311, psTuningTable). nStereoBands /
	// maxEnvelopes / iidQuantErrorThreshold by bitrate.
	psCfg := psTuningFor(bitRate)

	sbrEnc := new(sbr.SbrEncoder)
	nDelay := delayAAC(coreFrameLength)

	coreSampleRate, coreBandwidth, inputBufferOffset, _, errStatus := sbr.SbrEncoderInitPS(
		sbrEnc, &elInfo, sampleRate, coreFrameLength, 2, downSampleFactor,
		defaultHeaderPeriod, transFac, nDelay, psCfg, true)
	if errStatus != 0 {
		return nil, errUnsupportedConfig
	}
	enc.sbrEnc = sbrEnc
	enc.inputBufferOff = inputBufferOffset

	enc.sbrFrameLength = coreFrameLength * downSampleFactor
	enc.nSamplesToRead = enc.sbrFrameLength * enc.inChannels
	enc.nSamplesRead = 0

	// Metadata-encoder input-delay derivation (aacenc_lib.cpp:1431-1436): for SBR,
	// inputDataDelay = sbrRatio*DELAY_AAC(coreFrameLength) + GetInputDataDelay;
	// sbrRatio == downSampleFactor == 2. The metadata encoder operates on the
	// STEREO input (nChannels == 2).
	audioDelay := downSampleFactor*delayAAC(coreFrameLength) + sbrEnc.InputDataDelay
	delay := audioDelay - enc.sbrFrameLength
	for delay > 0 {
		delay -= enc.sbrFrameLength
	}
	enc.nAudioDataDelay = -delay
	if enc.nAudioDataDelay > 0 {
		enc.audioDelayBuf = make([]int16, enc.inChannels*enc.nAudioDataDelay)
	}

	ancDataBitRate := sbrEnc.EstimateBitrate

	core, cerr := nativeaac.NewSbrCoreEncoder(coreSampleRate, enc.coreChannels, bitRate,
		coreBandwidth, ancDataBitRate, inputBufferSize)
	if cerr != nativeaac.AacEncOK {
		return nil, errUnsupportedConfig
	}
	enc.core = core

	// Delay compensation: for PS nBitstrDelay == 1, so fill the SBR payload delay
	// line with nBitstrDelay dummy (clearOutput) frames over the zeroed input
	// buffer (sbr_encoder.cpp:2351-2353). clearOutput skips the PS branch (it runs
	// under !clearOutput), so only the SBR-envelope/delay-line plumbing executes.
	if enc.sbrEnc.NBitstrDelay > 0 {
		sbr.DelayCompensation(enc.sbrEnc, enc.inputBuffer, enc.inputBufSize)
	}

	enc.asc = buildHEAACv2ASC(sampleRate, coreSampleRate)
	return enc, nil
}

// FrameSamples returns the per-channel samples one EncodeAccessUnit consumes
// (== 2*coreFrameLength == 2048).
func (e *PSEncoder) FrameSamples() int { return e.sbrFrameLength }

// Channels returns the INPUT channel count (2 — stereo in, mono core out).
func (e *PSEncoder) Channels() int { return e.inChannels }

// ASC returns the AOT-29 AudioSpecificConfig describing the stream.
func (e *PSEncoder) ASC() []byte { return e.asc }

// EncodeAccessUnit encodes one HE-AAC v2 frame: interleaved int16 STEREO PCM
// (len == 2*FrameSamples()) at the input rate into one raw AAC-LC raw_data_block
// carrying the SBR fill element with ps_data. It mirrors the AOT_PS branch of
// aacEncEncode (aacenc_lib.cpp:1778-2010) for a full frame's worth of input.
func (e *PSEncoder) EncodeAccessUnit(interleaved []int16) ([]byte, error) {
	if len(interleaved) < e.inChannels*e.sbrFrameLength {
		return nil, errUnsupportedConfig
	}

	// Deinterleave new full-rate STEREO samples into the input buffer at
	// inputBufferOffset/aacConfig.nChannels(==1) + nSamplesRead/extParam.nChannels
	// (==2) (aacenc_lib.cpp:1785-1809).
	pInBase := e.inputBufferOff/e.coreChannels + e.nSamplesRead/e.inChannels
	nFrames := e.sbrFrameLength
	for ch := 0; ch < e.inChannels; ch++ {
		dst := e.inputBuffer[ch*e.inputBufSize+pInBase:]
		for i := 0; i < nFrames; i++ {
			dst[i] = interleaved[i*e.inChannels+ch]
		}
	}
	e.nSamplesRead += e.nSamplesToRead

	// Metadata-encoder audio delay over the STEREO input (the metadata base is
	// inputBuffer + inputBufferOffset/coderConfig.noChannels(==1), nChannels==2).
	e.compensateAudioDelay()

	// Encode SBR+PS data (sbrEncoder_EncodeFrame): the PS branch of EnvEncodeFrame
	// runs the stereo QMF+hybrid analysis, PS extraction, downmix + hybrid
	// synthesis, writes the downsampled MONO core signal back into channel 0's
	// base and emits the SBR payload (with ps_data).
	sbrData := make([]byte, 256) // MAX_PAYLOAD_SIZE
	sbrDataBits, serr := sbr.SbrEncoderEncodeFrame(e.sbrEnc, e.inputBuffer, e.inputBufSize, sbrData)
	if serr != 0 {
		return nil, errUnsupportedConfig
	}

	var extPayload []nativeaac.AacEncExtPayload
	if sbrDataBits > 0 {
		extPayload = []nativeaac.AacEncExtPayload{{
			Payload:             sbrData[:(sbrDataBits+7)>>3],
			DataSize:            sbrDataBits,
			DataType:            extSbrData,
			AssociatedChElement: 0,
		}}
	}

	// Encode the AAC core over the downsampled MONO signal with the SBR fill
	// element. The core reads channel 0's base (the mono downmix).
	au, cerr := e.core.EncodeFramePlanar(e.inputBuffer, extPayload)
	if cerr != nativeaac.AacEncOK {
		return nil, errUnsupportedConfig
	}

	// nSamplesRead -= nSamplesToRead; shift delay buffers (aacenc_lib.cpp:2000-2007).
	e.nSamplesRead -= e.nSamplesToRead
	sbr.SbrEncoderUpdateBuffers(e.sbrEnc, e.inputBuffer, e.inputBufSize)

	return au, nil
}

// compensateAudioDelay is the 1:1 port of CompensateAudioDelay
// (metadata_main.cpp:770-808) over the STEREO input. The metadata encoder's
// nChannels is the input count (2); the per-channel window base is inputBuffer +
// inputBufferOffset/coderConfig.noChannels (== mono core count, 1) and the
// per-channel sample count is nSamplesRead/nChannels (== sbrFrameLength). The
// do/while runs M = min(1024, remaining) iterations — for PS nAudioDataDelay is
// 1057 (> 1024), so it takes two iterations (M = 1024 then 33), unlike the v1
// single-iteration case (nAudioDataDelay == 892 <= 1024).
func (e *PSEncoder) compensateAudioDelay() {
	if e.nAudioDataDelay == 0 {
		return
	}
	nAudioSamples := e.nSamplesRead / e.inChannels // == sbrFrameLength
	base := e.inputBufferOff / e.coreChannels
	scratch := make([]int16, 1024)
	for c := 0; c < e.inChannels; c++ {
		w := e.inputBuffer[c*e.inputBufSize+base:]
		d := e.audioDelayBuf[c*e.nAudioDataDelay:]
		m := 1024
		delayIdx := e.nAudioDataDelay
		for {
			if delayIdx < m {
				m = delayIdx
			}
			delayIdx -= m
			copy(scratch[:m], w[nAudioSamples-m:nAudioSamples])
			copy(w[m:nAudioSamples], w[:nAudioSamples-m]) // memmove, dst > src
			copy(w[:m], d[delayIdx:delayIdx+m])
			copy(d[delayIdx:delayIdx+m], scratch[:m])
			if delayIdx <= 0 {
				break
			}
		}
	}
}

// psTuningFor returns the PS tuning settings for a bitrate (psTuningTable,
// sbrenc_rom.cpp:899-908). The IID quant-error thresholds are FL2FXCONST_DBL of
// 3/4, 2/4, 1.5/4, 1.1/4.
func psTuningFor(bitrate int) sbr.PSEncConfig {
	switch {
	case bitrate < 22000:
		return sbr.PSEncConfig{NStereoBands: 10, MaxEnvelopes: 1, IidQuantErrorThreshold: nativeaac.Fl2fxconstDBL(3.0 / 4.0)}
	case bitrate < 28000:
		return sbr.PSEncConfig{NStereoBands: 20, MaxEnvelopes: 1, IidQuantErrorThreshold: nativeaac.Fl2fxconstDBL(2.0 / 4.0)}
	case bitrate < 36000:
		return sbr.PSEncConfig{NStereoBands: 20, MaxEnvelopes: 2, IidQuantErrorThreshold: nativeaac.Fl2fxconstDBL(1.5 / 4.0)}
	default:
		return sbr.PSEncConfig{NStereoBands: 20, MaxEnvelopes: 4, IidQuantErrorThreshold: nativeaac.Fl2fxconstDBL(1.1 / 4.0)}
	}
}
