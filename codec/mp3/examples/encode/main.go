//go:build mp3lame

// Encode example: turn interleaved float64 PCM into a native MP3 byte stream
// with codec/mp3. This teaches one concept — driving the streaming
// codec.Encoder loop and flushing it — and nothing else.
//
// The MP3 encoder is derived from LAME and is LGPL-licensed, so it is fenced
// behind the mp3lame build tag (see LICENSING.md). This example therefore
// carries //go:build mp3lame and only compiles into a build that has opted in:
//
//	go run -tags mp3lame ./codec/mp3/examples/encode out.mp3
//
// Without the tag, codec/mp3.NewEncoder returns mp3: encoder requires the
// mp3lame build tag (LGPL); decoding (minimp3, MIT) is always available.
//
// Usage: encode <output.mp3>
package main

import (
	"log"
	"os"
	"time"

	codecmp3 "go-mediatoolkit/codec/mp3"
	"go-mediatoolkit/generators"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <output.mp3>", os.Args[0])
	}

	const sampleRate = 44100
	const channels = 1

	// A one-second 440 Hz sine is the entire audio source — Sine returns a
	// mutations.Audio of interleaved float64 in [-1.0, 1.0], the encoder's
	// input type.
	tone := generators.Sine(440, time.Second, sampleRate)

	out, err := os.Create(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	// NewEncoder bakes sampleRate and channels into the stream; every Write
	// must supply an Audio whose SampleRate / Channels match.
	enc, err := codecmp3.NewEncoder(out, sampleRate, channels)
	if err != nil {
		log.Fatal(err)
	}

	if _, err := enc.Write(tone); err != nil {
		log.Fatal(err)
	}

	// Close flushes the encoder's trailing frames; it does not close out.
	if err := enc.Close(); err != nil {
		log.Fatal(err)
	}

	log.Printf("encoded %s of audio to %s", tone.Duration(), os.Args[1])
}
