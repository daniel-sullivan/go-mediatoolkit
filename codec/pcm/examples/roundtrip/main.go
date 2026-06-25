// Round-trip example: encode float64 samples to int16 PCM bytes,
// then decode them back.
package main

import (
	"bytes"
	"fmt"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/codec/pcm"
	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func main() {
	const (
		sampleRate = consts.SampleRate48000
		channels   = 1
		freq       = consts.FreqNoteA4
	)

	input := generators.Sine(freq, 100*time.Millisecond, sampleRate)

	var buf bytes.Buffer
	enc, err := pcm.NewEncoder(&buf, sampleRate, channels, mutations.FormatInt16)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := enc.Write(input); err != nil {
		log.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("encoded %d samples -> %d bytes (int16 LE)\n", len(input.Data), buf.Len())

	dec, err := pcm.NewDecoder(&buf, sampleRate, channels, mutations.FormatInt16)
	if err != nil {
		log.Fatal(err)
	}

	output := make([]float64, len(input.Data))
	decoded, _ := dec.Read(output)
	fmt.Printf("decoded %d samples at %d Hz (%d channels)\n", len(decoded.Data), decoded.SampleRate, decoded.Channels)

	var peak float64
	for _, v := range decoded.Data {
		if v > peak {
			peak = v
		} else if -v > peak {
			peak = -v
		}
	}
	fmt.Printf("peak amplitude after round-trip: %.4f\n", peak)
}
