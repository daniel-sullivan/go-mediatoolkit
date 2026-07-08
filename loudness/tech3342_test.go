package loudness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EBU Tech 3342 v4 (Geneva, Nov 2023), Table 1 — Loudness Range (LRA)
// compliance vectors. LRA = P95 − P10 of the doubly-gated (abs −70 LUFS,
// rel −20 LU) short-term loudness distribution, in LU. Tolerance ±1 LU.
//
// Trailing-silence / window-drain note (Tech 3342 §5 MATLAB reference):
// the reference recommends following a file with ≥1.5 s of silence so the
// final 3 s analysis window can drain. Investigated empirically for this
// libebur128-based meter: adding trailing silence is unnecessary AND
// harmful. libebur128 collects a short-term block every 100 ms as audio is
// fed and keeps any block above the −70 LUFS absolute gate; trailing silence
// produces partial-window blocks (window straddling the final tone and the
// silence) that sit well above −70 LUFS and drag the 10th percentile down,
// inflating LRA. The last block over the real content (window fully inside
// the final plateau) is already captured when the signal is fed exactly, so
// we feed each case's tones with NO added trailing silence.
//
// (LRA is unchanged if the signal is repeated one or more times — Tech 3342
// Table 1 note — so a single pass is sufficient.)

func TestTech3342LRACases1To4(t *testing.T) {
	cases := []struct {
		name    string
		segs    []seg
		wantLRA float64
	}{
		{
			// Case 1: "1000 Hz, −20.0 dBFS … 20 s; followed immediately by
			//   the same signal at −30.0 dBFS" (10 dB apart). LRA = 10 ±1 LU.
			"case1 10LU",
			[]seg{tone(20, 1000, -20), tone(20, 1000, -30)},
			10,
		},
		{
			// Case 2: "As #1, with the 2 tones at −20.0 dBFS and −15.0 dBFS"
			//   (5 dB apart). LRA = 5 ±1 LU.
			"case2 5LU",
			[]seg{tone(20, 1000, -20), tone(20, 1000, -15)},
			5,
		},
		{
			// Case 3: "As #1, with the 2 tones at −40.0 dBFS and −20.0 dBFS"
			//   (20 dB apart). LRA = 20 ±1 LU.
			"case3 20LU",
			[]seg{tone(20, 1000, -40), tone(20, 1000, -20)},
			20,
		},
		{
			// Case 4: "As #1, but with 5 tone-segments at −50.0, −35.0,
			//   −20.0, −35.0, −50.0 dBFS; 20 s each." LRA = 15 ±1 LU.
			"case4 15LU",
			[]seg{
				tone(20, 1000, -50), tone(20, 1000, -35), tone(20, 1000, -20),
				tone(20, 1000, -35), tone(20, 1000, -50),
			},
			15,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig := buildStereoSine(tc.segs)
			m, err := NewMeter(synthRate, 2, ModeLRA)
			require.NoError(t, err)
			require.NoError(t, m.AddFrames(sig))
			lra, err := m.Range()
			require.NoError(t, err)
			assert.InDelta(t, tc.wantLRA, lra, 1.0,
				"LRA = %.0f ±1 LU", tc.wantLRA)
		})
	}
}

func TestTech3342LRACases5And6Programme(t *testing.T) {
	// Case 5: "Authentic programme 1, stereo, narrow Loudness Range (NLR)."
	//   Expected: LRA = 5 ±1 LU.
	// Case 6: "Authentic programme 2, stereo, wide Loudness Range (WLR)."
	//   Expected: LRA = 15 ±1 LU.
	// Both require the official EBU programme WAVs (no waveform parameters
	// given), so they cannot be synthesized. Kept so the skip is visible.
	for _, name := range []string{"case5 NLR programme", "case6 WLR programme"} {
		t.Run(name, func(t *testing.T) {
			t.Skip("requires authentic EBU programme WAV (not synthesizable from the document text)")
		})
	}
}
