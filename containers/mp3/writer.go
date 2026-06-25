package mp3

import (
	"io"

	mp3lib "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3"
)

// Writer encodes an MP3 stream from a [Header] and interleaved int16 samples.
// It writes an ID3v2.3 tag projected from Header.Tags ahead of the audio
// frames, then wraps a [libraries/mp3.Encoder] for the frames themselves:
//   - Header.SampleRate and Header.Channels drive the encoder's StreamInfo.
//   - Header.BitRate, when non-zero, sets the target constant bit rate.
//   - Header.Tags is converted to ID3v2 text frames written before the first
//     audio frame.
//
// Unlike FLAC (where the encoder owns every byte), the MP3 encoder does not
// emit metadata, so the container layer writes the ID3v2 tag bytes itself and
// then defers audio framing to the encoder.
//
// The encoder is constructed lazily, on the first audio-frame [Writer.Encode],
// so metadata-only use (NewWriter + Close, writing just the ID3v2 tag) works in
// a default build with no encoder involvement. The encoder-unavailable sentinel
// [libraries/mp3.ErrEncoderRequiresLAME] therefore surfaces on the first Encode,
// not at NewWriter.
type Writer struct {
	dst     io.Writer
	header  Header
	encOpts []mp3lib.EncoderOption
	enc     mp3lib.Encoder // nil until the first audio frame is written
	closed  bool
}

// WriterOption tunes a [Writer] beyond what the [Header] expresses.
type WriterOption func(*writerOptions)

type writerOptions struct {
	bitRate int
	quality int
	vbr     bool
}

// WithBitRate sets the target constant bit rate, in bits per second.
// Default: 128000 (or Header.BitRate when non-zero). Ignored under VBR.
func WithBitRate(bps int) WriterOption {
	return func(o *writerOptions) { o.bitRate = bps }
}

// WithQuality sets the LAME encoding quality, in the range [0, 9] where 0 is
// highest quality / slowest. Default: 3.
func WithQuality(q int) WriterOption {
	return func(o *writerOptions) { o.quality = q }
}

// WithVBR enables variable-bit-rate encoding.
func WithVBR(enable bool) WriterOption {
	return func(o *writerOptions) { o.vbr = enable }
}

// NewWriter returns a Writer that encodes an MP3 stream to w according to h.
// Header.SampleRate and Header.Channels are required for audio, but are not
// consulted until the first [Writer.Encode]. The ID3v2 tag derived from
// Header.Tags is written to w immediately; audio frames follow as the caller
// calls [Writer.Encode].
//
// The encoder itself is not constructed here: a metadata-only Writer (one that
// is closed without any Encode) works in a default build, and the encoder
// backend's availability (ErrEncoderRequiresLAME without -tags mp3lame) is only
// reported on the first Encode.
func NewWriter(w io.Writer, h Header, opts ...WriterOption) (*Writer, error) {
	if w == nil {
		return nil, ErrBadArg
	}
	wo := writerOptions{bitRate: h.BitRate, quality: 3}
	for _, o := range opts {
		o(&wo)
	}

	// Write the ID3v2 tag ahead of the audio. An empty tag map yields a
	// header-only (zero-frame) tag, which decoders skip harmlessly; to avoid
	// emitting an empty tag at all, only write when there is something to say.
	if tagMap := h.Tags.Map(); len(tagMap) > 0 {
		if _, err := w.Write(encodeID3v2(tagMap)); err != nil {
			return nil, err
		}
	}

	encOpts := []mp3lib.EncoderOption{
		mp3lib.WithQuality(wo.quality),
	}
	if wo.vbr {
		encOpts = append(encOpts, mp3lib.WithVBR(true))
	} else if wo.bitRate > 0 {
		encOpts = append(encOpts, mp3lib.WithBitRate(wo.bitRate))
	}

	return &Writer{dst: w, header: h, encOpts: encOpts}, nil
}

// Header returns the header this writer was constructed with.
func (w *Writer) Header() Header { return w.header }

// Encode submits a block of interleaved int16 samples to the encoder. The
// encoder is constructed on the first call (which is where an unavailable
// backend surfaces ErrEncoderRequiresLAME in a default build). See
// [libraries/mp3.Encoder.EncodeFrame] for buffer-shape requirements.
func (w *Writer) Encode(samples []int16) error {
	if w.closed {
		return ErrAlreadyClosed
	}
	if err := w.ensureEncoder(); err != nil {
		return err
	}
	return w.enc.EncodeFrame(samples)
}

// ensureEncoder lazily constructs the underlying [libraries/mp3.Encoder] from
// the header and options captured at NewWriter. It is a no-op once the encoder
// exists.
func (w *Writer) ensureEncoder() error {
	if w.enc != nil {
		return nil
	}
	info := mp3lib.StreamInfo{
		SampleRate: w.header.SampleRate,
		Channels:   w.header.Channels,
		BitRate:    w.header.BitRate,
	}
	enc, err := mp3lib.NewEncoder(w.dst, info, w.encOpts...)
	if err != nil {
		return err
	}
	w.enc = enc
	return nil
}

// Encoder returns the underlying [libraries/mp3.Encoder], constructing it lazily
// on first use. A non-nil error means the encoder backend is unavailable (e.g.
// ErrEncoderRequiresLAME in a default build).
func (w *Writer) Encoder() (mp3lib.Encoder, error) {
	if w.closed {
		return nil, ErrAlreadyClosed
	}
	if err := w.ensureEncoder(); err != nil {
		return nil, err
	}
	return w.enc, nil
}

// Close flushes the encoder if one was constructed, then marks the Writer
// closed. A metadata-only Writer (no Encode call) closes without touching the
// encoder, so Close succeeds in a default build. It does not close the
// underlying writer.
func (w *Writer) Close() error {
	if w.closed {
		return ErrAlreadyClosed
	}
	w.closed = true
	if w.enc == nil {
		return nil
	}
	return w.enc.Close()
}
