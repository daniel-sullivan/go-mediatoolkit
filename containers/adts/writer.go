package adts

import (
	"io"

	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"
)

// Writer wraps raw AAC access units in ADTS frame headers and writes the
// framed bytes to an [io.Writer]. It is configured once with the AOT / sample
// rate / channel count (the fixed-header fields shared by every frame) and
// then frames each access unit handed to [Writer.WritePacket], computing the
// per-frame aac_frame_length and, when enabled, the CRC.
//
// Writer implements [github.com/daniel-sullivan/go-mediatoolkit/codec/aac.PacketWriter], so an encoder
// pipeline (codec/aac.NewEncoder) can emit a standalone `.aac` stream by
// pointing its PacketWriter at a Writer.
type Writer struct {
	w           io.Writer
	mpegVersion int
	profile     int
	sfIndex     int
	chanConfig  int
	channels    int
	crc         bool
	frames      int
	hdr         [HeaderLenCRC]byte // reusable header scratch
}

// WriterOption configures a [Writer].
type WriterOption func(*Writer)

// WithObjectType selects the AAC profile to advertise in the ADTS header
// (default [aaclib.AOTAACLC]). The header's 2-bit profile field is the object
// type minus one.
func WithObjectType(t aaclib.AudioObjectType) WriterOption {
	return func(w *Writer) {
		p := int(t) - 1
		if p < 0 {
			p = 0
		}
		w.profile = p & 0x03
	}
}

// WithMPEGVersion selects the ID bit: 0 for MPEG-4 (default), 1 for MPEG-2.
func WithMPEGVersion(v int) WriterOption {
	return func(w *Writer) { w.mpegVersion = v & 0x01 }
}

// WithCRC enables the optional 2-byte ADTS CRC (protection_absent = 0). The
// CRC is a 16-bit CRC over the header and payload as specified for ADTS; when
// disabled (the default) frames carry a 7-byte header.
func WithCRC(enabled bool) WriterOption {
	return func(w *Writer) { w.crc = enabled }
}

// NewWriter constructs a Writer emitting ADTS frames to w. sampleRate and
// channels must map to an MPEG-4 samplingFrequencyIndex and
// channel-configuration respectively; otherwise NewWriter returns
// [ErrUnsupportedSampleRate] / [ErrUnsupportedChannels]. The default profile
// is AAC-LC and CRC is disabled.
func NewWriter(w io.Writer, sampleRate, channels int, opts ...WriterOption) (*Writer, error) {
	if w == nil {
		return nil, ErrBadArg
	}
	sfIndex, ok := sampleRateIndex(sampleRate)
	if !ok {
		return nil, ErrUnsupportedSampleRate
	}
	chanConfig, ok := channelConfigIndex(channels)
	if !ok || chanConfig == 0 {
		return nil, ErrUnsupportedChannels
	}

	ww := &Writer{
		w:          w,
		profile:    int(aaclib.AOTAACLC) - 1, // AAC-LC
		sfIndex:    sfIndex,
		chanConfig: chanConfig,
		channels:   channels,
	}
	for _, o := range opts {
		o(ww)
	}
	return ww, nil
}

// WritePacket frames one raw AAC access unit in an ADTS header and writes it.
// It computes the frame length, encodes the header, fills the CRC when
// enabled, and writes header + (CRC) + payload. Returns [ErrPacketTooLarge]
// if the framed length exceeds the 13-bit aac_frame_length maximum.
func (w *Writer) WritePacket(au []byte) error {
	hdrLen := HeaderLen
	if w.crc {
		hdrLen = HeaderLenCRC
	}
	if hdrLen+len(au) > MaxFrameLen {
		return ErrPacketTooLarge
	}

	h := FrameHeader{
		MPEGVersion:          w.mpegVersion,
		Profile:              w.profile,
		SampleRateIndex:      w.sfIndex,
		ChannelConfiguration: w.chanConfig,
		CRCPresent:           w.crc,
		RawDataBlocks:        1,
	}
	n, err := EncodeHeader(w.hdr[:], h, len(au))
	if err != nil {
		return err
	}

	if w.crc {
		// The ADTS CRC covers the 7 header bytes (with the CRC field treated
		// as absent) plus the payload, per ISO/IEC 13818-7. Compute over the
		// fixed header followed by the access unit, then store big-endian in
		// the two CRC bytes (header bytes 7..8).
		crc := crc16ADTS(w.hdr[:HeaderLen], au)
		w.hdr[7] = byte(crc >> 8)
		w.hdr[8] = byte(crc)
	}

	if _, err := w.w.Write(w.hdr[:n]); err != nil {
		return err
	}
	if _, err := w.w.Write(au); err != nil {
		return err
	}
	w.frames++
	return nil
}

// Frames returns the number of ADTS frames written so far.
func (w *Writer) Frames() int { return w.frames }

// ASC returns the [aaclib.AudioSpecificConfig] the Writer's frames imply
// (AOT = profile+1, the configured rate and channels). Useful when the same
// stream must also be described out-of-band (e.g. re-muxed into MP4).
func (w *Writer) ASC() aaclib.AudioSpecificConfig {
	objectType := w.profile + 1
	asc := aaclib.AudioSpecificConfig{
		ObjectType:   aaclib.AudioObjectType(objectType),
		SampleRate:   adtsSampleRates[w.sfIndex],
		Channels:     w.channels,
		FrameSamples: aaclib.FrameSamplesShort,
	}
	asc.Raw = packASC(objectType, w.sfIndex, w.chanConfig)
	return asc
}

// crc16ADTS computes the 16-bit CRC used by ADTS error protection over the
// concatenation of the given byte runs. ADTS uses the MPEG-2 CRC-16 with
// polynomial 0x8005, initial value 0xFFFF, MSB-first, no final XOR — the same
// generator the AAC/MPEG bitstream uses for its error_check field.
func crc16ADTS(parts ...[]byte) uint16 {
	const poly = 0x8005
	crc := uint16(0xFFFF)
	for _, p := range parts {
		for _, b := range p {
			crc ^= uint16(b) << 8
			for i := 0; i < 8; i++ {
				if crc&0x8000 != 0 {
					crc = (crc << 1) ^ poly
				} else {
					crc <<= 1
				}
			}
		}
	}
	return crc
}
