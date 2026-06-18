// Demux + decode example: open a standalone .aac / ADTS stream, demux its AAC
// access units, derive the AudioSpecificConfig from the first frame header, and
// decode straight to interleaved float64 PCM through codec/aac.
//
// This is the headline integration: containers/adts is a codec/aac.PacketReader
// and exposes the ASC the codec needs, so an ADTS stream pipes into
// codec/aac.NewDecoder with no separate config record (unlike MP4, ADTS has no
// out-of-band esds). The framing/demux layer is pure Go and MIT; decoding the
// access units reaches the Fraunhofer FDK-AAC engine, which is fenced behind
// the aacfdk build tag. Without -tags aacfdk, codec/aac surfaces
// ErrEngineRequiresFDK and this example reports the parsed framing and exits
// cleanly. Build with `-tags aacfdk` (and CGO_ENABLED=1 for the C backend) to
// decode.
//
// Usage: go run . path/to/file.aac
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"

	aaccodec "go-mediatoolkit/codec/aac"
	"go-mediatoolkit/containers/adts"
	aaclib "go-mediatoolkit/libraries/aac"
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

	// Demux the ADTS framing (pure Go, MIT — no FDK-AAC linked here).
	rd, err := adts.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}
	h := rd.Header()
	fmt.Printf("object type:  %s\n", h.Extra.Config.ObjectType)
	fmt.Printf("sample rate:  %d Hz\n", h.SampleRate)
	fmt.Printf("channels:     %d\n", h.Channels)

	// Reader is a codec/aac.PacketReader; rd.ASC() is the derived config.
	dec, err := aaccodec.NewDecoder(rd, rd.ASC())
	if err != nil {
		if engineUnavailable(err) {
			fmt.Println("decode: AAC engine unavailable; rebuild with -tags aacfdk to decode")
			return
		}
		log.Fatal(err)
	}

	buf := make([]float64, 8192)
	var total int
	var peak float64
	for {
		audio, err := dec.Read(buf)
		for _, v := range audio.Data {
			if a := math.Abs(v); a > peak {
				peak = a
			}
		}
		total += len(audio.Data)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if engineUnavailable(err) {
				fmt.Println("decode: AAC engine unavailable; rebuild with -tags aacfdk to decode")
				return
			}
			log.Fatal(err)
		}
	}

	fmt.Printf("decoded %d samples (%d per channel), peak amplitude %.4f\n",
		total, total/max(h.Channels, 1), peak)
}

// engineUnavailable reports whether err is the FDK-fence signal from
// libraries/aac: decode requires -tags aacfdk (ErrEngineRequiresFDK), or the
// stream uses a profile/coding tool the pure-Go port does not yet cover
// (ErrUnimplemented).
func engineUnavailable(err error) bool {
	return errors.Is(err, aaclib.ErrEngineRequiresFDK) ||
		errors.Is(err, aaclib.ErrUnimplemented)
}
