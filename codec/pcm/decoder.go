package pcm

import (
	"encoding/binary"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"io"
)

type decoder struct {
	r          io.Reader
	channels   int
	sampleRate int
	format     mutations.SampleFormat
	order      binary.ByteOrder
	bps        int // bytes per sample, cached
	byteBuf    []byte
	decode     decodeFunc

	// Partial sample bytes carried over between Read calls. This handles
	// the case where io.Reader returns a byte count that is not a multiple
	// of bps (e.g., network sockets, pipes).
	leftover    [8]byte // max 8 bytes for float64
	leftoverLen int
}

func (d *decoder) Read(buf []float64) (mutations.Audio, error) {
	if len(buf) == 0 {
		return d.wrap(buf, 0), nil
	}

	// How many bytes do we need to fill buf?
	needed := len(buf) * d.bps

	// Ensure byteBuf is large enough: leftover + new bytes.
	total := d.leftoverLen + needed
	if total > cap(d.byteBuf) {
		d.byteBuf = make([]byte, total)
	} else {
		d.byteBuf = d.byteBuf[:total]
	}

	// Copy leftover bytes from previous call.
	copy(d.byteBuf, d.leftover[:d.leftoverLen])

	// Read from the underlying reader.
	n, err := d.r.Read(d.byteBuf[d.leftoverLen:])
	available := d.leftoverLen + n
	d.leftoverLen = 0

	// Align down to whole-sample boundary.
	aligned := available - available%d.bps
	remainder := available - aligned

	if aligned == 0 {
		// Not enough bytes for a single sample. Save what we have.
		copy(d.leftover[:], d.byteBuf[:remainder])
		d.leftoverLen = remainder
		if err == nil {
			return d.wrap(buf, 0), nil
		}
		return d.wrap(buf, 0), err
	}

	// Convert aligned bytes to float64.
	samples := d.decode(d.byteBuf[:aligned], buf, d.order)

	// Save any trailing partial-sample bytes.
	if remainder > 0 {
		copy(d.leftover[:], d.byteBuf[aligned:available])
		d.leftoverLen = remainder
	}

	return d.wrap(buf, samples), err
}

func (d *decoder) wrap(buf []float64, n int) mutations.Audio {
	return mutations.Audio{Data: buf[:n], SampleRate: d.sampleRate, Channels: d.channels}
}

func (d *decoder) Channels() int   { return d.channels }
func (d *decoder) SampleRate() int { return d.sampleRate }
