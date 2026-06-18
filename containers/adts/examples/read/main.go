// Read example: parse a standalone .aac / ADTS stream, derive its
// AudioSpecificConfig from the first frame header, and report every AAC access
// unit it carries.
//
// ADTS is a pure framing container (containers/adts): it adds nothing but a
// per-frame 7-byte header (9 with CRC) over raw AAC access units, and carries
// no out-of-band config — the AudioSpecificConfig is re-derived from the first
// header. This example reads a file, prints the projected config and the frame
// table, and does NOT decode (no FDK-AAC engine is linked); see the decode
// example for piping the access units into codec/aac.
//
// Usage: go run . path/to/file.aac
package main

import (
	"fmt"
	"log"
	"os"

	"go-mediatoolkit/containers/adts"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s path/to/file.aac", os.Args[0])
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	rd, err := adts.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}

	h := rd.Header()
	fmt.Printf("format:       %s\n", h.Format)
	fmt.Printf("object type:  %s\n", h.Extra.Config.ObjectType)
	fmt.Printf("sample rate:  %d Hz\n", h.SampleRate)
	fmt.Printf("channels:     %d\n", h.Channels)
	fmt.Printf("mpeg version: %d (0=MPEG-4, 1=MPEG-2)\n", h.Extra.MPEGVersion)
	fmt.Printf("crc present:  %v\n", h.Extra.CRCPresent)
	fmt.Printf("asc bytes:    % X\n", h.Extra.Config.Raw)

	var frames, totalAU int
	for {
		au, err := rd.ReadPacket()
		if err != nil {
			break
		}
		if frames < 8 {
			fmt.Printf("  frame %2d: %d-byte access unit\n", frames, len(au))
		}
		frames++
		totalAU += len(au)
	}
	fmt.Printf("frames:       %d (%d AU bytes total)\n", frames, totalAU)
}
