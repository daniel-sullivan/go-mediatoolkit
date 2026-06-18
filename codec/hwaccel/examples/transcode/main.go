// Transcode example: the headline NVR / re-encode path — decode an H.264 stream
// and re-encode it as H.265, entirely on hardware. This is the reason
// codec/hwaccel exists: take an incoming stream off the wire and rewrite it to a
// more efficient codec on whatever fixed-function silicon the host has, without
// the CPU ever touching the pixels.
//
// The pipeline is:
//
//	H.264 packets ──► hwaccel.Decoder ──► video.Frame ──► hwaccel.Encoder ──► H.265 packets
//
// It is self-contained: rather than require an input file, the example first
// synthesises the H.264 source by encoding a short run of NV12 frames (so the
// whole thing is one binary with no assets). All three stages use
// PreferHardware; on a host with no hardware codec the framework falls back
// loudly, Open* returns ErrNoBackend, and the example logs "no hardware codec
// available on this host" and exits 0.
//
// Usage: transcode
package main

import (
	"errors"
	"log"

	"go-mediatoolkit/codec/hwaccel"
	"go-mediatoolkit/video"
)

const (
	srcCodec   = video.H264
	dstCodec   = video.H265
	width      = 1280
	height     = 720
	frameCount = 60
)

func main() {
	log.SetFlags(0)

	// Stage 0: synthesise an H.264 source stream so the example needs no input.
	h264, ok := synthSource()
	if !ok {
		log.Println("no hardware codec available on this host — nothing to transcode")
		return
	}
	srcBytes := totalBytes(h264)
	log.Printf("source: %d %s packets, %d bytes", len(h264), srcCodec, srcBytes)

	// Stage 1: hardware H.264 decoder.
	dec, err := hwaccel.OpenDecoder(
		hwaccel.Policy{Mode: hwaccel.PreferHardware},
		hwaccel.NewConfig(hwaccel.WithCodec(srcCodec)),
	)
	if err != nil {
		if isNoHardware(err) {
			log.Println("no hardware H.264 decoder on this host")
			return
		}
		log.Fatal(err)
	}
	defer dec.Close()

	// Stage 2: hardware H.265 encoder.
	enc, err := hwaccel.OpenEncoder(
		hwaccel.Policy{Mode: hwaccel.PreferHardware},
		hwaccel.NewConfig(
			hwaccel.WithCodec(dstCodec),
			hwaccel.WithResolution(width, height),
			hwaccel.WithBitrate(3_000_000), // HEVC at ~half the bitrate of the H.264 source
			hwaccel.WithFrameRate(30, 1),
			hwaccel.WithPixelFormat(video.NV12),
		),
	)
	if err != nil {
		if isNoHardware(err) {
			log.Println("no hardware H.265 encoder on this host")
			return
		}
		log.Fatal(err)
	}
	defer enc.Close()

	// Drive the pipeline: every decoded frame is fed straight into the encoder.
	var framesDecoded int
	var out []video.Packet
	feed := func(frames []video.Frame) {
		for _, f := range frames {
			framesDecoded++
			pkts, err := enc.Encode(f)
			if err != nil {
				log.Fatal(err)
			}
			out = append(out, pkts...)
		}
	}

	for _, p := range h264 {
		frames, err := dec.Decode(p)
		if err != nil {
			log.Fatal(err)
		}
		feed(frames)
	}
	// Drain the decoder, then the encoder, at end of stream.
	decTail, err := dec.Flush()
	if err != nil {
		log.Fatal(err)
	}
	feed(decTail)
	encTail, err := enc.Flush()
	if err != nil {
		log.Fatal(err)
	}
	out = append(out, encTail...)

	dstBytes := totalBytes(out)
	log.Printf("transcoded %s → %s: decoded %d frames, emitted %d %s packets, %d bytes",
		srcCodec, dstCodec, framesDecoded, len(out), dstCodec, dstBytes)
	if srcBytes > 0 {
		log.Printf("size ratio (dst/src): %.2f", float64(dstBytes)/float64(srcBytes))
	}
}

// synthSource encodes frameCount synthetic NV12 frames to the source codec
// (H.264) on hardware and returns the packets. ok is false when no hardware
// encoder is available.
func synthSource() (pkts []video.Packet, ok bool) {
	enc, err := hwaccel.OpenEncoder(
		hwaccel.Policy{Mode: hwaccel.PreferHardware},
		hwaccel.NewConfig(
			hwaccel.WithCodec(srcCodec),
			hwaccel.WithResolution(width, height),
			hwaccel.WithBitrate(6_000_000),
			hwaccel.WithFrameRate(30, 1),
			hwaccel.WithPixelFormat(video.NV12),
		),
	)
	if err != nil {
		if isNoHardware(err) {
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
	return append(pkts, tail...), true
}

// synthFrame builds one tightly-packed NV12 frame (stride == width): a scrolling
// vertical luma ramp over a neutral chroma plane.
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

func totalBytes(pkts []video.Packet) int {
	n := 0
	for _, p := range pkts {
		n += len(p.Data)
	}
	return n
}

func isNoHardware(err error) bool {
	return errors.Is(err, hwaccel.ErrNoBackend) || errors.Is(err, hwaccel.ErrHardwareUnavailable)
}
