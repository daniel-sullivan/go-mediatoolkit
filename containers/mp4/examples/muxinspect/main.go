// Mux + inspect example: write a sine wave into an in-memory .m4a file with
// iTunes tags via the MP4 Writer, then re-open it with the MP4 Reader and print
// the parsed header, tags, and AAC access-unit count.
//
// The container layer is pure Go: it assembles the ftyp / moov (esds +
// stsz/stsc/stco/stts) / mdat box tree and projects Header.Tags onto an ilst
// box. The AAC bitstream itself comes from codec/aac, whose FDK-AAC engine is
// fenced behind the aacfdk build tag — so in a default build WriteAudio
// surfaces ErrEngineRequiresFDK and this example reports that and exits
// cleanly. Build with `-tags aacfdk` to mux a real .m4a.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"time"

	"go-mediatoolkit/consts"
	"go-mediatoolkit/containers"
	"go-mediatoolkit/containers/mp4"
	"go-mediatoolkit/generators"
	aaclib "go-mediatoolkit/libraries/aac"
)

func main() {
	const (
		sampleRate = consts.SampleRate44100
		channels   = 2
		duration   = 250 * time.Millisecond
	)

	// Mono source up-mixed to stereo interleaving for the writer.
	mono := generators.Sine(consts.FreqNoteA4, duration, sampleRate)
	stereo := make([]float64, len(mono.Data)*channels)
	for i, v := range mono.Data {
		stereo[i*channels] = v
		stereo[i*channels+1] = v
	}

	str := func(s string) *string { return &s }
	header := mp4.Header{
		SampleRate: sampleRate,
		Channels:   channels,
		Tags: containers.StandardTags{
			Title:  str("Example Tone"),
			Artist: str("go-mediatoolkit"),
			Album:  str("Examples"),
		},
	}

	var buf bytes.Buffer
	w, err := mp4.NewWriter(&buf, header, mp4.WithBitrate(128000))
	if err != nil {
		log.Fatal(err)
	}
	if err := w.WriteAudio(stereo); err != nil {
		if errors.Is(err, aaclib.ErrEngineRequiresFDK) {
			log.Printf("AAC encode requires the FDK engine; rebuild with -tags aacfdk to run this example")
			return
		}
		log.Fatal(err)
	}
	if err := w.Close(); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("wrote %d-byte .m4a file\n", buf.Len())

	// Re-open and inspect.
	rd, err := mp4.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		log.Fatal(err)
	}
	h := rd.Header()
	fmt.Printf("format:       %s\n", h.Format)
	fmt.Printf("major brand:  %s\n", h.Extra.MajorBrand)
	fmt.Printf("sample rate:  %d Hz\n", h.SampleRate)
	fmt.Printf("channels:     %d\n", h.Channels)
	fmt.Printf("duration:     %s\n", h.Duration)
	fmt.Printf("object type:  %s\n", h.Extra.Config.ObjectType)
	fmt.Printf("access units: %d\n", len(rd.AccessUnits()))

	fmt.Println("tags:")
	tags := h.Tags.Map()
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		for _, v := range tags[k] {
			fmt.Printf("  %-8s = %s\n", strings.ToLower(k), v)
		}
	}
}
