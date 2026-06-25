package flac

import (
	"io"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// newNativeDecoder builds a Decoder backed by the pure-Go nativeflac
// decoder state machine (internal/nativeflac/decoder_stream.go). It
// exposes the same Decoder interface the cgo path does, driving the
// native ProcessSingle / ProcessUntilEndOfMetadata loops and collecting
// decoded frames through the native write callback.
//
// newNativeEncoder builds an Encoder backed by the pure-Go nativeflac
// encoder, delegating to newNativeStreamEncoder (native_encoder.go),
// which drives the native encoder state machine.

func newNativeDecoder(r io.Reader, cfg decoderConfig) (Decoder, error) {
	d := &nativeDecoder{
		dec: nativeflac.NewDecoder(),
		r:   r,
		cfg: cfg,
	}
	// InitStream wires r into the BitReader read callback and stores the
	// write / error callbacks. The native path mirrors libFLAC: an
	// all-zero STREAMINFO md5sum disables md5 checking, so WithMD5Check
	// only takes effect when the stream carries a real signature.
	st := d.dec.InitStream(r, d.write, d.onError, cfg.md5Check)
	if st != nativeflac.DecoderSearchForMetadata {
		return nil, ErrInternal
	}
	return d, nil
}

func newNativeEncoder(w io.Writer, info StreamInfo, cfg encoderConfig) (Encoder, error) {
	return newNativeStreamEncoder(w, info, cfg)
}

// nativeDecoder adapts the nativeflac.Decoder to the public Decoder
// interface. It mirrors cgoDecoder field-for-field so the two backends
// behave identically: a pending interleaved buffer drained across
// Decode calls, lazy metadata parsing, and a deferred end-of-stream MD5
// check.
type nativeDecoder struct {
	dec *nativeflac.Decoder
	r   io.Reader
	cfg decoderConfig

	info     StreamInfo
	haveInfo bool

	// pending is the interleaved sample buffer for the most recently
	// decoded frame; pos is the read offset into pending. When pos ==
	// len(pending) the next Decode call drives another frame.
	pending []int32
	pos     int

	// decodeErr is set by onError when the native decoder reports a
	// decode-side error against the bitstream.
	decodeErr error

	eof    bool
	closed bool
}

// write is the native write callback (nativeflac.DecoderWriteCallback).
// It interleaves the per-channel decoded samples into pending, matching
// the toolkit-wide [L0, R0, L1, R1, …] convention the cgo write
// callback uses.
func (d *nativeDecoder) write(header *nativeflac.FrameHeader, buffer [][]int32) nativeflac.DecoderWriteStatus {
	blockSize := int(header.Blocksize)
	channels := int(header.Channels)
	total := blockSize * channels
	if cap(d.pending) < total {
		d.pending = make([]int32, total)
	} else {
		d.pending = d.pending[:total]
	}
	d.pos = 0
	for ch := 0; ch < channels; ch++ {
		src := buffer[ch]
		for i := 0; i < blockSize; i++ {
			d.pending[i*channels+ch] = src[i]
		}
	}
	return nativeflac.DecoderWriteContinue
}

// onError is the native error callback (nativeflac.DecoderErrorCallback).
// Any decode-side error against the bitstream surfaces out of Decode as
// ErrInvalidStream, matching the cgo error callback.
func (d *nativeDecoder) onError(_ nativeflac.DecoderErrorStatus) {
	d.decodeErr = ErrInvalidStream
}

func (d *nativeDecoder) ensureMetadata() error {
	if d.haveInfo {
		return nil
	}
	if !d.dec.ProcessUntilEndOfMetadata() {
		return d.activeError(ErrInvalidStream)
	}
	si, ok := d.dec.StreamInfo()
	if !ok {
		return ErrInvalidStream
	}
	d.info = StreamInfo{
		SampleRate:    int(si.SampleRate),
		Channels:      int(si.Channels),
		BitsPerSample: int(si.BitsPerSample),
		MinBlockSize:  int(si.MinBlockSize),
		MaxBlockSize:  int(si.MaxBlockSize),
		MinFrameSize:  int(si.MinFrameSize),
		MaxFrameSize:  int(si.MaxFrameSize),
		TotalSamples:  si.TotalSamples,
	}
	d.info.MD5Signature = si.MD5Sum
	d.haveInfo = true
	return nil
}

func (d *nativeDecoder) Decode(buf []int32) (int, error) {
	if d.closed {
		return 0, ErrClosed
	}
	if err := d.ensureMetadata(); err != nil {
		return 0, err
	}
	if len(buf) == 0 {
		return 0, nil
	}
	if len(buf)%d.info.Channels != 0 {
		return 0, ErrBadArg
	}

	written := 0
	for written < len(buf) {
		if d.pos < len(d.pending) {
			avail := len(d.pending) - d.pos
			n := len(buf) - written
			if n > avail {
				n = avail
			}
			// pending is whole blocks, so n is naturally a channel
			// multiple unless buf has fewer samples than channels —
			// guarded by the modulus check above.
			copy(buf[written:written+n], d.pending[d.pos:d.pos+n])
			d.pos += n
			written += n
			continue
		}
		if d.eof {
			break
		}
		if cap(buf)-written < d.info.MaxBlockSize*d.info.Channels && written > 0 {
			// Caller's buffer can't safely absorb another whole block.
			// Return what we have; the caller can try again.
			break
		}
		if !d.dec.ProcessSingle() {
			if written > 0 {
				return written / d.info.Channels, d.activeError(ErrInvalidStream)
			}
			return 0, d.activeError(ErrInvalidStream)
		}
		if d.dec.State() == nativeflac.DecoderEndOfStream {
			d.eof = true
			if d.cfg.md5Check {
				if !d.dec.Finish() {
					return written / d.info.Channels, ErrMD5Mismatch
				}
			}
		}
	}

	if written == 0 {
		return 0, io.EOF
	}
	if d.eof && d.pos == len(d.pending) {
		return written / d.info.Channels, io.EOF
	}
	return written / d.info.Channels, nil
}

// activeError surfaces a pending decode-side error, clearing it so it is
// reported once. Mirrors cgoDecoder.activeError minus the Go-reader
// error (the native BitReader read callback folds io.EOF into a clean
// starve rather than propagating reader errors).
func (d *nativeDecoder) activeError(fallback error) error {
	if d.decodeErr != nil {
		err := d.decodeErr
		d.decodeErr = nil
		return err
	}
	return fallback
}

func (d *nativeDecoder) StreamInfo() StreamInfo { return d.info }
func (d *nativeDecoder) SampleRate() int        { return d.info.SampleRate }
func (d *nativeDecoder) Channels() int          { return d.info.Channels }
func (d *nativeDecoder) BitsPerSample() int     { return d.info.BitsPerSample }

// Vendor returns "" — the native decode-only path length-skips
// VORBIS_COMMENT (metadata_decode.go), matching libFLAC when no
// metadata callback is registered for that block. A stream with no
// VORBIS_COMMENT yields the same result on the cgo path.
func (d *nativeDecoder) Vendor() string { return "" }

// Tags returns nil for the same reason Vendor returns "": the native
// decode path does not parse VORBIS_COMMENT.
func (d *nativeDecoder) Tags() map[string][]string { return nil }

func (d *nativeDecoder) Reset() error {
	seeker, ok := d.r.(io.Seeker)
	if !ok {
		return ErrUnsupportedStream
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return err
	}
	// Re-init the native state machine against the rewound reader.
	st := d.dec.InitStream(d.r, d.write, d.onError, d.cfg.md5Check)
	if st != nativeflac.DecoderSearchForMetadata {
		return ErrInternal
	}
	d.haveInfo = false
	d.pending = d.pending[:0]
	d.pos = 0
	d.eof = false
	d.decodeErr = nil
	return nil
}

func (d *nativeDecoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	d.dec = nil
	return nil
}
