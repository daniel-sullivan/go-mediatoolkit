// Read example: open a native FLAC file with the pure-Go Reader and print the
// STREAMINFO block, the VORBIS_COMMENT vendor + tags projected onto
// containers.StandardTags, and a summary of the remaining metadata blocks.
//
// This teaches one concept — parsing the fLaC magic + metadata chain and
// inspecting the header — without decoding any audio frames. The container
// layer is pure Go and MIT; it links no third-party code.
//
// Usage: go run . <path.flac>
package main

import (
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strings"

	flac "github.com/daniel-sullivan/go-mediatoolkit/containers/flac"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <path.flac>", os.Args[0])
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Parse the fLaC magic + metadata chain (pure Go, MIT — no decoding here).
	r, err := flac.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}
	h := r.Header()
	si := h.Extra.StreamInfo

	fmt.Printf("format:        %s\n", h.Format)
	fmt.Printf("sample rate:   %d Hz\n", h.SampleRate)
	fmt.Printf("channels:      %d\n", h.Channels)
	fmt.Printf("bits/sample:   %d\n", si.BitsPerSample)
	fmt.Printf("total samples: %d\n", si.TotalSamples)
	fmt.Printf("block size:    %d..%d\n", si.MinBlockSize, si.MaxBlockSize)
	fmt.Printf("duration:      %s\n", h.Duration)
	fmt.Printf("vendor:        %s\n", h.Extra.Vendor)

	fmt.Println("tags:")
	tags := h.Tags.Map()
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		for _, v := range tags[k] {
			fmt.Printf("  %-12s = %s\n", strings.ToLower(k), v)
		}
	}

	fmt.Println("metadata blocks:")
	if n := len(h.Extra.SeekTable); n > 0 {
		fmt.Printf("  seektable:   %d points\n", n)
	}
	if h.Extra.Padding > 0 {
		fmt.Printf("  padding:     %d bytes\n", h.Extra.Padding)
	}
	if n := len(h.Extra.Pictures); n > 0 {
		fmt.Printf("  pictures:    %d\n", n)
	}
	if n := len(h.Extra.Application); n > 0 {
		fmt.Printf("  application: %d\n", n)
	}
	if len(h.Extra.Cuesheet) > 0 {
		fmt.Printf("  cuesheet:    %d bytes\n", len(h.Extra.Cuesheet))
	}
}
