// game_sfx demonstrates the "cue in the future" scheduling pattern
// typical of game engines: every event places a CachedClip at a
// specific offset on a Timeline, which the mixer consumes at
// wall-clock rate. Run with `go run ./timeline/examples/game_sfx`;
// Ctrl-C to exit.
//
// Requires an audio output device (chosen via the device picker); plays
// until Ctrl-C.
//
// The example pre-schedules a handful of SFX cues so they fire at
// known offsets after start. A real game would call tl.Schedule on
// every event — same API, just driven by gameplay rather than a
// startup loop. Notice how clips with different durations can
// overlap freely: the timeline sums them, the mixer applies the
// soft saturator.
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
	"go-mediatoolkit/timeline"
	"go-mediatoolkit/tools/audioio"
	"go-mediatoolkit/tools/devicepicker"
)

const (
	sampleRate = consts.SampleRate48000
	channels   = 1
)

func main() {
	sys, err := devices.GetSystem()
	if err != nil {
		log.Fatalf("GetSystem: %v", err)
	}
	defer sys.Close()
	sel, err := devicepicker.Select(devicepicker.Options{System: sys, Output: true, Title: "Game SFX — pick a speaker"})
	if err != nil {
		log.Fatalf("device pick: %v", err)
	}
	speaker := *sel.Output

	tl, err := timeline.NewTimeline(sampleRate, channels)
	if err != nil {
		log.Fatalf("NewTimeline: %v", err)
	}
	defer tl.Close()

	// Pre-build a handful of SFX clips from generators.Beep — each
	// is a sine burst with a short fade-out tail.
	bleep := timeline.MustCacheClip(generators.Beep(consts.FreqNoteA5, 150*time.Millisecond, sampleRate))
	thud := timeline.MustCacheClip(generators.Beep(consts.FreqNoteE2, 250*time.Millisecond, sampleRate))
	chime := timeline.MustCacheClip(generators.Beep(consts.FreqNoteE6, 500*time.Millisecond, sampleRate))

	// Schedule the events. The timeline cursor advances at wall
	// clock, so Start is "seconds from now".
	schedule := []struct {
		name string
		at   time.Duration
		clip *timeline.CachedClip
	}{
		{"bleep", 500 * time.Millisecond, bleep},
		{"bleep", 800 * time.Millisecond, bleep},
		{"thud", 1200 * time.Millisecond, thud},
		{"chime", 2000 * time.Millisecond, chime},
		{"bleep", 2500 * time.Millisecond, bleep},
		{"thud", 3000 * time.Millisecond, thud},
	}
	for _, s := range schedule {
		_, err := tl.Schedule(timeline.Cue{
			Source:    s.clip.Playhead(),
			Start:     s.at,
			Transform: timeline.NewFadeIn(20 * time.Millisecond), // click-free attack
		})
		if err != nil {
			log.Fatalf("schedule %s@%s: %v", s.name, s.at, err)
		}
		log.Printf("scheduled %s at %s", s.name, s.at)
	}

	// Frames=0 lets the backend pick its native callback size; the
	// mixer's ChunkFrames default (~10ms) and live-cap auto-grow
	// together absorb whatever the device hands back.
	out, err := audioio.OpenOutput(sys, speaker, devices.StreamFormat{SampleRate: sampleRate, Channels: channels})
	if err != nil {
		log.Fatalf("OpenOutput: %v", err)
	}
	defer out.Close()
	outFmt := out.Format()

	mx, err := mixer.New(mixer.Config{SampleRate: outFmt.SampleRate, Channels: outFmt.Channels})
	if err != nil {
		log.Fatalf("mixer.New: %v", err)
	}
	defer mx.Close()
	if _, err := mx.AddSource(tl); err != nil {
		log.Fatalf("AddSource: %v", err)
	}
	out.Bind(mx.Fill)

	if err := out.Start(); err != nil {
		log.Fatalf("out.Start: %v", err)
	}
	log.Printf("playing scheduled cues to %q (Ctrl-C to exit)", speaker.Name)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	<-sigs
	fmt.Println()
}
