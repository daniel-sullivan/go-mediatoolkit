// Encode example: open a hardware video Encoder under PreferHardware, feed it a
// handful of synthetic NV12 frames, and report how many packets / bytes the
// accelerator produced. This teaches one concept — driving the pipelined
// hwaccel.Encoder loop (Encode → 0..n packets, then Flush to drain) — and
// nothing else.
//
// It is self-contained: the input frames are synthesised in-process (a moving
// luma ramp on a flat chroma plane), so no input file is needed. On a host with
// no hardware H.265 encoder the framework falls back loudly and OpenEncoder
// returns ErrNoBackend; the example logs "no hardware codec available on this
// host" and exits 0.
//
// Usage: encode
package main

import (
	"errors"
	"log"

	"go-mediatoolkit/codec/hwaccel"
	"go-mediatoolkit/video"
)

const (
	width      = 1280
	height     = 720
	frameCount = 30
)

func main() {
	log.SetFlags(0)

	enc, err := hwaccel.OpenEncoder(
		hwaccel.Policy{Mode: hwaccel.PreferHardware},
		hwaccel.NewConfig(
			hwaccel.WithCodec(video.H265),
			hwaccel.WithResolution(width, height),
			hwaccel.WithBitrate(4_000_000),
			hwaccel.WithFrameRate(30, 1),
			hwaccel.WithPixelFormat(video.NV12),
		),
	)
	if err != nil {
		// No hardware encoder (and no software tier wired in yet): the framework
		// already logged a loud WARNING. Exit cleanly — this is the graceful
		// no-hardware path, not a program error.
		if errors.Is(err, hwaccel.ErrNoBackend) || errors.Is(err, hwaccel.ErrHardwareUnavailable) {
			log.Println("no hardware codec available on this host — nothing to encode")
			return
		}
		log.Fatal(err)
	}
	defer enc.Close()

	var packets, bytes int
	for i := 0; i < frameCount; i++ {
		pkts, err := enc.Encode(synthFrame(i))
		if err != nil {
			log.Fatal(err)
		}
		for _, p := range pkts {
			packets++
			bytes += len(p.Data)
		}
	}

	// Flush drains the encoder's pipeline (reorder / lookahead) at end of stream.
	tail, err := enc.Flush()
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range tail {
		packets++
		bytes += len(p.Data)
	}

	log.Printf("encoded %d NV12 frames (%dx%d) → %d packets, %d bytes of H.265",
		frameCount, width, height, packets, bytes)
}

// synthFrame builds one tightly-packed NV12 frame (stride == width): a vertical
// luma ramp that scrolls with i over a neutral (gray) chroma plane. NV12 is
// 2-plane 4:2:0 — plane 0 is Y (w*h), plane 1 is interleaved Cb/Cr (w*h/2).
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
		uv[j] = 128 // neutral chroma
	}
	return video.Frame{
		PixelFormat: video.NV12,
		Width:       width,
		Height:      height,
		Planes:      [][]byte{y, uv},
		Strides:     []int{width, width},
	}
}
