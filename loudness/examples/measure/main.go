// Measure example: synthesise a short multi-level programme (a quiet
// intro, a loud chorus, and a mid-level outro) and run it through
// loudness.Measure in one shot, printing the full report the way a
// broadcast loudness scanner would.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/loudness"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func main() {
	const sampleRate = consts.SampleRate48000

	// Build a three-section mono programme at three different levels,
	// each long enough to be gated into the integrated measurement
	// (segments well over the 400ms momentary window).
	intro := generators.Sine(consts.FreqNoteA4, 2*time.Second, sampleRate)
	mutations.ApplyGain(intro.Data, mutations.Decibels(-30)) // quiet intro

	chorus := generators.Sine(consts.FreqNoteA4, 3*time.Second, sampleRate)
	mutations.ApplyGain(chorus.Data, mutations.Decibels(-6)) // loud chorus

	outro := generators.Sine(consts.FreqNoteA4, 2*time.Second, sampleRate)
	mutations.ApplyGain(outro.Data, mutations.Decibels(-18)) // mid-level outro

	programme := mutations.Audio{
		SampleRate: sampleRate,
		Channels:   1,
		Data:       append(append(append([]float64{}, intro.Data...), chorus.Data...), outro.Data...),
	}

	meas, err := loudness.Measure(programme)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("programme length:   %s\n", programme.Duration())
	fmt.Printf("Integrated loudness: %.1f LUFS\n", meas.Integrated)
	fmt.Printf("Max momentary:       %.1f LUFS\n", meas.MaxMomentary)
	fmt.Printf("Max short-term:      %.1f LUFS\n", meas.MaxShortTerm)
	fmt.Printf("Loudness range:      %.1f LU\n", meas.Range)
	fmt.Printf("True peak:           %.1f dBTP\n", meas.TruePeak)
	fmt.Printf("Sample peak:         %.1f dBFS\n", meas.SamplePeak)
	fmt.Printf("RMS:                 %.1f dBFS\n", meas.RMS)
	for c, tp := range meas.ChannelTruePeaks {
		fmt.Printf("  channel %d true peak: %.4f (linear)\n", c, tp)
	}
}
