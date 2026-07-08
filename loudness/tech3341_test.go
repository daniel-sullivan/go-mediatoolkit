package loudness

import (
	"fmt"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EBU Tech 3341 v4 (Geneva, Nov 2023), Table 1 — minimum-requirements
// compliance vectors. The meter under test (Meter, over the bit-exact
// libebur128 v1.2.6 port) is parity-verified against libebur128, which is
// known to satisfy the Tech 3341 minimum requirements — so these cases
// validate signal synthesis as much as metering.
//
// Tolerances are verbatim from the document: ±0.1 LU for M/S/I; true-peak
// +0.2/−0.4 dB. All levels are PEAK dBFS (see helpers_test.go).

const (
	// chunk100ms is the 100 ms meter-update block used to poll M/S. Tech
	// 3341 §2.1 sets the short-term live update rate ≥10 Hz; polling every
	// 100 ms is that cadence and defines the "max reading" semantics.
	chunk100ms = synthRate / 10 // 4800 frames

	// tol is the ±0.1 LU loudness tolerance (Table 1, cases 1–14).
	tol = 0.1
)

// ── Cases 1–2: steady stereo 1 kHz tone; M, S, I all asserted ────────────

func TestTech3341Cases1And2(t *testing.T) {
	// Case 1: "Stereo sine wave, 1000 Hz, −23.0 dBFS (per-channel peak
	//   level) … 20 s duration." Expected: M, S, I = −23.0 ±0.1 LUFS.
	// Case 2: "As #1 at −33.0 dBFS." Expected: M, S, I = −33.0 ±0.1 LUFS.
	cases := []struct {
		name  string
		dbfs  float64
		wantL float64 // expected LUFS
	}{
		{"case1 -23dBFS", -23.0, -23.0},
		{"case2 -33dBFS", -33.0, -33.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig := buildStereoSine([]seg{tone(20, 1000, tc.dbfs)})
			m, err := NewMeter(synthRate, 2, ModeIntegrated|ModeShortTerm)
			require.NoError(t, err)
			// Poll M and S at the 100 ms cadence; the steady tone drives M
			// and S to the tone level once their windows fill.
			maxM, _ := feedAndSampleMax(t, m, sig, 2, chunk100ms, m.Momentary)

			// Re-feed for a clean short-term poll (fresh meter keeps this
			// simple and independent of poll ordering).
			ms, err := NewMeter(synthRate, 2, ModeShortTerm)
			require.NoError(t, err)
			maxS, _ := feedAndSampleMax(t, ms, sig, 2, chunk100ms, ms.ShortTerm)

			i, err := m.Integrated()
			require.NoError(t, err)

			assert.InDelta(t, tc.wantL, maxM, tol, "M = %.1f ±0.1 LUFS", tc.wantL)
			assert.InDelta(t, tc.wantL, maxS, tol, "S = %.1f ±0.1 LUFS", tc.wantL)
			assert.InDelta(t, tc.wantL, i, tol, "I = %.1f ±0.1 LUFS", tc.wantL)
		})
	}
}

// ── Cases 3–5: gated integrated loudness over multi-level tone sequences ──

func TestTech3341Cases3To5(t *testing.T) {
	cases := []struct {
		name string
		segs []seg
	}{
		{
			// Case 3: "3 tones … 10 s at −36.0 dBFS; 60 s at −23.0 dBFS;
			//   10 s at −36.0 dBFS." Expected: I = −23.0 ±0.1 LUFS. The
			//   −36 dBFS bookends sit below the −10 LU relative gate and
			//   are excluded.
			"case3",
			[]seg{tone(10, 1000, -36), tone(60, 1000, -23), tone(10, 1000, -36)},
		},
		{
			// Case 4: "5 tones … 10 s at −72.0 dBFS; 10 s at −36.0 dBFS;
			//   60 s at −23.0 dBFS; 10 s at −36.0 dBFS; 10 s at −72.0 dBFS."
			//   Expected: I = −23.0 ±0.1 LUFS. −72 dBFS excluded by the
			//   absolute (−70 LUFS) gate, −36 dBFS by the relative gate.
			"case4",
			[]seg{
				tone(10, 1000, -72), tone(10, 1000, -36), tone(60, 1000, -23),
				tone(10, 1000, -36), tone(10, 1000, -72),
			},
		},
		{
			// Case 5: "3 tones … 20 s at −26.0 dBFS; 20.1 s at −20.0 dBFS;
			//   20 s at −26.0 dBFS." Expected: I = −23.0 ±0.1 LUFS. Here the
			//   −26 dBFS bookends are only 3 LU below programme, so they stay
			//   inside the −10 LU relative gate and all three segments
			//   contribute. The 20.1 s middle length is transcribed exactly
			//   as printed (asymmetry intentional; no stated rationale).
			"case5",
			[]seg{tone(20, 1000, -26), tone(20.1, 1000, -20), tone(20, 1000, -26)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig := buildStereoSine(tc.segs)
			m, err := NewMeter(synthRate, 2, ModeIntegrated)
			require.NoError(t, err)
			require.NoError(t, m.AddFrames(sig))
			i, err := m.Integrated()
			require.NoError(t, err)
			// Expected: I = −23.0 ±0.1 LUFS.
			assert.InDelta(t, -23.0, i, tol, "I = −23.0 ±0.1 LUFS")
		})
	}
}

// ── Case 6: 5.0-channel integrated loudness (surround weighting) ──────────

func TestTech3341Case6FiveChannel(t *testing.T) {
	// Case 6: "5.0 channel sine wave, 1000 Hz, 20 s duration, with
	//   per-channel peak levels … −28.0 dBFS in L and R; −24.0 dBFS in C;
	//   −30.0 dBFS in Ls and Rs." Expected: I = −23.0 ±0.1 LUFS.
	//
	// Hitting −23.0 requires BS.1770's +1.5 dB (×1.41) surround-channel
	// weighting on Ls/Rs — inherited from BS.1770, not restated in Tech
	// 3341. The default 5-channel map here is L,R,C,Ls,Rs (verified against
	// loudness.go / the r128 port), which applies that weighting, so no
	// SetChannel override is needed.
	amps := []float64{
		sinePeakAmplitude(-28), // L
		sinePeakAmplitude(-28), // R
		sinePeakAmplitude(-24), // C
		sinePeakAmplitude(-30), // Ls
		sinePeakAmplitude(-30), // Rs
	}
	sig := buildMultiChannelSine(1000, 20, amps)
	m, err := NewMeter(synthRate, 5, ModeIntegrated)
	require.NoError(t, err)
	require.NoError(t, m.AddFrames(sig))
	i, err := m.Integrated()
	require.NoError(t, err)
	assert.InDelta(t, -23.0, i, tol, "I = −23.0 ±0.1 LUFS")
}

// ── Cases 7/8: authentic programme audio — not synthesizable ─────────────

func TestTech3341Cases7And8Programme(t *testing.T) {
	// Case 7: "Authentic programme 1, stereo, narrow loudness range (NLR)."
	//   Expected: I = −23.0 ±0.1 LUFS.
	// Case 8: "Authentic programme 2, stereo, wide loudness range (WLR)."
	//   Expected: I = −23.0 ±0.1 LUFS.
	// Both require the official EBU programme WAVs (no waveform parameters
	// are given in the document — only a qualitative genre description), so
	// they cannot be synthesized. Kept in the suite so the skip is visible.
	for _, name := range []string{"case7 NLR programme", "case8 WLR programme"} {
		t.Run(name, func(t *testing.T) {
			t.Skip("requires authentic EBU programme WAV (not synthesizable from the document text)")
		})
	}
}

// ── Case 9: max short-term is constant after 3 s ─────────────────────────

func TestTech3341Case9ShortTermConstant(t *testing.T) {
	// Case 9: "2 tones … (1.34 s at −20.0 dBFS; 1.66 s at −30.0 dBFS)
	//   repeated 5 times." Expected: S = −23.0 ±0.1 LUFS, constant after 3 s.
	//
	// The repeat unit is 1.34 + 1.66 = 3.0 s = the short-term window length,
	// so once the sliding 3 s window is fully inside the periodic region
	// (t ≥ 3 s) it always spans exactly one full period → constant S. We
	// verify both the max reading and that every reading at/after 4 s (a
	// margin past the 3 s onset) sits within ±0.1 LUFS of −23.0.
	var segs []seg
	for r := 0; r < 5; r++ {
		segs = append(segs, tone(1.34, 1000, -20), tone(1.66, 1000, -30))
	}
	sig := buildStereoSine(segs)
	m, err := NewMeter(synthRate, 2, ModeShortTerm)
	require.NoError(t, err)
	maxS, readings := feedAndSampleMax(t, m, sig, 2, chunk100ms, m.ShortTerm)
	assert.InDelta(t, -23.0, maxS, tol, "max S = −23.0 ±0.1 LUFS")

	// readings[k] is the S poll after (k+1)*100 ms. Assert steady-state
	// from t ≥ 4 s (index 40) onward — "constant after 3 s" with margin.
	for k := 40; k < len(readings); k++ {
		assert.InDelta(t, -23.0, readings[k], tol,
			"S constant after 3 s: reading at %.1f s", float64(k+1)*0.1)
	}
}

// ── Case 12: max momentary is constant after 1 s ─────────────────────────

func TestTech3341Case12MomentaryConstant(t *testing.T) {
	// Case 12: "2 tones … (0.18 s at −20.0 dBFS; 0.22 s at −30.0 dBFS)
	//   repeated 25 times." Expected: M = −23.0 ±0.1 LUFS, constant after 1 s.
	//
	// Same mechanism as case 9 but for the 0.4 s momentary window: the
	// repeat unit is 0.18 + 0.22 = 0.4 s = the momentary window length, so
	// M is constant once the window is filled (t ≥ 0.4 s); "constant after
	// 1 s" gives a transient margin. Verified at ±0.1 LUFS from t ≥ 1 s.
	var segs []seg
	for r := 0; r < 25; r++ {
		segs = append(segs, tone(0.18, 1000, -20), tone(0.22, 1000, -30))
	}
	sig := buildStereoSine(segs)
	m, err := NewMeter(synthRate, 2, ModeMomentary)
	require.NoError(t, err)
	maxM, readings := feedAndSampleMax(t, m, sig, 2, chunk100ms, m.Momentary)
	assert.InDelta(t, -23.0, maxM, tol, "max M = −23.0 ±0.1 LUFS")

	// readings[k] after (k+1)*100 ms; assert steady state from t ≥ 1 s.
	for k := 10; k < len(readings); k++ {
		assert.InDelta(t, -23.0, readings[k], tol,
			"M constant after 1 s: reading at %.1f s", float64(k+1)*0.1)
	}
}

// ── Cases 10/13: file-based per-segment max S / max M ────────────────────

func TestTech3341Case10FileShortTerm(t *testing.T) {
	// Case 10 (file-based meters): "20 segments … (i * 0.15 s of silence;
	//   3 s at −23.0 dBFS; 1 s of silence) for i = 0..19." Expected: max
	//   S = −23.0 ±0.1 LUFS, for each segment (each measured on a fresh
	//   meter).
	//
	// The 3 s tone equals the short-term window, so a window position exists
	// where it spans exactly the tone → S = −23.0. We read S at the tone's
	// end (the aligned window), which is that maximum.
	for i := 0; i < 20; i++ {
		m, err := NewMeter(synthRate, 2, ModeShortTerm)
		require.NoError(t, err)
		lead := buildStereoSine([]seg{silence(float64(i) * 0.15), tone(3, 1000, -23)})
		require.NoError(t, m.AddFrames(lead))
		s, err := m.ShortTerm() // trailing 3 s window == the tone
		require.NoError(t, err)
		assert.InDelta(t, -23.0, s, tol, "segment i=%d: max S = −23.0 ±0.1 LUFS", i)
	}
}

func TestTech3341Case13FileMomentary(t *testing.T) {
	// Case 13 (file-based meters): "20 segments … (i * 20 ms of silence;
	//   400 ms at −23.0 dBFS; 1 s of silence) for i = 0..19." Expected: max
	//   M = −23.0 ±0.1 LUFS, for each segment.
	//
	// The 20 ms silence increment deliberately shifts the tone onset off the
	// 100 ms gating grid, so a meter that only polled on the coarse grid
	// would miss the peak. We read M at the tone's end, where the 400 ms
	// window spans exactly the 400 ms tone → M = −23.0.
	for i := 0; i < 20; i++ {
		m, err := NewMeter(synthRate, 2, ModeMomentary)
		require.NoError(t, err)
		lead := buildStereoSine([]seg{silence(float64(i) * 0.02), tone(0.4, 1000, -23)})
		require.NoError(t, m.AddFrames(lead))
		mm, err := m.Momentary() // trailing 400 ms window == the tone
		require.NoError(t, err)
		assert.InDelta(t, -23.0, mm, tol, "segment i=%d: max M = −23.0 ±0.1 LUFS", i)
	}
}

// ── Cases 11/14: live per-segment successive max readings ────────────────

func TestTech3341Case11LiveShortTerm(t *testing.T) {
	// Case 11 (live meters): "20 tones … (i * 0.15 s of silence; 3 s at
	//   −38.0+i dBFS; 3 − i * 0.15 s of silence) for i = 0..19." Expected:
	//   max S = −38.0, −37.0, …, −19.0 ±0.1 LUFS, successive values (segment
	//   i → −38.0 + i). One continuous stream; each per-i sub-segment is
	//   exactly 6.0 s.
	//
	// We read S at each tone's end, where the trailing 3 s window spans
	// exactly that segment's 3 s tone → S = −38.0 + i (the running max for a
	// monotonically increasing sequence).
	m, err := NewMeter(synthRate, 2, ModeShortTerm)
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		want := -38.0 + float64(i)
		lead := buildStereoSine([]seg{
			silence(float64(i) * 0.15),
			tone(3, 1000, want),
		})
		require.NoError(t, m.AddFrames(lead))
		s, err := m.ShortTerm()
		require.NoError(t, err)
		assert.InDelta(t, want, s, tol, "segment i=%d: max S = %.1f ±0.1 LUFS", i, want)

		trail := buildStereoSine([]seg{silence(3 - float64(i)*0.15)})
		require.NoError(t, m.AddFrames(trail))
	}
}

func TestTech3341Case14LiveMomentary(t *testing.T) {
	// Case 14 (live meters): "20 tones … (i * 20 ms of silence; 400 ms at
	//   −38.0+i dBFS; 400 − i * 20 ms of silence) for i = 0..19." Expected:
	//   max M = −38.0, −37.0, …, −19.0 ±0.1 LUFS, successive values. One
	//   continuous stream; each sub-segment is exactly 800 ms.
	//
	// We read M at each tone's end, where the trailing 400 ms window spans
	// exactly that segment's 400 ms tone → M = −38.0 + i.
	m, err := NewMeter(synthRate, 2, ModeMomentary)
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		want := -38.0 + float64(i)
		lead := buildStereoSine([]seg{
			silence(float64(i) * 0.02),
			tone(0.4, 1000, want),
		})
		require.NoError(t, m.AddFrames(lead))
		mm, err := m.Momentary()
		require.NoError(t, err)
		assert.InDelta(t, want, mm, tol, "segment i=%d: max M = %.1f ±0.1 LUFS", i, want)

		trail := buildStereoSine([]seg{silence(0.4 - float64(i)*0.02)})
		require.NoError(t, m.AddFrames(trail))
	}
}

// ── Cases 15–19: true peak ───────────────────────────────────────────────

func TestTech3341Cases15To19TruePeak(t *testing.T) {
	// True-peak tones synthesized directly at fs (48 kHz). Expected max
	// true-peak level with tolerance +0.2/−0.4 dBTP (Table 1).
	//
	// Fade resolution (ambiguity #4): the "10 ms fade-in and fade-out" is
	// stated explicitly only for case 15; cases 16–19 neither restate it nor
	// use "As #15 but …" phrasing. Following the transcription's
	// recommendation, we apply the same 10 ms linear fade to all of 15–19
	// (conservative — it only suppresses onset/offset transients and cannot
	// inflate the steady-state inter-sample peak under test).
	const fade = 0.010 // 10 ms
	fsOver4 := float64(synthRate) / 4
	fsOver6 := float64(synthRate) / 6
	fsOver8 := float64(synthRate) / 8

	cases := []struct {
		name   string
		freq   float64
		amp    float64
		phase  float64
		wantTP float64 // expected dBTP
	}{
		// Case 15: fs/4, amplitude 0.50 FFS, phase 0.0°. → −6.0 dBTP.
		{"case15 fs/4 0.50 0deg", fsOver4, 0.50, 0.0, -6.0},
		// Case 16: fs/4, amplitude 0.50 FFS, phase 45.0°. → −6.0 dBTP.
		{"case16 fs/4 0.50 45deg", fsOver4, 0.50, 45.0, -6.0},
		// Case 17: fs/6, amplitude 0.50 FFS, phase 60.0°. → −6.0 dBTP.
		{"case17 fs/6 0.50 60deg", fsOver6, 0.50, 60.0, -6.0},
		// Case 18: fs/8, amplitude 0.50 FFS, phase 67.5°. → −6.0 dBTP.
		{"case18 fs/8 0.50 67.5deg", fsOver8, 0.50, 67.5, -6.0},
		// Case 19: fs/4, amplitude 1.41 FFS, phase 45.0°. → +3.0 dBTP
		//   (inter-sample over beyond 0 dBFS sample peak; amplitude
		//   legitimately exceeds full scale).
		{"case19 fs/4 1.41 45deg", fsOver4, 1.41, 45.0, +3.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig := buildTruePeakTone(tc.freq, tc.amp, tc.phase, 0.5, fade)
			m, err := NewMeter(synthRate, 2, ModeTruePeak)
			require.NoError(t, err)
			require.NoError(t, m.AddFrames(sig))

			peak := 0.0
			for ch := 0; ch < 2; ch++ {
				p, err := m.TruePeak(ch)
				require.NoError(t, err)
				if p > peak {
					peak = p
				}
			}
			dbtp := mutations.AmplitudeToDecibels(peak)
			// Tolerance +0.2/−0.4 dBTP: dbtp ∈ [want−0.4, want+0.2].
			assert.GreaterOrEqual(t, dbtp, tc.wantTP-0.4,
				"true peak %.3f dBTP below want %.1f −0.4", dbtp, tc.wantTP)
			assert.LessOrEqual(t, dbtp, tc.wantTP+0.2,
				"true peak %.3f dBTP above want %.1f +0.2", dbtp, tc.wantTP)
		})
	}
}

// ── §2.9 calibration / alignment check ────────────────────────────────────

func TestTech3341CalibrationCheck(t *testing.T) {
	// Tech 3341 §2.9: "a 1 kHz stereo sine-wave (signal applied in phase to
	//   both channels simultaneously), with its peak level at −18 dBFS … The
	//   meter should read −18.0 LUFS." Tolerance ±0.1 LUFS.
	//
	// §2.9 flags this point as especially frequency-sensitive: 1 kHz sits on
	// a slope of the K-weighting pre-filter, so an error in the tone's
	// frequency shifts the result. We therefore synthesize at exactly
	// 1000.0 Hz (not an approximation), which is the condition under which
	// −18.0 LUFS is expected.
	sig := buildStereoSine([]seg{tone(5, 1000, -18)})
	m, err := NewMeter(synthRate, 2, ModeIntegrated)
	require.NoError(t, err)
	require.NoError(t, m.AddFrames(sig))
	i, err := m.Integrated()
	require.NoError(t, err)
	assert.InDelta(t, -18.0, i, tol, "calibration: −18.0 LUFS ±0.1")
}

// ── Cases 20–23: 4×-oversampled, downsample-phase-offset true peak ────────

func TestTech3341Cases20To23TruePeak(t *testing.T) {
	// Cases 20–23 (Table 1): a stereo fs/6 Hz sine at 0.50 FFS containing a
	// single period of an fs/4 sine at amplitude 1.00, spliced in "continuous
	// in phase at both sides of the single period"; the whole is synthesized at
	// 4·fs, lowpass (anti-alias) filtered, and downsampled to fs at a sample
	// offset (at the 4·fs rate) of 0/1/2/3 for cases 20/21/22/23 respectively.
	//   Expected: Max. true-peak level = 0.0 +0.2/−0.4 dBTP for all four.
	// The four offsets sample the same continuous 4·fs waveform at four phase
	// alignments, exercising the true-peak interpolator's accuracy regardless
	// of where, modulo the decimation phase, the inter-sample peak falls.
	//
	// Unlike the fs-rate tones of cases 15–19, the document does NOT fully
	// specify this construction (transcription ambiguity #5): the splice math
	// ("continuous in phase" implies neither amplitude nor derivative
	// continuity), the anti-alias filter, and the "short" fade are all
	// unstated. Per the spec's own recommendation these cases therefore use the
	// OFFICIAL EBU reference WAVs (© EBU) — the 48 kHz / 24-bit stereo
	// seq-3341-20…23 signals from the EBU Loudness test set (v05) — loaded via
	// the repo's own containers/wav + codec/pcm readers. The fixtures are
	// fetched/cached at test time and never vendored (EBU Terms of Use:
	// internal technical testing only, no redistribution); each subtest skips
	// cleanly when its fixture is absent, so this never fails offline CI. See
	// ebu_testset_test.go for the fixture plumbing.
	const wantTP = 0.0 // dBTP; tolerance +0.2/−0.4 (Table 1), as cases 15–19.
	for _, caseNum := range []int{20, 21, 22, 23} {
		offset := caseNum - 20
		t.Run(fmt.Sprintf("case%d offset%d", caseNum, offset), func(t *testing.T) {
			sig, rate, channels := loadEBUTruePeakCase(t, caseNum)
			m, err := NewMeter(rate, channels, ModeTruePeak)
			require.NoError(t, err)
			require.NoError(t, m.AddFrames(sig))

			peak := 0.0
			for ch := 0; ch < channels; ch++ {
				p, err := m.TruePeak(ch)
				require.NoError(t, err)
				if p > peak {
					peak = p
				}
			}
			dbtp := mutations.AmplitudeToDecibels(peak)
			// Tolerance +0.2/−0.4 dBTP: dbtp ∈ [want−0.4, want+0.2].
			assert.GreaterOrEqual(t, dbtp, wantTP-0.4,
				"case %d true peak %.3f dBTP below want %.1f −0.4", caseNum, dbtp, wantTP)
			assert.LessOrEqual(t, dbtp, wantTP+0.2,
				"case %d true peak %.3f dBTP above want %.1f +0.2", caseNum, dbtp, wantTP)
		})
	}
}
