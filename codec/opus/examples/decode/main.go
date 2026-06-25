// Decode example: turn a stream of Opus packets into interleaved float64 PCM
// with codec/opus. This teaches one concept — driving the streaming
// codec.Decoder loop over a PacketReader — and nothing else.
//
// Opus has no canonical byte-stream framing, so the decoder reads individual
// packets through a PacketReader. To stay self-contained (no input fixture),
// this example first encodes a generated tone to packets, then feeds those
// packets back through NewSlicePacketReader and decodes them — the decode loop
// is the point, the encode step just manufactures real packets to decode.
//
// Decoding is always available (no license-fenced build tag): cgo libopus when
// CGO_ENABLED=1, the pure-Go RFC 6716 port otherwise.
//
// Usage: decode
package main

import (
	"io"
	"log"
	"math"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/codec/opus"
	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
)

func main() {
	const (
		sampleRate = consts.SampleRate48000
		channels   = 1
	)

	// Manufacture real Opus packets to decode: encode a one-second tone and
	// collect every emitted packet.
	var packets [][]byte
	enc, err := opus.NewEncoder(
		opus.PacketWriterFunc(func(pkt []byte) error {
			packets = append(packets, append([]byte(nil), pkt...))
			return nil
		}),
		sampleRate, channels,
	)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := enc.Write(generators.Sine(consts.FreqNoteA4, time.Second, sampleRate)); err != nil {
		log.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		log.Fatal(err)
	}

	// The actual lesson: drive the codec.Decoder loop. NewSlicePacketReader is a
	// PacketReader over the in-memory packets; Read fills a float64 buffer with
	// interleaved samples in [-1.0, 1.0] until io.EOF.
	dec, err := opus.NewDecoder(opus.NewSlicePacketReader(packets), sampleRate, channels)
	if err != nil {
		log.Fatal(err)
	}

	buf := make([]float64, 4096)
	var totalSamples int
	var peak float64
	for {
		audio, err := dec.Read(buf)
		for _, v := range audio.Data {
			if a := math.Abs(v); a > peak {
				peak = a
			}
		}
		totalSamples += len(audio.Data)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
	}

	frames := totalSamples / dec.Channels()
	log.Printf("decoded %d frames (%d samples) at %d Hz, %d channel(s), peak %.4f",
		frames, totalSamples, dec.SampleRate(), dec.Channels(), peak)
}
