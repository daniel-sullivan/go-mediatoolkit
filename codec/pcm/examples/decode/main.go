// Decode example: turn raw, headerless int16 PCM bytes into interleaved float64
// samples with codec/pcm. This teaches one concept — driving the streaming
// codec.Decoder loop over a byte source whose format is supplied out of band
// (raw PCM has no header) — and nothing else.
//
// To stay self-contained, the input bytes are synthesized here (a tone encoded
// to int16 LE); a real program would read them from a file or socket. The
// sample format, rate, and channel count are passed to NewDecoder because the
// stream itself carries none of that.
//
// Usage: decode
package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"math"
	"time"

	"go-mediatoolkit/codec/pcm"
	"go-mediatoolkit/consts"
	"go-mediatoolkit/generators"
	"go-mediatoolkit/mutations"
)

func main() {
	const (
		sampleRate = consts.SampleRate44100
		channels   = 1
		format     = mutations.FormatInt16
	)

	// Synthesize the "input file": a 100 ms tone encoded to int16 little-endian.
	samples := generators.Sine(consts.FreqNoteA4, 100*time.Millisecond, sampleRate).Data
	srcBytes := make([]byte, format.BytesPerSample()*len(samples))
	mutations.EncodeSamples(samples, srcBytes, format, binary.LittleEndian)

	// The lesson: drive the codec.Decoder loop. The format/rate/channels are
	// supplied at construction — there is no header to discover them from.
	dec, err := pcm.NewDecoder(bytes.NewReader(srcBytes), sampleRate, channels, format)
	if err != nil {
		log.Fatal(err)
	}

	buf := make([]float64, 1024)
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
