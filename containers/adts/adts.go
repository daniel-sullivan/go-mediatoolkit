// Package adts reads and writes the ADTS (Audio Data Transport Stream)
// framing for raw AAC — the per-frame header used by standalone `.aac`
// files and broadcast streams (ISO/IEC 14496-3, MPEG-2/4 Part 3).
//
// # Codec vs. container
//
// ADTS is a pure *framing* container over raw AAC access units: each access
// unit is prefixed with a 7-byte fixed+variable header (9 bytes when CRC is
// present) that carries the syncword (0xFFF), MPEG version, profile (the
// AudioObjectType minus one), the samplingFrequencyIndex, the
// channelConfiguration, and the total frame length. It performs **no
// compression of its own** — it adds only the header bytes, exactly as
// [github.com/daniel-sullivan/go-mediatoolkit/containers/ogg] frames Opus packets into pages. The AAC
// bitstream engine, the cgo-vs-native routing, and the FDK license fence all
// live in [github.com/daniel-sullivan/go-mediatoolkit/libraries/aac]; the streaming float64 seam lives
// in [github.com/daniel-sullivan/go-mediatoolkit/codec/aac]. This package is **pure Go, MIT/untagged**,
// and links **zero** FDK-AAC code: it only locates and emits AAC access
// units, never decoding them.
//
// Unlike MP4, ADTS carries no out-of-band [aaclib.AudioSpecificConfig]: the
// decoder configuration is re-derived from the first frame header. A
// [Reader] parses each frame, exposes its access unit as a packet (a
// [github.com/daniel-sullivan/go-mediatoolkit/codec/aac.PacketReader]), and projects the first header
// onto an AudioSpecificConfig so callers can pipe an ADTS stream straight
// into codec/aac without a separate config record. A [Writer] wraps raw AAC
// access units in ADTS headers given the AOT / sample rate / channel count.
//
// Tags/metadata are not part of ADTS; there is no tag surface here.
//
// All access units are raw AAC; this package never touches sample data.
// Neither Reader nor Writer is safe for concurrent use.
package adts

import (
	"github.com/daniel-sullivan/go-mediatoolkit/containers"
	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"
)

// Header is the container Header specialised to ADTS [Extras]. SampleRate,
// Channels, BitRate, and Tags are projected from the first parsed frame; the
// per-frame ADTS specifics live in Extra.
type Header = containers.Header[Extras]

// Extras carries ADTS-specific metadata that does not fit the uniform
// [containers.Header] view: the per-frame header fields recovered from the
// first frame, and the derived [aaclib.AudioSpecificConfig] callers feed to
// codec/aac.
type Extras struct {
	// Config is the AudioSpecificConfig derived from the first frame header
	// (AOT = profile+1, the samplingFrequencyIndex resolved to a rate, and
	// the channelConfiguration). Its Raw holds the two-byte AAC-LC ASC so a
	// re-muxer into MP4 can carry it forward.
	Config aaclib.AudioSpecificConfig

	// MPEGVersion is 0 for MPEG-4 (the common case) or 1 for MPEG-2, taken
	// from the ID bit of the first header.
	MPEGVersion int

	// Profile is the two-bit ADTS profile field of the first header
	// (AudioObjectType minus one): 1 == AAC-LC.
	Profile int

	// SampleRateIndex is the 4-bit samplingFrequencyIndex of the first
	// header.
	SampleRateIndex int

	// ChannelConfiguration is the 3-bit channel-configuration field of the
	// first header.
	ChannelConfiguration int

	// CRCPresent reports whether the first frame carried the optional 2-byte
	// CRC (protection_absent == 0).
	CRCPresent bool

	// Frames counts the ADTS frames the Reader consumed (populated as the
	// stream is read).
	Frames int
}

// ADTS header field sizes and limits (ISO/IEC 13818-7 / 14496-3).
const (
	// HeaderLen is the ADTS header length in bytes without CRC.
	HeaderLen = 7

	// HeaderLenCRC is the ADTS header length in bytes when the optional CRC
	// is present (protection_absent == 0).
	HeaderLenCRC = 9

	// SyncWord is the 12-bit ADTS sync pattern (0xFFF) that opens every
	// frame; on the wire the first byte is 0xFF and the top nibble of the
	// second is 0xF.
	SyncWord = 0xFFF

	// MaxFrameLen bounds the 13-bit aac_frame_length field (header + CRC +
	// payload).
	MaxFrameLen = 0x1FFF
)

// adtsSampleRates is the MPEG-4 sampling-frequency-index table
// (ISO/IEC 14496-3). Indices 13–15 are reserved/explicit and resolve to 0
// here. This mirrors libraries/aac and containers/mp4's tables; ADTS uses the
// same 4-bit index in its header.
var adtsSampleRates = [...]int{
	96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050,
	16000, 12000, 11025, 8000, 7350, 0, 0, 0,
}

// adtsChannelCounts maps the 3-bit ADTS channel-configuration field to a
// channel count. Index 0 means "defined in AOT-specific config" (not used by
// ADTS in practice); 7 denotes 7.1 (eight physical channels).
var adtsChannelCounts = [...]int{0, 1, 2, 3, 4, 5, 6, 8}

// FrameHeader holds the parsed fields of a single ADTS frame header. It is
// the structural decode of the fixed (28-bit) + variable (28-bit) header,
// excluding the syncword.
type FrameHeader struct {
	// MPEGVersion is the ID bit: 0 = MPEG-4, 1 = MPEG-2.
	MPEGVersion int

	// Profile is the 2-bit profile field (AudioObjectType minus one).
	Profile int

	// SampleRateIndex is the 4-bit samplingFrequencyIndex.
	SampleRateIndex int

	// ChannelConfiguration is the 3-bit channel-configuration field.
	ChannelConfiguration int

	// CRCPresent reports whether the optional 2-byte CRC follows the fixed
	// header (the protection_absent bit inverted).
	CRCPresent bool

	// FrameLength is the aac_frame_length field: header + CRC + payload, in
	// bytes.
	FrameLength int

	// RawDataBlocks is number_of_raw_data_blocks_in_frame + 1, i.e. the
	// number of AAC raw data blocks packed in this frame (1 in the common
	// single-block case).
	RawDataBlocks int
}

// ObjectType returns the AAC AudioObjectType the header's profile field
// encodes: Profile + 1 (so profile 1 → AAC-LC).
func (h FrameHeader) ObjectType() aaclib.AudioObjectType {
	return aaclib.AudioObjectType(h.Profile + 1)
}

// SampleRate resolves the header's samplingFrequencyIndex to a rate in Hz, or
// 0 if the index is reserved.
func (h FrameHeader) SampleRate() int {
	if h.SampleRateIndex < 0 || h.SampleRateIndex >= len(adtsSampleRates) {
		return 0
	}
	return adtsSampleRates[h.SampleRateIndex]
}

// Channels resolves the header's channel-configuration field to a channel
// count, or 0 if it is the "defined in config" escape.
func (h FrameHeader) Channels() int {
	if h.ChannelConfiguration < 0 || h.ChannelConfiguration >= len(adtsChannelCounts) {
		return 0
	}
	return adtsChannelCounts[h.ChannelConfiguration]
}

// HeaderSize returns the on-wire header length in bytes (7, or 9 with CRC).
func (h FrameHeader) HeaderSize() int {
	if h.CRCPresent {
		return HeaderLenCRC
	}
	return HeaderLen
}

// AudioSpecificConfig projects the header onto an [aaclib.AudioSpecificConfig]
// (AOT = profile+1, the resolved rate, the channel count) with a freshly
// packed two-byte AAC-LC Raw, so callers can hand it to codec/aac or re-mux
// into MP4. The AAC-LC frame length (1024 samples) is assumed.
func (h FrameHeader) AudioSpecificConfig() aaclib.AudioSpecificConfig {
	asc := aaclib.AudioSpecificConfig{
		ObjectType:   h.ObjectType(),
		SampleRate:   h.SampleRate(),
		Channels:     h.Channels(),
		FrameSamples: aaclib.FrameSamplesShort,
	}
	asc.Raw = packASC(int(asc.ObjectType), h.SampleRateIndex, h.ChannelConfiguration)
	return asc
}

// ParseHeader parses an ADTS frame header from the start of buf. buf must
// begin with the syncword. It returns the decoded [FrameHeader]; it does not
// validate the CRC or that the full frame is present. Returns
// [ErrShortHeader] if buf is shorter than the fixed header, [ErrBadSyncword]
// if the syncword is absent, and [ErrBadFrameLength] if the encoded frame
// length is smaller than its own header.
func ParseHeader(buf []byte) (FrameHeader, error) {
	if len(buf) < HeaderLen {
		return FrameHeader{}, ErrShortHeader
	}
	// Byte 0 = 0xFF; byte 1 top nibble = 0xF (the 12-bit syncword).
	if buf[0] != 0xFF || buf[1]&0xF0 != 0xF0 {
		return FrameHeader{}, ErrBadSyncword
	}

	var h FrameHeader
	// Byte 1: FFFF | ID(1) | layer(2) | protection_absent(1).
	h.MPEGVersion = int((buf[1] >> 3) & 0x01)
	protectionAbsent := buf[1] & 0x01
	h.CRCPresent = protectionAbsent == 0

	// Byte 2: profile(2) | samplingFrequencyIndex(4) | private(1) | chan_hi(1).
	h.Profile = int((buf[2] >> 6) & 0x03)
	h.SampleRateIndex = int((buf[2] >> 2) & 0x0F)
	chanHi := (buf[2] & 0x01) << 2

	// Byte 3: chan_lo(2) | orig/copy(1) | home(1) | copyright_id(1) |
	//         copyright_start(1) | frame_length_hi(2).
	chanLo := (buf[3] >> 6) & 0x03
	h.ChannelConfiguration = int(chanHi | chanLo)

	// aac_frame_length: 13 bits spanning bytes 3..5.
	h.FrameLength = int(buf[3]&0x03)<<11 | int(buf[4])<<3 | int(buf[5]>>5)

	// Byte 6 low 2 bits: number_of_raw_data_blocks_in_frame.
	h.RawDataBlocks = int(buf[6]&0x03) + 1

	if h.FrameLength < h.HeaderSize() {
		return FrameHeader{}, ErrBadFrameLength
	}
	return h, nil
}

// EncodeHeader serialises an ADTS frame header for an access unit of
// payloadLen bytes into dst, returning the number of header bytes written
// (7, or 9 with CRC). dst must have room for [FrameHeader.HeaderSize] bytes.
// The CRC bytes (when CRCPresent) are written as zero placeholders; callers
// computing a CRC fill them afterwards. EncodeHeader sets FrameLength from
// payloadLen + header size and ignores any FrameLength already on h.
//
// It returns [ErrShortHeader] if dst is too small and [ErrBadFrameLength] if
// the resulting frame length exceeds [MaxFrameLen].
func EncodeHeader(dst []byte, h FrameHeader, payloadLen int) (int, error) {
	hdrLen := HeaderLen
	if h.CRCPresent {
		hdrLen = HeaderLenCRC
	}
	if len(dst) < hdrLen {
		return 0, ErrShortHeader
	}
	frameLen := hdrLen + payloadLen
	if frameLen > MaxFrameLen {
		return 0, ErrBadFrameLength
	}

	rdb := h.RawDataBlocks
	if rdb < 1 {
		rdb = 1
	}

	for i := 0; i < hdrLen; i++ {
		dst[i] = 0
	}
	// Byte 0..1: syncword 0xFFF, then ID, layer(00), protection_absent.
	dst[0] = 0xFF
	protectionAbsent := byte(1)
	if h.CRCPresent {
		protectionAbsent = 0
	}
	dst[1] = 0xF0 | byte(h.MPEGVersion&0x01)<<3 | protectionAbsent
	// Byte 2: profile(2) | sfIndex(4) | private(1)=0 | chan_hi(1).
	dst[2] = byte(h.Profile&0x03)<<6 | byte(h.SampleRateIndex&0x0F)<<2 |
		byte(h.ChannelConfiguration>>2)&0x01
	// Byte 3: chan_lo(2) | orig/copy/home/cid/cstart(4)=0 | frame_len_hi(2).
	dst[3] = byte(h.ChannelConfiguration&0x03)<<6 | byte(frameLen>>11)&0x03
	// Byte 4: frame_len_mid(8).
	dst[4] = byte(frameLen >> 3)
	// Byte 5: frame_len_lo(3) | buffer_fullness_hi(5). Fullness 0x7FF (VBR)
	// → top 5 bits all 1.
	dst[5] = byte(frameLen&0x07)<<5 | 0x1F
	// Byte 6: buffer_fullness_lo(6) | number_of_raw_data_blocks(2).
	dst[6] = 0xFC | byte(rdb-1)&0x03
	return hdrLen, nil
}

// packASC packs a two-byte AAC-LC AudioSpecificConfig: audioObjectType(5) |
// samplingFrequencyIndex(4) | channelConfiguration(4), padded to 16 bits.
// Mirrors containers/mp4's encodeAudioSpecificConfig for the AAC-LC case so a
// re-mux carries a byte-identical ASC.
func packASC(objectType, sfIndex, chanConfig int) []byte {
	bits := uint32(objectType&0x1f) << 11
	bits |= uint32(sfIndex&0x0f) << 7
	bits |= uint32(chanConfig&0x0f) << 3
	return []byte{byte(bits >> 8), byte(bits)}
}

// sampleRateIndex returns the 4-bit samplingFrequencyIndex for rate, or
// [ErrUnsupportedSampleRate]'s sentinel index 15 when rate is not in the
// table. Used by the Writer.
func sampleRateIndex(rate int) (int, bool) {
	for i, r := range adtsSampleRates {
		if r != 0 && r == rate {
			return i, true
		}
	}
	return 15, false
}

// channelConfigIndex returns the 3-bit channel-configuration field for a
// channel count, or 0 when the count has no direct mapping.
func channelConfigIndex(channels int) (int, bool) {
	for i, c := range adtsChannelCounts {
		if c == channels {
			return i, true
		}
	}
	return 0, false
}
