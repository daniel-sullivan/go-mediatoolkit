package aec

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/generators"
)

// BenchmarkProcess_48kStereo measures Canceller.Process's steady-state
// per-frame throughput at 48kHz stereo -- the highest-load supported
// configuration this package accepts (the top supported sample rate;
// Process's cost also scales with CaptureChannels) -- one 10ms AEC3
// frame (480 samples/channel) per b.N iteration. FeedFarEnd is primed
// every iteration too, so the benchmark reflects the full per-frame
// cost a real caller pays driving both halves of the two-stream API
// (see the Canceller type doc comment), not Process in isolation.
func BenchmarkProcess_48kStereo(b *testing.B) {
	const sampleRate = 48000
	const channels = 2
	cfg := CancellerConfig{SampleRate: sampleRate, CaptureChannels: channels, RenderChannels: channels}
	c, err := NewCanceller(cfg)
	if err != nil {
		b.Fatalf("NewCanceller: %v", err)
	}

	const frameSamples = sampleRate / 100 * channels // 10ms, interleaved
	render := make([]float64, frameSamples)
	generators.SineInto(render, 400, sampleRate)
	captureOrig := make([]float64, frameSamples)
	for i := range captureOrig {
		captureOrig[i] = render[i] * 0.3
	}
	buf := make([]float64, frameSamples)

	b.ReportAllocs()
	b.SetBytes(int64(frameSamples) * 8) // float64 capture bytes/frame
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(buf, captureOrig)
		if err := c.FeedFarEnd(render); err != nil {
			b.Fatalf("FeedFarEnd: %v", err)
		}
		c.Process(buf)
	}
}
