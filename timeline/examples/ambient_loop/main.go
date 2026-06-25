// ambient_loop demonstrates LoopingTimeline with a slow tremolo
// modulating the overall gain via Transform.GainFunc — a callback
// escape hatch for gain shapes that can't be expressed as piecewise-
// linear points. Run with `go run ./timeline/examples/ambient_loop`;
// Ctrl-C to exit.
//
// Requires a default audio output device; plays until Ctrl-C.
//
// The loop is a 4-second filtered pink-noise texture (pad-ish). The
// LFO on the cue's gain breathes it up and down by ±3 dB over 6
// seconds — the sort of thing a drawn automation curve or slow
// oscillator would do in a DAW.
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/devices"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/mixer"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/timeline"
)

const (
	sampleRate = consts.SampleRate48000
	channels   = 1
	frames     = 256

	rawLoopSeconds = 8 // length before self-crossfade trims the tail
	seamFade       = 20 * time.Millisecond

	lfoPeriod  = 8 * time.Second
	lfoDepthDB = 3.0 // symmetric ±3 dB in the dB domain
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

	// Backing texture: pink noise filtered through a soft LPF so it
	// sounds like a pad rather than hiss, then self-crossfaded so it
	// loops seamlessly.
	pad := generators.PinkNoise(rawLoopSeconds*time.Second, sampleRate, 42).
		ApplyEffect(mutations.NewLowpass(500, 0.707, sampleRate, channels)).
		CrossfadeLoop(seamFade)

	clip, err := timeline.LoadClipFromAudio(pad)
	if err != nil {
		log.Fatalf("LoadClipFromAudio: %v", err)
	}

	// Wrap the clip in a Repeat source that loops forever on each
	// Source EOF. Then schedule it as a cue on a Timeline so we can
	// attach a Transform (GainFunc for the breathing LFO).
	loopSrc := timeline.Repeat(sampleRate, channels, 0, clip.Playhead)
	loop, err := timeline.NewTimeline(sampleRate, channels)
	if err != nil {
		log.Fatalf("NewTimeline: %v", err)
	}
	defer loop.Close()
	_, err = loop.Schedule(timeline.Cue{
		Source: loopSrc,
		Transform: timeline.Transform{
			GainFunc: func(elapsed time.Duration) float64 {
				phase := 2 * math.Pi * elapsed.Seconds() / lfoPeriod.Seconds()
				return mutations.Decibels(lfoDepthDB * math.Sin(phase))
			},
		},
	})
	if err != nil {
		log.Fatalf("loop.Schedule: %v", err)
	}

	mx, err := mixer.New(mixer.Config{SampleRate: sampleRate, Channels: channels, ChunkFrames: frames})
	if err != nil {
		log.Fatalf("mixer.New: %v", err)
	}
	defer mx.Close()
	if _, err := mx.AddSource(loop); err != nil {
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
	log.Printf("ambient loop playing to %q (loop=%s, LFO=%s, ±%.1fdB)", speaker.Name, pad.Duration().Round(time.Millisecond), lfoPeriod, lfoDepthDB)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	<-sigs
	fmt.Println()
}
