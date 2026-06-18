// Decode example: the full container -> codec split for MP3. Open an MP3 file
// with containers/mp3 (which parses the ID3 metadata around the self-framed
// audio), then pipe Reader.Data() — the ID3 prefix + audio frames + any ID3v1
// trailer, unchanged — straight into codec/mp3.NewDecoder to recover interleaved
// float64 samples, reporting the count and peak amplitude.
//
// The decode path uses the pure-Go minimp3 port (CC0) by default, so this runs
// in a default CGO_ENABLED=0 build with no LGPL code linked — reading and
// decoding never need the mp3lame encoder. Reader.Data() replays the ID3 frames
// verbatim; the decoder skips them itself, so no re-seeking is needed.
//
// Usage: go run . <input.mp3>
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"

	"go-mediatoolkit/codec/mp3"
	ctrmp3 "go-mediatoolkit/containers/mp3"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <input.mp3>", os.Args[0])
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Container: parse the ID3 metadata around the self-framed MP3 stream.
	r, err := ctrmp3.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}
	h := r.Header()
	// Header().SampleRate / Channels now come from the first MPEG frame header,
	// which NewReader peeks (without decoding) up front — so they are known
	// before a single sample is read. Extra.StreamInfo carries the MP3-only
	// details (MPEG version, samples/frame, nominal bit rate).
	fmt.Printf("%d Hz, %d ch", h.SampleRate, h.Channels)
	if br := h.Extra.StreamInfo.BitRate; br > 0 {
		fmt.Printf(", %d kbps", br/1000)
	}
	if h.Tags.Title != nil {
		fmt.Printf(", title %q", *h.Tags.Title)
	}
	fmt.Println()

	// Codec: Reader.Data() replays the whole byte stream (ID3 + frames); the
	// minimp3 decoder skips the ID3 prefix itself.
	dec, err := mp3.NewDecoder(r.Data())
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

	// The decoder reports the same parameters once it has decoded a frame; they
	// should match the container header peeked from the first frame above.
	fmt.Printf("decoder reports %d Hz, %d ch\n", dec.SampleRate(), dec.Channels())

	fmt.Printf("decoded %d samples (%d per channel), peak amplitude %.4f\n",
		total, total/max(dec.Channels(), h.Channels, 1), peak)
}
