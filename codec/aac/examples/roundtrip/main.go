// Round-trip example: encode a sine wave to AAC, frame it as an ADTS (.aac)
// byte stream, then read that stream back and decode it -- exercising the
// codec + container split end to end.
//
// codec/aac works on raw AAC access units via PacketReader / PacketWriter; the
// ADTS container (containers/adts) frames those access units into a self-
// describing byte stream and recovers the AudioSpecificConfig on read, so the
// decoder needs no out-of-band config. (The earlier version hand-built an ASC
// without its Raw bytes, which the FDK engine rejects -- sourcing it from the
// container is the correct pattern.)
//
// FDK-AAC is the only AAC engine and is fenced behind the aacfdk build tag, so
// a default build surfaces ErrEngineRequiresFDK from the encoder; this example
// reports that and exits cleanly. Build with `-tags aacfdk` to run for real.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"go-mediatoolkit/codec"
	aaccodec "go-mediatoolkit/codec/aac"
	"go-mediatoolkit/consts"
	"go-mediatoolkit/containers/adts"
	"go-mediatoolkit/generators"
	aaclib "go-mediatoolkit/libraries/aac"
)

func main() {
	const (
		sampleRate = consts.SampleRate44100
		channels   = 1
		duration   = 100 * time.Millisecond
	)

	input := generators.Sine(consts.FreqNoteA4, duration, sampleRate)

	// Encode straight into an ADTS byte stream: the adts.Writer is a
	// codec/aac.PacketWriter, so the encoder emits framed .aac bytes.
	var stream bytes.Buffer
	aw, err := adts.NewWriter(&stream, sampleRate, channels)
	if err != nil {
		log.Fatal(err)
	}
	enc, err := aaccodec.NewEncoder(aw, sampleRate, channels, aaccodec.WithBitrate(128000))
	if err != nil {
		if errors.Is(err, aaclib.ErrEngineRequiresFDK) {
			log.Printf("AAC requires the FDK engine; rebuild with -tags aacfdk to run this example")
			return
		}
		log.Fatal(err)
	}
	if _, err := enc.Write(input); err != nil {
		log.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("encoded %d samples -> %d-byte ADTS stream\n", len(input.Data), stream.Len())

	// Read the ADTS stream back: the reader recovers the AudioSpecificConfig and
	// replays the access units as a PacketReader, so the decoder is configured
	// entirely from the container.
	ar, err := adts.NewReader(&stream)
	if err != nil {
		log.Fatal(err)
	}
	dec, err := aaccodec.NewDecoder(ar, ar.ASC())
	if err != nil {
		log.Fatal(err)
	}

	output := make([]float64, len(input.Data))
	got, _ := codec.ReadFull(dec, output)

	var peak float64
	for _, v := range got.Data {
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	fmt.Printf("decoded %d samples, peak amplitude %.4f\n", len(got.Data), peak)
}
