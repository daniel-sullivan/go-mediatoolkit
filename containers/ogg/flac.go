package ogg

import (
	"bytes"
	"encoding/binary"
	"io"
	"math/rand/v2"

	"github.com/daniel-sullivan/go-mediatoolkit/containers"
	flaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/flac"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// FLACHead mirrors the Ogg-FLAC ID header (xiph "Ogg encapsulation for
// the FLAC Codec" mapping, §2.1). The header occupies the first packet
// of the BOS page and carries the FLAC mapping version, the count of
// remaining metadata packets, the native FLAC magic, and the
// STREAMINFO block.
type FLACHead struct {
	// MajorVersion / MinorVersion identify the Ogg-FLAC mapping
	// version. The current spec defines 1.0; readers tolerate
	// non-zero MinorVersion and reject any other MajorVersion.
	MajorVersion uint8
	MinorVersion uint8

	// NumOtherHeaders is the count of metadata packets that follow
	// the BOS packet (not counting the BOS itself). Zero means
	// "unknown" — readers must scan until they observe a metadata
	// block whose is_last flag is set.
	NumOtherHeaders uint16

	// StreamInfoBody is the 34-byte STREAMINFO body (no metadata
	// block header). Decoded fields are exposed below for
	// convenience.
	StreamInfoBody [34]byte

	// SampleRate, Channels, BitsPerSample, TotalSamples are decoded
	// from StreamInfoBody and surfaced for direct consumption by
	// the container Header.
	SampleRate    uint32
	Channels      uint8
	BitsPerSample uint8
	TotalSamples  uint64

	// MinBlockSize, MaxBlockSize, MinFrameSize, MaxFrameSize are the
	// remaining STREAMINFO fields.
	MinBlockSize uint16
	MaxBlockSize uint16
	MinFrameSize uint32
	MaxFrameSize uint32

	// MD5Signature is the STREAMINFO MD5 (zero when unknown).
	MD5Signature [16]byte
}

// FLACExtras is the format-specific extras for an Ogg-FLAC container.
type FLACExtras struct {
	Ogg  Extras
	Head FLACHead

	// Vendor is the VORBIS_COMMENT vendor string parsed from the
	// stream's tag packet.
	Vendor string

	// SerialNo is the serial number of the FLAC logical stream.
	SerialNo int32

	// MetadataBlocks preserves every metadata block's bytes (header
	// + body) other than STREAMINFO, in the order they appeared.
	// Used by [NewFLACReader.Data] to rebuild a synthetic native
	// FLAC byte stream for the libFLAC decoder.
	MetadataBlocks [][]byte
}

// FLACHeader is the Header specialised to Ogg-FLAC.
type FLACHeader = containers.Header[FLACExtras]

// FLACReader reads an Ogg-FLAC file.
//
// The reader parses the BOS Ogg-FLAC ID packet and any subsequent
// metadata packets, then yields the audio frames as a single
// continuous byte stream that re-prepends the native "fLaC" magic and
// metadata blocks. Pass [FLACReader.Data] to
// [github.com/daniel-sullivan/go-mediatoolkit/libraries/flac.NewDecoder] to decode samples.
type FLACReader struct {
	ogg    *Reader
	stream *Stream
	header FLACHeader
	data   io.Reader
}

// NewFLACReader parses the Ogg framing in r, locates the first FLAC
// logical stream, reads the mapping header + metadata packets, and
// returns a reader ready to supply a synthetic native FLAC byte stream.
func NewFLACReader(r io.Reader) (*FLACReader, error) {
	oreader, err := NewReader(r)
	if err != nil {
		return nil, err
	}

	var stream *Stream
	for _, s := range oreader.Streams() {
		if s.CodecHint == "flac" {
			stream = s
			break
		}
	}
	if stream == nil {
		return nil, ErrNoFLACStream
	}

	bos := stream.HeaderPackets[0]
	head, streamInfoBlock, err := parseOggFLACBOS(bos)
	if err != nil {
		return nil, err
	}

	// Pull the remaining metadata packets. If the count is known,
	// take exactly that many; otherwise pull until a packet's first
	// byte signals is_last.
	var metaBlocks [][]byte
	var vendor string
	rawTags := containers.NewTags()
	if head.NumOtherHeaders > 0 {
		for i := uint16(0); i < head.NumOtherHeaders; i++ {
			pkt, err := stream.pump.pullPacketFor(stream.SerialNo)
			if err != nil {
				return nil, err
			}
			block := append([]byte(nil), pkt.Data...)
			metaBlocks = append(metaBlocks, block)
			stream.HeaderPackets = append(stream.HeaderPackets, block)
			collectMetadataTags(block, &vendor, rawTags)
		}
	} else {
		for {
			pkt, err := stream.pump.pullPacketFor(stream.SerialNo)
			if err != nil {
				return nil, err
			}
			block := append([]byte(nil), pkt.Data...)
			metaBlocks = append(metaBlocks, block)
			stream.HeaderPackets = append(stream.HeaderPackets, block)
			collectMetadataTags(block, &vendor, rawTags)
			if len(block) > 0 && block[0]&0x80 != 0 {
				break // is_last set
			}
		}
	}

	// Force the LAST metadata block's is_last flag to 1 so the
	// synthesised native FLAC stream is well-formed even when the
	// upstream Ogg-FLAC packets carry stale flags.
	if n := len(metaBlocks); n > 0 {
		metaBlocks[n-1][0] |= 0x80
	}

	header := FLACHeader{
		Format:       "ogg/flac",
		SampleRate:   int(head.SampleRate),
		Channels:     int(head.Channels),
		SampleFormat: mutations.FormatFloat64,
		Tags:         containers.StandardTagsFromMap(rawTags),
		Extra: FLACExtras{
			Ogg:            oreader.Header().Extra,
			Head:           head,
			Vendor:         vendor,
			SerialNo:       stream.SerialNo,
			MetadataBlocks: metaBlocks,
		},
	}

	// Build the native FLAC prefix (magic + STREAMINFO block + other
	// metadata blocks). Audio frames are appended one packet at a
	// time via the streaming reader.
	prefix := buildNativeFLACPrefix(streamInfoBlock, metaBlocks, head.NumOtherHeaders == 0)

	return &FLACReader{
		ogg:    oreader,
		stream: stream,
		header: header,
		data:   io.MultiReader(bytes.NewReader(prefix), &flacFrameStream{stream: stream}),
	}, nil
}

// Header returns the parsed Ogg-FLAC header.
func (r *FLACReader) Header() FLACHeader { return r.header }

// Data returns an io.Reader yielding a synthetic native FLAC byte
// stream — magic + metadata blocks (with is_last properly stamped) +
// audio frames concatenated. Hand it to
// [github.com/daniel-sullivan/go-mediatoolkit/libraries/flac.NewDecoder] to decode samples.
func (r *FLACReader) Data() io.Reader { return r.data }

// flacFrameStream concatenates the raw bytes of each subsequent Ogg
// packet (one FLAC frame per packet) into a continuous io.Reader.
type flacFrameStream struct {
	stream *Stream
	buf    []byte // remainder of the most recently fetched packet
	eof    bool
}

func (f *flacFrameStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(f.buf) == 0 {
		if f.eof {
			return 0, io.EOF
		}
		pkt, err := f.stream.pump.pullPacketFor(f.stream.SerialNo)
		if err == io.EOF {
			f.eof = true
			return 0, io.EOF
		}
		if err != nil {
			return 0, err
		}
		// pkt.Data is owned by libogg's queue; copy so we can hand it
		// out across multiple Read calls without aliasing.
		f.buf = append([]byte(nil), pkt.Data...)
	}
	n := copy(p, f.buf)
	f.buf = f.buf[n:]
	return n, nil
}

// parseOggFLACBOS parses the BOS packet of an Ogg-FLAC stream and
// returns the decoded mapping header along with the verbatim
// 4+34-byte STREAMINFO metadata block embedded inside.
func parseOggFLACBOS(pkt []byte) (FLACHead, []byte, error) {
	// Fixed prefix: 0x7F + "FLAC" + ver_major(1) + ver_minor(1) +
	// num_other(2) + "fLaC" = 13 bytes; followed by a STREAMINFO
	// metadata block (4-byte header + 34-byte body) = 38 bytes.
	const minLen = 13 + 38
	if len(pkt) < minLen {
		return FLACHead{}, nil, ErrBadFLACHead
	}
	if pkt[0] != 0x7F || string(pkt[1:5]) != "FLAC" {
		return FLACHead{}, nil, ErrBadFLACHead
	}

	var h FLACHead
	h.MajorVersion = pkt[5]
	h.MinorVersion = pkt[6]
	if h.MajorVersion != 1 {
		return FLACHead{}, nil, ErrBadFLACHead
	}
	h.NumOtherHeaders = binary.BigEndian.Uint16(pkt[7:9])
	if string(pkt[9:13]) != "fLaC" {
		return FLACHead{}, nil, ErrBadFLACHead
	}

	// STREAMINFO: 4-byte block header + 34-byte body.
	siBlock := pkt[13:51]
	if siBlock[0]&0x7F != 0 { // type 0 = STREAMINFO
		return FLACHead{}, nil, ErrBadFLACHead
	}
	bodyLen := uint32(siBlock[1])<<16 | uint32(siBlock[2])<<8 | uint32(siBlock[3])
	if bodyLen != 34 {
		return FLACHead{}, nil, ErrBadFLACHead
	}
	body := siBlock[4:38]
	copy(h.StreamInfoBody[:], body)

	h.MinBlockSize = binary.BigEndian.Uint16(body[0:2])
	h.MaxBlockSize = binary.BigEndian.Uint16(body[2:4])
	h.MinFrameSize = uint32(body[4])<<16 | uint32(body[5])<<8 | uint32(body[6])
	h.MaxFrameSize = uint32(body[7])<<16 | uint32(body[8])<<8 | uint32(body[9])
	packed := binary.BigEndian.Uint64(body[10:18])
	h.SampleRate = uint32(packed >> 44)
	h.Channels = uint8((packed>>41)&0x7) + 1
	h.BitsPerSample = uint8((packed>>36)&0x1F) + 1
	h.TotalSamples = packed & ((uint64(1) << 36) - 1)
	copy(h.MD5Signature[:], body[18:34])

	return h, append([]byte(nil), siBlock...), nil
}

// collectMetadataTags walks a single metadata block and, if it is a
// VORBIS_COMMENT, updates vendor + tags. Other block types are ignored.
func collectMetadataTags(block []byte, vendor *string, tags containers.Tags) {
	if len(block) < 4 {
		return
	}
	const vorbisType = 4
	if block[0]&0x7F != vorbisType {
		return
	}
	body := block[4:]
	if len(body) < 4 {
		return
	}
	off := 0
	vlen := int(binary.LittleEndian.Uint32(body[off : off+4]))
	off += 4
	if off+vlen > len(body) {
		return
	}
	*vendor = string(body[off : off+vlen])
	off += vlen
	if off+4 > len(body) {
		return
	}
	count := int(binary.LittleEndian.Uint32(body[off : off+4]))
	off += 4
	for i := 0; i < count; i++ {
		if off+4 > len(body) {
			return
		}
		entryLen := int(binary.LittleEndian.Uint32(body[off : off+4]))
		off += 4
		if off+entryLen > len(body) {
			return
		}
		entry := string(body[off : off+entryLen])
		off += entryLen
		eq := bytesIndexByte(entry, '=')
		if eq <= 0 {
			continue
		}
		tags.Add(entry[:eq], entry[eq+1:])
	}
}

// buildNativeFLACPrefix concatenates the native "fLaC" magic, the
// STREAMINFO block (with is_last cleared, since other metadata blocks
// follow), and the remaining metadata blocks. The last block in the
// list has its is_last flag forced on by the caller before this is
// invoked.
func buildNativeFLACPrefix(streamInfoBlock []byte, metaBlocks [][]byte, _ bool) []byte {
	var buf bytes.Buffer
	buf.WriteString("fLaC")

	// Stamp STREAMINFO's is_last bit based on whether more blocks follow.
	si := append([]byte(nil), streamInfoBlock...)
	if len(metaBlocks) > 0 {
		si[0] &^= 0x80
	} else {
		si[0] |= 0x80
	}
	buf.Write(si)

	for _, b := range metaBlocks {
		buf.Write(b)
	}
	return buf.Bytes()
}

// FLACWriter encodes a FLAC stream and emits Ogg-FLAC pages.
//
// Internally, FLACWriter wraps a [libraries/flac.Encoder] whose output
// is captured in an in-memory buffer; on every [FLACWriter.Encode]
// call (and on Close), the captured bytes are split into a BOS ID
// packet, additional metadata packets, and one packet per audio frame,
// which are then handed to the underlying [StreamWriter].
//
// Frame boundaries are located by scanning for the next FLAC sync code
// (0xFF 0xF8 / 0xF9) and validating each candidate via the 16-bit
// CRC-16 footer the FLAC format mandates. False sync inside frame data
// is therefore rejected; the splitter is robust to typical FLAC
// payloads.
type FLACWriter struct {
	ogg    *Writer
	stream *StreamWriter
	enc    flaclib.Encoder
	buffer *bytes.Buffer

	channels      int
	sampleRate    int
	bitsPerSample int

	state        splitState
	pendingFrame []byte // bytes from a frame whose end has not yet been observed
	frameSamples int    // running granule (inter-channel sample count)
	prevSamples  int    // samples accumulated across all completed frames
	closed       bool
}

type splitState int

const (
	stateMagic splitState = iota
	stateMetadata
	stateFrames
	stateClosed
)

// FLACWriterOption configures a [FLACWriter].
type FLACWriterOption func(*flacWriterConfig)

type flacWriterConfig struct {
	serialNo      int32
	bitsPerSample int
	compression   int
	verify        bool
	blockSize     int
	totalSamples  uint64
	tags          [][2]string
	vendor        string
}

// WithFLACSerialNo sets a fixed serial number. Default: random.
func WithFLACSerialNo(s int32) FLACWriterOption {
	return func(c *flacWriterConfig) { c.serialNo = s }
}

// WithFLACBitsPerSample sets the per-sample bit depth (default 16).
func WithFLACBitsPerSample(bits int) FLACWriterOption {
	return func(c *flacWriterConfig) { c.bitsPerSample = bits }
}

// WithFLACCompressionLevel sets the libFLAC compression level [0, 8].
func WithFLACCompressionLevel(l int) FLACWriterOption {
	return func(c *flacWriterConfig) { c.compression = l }
}

// WithFLACVerify enables encoder self-verification.
func WithFLACVerify(enable bool) FLACWriterOption {
	return func(c *flacWriterConfig) { c.verify = enable }
}

// WithFLACBlockSize sets a fixed block size (samples per channel).
func WithFLACBlockSize(samples int) FLACWriterOption {
	return func(c *flacWriterConfig) { c.blockSize = samples }
}

// WithFLACTotalSamples declares the total number of inter-channel
// samples the caller will submit before [FLACWriter.Close].
func WithFLACTotalSamples(n uint64) FLACWriterOption {
	return func(c *flacWriterConfig) { c.totalSamples = n }
}

// WithFLACTag adds a single VORBIS_COMMENT entry. Repeated calls with
// the same key append additional values.
func WithFLACTag(key, value string) FLACWriterOption {
	return func(c *flacWriterConfig) {
		c.tags = append(c.tags, [2]string{key, value})
	}
}

// WithFLACVendor sets the VORBIS_COMMENT vendor string. libFLAC
// overrides this with its own identification when the cgo backend is
// used (see [libraries/flac.WithVendor]).
func WithFLACVendor(v string) FLACWriterOption {
	return func(c *flacWriterConfig) { c.vendor = v }
}

// NewFLACWriter returns a writer that emits an Ogg-encapsulated FLAC
// stream to w.
func NewFLACWriter(w io.Writer, sampleRate, channels int, opts ...FLACWriterOption) (*FLACWriter, error) {
	cfg := flacWriterConfig{
		serialNo:      int32(rand.Uint32()),
		bitsPerSample: 16,
		compression:   5,
	}
	for _, o := range opts {
		o(&cfg)
	}

	owriter := NewWriter(w)
	stream, err := owriter.AddStream(cfg.serialNo)
	if err != nil {
		return nil, err
	}

	fw := &FLACWriter{
		ogg:           owriter,
		stream:        stream,
		buffer:        &bytes.Buffer{},
		channels:      channels,
		sampleRate:    sampleRate,
		bitsPerSample: cfg.bitsPerSample,
	}

	info := flaclib.StreamInfo{
		SampleRate:    sampleRate,
		Channels:      channels,
		BitsPerSample: cfg.bitsPerSample,
	}
	libOpts := []flaclib.EncoderOption{
		flaclib.WithCompressionLevel(cfg.compression),
	}
	if cfg.verify {
		libOpts = append(libOpts, flaclib.WithVerify(true))
	}
	if cfg.blockSize > 0 {
		libOpts = append(libOpts, flaclib.WithBlockSize(cfg.blockSize))
	}
	if cfg.totalSamples > 0 {
		libOpts = append(libOpts, flaclib.WithTotalSamples(cfg.totalSamples))
	}
	if cfg.vendor != "" {
		libOpts = append(libOpts, flaclib.WithVendor(cfg.vendor))
	}
	for _, kv := range cfg.tags {
		libOpts = append(libOpts, flaclib.WithTag(kv[0], kv[1]))
	}
	enc, err := flaclib.NewEncoder(fw.buffer, info, libOpts...)
	if err != nil {
		return nil, err
	}
	fw.enc = enc
	return fw, nil
}

// Header returns a Header summarising the configured stream.
func (w *FLACWriter) Header() FLACHeader {
	return FLACHeader{
		Format:       "ogg/flac",
		SampleRate:   w.sampleRate,
		Channels:     w.channels,
		SampleFormat: mutations.FormatFloat64,
		Extra: FLACExtras{
			Head: FLACHead{
				MajorVersion:  1,
				SampleRate:    uint32(w.sampleRate),
				Channels:      uint8(w.channels),
				BitsPerSample: uint8(w.bitsPerSample),
			},
			SerialNo: w.stream.SerialNo,
		},
	}
}

// Encode submits one block of interleaved int32 samples.
func (w *FLACWriter) Encode(samples []int32) error {
	if w.closed {
		return ErrAlreadyClosed
	}
	if err := w.enc.Encode(samples); err != nil {
		return err
	}
	return w.drain(false)
}

// Close flushes the encoder, splits the trailing bytes into Ogg
// packets, stamps EOS, and closes the underlying ogg.Writer.
func (w *FLACWriter) Close() error {
	if w.closed {
		return ErrAlreadyClosed
	}
	w.closed = true
	if err := w.enc.Close(); err != nil {
		return err
	}
	if err := w.drain(true); err != nil {
		return err
	}
	w.stream.SetEOS()
	return w.ogg.Close()
}

// drain consumes whatever bytes the encoder has produced into
// w.buffer, peeling off (a) the magic + STREAMINFO BOS packet, (b)
// each subsequent metadata packet, and (c) each audio frame. final is
// true on Close — the trailing pending frame (if any) is then emitted
// even though no further sync code follows.
func (w *FLACWriter) drain(final bool) error {
	for {
		switch w.state {
		case stateMagic:
			if w.buffer.Len() < 4 {
				return nil
			}
			magic := w.buffer.Next(4)
			if string(magic) != "fLaC" {
				return ErrBadFLACHead
			}
			w.state = stateMetadata
			// We need STREAMINFO before we can emit BOS. STREAMINFO
			// is the very next 38 bytes (4-byte header + 34-byte body).
			if w.buffer.Len() < 38 {
				return nil
			}
			siBlock := w.buffer.Next(38)
			bos := buildOggFLACBOS(siBlock, 0)
			if err := w.stream.WritePacket(bos); err != nil {
				return err
			}
			if err := w.stream.forceFlush(); err != nil {
				return err
			}
			// Continue: next iteration reads more metadata blocks.
			isLast := siBlock[0]&0x80 != 0
			if isLast {
				w.state = stateFrames
			}

		case stateMetadata:
			if w.buffer.Len() < 4 {
				return nil
			}
			peek := w.buffer.Bytes()
			bodyLen := int(uint32(peek[1])<<16 | uint32(peek[2])<<8 | uint32(peek[3]))
			total := 4 + bodyLen
			if w.buffer.Len() < total {
				return nil
			}
			block := append([]byte(nil), w.buffer.Next(total)...)
			isLast := block[0]&0x80 != 0
			if err := w.stream.WritePacket(block); err != nil {
				return err
			}
			if err := w.stream.forceFlush(); err != nil {
				return err
			}
			if isLast {
				w.state = stateFrames
			}

		case stateFrames:
			emitted, err := w.emitFrames(final)
			if err != nil {
				return err
			}
			if !emitted {
				return nil
			}
		}
	}
}

// emitFrames pulls complete frames out of w.buffer and writes them as
// Ogg packets. Returns whether at least one frame was emitted in this
// call. On final, the trailing partial frame (if any) is also emitted.
func (w *FLACWriter) emitFrames(final bool) (bool, error) {
	emitted := false
	data := w.buffer.Bytes()
	// Frame must start with a sync code; if there's a pending frame,
	// we are already mid-frame.
	if len(w.pendingFrame) == 0 && len(data) > 0 {
		if !isFLACFrameStart(data) {
			return false, ErrBadFLACMetadata
		}
	}

	// Concatenate pending + new for scanning.
	all := append(w.pendingFrame, data...)
	w.buffer.Reset()
	w.pendingFrame = nil

	// The splitter walks `all` and emits every complete frame. The
	// remainder (a possibly-incomplete trailing frame) is stashed
	// back into pendingFrame.
	frames, remainder, err := splitFLACFrames(all, final)
	if err != nil {
		return false, err
	}
	for _, frame := range frames {
		samples, err := frameSampleCount(frame)
		if err != nil {
			return false, err
		}
		w.prevSamples += samples
		w.stream.SetGranule(int64(w.prevSamples))
		if err := w.stream.WritePacket(frame); err != nil {
			return false, err
		}
		emitted = true
	}
	if len(remainder) > 0 {
		w.pendingFrame = remainder
	}
	return emitted, nil
}

// buildOggFLACBOS constructs the BOS packet from an existing STREAMINFO
// metadata block (4-byte header + 34-byte body).
func buildOggFLACBOS(streamInfoBlock []byte, numOtherHeaders uint16) []byte {
	out := make([]byte, 0, 13+len(streamInfoBlock))
	out = append(out, 0x7F)
	out = append(out, 'F', 'L', 'A', 'C')
	out = append(out, 1, 0) // major.minor
	var n [2]byte
	binary.BigEndian.PutUint16(n[:], numOtherHeaders)
	out = append(out, n[:]...)
	out = append(out, 'f', 'L', 'a', 'C')
	out = append(out, streamInfoBlock...)
	return out
}

// isFLACFrameStart returns true if data begins with a FLAC frame sync
// code (0xFF then 0xF8 fixed-blocking or 0xF9 variable-blocking).
func isFLACFrameStart(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	return data[0] == 0xFF && (data[1] == 0xF8 || data[1] == 0xF9)
}

// splitFLACFrames divides a buffer of FLAC frame bytes into individual
// frames. Each frame is identified by:
//   - starting with a sync code (validated by the caller for the first
//     frame; subsequent frame starts are sync codes we found in-buffer)
//   - ending with a 16-bit big-endian CRC-16 over the preceding bytes
//
// The function scans forward for candidate sync codes and validates
// each via CRC-16; valid candidates terminate the prior frame. When
// final is true, the last frame extends to the end of the buffer (a
// final-frame CRC-16 still trails it but we don't need to verify it
// here — the decoder will).
func splitFLACFrames(data []byte, final bool) (frames [][]byte, remainder []byte, err error) {
	if len(data) == 0 {
		return nil, nil, nil
	}
	start := 0
	for {
		// Find next candidate sync code AT OR AFTER start+5 (sync at
		// position start is the current frame's; we want the NEXT one).
		next, ok := findNextSync(data, start+5)
		if !ok {
			// No more sync codes ahead. If this is the final flush,
			// the last frame extends to end-of-buffer.
			if final {
				frames = append(frames, append([]byte(nil), data[start:]...))
				return frames, nil, nil
			}
			// Otherwise, keep the trailing bytes for the next round.
			remainder = append([]byte(nil), data[start:]...)
			return frames, remainder, nil
		}
		// Validate via CRC-16: the 2 bytes preceding `next` must be
		// the big-endian CRC-16 over data[start:next-2].
		if next < 2 || next-2 < start+5 {
			start = next
			continue
		}
		want := binary.BigEndian.Uint16(data[next-2 : next])
		got := flacCRC16(data[start : next-2])
		if got != want {
			// False sync — keep scanning forward.
			start = next
			continue
		}
		frames = append(frames, append([]byte(nil), data[start:next]...))
		start = next
	}
}

// findNextSync returns the byte index of the next FLAC frame sync code
// at or after offset.
func findNextSync(data []byte, offset int) (int, bool) {
	for i := offset; i+1 < len(data); i++ {
		if data[i] == 0xFF && (data[i+1] == 0xF8 || data[i+1] == 0xF9) {
			return i, true
		}
	}
	return 0, false
}

// flacCRC16 computes a CRC-16 with polynomial x^16 + x^15 + x^2 + 1
// (= 0x8005), MSB-first, init 0 — the variant the FLAC frame footer
// uses (RFC 9639 §11.5).
func flacCRC16(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x8005
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// frameSampleCount extracts the inter-channel sample count from a FLAC
// frame's header. Used to keep the Ogg granule position accurate.
//
// FLAC frame header (RFC 9639 §11.1):
//
//	bits  0..13: sync code
//	bit   14:    reserved (0)
//	bit   15:    blocking strategy
//	bits 16..19: blocksize encoding
//	bits 20..23: sample rate encoding
//	bits 24..27: channel assignment
//	bits 28..30: sample size in bits encoding
//	bit   31:    reserved (0)
//	then variable-length sample-or-frame number in UTF-8-style varint
//	then optional 8/16-bit blocksize follow-on
//	then optional 8/16-bit samplerate follow-on
//	then 8-bit CRC-8 over header
//
// We only need the blocksize encoding plus the optional follow-on.
func frameSampleCount(frame []byte) (int, error) {
	if len(frame) < 5 {
		return 0, ErrBadFLACMetadata
	}
	bsBits := frame[2] >> 4 // bits 16..19
	switch bsBits {
	case 0:
		return 0, ErrBadFLACMetadata // reserved
	case 1:
		return 192, nil
	case 2, 3, 4, 5:
		return 576 << (bsBits - 2), nil // 576, 1152, 2304, 4608
	case 6, 7:
		// Read the 8/16-bit blocksize after the variable-length
		// frame number. Skip the frame number first.
		off, err := skipUTF8FrameNumber(frame, 4)
		if err != nil {
			return 0, err
		}
		switch bsBits {
		case 6:
			if off+1 > len(frame) {
				return 0, ErrBadFLACMetadata
			}
			return int(frame[off]) + 1, nil
		case 7:
			if off+2 > len(frame) {
				return 0, ErrBadFLACMetadata
			}
			return int(binary.BigEndian.Uint16(frame[off:off+2])) + 1, nil
		}
	default:
		return 256 << (bsBits - 8), nil // 256, 512, 1024, 2048, 4096, 8192, 16384, 32768
	}
	return 0, ErrBadFLACMetadata
}

// skipUTF8FrameNumber walks a variable-length sample-or-frame number
// at offset `off` within frame, returning the position immediately
// after it. The encoding is the UTF-8-like style used by FLAC: the
// number of leading-1 bits in the first byte indicates the total byte
// count (1..7), with 0xxxxxxx for single-byte values.
func skipUTF8FrameNumber(frame []byte, off int) (int, error) {
	if off >= len(frame) {
		return 0, ErrBadFLACMetadata
	}
	b := frame[off]
	switch {
	case b&0x80 == 0:
		return off + 1, nil
	case b&0xE0 == 0xC0:
		return off + 2, nil
	case b&0xF0 == 0xE0:
		return off + 3, nil
	case b&0xF8 == 0xF0:
		return off + 4, nil
	case b&0xFC == 0xF8:
		return off + 5, nil
	case b&0xFE == 0xFC:
		return off + 6, nil
	case b == 0xFE:
		return off + 7, nil
	}
	return 0, ErrBadFLACMetadata
}
