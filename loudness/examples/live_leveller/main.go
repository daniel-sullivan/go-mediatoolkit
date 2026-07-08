// Live leveller example: drive a mixer with a master-bus Leveller (AGC)
// followed by a Monitor, feeding it a track that starts far too quiet,
// and print the leveller's gain and the monitor's short-term loudness
// converging toward the leveller's target over simulated time. Output
// is pulled headlessly via Mixer.Fill (no real audio device), so this
// runs to completion without needing hardware.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/loudness"
	"github.com/daniel-sullivan/go-mediatoolkit/mixer"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/timeline"
)

func main() {
	const (
		sampleRate  = consts.SampleRate48000
		channels    = 2
		toneSeconds = 10.0
	)

	// A mono tone, deliberately far too quiet (roughly -35 LUFS)
	// relative to the leveller's default -23 LUFS target, long enough
	// to watch the AGC converge over several release-time-constants
	// (the default release is 3s).
	tone := generators.Sine(consts.FreqNoteA4, toneSeconds*time.Second, sampleRate)
	mutations.ApplyGain(tone.Data, mutations.Decibels(-32))

	clip, err := timeline.LoadClipFromAudio(tone)
	if err != nil {
		log.Fatal(err)
	}

	lev, err := loudness.NewLeveller(loudness.LevellerConfig{
		SampleRate: sampleRate,
		Channels:   channels,
	})
	if err != nil {
		log.Fatal(err)
	}

	mon, err := loudness.NewMonitor(sampleRate, channels, loudness.ModeShortTerm)
	if err != nil {
		log.Fatal(err)
	}

	// The leveller runs first on the master bus so the monitor reads
	// its output — showing loudness converging toward the leveller's
	// target, not the raw (too-quiet) track.
	mx, err := mixer.New(mixer.Config{
		SampleRate: sampleRate,
		Channels:   channels,
		Processors: []mutations.Processor{lev, mon},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer mx.Close()

	if _, err := mx.AddSource(clip.Playhead()); err != nil {
		log.Fatal(err)
	}

	// Pull output in 100ms chunks, headless (no real audio device).
	// The mix goroutine produces ahead of wall clock into a bounded
	// ring (~200ms by default), so Fill must be paced at roughly the
	// chunk's real duration — draining faster than that starves the
	// ring and reports spurious underrun silence. This still runs
	// entirely offline: nothing here talks to an audio device, the
	// sleep is just pacing against the mixer's own real-time model.
	const chunkDur = 100 * time.Millisecond
	buf := make([]float64, int(chunkDur.Seconds()*sampleRate)*channels)
	steps := int(toneSeconds * time.Second / chunkDur)
	stepsPerSecond := int(time.Second / chunkDur)

	for i := 0; i < steps; i++ {
		mx.Fill(buf)
		time.Sleep(chunkDur)
		if i%stepsPerSecond == 0 {
			st, _ := mon.ShortTerm()
			fmt.Printf("t=%4.1fs  leveller gain=%+6.2f dB  monitor short-term=%6.1f LUFS\n",
				float64(i)*chunkDur.Seconds(), lev.GainDB(), st)
		}
	}
}
