package mp4

import (
	"encoding/binary"
	"io"
	"math"

	"go-mediatoolkit/codec"
	aaccodec "go-mediatoolkit/codec/aac"
	aaclib "go-mediatoolkit/libraries/aac"
	"go-mediatoolkit/mutations"
)

// Writer muxes AAC access units into an ISOBMFF/MP4 file. It wraps a
// [go-mediatoolkit/codec/aac] streaming encoder: callers Write interleaved
// float64 PCM (any sample count), the encoder produces AAC access units, and
// Close assembles the ftyp / moov (with esds + stsz/stsc/stco + stts) / mdat
// box tree, projecting [Header].Tags onto an iTunes ilst box.
//
// Because the moov box carries the sample-size and chunk-offset tables that
// reference the mdat payload, the file cannot be emitted until every access
// unit is known. Writer therefore buffers the encoded access units and writes
// the whole file in Close. A Writer is not safe for concurrent use.
//
// The mvhd/tkhd/mdhd boxes are emitted at version 0 (32-bit fields) when the
// duration in timescale ticks fits a uint32 and at version 1 (64-bit fields)
// otherwise; the chunk-offset table is emitted as stco (32-bit) until the
// offset would overflow a uint32, at which point co64 (64-bit) is used. So
// streams past ~uint32 ticks or larger than 4 GiB are represented per
// ISO/IEC 14496-12 rather than rejected.
type Writer struct {
	w         io.Writer
	header    Header
	encoder   codec.Encoder
	encOpts   []aaccodec.EncoderOption // captured for the lazy encoder
	asc       aaclib.AudioSpecificConfig
	timescale int
	packets   [][]byte
	closed    bool

	// forceCo64 forces the 64-bit co64 chunk-offset box even when the real
	// offset fits a uint32. This lets tests exercise the co64 write/read path
	// (co64 emitted, offset pointing at the actual mdat, full round-trip)
	// without buffering a multi-gigabyte mdat payload. Production muxing never
	// sets it; the box form is chosen automatically from the real offset in
	// buildFile (co64 once the offset would overflow uint32).
	forceCo64 bool
}

// WriterOption tunes a [Writer] beyond what the [Header] expresses.
type WriterOption func(*writerOptions)

type writerOptions struct {
	bitrate    int
	objectType aaclib.AudioObjectType
}

// WithBitrate sets the AAC target bitrate in bits per second (default
// 128000).
func WithBitrate(bps int) WriterOption {
	return func(o *writerOptions) { o.bitrate = bps }
}

// WithObjectType selects the AAC profile to encode (default AAC-LC).
func WithObjectType(t aaclib.AudioObjectType) WriterOption {
	return func(o *writerOptions) { o.objectType = t }
}

// NewWriter returns a Writer that encodes AAC and muxes it into an MP4 file
// written to w on [Writer.Close]. Header.SampleRate and Header.Channels are
// required.
func NewWriter(w io.Writer, h Header, opts ...WriterOption) (*Writer, error) {
	if w == nil {
		return nil, ErrBadArg
	}
	if h.SampleRate < 1 || h.Channels < 1 {
		return nil, ErrBadArg
	}
	wo := writerOptions{bitrate: 128000, objectType: aaclib.AOTAACLC}
	for _, o := range opts {
		o(&wo)
	}

	mw := &Writer{w: w, header: h, timescale: h.SampleRate}

	mw.encOpts = append(mw.encOpts, aaccodec.WithObjectType(wo.objectType))
	if wo.bitrate != 0 {
		mw.encOpts = append(mw.encOpts, aaccodec.WithBitrate(wo.bitrate))
	}

	// The AudioSpecificConfig for the esds box is synthesised from the
	// header fields, so a pure re-mux (WritePacket only) needs no AAC
	// engine and the container layer stays MIT/untagged. The encoder is
	// constructed lazily on the first WriteAudio call (see WriteAudio),
	// which is the only path that requires the FDK-AAC engine; at that
	// point an unavailable engine surfaces aac.ErrEngineRequiresFDK.
	mw.asc = aaclib.AudioSpecificConfig{
		ObjectType:   wo.objectType,
		SampleRate:   h.SampleRate,
		Channels:     h.Channels,
		FrameSamples: aaclib.FrameSamplesShort,
	}
	if h.Extra.Config.SampleRate != 0 {
		// Prefer an explicitly-supplied config (e.g. a re-mux copying the
		// original ASC bytes byte-for-byte).
		mw.asc = h.Extra.Config
	}
	return mw, nil
}

// ensureEncoder lazily constructs the AAC streaming encoder on the first
// WriteAudio. Encoding requires the FDK-AAC engine (the aacfdk build tag);
// without it aaccodec.NewEncoder returns aac.ErrEngineRequiresFDK, which is
// surfaced here. Pure re-muxes that only call WritePacket never reach this
// path and need no engine.
func (w *Writer) ensureEncoder() error {
	if w.encoder != nil {
		return nil
	}
	enc, err := aaccodec.NewEncoder(
		aaccodec.PacketWriterFunc(func(pkt []byte) error {
			cp := make([]byte, len(pkt))
			copy(cp, pkt)
			w.packets = append(w.packets, cp)
			return nil
		}),
		w.header.SampleRate, w.header.Channels, w.encOpts...,
	)
	if err != nil {
		return err
	}
	w.encoder = enc

	// If the engine reports a precise AudioSpecificConfig (the exact
	// decoder-config bytes), prefer it over the synthesised one — unless an
	// explicit re-mux config was supplied to NewWriter.
	if w.header.Extra.Config.SampleRate == 0 {
		if cfg, ok := encoderConfig(enc); ok {
			w.asc = cfg
		}
	}
	return nil
}

// Header returns the header this writer was constructed with.
func (w *Writer) Header() Header { return w.header }

// WriteAudio submits a block of interleaved float64 PCM. The block may hold
// any number of samples; the encoder buffers across calls and emits whole
// AAC access units.
func (w *Writer) WriteAudio(samples []float64) error {
	if w.closed {
		return ErrAlreadyClosed
	}
	if err := w.ensureEncoder(); err != nil {
		return err
	}
	_, err := w.encoder.Write(mutations.Audio{
		Data:       samples,
		SampleRate: w.header.SampleRate,
		Channels:   w.header.Channels,
	})
	return err
}

// WritePacket appends a pre-encoded AAC access unit directly, bypassing the
// internal encoder. Used by re-muxers copying access units byte-for-byte.
func (w *Writer) WritePacket(pkt []byte) error {
	if w.closed {
		return ErrAlreadyClosed
	}
	cp := make([]byte, len(pkt))
	copy(cp, pkt)
	w.packets = append(w.packets, cp)
	return nil
}

// Close flushes the encoder, assembles the box tree, and writes the complete
// MP4 file to the underlying writer. It does not close that writer.
func (w *Writer) Close() error {
	if w.closed {
		return ErrAlreadyClosed
	}
	w.closed = true

	// Flush the encoder so any partial final frame is emitted. A re-mux that
	// only used WritePacket never built an encoder, so there is nothing to
	// flush.
	if w.encoder != nil {
		if err := w.encoder.Close(); err != nil {
			return err
		}
	}

	file, err := w.buildFile()
	if err != nil {
		return err
	}
	_, err = w.w.Write(file)
	return err
}

// buildFile assembles the complete ISOBMFF byte stream from the buffered
// access units and the header.
//
// The movie/track/media headers are emitted as version-0 (32-bit) boxes when
// the duration in timescale ticks fits a uint32, and as version-1 (64-bit)
// boxes otherwise (ISO/IEC 14496-12 §8.2.2 / §8.3.2 / §8.4.2). The chunk-offset
// table is emitted as a 32-bit stco when the single chunk offset fits a uint32
// and as a 64-bit co64 (§8.7.5) otherwise, so streams longer than ~uint32 ticks
// or larger than 4 GiB are represented rather than rejected.
func (w *Writer) buildFile() ([]byte, error) {
	frameSamples := w.asc.FrameSamples
	if frameSamples == 0 {
		frameSamples = aaclib.FrameSamplesShort
	}

	// The media duration in timescale ticks (len(packets)*frameSamples,
	// recomputed in buildMoov) drives whether the mvhd/tkhd/mdhd boxes are
	// emitted at version 0 (32-bit) or version 1 (64-bit). A duration past
	// math.MaxUint64 is genuinely unrepresentable, but that product cannot
	// reach it with any realistic input, so no guard is needed.
	ftyp := buildFtyp(w.header)

	// mdat = concatenated access units. Its payload begins 8 bytes after
	// the mdat box header start; the chunk offset must point at the absolute
	// file position of the first access unit, so it is computed once the
	// preceding box sizes (ftyp + moov) are known.
	var mdatPayload []byte
	for _, pkt := range w.packets {
		mdatPayload = append(mdatPayload, pkt...)
	}

	mdatHeaderLen := uint64(8)

	// Decide the chunk-offset box form before measuring moov, so the moov size
	// is stable across the measure-then-patch passes. Switching stco→co64 only
	// grows the offset (the co64 box is 4 bytes larger), so the decision is
	// monotonic: if the offset computed with the smaller stco already overflows
	// uint32, co64 is required, and the extra 4 bytes cannot bring it back
	// under the threshold. w.forceCo64 lets tests force the 64-bit form without
	// a multi-gigabyte payload.
	stcoMoov := w.buildMoov(frameSamples, 0, false)
	stcoOffset := uint64(len(ftyp)) + uint64(len(stcoMoov)) + mdatHeaderLen
	useCo64 := w.forceCo64 || stcoOffset > math.MaxUint32

	// Build moov with a placeholder chunk offset (fixed box form), measure it,
	// then patch the real offset. moov precedes mdat in the output.
	moov := w.buildMoov(frameSamples, 0, useCo64)
	chunkOffset := uint64(len(ftyp)) + uint64(len(moov)) + mdatHeaderLen
	moov = w.buildMoov(frameSamples, chunkOffset, useCo64)

	mdat := buildBox("mdat", mdatPayload)

	out := make([]byte, 0, len(ftyp)+len(moov)+len(mdat))
	out = append(out, ftyp...)
	out = append(out, moov...)
	out = append(out, mdat...)
	return out, nil
}

// buildMoov builds the moov box: an mvhd, a single audio trak (with the esds
// sample entry and the stsz/stsc/stco-or-co64/stts sample tables), and a
// udta/meta/ilst metadata block. chunkOffset is the absolute file offset of the
// first access unit (all samples live in one chunk); useCo64 selects the 64-bit
// chunk-offset box.
func (w *Writer) buildMoov(frameSamples int, chunkOffset uint64, useCo64 bool) []byte {
	numSamples := uint32(len(w.packets))
	durationTicks := uint64(numSamples) * uint64(frameSamples)

	mvhd := buildMvhd(uint32(w.timescale), durationTicks)
	trak := w.buildTrak(frameSamples, chunkOffset, numSamples, durationTicks, useCo64)
	udta := buildUdta(w.header)

	body := append([]byte{}, mvhd...)
	body = append(body, trak...)
	if udta != nil {
		body = append(body, udta...)
	}
	return buildBox("moov", body)
}

// buildTrak builds a trak → mdia → (mdhd, hdlr, minf → (stbl → (stsd → mp4a
// → esds, stts, stsc, stsz, stco/co64))) audio-track hierarchy. useCo64 selects
// the 64-bit chunk-offset box.
func (w *Writer) buildTrak(frameSamples int, chunkOffset uint64, numSamples uint32, durationTicks uint64, useCo64 bool) []byte {
	tkhd := buildTkhd(durationTicks)
	mdhd := buildMdhd(uint32(w.timescale), durationTicks)
	hdlr := buildHdlr()

	stsd := buildStsd(w.asc, uint32(w.header.Channels), uint32(w.header.SampleRate))
	stts := buildStts(numSamples, uint32(frameSamples))
	stsc := buildStsc(numSamples)
	stsz := buildStsz(w.packets)
	stco := buildChunkOffset(chunkOffset, useCo64)

	stbl := buildBox("stbl", concat(stsd, stts, stsc, stsz, stco))
	smhd := buildSmhd()
	dinf := buildDinf()
	minf := buildBox("minf", concat(smhd, dinf, stbl))
	mdia := buildBox("mdia", concat(mdhd, hdlr, minf))
	return buildBox("trak", concat(tkhd, mdia))
}

// encoderConfig extracts the AudioSpecificConfig from a codec encoder when it
// exposes the underlying library encoder. The codec/aac encoder is an
// unexported type; it is queried via the optional interface below.
func encoderConfig(enc interface{}) (aaclib.AudioSpecificConfig, bool) {
	type configer interface {
		Config() aaclib.AudioSpecificConfig
	}
	if c, ok := enc.(configer); ok {
		return c.Config(), true
	}
	return aaclib.AudioSpecificConfig{}, false
}

// ── low-level box builders ───────────────────────────────────────────────

// buildBox wraps body in a box of the given four-character type, prefixing
// the 32-bit big-endian size and the type.
func buildBox(boxType string, body []byte) []byte {
	out := make([]byte, 8+len(body))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(body)))
	copy(out[4:8], boxType)
	copy(out[8:], body)
	return out
}

// concat joins box byte slices.
func concat(parts ...[]byte) []byte {
	var n int
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func buildFtyp(h Header) []byte {
	major := h.Extra.MajorBrand
	if major == "" {
		major = "M4A "
	}
	body := make([]byte, 0, 16)
	body = append(body, []byte(brand4(major))...)
	body = append(body, 0, 0, 0, 0) // minor version
	compat := h.Extra.CompatibleBrands
	if len(compat) == 0 {
		compat = []string{"M4A ", "mp42", "isom"}
	}
	for _, b := range compat {
		body = append(body, []byte(brand4(b))...)
	}
	return buildBox("ftyp", body)
}

// brand4 normalises a brand string to exactly four bytes (space-padded /
// truncated).
func brand4(s string) string {
	b := []byte(s)
	switch {
	case len(b) == 4:
		return s
	case len(b) > 4:
		return string(b[:4])
	default:
		for len(b) < 4 {
			b = append(b, ' ')
		}
		return string(b)
	}
}

// buildMvhd builds the movie-header box (ISO/IEC 14496-12 §8.2.2). When the
// duration in timescale ticks fits a uint32 it emits the version-0 (32-bit)
// layout for maximum compatibility; otherwise it emits the version-1 (64-bit)
// layout, widening creation_time, modification_time and duration to 64 bits.
func buildMvhd(timescale uint32, duration uint64) []byte {
	if duration > math.MaxUint32 {
		// version 1: creation_time(8) modification_time(8) timescale(4)
		// duration(8), then rate/volume/reserved/matrix/pre_defined/
		// next_track_ID — identical to v0 from the timescale field onward
		// except the time/duration widths.
		body := make([]byte, 112)
		body[0] = 1 // version 1
		binary.BigEndian.PutUint32(body[20:24], timescale)
		binary.BigEndian.PutUint64(body[24:32], duration)
		binary.BigEndian.PutUint32(body[32:36], 0x00010000) // rate 1.0
		binary.BigEndian.PutUint16(body[36:38], 0x0100)     // volume 1.0
		matrix := []uint32{0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000}
		for i, m := range matrix {
			binary.BigEndian.PutUint32(body[48+i*4:], m)
		}
		binary.BigEndian.PutUint32(body[108:112], 2) // next track ID
		return buildBox("mvhd", body)
	}
	body := make([]byte, 100)
	// version 0, flags 0 already zero.
	binary.BigEndian.PutUint32(body[12:16], timescale)
	binary.BigEndian.PutUint32(body[16:20], uint32(duration))
	binary.BigEndian.PutUint32(body[20:24], 0x00010000) // rate 1.0
	binary.BigEndian.PutUint16(body[24:26], 0x0100)     // volume 1.0
	// unity matrix
	matrix := []uint32{0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000}
	for i, m := range matrix {
		binary.BigEndian.PutUint32(body[36+i*4:], m)
	}
	binary.BigEndian.PutUint32(body[96:100], 2) // next track ID
	return buildBox("mvhd", body)
}

// buildTkhd builds the track-header box (ISO/IEC 14496-12 §8.3.2). It emits the
// version-1 (64-bit creation_time/modification_time/duration) layout when the
// duration overflows a uint32, otherwise the version-0 layout.
func buildTkhd(duration uint64) []byte {
	if duration > math.MaxUint32 {
		// version 1: creation_time(8) modification_time(8) track_ID(4)
		// reserved(4) duration(8), then layer/alt-group/volume/matrix/
		// width/height.
		body := make([]byte, 96)
		body[0] = 1                                // version 1
		body[3] = 0x07                             // flags: enabled | in movie | in preview
		binary.BigEndian.PutUint32(body[20:24], 1) // track ID
		binary.BigEndian.PutUint64(body[28:36], duration)
		binary.BigEndian.PutUint16(body[48:50], 0x0100) // volume 1.0 (audio)
		matrix := []uint32{0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000}
		for i, m := range matrix {
			binary.BigEndian.PutUint32(body[52+i*4:], m)
		}
		return buildBox("tkhd", body)
	}
	body := make([]byte, 84)
	body[3] = 0x07                             // flags: track enabled | in movie | in preview
	binary.BigEndian.PutUint32(body[12:16], 1) // track ID
	binary.BigEndian.PutUint32(body[20:24], uint32(duration))
	binary.BigEndian.PutUint16(body[36:38], 0x0100) // volume 1.0 (audio)
	matrix := []uint32{0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000}
	for i, m := range matrix {
		binary.BigEndian.PutUint32(body[40+i*4:], m)
	}
	return buildBox("tkhd", body)
}

// buildMdhd builds the media-header box (ISO/IEC 14496-12 §8.4.2). It emits the
// version-1 (64-bit creation_time/modification_time/duration) layout when the
// duration overflows a uint32, otherwise the version-0 layout.
func buildMdhd(timescale uint32, duration uint64) []byte {
	if duration > math.MaxUint32 {
		// version 1: creation_time(8) modification_time(8) timescale(4)
		// duration(8) language(2) pre_defined(2).
		body := make([]byte, 36)
		body[0] = 1 // version 1
		binary.BigEndian.PutUint32(body[20:24], timescale)
		binary.BigEndian.PutUint64(body[24:32], duration)
		binary.BigEndian.PutUint16(body[32:34], 0x55c4) // language "und"
		return buildBox("mdhd", body)
	}
	body := make([]byte, 24)
	binary.BigEndian.PutUint32(body[12:16], timescale)
	binary.BigEndian.PutUint32(body[16:20], uint32(duration))
	binary.BigEndian.PutUint16(body[20:22], 0x55c4) // language "und"
	return buildBox("mdhd", body)
}

func buildHdlr() []byte {
	name := "SoundHandler\x00"
	body := make([]byte, 24+len(name))
	copy(body[8:12], "soun")
	copy(body[24:], name)
	return buildBox("hdlr", body)
}

func buildSmhd() []byte {
	return buildBox("smhd", make([]byte, 8))
}

func buildDinf() []byte {
	// dref with one self-contained url entry (flags 0x000001).
	url := buildBox("url ", []byte{0, 0, 0, 1})
	drefBody := make([]byte, 8)
	binary.BigEndian.PutUint32(drefBody[4:8], 1) // entry count
	drefBody = append(drefBody, url...)
	dref := buildBox("dref", drefBody)
	return buildBox("dinf", dref)
}

// buildStsd builds the sample-description box with one mp4a entry carrying the
// esds (AudioSpecificConfig).
func buildStsd(asc aaclib.AudioSpecificConfig, channels, sampleRate uint32) []byte {
	esds := buildEsds(asc)

	mp4aBody := make([]byte, 28)
	// reserved(6) | dataRefIndex(2)
	binary.BigEndian.PutUint16(mp4aBody[6:8], 1)
	binary.BigEndian.PutUint16(mp4aBody[16:18], uint16(channels))
	binary.BigEndian.PutUint16(mp4aBody[18:20], 16) // sample size bits
	// sampleRate is 16.16 fixed point; the integer part goes in the high 16
	// bits.
	binary.BigEndian.PutUint32(mp4aBody[24:28], sampleRate<<16)
	mp4aBody = append(mp4aBody, esds...)
	mp4a := buildBox("mp4a", mp4aBody)

	body := make([]byte, 8)
	binary.BigEndian.PutUint32(body[4:8], 1) // entry count
	body = append(body, mp4a...)
	return buildBox("stsd", body)
}

// buildEsds builds the esds box around the AudioSpecificConfig, nesting the
// ES_Descriptor → DecoderConfigDescriptor → DecoderSpecificInfo descriptors.
func buildEsds(asc aaclib.AudioSpecificConfig) []byte {
	ascBytes := encodeAudioSpecificConfig(asc)

	// DecoderSpecificInfo (tag 0x05): the raw ASC.
	dsi := buildDescriptor(tagDecSpec, ascBytes)

	// DecoderConfigDescriptor (tag 0x04):
	//   objectTypeIndication(1=0x40 audio) | streamType<<2|0x01 (audio,
	//   upstream 0) | bufferSizeDB(3) | maxBitrate(4) | avgBitrate(4) | dsi
	dcfg := make([]byte, 13)
	dcfg[0] = 0x40               // Audio ISO/IEC 14496-3
	dcfg[1] = (0x05 << 2) | 0x01 // streamType=AudioStream(5), upStream=0, reserved=1
	// buffer/bitrate left zero (informational).
	dcfg = append(dcfg, dsi...)
	dcd := buildDescriptor(tagDecConfig, dcfg)

	// ES_Descriptor (tag 0x03): ES_ID(2) | flags(1) | dcd | (SLConfig).
	es := make([]byte, 3)
	// ES_ID 0, no dependency/url/ocr flags.
	es = append(es, dcd...)
	// SLConfigDescriptor (tag 0x06) predefined=2 (MP4).
	sl := buildDescriptor(0x06, []byte{0x02})
	es = append(es, sl...)
	esd := buildDescriptor(tagES, es)

	body := make([]byte, 4) // FullBox version+flags
	body = append(body, esd...)
	return buildBox("esds", body)
}

// buildDescriptor wraps payload in an MPEG-4 descriptor: a one-byte tag, a
// variable-length size (one byte suffices for the sizes used here), and the
// payload.
func buildDescriptor(tag byte, payload []byte) []byte {
	// Encode the size in the minimum number of 7-bit groups.
	size := len(payload)
	var sizeBytes []byte
	if size < 0x80 {
		sizeBytes = []byte{byte(size)}
	} else {
		var tmp []byte
		for {
			tmp = append([]byte{byte(size & 0x7f)}, tmp...)
			size >>= 7
			if size == 0 {
				break
			}
		}
		for i := 0; i < len(tmp)-1; i++ {
			tmp[i] |= 0x80
		}
		sizeBytes = tmp
	}
	out := make([]byte, 0, 1+len(sizeBytes)+len(payload))
	out = append(out, tag)
	out = append(out, sizeBytes...)
	out = append(out, payload...)
	return out
}

func buildStts(numSamples, delta uint32) []byte {
	body := make([]byte, 16)
	binary.BigEndian.PutUint32(body[4:8], 1) // one run
	binary.BigEndian.PutUint32(body[8:12], numSamples)
	binary.BigEndian.PutUint32(body[12:16], delta)
	return buildBox("stts", body)
}

func buildStsc(numSamples uint32) []byte {
	// Single chunk holding every sample.
	body := make([]byte, 20)
	binary.BigEndian.PutUint32(body[4:8], 1)  // one entry
	binary.BigEndian.PutUint32(body[8:12], 1) // first chunk
	binary.BigEndian.PutUint32(body[12:16], numSamples)
	binary.BigEndian.PutUint32(body[16:20], 1) // sample description index
	return buildBox("stsc", body)
}

func buildStsz(packets [][]byte) []byte {
	body := make([]byte, 12+len(packets)*4)
	// sampleSize 0 → per-sample table follows.
	binary.BigEndian.PutUint32(body[8:12], uint32(len(packets)))
	for i, p := range packets {
		binary.BigEndian.PutUint32(body[12+i*4:], uint32(len(p)))
	}
	return buildBox("stsz", body)
}

// buildChunkOffset builds the chunk-offset table for the single chunk holding
// every sample. It emits the 64-bit co64 box (ISO/IEC 14496-12 §8.7.5) when
// useCo64 is set or the offset overflows a uint32, otherwise the 32-bit stco
// box (§8.7.5) for maximum compatibility.
func buildChunkOffset(chunkOffset uint64, useCo64 bool) []byte {
	if useCo64 || chunkOffset > math.MaxUint32 {
		body := make([]byte, 16)
		binary.BigEndian.PutUint32(body[4:8], 1) // one chunk
		binary.BigEndian.PutUint64(body[8:16], chunkOffset)
		return buildBox("co64", body)
	}
	body := make([]byte, 12)
	binary.BigEndian.PutUint32(body[4:8], 1) // one chunk
	binary.BigEndian.PutUint32(body[8:12], uint32(chunkOffset))
	return buildBox("stco", body)
}

// buildUdta builds the udta → meta → (hdlr, ilst) metadata block, or nil when
// there is nothing to write.
func buildUdta(h Header) []byte {
	ilstBody := buildIlst(h.Tags, h.Extra.FreeformTags, h.Extra.CoverArt)
	if len(ilstBody) == 0 {
		return nil
	}
	ilst := buildBox("ilst", ilstBody)

	// meta handler: "mdir"/"appl" identifies an iTunes metadata list.
	hdlrBody := make([]byte, 24)
	copy(hdlrBody[8:12], "mdir")
	copy(hdlrBody[12:16], "appl")
	hdlr := buildBox("hdlr", hdlrBody)

	metaBody := make([]byte, 4) // FullBox version+flags
	metaBody = append(metaBody, hdlr...)
	metaBody = append(metaBody, ilst...)
	meta := buildBox("meta", metaBody)

	return buildBox("udta", meta)
}
