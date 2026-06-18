// multitrack demonstrates a three-track mix driven by separate
// Sources on a single Mixer. Track 1 is an ambient pad loop, track 2
// a repeating bass pulse on a Timeline, track 3 a short melody
// playlist. Per-track gains are set via mx.TrackHandle.SetGain so
// each layer can be balanced independently. Run with
// `go run ./timeline/examples/multitrack`; Ctrl-C to exit.
//
// Requires a default audio output device; plays until Ctrl-C.
//
// This is the pattern the toolkit is ultimately aiming for: any
// number of Sources (clips, timelines, live input) share one Mixer
// that handles format adaptation, summing, and final saturation
// before a single device output callback.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-mediatoolkit/consts"
	"go-mediatoolkit/devices"
	"go-mediatoolkit/generators"
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

	mx, err := mixer.New(mixer.Config{SampleRate: sampleRate, Channels: channels, ChunkFrames: frames})
	if err != nil {
		log.Fatalf("mixer.New: %v", err)
	}
	defer mx.Close()

	// Track 1: ambient pad (lowpassed pink noise), looped via Repeat.
	pad := generators.PinkNoise(4*time.Second, sampleRate, 42).
		ApplyEffect(mutations.NewLowpass(500, 0.707, sampleRate, channels))
	padClip := timeline.MustCacheClip(pad)
	padLoop := timeline.Repeat(sampleRate, channels, 0, padClip.Playhead)
	padTrack, _ := mx.AddSource(padLoop)
	padTrack.SetGain(0.4)

	// Track 2: a bass pulse every half-second on a Timeline.
	bass := timeline.MustCacheClip(generators.Pluck(consts.FreqNoteA2/2, 400*time.Millisecond, sampleRate))
	staticTL, _ := timeline.NewTimeline(sampleRate, channels)
	for i := 0; i < 16; i++ {
		_, err := staticTL.Schedule(timeline.Cue{
			Source: bass.Playhead(),
			Start:  time.Duration(i) * 500 * time.Millisecond,
		})
		if err != nil {
			log.Fatalf("bass schedule: %v", err)
		}
	}
	bassTrack, _ := mx.AddSource(staticTL)
	bassTrack.SetGain(0.8)

	// Track 3: a melody via Timeline.Append (sequential back-to-back).
	melody, _ := timeline.NewTimeline(sampleRate, channels)
	fade := append(append([]mutations.GainPoint{}, mutations.FadeInEnvelope(30*time.Millisecond)...),
		mutations.FadeOutEnvelope(500*time.Millisecond, 50*time.Millisecond)[1:]...)
	for _, f := range []float64{consts.FreqNoteA4, consts.FreqNoteC5, consts.FreqNoteE5, consts.FreqNoteC5, consts.FreqNoteA4, consts.FreqNoteG4, consts.FreqNoteA4} {
		c := timeline.MustCacheClip(generators.Pluck(f, 600*time.Millisecond, sampleRate))
		_, err := melody.Append(timeline.Cue{Source: c.Playhead(), Transform: timeline.Transform{Gain: fade}})
		if err != nil {
			log.Fatalf("melody append: %v", err)
		}
	}
	melTrack, _ := mx.AddSource(melody)
	melTrack.SetGain(0.6)

	out, err := sys.OpenOutput(speaker, devices.StreamFormat{SampleRate: sampleRate, Channels: channels, Frames: frames}, mx.Fill)
	if err != nil {
		log.Fatalf("OpenOutput: %v", err)
	}
	defer out.Close()
	if err := out.Start(); err != nil {
		log.Fatalf("out.Start: %v", err)
	}
	log.Printf("multitrack → %q (pad=%.2f, bass=%.2f, melody=%.2f)",
		speaker.Name, padTrack.Gain(), bassTrack.Gain(), melTrack.Gain())

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-sigs:
			fmt.Println()
			return
		case <-tick.C:
			if u := mx.Underruns(); u > 0 {
				log.Printf("underruns: %d", u)
			}
		}
	}
}
