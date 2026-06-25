//go:build darwin

package hwaccel

import (
	"os"
	"os/exec"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// vtNV12FrameBytes is the byte size of one NV12 (4:2:0) frame at w×h
// (w*h*3/2). Used as the b.SetBytes basis so the bench reports hardware MB/s
// (MP/s = MB/s ÷ 1.5).
func vtNV12FrameBytes(w, h int) int64 { return int64(w * h * 3 / 2) }

// vtBenchFrames is the number of frames driven through VideoToolbox per
// benchmark iteration.
const vtBenchFrames = 16

// vtProbeOrSkip returns a probed VideoToolbox backend or skips the benchmark.
func vtProbeOrSkip(b *testing.B) (*vtBackend, Capabilities) {
	b.Helper()
	back, err := newVTBackend()
	if err != nil {
		b.Skipf("videotoolbox unavailable: %v", err)
	}
	caps, err := back.Probe()
	if err != nil {
		b.Fatalf("probe: %v", err)
	}
	return back, caps
}

// vtEncodePackets encodes vtBenchFrames NV12 frames once, returning the coded
// packets for the decode benchmarks.
func vtEncodePackets(b *testing.B, back *vtBackend, codec video.Codec) []video.Packet {
	b.Helper()
	enc, err := back.NewEncoder(NewConfig(
		WithCodec(codec),
		WithResolution(testWidth, testHeight),
		WithBitrate(8_000_000),
		WithFrameRate(30, 1),
		WithKeyframeInterval(15),
	))
	if err != nil {
		b.Fatalf("new %s encoder: %v", codec, err)
	}
	var packets []video.Packet
	for i := 0; i < vtBenchFrames; i++ {
		pkts, err := enc.Encode(makeNV12(testWidth, testHeight, i))
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

// benchVTEncode drives the VideoToolbox encode path reporting MB/s of input NV12.
func benchVTEncode(b *testing.B, codec video.Codec) {
	back, caps := vtProbeOrSkip(b)
	if !caps.Supports(codec, Encode) {
		b.Skipf("%s hardware encode not supported on this host", codec)
	}
	frames := make([]video.Frame, vtBenchFrames)
	for i := range frames {
		frames[i] = makeNV12(testWidth, testHeight, i)
	}
	b.SetBytes(vtNV12FrameBytes(testWidth, testHeight) * int64(vtBenchFrames))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		enc, err := back.NewEncoder(NewConfig(
			WithCodec(codec),
			WithResolution(testWidth, testHeight),
			WithBitrate(8_000_000),
			WithFrameRate(30, 1),
			WithKeyframeInterval(15),
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

// benchVTDecode drives the VideoToolbox decode path for a coded stream,
// reporting MB/s of decoded NV12 output. Encode happens once before the timer.
func benchVTDecode(b *testing.B, codec video.Codec, packets []video.Packet) {
	back, caps := vtProbeOrSkip(b)
	if !caps.Supports(codec, Decode) {
		b.Skipf("%s hardware decode not supported on this host", codec)
	}
	b.SetBytes(vtNV12FrameBytes(testWidth, testHeight) * int64(vtBenchFrames))
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

// BenchmarkVideoToolboxH264Encode measures Apple VT H.264 encode throughput at
// 640×480 NV12.
func BenchmarkVideoToolboxH264Encode(b *testing.B) { benchVTEncode(b, video.H264) }

// BenchmarkVideoToolboxH265Encode measures Apple VT H.265 encode throughput.
func BenchmarkVideoToolboxH265Encode(b *testing.B) { benchVTEncode(b, video.H265) }

// BenchmarkVideoToolboxH264Decode measures Apple VT H.264 decode throughput.
func BenchmarkVideoToolboxH264Decode(b *testing.B) {
	back, caps := vtProbeOrSkip(b)
	if !caps.Supports(video.H264, Encode) || !caps.Supports(video.H264, Decode) {
		b.Skip("H.264 encode+decode not supported on this host")
	}
	packets := vtEncodePackets(b, back, video.H264)
	benchVTDecode(b, video.H264, packets)
}

// BenchmarkVideoToolboxH265Decode measures Apple VT H.265 decode throughput.
func BenchmarkVideoToolboxH265Decode(b *testing.B) {
	back, caps := vtProbeOrSkip(b)
	if !caps.Supports(video.H265, Encode) || !caps.Supports(video.H265, Decode) {
		b.Skip("H.265 encode+decode not supported on this host")
	}
	packets := vtEncodePackets(b, back, video.H265)
	benchVTDecode(b, video.H265, packets)
}

// BenchmarkVideoToolboxAV1Decode measures Apple VT AV1 decode throughput at
// 320×240 (the resolution of the ffmpeg reference clip; AV1 hardware decode is
// M3+ only). Skips when AV1 hardware decode is unavailable.
func BenchmarkVideoToolboxAV1Decode(b *testing.B) {
	back, caps := vtProbeOrSkip(b)
	if !caps.Supports(video.AV1, Decode) {
		b.Skip("AV1 hardware decode not supported on this host")
	}
	// vtFfmpegIVF / vtIVFFrames live in videotoolbox_av1vp9_darwin_test.go.
	data := vtFfmpegIVFBench(b, "libaom-av1", "-cpu-used", "8")
	frames := vtIVFFramesBench(b, data)
	const w, h = 320, 240
	b.SetBytes(vtNV12FrameBytes(w, h) * int64(len(frames)))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		dec, err := back.NewDecoder(NewConfig(WithCodec(video.AV1)))
		if err != nil {
			b.Fatalf("new decoder: %v", err)
		}
		var got int
		for i, fr := range frames {
			// All-intra clip (see vtFfmpegIVFBench): every frame is a keyframe.
			out, err := dec.Decode(video.Packet{Codec: video.AV1, Data: fr, Keyframe: true})
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

// vtFfmpegIVFBench / vtIVFFramesBench mirror the *_test helpers but take a
// *testing.B (the test helpers take *testing.T).
func vtFfmpegIVFBench(b *testing.B, encoder string, extra ...string) []byte {
	b.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		b.Skip("ffmpeg not installed")
	}
	tmp := b.TempDir() + "/ref.ivf"
	// All-intra (-g 1): every frame is an independent keyframe, so the decode
	// benchmark needs no inter-frame reference chain (VideoToolbox's AV1 path
	// decodes each TU standalone). Keeps the clip robust across frame counts.
	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=30:duration=0.6",
		"-pix_fmt", "yuv420p", "-c:v", encoder, "-g", "1"}
	args = append(args, extra...)
	args = append(args, "-f", "ivf", tmp)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		b.Skipf("ffmpeg %s failed (%v): %s", encoder, err, out)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		b.Fatalf("read ivf: %v", err)
	}
	return data
}

func vtIVFFramesBench(b *testing.B, data []byte) [][]byte {
	b.Helper()
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
