package generators

import (
	"math/rand"
	"time"

	"go-mediatoolkit/mutations"
)

// WhiteNoise generates uniform white noise in [-1, 1].
//
// Returned Audio is mono at the given sample rate. The stream is
// seeded from a Go math/rand source so runs are reproducible.
func WhiteNoise(duration time.Duration, sampleRate int, seed int64) mutations.Audio {
	frames := int(duration.Seconds() * float64(sampleRate))
	data := make([]float64, frames)
	WhiteNoiseInto(data, seed)
	return mutations.Audio{Data: data, SampleRate: sampleRate, Channels: 1}
}

// WhiteNoiseInto fills buf with uniform white noise in [-1, 1]. Zero-
// allocation form.
func WhiteNoiseInto(buf []float64, seed int64) int {
	r := rand.New(rand.NewSource(seed))
	for i := range buf {
		buf[i] = r.Float64()*2 - 1
	}
	return len(buf)
}

// PinkNoise generates 1/f "pink" noise using Paul Kellet's economy
// 7-tap filter on uniform white input. The output is roughly band-
// limited from DC to Nyquist with a -3 dB/octave roll-off, and is
// scaled so peak magnitude stays under ~0.5.
func PinkNoise(duration time.Duration, sampleRate int, seed int64) mutations.Audio {
	frames := int(duration.Seconds() * float64(sampleRate))
	data := make([]float64, frames)
	PinkNoiseInto(data, seed)
	return mutations.Audio{Data: data, SampleRate: sampleRate, Channels: 1}
}

// PinkNoiseInto fills buf with pink noise using Paul Kellet's filter.
// Each call maintains internal filter state across the buffer, so the
// result is statistically identical to a single call of the same
// total length.
func PinkNoiseInto(buf []float64, seed int64) int {
	r := rand.New(rand.NewSource(seed))
	var b0, b1, b2, b3, b4, b5, b6 float64
	for i := range buf {
		w := r.Float64()*2 - 1
		b0 = 0.99886*b0 + w*0.0555179
		b1 = 0.99332*b1 + w*0.0750759
		b2 = 0.96900*b2 + w*0.1538520
		b3 = 0.86650*b3 + w*0.3104856
		b4 = 0.55000*b4 + w*0.5329522
		b5 = -0.7616*b5 - w*0.0168980
		pink := b0 + b1 + b2 + b3 + b4 + b5 + b6 + w*0.5362
		b6 = w * 0.115926
		buf[i] = pink * 0.11
	}
	return len(buf)
}
