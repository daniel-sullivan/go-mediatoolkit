// Inspect example: encode packets and inspect their headers without decoding.
package main

import (
	"fmt"
	"log"
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus"
)

func main() {
	frameSamples := opus.SamplesPerFrame(20, opus.Rate48000)
	pcm := make([]float64, frameSamples)
	for i := range pcm {
		pcm[i] = 0.3 * math.Sin(2*math.Pi*1000*float64(i)/float64(opus.Rate48000))
	}

	// Encode with different configurations and inspect the resulting packets.
	configs := []struct {
		name string
		opts []opus.EncoderOption
	}{
		{"audio 64kbps", []opus.EncoderOption{
			opus.WithApplication(opus.AppAudio),
			opus.WithBitrate(64000),
		}},
		{"audio 24kbps", []opus.EncoderOption{
			opus.WithApplication(opus.AppAudio),
			opus.WithBitrate(24000),
		}},
		{"voip 16kbps", []opus.EncoderOption{
			opus.WithApplication(opus.AppVoIP),
			opus.WithBitrate(16000),
		}},
		{"low-delay 96kbps", []opus.EncoderOption{
			opus.WithApplication(opus.AppLowDelay),
			opus.WithBitrate(96000),
		}},
	}

	for _, cfg := range configs {
		enc, err := opus.NewEncoder(opus.Rate48000, 1, cfg.opts...)
		if err != nil {
			log.Fatal(err)
		}

		pkt, err := enc.Encode(pcm, opus.MaxFrameBytes)
		if err != nil {
			log.Fatalf("%s: encode: %v", cfg.name, err)
		}

		info, err := opus.ParsePacket(pkt)
		if err != nil {
			log.Fatalf("%s: parse: %v", cfg.name, err)
		}

		fmt.Printf("%-20s  %4d bytes  mode=%-6s  bw=%-14s  dur=%5.1fms  frames=%d  stereo=%v\n",
			cfg.name, len(pkt),
			info.Mode, info.Bandwidth,
			info.FrameDuration, info.FrameCount, info.Stereo)
	}
}
