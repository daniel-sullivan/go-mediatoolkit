// Pipeline example: generate a sine sweep, encode it to Opus, then decode
// and re-encode the output as int16 PCM — a mini "compress + save" pipeline.
package main

import (
	"bytes"
	"fmt"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/codec"
	"github.com/daniel-sullivan/go-mediatoolkit/codec/opus"
	"github.com/daniel-sullivan/go-mediatoolkit/codec/pcm"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	opuslib "github.com/daniel-sullivan/go-mediatoolkit/libraries/opus"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

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
		duration   = 500 * time.Millisecond
	)

	// 1. Generate a 200 Hz -> 2 kHz sine sweep.
	input := generators.SineSweep(200, 2000, duration, sampleRate)
	fmt.Printf("generated %d samples (%v @ %d Hz)\n", len(input.Data), duration, sampleRate)

	// 2. Encode through the Opus codec.
	collector := &packetCollector{}
	opusEnc, err := opus.NewEncoder(collector, sampleRate, channels,
		opus.WithBitrate(32000),
	)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := opusEnc.Write(input); err != nil {
		log.Fatal(err)
	}
	if err := opusEnc.Close(); err != nil {
		log.Fatal(err)
	}

	var opusBytes int
	for _, p := range collector.packets {
		opusBytes += len(p)
	}
	fmt.Printf("opus: %d packets, %d bytes\n", len(collector.packets), opusBytes)

	// 3. Decode Opus -> float64 and re-encode as int16 PCM, streaming in chunks.
	opusDec, err := opus.NewDecoder(
		opus.NewSlicePacketReader(collector.packets),
		sampleRate, channels,
	)
	if err != nil {
		log.Fatal(err)
	}

	var pcmOut bytes.Buffer
	pcmEnc, err := pcm.NewEncoder(&pcmOut, sampleRate, channels, mutations.FormatInt16)
	if err != nil {
		log.Fatal(err)
	}

	chunk := make([]float64, 960) // 20ms at 48 kHz
	var samples int
	for {
		got, rerr := opusDec.Read(chunk)
		if len(got.Data) > 0 {
			if _, werr := pcmEnc.Write(got); werr != nil {
				log.Fatal(werr)
			}
			samples += len(got.Data)
		}
		if rerr != nil {
			break
		}
	}
	if err := pcmEnc.Close(); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("pcm:  %d samples, %d bytes (int16 LE)\n", samples, pcmOut.Len())
	fmt.Printf("compression ratio: %.2fx\n", float64(pcmOut.Len())/float64(opusBytes))

	// 4. Demonstrate codec.ReadFull with a fresh decoder over the same packets.
	verify, _ := opus.NewDecoder(
		opus.NewSlicePacketReader(collector.packets),
		sampleRate, channels,
	)
	full := make([]float64, 4800) // first 100ms
	got, err := codec.ReadFull(verify, full)
	fmt.Printf("ReadFull fetched %d samples (err=%v)\n", len(got.Data), err)
}
