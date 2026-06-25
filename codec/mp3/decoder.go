package mp3

import (
	"io"

	mp3lib "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// scale16 converts a signed 16-bit sample to float64 in [-1.0, 1.0] by
// dividing by 2^15-1.
const scale16 = 1.0 / float64((1<<15)-1)

// decoder wraps a [libraries/mp3.Decoder] and converts its int16 output to
// interleaved float64 in [-1.0, 1.0]. The wrapper buffers one decoded frame
// worth of samples internally so callers can issue arbitrarily small Read
// requests.
type decoder struct {
	dec mp3lib.Decoder

	scratch []int16 // last decoded frame, interleaved

	// pendingOff / pendingTotal index unread samples in scratch.
	pendingOff   int
	pendingTotal int

	channelsCache   int
	sampleRateCache int

	eof bool
}

func (d *decoder) Read(out []float64) (mutations.Audio, error) {
	channels := d.channelsCache
	sampleRate := d.sampleRateCache

	written := d.drainPending(out)
	if written == len(out) {
		return d.wrap(out, written, channels, sampleRate), nil
	}
	if d.eof {
		if written == 0 {
			return d.wrap(out, 0, channels, sampleRate), io.EOF
		}
		return d.wrap(out, written, channels, sampleRate), io.EOF
	}

	for written < len(out) {
		err := d.decodeFrame()
		if d.channelsCache == 0 && d.dec.Channels() > 0 {
			d.channelsCache = d.dec.Channels()
			d.sampleRateCache = d.dec.SampleRate()
			channels = d.channelsCache
			sampleRate = d.sampleRateCache
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
		out[i] = float64(d.scratch[d.pendingOff+i]) * scale16
	}
	d.pendingOff += take
	return take
}

// decodeFrame decodes one MP3 frame into scratch. Returns io.EOF when the
// underlying decoder reports end-of-stream.
func (d *decoder) decodeFrame() error {
	channels := d.dec.Channels()
	if channels < 1 {
		channels = mp3lib.MaxChannels
	}
	// The backend requires room for a full worst-case frame
	// (MaxSamplesPerFrame x MaxChannels), independent of the current
	// stream's channel count, so always size scratch to that minimum.
	want := mp3lib.MaxSamplesPerFrame * mp3lib.MaxChannels
	if cap(d.scratch) < want {
		d.scratch = make([]int16, want)
	} else {
		d.scratch = d.scratch[:want]
	}
	n, err := d.dec.DecodeFrame(d.scratch)
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
