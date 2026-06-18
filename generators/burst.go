package generators

import (
	"math"
	"time"

	"go-mediatoolkit/mutations"
)

// Beep renders a short sine burst suitable for UI or game-SFX cues.
// The tone holds at amplitude 0.4 for most of duration, then fades
// linearly to silence over the final 30 ms (or the whole duration,
// whichever is shorter) so the clip ends without a click. Returns a
// mono Audio at the given sample rate.
func Beep(freq float64, duration time.Duration, sampleRate int) mutations.Audio {
	n := int(duration.Seconds() * float64(sampleRate))
	data := make([]float64, n)
	angular := 2.0 * math.Pi * freq / float64(sampleRate)
	for i := range data {
		data[i] = 0.4 * math.Sin(angular*float64(i))
	}
	fade := 30 * time.Millisecond
	if fade > duration {
		fade = duration
	}
	env := mutations.FadeOutEnvelope(duration-fade, fade)
	mutations.ApplyGainEnvelope(data, env, 0, 1, sampleRate)
	return mutations.Audio{Data: data, SampleRate: sampleRate, Channels: 1}
}

// Pluck renders a sine with exponential decay — a cheap imitation of
// a plucked string. Amplitude starts at 0.4 and decays with a time
// constant of duration/3, so perceived loudness tapers naturally to
// silence by the end. Returns a mono Audio at the given sample rate.
func Pluck(freq float64, duration time.Duration, sampleRate int) mutations.Audio {
	n := int(duration.Seconds() * float64(sampleRate))
	data := make([]float64, n)
	angular := 2.0 * math.Pi * freq / float64(sampleRate)
	for i := range data {
		decay := math.Exp(-3.0 * float64(i) / float64(n))
		data[i] = 0.4 * decay * math.Sin(angular*float64(i))
	}
	return mutations.Audio{Data: data, SampleRate: sampleRate, Channels: 1}
}
