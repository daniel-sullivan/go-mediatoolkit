// Metadata example: write an ID3v2 tag ahead of an MP3 stream with
// containers/mp3, then read it back and project frames onto the toolkit's
// standard tag set. This exercises the container layer end-to-end; the audio
// frame roundtrip is exercised in the codec tests.
package main

import (
	"bytes"
	"fmt"
	"log"

	"github.com/daniel-sullivan/go-mediatoolkit/containers"
	ctrmp3 "github.com/daniel-sullivan/go-mediatoolkit/containers/mp3"
)

func main() {
	title := "Nightcall"
	artist := "Kavinsky"
	album := "OutRun"

	header := ctrmp3.Header{
		Format:     "mp3",
		SampleRate: 44100,
		Channels:   2,
		Tags: containers.StandardTags{
			Title:  &title,
			Artist: &artist,
			Album:  &album,
		},
	}

	// NewWriter emits the ID3v2 tag derived from header.Tags to the buffer
	// immediately, before any audio frames, and constructs the encoder lazily.
	// We never call Encode here, so this runs on a stock build with no encoder —
	// this example is about the metadata, not the audio codec.
	var buf bytes.Buffer
	if _, err := ctrmp3.NewWriter(&buf, header); err != nil {
		log.Fatal(err)
	}

	// Append a single MPEG frame-sync header so the stream looks like real MP3
	// audio follows the tag (0xFF 0xFB = MPEG-1 Layer III sync).
	buf.Write([]byte{0xFF, 0xFB, 0x90, 0x00})
	fmt.Printf("wrote %d bytes (ID3v2 tag + frame-sync)\n", buf.Len())

	// Read the stream back: NewReader parses the ID3v2 prefix and projects the
	// frames onto containers.StandardTags.
	rd, err := ctrmp3.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		log.Fatal(err)
	}

	tags := rd.Header().Tags
	fmt.Printf("recovered title:  %s\n", deref(tags.Title))
	fmt.Printf("recovered artist: %s\n", deref(tags.Artist))
	fmt.Printf("recovered album:  %s\n", deref(tags.Album))
	fmt.Printf("ID3v2 version:    %d\n", rd.Header().Extra.ID3v2Version)
}

// deref returns the string a tag pointer holds, or "(absent)" if nil.
func deref(s *string) string {
	if s == nil {
		return "(absent)"
	}
	return *s
}
