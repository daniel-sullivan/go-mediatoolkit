package aac

// This file is the always-available pure-Go adapter that wires the
// NewNative{Decoder,Encoder} surface to the 1:1 C port under
// internal/nativeaac. It carries NO build tag: the pure-Go path compiles
// in every build (default, CGO_ENABLED=0, and the aacfdk island) so
// NewNativeDecoder / NewNativeEncoder are always reachable, mirroring
// libraries/opus and libraries/flac.
//
// The tag-routed New{Decoder,Encoder} constructors (newDecoder/newEncoder)
// live in the build-tag-split seams:
//
//   - decoder.go / encoder.go   (//go:build !aacfdk)        — the default
//     build: return ErrEngineRequiresFDK, because FDK-AAC is the only AAC
//     engine and it is fenced behind the opt-in aacfdk tag.
//   - decoder_cgo.go / encoder_cgo.go (//go:build cgo && aacfdk) — the
//     vendored Fraunhofer FDK-AAC backend.
//
// Under the aacfdk tag the engines are the internal/nativeaac 1:1 fixed-point
// port — decode is bit-exact and encode is byte-identical vs fdk-aac; in the
// default build the FDK-derived port is fenced out and the constructors return
// ErrEngineRequiresFDK.

// nativeDecodeEngine is the build-tag-routed AAC-LC decode core. Under aacfdk it
// is the internal/nativeaac 1:1 fixed-point port; in the default build it is
// unavailable (the FDK-derived port is fenced behind the aacfdk tag), so
// newNativeDecodeEngine returns ErrEngineRequiresFDK.
type nativeDecodeEngine interface {
	// DecodeAccessUnit decodes one AAC access unit into interleaved int16 PCM
	// and returns the samples-per-channel produced.
	DecodeAccessUnit(pkt []byte, out []int16) (int, error)
	Reset()
}

// newNativeDecoder constructs the pure-Go decoder. For an AAC-LC stream it
// instantiates the internal/nativeaac AAC-LC decode engine (the 1:1 fixed-point
// port of the vendored FDK-AAC reference); for an HE-AAC v1 stream (explicit
// AOT-5 or implicit SBR signalling) it routes to the SBR-upsampling engine
// (internal/nativeaac/heaac), which decodes the 1024-sample AAC-LC core and
// SBR-doubles it to 2048 samples per channel at the doubled output rate.
func newNativeDecoder(asc AudioSpecificConfig, cfg decoderConfig) (Decoder, error) {
	sbr, coreRate, outRate := sbrParamsFromASC(asc)

	if asc.ObjectType == AOTPS {
		// HE-AAC v2: a MONO AAC-LC core (1024 samples at coreRate) carrying
		// ps_data in its SBR extension is SBR-doubled to 2048 samples at outRate
		// and parametric-stereo upmixed to a 2-channel output. The native PS
		// decode engine (heaac.NewPSDecoder) is bit-exact stereo. The public
		// Output() already reports 2 channels for AOT-29 (native_stub.go above).
		eng, err := newNativePsDecodeEngine(FrameSamplesShort, coreRate, outRate)
		if err != nil {
			return nil, err
		}
		outASC := asc
		outASC.SampleRate = outRate
		outASC.Channels = 2
		outASC.FrameSamples = FrameSamplesLong
		return &nativeDecoder{
			asc:        outASC,
			sampleRate: outRate,
			channels:   2,
			frame:      FrameSamplesLong,
			engine:     eng,
			pcm16:      make([]int16, FrameSamplesLong*MaxChannels),
		}, nil
	}

	if sbr {
		// HE-AAC v1: the core decodes 1024-sample frames at coreRate, SBR
		// upsamples to 2048 at outRate (== 2*coreRate).
		eng, err := newNativeSbrDecodeEngine(FrameSamplesShort, coreRate, asc.Channels, outRate)
		if err != nil {
			return nil, err
		}
		outASC := asc
		outASC.SampleRate = outRate
		outASC.FrameSamples = FrameSamplesLong
		return &nativeDecoder{
			asc:        outASC,
			sampleRate: outRate,
			channels:   asc.Channels,
			frame:      FrameSamplesLong,
			engine:     eng,
			pcm16:      make([]int16, FrameSamplesLong*MaxChannels),
		}, nil
	}

	frame := asc.FrameSamples
	if frame == 0 {
		frame = FrameSamplesShort
	}
	eng, err := newNativeDecodeEngine(frame, asc.SampleRate, asc.Channels)
	if err != nil {
		return nil, err
	}
	return &nativeDecoder{
		asc:        asc,
		sampleRate: asc.SampleRate,
		channels:   asc.Channels,
		frame:      frame,
		engine:     eng,
		pcm16:      make([]int16, FrameSamplesLong*MaxChannels),
	}, nil
}

// sbrParamsFromASC inspects the AudioSpecificConfig to decide whether the stream
// is HE-AAC (SBR, optionally with PS) and, if so, derives the AAC-LC core sample
// rate and the SBR-doubled output sample rate. It handles the two MPEG-4 SBR
// signalling forms:
//
//   - Explicit (hierarchical): audioObjectType == 5 (AOT_SBR) or 29 (AOT_PS). The
//     ASC bit layout (ISO/IEC 14496-3 §1.6.2.1, as emitted by libfdk-aac) is
//     audioObjectType[5]=5/29, samplingFrequencyIndex[4] (the CORE rate),
//     channelConfiguration[4], extensionSamplingFrequencyIndex[4] (the OUTPUT
//     rate, == 2*core), coreAudioObjectType[5]. The parsed asc.SampleRate already
//     resolved one of the two indices, so we re-read the raw bits to recover both.
//   - Implicit: audioObjectType == 2 (AAC-LC) with no SBR field in the ASC; SBR is
//     only discovered from the per-frame extension payload. Without out-of-band
//     signalling we cannot know the output rate up front, so the native path
//     decodes plain AAC-LC here (implicit SBR is treated as core-only). Explicit
//     signalling is the form a container (mp4 esds) carries.
//
// Returns sbr=false for any non-SBR or unparseable config, in which case the
// core/out rates are the asc.SampleRate unchanged.
func sbrParamsFromASC(asc AudioSpecificConfig) (sbr bool, coreRate, outRate int) {
	if asc.ObjectType != AOTSBR && asc.ObjectType != AOTPS {
		return false, asc.SampleRate, asc.SampleRate
	}
	core, out, _, ok := parseExplicitSbrRates(asc.Raw)
	if !ok {
		// Fall back to the resolved asc.SampleRate as the OUTPUT rate.
		if asc.SampleRate <= 0 {
			return false, asc.SampleRate, asc.SampleRate
		}
		return true, asc.SampleRate / 2, asc.SampleRate
	}
	return true, core, out
}

// Output resolves the decoder's OUTPUT sample rate and channel count for the
// stream this AudioSpecificConfig describes, applying the HE-AAC extension
// signalling:
//
//   - HE-AAC v1 (AOT-5 SBR): the output rate is the SBR-doubled extension rate
//     (twice the AAC-LC core rate); the channel count is unchanged.
//   - HE-AAC v2 (AOT-29 PS): SBR doubling applies to the rate, and parametric
//     stereo promotes the mono core to a stereo (2-channel) output.
//   - Plain AAC (any other AOT): SampleRate and Channels unchanged.
//
// It reads the explicit-hierarchical rates from Raw when present, falling back to
// SampleRate (treated as the already-resolved output rate) otherwise. Output is
// the projection a container (e.g. mp4 esds) and the codec/aac adapter use to
// report the true decoded format up front, before the first frame is decoded.
func (a AudioSpecificConfig) Output() (sampleRate, channels int) {
	sampleRate = a.SampleRate
	channels = a.Channels

	switch a.ObjectType {
	case AOTSBR, AOTPS:
		_, out, chCfg, ok := parseExplicitSbrRates(a.Raw)
		if ok {
			sampleRate = out
			if chCfg > 0 {
				channels = chCfg
			}
		}
		if a.ObjectType == AOTPS && channels < 2 {
			// Parametric stereo decodes a mono core to a stereo output.
			channels = 2
		}
	}
	return sampleRate, channels
}

// parseExplicitSbrRates reads the explicit-hierarchical HE-AAC ASC bit layout
// (AOT_SBR=5 or AOT_PS=29) and returns the core (AAC-LC) and extension (SBR
// output) sample rates plus the channelConfiguration. ok is false if the bytes
// are too short or the object type is not an explicit HE-AAC AOT.
func parseExplicitSbrRates(raw []byte) (coreRate, outRate, chCfg int, ok bool) {
	if len(raw) < 3 {
		return 0, 0, 0, false
	}
	br := ascBitReader{data: raw}
	aot := br.read(5)
	if aot != int(AOTSBR) && aot != int(AOTPS) {
		return 0, 0, 0, false
	}
	coreRate = ascSampleRateAt(&br)
	chCfg = br.read(4) // channelConfiguration of the AAC-LC core
	outRate = ascSampleRateAt(&br)
	if br.err || coreRate <= 0 || outRate <= 0 {
		return 0, 0, 0, false
	}
	return coreRate, outRate, chCfg, true
}

// ascSampleRateAt reads a samplingFrequencyIndex[4] (with the 0x0f explicit
// 24-bit-frequency escape) and resolves it to a rate in Hz.
func ascSampleRateAt(br *ascBitReader) int {
	idx := br.read(4)
	if idx == 0x0f {
		return br.read(24)
	}
	rates := []int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050,
		16000, 12000, 11025, 8000, 7350}
	if idx >= 0 && idx < len(rates) {
		return rates[idx]
	}
	return 0
}

// ascBitReader reads big-endian (MSB-first) bit fields from the ASC byte string.
type ascBitReader struct {
	data []byte
	pos  int
	err  bool
}

func (b *ascBitReader) read(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		byteIdx := b.pos >> 3
		if byteIdx >= len(b.data) {
			b.err = true
			return 0
		}
		bit := (b.data[byteIdx] >> uint(7-(b.pos&7))) & 1
		v = (v << 1) | int(bit)
		b.pos++
	}
	return v
}

// nativeEncodeEngine is the build-tag-routed AAC-LC encode core. Under aacfdk it
// is the internal/nativeaac 1:1 fixed-point port; in the default build it is
// unavailable (the FDK-derived port is fenced behind the aacfdk tag), so
// newNativeEncodeEngine returns ErrEngineRequiresFDK.
type nativeEncodeEngine interface {
	// EncodeOneFrame encodes one frame of interleaved int16 PCM
	// (len == channels*frameLength) into a raw AAC-LC access unit.
	EncodeOneFrame(interleaved []int16) ([]byte, error)
	// FrameLength is the per-channel samples one EncodeOneFrame call consumes.
	FrameLength() int
}

// nativeSbrEncodeEngine is the build-tag-routed HE-AAC v1 (AOT-5) encode core.
// Under aacfdk it is the internal/nativeaac/heaac glue (SBR encoder + AAC-LC
// core); in the default build it is unavailable (the FDK-derived port is fenced
// behind the aacfdk tag), so newNativeSbrEncodeEngine returns ErrEngineRequiresFDK.
type nativeSbrEncodeEngine interface {
	// EncodeAccessUnit encodes one frame of interleaved int16 PCM
	// (len == channels*FrameSamples()) into one raw AAC-LC+SBR access unit.
	EncodeAccessUnit(interleaved []int16) ([]byte, error)
	// FrameSamples is the per-channel samples one EncodeAccessUnit consumes (2048).
	FrameSamples() int
	// ASC returns the explicit AOT-5 AudioSpecificConfig describing the stream.
	ASC() []byte
}

// nativePsEncodeEngine is the build-tag-routed HE-AAC v2 (AOT-29) encode core.
// Under aacfdk it is the internal/nativeaac/heaac PS glue (SBR+PS encoder over the
// full-rate STEREO input + AAC-LC core over the downsampled MONO downmix); in the
// default build it is unavailable (the FDK-derived port is fenced behind the aacfdk
// tag), so newNativePsEncodeEngine returns ErrEngineRequiresFDK. Parametric stereo
// takes a STEREO input and produces a MONO core carrying ps_data, so the input
// channel count is always 2.
type nativePsEncodeEngine interface {
	// EncodeAccessUnit encodes one frame of interleaved int16 STEREO PCM
	// (len == 2*FrameSamples()) into one raw AAC-LC+SBR access unit carrying
	// ps_data.
	EncodeAccessUnit(interleaved []int16) ([]byte, error)
	// FrameSamples is the per-channel samples one EncodeAccessUnit consumes (2048).
	FrameSamples() int
	// ASC returns the explicit AOT-29 AudioSpecificConfig describing the stream.
	ASC() []byte
}

// newNativeEncoder constructs the pure-Go encoder. For AAC-LC it instantiates
// the internal/nativeaac AAC-LC CBR encode engine; for HE-AAC v1 (AOT-5) it
// instantiates the internal/nativeaac/heaac SBR encode engine (SBR over the
// full-rate input + AAC-LC core over the downsampled signal), which emits one
// raw AAC-LC+SBR access unit per 2048-sample frame and the explicit AOT-5 ASC;
// for HE-AAC v2 (AOT-29) it instantiates the internal/nativeaac/heaac PS encode
// engine (SBR+PS over the full-rate STEREO input + AAC-LC core over the mono
// downmix), which requires a 2-channel input and emits one raw AAC-LC+SBR access
// unit carrying ps_data per 2048-sample frame and the explicit AOT-29 ASC.
func newNativeEncoder(sampleRate, channels int, cfg encoderConfig) (Encoder, error) {
	bitrate := cfg.bitrate
	if bitrate <= 0 {
		bitrate = 128000
	}

	if cfg.objectType == AOTSBR {
		eng, err := newNativeSbrEncodeEngine(sampleRate, channels, bitrate)
		if err != nil {
			return nil, err
		}
		frame := eng.FrameSamples()
		return &nativeSbrEncoder{
			sampleRate: sampleRate,
			channels:   channels,
			frame:      frame,
			engine:     eng,
			asc: AudioSpecificConfig{
				ObjectType:   AOTSBR,
				SampleRate:   sampleRate,
				Channels:     channels,
				FrameSamples: frame,
				Raw:          eng.ASC(),
			},
			pcm16: make([]int16, frame*channels),
		}, nil
	}

	if cfg.objectType == AOTPS {
		// HE-AAC v2 (AOT-29): parametric stereo encodes a STEREO input down to a
		// MONO AAC-LC core carrying ps_data, so it requires exactly 2 input
		// channels. sampleRate is the input (SBR-output) rate.
		if channels != 2 {
			return nil, ErrPSRequiresStereo
		}
		eng, err := newNativePsEncodeEngine(sampleRate, bitrate)
		if err != nil {
			return nil, err
		}
		frame := eng.FrameSamples()
		return &nativePsEncoder{
			sampleRate: sampleRate,
			channels:   channels,
			frame:      frame,
			engine:     eng,
			asc: AudioSpecificConfig{
				ObjectType:   AOTPS,
				SampleRate:   sampleRate,
				Channels:     channels,
				FrameSamples: frame,
				Raw:          eng.ASC(),
			},
			pcm16: make([]int16, frame*channels),
		}, nil
	}

	eng, err := newNativeEncodeEngine(sampleRate, channels, bitrate, cfg.vbrMode)
	if err != nil {
		return nil, err
	}
	frame := eng.FrameLength()
	return &nativeEncoder{
		sampleRate: sampleRate,
		channels:   channels,
		objectType: cfg.objectType,
		frame:      frame,
		engine:     eng,
		asc: AudioSpecificConfig{
			ObjectType:   cfg.objectType,
			SampleRate:   sampleRate,
			Channels:     channels,
			FrameSamples: frame,
			Raw:          buildASC(cfg.objectType, sampleRate, channels),
		},
		pcm16: make([]int16, frame*channels),
	}, nil
}

// nativeDecoder is the pure-Go [Decoder] implementation.
type nativeDecoder struct {
	asc        AudioSpecificConfig
	sampleRate int
	channels   int
	frame      int
	engine     nativeDecodeEngine
	pcm16      []int16 // scratch interleaved int16 PCM, reused across Decode calls
}

func (d *nativeDecoder) Decode(pkt []byte, pcm []float64) (int, error) {
	if len(pkt) == 0 {
		return 0, ErrInvalidPacket
	}
	if len(pcm) < d.frame*d.channels {
		return 0, ErrBufferTooSmall
	}
	n, err := d.engine.DecodeAccessUnit(pkt, d.pcm16)
	if err != nil {
		return 0, ErrInvalidPacket
	}
	total := n * d.channels
	if total > len(pcm) {
		return 0, ErrBufferTooSmall
	}
	// nativeaac emits interleaved int16 (the FDK INT_PCM output); normalise to
	// [-1, 1] exactly as the cgo decoder does (decoder_cgo.go:136).
	for i := 0; i < total; i++ {
		pcm[i] = float64(d.pcm16[i]) / 32768.0
	}
	return n, nil
}

func (d *nativeDecoder) SampleRate() int             { return d.sampleRate }
func (d *nativeDecoder) Channels() int               { return d.channels }
func (d *nativeDecoder) Config() AudioSpecificConfig { return d.asc }
func (d *nativeDecoder) Reset()                      { d.engine.Reset() }

// nativeEncoder is the pure-Go [Encoder] implementation.
type nativeEncoder struct {
	sampleRate int
	channels   int
	objectType AudioObjectType
	frame      int
	engine     nativeEncodeEngine
	asc        AudioSpecificConfig
	pcm16      []int16 // scratch interleaved int16 PCM, reused across Encode calls
}

func (e *nativeEncoder) Encode(pcm []float64) ([]byte, error) {
	if len(pcm) < e.frame*e.channels {
		return nil, ErrBufferTooSmall
	}
	// Quantise the [-1,1] float PCM to interleaved int16, mirroring the cgo
	// encoder's input conversion (encoder_cgo.go float->short).
	n := e.frame * e.channels
	for i := 0; i < n; i++ {
		v := pcm[i] * 32768.0
		if v > 32767.0 {
			v = 32767.0
		} else if v < -32768.0 {
			v = -32768.0
		}
		e.pcm16[i] = int16(v)
	}
	return e.engine.EncodeOneFrame(e.pcm16)
}

func (e *nativeEncoder) Config() AudioSpecificConfig { return e.asc }

func (e *nativeEncoder) SampleRate() int { return e.sampleRate }
func (e *nativeEncoder) Channels() int   { return e.channels }
func (e *nativeEncoder) Reset()          {}

// nativeSbrEncoder is the pure-Go HE-AAC v1 (AOT-5) [Encoder] implementation: it
// drives the heaac SBR encode engine over 2048-sample frames and emits one raw
// AAC-LC+SBR access unit per Encode call, carrying the explicit AOT-5 ASC.
type nativeSbrEncoder struct {
	sampleRate int
	channels   int
	frame      int
	engine     nativeSbrEncodeEngine
	asc        AudioSpecificConfig
	pcm16      []int16
}

func (e *nativeSbrEncoder) Encode(pcm []float64) ([]byte, error) {
	if len(pcm) < e.frame*e.channels {
		return nil, ErrBufferTooSmall
	}
	n := e.frame * e.channels
	for i := 0; i < n; i++ {
		v := pcm[i] * 32768.0
		if v > 32767.0 {
			v = 32767.0
		} else if v < -32768.0 {
			v = -32768.0
		}
		e.pcm16[i] = int16(v)
	}
	return e.engine.EncodeAccessUnit(e.pcm16)
}

func (e *nativeSbrEncoder) Config() AudioSpecificConfig { return e.asc }
func (e *nativeSbrEncoder) SampleRate() int             { return e.sampleRate }
func (e *nativeSbrEncoder) Channels() int               { return e.channels }
func (e *nativeSbrEncoder) Reset()                      {}

// nativePsEncoder is the pure-Go HE-AAC v2 (AOT-29) [Encoder] implementation: it
// drives the heaac PS encode engine over 2048-sample STEREO frames and emits one
// raw AAC-LC+SBR access unit (carrying ps_data over a mono core) per Encode call,
// carrying the explicit AOT-29 ASC. channels is the STEREO input count (2); the
// reported sampleRate is the input (SBR-output) rate.
type nativePsEncoder struct {
	sampleRate int
	channels   int
	frame      int
	engine     nativePsEncodeEngine
	asc        AudioSpecificConfig
	pcm16      []int16
}

func (e *nativePsEncoder) Encode(pcm []float64) ([]byte, error) {
	if len(pcm) < e.frame*e.channels {
		return nil, ErrBufferTooSmall
	}
	n := e.frame * e.channels
	for i := 0; i < n; i++ {
		v := pcm[i] * 32768.0
		if v > 32767.0 {
			v = 32767.0
		} else if v < -32768.0 {
			v = -32768.0
		}
		e.pcm16[i] = int16(v)
	}
	return e.engine.EncodeAccessUnit(e.pcm16)
}

func (e *nativePsEncoder) Config() AudioSpecificConfig { return e.asc }
func (e *nativePsEncoder) SampleRate() int             { return e.sampleRate }
func (e *nativePsEncoder) Channels() int               { return e.channels }
func (e *nativePsEncoder) Reset()                      {}

// ascSampleRateIndex returns the 4-bit MPEG-4 samplingFrequencyIndex for the
// given rate (ISO/IEC 14496-3, Table 1.16); 0x0f (escape) for unlisted rates.
func ascSampleRateIndex(sampleRate int) int {
	rates := []int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050,
		16000, 12000, 11025, 8000, 7350}
	for i, r := range rates {
		if r == sampleRate {
			return i
		}
	}
	return 0x0f
}

// buildASC builds the MPEG-4 AudioSpecificConfig byte string for an AAC-LC
// (or other plain-AOT) stream: 5 bits audioObjectType, 4 bits
// samplingFrequencyIndex, 4 bits channelConfiguration, then a 3-bit
// GASpecificConfig (frameLengthFlag=0, dependsOnCoreCoder=0, extensionFlag=0).
// 16 bits total -> 2 bytes. Mirrors the ASC the FDK lib emits for the raw path.
func buildASC(objectType AudioObjectType, sampleRate, channels int) []byte {
	aot := int(objectType) // AOTAACLC == 2
	sri := ascSampleRateIndex(sampleRate)
	chCfg := channels // 1 or 2 map directly to channelConfiguration 1/2

	var bits uint32
	n := 0
	put := func(v, w int) {
		bits = (bits << uint(w)) | (uint32(v) & ((1 << uint(w)) - 1))
		n += w
	}
	put(aot, 5)
	put(sri, 4)
	put(chCfg, 4)
	put(0, 3) // GASpecificConfig: frameLengthFlag/dependsOnCoreCoder/extensionFlag

	// left-align the n bits into whole bytes (MSB first)
	totalBytes := (n + 7) / 8
	bits <<= uint(totalBytes*8 - n)
	out := make([]byte, totalBytes)
	for i := 0; i < totalBytes; i++ {
		out[i] = byte(bits >> uint((totalBytes-1-i)*8))
	}
	return out
}
