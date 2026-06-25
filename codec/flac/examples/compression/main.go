// Compression example: FLAC's signature strength is lossless compression with a
// speed/size knob. This encodes the *same* signal at every compression level
// (0 = fastest, 8 = smallest) and reports the resulting FLAC size, so the
// size/level trade-off is visible at a glance. Because FLAC is lossless, every
// level decodes back to the identical samples — only the encode effort (and
// thus the file size) differs.
//
// A pink-noise + tone mix is used rather than a pure sine: a pure tone is so
// trivially predictable that all levels collapse to nearly the same size, which
// would hide the trade-off this example is about.
//
// Usage: compression
package main

import (
	"bytes"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/codec/flac"
	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
)

func main() {
	const (
		sampleRate = consts.SampleRate44100
		channels   = 1
		duration   = time.Second
	)

	// A tone plus low-amplitude pink noise: compressible but not trivial, so the
	// compression levels actually diverge.
	tone := generators.Sine(consts.FreqNoteA4, duration, sampleRate)
	noise := generators.PinkNoise(duration, sampleRate, 1)
	mix := make([]float64, len(tone.Data))
	for i := range mix {
		mix[i] = 0.7*tone.Data[i] + 0.2*noise.Data[i]
	}
	input := tone
	input.Data = mix

	rawBytes := len(input.Data) * 2 // 16-bit
	log.Printf("source: %d samples, %d raw bytes (16-bit)", len(input.Data), rawBytes)

	for level := 0; level <= 8; level++ {
		var out bytes.Buffer
		enc, err := flac.NewEncoder(&out, sampleRate, channels,
			flac.WithBitsPerSample(16),
			flac.WithCompressionLevel(level),
			flac.WithTotalSamples(uint64(len(input.Data))),
		)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := enc.Write(input); err != nil {
			log.Fatal(err)
		}
		if err := enc.Close(); err != nil {
			log.Fatal(err)
		}
		log.Printf("level %d: %6d FLAC bytes (%.1f%% of raw)",
			level, out.Len(), float64(out.Len())/float64(rawBytes)*100)
	}
}
