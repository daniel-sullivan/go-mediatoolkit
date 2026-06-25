package mp4

import aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"

// MPEG-4 descriptor tags carried inside the esds box (ISO/IEC 14496-1).
const (
	tagES        = 0x03 // ES_Descriptor
	tagDecConfig = 0x04 // DecoderConfigDescriptor
	tagDecSpec   = 0x05 // DecoderSpecificInfo (holds the AudioSpecificConfig)
)

// aacSampleRates is the MPEG-4 sampling-frequency-index table referenced by
// an AudioSpecificConfig (ISO/IEC 14496-3). Index 15 is the explicit-rate
// escape and is handled separately.
var aacSampleRates = [...]int{
	96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050,
	16000, 12000, 11025, 8000, 7350, 0, 0, 0,
}

// aacChannelCounts maps the AudioSpecificConfig channel-configuration field
// to a channel count. Index 0 means "defined in AOT-specific config"; 7
// denotes 7.1 (eight physical channels).
var aacChannelCounts = [...]int{0, 1, 2, 3, 4, 5, 6, 8}

// parseESDS extracts the AudioSpecificConfig from an esds box body. The body
// begins with a one-byte version and three reserved flag bytes (FullBox
// header), followed by a nested ES_Descriptor whose DecoderConfigDescriptor
// holds a DecoderSpecificInfo carrying the raw ASC bytes.
func parseESDS(body []byte) (aaclib.AudioSpecificConfig, error) {
	if len(body) < 4 {
		return aaclib.AudioSpecificConfig{}, ErrMissingEsds
	}
	// Skip the FullBox version (1) + flags (3).
	p := body[4:]

	asc, err := findDescriptor(p, tagES)
	if err != nil {
		return aaclib.AudioSpecificConfig{}, err
	}
	// ES_Descriptor body layout (after the descriptor header):
	//   ES_ID(2) + flags(1) [+ optional dependsOn/url/ocr fields]
	// We only need to step past the fixed prefix to reach the nested
	// DecoderConfigDescriptor. The optional fields are gated by flag bits.
	if len(asc) < 3 {
		return aaclib.AudioSpecificConfig{}, ErrMissingEsds
	}
	flags := asc[2]
	q := asc[3:]
	if flags&0x80 != 0 { // streamDependenceFlag
		if len(q) < 2 {
			return aaclib.AudioSpecificConfig{}, ErrMissingEsds
		}
		q = q[2:]
	}
	if flags&0x40 != 0 { // URL_Flag
		if len(q) < 1 {
			return aaclib.AudioSpecificConfig{}, ErrMissingEsds
		}
		urlLen := int(q[0])
		if len(q) < 1+urlLen {
			return aaclib.AudioSpecificConfig{}, ErrMissingEsds
		}
		q = q[1+urlLen:]
	}
	if flags&0x20 != 0 { // OCRstreamFlag
		if len(q) < 2 {
			return aaclib.AudioSpecificConfig{}, ErrMissingEsds
		}
		q = q[2:]
	}

	dec, err := findDescriptor(q, tagDecConfig)
	if err != nil {
		return aaclib.AudioSpecificConfig{}, err
	}
	// DecoderConfigDescriptor body:
	//   objectTypeIndication(1) + streamType/upStream/reserved(1) +
	//   bufferSizeDB(3) + maxBitrate(4) + avgBitrate(4) + DecoderSpecificInfo
	if len(dec) < 13 {
		return aaclib.AudioSpecificConfig{}, ErrMissingEsds
	}
	specInfo, err := findDescriptor(dec[13:], tagDecSpec)
	if err != nil {
		return aaclib.AudioSpecificConfig{}, err
	}

	raw := make([]byte, len(specInfo))
	copy(raw, specInfo)
	return decodeAudioSpecificConfig(raw)
}

// findDescriptor walks the MPEG-4 descriptor list in p, returning the body
// (the bytes after the size field) of the first descriptor with tag want.
// Each descriptor is a one-byte tag, a variable-length size (7 bits per
// byte, high bit = continuation), then the body.
func findDescriptor(p []byte, want byte) ([]byte, error) {
	for len(p) >= 2 {
		tag := p[0]
		i := 1
		size := 0
		for {
			if i >= len(p) {
				return nil, ErrMissingEsds
			}
			b := p[i]
			i++
			size = (size << 7) | int(b&0x7f)
			if b&0x80 == 0 {
				break
			}
		}
		if i+size > len(p) {
			return nil, ErrMissingEsds
		}
		if tag == want {
			return p[i : i+size], nil
		}
		p = p[i+size:]
	}
	return nil, ErrMissingEsds
}

// decodeAudioSpecificConfig parses the raw ASC bit string into the typed
// [aaclib.AudioSpecificConfig], preserving raw. The ASC layout
// (ISO/IEC 14496-3 §1.6.2.1):
//
//	audioObjectType        : 5 bits (escape 31 → +6 bits)
//	samplingFrequencyIndex : 4 bits (15 → explicit 24-bit frequency)
//	channelConfiguration   : 4 bits
func decodeAudioSpecificConfig(raw []byte) (aaclib.AudioSpecificConfig, error) {
	if len(raw) < 2 {
		return aaclib.AudioSpecificConfig{}, ErrMissingEsds
	}
	br := &bitReader{data: raw}

	objectType := br.read(5)
	if objectType == 31 {
		objectType = 32 + br.read(6)
	}

	freqIndex := br.read(4)
	sampleRate := 0
	if freqIndex == 15 {
		sampleRate = int(br.read(24))
	} else if int(freqIndex) < len(aacSampleRates) {
		sampleRate = aacSampleRates[freqIndex]
	}

	chanConfig := br.read(4)
	channels := 0
	if int(chanConfig) < len(aacChannelCounts) {
		channels = aacChannelCounts[chanConfig]
	}

	if br.err {
		return aaclib.AudioSpecificConfig{}, ErrMissingEsds
	}

	aot := aaclib.AudioObjectType(objectType)

	// HE-AAC frame length: SBR/PS access units decode to the long 2048-sample
	// frame; plain AAC-LC to the short 1024.
	frame := aaclib.FrameSamplesShort
	if aot == aaclib.AOTSBR || aot == aaclib.AOTPS {
		frame = aaclib.FrameSamplesLong
	}

	asc := aaclib.AudioSpecificConfig{
		ObjectType:   aot,
		SampleRate:   sampleRate,
		Channels:     channels,
		FrameSamples: frame,
		Raw:          raw,
	}

	// For an explicit-hierarchical HE-AAC ASC (AOT-5 SBR / AOT-29 PS) the bits
	// parsed above are the CORE rate and the core channelConfiguration. Project
	// them onto the true decoded OUTPUT format — the SBR-doubled rate, and the
	// PS-widened stereo channel count — so callers (and the codec/aac adapter)
	// see the rate/channels a decoder will actually emit.
	if aot == aaclib.AOTSBR || aot == aaclib.AOTPS {
		outRate, outChannels := asc.Output()
		if outRate > 0 {
			asc.SampleRate = outRate
		}
		if outChannels > 0 {
			asc.Channels = outChannels
		}
	}

	return asc, nil
}

// bitReader reads big-endian bit fields from a byte slice, most-significant
// bit first. It is local to ASC parsing; out-of-range reads set err and
// return zero.
type bitReader struct {
	data []byte
	pos  int // bit position
	err  bool
}

func (b *bitReader) read(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		byteIdx := b.pos >> 3
		if byteIdx >= len(b.data) {
			b.err = true
			return 0
		}
		bit := (b.data[byteIdx] >> (7 - uint(b.pos&7))) & 1
		v = (v << 1) | uint32(bit)
		b.pos++
	}
	return v
}

// encodeAudioSpecificConfig serialises an AudioSpecificConfig back to its wire
// form. When asc.Raw is non-empty it is returned verbatim so a re-muxer copies
// the original bytes (the common HE-AAC path — the explicit-hierarchical ASC the
// encoder emitted is preserved exactly). Otherwise the standard fields are
// packed: a two-byte AAC-LC entry, or a four-byte explicit-hierarchical entry
// for an HE-AAC (AOT-5 / AOT-29) config that carries no Raw.
func encodeAudioSpecificConfig(asc aaclib.AudioSpecificConfig) []byte {
	if len(asc.Raw) > 0 {
		out := make([]byte, len(asc.Raw))
		copy(out, asc.Raw)
		return out
	}

	objectType := int(asc.ObjectType)
	if objectType == 0 {
		objectType = int(aaclib.AOTAACLC)
	}

	if objectType == int(aaclib.AOTSBR) || objectType == int(aaclib.AOTPS) {
		return encodeExplicitHEAACConfig(asc, objectType)
	}

	freqIndex := sampleRateIndex(asc.SampleRate)
	chanConfig := channelConfigIndex(asc.Channels)

	// audioObjectType(5) | samplingFrequencyIndex(4) | channelConfig(4) →
	// 13 bits, padded to two bytes. (Explicit-frequency escape is not
	// emitted here; callers needing it supply Raw.)
	bits := uint32(objectType&0x1f) << 11
	bits |= uint32(freqIndex&0x0f) << 7
	bits |= uint32(chanConfig&0x0f) << 3
	return []byte{byte(bits >> 8), byte(bits)}
}

// encodeExplicitHEAACConfig builds the explicit-hierarchical HE-AAC ASC
// (ISO/IEC 14496-3 §1.6.2.1) for an AOT-5 (SBR) or AOT-29 (PS) config that has
// no Raw bytes: audioObjectType[5]=extAOT, samplingFrequencyIndex[4]=core rate,
// channelConfiguration[4], extensionSamplingFrequencyIndex[4]=output rate (==
// 2*core), coreAudioObjectType[5]=AAC-LC, GASpecificConfig[3]=0. asc.SampleRate
// is the decoder OUTPUT rate and asc.Channels the OUTPUT channel count; for PS
// the core channelConfiguration is mono (1).
func encodeExplicitHEAACConfig(asc aaclib.AudioSpecificConfig, objectType int) []byte {
	outRate := asc.SampleRate
	coreRate := outRate / 2

	coreChannels := asc.Channels
	if objectType == int(aaclib.AOTPS) {
		coreChannels = 1 // PS core is mono; stereo is reconstructed parametrically
	}

	coreIdx := sampleRateIndex(coreRate)
	outIdx := sampleRateIndex(outRate)
	chCfg := channelConfigIndex(coreChannels)
	if coreIdx == 15 || outIdx == 15 {
		// Rates outside the 4-bit index table fall back to the 2-byte form
		// rather than emitting an unparseable explicit config.
		bits := uint32(objectType&0x1f) << 11
		bits |= uint32(sampleRateIndex(outRate)&0x0f) << 7
		bits |= uint32(channelConfigIndex(asc.Channels)&0x0f) << 3
		return []byte{byte(bits >> 8), byte(bits)}
	}

	var bits uint32
	n := 0
	put := func(v, w int) {
		bits = (bits << uint(w)) | (uint32(v) & ((1 << uint(w)) - 1))
		n += w
	}
	put(objectType, 5)           // audioObjectType (extension AOT: SBR or PS)
	put(coreIdx, 4)              // samplingFrequencyIndex (core rate)
	put(chCfg, 4)                // channelConfiguration (core)
	put(outIdx, 4)               // extensionSamplingFrequencyIndex (output rate)
	put(int(aaclib.AOTAACLC), 5) // coreAudioObjectType = AAC-LC
	put(0, 3)                    // GASpecificConfig: frameLength/dependsOn/extension = 0

	totalBytes := (n + 7) / 8
	bits <<= uint(totalBytes*8 - n)
	out := make([]byte, totalBytes)
	for i := 0; i < totalBytes; i++ {
		out[i] = byte(bits >> uint((totalBytes-1-i)*8))
	}
	return out
}

// sampleRateIndex returns the 4-bit MPEG-4 samplingFrequencyIndex for rate, or
// 15 (the explicit-frequency escape) when rate is not in the index table.
func sampleRateIndex(rate int) int {
	for i, r := range aacSampleRates {
		if r != 0 && r == rate {
			return i
		}
	}
	return 15
}

// channelConfigIndex returns the channelConfiguration field for a channel count,
// or 0 ("defined in AOT-specific config") when the count has no direct mapping.
func channelConfigIndex(channels int) int {
	for i, c := range aacChannelCounts {
		if c == channels {
			return i
		}
	}
	return 0
}
