// Decode example: decode an AAC stream into interleaved float64 PCM with
// codec/aac. This teaches one concept — driving the streaming codec.Decoder
// loop over a container-supplied PacketReader — and nothing else.
//
// AAC has no canonical byte-stream framing, so the access units (packets) and
// the AudioSpecificConfig (ASC) come from a container. This example accepts an
// .m4a/.mp4 file (parsed by containers/mp4) or a raw .aac ADTS file (parsed by
// containers/adts); both expose a PacketReader plus the ASC the decoder needs.
//
// FDK-AAC is the only AAC engine and is fenced behind the aacfdk build tag, so
// a default build surfaces ErrEngineRequiresFDK from NewDecoder; this example
// reports that and exits cleanly. Build with `-tags aacfdk` to decode for real:
//
//	go run -tags aacfdk ./codec/aac/examples/decode input.m4a
//
// Usage: decode <input.m4a|input.aac>
package main

import (
	"errors"
	"io"
	"log"
	"os"
	"strings"

	aaccodec "github.com/daniel-sullivan/go-mediatoolkit/codec/aac"
	"github.com/daniel-sullivan/go-mediatoolkit/containers/adts"
	"github.com/daniel-sullivan/go-mediatoolkit/containers/mp4"
	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <input.m4a|input.aac>", os.Args[0])
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Pick the container by extension. Both an MP4 sample table and an ADTS
	// stream expose a PacketReader (ReadPacket) plus the AudioSpecificConfig
	// the decoder needs — only the parser differs.
	var (
		pr  aaccodec.PacketReader
		asc aaclib.AudioSpecificConfig
	)
	if strings.HasSuffix(strings.ToLower(os.Args[1]), ".aac") {
		rd, err := adts.NewReader(f)
		if err != nil {
			log.Fatal(err)
		}
		pr, asc = rd, rd.ASC()
	} else {
		rd, err := mp4.NewReader(f)
		if err != nil {
			log.Fatal(err)
		}
		// Packets() slices the AAC access units out of the mdat box;
		// Header().Extra.Config is the esds AudioSpecificConfig.
		pr, asc = rd.Packets(), rd.Header().Extra.Config
	}

	log.Printf("stream: %s, %d Hz, %d ch", asc.ObjectType, asc.SampleRate, asc.Channels)

	dec, err := aaccodec.NewDecoder(pr, asc)
	if err != nil {
		if errors.Is(err, aaclib.ErrEngineRequiresFDK) {
			log.Printf("AAC decode requires the FDK engine; rebuild with -tags aacfdk to run this example")
			return
		}
		log.Fatal(err)
	}

	// Drive the codec.Decoder loop: Read fills a float64 buffer with
	// interleaved samples in [-1.0, 1.0]; loop until io.EOF.
	buf := make([]float64, 8192)
	var total int
	for {
		audio, err := dec.Read(buf)
		total += len(audio.Data)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("decoded %d samples (%d per channel) at %d Hz, %d ch",
		total, total/max(dec.Channels(), 1), dec.SampleRate(), dec.Channels())
}
