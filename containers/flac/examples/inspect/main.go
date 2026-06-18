// Inspect example: synthesize a small native FLAC stream in memory (encode a
// tone with tags via codec/flac), then walk its on-wire metadata-block chain
// directly — dumping each block's type, is_last flag, and body length — to show
// the fLaC + metadata-block framing that containers/flac parses.
//
// This teaches the low-level FLAC container structure (RFC 9639 §8): the 4-byte
// magic, then a chain of [type+is_last byte | 24-bit big-endian length | body]
// blocks ending where is_last is set, then the audio frames. Self-contained: it
// needs no input file, and the whole path is pure Go and MIT.
//
// Usage: go run .
package main

import (
	"bytes"
	"fmt"
	"log"
	"time"

	"go-mediatoolkit/codec/flac"
	"go-mediatoolkit/consts"
	"go-mediatoolkit/generators"
)

// FLAC metadata block types (RFC 9639 §8.1).
var blockNames = map[byte]string{
	0: "STREAMINFO",
	1: "PADDING",
	2: "APPLICATION",
	3: "SEEKTABLE",
	4: "VORBIS_COMMENT",
	5: "CUESHEET",
	6: "PICTURE",
}

func main() {
	const (
		sampleRate = consts.SampleRate48000
		channels   = 1
		duration   = 100 * time.Millisecond
	)

	// Synthesize a native FLAC stream with a couple of tags so the chain has a
	// VORBIS_COMMENT block alongside the mandatory STREAMINFO.
	input := generators.Sine(consts.FreqNoteA4, duration, sampleRate)
	var buf bytes.Buffer
	enc, err := flac.NewEncoder(&buf, sampleRate, channels,
		flac.WithTotalSamples(uint64(len(input.Data))),
		flac.WithTag("TITLE", "Inspect Tone"),
		flac.WithTag("ARTIST", "go-mediatoolkit"),
	)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := enc.Write(input); err != nil {
		log.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		log.Fatal(err)
	}

	data := buf.Bytes()
	if len(data) < 4 || string(data[:4]) != "fLaC" {
		log.Fatal("not a FLAC stream")
	}
	fmt.Printf("magic: fLaC (%d total bytes)\n", len(data))

	// Walk the metadata-block chain: 4-byte header (type+is_last, 24-bit length)
	// then the body, until the is_last flag is set.
	off := 4
	for {
		if off+4 > len(data) {
			log.Fatal("truncated metadata block header")
		}
		isLast := data[off]&0x80 != 0
		blockType := data[off] & 0x7F
		length := int(data[off+1])<<16 | int(data[off+2])<<8 | int(data[off+3])
		name := blockNames[blockType]
		if name == "" {
			name = fmt.Sprintf("RESERVED(%d)", blockType)
		}
		fmt.Printf("  block %-14s last=%-5t length=%d\n", name, isLast, length)

		off += 4 + length
		if isLast {
			break
		}
	}
	fmt.Printf("audio frames begin at byte offset %d\n", off)
}
