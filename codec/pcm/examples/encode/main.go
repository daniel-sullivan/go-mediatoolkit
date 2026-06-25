// Encode example: turn interleaved float64 PCM into raw, headerless int16 PCM
// bytes with codec/pcm. This teaches one concept — driving the streaming
// codec.Encoder loop and flushing it — and nothing else.
//
// Raw PCM has no header, so the output is a flat run of sample bytes in the
// format/rate/channels baked into the encoder; a consumer must be told those
// out of band (or wrap the stream in containers/wav).
//
// Usage: encode
package main

import (
	"bytes"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/codec/pcm"
	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func main() {
	const (
		sampleRate = consts.SampleRate44100
		channels   = 1
		format     = mutations.FormatInt16
	)

	// A 250 ms 440 Hz tone is the source — interleaved float64 in [-1.0, 1.0].
	tone := generators.Sine(consts.FreqNoteA4, 250*time.Millisecond, sampleRate)

	var out bytes.Buffer
	enc, err := pcm.NewEncoder(&out, sampleRate, channels, format)
	if err != nil {
		log.Fatal(err)
	}

	// Write scales float64 to the target width, saturating values past ±1.0.
	// Close flushes any buffered bytes; it does not close the underlying writer.
	if _, err := enc.Write(tone); err != nil {
		log.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		log.Fatal(err)
	}

	log.Printf("encoded %d float64 samples -> %d bytes of raw %s PCM (%d Hz, %d ch)",
		len(tone.Data), out.Len(), "int16 LE", sampleRate, channels)
}
