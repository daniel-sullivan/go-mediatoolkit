// play renders a 440 Hz sine wave to the default output device for a
// fixed duration. Run with `go run ./devices/examples/play`.
//
// The audio callback is invoked on a backend-owned realtime-ish thread
// and must not allocate or block — here we just advance a phase
// counter and compute sin per sample. The stream's negotiated format
// is printed on Start so you can see what the OS actually gave us
// (shared-mode Windows/Linux frequently force 48 kHz).
package main

import (
	"log"
	"math"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/devices"
)

func main() {
	sys, err := devices.GetSystem()
	if err != nil {
		log.Fatalf("GetSystem: %v", err)
	}
	defer sys.Close()

	out, ok := sys.DefaultOutput()
	if !ok {
		log.Fatal("no default output device")
	}

	const freq = 440.0

	// inc is set to the per-sample phase step once the stream reports
	// its negotiated sample rate; the callback reads through a closure
	// so the callback body itself stays allocation-free.
	var (
		phase    float64
		inc      float64
		channels int
	)
	cb := func(buf []float64) {
		if inc == 0 {
			return
		}
		frames := len(buf) / channels
		for i := 0; i < frames; i++ {
			v := math.Sin(phase) * 0.2
			for c := 0; c < channels; c++ {
				buf[i*channels+c] = v
			}
			phase += inc
			if phase > 2*math.Pi {
				phase -= 2 * math.Pi
			}
		}
	}

	stream, err := sys.OpenOutput(out, devices.StreamFormat{SampleRate: 48000, Channels: 2}, cb)
	if err != nil {
		log.Fatalf("OpenOutput: %v", err)
	}
	defer stream.Close()

	actual := stream.Format()
	channels = actual.Channels
	inc = 2 * math.Pi * freq / float64(actual.SampleRate)
	log.Printf("playing %gHz on %q: rate=%d channels=%d frames=%d",
		freq, out.Name, actual.SampleRate, actual.Channels, actual.Frames)

	if err := stream.Start(); err != nil {
		log.Fatalf("Start: %v", err)
	}
	time.Sleep(3 * time.Second)
	if err := stream.Stop(); err != nil {
		log.Fatalf("Stop: %v", err)
	}
}
