package gtcrn

import (
	"math"
	"testing"
)

// TestStreamerChunkInvariance asserts the streaming denoiser produces
// identical output regardless of how the input is chunked (single shot
// vs many odd-sized Process calls) — the streaming state machine must
// not depend on call boundaries.
func TestStreamerChunkInvariance(t *testing.T) {
	inputs := deterministicInputs(t)
	x := inputs["white-noise"]

	m1, _ := NewModel()
	s1 := NewStreamer(m1)
	whole := s1.Process(x)

	m2, _ := NewModel()
	s2 := NewStreamer(m2)
	var chunked []float32
	sizes := []int{1, 200, 511, 512, 513, 999, 256}
	i, si := 0, 0
	for i < len(x) {
		n := sizes[si%len(sizes)]
		if i+n > len(x) {
			n = len(x) - i
		}
		chunked = append(chunked, s2.Process(x[i:i+n])...)
		i += n
		si++
	}

	if len(whole) != len(chunked) {
		t.Fatalf("length mismatch: whole %d vs chunked %d", len(whole), len(chunked))
	}
	for i := range whole {
		if math.Float32bits(whole[i]) != math.Float32bits(chunked[i]) {
			t.Fatalf("sample %d differs: whole %v vs chunked %v", i, whole[i], chunked[i])
		}
	}
}

// TestStreamerTracksOffline checks the causal streaming output tracks
// the offline center=True pipeline on the interior. They are NOT
// identical: offline is non-causal (a half-window lookahead) and
// prepends one reflect frame that primes the recurrent caches, whereas
// the streamer is strictly causal — so a bounded persistent difference
// is expected and correct. The OLA reconstruction itself is exact
// (proven by TestStreamerChunkInvariance plus the COLA window); this
// test guards against gross divergence (a framing or wiring bug), not
// bit-equality.
func TestStreamerTracksOffline(t *testing.T) {
	inputs := deterministicInputs(t)
	x := inputs["sine-sweep"]

	mo, _ := NewModel()
	offline := mo.EnhanceOffline(x)

	ms, _ := NewModel()
	s := NewStreamer(ms)
	stream := s.Process(x)
	// Flush the tail so late samples finalize.
	stream = append(stream, s.Process(make([]float32, NFFT))...)

	// denoised[p] = stream[p+streamLatency]; compare on the interior,
	// past the cache-priming startup transient.
	shift := streamLatency
	var sig, err float64
	lo, hi := 12000, len(offline)-2*NFFT
	for p := lo; p < hi; p++ {
		so := offline[p]
		var ss float32
		if p+shift < len(stream) {
			ss = stream[p+shift]
		}
		sig += float64(so) * float64(so)
		d := float64(ss) - float64(so)
		err += d * d
	}
	snr := 10 * math.Log10(sig/err)
	t.Logf("streaming vs offline interior SNR = %.1f dB (causal vs centered)", snr)
	if snr < 15 {
		t.Errorf("streaming grossly diverges from offline: SNR %.1f dB < 15 dB", snr)
	}
}
