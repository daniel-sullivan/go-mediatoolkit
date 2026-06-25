package flac

import (
	"io"

	flaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/flac"
)

// Writer encodes a FLAC stream from a [Header] and interleaved int32
// samples. It wraps a [libraries/flac.Encoder] and projects the
// container header onto the encoder's options:
//   - Header.SampleRate, Header.Channels, Header.Extra.StreamInfo.BitsPerSample
//     drive the encoder's StreamInfo. (BitsPerSample falls back to 16
//     if the supplied StreamInfo leaves it zero.)
//   - Header.Extra.StreamInfo.TotalSamples seeds the total-samples
//     estimate when non-zero.
//   - Header.Tags is converted to VORBIS_COMMENT entries. Header
//     .Extra.Vendor sets the vendor string (or stays libFLAC's
//     default when empty).
//
// The encoder owns all bytes on the wire — the magic, STREAMINFO, the
// optional VORBIS_COMMENT, and every audio frame. Writer does not emit
// any container framing of its own.
type Writer struct {
	enc    flaclib.Encoder
	header Header
	closed bool
}

// WriterOption tunes a [Writer] beyond what the [Header] expresses.
type WriterOption func(*writerOptions)

type writerOptions struct {
	compression int
	verify      bool
	blockSize   int
}

// WithCompressionLevel sets the encoder compression level [0, 8].
// Default: 5.
func WithCompressionLevel(level int) WriterOption {
	return func(o *writerOptions) { o.compression = level }
}

// WithVerify enables encoder self-verification.
func WithVerify(enable bool) WriterOption {
	return func(o *writerOptions) { o.verify = enable }
}

// WithBlockSize sets a fixed encoder block size in samples per channel.
func WithBlockSize(samples int) WriterOption {
	return func(o *writerOptions) { o.blockSize = samples }
}

// NewWriter returns a Writer that encodes a FLAC stream to w according
// to h. Header.SampleRate and Header.Channels are required; bit depth
// is taken from Header.Extra.StreamInfo.BitsPerSample and defaults to
// 16 when zero.
func NewWriter(w io.Writer, h Header, opts ...WriterOption) (*Writer, error) {
	if w == nil {
		return nil, ErrBadArg
	}
	wo := writerOptions{compression: 5}
	for _, o := range opts {
		o(&wo)
	}

	bits := h.Extra.StreamInfo.BitsPerSample
	if bits == 0 {
		bits = 16
	}
	info := flaclib.StreamInfo{
		SampleRate:    h.SampleRate,
		Channels:      h.Channels,
		BitsPerSample: bits,
	}

	encOpts := []flaclib.EncoderOption{
		flaclib.WithCompressionLevel(wo.compression),
	}
	if wo.verify {
		encOpts = append(encOpts, flaclib.WithVerify(true))
	}
	if wo.blockSize > 0 {
		encOpts = append(encOpts, flaclib.WithBlockSize(wo.blockSize))
	}
	if total := h.Extra.StreamInfo.TotalSamples; total > 0 {
		encOpts = append(encOpts, flaclib.WithTotalSamples(total))
	}
	if v := h.Extra.Vendor; v != "" {
		encOpts = append(encOpts, flaclib.WithVendor(v))
	}
	if tagMap := h.Tags.Map(); len(tagMap) > 0 {
		encOpts = append(encOpts, flaclib.WithTags(tagMap))
	}

	enc, err := flaclib.NewEncoder(w, info, encOpts...)
	if err != nil {
		return nil, err
	}
	return &Writer{enc: enc, header: h}, nil
}

// Header returns the header this writer was constructed with.
func (w *Writer) Header() Header { return w.header }

// Encode submits a block of interleaved int32 samples. See
// [libraries/flac.Encoder.Encode] for buffer-shape requirements.
func (w *Writer) Encode(samples []int32) error {
	if w.closed {
		return ErrAlreadyClosed
	}
	return w.enc.Encode(samples)
}

// Encoder returns the underlying [libraries/flac.Encoder]. Useful when
// the caller wants to feed samples through helpers that expect that
// type directly.
func (w *Writer) Encoder() flaclib.Encoder { return w.enc }

// Close flushes the encoder and writes its final metadata. It does not
// close the underlying writer.
func (w *Writer) Close() error {
	if w.closed {
		return ErrAlreadyClosed
	}
	w.closed = true
	return w.enc.Close()
}
