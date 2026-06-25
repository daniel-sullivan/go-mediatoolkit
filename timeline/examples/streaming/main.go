// streaming simulates an Opus-style streaming service: a producer
// goroutine "receives" a 20 ms audio packet every 20 ms (wall clock)
// and appends it to a StreamingTimeline; the mixer drains the
// timeline to the default output device.
//
// Two modes are demonstrated via the keepHistory flag:
//
//   - keepHistory=false (realtime-only): played segments are evicted
//     as soon as the cursor passes them. Memory stays bounded to
//     ~2 packets while the stream plays forever. Seeking backward
//     is not supported — the history simply isn't there.
//
//   - keepHistory=true: every appended packet is retained. The
//     example performs a Seek(-3s) after 10 seconds to demonstrate
//     scrubbing back through history. Memory grows linearly with
//     stream length; real callers should eventually Close or cap
//     their own producer.
//
// Run with `go run ./timeline/examples/streaming`; Ctrl-C to exit.
//
// Requires a default audio output device; runs for -run (default 30s)
// or until Ctrl-C.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	hpt "github.com/daniel-sullivan/go-hpt"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/devices"
	"github.com/daniel-sullivan/go-mediatoolkit/mixer"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/timeline"
)

const (
	sampleRate = consts.SampleRate48000
	channels   = 1
	frames     = 256

	// Packet cadence — matches the Opus default frame duration.
	packetInterval = 20 * time.Millisecond
	packetFrames   = int(sampleRate * packetInterval / time.Second) // 960 frames at 48 kHz
)

var (
	keepHistory = flag.Bool("history", true, "retain played packets and seek back after 10s")
	duration    = flag.Duration("run", 30*time.Second, "total run time before exiting")
)

func main() {
	flag.Parse()

	sys, err := devices.GetSystem()
	if err != nil {
		log.Fatalf("GetSystem: %v", err)
	}
	defer sys.Close()
	speaker, ok := sys.DefaultOutput()
	if !ok {
		log.Fatal("no default output device")
	}

	stream, err := timeline.NewTimelineWith(timeline.Config{
		SampleRate:  sampleRate,
		Channels:    channels,
		KeepHistory: *keepHistory,
	})
	if err != nil {
		log.Fatalf("NewTimeline: %v", err)
	}
	defer stream.Close()

	mx, err := mixer.New(mixer.Config{
		SampleRate:  sampleRate,
		Channels:    channels,
		ChunkFrames: frames,
		// Small ring — a streaming timeline can't race ahead of its
		// producer anyway, so a big mix buffer would just add latency.
		RingFrames: sampleRate / 20, // 50ms
	})
	if err != nil {
		log.Fatalf("mixer.New: %v", err)
	}
	defer mx.Close()
	if _, err := mx.AddSource(stream); err != nil {
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
	log.Printf("streaming → %q (keepHistory=%v, packet=%dms)", speaker.Name, *keepHistory, packetInterval/time.Millisecond)

	// Producer: every 20 ms, generate a 20 ms chunk of a slow sweep
	// and Append it. Using hpt so wake-up jitter doesn't accumulate
	// into buffer underruns.
	stopProducer := make(chan struct{})
	producerDone := make(chan struct{})
	go runProducer(stream, stopProducer, producerDone)

	// Optional seek-back demonstration.
	var seekTimer *time.Timer
	if *keepHistory {
		seekTimer = time.AfterFunc(10*time.Second, func() {
			if err := stream.Seek(-3 * time.Second); err != nil {
				log.Printf("seek: %v", err)
				return
			}
			log.Printf("seeked -3s (position=%s)", stream.Position())
		})
	}

	// Stats ticker.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	deadline := time.After(*duration)

loop:
	for {
		select {
		case <-sigs:
			break loop
		case <-deadline:
			break loop
		case <-tick.C:
			pos := stream.Position()
			sched := stream.ScheduledDuration()
			log.Printf("t=%s  buffered=%s  underruns=%d",
				pos.Round(time.Millisecond),
				(sched - pos).Round(time.Millisecond),
				mx.Underruns())
		}
	}
	fmt.Println()

	if seekTimer != nil {
		seekTimer.Stop()
	}
	close(stopProducer)
	<-producerDone
}

// runProducer emits one packetFrames-long chunk every packetInterval.
// The chunk content is a continuous slow-sweep sine so the audible
// stream sounds like a single long tone rather than buffered steps.
func runProducer(stream *timeline.Timeline, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	buf := make([]float64, packetFrames)

	// Phase accumulator keeps the sine continuous across packet
	// boundaries — the most common streaming bug is clicks at the
	// seam between packets.
	phase := 0.0
	startTime := time.Now()

	next := time.Now().Add(packetInterval)
	for {
		select {
		case <-stop:
			return
		case <-hpt.After(time.Until(next)):
		}
		next = next.Add(packetInterval)

		elapsed := time.Since(startTime).Seconds()
		// 200 → 800 Hz sweep across 30s.
		freq := 200 + 600*math.Min(1, elapsed/30)
		dPhase := 2 * math.Pi * freq / float64(sampleRate)
		for i := range buf {
			buf[i] = 0.25 * math.Sin(phase)
			phase += dPhase
			if phase > 2*math.Pi {
				phase -= 2 * math.Pi
			}
		}
		audio := mutations.Audio{Data: buf, SampleRate: sampleRate, Channels: channels}
		if _, err := stream.AppendAudio(audio); err != nil {
			log.Printf("append: %v", err)
			return
		}
	}
}
