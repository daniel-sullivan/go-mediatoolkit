// Command cancel is a minimal, fully offline demonstration of
// aec.Canceller: synthesize a far-end (loudspeaker) signal and a
// near-end (microphone) capture containing a delayed, attenuated echo
// of it plus a quiet noise floor, run the pair through a Canceller, and
// print the before/after residual-echo level and the converged
// metrics.
//
// No audio device, mixer, or timeline is involved — see the duplex
// example for a Canceller wired into a mixer's live output.
package main

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/aec"
	"github.com/daniel-sullivan/go-mediatoolkit/loudness"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	const sampleRate = 16000
	const delayFrames = 320 // 20ms synthetic acoustic echo path
	const echoGain = 0.5
	const renderAmplitude = 0.3
	const noiseAmplitude = 0.001
	const duration = 6 * time.Second

	c, err := aec.NewCanceller(aec.CancellerConfig{SampleRate: sampleRate, CaptureChannels: 1, RenderChannels: 1})
	if err != nil {
		return err
	}

	// The far-end (render) stimulus must be broadband (white noise, not
	// a pure tone): AEC3's delay estimator and adaptive filter identify
	// the echo path from the render signal's own statistics, and a
	// periodic tone makes the true delay ambiguous with that delay plus
	// or minus any whole number of periods — see
	// canceller_test.go's TestCanceller_EchoReduction doc comment for
	// the full rationale (measured: a 350Hz tone here converges to a
	// bogus delay and under 10dB of reduction; white noise converges
	// within a couple of ms and removes >25dB).
	n := int(duration.Seconds() * sampleRate)
	rng := rand.New(rand.NewSource(1))
	render := make([]float64, n)
	for i := range render {
		render[i] = renderAmplitude * (2*rng.Float64() - 1)
	}

	// Synthetic capture: a fixed-delay, attenuated copy of the render
	// signal (the "echo"), plus a small near-end noise floor standing
	// in for whatever the near end actually says.
	capture := make([]float64, n)
	for i := 0; i < n; i++ {
		var echo float64
		if i >= delayFrames {
			echo = echoGain * render[i-delayFrames]
		}
		noise := noiseAmplitude * (2*rng.Float64() - 1)
		capture[i] = echo + noise
	}
	captureOrig := append([]float64(nil), capture...)

	const chunk = sampleRate / 100 // one 10ms AEC3 frame
	for i := 0; i+chunk <= n; i += chunk {
		if err := c.FeedFarEnd(render[i : i+chunk]); err != nil {
			return err
		}
		c.Process(capture[i : i+chunk])
	}

	// Compare the last second of the ORIGINAL (uncancelled) signal
	// against the last second of the PROCESSED one, once the adaptive
	// filter has had several seconds to converge. Process's output is
	// delayed by exactly c.Latency() (one 10ms chunk here), immaterial
	// against a full one-second comparison window.
	tail := sampleRate
	beforeDB := mutations.AmplitudeToDecibels(loudness.RMS(captureOrig[n-tail:]))
	afterDB := mutations.AmplitudeToDecibels(loudness.RMS(capture[n-tail:]))

	m := c.Metrics()
	fmt.Printf("residual echo: before=%.1f dBFS after=%.1f dBFS (reduction=%.1f dB)\n", beforeDB, afterDB, beforeDB-afterDB)
	fmt.Printf("metrics: delay=%dms ERL=%.1fdB ERLE=%.1fdB clockdrift=%v\n", m.DelayMs, m.EchoReturnLoss, m.EchoReturnLossEnhancement, m.Clockdrift)
	return nil
}
