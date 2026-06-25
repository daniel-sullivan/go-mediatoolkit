// Write example: wrap a handful of synthetic "AAC access units" in ADTS frame
// headers and emit a standalone .aac stream, then re-parse it to confirm the
// framing round-trips.
//
// This demonstrates the framing layer in isolation: containers/adts.Writer
// computes each frame's aac_frame_length, encodes the fixed+variable header
// (optionally with CRC), and writes header + payload — no FDK-AAC engine is
// involved, because ADTS performs no compression. The "access units" here are
// arbitrary bytes standing in for an encoder's output; in a real pipeline they
// would come from codec/aac.NewEncoder pointed at this Writer (which is a
// codec/aac.PacketWriter).
//
// Usage: go run .            (writes adts-demo.aac in the working directory)
package main

import (
	"bytes"
	"fmt"
	"log"
	"os"

	"github.com/daniel-sullivan/go-mediatoolkit/containers/adts"
	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"
)

func main() {
	var buf bytes.Buffer

	// A 44.1 kHz stereo AAC-LC ADTS writer. WithCRC enables the optional
	// 2-byte per-frame CRC.
	w, err := adts.NewWriter(&buf, 44100, 2,
		adts.WithObjectType(aaclib.AOTAACLC),
		adts.WithCRC(false))
	if err != nil {
		log.Fatal(err)
	}

	// Stand-in access units of varying size.
	aus := [][]byte{
		bytes.Repeat([]byte{0x21}, 192),
		bytes.Repeat([]byte{0x22}, 256),
		bytes.Repeat([]byte{0x23}, 137),
	}
	for _, au := range aus {
		if err := w.WritePacket(au); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("wrote %d ADTS frames, %d bytes total\n", w.Frames(), buf.Len())
	fmt.Printf("asc the stream implies: % X (%s)\n", w.ASC().Raw, w.ASC().ObjectType)

	const out = "adts-demo.aac"
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s\n", out)

	// Re-parse to confirm the framing round-trips.
	rd, err := adts.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		log.Fatal(err)
	}
	got, err := rd.AccessUnits()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("re-parsed %d access units; first is %d bytes\n", len(got), len(got[0]))
}
