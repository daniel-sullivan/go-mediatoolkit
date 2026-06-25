// Round-trip example: encode a sine wave through the Opus codec layer,
// collecting packets via a PacketWriter, then decode them back.
package main

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/codec"
	"github.com/daniel-sullivan/go-mediatoolkit/codec/opus"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	opuslib "github.com/daniel-sullivan/go-mediatoolkit/libraries/opus"
)

// packetCollector captures encoded Opus packets into a slice.
type packetCollector struct {
	packets [][]byte
}

func (c *packetCollector) WritePacket(data []byte) error {
	pkt := make([]byte, len(data))
	copy(pkt, data)
	c.packets = append(c.packets, pkt)
	return nil
}

func main() {
	const (
		sampleRate = opuslib.Rate48000
		channels   = 1
		duration   = 100 * time.Millisecond
	)

	input := generators.Sine(consts.FreqNoteA4, duration, sampleRate)

	collector := &packetCollector{}
	enc, err := opus.NewEncoder(collector, sampleRate, channels,
		opus.WithBitrate(64000),
		opus.WithApplication(opuslib.AppAudio),
		opus.WithFrameDuration(20),
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

	var totalBytes int
	for _, p := range collector.packets {
		totalBytes += len(p)
	}
	fmt.Printf("encoded %d samples -> %d packets, %d bytes total\n",
		len(input.Data), len(collector.packets), totalBytes)

	dec, err := opus.NewDecoder(opus.NewSlicePacketReader(collector.packets),
		sampleRate, channels)
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
