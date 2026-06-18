// Inspect example: open any Ogg file with the generic, codec-agnostic Reader
// and report every logical bitstream — its serial number, the best-effort codec
// hint sniffed from the BOS packet, the count of header packets collected, and
// the total number of packets in the stream.
//
// This teaches the generic Ogg demultiplexer (no codec engine): containers/ogg
// slices pages into packets and separates logical streams by serial number,
// independent of whether the payload is Opus, Vorbis, or FLAC. The whole path is
// pure Go and MIT.
//
// Usage: go run . <path.ogg|.opus>
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"go-mediatoolkit/containers/ogg"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <path.ogg|.opus>", os.Args[0])
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Generic demux: one Stream per logical bitstream, no codec parsing.
	r, err := ogg.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}
	h := r.Header()
	fmt.Printf("format:  %s\n", h.Format)
	fmt.Printf("streams: %d\n", len(r.Streams()))

	for i, s := range r.Streams() {
		codec := s.CodecHint
		if codec == "" {
			codec = "(unknown)"
		}
		fmt.Printf("\nstream %d:\n", i)
		fmt.Printf("  serial no:      %d\n", s.SerialNo)
		fmt.Printf("  codec hint:     %s\n", codec)
		fmt.Printf("  header packets: %d\n", len(s.HeaderPackets))

		// Drain the remaining packets to count the stream's payload.
		var packets int
		for {
			_, err := s.ReadPacket()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				log.Fatal(err)
			}
			packets++
		}
		fmt.Printf("  data packets:   %d\n", packets)
	}
}
