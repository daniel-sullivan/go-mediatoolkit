// Decode example: the full container -> codec split for native FLAC. Synthesize
// a small FLAC stream in memory (encode a tone via codec/flac), re-open it with
// the containers/flac Reader, then pipe Reader.Data() — the original bytes,
// magic + metadata + frames — straight into codec/flac.NewDecoder to recover
// interleaved float64 samples, reporting the count and peak amplitude.
//
// The decode path uses the pure-Go FLAC port by default, so this runs in a
// default CGO_ENABLED=0 build; building with `cgo` routes through libFLAC
// instead. Both halves are MIT (or BSD-3-Clause libFLAC under cgo).
//
// Usage: go run .
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"time"

	codecflac "github.com/daniel-sullivan/go-mediatoolkit/codec/flac"
	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	ctrflac "github.com/daniel-sullivan/go-mediatoolkit/containers/flac"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
)

func main() {
	const (
		sampleRate = consts.SampleRate48000
		channels   = 1
		duration   = 200 * time.Millisecond
	)

	// Synthesize a native FLAC stream with tags.
	input := generators.Sine(consts.FreqNoteA4, duration, sampleRate)
	var buf bytes.Buffer
	enc, err := codecflac.NewEncoder(&buf, sampleRate, channels,
		codecflac.WithTotalSamples(uint64(len(input.Data))),
		codecflac.WithTag("TITLE", "Decode Tone"),
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
	fmt.Printf("encoded %d samples -> %d FLAC bytes\n", len(input.Data), buf.Len())

	// Container: parse the metadata chain; Reader.Data() replays the whole stream.
	r, err := ctrflac.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		log.Fatal(err)
	}
	h := r.Header()
	fmt.Printf("%d Hz, %d ch, %d-bit", h.SampleRate, h.Channels, h.Extra.StreamInfo.BitsPerSample)
	if h.Tags.Title != nil {
		fmt.Printf(", title %q", *h.Tags.Title)
	}
	fmt.Println()

	// Codec: feed the container's byte stream straight into the FLAC decoder.
	dec, err := codecflac.NewDecoder(r.Data())
	if err != nil {
		log.Fatal(err)
	}

	out := make([]float64, 4096)
	var (
		total int
		peak  float64
	)
	for {
		got, err := dec.Read(out)
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
		total, total/max(channels, 1), peak)
}
