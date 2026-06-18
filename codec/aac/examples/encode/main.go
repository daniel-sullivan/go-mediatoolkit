// Encode example: turn interleaved float64 PCM into AAC access units with
// codec/aac and frame them into a playable .aac (ADTS) file. This teaches one
// concept — driving the streaming codec.Encoder loop into a container framer —
// and nothing else.
//
// AAC has no canonical byte-stream framing, so the encoder emits raw access
// units (packets) through a PacketWriter. An adts.Writer is a PacketWriter that
// wraps each access unit in an ADTS header, producing a self-framed .aac file.
//
// The encoder profile is chosen with WithObjectType. The default is AAC-LC
// (AOTAACLC); pass -heaac to encode HE-AAC v1 (AOTSBR, SBR) instead. (HE-AAC v2
// / parametric stereo is shown in the heaac example.)
//
// FDK-AAC is the only AAC engine and is fenced behind the aacfdk build tag, so
// a default build surfaces ErrEngineRequiresFDK from NewEncoder; this example
// reports that and exits cleanly. Build with `-tags aacfdk` to encode for real:
//
//	go run -tags aacfdk ./codec/aac/examples/encode out.aac
//	go run -tags aacfdk ./codec/aac/examples/encode -heaac out.aac
//
// Usage: encode [-heaac] <output.aac>
package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"time"

	aaccodec "go-mediatoolkit/codec/aac"
	"go-mediatoolkit/consts"
	"go-mediatoolkit/containers/adts"
	"go-mediatoolkit/generators"
	aaclib "go-mediatoolkit/libraries/aac"
)

func main() {
	heaac := flag.Bool("heaac", false, "encode HE-AAC v1 (SBR) instead of AAC-LC")
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatalf("usage: %s [-heaac] <output.aac>", os.Args[0])
	}

	const (
		sampleRate = consts.SampleRate44100
		channels   = 1
	)

	// A one-second 440 Hz sine is the entire audio source — Sine returns a
	// mutations.Audio of interleaved float64 in [-1.0, 1.0], the encoder's
	// input type.
	tone := generators.Sine(consts.FreqNoteA4, time.Second, sampleRate)

	out, err := os.Create(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	// The ADTS writer is the PacketWriter: it frames each raw access unit in an
	// ADTS header so the result is a playable self-framed .aac stream.
	objectType := aaclib.AOTAACLC
	if *heaac {
		objectType = aaclib.AOTSBR
	}
	framer, err := adts.NewWriter(out, sampleRate, channels, adts.WithObjectType(objectType))
	if err != nil {
		log.Fatal(err)
	}

	enc, err := aaccodec.NewEncoder(framer, sampleRate, channels,
		aaccodec.WithObjectType(objectType),
		aaccodec.WithBitrate(96000),
	)
	if err != nil {
		if errors.Is(err, aaclib.ErrEngineRequiresFDK) {
			log.Printf("AAC encode requires the FDK engine; rebuild with -tags aacfdk to run this example")
			return
		}
		log.Fatal(err)
	}

	if _, err := enc.Write(tone); err != nil {
		log.Fatal(err)
	}
	// Close flushes the final, silence-padded frame; it does not close out.
	if err := enc.Close(); err != nil {
		log.Fatal(err)
	}

	log.Printf("encoded %s of %s audio to %s (%d ADTS frames)",
		tone.Duration(), objectType, flag.Arg(0), framer.Frames())
}
