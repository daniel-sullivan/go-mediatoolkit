// Speech detection example: run a Detector over a generated signal —
// background noise with a speech-like signal on top — and print the
// SpeechStart/SpeechEnd events as they fire, plus the detector's
// polled state at a few checkpoints. Everything is synthesised and
// processed offline, so this runs headlessly with no audio device.
//
// The -engine flag selects which of the three engines drives the same
// code path below, demonstrating that Gate/Ducker/user code never need
// to care which one they're given. The "speech" is real syllable-like
// bursts and formant sweeps (see the goldensignals import below), not
// a pure tone: WebRTC and Silero are trained models built specifically
// to reject stationary tones/hum (see vad/README.md), so a pure-tone
// signal would fire only the Energy engine and misrepresent the other
// two as never firing at all.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/vad"
	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/goldensignals"
)

func newDetector(engine string, sampleRate int) (vad.Detector, error) {
	switch engine {
	case "energy", "":
		return vad.NewEnergyDetector(vad.EnergyConfig{SampleRate: sampleRate, Channels: 1})
	case "webrtc":
		return vad.NewWebRTCDetector(vad.WebRTCConfig{SampleRate: sampleRate, Channels: 1})
	case "silero":
		return vad.NewSileroDetector(vad.SileroConfig{SampleRate: sampleRate, Channels: 1})
	default:
		return nil, fmt.Errorf("unknown -engine %q (want energy, webrtc, or silero)", engine)
	}
}

func main() {
	engine := flag.String("engine", "energy", "detector engine: energy, webrtc, or silero")
	flag.Parse()

	const (
		sampleRate = consts.SampleRate16000
		chunk      = 160 // 10 ms — a realistic capture callback size
		// goldensignals' speech-pulses signal gaits 1.5 s speech / 0.7 s
		// pause (2.2 s per cycle); 6 s covers three complete utterances
		// (0-1.5s, 2.2-3.7s, 4.4-5.9s).
		duration = 6 * time.Second
	)

	det, err := newDetector(*engine, sampleRate)
	if err != nil {
		log.Fatal(err)
	}

	// Events are published synchronously from Process, back-timestamped
	// to where the speech actually started/stopped (the detector needs
	// ~DecisionLatency to confirm a start, but reports the true onset).
	det.Events().Subscribe(func(ev vad.SpeechEvent) {
		fmt.Printf("%-11s at %7.3fs (frame %6d)\n", ev.Kind, ev.Pos.Seconds(), ev.Frame)
	})

	fmt.Printf("engine: %s, decision latency: %v\n", *engine, det.DecisionLatency())

	// Build the signal: a quiet noise bed (~ -70 dBFS; same deterministic
	// LCG as examples/gate's voice track — see that file for the
	// generator) plus the Silero parity suite's "speech-pulses" golden
	// test signal (vad/internal/goldensignals) — real syllable bursts
	// and sweeping formants over a declining, vibrato'd pitch, rather
	// than a tone this package's own trained engines are built to see
	// through.
	n := int(duration.Seconds() * sampleRate)
	sig := make([]float64, n)
	noise := uint64(1)
	for i := range sig {
		noise = noise*6364136223846793005 + 1442695040888963407 // LCG, deterministic
		sig[i] = (float64(noise>>11)/float64(1<<53) - 0.5) * 2e-3
	}
	for _, s := range goldensignals.Signals() {
		if s.Name != "speech-pulses" {
			continue
		}
		for i := 0; i < n && i < len(s.Samples); i++ {
			sig[i] += float64(s.Samples[i])
		}
	}

	// Feed the detector in 10 ms chunks, as a device callback would,
	// polling the goroutine-safe state at one-second checkpoints.
	for off := 0; off < len(sig); off += chunk {
		det.Process(sig[off : off+chunk])
		if pos := off + chunk; pos%sampleRate == 0 {
			fmt.Printf("  t=%ds poll: active=%-5v probability=%.0f\n",
				pos/sampleRate, det.Active(), det.Probability())
		}
	}

	if last, ok := det.LastTransition(); ok {
		fmt.Printf("last transition: %s at %.3fs\n", last.Kind, last.Pos.Seconds())
	}
}
