// Write example: build an MP3 file in memory with an ID3v2 tag via the
// containers/mp3 Writer, encode a tone through the LAME backend, then re-open the
// result and print the recovered tags and stream info.
//
// The Writer projects Header.Tags onto an ID3v2 tag eagerly (always available,
// no LGPL), but the audio encoder is the LAME-derived path: it is fenced behind
// the mp3lame build tag, so in a default build the first Encode surfaces
// ErrEncoderRequiresLAME and this example reports that and exits cleanly. Build
// with `-tags mp3lame` (and CGO_ENABLED=1 for the C oracle backend) to encode a
// real MP3.
//
// Usage: go run . (or: go run -tags mp3lame .)
package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"time"

	"go-mediatoolkit/consts"
	"go-mediatoolkit/containers"
	ctrmp3 "go-mediatoolkit/containers/mp3"
	"go-mediatoolkit/generators"
	mp3lib "go-mediatoolkit/libraries/mp3"
)

func main() {
	const (
		sampleRate = consts.SampleRate44100
		channels   = 1
		duration   = 250 * time.Millisecond
	)

	// Mono tone, converted to the interleaved int16 the Writer encodes.
	mono := generators.Sine(consts.FreqNoteA4, duration, sampleRate)
	pcm := make([]int16, len(mono.Data))
	for i, v := range mono.Data {
		pcm[i] = int16(max(min(v, 1), -1) * 32767)
	}

	header := ctrmp3.Header{
		SampleRate: sampleRate,
		Channels:   channels,
		BitRate:    192000,
		Tags: containers.StandardTags{
			Title:  new("Example Tone"),
			Artist: new("go-mediatoolkit"),
			Album:  new("Examples"),
		},
	}

	var buf bytes.Buffer
	w, err := ctrmp3.NewWriter(&buf, header,
		ctrmp3.WithBitRate(192000), ctrmp3.WithQuality(2))
	if err != nil {
		log.Fatal(err)
	}

	// Encode constructs the LAME encoder lazily; this is where the fence surfaces.
	if err := w.Encode(pcm); err != nil {
		if errors.Is(err, mp3lib.ErrEncoderRequiresLAME) {
			log.Printf("MP3 encode requires the LAME backend; rebuild with -tags mp3lame to run this example")
			return
		}
		log.Fatal(err)
	}
	if err := w.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %d-byte .mp3 file\n", buf.Len())

	// Re-open and inspect.
	rd, err := ctrmp3.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		log.Fatal(err)
	}
	h := rd.Header()
	fmt.Printf("format:        %s\n", h.Format)
	fmt.Printf("sample rate:   %d Hz\n", h.SampleRate)
	fmt.Printf("channels:      %d\n", h.Channels)
	fmt.Printf("ID3v2 version: %d\n", h.Extra.ID3v2Version)
	if h.Tags.Title != nil {
		fmt.Printf("title:         %s\n", *h.Tags.Title)
	}
	if h.Tags.Artist != nil {
		fmt.Printf("artist:        %s\n", *h.Tags.Artist)
	}
}
