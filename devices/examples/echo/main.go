// echo pipes samples from the default input device to the default
// output device via a simple ring buffer. Run with
// `go run ./devices/examples/echo`. Ctrl-C to exit.
//
// Input and output streams run independently on their own backend
// threads; a buffers.Ring (SPSC, lock-free) decouples them. This is
// deliberately the naive approach — no rate conversion, no latency
// compensation, no AEC. If input and output run at different rates
// the buffer slowly drifts; xrun counters print every second so you
// can watch it happen.
//
// The ring carries mono. Backends report each device's native
// channel count regardless of what the format hint asks for, so each
// side converts at its own boundary: stereo mics downmix on the way
// in, stereo speakers upmix on the way out. Only mono/stereo devices
// are supported.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/buffers"
	"github.com/daniel-sullivan/go-mediatoolkit/devices"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

const (
	sampleRate = consts.SampleRate48000

	// Roughly 1s of mono headroom — generous enough to soak up
	// startup jitter without sounding obviously delayed.
	ringFrames = sampleRate
)

func main() {
	sys, err := devices.GetSystem()
	if err != nil {
		log.Fatalf("GetSystem: %v", err)
	}
	defer sys.Close()

	mic, ok := sys.DefaultInput()
	if !ok {
		log.Fatal("no default input device")
	}
	speaker, ok := sys.DefaultOutput()
	if !ok {
		log.Fatal("no default output device")
	}

	ring := buffers.NewRing(ringFrames)
	var underruns, overruns atomic.Uint64

	format := devices.StreamFormat{SampleRate: sampleRate, Channels: 1}

	var (
		inCh, outCh        int
		inScratch, outMono []float64
	)

	// Open output first so the render thread is already waiting for
	// data by the time capture starts producing.
	out, err := sys.OpenOutput(speaker, format, func(samples []float64) {
		mono := samples
		if outCh == 2 {
			mono = outMono[:len(samples)/2]
		}
		n := ring.Read(mono)
		for i := n; i < len(mono); i++ {
			mono[i] = 0
		}
		underruns.Add(uint64(len(mono) - n))
		if outCh == 2 {
			mutations.UpmixMonoToStereo(mono, samples)
		}
	})
	if err != nil {
		log.Fatalf("OpenOutput: %v", err)
	}
	defer out.Close()
	outCh = out.Format().Channels
	if outCh != 1 && outCh != 2 {
		log.Fatalf("output is %dch, only mono/stereo supported", outCh)
	}
	outMono = make([]float64, out.Format().Frames)

	in, err := sys.OpenInput(mic, format, func(samples []float64) {
		mono := samples
		if inCh == 2 {
			mono = inScratch[:len(samples)/2]
			mutations.DownmixStereoToMono(samples, mono)
		}
		n := ring.Write(mono)
		overruns.Add(uint64(len(mono) - n))
	})
	if err != nil {
		log.Fatalf("OpenInput: %v", err)
	}
	defer in.Close()
	inCh = in.Format().Channels
	if inCh != 1 && inCh != 2 {
		log.Fatalf("input is %dch, only mono/stereo supported", inCh)
	}
	inScratch = make([]float64, in.Format().Frames)

	log.Printf("echo %q -> %q (in %dHz/%dch, out %dHz/%dch)",
		mic.Name, speaker.Name,
		in.Format().SampleRate, in.Format().Channels,
		out.Format().SampleRate, out.Format().Channels)

	if err := out.Start(); err != nil {
		log.Fatalf("out.Start: %v", err)
	}
	if err := in.Start(); err != nil {
		log.Fatalf("in.Start: %v", err)
	}

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
			u := underruns.Swap(0)
			o := overruns.Swap(0)
			if u > 0 || o > 0 {
				log.Printf("xruns: underruns=%d overruns=%d", u, o)
			}
		}
	}
}
