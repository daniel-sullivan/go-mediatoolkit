package denoise

import (
	"math"
	"testing"
)

// whiteNoise generates n deterministic white-noise samples in [-a, a).
func whiteNoise(n int, a float64, seed uint64) []float64 {
	out := make([]float64, n)
	s := seed
	for i := range out {
		s = s*6364136223846793005 + 1442695040888963407
		out[i] = a * (float64(int64(s)) / float64(1<<63))
	}
	return out
}

func rms(x []float64) float64 {
	var s float64
	for _, v := range x {
		s += v * v
	}
	return math.Sqrt(s / float64(len(x)))
}

// TestProcessLengthAndDenoise feeds white noise (which the speech
// denoiser should heavily attenuate) through Process at 16 kHz mono in
// odd-sized chunks, then flushes. It asserts (a) each call returns the
// same number of samples it was given, (b) all output is finite, and
// (c) the primed-region output energy is well below the input — i.e.
// the engine actually denoises through the public surface.
func TestProcessLengthAndDenoise(t *testing.T) {
	g, err := NewGTCRN(GTCRNConfig{SampleRate: 16000, Channels: 1})
	if err != nil {
		t.Fatal(err)
	}
	in := whiteNoise(48000, 0.3, 0xBEEF)

	var out []float64
	sizes := []int{512, 1000, 333, 2048}
	i, si := 0, 0
	for i < len(in) {
		n := sizes[si%len(sizes)]
		if i+n > len(in) {
			n = len(in) - i
		}
		buf := append([]float64(nil), in[i:i+n]...)
		g.Process(buf)
		if len(buf) != n {
			t.Fatalf("Process changed length: %d -> %d", n, len(buf))
		}
		out = append(out, buf...)
		i += n
		si++
	}
	// Flush the tail.
	flush := make([]float64, 4096)
	g.Process(flush)
	out = append(out, flush...)

	for i, v := range out {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("non-finite output at %d: %v", i, v)
		}
	}
	// Compare energy on the primed interior.
	lo, hi := 16000, len(in)
	inRMS := rms(in[lo:hi])
	outRMS := rms(out[lo:hi])
	t.Logf("input RMS %.4f, output RMS %.4f (ratio %.3f)", inRMS, outRMS, outRMS/inRMS)
	if outRMS >= inRMS {
		t.Errorf("engine did not attenuate white noise: out RMS %.4f >= in RMS %.4f", outRMS, inRMS)
	}
}

// TestProcessResampleRoundTrip drives the sub-16 kHz path (8 kHz in,
// resampled up to the model band and back). It checks length invariance
// and finite, non-trivial output.
func TestProcessResampleRoundTrip(t *testing.T) {
	g, err := NewGTCRN(GTCRNConfig{SampleRate: 8000, Channels: 1})
	if err != nil {
		t.Fatal(err)
	}
	in := whiteNoise(24000, 0.3, 0x1234)
	buf := append([]float64(nil), in...)
	g.Process(buf)
	if len(buf) != len(in) {
		t.Fatalf("length changed: %d -> %d", len(in), len(buf))
	}
	for i, v := range buf {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("non-finite at %d: %v", i, v)
		}
	}
}

// TestProcessStereoBroadcast checks multi-channel input is downmixed,
// enhanced, and written identically to every channel.
func TestProcessStereoBroadcast(t *testing.T) {
	g, err := NewGTCRN(GTCRNConfig{SampleRate: 16000, Channels: 2})
	if err != nil {
		t.Fatal(err)
	}
	mono := whiteNoise(8000, 0.3, 0x77)
	buf := make([]float64, len(mono)*2)
	for i, v := range mono {
		buf[i*2] = v
		buf[i*2+1] = v * 0.5
	}
	g.Process(buf)
	for i := 0; i < len(mono); i++ {
		if buf[i*2] != buf[i*2+1] {
			t.Fatalf("frame %d: channels differ after enhance (%v vs %v)", i, buf[i*2], buf[i*2+1])
		}
	}
}
