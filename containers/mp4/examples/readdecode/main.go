// Read + decode example: open an .m4a/.mp4 file, print its parsed header and
// iTunes tags via the pure-Go MP4 Reader, then decode the AAC access units it
// carries through codec/aac and report the peak amplitude of the recovered
// signal.
//
// The container layer is pure Go and MIT: containers/mp4 walks the ISOBMFF box
// tree (ftyp/moov/mdat), recovers the esds AudioSpecificConfig and the
// stsz/stsc/stco sample tables, slices mdat into AAC access units, and projects
// the moov.udta.meta.ilst atoms onto containers.StandardTags. Decoding those
// access units goes through codec/aac, which reaches the Fraunhofer FDK-AAC
// engine — so the decode step needs -tags aacfdk (and, for the C backend,
// CGO_ENABLED=1). Without the tag, codec/aac surfaces ErrEngineRequiresFDK; a
// stream using a profile/coding tool the pure-Go port does not yet cover
// surfaces ErrUnimplemented. In either case this example prints the tags it
// read and exits cleanly, documenting the intended read-and-decode flow.
//
// Usage: go run . path/to/file.m4a
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"math"
	"os"
	"slices"
	"strings"

	aaccodec "github.com/daniel-sullivan/go-mediatoolkit/codec/aac"
	"github.com/daniel-sullivan/go-mediatoolkit/containers/mp4"
	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s path/to/file.m4a", os.Args[0])
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Parse the ISOBMFF box tree (pure Go, MIT — no FDK-AAC linked here).
	rd, err := mp4.NewReader(f)
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
			fmt.Printf("  %-12s = %s\n", strings.ToLower(k), v)
		}
	}
	if len(h.Extra.CoverArt) > 0 {
		fmt.Printf("cover art:    %d image(s)\n", len(h.Extra.CoverArt))
	}

	// Decode the AAC access units via codec/aac. Reader.Packets() is a
	// codec/aac.PacketReader; Header.Extra.Config is the esds ASC.
	dec, err := aaccodec.NewDecoder(rd.Packets(), h.Extra.Config)
	if err != nil {
		if engineUnavailable(err) {
			fmt.Println("decode: AAC engine unavailable; rebuild with -tags aacfdk to decode")
			return
		}
		log.Fatal(err)
	}

	buf := make([]float64, 8192)
	var total int
	var peak float64
	for {
		audio, err := dec.Read(buf)
		for _, v := range audio.Data {
			if a := math.Abs(v); a > peak {
				peak = a
			}
		}
		total += len(audio.Data)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if engineUnavailable(err) {
				fmt.Println("decode: AAC engine unavailable; rebuild with -tags aacfdk to decode")
				return
			}
			log.Fatal(err)
		}
	}

	fmt.Printf("decoded %d samples (%d per channel), peak amplitude %.4f\n",
		total, total/max(h.Channels, 1), peak)
}

// engineUnavailable reports whether err is the FDK-fence signal from
// libraries/aac: decode requires -tags aacfdk (ErrEngineRequiresFDK), or the
// stream uses a profile/coding tool the pure-Go port does not yet cover
// (ErrUnimplemented).
func engineUnavailable(err error) bool {
	return errors.Is(err, aaclib.ErrEngineRequiresFDK) ||
		errors.Is(err, aaclib.ErrUnimplemented)
}
