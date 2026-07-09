//go:build cgo && aec_oracle

package smoke

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	smokeSampleRate    = 16000
	smokeFrameLen      = 160 // 10ms at 16kHz mono
	smokeNumFrames     = 400 // 4s
	smokeDelayFrames   = 5   // 50ms echo path delay
	smokeAttenuation   = 0.5 // near-end echo amplitude vs far-end
	smokeMeasureFrames = 100 // trailing frames used for the ERLE measurement, after convergence
)

// lcgWhiteNoise generates n deterministic pseudo-random float32
// samples in [-1, 1) via a fixed-seed linear congruential generator —
// not math/rand, whose algorithm is not part of its Go-version
// compatibility guarantee — so the far-end signal (and therefore this
// test's expected ERLE/determinism) is reproducible byte-for-byte
// across Go versions and platforms.
func lcgWhiteNoise(n int, seed uint32) []float32 {
	out := make([]float32, n)
	state := seed
	for i := range out {
		state = state*1664525 + 1013904223 // Numerical Recipes LCG constants
		out[i] = float32(int32(state)) / float32(1<<31)
	}
	return out
}

// buildEchoScenario returns (far, near) buffers for a synthetic echo
// scenario: far-end is white noise, near-end is a delayed + attenuated
// copy of far-end (i.e. a pure acoustic echo, no independent near-end
// speech) — the simplest signal an echo canceller must suppress.
func buildEchoScenario() (far, near []float32) {
	far = lcgWhiteNoise(smokeNumFrames*smokeFrameLen, 0xC0FFEE)
	near = make([]float32, len(far))
	delaySamples := smokeDelayFrames * smokeFrameLen
	for i := delaySamples; i < len(near); i++ {
		near[i] = float32(smokeAttenuation) * far[i-delaySamples]
	}
	return far, near
}

// runCanceller feeds far/near through a fresh canceller frame-by-frame
// and returns the full echo-cancelled output buffer.
func runCanceller(t *testing.T, far, near []float32) []float32 {
	t.Helper()
	c, err := newCanceller(smokeSampleRate, 1)
	require.NoError(t, err)
	defer c.close()

	out := make([]float32, len(far))
	for f := 0; f < smokeNumFrames; f++ {
		s := f * smokeFrameLen
		e := s + smokeFrameLen
		require.NoError(t, c.process(far[s:e], near[s:e], out[s:e], smokeFrameLen))
	}
	return out
}

// TestSmokeERLEAndDeterminism proves the fetched AEC3 oracle
// substantially attenuates a pure echo (ERLE > 6dB, measured over the
// trailing smokeMeasureFrames once the adaptive filter has converged)
// and produces bit-identical output across two independent process
// runs — pinning the determinism this repo's future bit-exact parity
// work against a native Go port will depend on.
func TestSmokeERLEAndDeterminism(t *testing.T) {
	far, near := buildEchoScenario()

	out1 := runCanceller(t, far, near)
	out2 := runCanceller(t, far, near)
	require.Equal(t, out1, out2, "AEC3 output must be bit-identical across two independent runs")

	tailStart := (smokeNumFrames - smokeMeasureFrames) * smokeFrameLen
	var inEnergy, outEnergy float64
	for i := tailStart; i < len(near); i++ {
		inEnergy += float64(near[i]) * float64(near[i])
		outEnergy += float64(out1[i]) * float64(out1[i])
	}
	require.Greater(t, outEnergy, 0.0, "output energy unexpectedly zero")
	require.Greater(t, inEnergy, 0.0, "input echo energy unexpectedly zero")

	erle := 10 * math.Log10(inEnergy/outEnergy)
	t.Logf("ERLE over trailing %d frames: %.2f dB", smokeMeasureFrames, erle)
	require.Greater(t, erle, 6.0, "ERLE must exceed 6dB after convergence")
}
