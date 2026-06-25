// Package generators provides audio signal generators for testing,
// examples, and synthesis.
package generators

import (
	"math"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Sine generates a mono sine wave at the given frequency over the
// requested duration. Amplitude is in the range [-1, 1].
func Sine(freq float64, duration time.Duration, sampleRate int) mutations.Audio {
	frames := int(duration.Seconds() * float64(sampleRate))
	data := make([]float64, frames)
	SineInto(data, freq, sampleRate)
	return mutations.Audio{Data: data, SampleRate: sampleRate, Channels: 1}
}

// SineInto writes a sine wave into the provided buffer and returns
// the number of samples written. Zero-allocation form for callers
// that already own an output buffer (e.g. streaming into a ring).
func SineInto(buf []float64, freq float64, sampleRate int) int {
	angular := 2.0 * math.Pi * freq / float64(sampleRate)
	for i := range buf {
		buf[i] = math.Sin(angular * float64(i))
	}
	return len(buf)
}

// Chord sums unit-amplitude sines at every frequency in freqs and
// normalises by the count so the output stays near ±0.5 regardless
// of how many notes are stacked. Useful for static chord pads,
// drones, and sustained tones whose individual pitches might not
// complete integer cycles in the buffer — wrap the returned Audio
// with .CrossfadeLoop(...) before looping to remove seam clicks.
func Chord(freqs []float64, duration time.Duration, sampleRate int) mutations.Audio {
	frames := int(duration.Seconds() * float64(sampleRate))
	data := make([]float64, frames)
	if len(freqs) == 0 {
		return mutations.Audio{Data: data, SampleRate: sampleRate, Channels: 1}
	}
	inv := 0.5 / float64(len(freqs))
	for _, f := range freqs {
		angular := 2.0 * math.Pi * f / float64(sampleRate)
		for i := range data {
			data[i] += math.Sin(angular*float64(i)) * inv
		}
	}
	return mutations.Audio{Data: data, SampleRate: sampleRate, Channels: 1}
}
