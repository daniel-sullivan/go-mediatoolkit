// Encode example: turn interleaved float64 PCM into a stream of Opus packets
// with codec/opus. This teaches one concept — driving the streaming
// codec.Encoder loop over a PacketWriter and flushing it — and nothing else.
//
// Opus has no canonical byte-stream framing, so the encoder emits individual
// packets through a PacketWriter; the caller owns framing (Ogg, WebM, RTP, ...).
// Here the PacketWriter just counts bytes, to keep the focus on the encode loop.
//
// Encoding is always available (no license-fenced build tag), unlike codec/mp3
// (mp3lame) or codec/aac (aacfdk): cgo libopus when CGO_ENABLED=1, the pure-Go
// RFC 6716 port otherwise.
//
// Usage: encode
package main

import (
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/codec/opus"
	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
)

// counter is a PacketWriter that tallies packets and their total size — the
// minimal "framing" sink for an encode-only example.
type counter struct {
	packets int
	bytes   int
}

func (c *counter) WritePacket(data []byte) error {
	c.packets++
	c.bytes += len(data)
	return nil
}

func main() {
	const (
		sampleRate = consts.SampleRate48000
		channels   = 1
		duration   = time.Second
	)

	// A one-second 440 Hz tone is the entire source — Sine returns interleaved
	// float64 in [-1.0, 1.0], the encoder's input type.
	tone := generators.Sine(consts.FreqNoteA4, duration, sampleRate)

	sink := &counter{}
	enc, err := opus.NewEncoder(sink, sampleRate, channels,
		opus.WithBitrate(64000),
		opus.WithFrameDuration(20), // 20 ms per packet
	)
	if err != nil {
		log.Fatal(err)
	}

	// Write may buffer a partial frame internally; Close flushes the trailing
	// (silence-padded) frame. The PacketWriter is not closed.
	if _, err := enc.Write(tone); err != nil {
		log.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		log.Fatal(err)
	}

	rawBytes := len(tone.Data) * 4 // float32-equivalent source size, for context
	log.Printf("encoded %v -> %d Opus packets, %d bytes (%.1fx smaller than raw float32)",
		duration, sink.packets, sink.bytes, float64(rawBytes)/float64(sink.bytes))
}
