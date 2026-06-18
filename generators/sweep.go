package generators

import (
	"math"
	"time"

	"go-mediatoolkit/mutations"
)

// SineSweep generates a logarithmic sine sweep from startHz to endHz
// over the requested duration. This is useful as a test signal that
// excites every frequency band from the audible range into the
// high-mid transition.
//
// Amplitude is fixed at 1.0.
func SineSweep(startHz, endHz float64, duration time.Duration, sampleRate int) mutations.Audio {
	frames := int(duration.Seconds() * float64(sampleRate))
	data := make([]float64, frames)
	SineSweepInto(data, startHz, endHz, sampleRate)
	return mutations.Audio{Data: data, SampleRate: sampleRate, Channels: 1}
}

// SineSweepInto writes a log sine sweep into buf. The sweep ramps
// instantaneous frequency from startHz at buf[0] to endHz at
// buf[len(buf)-1].
func SineSweepInto(buf []float64, startHz, endHz float64, sampleRate int) int {
	if len(buf) == 0 {
		return 0
	}
	fs := float64(sampleRate)
	dur := float64(len(buf)) / fs
	logRatio := math.Log(endHz / startHz)
	phase := 0.0
	for i := range buf {
		t := float64(i) / fs
		freq := startHz * math.Exp(logRatio*t/dur)
		phase += 2 * math.Pi * freq / fs
		buf[i] = math.Sin(phase)
	}
	return len(buf)
}
