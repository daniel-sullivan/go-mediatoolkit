package flac

import (
	"io"

	flaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/flac"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// decoder wraps a [libraries/flac.Decoder] and converts its int32
// output to interleaved float64 in [-1.0, 1.0]. The wrapper buffers
// one decoded block worth of samples internally so callers can issue
// arbitrarily small Read requests.
type decoder struct {
	dec flaclib.Decoder

	scratch []int32 // last decoded block, interleaved
	scaleOK bool    // scale + channelsCache are populated after the first decode
	scale   float64 // 1 / (2^(bits-1)-1)

	// pendingOff / pendingTotal index unread samples in scratch.
	// pendingOff..pendingTotal contains the float64 values that have
	// not yet been delivered to the caller.
	pendingOff   int
	pendingTotal int

	// channelsCache mirrors d.dec.Channels() so we have a stable value
	// before metadata has been parsed (which happens lazily on the
	// first decodeBlock call).
	channelsCache   int
	sampleRateCache int

	eof bool
}

func (d *decoder) Read(out []float64) (mutations.Audio, error) {
	channels := d.channelsCache
	sampleRate := d.sampleRateCache

	// Drain pending samples first.
	written := d.drainPending(out)

	// If the buffer is fully written or we cannot fit another channel
	// frame, return what we have.
	if written == len(out) {
		return d.wrap(out, written, channels, sampleRate), nil
	}

	// EOF: nothing more to decode. Return whatever we drained.
	if d.eof {
		if written == 0 {
			return d.wrap(out, 0, channels, sampleRate), io.EOF
		}
		return d.wrap(out, written, channels, sampleRate), io.EOF
	}

	// Decode further blocks until the caller's buffer is full or the
	// stream ends.
	for written < len(out) {
		err := d.decodeBlock()
		// Re-read channel/rate after the first decode (they may have
		// just been resolved from STREAMINFO).
		if !d.scaleOK && d.dec.BitsPerSample() > 0 {
			d.scale = 1.0 / float64(int32(1)<<(d.dec.BitsPerSample()-1)-1)
			d.channelsCache = d.dec.Channels()
			d.sampleRateCache = d.dec.SampleRate()
			channels = d.channelsCache
			sampleRate = d.sampleRateCache
			d.scaleOK = true
		}

		written += d.drainPending(out[written:])

		if err == io.EOF {
			d.eof = true
			break
		}
		if err != nil {
			return d.wrap(out, written, channels, sampleRate), err
		}
	}

	if d.eof && written == 0 {
		return d.wrap(out, 0, channels, sampleRate), io.EOF
	}
	if d.eof && d.pendingOff == d.pendingTotal {
		// All decoded samples delivered and stream ended — surface EOF
		// alongside the final samples (codec.Decoder doc allows this).
		return d.wrap(out, written, channels, sampleRate), io.EOF
	}
	return d.wrap(out, written, channels, sampleRate), nil
}

// drainPending copies as many leftover scratch samples as fit in out
// (rounded down to a channel boundary). Returns the count copied.
func (d *decoder) drainPending(out []float64) int {
	if d.pendingOff >= d.pendingTotal {
		return 0
	}
	channels := d.channelsCache
	if channels < 1 {
		// Channels not resolved yet; nothing to drain.
		return 0
	}
	avail := d.pendingTotal - d.pendingOff
	take := avail
	if take > len(out) {
		take = (len(out) / channels) * channels
	}
	if take == 0 {
		return 0
	}
	for i := 0; i < take; i++ {
		out[i] = float64(d.scratch[d.pendingOff+i]) * d.scale
	}
	d.pendingOff += take
	return take
}

// decodeBlock decodes one FLAC block into scratch. Returns io.EOF when
// the underlying decoder reports end-of-stream; the partial samples (if
// any) are still available in pendingTotal.
func (d *decoder) decodeBlock() error {
	channels := d.dec.Channels()
	if channels < 1 {
		channels = flaclib.MaxChannels
	}
	want := flaclib.MaxBlockSize * channels
	if cap(d.scratch) < want {
		d.scratch = make([]int32, want)
	} else {
		d.scratch = d.scratch[:want]
	}
	n, err := d.dec.Decode(d.scratch)
	c := d.dec.Channels()
	if c < 1 {
		c = channels
	}
	d.pendingOff = 0
	d.pendingTotal = n * c
	return err
}

func (d *decoder) wrap(buf []float64, n, channels, sampleRate int) mutations.Audio {
	return mutations.Audio{Data: buf[:n], SampleRate: sampleRate, Channels: channels}
}

func (d *decoder) Channels() int {
	if d.channelsCache > 0 {
		return d.channelsCache
	}
	return d.dec.Channels()
}

func (d *decoder) SampleRate() int {
	if d.sampleRateCache > 0 {
		return d.sampleRateCache
	}
	return d.dec.SampleRate()
}
