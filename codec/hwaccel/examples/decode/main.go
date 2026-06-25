// Decode example: open a hardware video Decoder under PreferHardware, feed it an
// encoded stream packet by packet, and report the decoded resolution and frame
// count. This teaches one concept — driving the pipelined hwaccel.Decoder loop
// (Decode → 0..n frames, then Flush to drain) — and nothing else.
//
// It is self-contained: rather than require an input file, the example first
// encodes a short run of synthetic NV12 frames to H.265 on the same hardware,
// then decodes those packets back. Both halves use PreferHardware, so on a host
// with no hardware codec the framework falls back loudly, Open* returns
// ErrNoBackend, and the example logs "no hardware codec available on this host"
// and exits 0.
//
// Usage: decode
package main

import (
	"errors"
	"log"

	"github.com/daniel-sullivan/go-mediatoolkit/codec/hwaccel"
	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

const (
	codec      = video.H265
	width      = 1280
	height     = 720
	frameCount = 30
)

func main() {
	log.SetFlags(0)

	// Step 1: synthesise an encoded stream so the example needs no input file.
	packets, ok := encodeStream()
	if !ok {
		log.Println("no hardware codec available on this host — nothing to decode")
		return
	}
	log.Printf("encoded %d source frames into %d %s packets", frameCount, len(packets), codec)

	// Step 2: decode the stream back to raw frames.
	dec, err := hwaccel.OpenDecoder(
		hwaccel.Policy{Mode: hwaccel.PreferHardware},
		hwaccel.NewConfig(hwaccel.WithCodec(codec)), // a decoder learns geometry from the stream
	)
	if err != nil {
		if errors.Is(err, hwaccel.ErrNoBackend) || errors.Is(err, hwaccel.ErrHardwareUnavailable) {
			log.Println("no hardware decoder available on this host")
			return
		}
		log.Fatal(err)
	}
	defer dec.Close()

	var frames int
	var w, h int
	var pixFmt video.PixelFormat
	for _, p := range packets {
		fs, err := dec.Decode(p)
		if err != nil {
			log.Fatal(err)
		}
		for _, f := range fs {
			frames++
			w, h, pixFmt = f.Width, f.Height, f.PixelFormat
		}
	}
	tail, err := dec.Flush() // drain the decode pipeline at end of stream
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range tail {
		frames++
		w, h, pixFmt = f.Width, f.Height, f.PixelFormat
	}

	log.Printf("decoded %d frames at %dx%d (%s)", frames, w, h, pixFmt)
}

// encodeStream encodes frameCount synthetic NV12 frames to `codec` on hardware
// and returns the packets. ok is false when no hardware encoder is available
// (the framework has already logged the loud fallback WARNING).
func encodeStream() (pkts []video.Packet, ok bool) {
	enc, err := hwaccel.OpenEncoder(
		hwaccel.Policy{Mode: hwaccel.PreferHardware},
		hwaccel.NewConfig(
			hwaccel.WithCodec(codec),
			hwaccel.WithResolution(width, height),
			hwaccel.WithBitrate(4_000_000),
			hwaccel.WithFrameRate(30, 1),
			hwaccel.WithPixelFormat(video.NV12),
		),
	)
	if err != nil {
		if errors.Is(err, hwaccel.ErrNoBackend) || errors.Is(err, hwaccel.ErrHardwareUnavailable) {
			return nil, false
		}
		log.Fatal(err)
	}
	defer enc.Close()

	for i := 0; i < frameCount; i++ {
		out, err := enc.Encode(synthFrame(i))
		if err != nil {
			log.Fatal(err)
		}
		pkts = append(pkts, out...)
	}
	tail, err := enc.Flush()
	if err != nil {
		log.Fatal(err)
	}
	pkts = append(pkts, tail...)
	return pkts, true
}

// synthFrame builds one tightly-packed NV12 frame (stride == width): a vertical
// luma ramp that scrolls with i over a neutral (gray) chroma plane.
func synthFrame(i int) video.Frame {
	y := make([]byte, width*height)
	for row := 0; row < height; row++ {
		v := byte((row + i*4) & 0xff)
		base := row * width
		for col := 0; col < width; col++ {
			y[base+col] = v
		}
	}
	uv := make([]byte, width*height/2)
	for j := range uv {
		uv[j] = 128
	}
	return video.Frame{
		PixelFormat: video.NV12,
		Width:       width,
		Height:      height,
		Planes:      [][]byte{y, uv},
		Strides:     []int{width, width},
	}
}
