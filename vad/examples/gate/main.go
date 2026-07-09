// Speech gate example: clean background noise out of a voice
// recording. A generated "voice track" — constant room noise with two
// tone utterances on top — is run through a Gate whose lookahead is
// set to the detector's DecisionLatency, so the gate is already open
// when each utterance emerges. The example prints the RMS level of the
// speech and the gaps before and after gating: speech passes intact,
// gaps drop to (near) silence. Fully offline, no audio device.
package main

import (
	"fmt"
	"log"
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/vad"
)

func main() {
	const (
		sampleRate = consts.SampleRate16000
		chunk      = 160 // 10 ms
	)

	det, err := vad.NewEnergyDetector(vad.EnergyConfig{
		SampleRate: sampleRate,
		Channels:   1,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Lookahead = DecisionLatency delays the AUDIO by exactly the time
	// the detector needs to confirm an onset, so the gate opens before
	// the first syllable reaches the output. The gate feeds the
	// detector itself — don't insert det anywhere else.
	gate, err := vad.NewGate(vad.GateConfig{
		SampleRate: sampleRate,
		Channels:   1,
		Detector:   det,
		Lookahead:  det.DecisionLatency(),
		FloorDB:    -60, // not a full mute: leave a hint of room tone
	})
	if err != nil {
		log.Fatal(err)
	}

	// 3 s of "recording": room noise at ~-58 dBFS RMS with utterances
	// (a 300 Hz + 1.1 kHz mix at -20 dBFS) at 0.5-1.2 s and 1.8-2.4 s.
	sig := make([]float64, 3*sampleRate)
	noise := uint64(7)
	for i := range sig {
		noise = noise*6364136223846793005 + 1442695040888963407 // LCG, deterministic (same generator as examples/detect)
		sig[i] = (float64(noise>>11)/float64(1<<53) - 0.5) * 8e-3
	}
	spans := [][2]int{
		{sampleRate / 2, sampleRate * 12 / 10},
		{sampleRate * 18 / 10, sampleRate * 24 / 10},
	}
	for _, s := range spans {
		for i := s[0]; i < s[1]; i++ {
			ph := 2 * math.Pi * float64(i) / sampleRate
			sig[i] += 0.07*math.Sin(300*ph) + 0.07*math.Sin(1100*ph)
		}
	}

	// Gate a copy in 10 ms chunks (Process is in-place).
	gated := make([]float64, len(sig))
	copy(gated, sig)
	for off := 0; off < len(gated); off += chunk {
		gate.Process(gated[off : off+chunk])
	}

	// Compare levels region by region. The gated output is delayed by
	// gate.Latency(), so measure it at shifted positions.
	shift := gate.LatencyFrames()
	rms := func(buf []float64, from, to int) float64 {
		sum := 0.0
		for i := from; i < to; i++ {
			sum += buf[i] * buf[i]
		}
		// Same regularising epsilon as vad's internal energyEpsilon
		// (1e-12) — kept in sync so this printed floor reads the same
		// as what the Energy engine itself sees.
		return 10 * math.Log10(sum/float64(to-from)+1e-12)
	}
	regions := []struct {
		name     string
		from, to int
	}{
		{"gap 0.0-0.4s   ", 0, sampleRate * 4 / 10},
		{"speech 0.6-1.1s", sampleRate * 6 / 10, sampleRate * 11 / 10},
		{"gap 1.4-1.7s   ", sampleRate * 14 / 10, sampleRate * 17 / 10},
		{"speech 1.9-2.3s", sampleRate * 19 / 10, sampleRate * 23 / 10},
		{"gap 2.6-2.9s   ", sampleRate * 26 / 10, sampleRate * 29 / 10},
	}
	fmt.Printf("gate lookahead/latency: %v  (floor %.0f dB)\n\n", gate.Latency(), -60.0)
	fmt.Println("region             dry RMS      gated RMS")
	for _, r := range regions {
		fmt.Printf("%s  %7.1f dBFS  %7.1f dBFS\n",
			r.name, rms(sig, r.from, r.to), rms(gated, r.from+shift, r.to+shift))
	}
}
