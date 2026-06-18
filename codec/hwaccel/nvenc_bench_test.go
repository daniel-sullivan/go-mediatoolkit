//go:build linux

package hwaccel

import (
	"testing"

	"go-mediatoolkit/video"
)

// BenchmarkNVENCH264Encode measures NVIDIA NVENC H.264 encode throughput at
// 640×480 NV12, reporting MB/s of input NV12 (MP/s = MB/s ÷ 1.5). It skips
// cleanly when no NVIDIA hardware is present, so it is a no-op on the Arc box
// and any NVIDIA-less host. UNRUN here (no NVIDIA device was available); present
// so a real GPU box can produce the number with `go test -bench`.
func BenchmarkNVENCH264Encode(b *testing.B) {
	back, err := newNVBackend()
	if err != nil {
		b.Skipf("nvenc unavailable (no NVIDIA hardware): %v", err)
	}
	caps, err := back.Probe()
	if err != nil {
		b.Fatalf("probe: %v", err)
	}
	if !caps.Supports(video.H264, Encode) {
		b.Skip("H.264 encode not supported on this NVIDIA host")
	}

	frames := make([]video.Frame, nvTestFrames)
	for i := range frames {
		frames[i] = makeNVNV12(nvTestWidth, nvTestHeight, i)
	}
	b.SetBytes(int64(nvTestWidth*nvTestHeight*3/2) * int64(nvTestFrames))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		enc, err := back.NewEncoder(NewConfig(
			WithCodec(video.H264),
			WithResolution(nvTestWidth, nvTestHeight),
			WithBitrate(6_000_000),
			WithFrameRate(30, 1),
		))
		if err != nil {
			b.Fatalf("new encoder: %v", err)
		}
		var got int
		for _, f := range frames {
			pkts, err := enc.Encode(f)
			if err != nil {
				b.Fatalf("encode: %v", err)
			}
			got += len(pkts)
		}
		tail, err := enc.Flush()
		if err != nil {
			b.Fatalf("flush: %v", err)
		}
		got += len(tail)
		if err := enc.Close(); err != nil {
			b.Fatalf("close encoder: %v", err)
		}
		if got == 0 {
			b.Fatal("encoder produced no packets")
		}
	}
}

// BenchmarkNVDECH264Decode measures NVIDIA NVDEC H.264 decode throughput.
// Skips cleanly without NVIDIA hardware; UNRUN here.
func BenchmarkNVDECH264Decode(b *testing.B) {
	back, err := newNVBackend()
	if err != nil {
		b.Skipf("nvenc unavailable (no NVIDIA hardware): %v", err)
	}
	caps, err := back.Probe()
	if err != nil {
		b.Fatalf("probe: %v", err)
	}
	if !caps.Supports(video.H264, Encode) || !caps.Supports(video.H264, Decode) {
		b.Skip("H.264 encode+decode not supported on this NVIDIA host")
	}

	// Encode a stream once to feed the decoder.
	enc, err := back.NewEncoder(NewConfig(
		WithCodec(video.H264),
		WithResolution(nvTestWidth, nvTestHeight),
		WithBitrate(6_000_000),
		WithFrameRate(30, 1),
	))
	if err != nil {
		b.Fatalf("new encoder: %v", err)
	}
	var packets []video.Packet
	for i := 0; i < nvTestFrames; i++ {
		pkts, err := enc.Encode(makeNVNV12(nvTestWidth, nvTestHeight, i))
		if err != nil {
			b.Fatalf("encode frame %d: %v", i, err)
		}
		packets = append(packets, pkts...)
	}
	tail, err := enc.Flush()
	if err != nil {
		b.Fatalf("flush: %v", err)
	}
	packets = append(packets, tail...)
	if err := enc.Close(); err != nil {
		b.Fatalf("close encoder: %v", err)
	}
	if len(packets) == 0 {
		b.Fatal("encoder produced no packets")
	}

	b.SetBytes(int64(nvTestWidth*nvTestHeight*3/2) * int64(nvTestFrames))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		dec, err := back.NewDecoder(NewConfig(WithCodec(video.H264)))
		if err != nil {
			b.Fatalf("new decoder: %v", err)
		}
		var got int
		for _, p := range packets {
			frames, err := dec.Decode(p)
			if err != nil {
				b.Fatalf("decode: %v", err)
			}
			got += len(frames)
		}
		drained, err := dec.Flush()
		if err != nil {
			b.Fatalf("flush: %v", err)
		}
		got += len(drained)
		if err := dec.Close(); err != nil {
			b.Fatalf("close decoder: %v", err)
		}
		if got == 0 {
			b.Fatal("decoder produced no frames")
		}
	}
}
