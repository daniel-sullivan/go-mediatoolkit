// PLC example: demonstrate packet loss concealment by simulating dropped frames.
package main

import (
	"fmt"
	"log"
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus"
)

func main() {
	const (
		sampleRate = opus.Rate48000
		channels   = 1
		numFrames  = 10
	)
	frameSamples := opus.SamplesPerFrame(20, sampleRate)

	enc, err := opus.NewEncoder(sampleRate, channels,
		opus.WithApplication(opus.AppAudio),
		opus.WithBitrate(64000),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Pre-encode all frames.
	pcm := make([]float64, frameSamples)
	packets := make([][]byte, numFrames)
	for f := 0; f < numFrames; f++ {
		offset := f * frameSamples
		for i := 0; i < frameSamples; i++ {
			t := float64(offset+i) / float64(sampleRate)
			pcm[i] = 0.5 * math.Sin(2*math.Pi*440*t)
		}
		pkt, err := enc.Encode(pcm, opus.MaxFrameBytes)
		if err != nil {
			log.Fatal(err)
		}
		packets[f] = pkt
	}

	// Simulate decoding with packet loss on frames 4, 5, 6.
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		log.Fatal(err)
	}

	pcmOut := make([]float64, opus.MaxFrameSize(sampleRate))
	lostFrames := map[int]bool{4: true, 5: true, 6: true}

	for f := 0; f < numFrames; f++ {
		var samples int
		var err error

		if lostFrames[f] {
			// Pass nil to trigger PLC.
			samples, err = dec.Decode(nil, pcmOut)
		} else {
			samples, err = dec.Decode(packets[f], pcmOut)
		}
		if err != nil {
			log.Fatalf("frame %d: %v", f, err)
		}

		// Compute RMS of the output frame.
		rms := 0.0
		for i := 0; i < samples*channels; i++ {
			rms += pcmOut[i] * pcmOut[i]
		}
		rms = math.Sqrt(rms / float64(samples*channels))

		status := "ok"
		if lostFrames[f] {
			status = "LOST (PLC)"
		}
		fmt.Printf("frame %2d: %d samples, rms=%.4f  %s\n", f, samples, rms, status)
	}

	fmt.Println()
	fmt.Println("Note: PLC attenuates output by ~3dB per consecutive lost frame.")
	fmt.Println("After receiving a real packet, the decoder recovers.")
}
