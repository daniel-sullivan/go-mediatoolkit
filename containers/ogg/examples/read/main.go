// Read example: open an Opus-in-Ogg file, print metadata, then decode it
// through codec/opus.
//
// Usage: read <path.opus>
package main

import (
	"fmt"
	"log"
	"maps"
	"math"
	"os"
	"slices"
	"strings"

	"github.com/daniel-sullivan/go-mediatoolkit/codec/opus"
	"github.com/daniel-sullivan/go-mediatoolkit/containers/ogg"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <path.opus>", os.Args[0])
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	r, err := ogg.NewOpusReader(f)
	if err != nil {
		log.Fatal(err)
	}

	h := r.Header()
	fmt.Printf("format:        %s\n", h.Format)
	fmt.Printf("sample rate:   %d Hz\n", h.SampleRate)
	fmt.Printf("channels:      %d\n", h.Channels)
	fmt.Printf("pre-skip:      %d samples\n", h.Extra.Head.PreSkip)
	fmt.Printf("vendor:        %s\n", h.Extra.Vendor)
	fmt.Printf("serial no:     %d\n", h.Extra.SerialNo)

	fmt.Println("tags:")
	tags := h.Tags.Map()
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		for _, v := range tags[k] {
			fmt.Printf("  %-12s = %s\n", strings.ToLower(k), v)
		}
	}

	// Decode through codec/opus (OpusReader satisfies its PacketReader shape).
	dec, err := opus.NewDecoder(r, h.SampleRate, h.Channels)
	if err != nil {
		log.Fatal(err)
	}

	buf := make([]float64, 4096)
	var (
		total int
		peak  float64
	)
	for {
		got, rerr := dec.Read(buf)
		for _, v := range got.Data {
			if a := math.Abs(v); a > peak {
				peak = a
			}
		}
		total += len(got.Data)
		if rerr != nil {
			break
		}
	}

	frames := total / h.Channels
	fmt.Printf("\ndecoded %d sample frames, peak amplitude %.4f\n", frames, peak)
}
