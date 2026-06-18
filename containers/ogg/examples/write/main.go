// Write example: generate a sine wave, encode it through codec/opus, and
// mux the packets into an Ogg file with metadata.
//
// Usage: write <output.opus>
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"go-mediatoolkit/codec/opus"
	"go-mediatoolkit/consts"
	"go-mediatoolkit/containers"
	"go-mediatoolkit/containers/ogg"
	"go-mediatoolkit/generators"
	opuslib "go-mediatoolkit/libraries/opus"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <output.opus>", os.Args[0])
	}
	path := os.Args[1]

	const (
		sampleRate = opuslib.Rate48000
		channels   = 1
		duration   = 1 * time.Second
	)

	input := generators.Sine(consts.FreqNoteA4, duration, sampleRate)

	out, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	// OpusWriter handles the Ogg framing + OpusHead/OpusTags headers.
	ow, err := ogg.NewOpusWriter(out, channels,
		ogg.WithOpusTags(containers.StandardTags{
			Title:  new("440 Hz tone"),
			Artist: new("go-mediatoolkit"),
			Date:   new("2026-04-22"),
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// codec/opus handles the sample -> packet encoding. The ogg writer
	// satisfies the PacketWriter interface structurally.
	enc, err := opus.NewEncoder(ow, sampleRate, channels,
		opus.WithBitrate(64000),
		opus.WithFrameDuration(20),
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
	if err := ow.Close(); err != nil {
		log.Fatal(err)
	}

	stat, _ := os.Stat(path)
	fmt.Printf("wrote %s (%d bytes, %d samples)\n", path, stat.Size(), len(input.Data))
}
