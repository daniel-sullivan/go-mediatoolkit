//go:build linux

package hwaccel

import (
	"os"
	"os/exec"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// nv12FrameBytes is the byte size of one NV12 (4:2:0) frame at w×h: a w×h luma
// plane plus a w×(h/2) interleaved chroma plane = w*h*3/2. Used as the per-op
// b.SetBytes basis so `go test -bench` reports hardware throughput in MB/s
// (and, via the writeup, MP/s = MB/s ÷ 1.5).
func nv12FrameBytes(w, h int) int64 { return int64(w * h * 3 / 2) }

// vaEncodePackets synthesises benchFrames NV12 frames and hardware-encodes them
// once, returning the coded packets. Used to feed the decode benchmarks a real
// hardware-coded stream (so the decode loop measures pure decode throughput).
func vaEncodePackets(b *testing.B, back *vaBackend, codec video.Codec) []video.Packet {
	b.Helper()
	enc, err := back.NewEncoder(NewConfig(
		WithCodec(codec),
		WithResolution(vaTestWidth, vaTestHeight),
		WithFrameRate(30, 1),
	))
	if err != nil {
		b.Fatalf("new %s encoder: %v", codec, err)
	}
	var packets []video.Packet
	for i := 0; i < benchFrames; i++ {
		pkts, err := enc.Encode(makeVANV12(vaTestWidth, vaTestHeight, i))
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
	return packets
}

// benchFrames is the number of frames pushed through the hardware per
// benchmark iteration; large enough to amortise per-call overhead.
const benchFrames = 16

// vaProbeOrSkip returns a probed VAAPI backend or skips the benchmark.
func vaProbeOrSkip(b *testing.B) (*vaBackend, Capabilities) {
	b.Helper()
	back, err := newVABackend()
	if err != nil {
		b.Skipf("vaapi unavailable: %v", err)
	}
	caps, err := back.Probe()
	if err != nil {
		b.Fatalf("probe: %v", err)
	}
	return back, caps
}

// benchVADecode drives the decode path for a hardware-coded stream and reports
// MB/s of *decoded NV12 output*. The encode happens once before the timer.
func benchVADecode(b *testing.B, codec video.Codec, packets []video.Packet) {
	back, caps := vaProbeOrSkip(b)
	if !caps.Supports(codec, Decode) {
		b.Skipf("%s decode not supported on this host", codec)
	}
	b.SetBytes(nv12FrameBytes(vaTestWidth, vaTestHeight) * int64(benchFrames))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		dec, err := back.NewDecoder(NewConfig(WithCodec(codec)))
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

// BenchmarkVAAPIH264Decode measures Arc H.264 hardware decode throughput at
// 640×480 NV12. Skips cleanly off the Arc box.
func BenchmarkVAAPIH264Decode(b *testing.B) {
	back, caps := vaProbeOrSkip(b)
	if !caps.Supports(video.H264, Decode) || !caps.Supports(video.H264, Encode) {
		b.Skip("H.264 encode+decode not supported on this host")
	}
	packets := vaEncodePackets(b, back, video.H264)
	benchVADecode(b, video.H264, packets)
}

// BenchmarkVAAPIH265Decode measures Arc H.265 hardware decode throughput.
func BenchmarkVAAPIH265Decode(b *testing.B) {
	back, caps := vaProbeOrSkip(b)
	if !caps.Supports(video.H265, Decode) || !caps.Supports(video.H265, Encode) {
		b.Skip("H.265 encode+decode not supported on this host")
	}
	packets := vaEncodePackets(b, back, video.H265)
	benchVADecode(b, video.H265, packets)
}

// benchVAEncode drives the encode path and reports MB/s of *input NV12*.
func benchVAEncode(b *testing.B, codec video.Codec) {
	back, caps := vaProbeOrSkip(b)
	if !caps.Supports(codec, Encode) {
		b.Skipf("%s encode not supported on this host", codec)
	}
	frames := make([]video.Frame, benchFrames)
	for i := range frames {
		frames[i] = makeVANV12(vaTestWidth, vaTestHeight, i)
	}
	b.SetBytes(nv12FrameBytes(vaTestWidth, vaTestHeight) * int64(benchFrames))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		enc, err := back.NewEncoder(NewConfig(
			WithCodec(codec),
			WithResolution(vaTestWidth, vaTestHeight),
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

// BenchmarkVAAPIH264Encode measures Arc H.264 hardware encode throughput.
func BenchmarkVAAPIH264Encode(b *testing.B) { benchVAEncode(b, video.H264) }

// BenchmarkVAAPIH265Encode measures Arc H.265 (iHD low-power) encode throughput.
func BenchmarkVAAPIH265Encode(b *testing.B) { benchVAEncode(b, video.H265) }

// BenchmarkVAAPIH264ToH265Transcode measures the full NVR transcode path:
// H.264 decode -> H.265 re-encode, end to end, reporting MB/s of input NV12
// equivalent. The H.264 source stream is encoded once before the timer.
func BenchmarkVAAPIH264ToH265Transcode(b *testing.B) {
	back, caps := vaProbeOrSkip(b)
	if !caps.Supports(video.H264, Decode) || !caps.Supports(video.H264, Encode) ||
		!caps.Supports(video.H265, Decode) || !caps.Supports(video.H265, Encode) {
		b.Skip("H.264+H.265 encode+decode not all supported on this host")
	}
	h264 := vaEncodePackets(b, back, video.H264)

	b.SetBytes(nv12FrameBytes(vaTestWidth, vaTestHeight) * int64(benchFrames))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		// Decode H.264.
		dec, err := back.NewDecoder(NewConfig(WithCodec(video.H264)))
		if err != nil {
			b.Fatalf("new h264 decoder: %v", err)
		}
		var mid []video.Frame
		for _, p := range h264 {
			frames, err := dec.Decode(p)
			if err != nil {
				b.Fatalf("h264 decode: %v", err)
			}
			mid = append(mid, frames...)
		}
		drained, err := dec.Flush()
		if err != nil {
			b.Fatalf("h264 flush: %v", err)
		}
		mid = append(mid, drained...)
		if err := dec.Close(); err != nil {
			b.Fatalf("close h264 decoder: %v", err)
		}

		// Re-encode as H.265.
		enc, err := back.NewEncoder(NewConfig(
			WithCodec(video.H265),
			WithResolution(vaTestWidth, vaTestHeight),
			WithFrameRate(30, 1),
		))
		if err != nil {
			b.Fatalf("new h265 encoder: %v", err)
		}
		var out int
		for _, f := range mid {
			pkts, err := enc.Encode(f)
			if err != nil {
				b.Fatalf("h265 encode: %v", err)
			}
			out += len(pkts)
		}
		tail, err := enc.Flush()
		if err != nil {
			b.Fatalf("h265 flush: %v", err)
		}
		out += len(tail)
		if err := enc.Close(); err != nil {
			b.Fatalf("close h265 encoder: %v", err)
		}
		if out == 0 {
			b.Fatal("transcode produced no packets")
		}
	}
}

// vaIVFBenchFrames produces an IVF stream with ffmpeg for the given encoder and
// returns its coded frame payloads, or skips when ffmpeg/the encoder is
// unavailable. Used to feed the VP9/AV1 decode benchmarks (no hardware encode
// for those codecs on the iHD driver).
func vaIVFBenchFrames(b *testing.B, encoder string, extra ...string) [][]byte {
	b.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		b.Skip("ffmpeg not installed (needed to produce a reference stream)")
	}
	tmp := b.TempDir() + "/ref.ivf"
	// All-intra (-g 1): every coded frame is an independent keyframe so the
	// decode loop has no inter-frame reference dependency to manage.
	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=640x480:rate=30:duration=0.6",
		"-pix_fmt", "yuv420p", "-c:v", encoder, "-g", "1"}
	args = append(args, extra...)
	args = append(args, "-f", "ivf", tmp)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		b.Skipf("ffmpeg %s reference encode failed (%v): %s", encoder, err, out)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		b.Fatalf("read ivf: %v", err)
	}
	// Inline IVF frame split (32-byte DKIF header, per-frame 12-byte header).
	if len(data) < 32 || string(data[0:4]) != "DKIF" {
		b.Fatalf("not an IVF file (%d bytes)", len(data))
	}
	var frames [][]byte
	off := 32
	for off+12 <= len(data) {
		sz := int(data[off]) | int(data[off+1])<<8 | int(data[off+2])<<16 | int(data[off+3])<<24
		off += 12
		if off+sz > len(data) {
			break
		}
		frames = append(frames, data[off:off+sz])
		off += sz
	}
	if len(frames) == 0 {
		b.Fatal("no frames in IVF")
	}
	return frames
}

// benchVAIVFDecode decodes a non-Annex-B (VP9/AV1) IVF stream and reports MB/s
// of decoded NV12 output at 640×480.
func benchVAIVFDecode(b *testing.B, codec video.Codec, frames [][]byte) {
	back, caps := vaProbeOrSkip(b)
	if !caps.Supports(codec, Decode) {
		b.Skipf("%s decode not supported on this host", codec)
	}
	b.SetBytes(nv12FrameBytes(vaTestWidth, vaTestHeight) * int64(len(frames)))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		dec, err := back.NewDecoder(NewConfig(WithCodec(codec)))
		if err != nil {
			b.Fatalf("new decoder: %v", err)
		}
		var got int
		for i, fr := range frames {
			// All-intra clip (see vaIVFBenchFrames): every frame is a keyframe.
			out, err := dec.Decode(video.Packet{Codec: codec, Data: fr, Keyframe: true})
			if err != nil {
				b.Fatalf("decode frame %d: %v", i, err)
			}
			got += len(out)
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

// BenchmarkVAAPIVP9Decode measures Arc VP9 hardware decode throughput at
// 640×480 (decode-only: VP9 encode is gated on the iHD driver).
func BenchmarkVAAPIVP9Decode(b *testing.B) {
	frames := vaIVFBenchFrames(b, "libvpx-vp9", "-profile:v", "0")
	benchVAIVFDecode(b, video.VP9, frames)
}

// BenchmarkVAAPIAV1Decode measures Arc AV1 hardware decode throughput at
// 640×480 (decode-only: AV1 encode is gated on the iHD driver).
func BenchmarkVAAPIAV1Decode(b *testing.B) {
	frames := vaIVFBenchFrames(b, "libaom-av1", "-cpu-used", "8")
	benchVAIVFDecode(b, video.AV1, frames)
}
