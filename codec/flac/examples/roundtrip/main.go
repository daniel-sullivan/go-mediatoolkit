// Round-trip example: encode a sine wave through the FLAC codec layer
// to an in-memory byte buffer, then decode it back and report the
// compression ratio along with peak amplitude of the recovered signal.
package main

import (
	"bytes"
	"fmt"
	"log"
	"math"
	"time"

	"go-mediatoolkit/codec"
	"go-mediatoolkit/codec/flac"
	"go-mediatoolkit/consts"
	"go-mediatoolkit/generators"
)

func main() {
	const (
		sampleRate = consts.SampleRate48000
		channels   = 1
		duration   = 100 * time.Millisecond
	)

	input := generators.Sine(consts.FreqNoteA4, duration, sampleRate)

	var buf bytes.Buffer
	enc, err := flac.NewEncoder(&buf, sampleRate, channels,
		flac.WithBitsPerSample(16),
		flac.WithCompressionLevel(5),
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

	rawBytes := len(input.Data) * 2 // 16-bit
	encodedBytes := buf.Len()
	fmt.Printf("encoded %d samples (%d raw bytes) -> %d FLAC bytes (%.1f%% of raw)\n",
		len(input.Data), rawBytes, encodedBytes,
		float64(encodedBytes)/float64(rawBytes)*100)

	dec, err := flac.NewDecoder(bytes.NewReader(buf.Bytes()))
	if err != nil {
		log.Fatal(err)
	}

	output := make([]float64, len(input.Data))
	got, _ := codec.ReadFull(dec, output)

	var peak float64
	for _, v := range got.Data {
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	fmt.Printf("decoded %d samples, peak amplitude %.4f\n", len(got.Data), peak)
}
