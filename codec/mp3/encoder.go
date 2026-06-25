package mp3

import (
	mp3lib "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// encoder wraps a [libraries/mp3.Encoder] and converts interleaved float64
// input to int16 (scaled and rounded) before passing the buffer to the LAME
// backend.
type encoder struct {
	enc        mp3lib.Encoder
	sampleRate int
	channels   int

	scratch []int16 // reused per-Write conversion target
}

func (e *encoder) Write(audio mutations.Audio) (int, error) {
	if audio.SampleRate != e.sampleRate || audio.Channels != e.channels {
		return 0, ErrFormatMismatch
	}
	if len(audio.Data) == 0 {
		return 0, nil
	}
	if len(audio.Data)%e.channels != 0 {
		return 0, ErrBadArg
	}

	if cap(e.scratch) < len(audio.Data) {
		e.scratch = make([]int16, len(audio.Data))
	} else {
		e.scratch = e.scratch[:len(audio.Data)]
	}

	// Scale float64 [-1.0, 1.0] to signed 16-bit, saturating so values just
	// above ±1.0 don't wrap.
	const maxAbs = float64((1 << 15) - 1)
	for i, f := range audio.Data {
		v := f * maxAbs
		switch {
		case v >= maxAbs:
			e.scratch[i] = (1 << 15) - 1
		case v <= -float64(int32(1)<<15):
			e.scratch[i] = -(1 << 15)
		case v >= 0:
			e.scratch[i] = int16(v + 0.5)
		default:
			e.scratch[i] = int16(v - 0.5)
		}
	}

	if err := e.enc.EncodeFrame(e.scratch); err != nil {
		return 0, err
	}
	return len(audio.Data), nil
}

func (e *encoder) Close() error    { return e.enc.Close() }
func (e *encoder) Channels() int   { return e.channels }
func (e *encoder) SampleRate() int { return e.sampleRate }
