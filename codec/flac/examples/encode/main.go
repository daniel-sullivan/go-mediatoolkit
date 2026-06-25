// Encode example: turn interleaved float64 PCM into a native FLAC byte stream
// with codec/flac. This teaches one concept — driving the streaming
// codec.Encoder loop and flushing it — and nothing else.
//
// FLAC is lossless, so the only lossy step here is the float64 → integer
// quantization at the configured bit depth; the FLAC frames themselves preserve
// those integers exactly. Encoding is always available (no license-fenced build
// tag): cgo libFLAC when CGO_ENABLED=1, the bit-exact pure-Go port otherwise.
//
// Usage: encode <output.flac>
package main

import (
	"log"
	"os"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/codec/flac"
	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <output.flac>", os.Args[0])
	}

	const (
		sampleRate = consts.SampleRate44100
		channels   = 1
	)

	// A one-second 440 Hz tone is the entire source — interleaved float64 in
	// [-1.0, 1.0], the encoder's input type.
	tone := generators.Sine(consts.FreqNoteA4, time.Second, sampleRate)

	out, err := os.Create(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	// sampleRate and channels are baked into STREAMINFO; declaring the total
	// sample count up front lets libFLAC finalize the header in one pass.
	enc, err := flac.NewEncoder(out, sampleRate, channels,
		flac.WithBitsPerSample(16),
		flac.WithCompressionLevel(8), // smallest output
		flac.WithTotalSamples(uint64(len(tone.Data))),
		flac.WithTag("TITLE", "A4 sine"),
	)
	if err != nil {
		log.Fatal(err)
	}

	if _, err := enc.Write(tone); err != nil {
		log.Fatal(err)
	}
	// Close flushes the final frame and trailing metadata; it does not close out.
	if err := enc.Close(); err != nil {
		log.Fatal(err)
	}

	info, _ := out.Stat()
	rawBytes := len(tone.Data) * 2 // 16-bit
	log.Printf("encoded %v -> %s (%d bytes, %.1f%% of %d raw bytes)",
		tone.Duration(), os.Args[1], info.Size(),
		float64(info.Size())/float64(rawBytes)*100, rawBytes)
}
