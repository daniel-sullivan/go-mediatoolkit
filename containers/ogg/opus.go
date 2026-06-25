package ogg

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand/v2"

	"github.com/daniel-sullivan/go-mediatoolkit/containers"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// OpusHead is the identification header from an Opus-in-Ogg stream
// (Opus RFC 7845 §5.1).
type OpusHead struct {
	Version             uint8
	Channels            uint8
	PreSkip             uint16
	InputSampleRate     uint32 // 0 means "unspecified"
	OutputGain          int16
	ChannelMapping      uint8
	StreamCount         uint8  // only set when ChannelMapping != 0
	CoupledCount        uint8  // only set when ChannelMapping != 0
	ChannelMappingTable []byte // only set when ChannelMapping != 0
}

// OpusExtras is the format-specific extras for an Opus-in-Ogg container.
type OpusExtras struct {
	Ogg  Extras
	Head OpusHead

	// Vendor is the "vendor string" from the OpusTags packet (typically the
	// encoder identification string).
	Vendor string

	// SerialNo is the serial number of the Opus logical stream.
	SerialNo int32
}

// OpusHeader is the Header specialised to Opus-in-Ogg.
type OpusHeader = containers.Header[OpusExtras]

// OpusReader reads an Opus-in-Ogg file.
type OpusReader struct {
	ogg    *Reader
	stream *Stream
	header OpusHeader
}

// NewOpusReader parses an Ogg file, locates the first Opus logical stream,
// reads its two header packets (OpusHead + OpusTags) and returns an
// OpusReader ready to supply audio packets.
//
// The returned [OpusReader] implements [containers.PacketReader] so it can
// be passed directly to [github.com/daniel-sullivan/go-mediatoolkit/codec/opus.NewDecoder] (the
// [codec/opus.PacketReader] interface is structurally compatible).
func NewOpusReader(r io.Reader) (*OpusReader, error) {
	oreader, err := NewReader(r)
	if err != nil {
		return nil, err
	}

	var stream *Stream
	for _, s := range oreader.Streams() {
		if s.CodecHint == "opus" {
			stream = s
			break
		}
	}
	if stream == nil {
		return nil, ErrNoOpusStream
	}

	// The BOS packet is already in HeaderPackets[0]. Pull one more for OpusTags.
	if err := stream.readHeaderPackets(1); err != nil {
		return nil, err
	}

	head, err := parseOpusHead(stream.HeaderPackets[0])
	if err != nil {
		return nil, err
	}
	vendor, tags, err := parseOpusTags(stream.HeaderPackets[1])
	if err != nil {
		return nil, err
	}

	sampleRate := int(head.InputSampleRate)
	if sampleRate == 0 {
		sampleRate = 48000 // Opus always decodes at 48 kHz
	}

	header := OpusHeader{
		Format:       "ogg/opus",
		SampleRate:   sampleRate,
		Channels:     int(head.Channels),
		SampleFormat: mutations.FormatFloat64,
		Tags:         containers.StandardTagsFromMap(tags),
		Extra: OpusExtras{
			Ogg:      oreader.Header().Extra,
			Head:     head,
			Vendor:   vendor,
			SerialNo: stream.SerialNo,
		},
	}

	return &OpusReader{
		ogg:    oreader,
		stream: stream,
		header: header,
	}, nil
}

// Header returns the fully-populated Opus container header.
func (r *OpusReader) Header() OpusHeader { return r.header }

// ReadPacket returns the next Opus data packet (OpusHead/OpusTags are
// already skipped). Returns io.EOF when the stream ends.
func (r *OpusReader) ReadPacket() ([]byte, error) {
	return r.stream.ReadPacket()
}

// OpusWriter writes an Opus-in-Ogg file.
type OpusWriter struct {
	ogg      *Writer
	stream   *StreamWriter
	closed   bool
	samples  int64 // running granule counter (48 kHz samples)
	channels int
}

// OpusWriterOption configures an [OpusWriter].
type OpusWriterOption func(*opusWriterConfig)

type opusWriterConfig struct {
	serialNo        int32
	preSkip         uint16
	inputSampleRate uint32
	outputGain      int16
	vendor          string
	tags            containers.StandardTags
}

// WithOpusSerialNo sets a fixed serial number. Default: a random value.
func WithOpusSerialNo(s int32) OpusWriterOption {
	return func(c *opusWriterConfig) { c.serialNo = s }
}

// WithOpusPreSkip sets the pre-skip value (samples of codec delay to discard).
// Default: 312 samples (6.5 ms), a conservative value that works for any
// Opus configuration.
func WithOpusPreSkip(n uint16) OpusWriterOption {
	return func(c *opusWriterConfig) { c.preSkip = n }
}

// WithOpusInputSampleRate records the original input sample rate. This is
// informational only — Opus always decodes at 48 kHz. Default: 48000.
func WithOpusInputSampleRate(hz uint32) OpusWriterOption {
	return func(c *opusWriterConfig) { c.inputSampleRate = hz }
}

// WithOpusOutputGain applies a fixed output gain (Q8 dB units). Default: 0.
func WithOpusOutputGain(q8 int16) OpusWriterOption {
	return func(c *opusWriterConfig) { c.outputGain = q8 }
}

// WithOpusVendor sets the vendor string written into OpusTags.
// Default: "go-mediatoolkit".
func WithOpusVendor(v string) OpusWriterOption {
	return func(c *opusWriterConfig) { c.vendor = v }
}

// WithOpusTags sets the user comment tags written into OpusTags.
func WithOpusTags(t containers.StandardTags) OpusWriterOption {
	return func(c *opusWriterConfig) { c.tags = t }
}

// NewOpusWriter returns an OpusWriter that emits an Ogg-framed Opus file
// with the given channel count.
func NewOpusWriter(w io.Writer, channels int, opts ...OpusWriterOption) (*OpusWriter, error) {
	if channels < 1 || channels > 255 {
		return nil, fmt.Errorf("ogg: invalid Opus channel count %d", channels)
	}
	cfg := opusWriterConfig{
		serialNo:        int32(rand.Uint32()),
		preSkip:         312,
		inputSampleRate: 48000,
		vendor:          "go-mediatoolkit",
	}
	for _, o := range opts {
		o(&cfg)
	}

	owriter := NewWriter(w)
	stream, err := owriter.AddStream(cfg.serialNo)
	if err != nil {
		return nil, err
	}

	headPkt := buildOpusHead(OpusHead{
		Version:         1,
		Channels:        uint8(channels),
		PreSkip:         cfg.preSkip,
		InputSampleRate: cfg.inputSampleRate,
		OutputGain:      cfg.outputGain,
		ChannelMapping:  0, // simple mono/stereo mapping
	})
	tagsPkt := buildOpusTags(cfg.vendor, cfg.tags)

	// Per RFC 7845, OpusHead must be the sole packet on the BOS page and
	// OpusTags must be the sole packet on the following page. The underlying
	// ogg encoder emits one page per packet when we flush after each.
	if err := stream.WritePacket(headPkt); err != nil {
		return nil, err
	}
	// Force OpusHead onto its own BOS page, per RFC 7845.
	if err := stream.forceFlush(); err != nil {
		return nil, err
	}
	if err := stream.WritePacket(tagsPkt); err != nil {
		return nil, err
	}
	if err := stream.forceFlush(); err != nil {
		return nil, err
	}

	return &OpusWriter{
		ogg:      owriter,
		stream:   stream,
		channels: channels,
	}, nil
}

// WritePacket appends an encoded Opus packet to the file. The caller is
// responsible for computing the packet's sample count and calling
// [OpusWriter.AdvanceGranule] to keep the granule position accurate; by
// default WritePacket assumes a standard 20 ms frame at 48 kHz (960 samples)
// per packet.
func (w *OpusWriter) WritePacket(data []byte) error {
	// Assume 20 ms / 48 kHz = 960 samples unless the caller overrode via
	// AdvanceGranule before calling WritePacket.
	return w.writePacketWithAdvance(data, 960)
}

// WritePacketWithFrames advances the granule counter by samples before
// writing the packet, so the page's granule position reflects the samples
// produced up to and including this packet.
func (w *OpusWriter) WritePacketWithFrames(data []byte, samples int) error {
	return w.writePacketWithAdvance(data, samples)
}

func (w *OpusWriter) writePacketWithAdvance(data []byte, samples int) error {
	w.samples += int64(samples)
	w.stream.SetGranule(w.samples)
	return w.stream.WritePacket(data)
}

// Close flushes any remaining data and writes the EOS marker.
func (w *OpusWriter) Close() error {
	if w.closed {
		return ErrAlreadyClosed
	}
	w.closed = true
	w.stream.SetGranule(w.samples)
	w.stream.SetEOS()
	return w.ogg.Close()
}

// --- OpusHead / OpusTags packet helpers ---

func parseOpusHead(pkt []byte) (OpusHead, error) {
	var h OpusHead
	if len(pkt) < 19 || !bytes.HasPrefix(pkt, []byte("OpusHead")) {
		return h, ErrBadOpusHead
	}
	h.Version = pkt[8]
	h.Channels = pkt[9]
	h.PreSkip = binary.LittleEndian.Uint16(pkt[10:12])
	h.InputSampleRate = binary.LittleEndian.Uint32(pkt[12:16])
	h.OutputGain = int16(binary.LittleEndian.Uint16(pkt[16:18]))
	h.ChannelMapping = pkt[18]
	if h.ChannelMapping != 0 {
		if len(pkt) < 21 {
			return h, ErrBadOpusHead
		}
		h.StreamCount = pkt[19]
		h.CoupledCount = pkt[20]
		wantMap := int(h.Channels)
		if len(pkt) < 21+wantMap {
			return h, ErrBadOpusHead
		}
		h.ChannelMappingTable = append([]byte{}, pkt[21:21+wantMap]...)
	}
	return h, nil
}

func buildOpusHead(h OpusHead) []byte {
	var buf bytes.Buffer
	buf.WriteString("OpusHead")
	buf.WriteByte(h.Version)
	buf.WriteByte(h.Channels)
	var u16 [2]byte
	binary.LittleEndian.PutUint16(u16[:], h.PreSkip)
	buf.Write(u16[:])
	var u32 [4]byte
	binary.LittleEndian.PutUint32(u32[:], h.InputSampleRate)
	buf.Write(u32[:])
	binary.LittleEndian.PutUint16(u16[:], uint16(h.OutputGain))
	buf.Write(u16[:])
	buf.WriteByte(h.ChannelMapping)
	if h.ChannelMapping != 0 {
		buf.WriteByte(h.StreamCount)
		buf.WriteByte(h.CoupledCount)
		buf.Write(h.ChannelMappingTable)
	}
	return buf.Bytes()
}

func parseOpusTags(pkt []byte) (vendor string, tags containers.Tags, err error) {
	tags = containers.NewTags()
	if len(pkt) < 8+4 || !bytes.HasPrefix(pkt, []byte("OpusTags")) {
		return "", tags, ErrBadOpusTags
	}
	off := 8
	vendorLen := int(binary.LittleEndian.Uint32(pkt[off : off+4]))
	off += 4
	if off+vendorLen > len(pkt) {
		return "", tags, ErrBadOpusTags
	}
	vendor = string(pkt[off : off+vendorLen])
	off += vendorLen

	if off+4 > len(pkt) {
		return "", tags, ErrBadOpusTags
	}
	commentCount := int(binary.LittleEndian.Uint32(pkt[off : off+4]))
	off += 4

	for i := 0; i < commentCount; i++ {
		if off+4 > len(pkt) {
			return "", tags, ErrBadOpusTags
		}
		n := int(binary.LittleEndian.Uint32(pkt[off : off+4]))
		off += 4
		if off+n > len(pkt) {
			return "", tags, ErrBadOpusTags
		}
		entry := string(pkt[off : off+n])
		off += n
		eq := bytesIndexByte(entry, '=')
		if eq <= 0 {
			continue
		}
		key := entry[:eq]
		value := entry[eq+1:]
		tags.Add(key, value)
	}
	return vendor, tags, nil
}

func buildOpusTags(vendor string, st containers.StandardTags) []byte {
	tags := st.Map()
	var buf bytes.Buffer
	buf.WriteString("OpusTags")

	var u32 [4]byte
	binary.LittleEndian.PutUint32(u32[:], uint32(len(vendor)))
	buf.Write(u32[:])
	buf.WriteString(vendor)

	// Count total entries (Vorbis comments allow multiple values per key;
	// each is emitted as a separate KEY=value line).
	count := 0
	for _, values := range tags {
		count += len(values)
	}
	binary.LittleEndian.PutUint32(u32[:], uint32(count))
	buf.Write(u32[:])

	for key, values := range tags {
		for _, v := range values {
			entry := key + "=" + v
			binary.LittleEndian.PutUint32(u32[:], uint32(len(entry)))
			buf.Write(u32[:])
			buf.WriteString(entry)
		}
	}
	return buf.Bytes()
}

func bytesIndexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
