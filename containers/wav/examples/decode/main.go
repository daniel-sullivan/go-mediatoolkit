// Decode example: the full container -> codec split. Open a WAV file with the
// pure-Go RIFF Reader, then feed the data chunk (plus the sample format the fmt
// chunk reported) straight into codec/pcm to recover interleaved float64
// samples, reporting the count and peak amplitude.
//
// This is the WAV analogue of the FLAC/Opus decode examples: the container
// (containers/wav) frames the PCM and describes its layout; the codec
// (codec/pcm) turns those bytes into samples. Both halves are pure Go and MIT —
// it runs in a default CGO_ENABLED=0 build.
//
// Usage: go run . <path.wav>
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"

	"github.com/daniel-sullivan/go-mediatoolkit/codec/pcm"
	"github.com/daniel-sullivan/go-mediatoolkit/containers/wav"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <path.wav>", os.Args[0])
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Container: parse the RIFF chunk tree and locate the data payload.
	r, err := wav.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}
	h := r.Header()
	fmt.Printf("%d Hz, %d ch, %s\n", h.SampleRate, h.Channels, h.SampleFormat)

	// Codec: Reader.Data() are the raw PCM bytes; Header describes their layout.
	dec, err := pcm.NewDecoder(r.Data(), h.SampleRate, h.Channels, h.SampleFormat)
	if err != nil {
		log.Fatal(err)
	}

	buf := make([]float64, 4096)
	var (
		total int
		peak  float64
	)
	for {
		got, err := dec.Read(buf)
		for _, v := range got.Data {
			if a := math.Abs(v); a > peak {
				peak = a
			}
		}
		total += len(got.Data)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
	}

	fmt.Printf("decoded %d samples (%d per channel), peak amplitude %.4f\n",
		total, total/max(h.Channels, 1), peak)
}
