// Read example: open a WAV file with the pure-Go RIFF Reader and print its
// parsed header, the sample format recovered from the fmt chunk, and the
// LIST/INFO + broadcast-WAV (bext) metadata projected onto containers.StandardTags.
//
// This teaches one concept — parsing the RIFF chunk tree and inspecting the
// header — without decoding any samples. The whole path is pure Go and MIT:
// containers/wav walks RIFF/fmt/LIST/bext/data and links no third-party code.
//
// Usage: go run . <path.wav>
package main

import (
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strings"

	"go-mediatoolkit/containers/wav"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <path.wav>", os.Args[0])
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Parse the RIFF chunk tree (pure Go, MIT — no decoding here).
	r, err := wav.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}
	h := r.Header()

	fmt.Printf("format:        %s\n", h.Format)
	fmt.Printf("sample rate:   %d Hz\n", h.SampleRate)
	fmt.Printf("channels:      %d\n", h.Channels)
	fmt.Printf("sample format: %s\n", h.SampleFormat)
	fmt.Printf("bits/sample:   %d (fmt tag 0x%04X)\n", h.Extra.BitsPerSample, h.Extra.FormatTag)
	fmt.Printf("bit rate:      %d bps\n", h.BitRate)
	fmt.Printf("duration:      %s\n", h.Duration)

	fmt.Println("tags:")
	tags := h.Tags.Map()
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		for _, v := range tags[k] {
			fmt.Printf("  %-12s = %s\n", strings.ToLower(k), v)
		}
	}

	if b := h.Extra.Bext; b != nil {
		fmt.Println("bext:")
		fmt.Printf("  description = %s\n", b.Description)
		fmt.Printf("  originator  = %s\n", b.Originator)
		fmt.Printf("  coding      = %s\n", b.CodingHistory)
	}
	if len(h.Extra.Cues) > 0 {
		fmt.Printf("cue points:    %d\n", len(h.Extra.Cues))
	}
	if len(h.Extra.Unknown) > 0 {
		fmt.Printf("other chunks:  %d (round-tripped verbatim)\n", len(h.Extra.Unknown))
	}
}
