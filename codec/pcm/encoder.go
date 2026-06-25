package pcm

import (
	"encoding/binary"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"io"
)

type encoder struct {
	w          io.Writer
	channels   int
	sampleRate int
	format     mutations.SampleFormat
	order      binary.ByteOrder
	bps        int // bytes per sample, cached
	byteBuf    []byte
	encode     encodeFunc
}

func (e *encoder) Write(audio mutations.Audio) (int, error) {
	if audio.SampleRate != e.sampleRate || audio.Channels != e.channels {
		return 0, ErrFormatMismatch
	}
	buf := audio.Data
	if len(buf) == 0 {
		return 0, nil
	}

	// Ensure byteBuf is large enough.
	needed := len(buf) * e.bps
	if needed > cap(e.byteBuf) {
		e.byteBuf = make([]byte, needed)
	} else {
		e.byteBuf = e.byteBuf[:needed]
	}

	samples := e.encode(buf, e.byteBuf, e.order)
	byteCount := samples * e.bps

	written, err := e.w.Write(e.byteBuf[:byteCount])
	if err != nil {
		return written / e.bps, err
	}
	if written < byteCount {
		return written / e.bps, ErrShortWrite
	}

	return samples, nil
}

func (e *encoder) Channels() int   { return e.channels }
func (e *encoder) SampleRate() int { return e.sampleRate }

// Close is a no-op for PCM encoding (no framing to flush).
func (e *encoder) Close() error { return nil }
