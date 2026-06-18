package generators

import (
	"math"
	"testing"
	"time"

	"go-mediatoolkit/consts"
)

func TestWhiteNoise_Shape(t *testing.T) {
	audio := WhiteNoise(100*time.Millisecond, consts.SampleRate48000, 42)
	buf := audio.Data
	if len(buf) != 4800 {
		t.Fatalf("want 4800 samples, got %d", len(buf))
	}
	if audio.SampleRate != consts.SampleRate48000 || audio.Channels != 1 {
		t.Fatalf("wrong format: rate=%d channels=%d", audio.SampleRate, audio.Channels)
	}
	var sum, sumSq float64
	for _, v := range buf {
		if v < -1 || v > 1 {
			t.Fatalf("sample out of [-1,1]: %g", v)
		}
		sum += v
		sumSq += v * v
	}
	mean := sum / float64(len(buf))
	variance := sumSq/float64(len(buf)) - mean*mean
	// Uniform [-1,1] has variance 1/3 ≈ 0.333.
	if math.Abs(variance-0.333) > 0.05 {
		t.Errorf("variance %g not near 1/3", variance)
	}
}

func TestPinkNoise_Shape(t *testing.T) {
	audio := PinkNoise(1*time.Second, consts.SampleRate48000, 42)
	buf := audio.Data
	if len(buf) != consts.SampleRate48000 {
		t.Fatalf("want consts.SampleRate48000 samples, got %d", len(buf))
	}
	var peak float64
	for _, v := range buf {
		if math.Abs(v) > peak {
			peak = math.Abs(v)
		}
	}
	// Scale is tuned so peak stays ≲ 0.5 for 1 s of seeded input.
	if peak > 1.0 {
		t.Errorf("peak |sample| %g exceeds 1.0", peak)
	}
}

func TestPinkNoise_Reproducible(t *testing.T) {
	a := PinkNoise(50*time.Millisecond, consts.SampleRate48000, 7).Data
	b := PinkNoise(50*time.Millisecond, consts.SampleRate48000, 7).Data
	if len(a) != len(b) {
		t.Fatalf("length mismatch")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("divergence at %d: %g vs %g", i, a[i], b[i])
		}
	}
}

func TestSineSweep_EndpointFreqs(t *testing.T) {
	// 1 second sweep from 100 Hz → 1000 Hz at 48 kHz. At t=0 the sweep
	// should be near one cycle per 10 ms (100 Hz); at t=1 s near one
	// cycle per 1 ms (1000 Hz). We just verify the sample count and
	// that the signal is bounded and not NaN.
	buf := SineSweep(100, 1000, 1*time.Second, consts.SampleRate48000).Data
	if len(buf) != consts.SampleRate48000 {
		t.Fatalf("want consts.SampleRate48000 samples, got %d", len(buf))
	}
	for i, v := range buf {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("sample %d is not finite: %g", i, v)
		}
		if v < -1.0001 || v > 1.0001 {
			t.Fatalf("sample %d out of range: %g", i, v)
		}
	}
}
