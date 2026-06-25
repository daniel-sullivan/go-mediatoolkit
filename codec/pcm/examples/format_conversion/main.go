// Format conversion example: decode int16 PCM bytes from one stream and
// re-encode them as float32 PCM bytes into another.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/codec"
	"github.com/daniel-sullivan/go-mediatoolkit/codec/pcm"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func main() {
	const (
		sampleRate = consts.SampleRate44100
		channels   = 1
	)

	// Prepare some int16 little-endian PCM bytes to act as our "input file".
	samples := generators.Sine(consts.FreqNoteA4, 50*time.Millisecond, sampleRate).Data
	srcBytes := make([]byte, mutations.FormatInt16.BytesPerSample()*len(samples))
	mutations.EncodeSamples(samples, srcBytes, mutations.FormatInt16, binary.LittleEndian)

	src := bytes.NewReader(srcBytes)
	var dst bytes.Buffer

	dec, err := pcm.NewDecoder(src, sampleRate, channels, mutations.FormatInt16)
	if err != nil {
		log.Fatal(err)
	}
	enc, err := pcm.NewEncoder(&dst, sampleRate, channels, mutations.FormatFloat32)
	if err != nil {
		log.Fatal(err)
	}

	// Stream in 1024-sample chunks — no need to hold the whole file in memory.
	buf := make([]float64, 1024)
	var total int
	for {
		chunk, rerr := dec.Read(buf)
		if len(chunk.Data) > 0 {
			if _, werr := enc.Write(chunk); werr != nil {
				log.Fatal(werr)
			}
			total += len(chunk.Data)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			log.Fatal(rerr)
		}
	}
	if err := enc.Close(); err != nil {
		log.Fatal(err)
	}

	srcBPS := mutations.FormatInt16.BytesPerSample()
	dstBPS := mutations.FormatFloat32.BytesPerSample()
	fmt.Printf("converted %d samples: %d bytes (int16) -> %d bytes (float32)\n",
		total, total*srcBPS, dst.Len())
	fmt.Printf("size ratio: %.2fx\n", float64(dstBPS)/float64(srcBPS))

	// Read back a handful to confirm the output is well-formed.
	verify, err := pcm.NewDecoder(&dst, sampleRate, channels, mutations.FormatFloat32)
	if err != nil {
		log.Fatal(err)
	}
	check := make([]float64, 4)
	got, _ := codec.ReadFull(verify, check)
	fmt.Printf("first 4 samples: %v\n", got.Data)
}
