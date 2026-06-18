//go:build linux

package hwaccel

import (
	"os"
	"testing"

	"go-mediatoolkit/video"
)

// BenchmarkV4L2StatelessHEVCDecode measures the Raspberry Pi 5 stateless
// (Request API) HEVC decode throughput on a real Annex-B .h265 stream supplied
// via V4L2_TEST_HEVC. b.SetBytes uses the decoded NV12 output bytes
// (w*h*3/2 × number of access units) so `go test -bench` reports MB/s; MP/s and
// fps are derived in the writeup. Skips cleanly when no stateless HEVC node is
// present or the stream is not supplied (so it is a no-op everywhere but the Pi).
func BenchmarkV4L2StatelessHEVCDecode(b *testing.B) {
	path := os.Getenv("V4L2_TEST_HEVC")
	if path == "" {
		b.Skip("V4L2_TEST_HEVC not set (path to an Annex-B .h265 stream)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatalf("read %s: %v", path, err)
	}

	back, err := newV4L2Backend()
	if err != nil {
		b.Skipf("v4l2: no M2M video node present: %v", err)
	}
	caps, err := back.Probe()
	if err != nil {
		b.Fatalf("probe: %v", err)
	}
	if !caps.Supports(video.H265, Decode) {
		b.Skip("v4l2: no HEVC decode node on this host")
	}
	var stateless bool
	for _, n := range back.nodes {
		if n.decodes[video.H265] && n.decKind == v4l2KindStateless {
			stateless = true
		}
	}
	if !stateless {
		b.Skip("v4l2: HEVC decode node is not stateless (Request API)")
	}

	aus := splitAccessUnits(data)
	if len(aus) == 0 {
		b.Fatalf("no access units parsed from %s", path)
	}

	// Probe the geometry once so b.SetBytes reflects the real decoded frame
	// size; this also validates the stream decodes before timing.
	probeDec, err := back.NewDecoder(NewConfig(WithCodec(video.H265), WithFrameRate(30, 1)))
	if err != nil {
		b.Fatalf("new decoder: %v", err)
	}
	var probeFrames []video.Frame
	for i, au := range aus {
		fs, derr := probeDec.Decode(video.Packet{Codec: video.H265, Data: au, Keyframe: i == 0})
		if derr != nil {
			b.Fatalf("probe decode AU %d: %v", i, derr)
		}
		probeFrames = append(probeFrames, fs...)
	}
	drained, err := probeDec.Flush()
	if err != nil {
		b.Fatalf("probe flush: %v", err)
	}
	probeFrames = append(probeFrames, drained...)
	if err := probeDec.Close(); err != nil {
		b.Fatalf("close probe decoder: %v", err)
	}
	if len(probeFrames) == 0 {
		b.Fatal("decoder produced no frames")
	}
	w, h := probeFrames[0].Width, probeFrames[0].Height

	b.SetBytes(int64(w*h*3/2) * int64(len(aus)))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		dec, err := back.NewDecoder(NewConfig(WithCodec(video.H265), WithFrameRate(30, 1)))
		if err != nil {
			b.Fatalf("new decoder: %v", err)
		}
		var got int
		for i, au := range aus {
			fs, derr := dec.Decode(video.Packet{Codec: video.H265, Data: au, Keyframe: i == 0})
			if derr != nil {
				b.Fatalf("decode AU %d: %v", i, derr)
			}
			got += len(fs)
		}
		tail, err := dec.Flush()
		if err != nil {
			b.Fatalf("flush: %v", err)
		}
		got += len(tail)
		if err := dec.Close(); err != nil {
			b.Fatalf("close decoder: %v", err)
		}
		if got == 0 {
			b.Fatal("decoder produced no frames")
		}
	}
}
