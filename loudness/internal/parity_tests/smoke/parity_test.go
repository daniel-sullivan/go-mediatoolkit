//go:build cgo

package smoke

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSmokeOracleLinksAndRuns proves the vendored libebur128 cgo
// oracle links and runs end-to-end: init a stereo 48 kHz
// integrated-loudness meter, feed it a 5 s 997 Hz sine at 0.5
// amplitude, read back the integrated loudness, and destroy the
// state. 997 Hz (not an exact 1 kHz) is the traditional test-tone
// choice — a frequency that does not align neatly with block/frame
// boundaries.
//
// This is deliberately a coarse smoke check, not a parity assertion —
// Phase B's parity slices (kweight, gatingblock, integrated, windows,
// lra, truepeak, e2e) pin exact expected values against the pure-Go
// port once it exists. Here we only assert the ebur128_init /
// ebur128_add_frames_double / ebur128_loudness_global /
// ebur128_destroy call sequence works and the result is finite and in
// a plausible range. A 0.5-amplitude 997 Hz sine measures
// approximately -9.05 LUFS once K-weighted and gated (a full-scale
// sine's RMS alone is -3.01 dBFS; BS.1770's shelf+RLB K-weighting
// filters shift the reading up several LU from that at midband
// frequencies) — checked here only loosely as [-12, -6] LUFS since
// this is a smoke test, not a precision one.
func TestSmokeOracleLinksAndRuns(t *testing.T) {
	const (
		channels   = 2
		sampleRate = 48000
		durationS  = 5
		freqHz     = 997.0
		amplitude  = 0.5
	)

	frames := sampleRate * durationS
	buf := make([]float64, frames*channels)
	for i := 0; i < frames; i++ {
		v := amplitude * math.Sin(2*math.Pi*freqHz*float64(i)/sampleRate)
		buf[i*channels+0] = v
		buf[i*channels+1] = v
	}

	got, err := smokeMeasureIntegratedLoudness(channels, sampleRate, buf)
	require.NoError(t, err)
	require.GreaterOrEqual(t, got, -12.0, "loudness implausibly low: %v LUFS", got)
	require.LessOrEqual(t, got, -6.0, "loudness implausibly high: %v LUFS", got)
}
