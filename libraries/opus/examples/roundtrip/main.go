// Round-trip example: encode a sine wave to Opus, then decode it back.
package main

import (
	"fmt"
	"log"
	"math"

	"go-mediatoolkit/libraries/opus"
)

func main() {
	const (
		sampleRate = opus.Rate48000
		channels   = 1
		frameMs    = 20
		frequency  = 440.0 // Hz
	)
	frameSamples := opus.SamplesPerFrame(frameMs, sampleRate)

	enc, err := opus.NewEncoder(sampleRate, channels,
		opus.WithBitrate(64000),
		opus.WithApplication(opus.AppAudio),
	)
	if err != nil {
		log.Fatal(err)
	}

	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		log.Fatal(err)
	}

	// Generate 5 frames of a 440 Hz sine wave (100ms total).
	const numFrames = 5
	pcmIn := make([]float64, frameSamples)
	pcmOut := make([]float64, opus.MaxFrameSize(sampleRate))

	for f := 0; f < numFrames; f++ {
		// Fill frame with sine wave.
		offset := f * frameSamples
		for i := 0; i < frameSamples; i++ {
			t := float64(offset+i) / float64(sampleRate)
			pcmIn[i] = 0.5 * math.Sin(2*math.Pi*frequency*t)
		}

		// Encode.
		packet, err := enc.Encode(pcmIn, opus.MaxFrameBytes)
		if err != nil {
			log.Fatalf("encode frame %d: %v", f, err)
		}

		// Decode.
		samples, err := dec.Decode(packet, pcmOut)
		if err != nil {
			log.Fatalf("decode frame %d: %v", f, err)
		}

		// Compute peak amplitude of decoded frame.
		peak := 0.0
		for i := 0; i < samples; i++ {
			if a := math.Abs(pcmOut[i]); a > peak {
				peak = a
			}
		}

		fmt.Printf("frame %d: %d bytes -> %d samples, peak=%.4f\n",
			f, len(packet), samples, peak)
	}
}
