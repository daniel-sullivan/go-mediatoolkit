// Decode example: turn a native MP3 byte stream into interleaved float64 PCM
// with codec/mp3. This teaches one concept — driving the streaming
// codec.Decoder loop — and nothing else: an MP3 file in, normalized float64
// samples in [-1.0, 1.0] out.
//
// The decoder is the MIT/public-domain minimp3 path (cgo when available, the
// pure-Go 1:1 port otherwise); decoding never requires the LGPL mp3lame tag.
//
// Usage: decode <input.mp3>
package main

import (
	"io"
	"log"
	"os"

	codecmp3 "github.com/daniel-sullivan/go-mediatoolkit/codec/mp3"
	mp3lib "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <input.mp3>", os.Args[0])
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// NewDecoder reads a continuous MP3 frame stream and yields interleaved
	// float64 samples. SampleRate / Channels are zero until the first frame
	// header is parsed (surfaced by the first non-empty Read).
	dec, err := codecmp3.NewDecoder(f)
	if err != nil {
		log.Fatal(err)
	}

	// Drive the codec.Decoder loop: Read fills a float64 buffer with interleaved
	// samples until io.EOF. Size the buffer to one worst-case frame so each Read
	// can deliver a whole frame at once.
	buf := make([]float64, mp3lib.MaxSamplesPerFrame*mp3lib.MaxChannels)
	var totalSamples int
	for {
		audio, err := dec.Read(buf)
		totalSamples += len(audio.Data)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
	}

	channels := dec.Channels()
	frames := 0
	if channels > 0 {
		frames = totalSamples / channels
	}
	log.Printf("decoded %d frames (%d samples) at %d Hz, %d channel(s)",
		frames, totalSamples, dec.SampleRate(), channels)
}
