// Streaming example: wire up the codec/mp3 streaming decoder — an io.Reader of
// MP3 bytes in, interleaved float64 in [-1.0, 1.0] out — over an ID3-prefixed
// stream parsed by containers/mp3. This shows the intended pipeline shape:
// containers/mp3.Reader.Data() replays the ID3 prefix + audio frames straight
// into codec/mp3.NewDecoder, which yields mutations.Audio via codec.Decoder.
//
// The decoder is fully wired (cgo minimp3 or the pure-Go port). Generating real
// MP3 frames at runtime would require the LAME encoder (-tags mp3lame), so this
// stock-build example drives the decode loop over a metadata + frame-sync stream
// to demonstrate the wiring; it reports the recovered tag and the decoded sample
// count (zero here, since the synthetic stream carries no real audio payload).
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"

	codecmp3 "go-mediatoolkit/codec/mp3"
	"go-mediatoolkit/containers"
	ctrmp3 "go-mediatoolkit/containers/mp3"
	mp3lib "go-mediatoolkit/libraries/mp3"
)

func main() {
	// Build a tiny ID3v2-prefixed "MP3" stream (tag + a frame-sync header).
	// NewWriter emits the ID3v2 tag eagerly and constructs no encoder, so this
	// runs on a stock build with no mp3lame tag.
	title := "Demo"
	var raw bytes.Buffer
	if _, err := ctrmp3.NewWriter(&raw, ctrmp3.Header{
		Format: "mp3", SampleRate: 44100, Channels: 2,
		Tags: containers.StandardTags{Title: &title},
	}); err != nil {
		log.Fatal(err)
	}
	raw.Write([]byte{0xFF, 0xFB, 0x90, 0x00})

	// Parse the container: rd.Data() yields the ID3 prefix + audio frames as one
	// continuous reader, ready to feed straight into the codec decoder.
	rd, err := ctrmp3.NewReader(bytes.NewReader(raw.Bytes()))
	if err != nil {
		log.Fatal(err)
	}
	if t := rd.Header().Tags.Title; t != nil {
		fmt.Printf("container title: %s\n", *t)
	}

	// Construct the streaming float64 decoder over the container's replay reader.
	dec, err := codecmp3.NewDecoder(rd.Data())
	if err != nil {
		log.Fatal(err)
	}

	// Drive the codec.Decoder loop: Read fills a float64 buffer with interleaved
	// samples until io.EOF.
	buf := make([]float64, mp3lib.MaxSamplesPerFrame*mp3lib.MaxChannels)
	var total int
	for {
		audio, err := dec.Read(buf)
		total += len(audio.Data)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("decoded %d float64 samples (pipeline wiring verified)\n", total)
}
