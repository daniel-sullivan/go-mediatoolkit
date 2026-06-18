// playlist demonstrates Timeline.Append playing a sequence of short
// clips back-to-back with a fade-in/out on each entry. Run with
// `go run ./timeline/examples/playlist`; Ctrl-C to exit or wait for
// the last clip to finish.
//
// Requires a default audio output device; exits when the last clip
// finishes or on Ctrl-C.
//
// Each clip is a different musical interval so the ear can hear the
// seamless mid-Pull transition Append delivers. Transform.Gain
// gives a short fade-in/out to prevent clicks at entry boundaries.
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-mediatoolkit/consts"
	"go-mediatoolkit/devices"
	"go-mediatoolkit/mixer"
	"go-mediatoolkit/mutations"
	"go-mediatoolkit/timeline"
)

const (
	sampleRate = consts.SampleRate48000
	channels   = 1
	frames     = 256
)

func main() {
	sys, err := devices.GetSystem()
	if err != nil {
		log.Fatalf("GetSystem: %v", err)
	}
	defer sys.Close()
	speaker, ok := sys.DefaultOutput()
	if !ok {
		log.Fatal("no default output device")
	}

	tl, err := timeline.NewTimeline(sampleRate, channels)
	if err != nil {
		log.Fatalf("NewTimeline: %v", err)
	}
	defer tl.Close()

	notes := []struct {
		name string
		freq float64
	}{
		{"A3", consts.FreqNoteA3},
		{"C4", consts.FreqNoteC4},
		{"E4", consts.FreqNoteE4},
		{"G4", consts.FreqNoteG4},
		{"A4", consts.FreqNoteA4},
	}

	fadeIn := mutations.FadeInEnvelope(50 * time.Millisecond)
	fadeOut := mutations.FadeOutEnvelope(700*time.Millisecond, 100*time.Millisecond)
	// Combine fade-in and fade-out into one envelope by
	// concatenating points.
	combined := append(append([]mutations.GainPoint{}, fadeIn...), fadeOut[1:]...)

	var lastHandle timeline.Handle
	for _, n := range notes {
		clip, err := timeline.LoadClipFromPCM(tone(n.freq, 800*time.Millisecond), sampleRate, channels)
		if err != nil {
			log.Fatalf("LoadClipFromPCM: %v", err)
		}
		h, err := tl.Append(timeline.Cue{Source: clip.Playhead(), Transform: timeline.Transform{Gain: combined}})
		if err != nil {
			log.Fatalf("Append %s: %v", n.name, err)
		}
		lastHandle = h
		log.Printf("queued %s (%.2fHz)", n.name, n.freq)
	}

	mx, err := mixer.New(mixer.Config{SampleRate: sampleRate, Channels: channels, ChunkFrames: frames})
	if err != nil {
		log.Fatalf("mixer.New: %v", err)
	}
	defer mx.Close()
	if _, err := mx.AddSource(tl); err != nil {
		log.Fatalf("AddSource: %v", err)
	}

	out, err := sys.OpenOutput(speaker, devices.StreamFormat{SampleRate: sampleRate, Channels: channels, Frames: frames}, mx.Fill)
	if err != nil {
		log.Fatalf("OpenOutput: %v", err)
	}
	defer out.Close()
	if err := out.Start(); err != nil {
		log.Fatalf("out.Start: %v", err)
	}
	log.Printf("playlist → %q (%d items)", speaker.Name, len(notes))

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sigs:
	case <-lastHandle.Done():
		log.Printf("playlist finished")
	}
	fmt.Println()
}

func tone(freq float64, dur time.Duration) []float64 {
	n := int(dur.Seconds() * sampleRate)
	out := make([]float64, n)
	angular := 2 * math.Pi * freq / float64(sampleRate)
	for i := range out {
		out[i] = 0.3 * math.Sin(angular*float64(i))
	}
	return out
}
