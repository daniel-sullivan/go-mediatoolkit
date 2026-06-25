package flac

import (
	flaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/flac"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// encoder wraps a [libraries/flac.Encoder] and converts interleaved
// float64 input to int32 sign-extended for the configured bit depth
// before passing the buffer to libFLAC.
type encoder struct {
	enc           flaclib.Encoder
	sampleRate    int
	channels      int
	bitsPerSample int

	scratch []int32 // reused per-Write conversion target
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
		e.scratch = make([]int32, len(audio.Data))
	} else {
		e.scratch = e.scratch[:len(audio.Data)]
	}

	// Convert float64 → int32, saturating at the bit-depth limits so
	// values just above ±1.0 don't wrap around. Compute the limits in
	// int64 so they remain correct at bps=32 (where 1<<31 is already
	// INT32_MIN and arithmetic on int32 would wrap).
	half := int64(1) << uint(e.bitsPerSample-1)
	maxVal := int32(half - 1)
	minVal := int32(-half)
	maxAbs := float64(half - 1)
	for i, f := range audio.Data {
		v := f * maxAbs
		switch {
		case v >= float64(maxVal):
			e.scratch[i] = maxVal
		case v <= float64(minVal):
			e.scratch[i] = minVal
		case v >= 0:
			e.scratch[i] = int32(v + 0.5)
		default:
			e.scratch[i] = int32(v - 0.5)
		}
	}

	if err := e.enc.Encode(e.scratch); err != nil {
		return 0, err
	}
	return len(audio.Data), nil
}

func (e *encoder) Close() error    { return e.enc.Close() }
func (e *encoder) Channels() int   { return e.channels }
func (e *encoder) SampleRate() int { return e.sampleRate }
