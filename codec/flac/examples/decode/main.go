// Decode example: turn a native FLAC byte stream into interleaved float64 PCM
// with codec/flac. This teaches one concept — driving the streaming
// codec.Decoder loop — and nothing else: a FLAC stream in, normalized float64
// samples in [-1.0, 1.0] out.
//
// To stay self-contained (no input fixture), this example first encodes a
// generated tone to an in-memory FLAC stream, then decodes it back — the decode
// loop is the point, the encode step just manufactures a real FLAC stream to
// decode. Decoding is always available: cgo libFLAC when CGO_ENABLED=1, the
// bit-exact pure-Go port otherwise.
//
// Usage: decode
package main

import (
	"bytes"
	"io"
	"log"
	"math"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/codec/flac"
	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
)

func main() {
	const (
		sampleRate = consts.SampleRate48000
		channels   = 1
	)

	// Manufacture a real FLAC stream to decode.
	input := generators.Sine(consts.FreqNoteA4, time.Second, sampleRate)
	var stream bytes.Buffer
	enc, err := flac.NewEncoder(&stream, sampleRate, channels,
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

	// The lesson: drive the codec.Decoder loop. SampleRate/Channels are zero
	// until STREAMINFO is parsed (surfaced by the first non-empty Read).
	dec, err := flac.NewDecoder(bytes.NewReader(stream.Bytes()))
	if err != nil {
		log.Fatal(err)
	}

	buf := make([]float64, 4096)
	var total int
	var peak float64
	for {
		audio, err := dec.Read(buf)
		for _, v := range audio.Data {
			if a := math.Abs(v); a > peak {
				peak = a
			}
		}
		total += len(audio.Data)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("decoded %d samples at %d Hz, %d channel(s), peak %.4f",
		total, dec.SampleRate(), dec.Channels(), peak)
}
