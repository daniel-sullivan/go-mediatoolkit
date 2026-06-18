// karaoke mixes microphone input with a generated "backing track"
// (a slow-sweeping sine chord) and plays the result to the default
// output. Run with `go run ./timeline/examples/karaoke`. Ctrl-C to
// exit.
//
// Requires both an audio input (microphone) and output device (chosen
// via the device picker); plays until Ctrl-C.
//
// The example demonstrates the InputSource → EffectSource → Mixer
// path: the mic's input callback writes into an InputSource; an
// EffectSource wraps the InputSource with HPF → echo → reverb →
// make-up gain; the mixer pulls the processed mic alongside a looping
// backing-track clip. Because the input source reports Live() == true,
// the mixer caps its ring pre-roll via LiveRingFrames so live samples
// do not fall behind wall clock.
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
	"go-mediatoolkit/tools/audioio"
	"go-mediatoolkit/tools/devicepicker"
)

const (
	sampleRate     = consts.SampleRate48000
	channels       = 1
	backingSeconds = 4

	micHpfCutoff = 80.0
	micHpfQ      = 0.707

	echoDelay    = 220 * time.Millisecond
	echoFeedback = 0.45
	echoWet      = 0.35

	reverbRoomSize = 0.25
	reverbDamping  = 0.35
	reverbWet      = 0.1

	// Make-up gain trims the wet-heavy chain tail before the mixer's
	// saturator sees it — that's what an inline Gain stage is *for*;
	// reach for micTrack.SetGain instead when you want a live fader.
	micMakeupGain = 0.7
)

func main() {
	sys, err := devices.GetSystem()
	if err != nil {
		log.Fatalf("GetSystem: %v", err)
	}
	defer sys.Close()
	sel, err := devicepicker.Select(devicepicker.Options{System: sys, Input: true, Output: true, Title: "Karaoke — pick a mic and speaker"})
	if err != nil {
		log.Fatalf("device pick: %v", err)
	}
	mic, speaker := *sel.Input, *sel.Output

	// Backing: a sustained C-major chord drone underneath a melody —
	// both built dynamically by the generators package, no audio file
	// required. The chord uses CrossfadeLoop so its 4s window wraps
	// without a click; the melody is a one-shot rendering of "Mary
	// Had a Little Lamb" (see generators.MaryHadALittleLamb) wrapped
	// in Repeat so it also loops indefinitely. Each goes onto the
	// mixer as its own track, demonstrating that independent Sources
	// — different durations, different loop strategies — compose
	// without needing to be merged ahead of time.
	chord := generators.Chord(
		[]float64{consts.FreqNoteC3, consts.FreqNoteE3, consts.FreqNoteG3},
		backingSeconds*time.Second,
		sampleRate,
	)
	chordClip := timeline.MustCacheClip(chord.CrossfadeLoop(50 * time.Millisecond))
	chordLoop := timeline.Repeat(sampleRate, channels, 0, chordClip.Playhead)

	melodyClip := timeline.MustCacheClip(generators.MaryHadALittleLamb(sampleRate))
	melodyLoop := timeline.Repeat(sampleRate, channels, 0, melodyClip.Playhead)

	// audioio hides the open-then-learn-format dance so this file can
	// stay focused on the audio graph. Frames=256 is the lesson here:
	// karaoke's whole point is low-latency live monitoring, so we
	// hint the device toward small callbacks (~5ms at 48kHz). Buffer
	// sizing, mixer chunking and live-cap headroom all default —
	// the mixer auto-adapts to whatever the backend actually
	// negotiates.
	format := devices.StreamFormat{SampleRate: sampleRate, Channels: channels, Frames: 256}
	input, err := audioio.OpenInput(sys, mic, format, 0)
	if err != nil {
		log.Fatalf("OpenInput: %v", err)
	}
	defer input.Close()
	micFmt := input.Format()
	log.Printf("mic negotiated: %dHz %dch frames=%d", micFmt.SampleRate, micFmt.Channels, micFmt.Frames)

	// Effects run at the mic's native format; the mixer's adaptSource
	// resamples/remixes into the output format at AddSource time.
	// Processor order matters: HPF first so echo/reverb don't sustain
	// low-frequency pops, Gain last so it trims the wet tail.
	micWithFX := timeline.NewEffectSource(
		input,
		mutations.NewHighpass(micHpfCutoff, micHpfQ, micFmt.SampleRate, micFmt.Channels),
		mutations.NewEcho(echoDelay, micFmt.SampleRate, micFmt.Channels, echoFeedback, echoWet),
		mutations.NewReverb(micFmt.SampleRate, micFmt.Channels, reverbRoomSize, reverbDamping, reverbWet),
		mutations.NewGain(micMakeupGain),
	)

	out, err := audioio.OpenOutput(sys, speaker, format)
	if err != nil {
		log.Fatalf("OpenOutput: %v", err)
	}
	defer out.Close()
	outFmt := out.Format()
	log.Printf("speaker negotiated: %dHz %dch frames=%d", outFmt.SampleRate, outFmt.Channels, outFmt.Frames)

	mx, err := mixer.New(mixer.Config{
		SampleRate: outFmt.SampleRate,
		Channels:   outFmt.Channels,
	})
	if err != nil {
		log.Fatalf("mixer.New: %v", err)
	}
	defer mx.Close()
	out.Bind(mx.Fill)

	chordTrack, err := mx.AddSource(chordLoop)
	if err != nil {
		log.Fatalf("add chord: %v", err)
	}
	chordTrack.SetGain(0.2) // sit underneath the melody

	melodyTrack, err := mx.AddSource(melodyLoop)
	if err != nil {
		log.Fatalf("add melody: %v", err)
	}
	melodyTrack.SetGain(0.4)

	micTrack, err := mx.AddSource(micWithFX)
	if err != nil {
		log.Fatalf("add mic: %v", err)
	}
	micTrack.SetGain(1.0)

	if err := out.Start(); err != nil {
		log.Fatalf("out.Start: %v", err)
	}
	if err := input.Start(); err != nil {
		log.Fatalf("input.Start: %v", err)
	}
	log.Printf("karaoke: %q -> %q", mic.Name, speaker.Name)

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
			log.Printf("underruns=%d dropped_mic=%d", mx.Underruns(), input.Dropped())
		}
	}
}
