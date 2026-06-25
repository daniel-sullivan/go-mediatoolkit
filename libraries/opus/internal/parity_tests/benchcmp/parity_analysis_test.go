//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// Phase 9d — analysis.c parity tests.
//
// Covered entry points:
//   - tonality_analysis_init       (deterministic zero-init)
//   - tonality_get_info            (seeded state, full logic)
//   - silk_resampler_down2_hp      (HF-energy resampler helper)
//   - is_digital_silence           (silence detector)
//
// Full-pipeline tonality_analysis / run_analysis parity is deferred:
// it depends on opus_fft + the MLP forward pass; MLP drift is already
// covered in parity_mlp_*.go (Wave 9c), and the 480-point FFT needs
// the static_modes_float.h path which lands later.

// TestParity_Analysis_Init — post-init state must match the C oracle
// byte-for-byte across every numeric field.
func TestParity_Analysis_Init(t *testing.T) {
	for _, Fs := range []int32{48000, 24000, 16000} {
		t.Run(fSFormat(Fs), func(t *testing.T) {
			// Go side.
			goSt := nativeopus.ExportTonalityAnalysisInit(Fs)
			goSnap := nativeopus.ExportSnapshotAnalysisState(goSt)

			// C side.
			cSt := cTonalityAnalysisInit(Fs)
			defer cSt.Free()
			cSnap := cSnapshotInit(cSt)

			// Compare scalar fields.
			checkI(t, "arch", goSnap.Arch, cSnap.Arch)
			checkI(t, "application", goSnap.Application, cSnap.Application)
			if goSnap.Fs != cSnap.Fs {
				t.Errorf("Fs: go=%d c=%d", goSnap.Fs, cSnap.Fs)
			}
			checkI(t, "mem_fill", goSnap.MemFill, cSnap.MemFill)
			checkI(t, "prev_bandwidth", goSnap.PrevBandwidth, cSnap.PrevBandwidth)
			checkF(t, "prev_tonality", goSnap.PrevTonality, cSnap.PrevTonality)
			checkF(t, "Etracker", goSnap.Etracker, cSnap.Etracker)
			checkF(t, "lowECount", goSnap.LowECount, cSnap.LowECount)
			checkI(t, "E_count", goSnap.ECount, cSnap.ECount)
			checkI(t, "count", goSnap.Count, cSnap.Count)
			checkI(t, "analysis_offset", goSnap.AnalysisOffset, cSnap.AnalysisOffset)
			checkI(t, "write_pos", goSnap.WritePos, cSnap.WritePos)
			checkI(t, "read_pos", goSnap.ReadPos, cSnap.ReadPos)
			checkI(t, "read_subframe", goSnap.ReadSubframe, cSnap.ReadSubframe)
			checkF(t, "hp_ener_accum", goSnap.HpEnerAccum, cSnap.HpEnerAccum)
			checkI(t, "initialized", goSnap.Initialized, cSnap.Initialized)

			// Arrays.
			for i := 0; i < 240; i++ {
				checkF(t, idxName("angle", i), goSnap.Angle[i], cSnap.Angle[i])
				checkF(t, idxName("d_angle", i), goSnap.DAngle[i], cSnap.DAngle[i])
				checkF(t, idxName("d2_angle", i), goSnap.D2Angle[i], cSnap.D2Angle[i])
			}
			for i := 0; i < 18; i++ {
				checkF(t, idxName("prev_band_tonality", i), goSnap.PrevBandTonality[i], cSnap.PrevBandTonality[i])
				checkF(t, idxName("lowE", i), goSnap.LowE[i], cSnap.LowE[i])
				checkF(t, idxName("highE", i), goSnap.HighE[i], cSnap.HighE[i])
			}
			for i := 0; i < 19; i++ {
				checkF(t, idxName("meanE", i), goSnap.MeanE[i], cSnap.MeanE[i])
			}
			for i := 0; i < 32; i++ {
				checkF(t, idxName("mem", i), goSnap.Mem[i], cSnap.Mem[i])
				checkF(t, idxName("rnn_state", i), goSnap.RnnState[i], cSnap.RnnState[i])
			}
			for i := 0; i < 8; i++ {
				checkF(t, idxName("cmean", i), goSnap.Cmean[i], cSnap.Cmean[i])
			}
			for i := 0; i < 9; i++ {
				checkF(t, idxName("std", i), goSnap.Std[i], cSnap.Std[i])
			}
			for i := 0; i < 3; i++ {
				checkF(t, idxName("downmix_state", i), goSnap.DownmixState[i], cSnap.DownmixState[i])
			}
		})
	}
}

// TestParity_Analysis_TonalityGetInfo — seed matching Go/C states
// then compare the resulting AnalysisInfo bit-exactly. Covers every
// control-flow branch in tonality_get_info (look-ahead compensation,
// past-min/past-max fallback, prob_min/prob_max accumulation).
func TestParity_Analysis_TonalityGetInfo(t *testing.T) {
	r := rand.New(rand.NewSource(0x9D0001))
	// Test across a variety of state shapes.
	for trial := 0; trial < 64; trial++ {
		seed := randomSeed(r)
		// frameLen: a typical 10-20 ms frame at 48 kHz, 24 kHz, or 16 kHz.
		var frameLen int
		switch seed.Fs {
		case 48000:
			frameLen = []int{480, 960, 1920}[r.Intn(3)]
		case 24000:
			frameLen = []int{240, 480, 960}[r.Intn(3)]
		default:
			frameLen = []int{160, 320, 640}[r.Intn(3)]
		}

		goInfo := nativeopus.ExportTonalityGetInfoFromSeed(toLibSeed(seed), frameLen)

		cValid, cTonality, cTonalitySlope, cNoisiness, cActivity,
			cMusicProb, cMusicProbMin, cMusicProbMax, cBandwidth,
			cActProb, cMaxPitchRatio := cTonalityGetInfoFromSeed(seed, frameLen)

		mismatch := false
		if int32(goInfo.Valid) != cValid {
			t.Errorf("trial=%d valid: go=%d c=%d", trial, goInfo.Valid, cValid)
			mismatch = true
		}
		if !bitExactF32(goInfo.Tonality, cTonality) {
			t.Errorf("trial=%d tonality: go=%g (0x%08x) c=%g (0x%08x)",
				trial, goInfo.Tonality, math.Float32bits(goInfo.Tonality),
				cTonality, math.Float32bits(cTonality))
			mismatch = true
		}
		if !bitExactF32(goInfo.TonalitySlope, cTonalitySlope) {
			t.Errorf("trial=%d tonality_slope: go=%g c=%g", trial, goInfo.TonalitySlope, cTonalitySlope)
			mismatch = true
		}
		if !bitExactF32(goInfo.Noisiness, cNoisiness) {
			t.Errorf("trial=%d noisiness: go=%g c=%g", trial, goInfo.Noisiness, cNoisiness)
			mismatch = true
		}
		if !bitExactF32(goInfo.Activity, cActivity) {
			t.Errorf("trial=%d activity: go=%g c=%g", trial, goInfo.Activity, cActivity)
			mismatch = true
		}
		if !bitExactF32(goInfo.MusicProb, cMusicProb) {
			t.Errorf("trial=%d music_prob: go=%g (0x%08x) c=%g (0x%08x)",
				trial, goInfo.MusicProb, math.Float32bits(goInfo.MusicProb),
				cMusicProb, math.Float32bits(cMusicProb))
			mismatch = true
		}
		if !bitExactF32(goInfo.MusicProbMin, cMusicProbMin) {
			t.Errorf("trial=%d music_prob_min: go=%g c=%g", trial, goInfo.MusicProbMin, cMusicProbMin)
			mismatch = true
		}
		if !bitExactF32(goInfo.MusicProbMax, cMusicProbMax) {
			t.Errorf("trial=%d music_prob_max: go=%g c=%g", trial, goInfo.MusicProbMax, cMusicProbMax)
			mismatch = true
		}
		if goInfo.Bandwidth != cBandwidth {
			t.Errorf("trial=%d bandwidth: go=%d c=%d", trial, goInfo.Bandwidth, cBandwidth)
			mismatch = true
		}
		if !bitExactF32(goInfo.ActivityProbability, cActProb) {
			t.Errorf("trial=%d activity_probability: go=%g c=%g", trial, goInfo.ActivityProbability, cActProb)
			mismatch = true
		}
		if !bitExactF32(goInfo.MaxPitchRatio, cMaxPitchRatio) {
			t.Errorf("trial=%d max_pitch_ratio: go=%g c=%g", trial, goInfo.MaxPitchRatio, cMaxPitchRatio)
			mismatch = true
		}
		if mismatch {
			return
		}
	}
}

// TestParity_Analysis_SilkResamplerDown2HP — 3-tap all-pass
// downsampler used for analysis HF-energy accumulation.
func TestParity_Analysis_SilkResamplerDown2HP(t *testing.T) {
	r := rand.New(rand.NewSource(0x9D0002))
	for trial := 0; trial < 200; trial++ {
		n := 2 * (2 + r.Intn(239)) // len 4..480, even
		in := make([]float32, n)
		for i := range in {
			// Realistic downmixed PCM at 48 kHz (amplitude up to 32768*C
			// and capped at 65536 by downmix_float).
			in[i] = float32(r.Float64()*2-1) * 32768
		}
		goS := []float32{0, 0, 0}
		cS := []float32{0, 0, 0}
		goOut := make([]float32, n/2)
		cOut := make([]float32, n/2)

		goRet := nativeopus.ExportSilkResamplerDown2HP(goS, goOut, in)
		cRet := cSilkResamplerDown2HP(cS, cOut, in)

		if !bitExactF32(goRet, cRet) {
			t.Fatalf("trial=%d n=%d return: go=%g (0x%08x) c=%g (0x%08x) ULP=%d",
				trial, n, goRet, math.Float32bits(goRet), cRet, math.Float32bits(cRet),
				int64(math.Float32bits(goRet))-int64(math.Float32bits(cRet)))
		}
		for i := 0; i < n/2; i++ {
			if !bitExactF32(goOut[i], cOut[i]) {
				t.Fatalf("trial=%d n=%d out[%d]: go=%g (0x%08x) c=%g (0x%08x)",
					trial, n, i, goOut[i], math.Float32bits(goOut[i]),
					cOut[i], math.Float32bits(cOut[i]))
			}
		}
		for i := 0; i < 3; i++ {
			if !bitExactF32(goS[i], cS[i]) {
				t.Fatalf("trial=%d n=%d S[%d]: go=%g c=%g", trial, n, i, goS[i], cS[i])
			}
		}
	}
}

// TestParity_Analysis_IsDigitalSilence — silence threshold check.
func TestParity_Analysis_IsDigitalSilence(t *testing.T) {
	r := rand.New(rand.NewSource(0x9D0003))
	lsbDepths := []int{8, 16, 24}
	for trial := 0; trial < 300; trial++ {
		lsb := lsbDepths[r.Intn(len(lsbDepths))]
		n := 64 + r.Intn(512)
		pcm := make([]float32, n)
		switch r.Intn(4) {
		case 0:
			// All zero.
		case 1:
			// Near-silence (below 1/(1<<lsb) threshold).
			for i := range pcm {
				pcm[i] = float32(r.Float64()*0.5-0.25) / float32(int(1)<<(lsb+2))
			}
		case 2:
			// A single loud sample.
			pcm[r.Intn(n)] = float32(r.Float64()*2 - 1)
		default:
			// Full-scale noise.
			for i := range pcm {
				pcm[i] = float32(r.Float64()*2 - 1)
			}
		}
		goRes := nativeopus.ExportIsDigitalSilence(pcm, lsb)
		cRes := cIsDigitalSilence(pcm, lsb)
		if goRes != cRes {
			t.Fatalf("trial=%d lsb=%d n=%d go=%d c=%d", trial, lsb, n, goRes, cRes)
		}
	}
}

// TestParity_Analysis_RunAnalysis — full-pipeline parity across the
// Go run_analysis port and the C oracle. Drives several synthetic PCM
// frames (sine + noise) through both sides and asserts:
//   - every float field of the returned AnalysisInfo matches bit-exact
//     via uint32-bit comparison.
//   - every int field matches exactly.
//   - every state field (angle, dAngle, inmem, rnn_state, …) still
//     matches after a sequence of frames — i.e. state evolution is
//     identical across runs.
//
// Matrix: {48 kHz, 24 kHz} × {mono, stereo} × {10 ms, 20 ms frame}.
func TestParity_Analysis_RunAnalysis(t *testing.T) {
	cases := []struct {
		name      string
		Fs        int32
		channels  int
		frameMs   int // 10 or 20
		lsbDepth  int
		numFrames int
	}{
		{"Fs48000/mono/10ms", 48000, 1, 10, 16, 12},
		{"Fs48000/mono/20ms", 48000, 1, 20, 16, 12},
		{"Fs48000/stereo/10ms", 48000, 2, 10, 16, 12},
		{"Fs48000/stereo/20ms", 48000, 2, 20, 16, 12},
		{"Fs24000/mono/20ms", 24000, 1, 20, 16, 12},
		{"Fs24000/stereo/20ms", 24000, 2, 20, 16, 12},
	}

	// Allocate FFT once per test; share twiddles between C and Go.
	fftC := cAnalysisFFTAlloc(480)
	defer fftC.Free()

	factors := make([]int16, 2*8)
	for i := range factors {
		factors[i] = fftC.Factor(i)
	}
	bitrev := make([]int16, 480)
	for i := range bitrev {
		bitrev[i] = fftC.Bitrev(i)
	}
	twR := make([]float32, 480)
	twI := make([]float32, 480)
	for i := 0; i < 480; i++ {
		twR[i] = fftC.Twr(i)
		twI[i] = fftC.Twi(i)
	}
	fftGoHandle := nativeopus.NewFftStateFromData(
		480, fftC.Scale(), fftC.Shift(), factors, bitrev, twR, twI)
	goMode := nativeopus.ExportNewCELTModeFor480FFT(fftGoHandle)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Frame size in samples @ Fs (per channel).
			frameSize := int(tc.Fs) * tc.frameMs / 1000

			// Generate PCM: a chain of frames of interleaved audio.
			r := rand.New(rand.NewSource(int64(0x9D9D00 + int(tc.Fs) + tc.channels*1000 + tc.frameMs)))
			totalSamples := frameSize * tc.numFrames * tc.channels
			pcm := make([]float32, totalSamples)
			// Build sine (440 Hz) + light noise at each channel offset.
			for n := 0; n < frameSize*tc.numFrames; n++ {
				base := float32(math.Sin(2*math.Pi*440*float64(n)/float64(tc.Fs))) * 0.5
				for c := 0; c < tc.channels; c++ {
					pcm[n*tc.channels+c] = base + float32(r.Float64()*0.04-0.02)
				}
			}

			// Initialize states.
			goSt := nativeopus.ExportTonalityAnalysisInit(tc.Fs)
			cSt := cTonalityAnalysisInit(tc.Fs)
			defer cSt.Free()

			// c1=0, c2=-2 drives downmix_float's "sum all channels" path
			// — matches opus_encode_native's analysis call site.
			const c1, c2 = 0, -2

			for f := 0; f < tc.numFrames; f++ {
				// Each frame: pcm slice for this frame, run_analysis once.
				// analysis_frame_size == frame_size.
				off := f * frameSize * tc.channels
				end := off + frameSize*tc.channels
				framePCM := pcm[off:end]

				goInfoSnap, goStSnap := nativeopus.ExportRunAnalysisFloat(
					goSt, goMode, framePCM,
					frameSize, frameSize, c1, c2, tc.channels, tc.Fs, tc.lsbDepth)

				cInfo := cRunAnalysisFloat(cSt, fftC, framePCM,
					frameSize, frameSize, c1, c2, tc.channels, tc.Fs, tc.lsbDepth)
				cStSnap := cSnapshotInit(cSt)

				// ── AnalysisInfo comparison ────────────────────────
				if int32(goInfoSnap.Valid) != cInfo.Valid {
					t.Fatalf("frame=%d valid: go=%d c=%d", f, goInfoSnap.Valid, cInfo.Valid)
				}
				mustBitEqF(t, "tonality", f, goInfoSnap.Tonality, cInfo.Tonality)
				mustBitEqF(t, "tonality_slope", f, goInfoSnap.TonalitySlope, cInfo.TonalitySlope)
				mustBitEqF(t, "noisiness", f, goInfoSnap.Noisiness, cInfo.Noisiness)
				mustBitEqF(t, "activity", f, goInfoSnap.Activity, cInfo.Activity)
				mustBitEqF(t, "music_prob", f, goInfoSnap.MusicProb, cInfo.MusicProb)
				mustBitEqF(t, "music_prob_min", f, goInfoSnap.MusicProbMin, cInfo.MusicProbMin)
				mustBitEqF(t, "music_prob_max", f, goInfoSnap.MusicProbMax, cInfo.MusicProbMax)
				if int32(goInfoSnap.Bandwidth) != cInfo.Bandwidth {
					t.Fatalf("frame=%d bandwidth: go=%d c=%d", f, goInfoSnap.Bandwidth, cInfo.Bandwidth)
				}
				mustBitEqF(t, "activity_probability", f,
					goInfoSnap.ActivityProbability, cInfo.ActivityProbability)
				mustBitEqF(t, "max_pitch_ratio", f, goInfoSnap.MaxPitchRatio, cInfo.MaxPitchRatio)
				for i := 0; i < len(cInfo.LeakBoost); i++ {
					if goInfoSnap.LeakBoost[i] != cInfo.LeakBoost[i] {
						t.Fatalf("frame=%d leak_boost[%d]: go=%d c=%d",
							f, i, goInfoSnap.LeakBoost[i], cInfo.LeakBoost[i])
					}
				}

				// ── State comparison (subset — the ones mirrored) ──
				mustStateEq(t, f, goStSnap, cStSnap)
			}
		})
	}
}

// mustBitEqF fails the test if a and b differ bit-wise.
func mustBitEqF(t *testing.T, name string, frame int, a, b float32) {
	t.Helper()
	if !bitExactF32(a, b) {
		t.Fatalf("frame=%d %s: go=%g (0x%08x) c=%g (0x%08x) ULP=%d",
			frame, name, a, math.Float32bits(a), b, math.Float32bits(b),
			int64(math.Float32bits(a))-int64(math.Float32bits(b)))
	}
}

// mustStateEq compares the mirrored fields of AnalysisStateSnapshot
// and cAnalysisInitSnapshot bit-exactly.
func mustStateEq(t *testing.T, frame int, g nativeopus.AnalysisStateSnapshot, c cAnalysisInitSnapshot) {
	t.Helper()
	cmpI := func(name string, gv, cv int) {
		if gv != cv {
			t.Fatalf("frame=%d state.%s: go=%d c=%d", frame, name, gv, cv)
		}
	}
	cmpF := func(name string, gv, cv float32) {
		if !bitExactF32(gv, cv) {
			t.Fatalf("frame=%d state.%s: go=%g (0x%08x) c=%g (0x%08x)",
				frame, name, gv, math.Float32bits(gv), cv, math.Float32bits(cv))
		}
	}
	cmpI("mem_fill", g.MemFill, c.MemFill)
	cmpI("prev_bandwidth", g.PrevBandwidth, c.PrevBandwidth)
	cmpI("E_count", g.ECount, c.ECount)
	cmpI("count", g.Count, c.Count)
	cmpI("analysis_offset", g.AnalysisOffset, c.AnalysisOffset)
	cmpI("write_pos", g.WritePos, c.WritePos)
	cmpI("read_pos", g.ReadPos, c.ReadPos)
	cmpI("read_subframe", g.ReadSubframe, c.ReadSubframe)
	cmpI("initialized", g.Initialized, c.Initialized)
	cmpF("prev_tonality", g.PrevTonality, c.PrevTonality)
	cmpF("Etracker", g.Etracker, c.Etracker)
	cmpF("lowECount", g.LowECount, c.LowECount)
	cmpF("hp_ener_accum", g.HpEnerAccum, c.HpEnerAccum)
	for i := 0; i < 240; i++ {
		cmpF("angle["+itoa(i)+"]", g.Angle[i], c.Angle[i])
		cmpF("d_angle["+itoa(i)+"]", g.DAngle[i], c.DAngle[i])
		cmpF("d2_angle["+itoa(i)+"]", g.D2Angle[i], c.D2Angle[i])
	}
	for i := 0; i < 18; i++ {
		cmpF("prev_band_tonality["+itoa(i)+"]", g.PrevBandTonality[i], c.PrevBandTonality[i])
		cmpF("lowE["+itoa(i)+"]", g.LowE[i], c.LowE[i])
		cmpF("highE["+itoa(i)+"]", g.HighE[i], c.HighE[i])
	}
	for i := 0; i < 19; i++ {
		cmpF("meanE["+itoa(i)+"]", g.MeanE[i], c.MeanE[i])
	}
	for i := 0; i < 32; i++ {
		cmpF("mem["+itoa(i)+"]", g.Mem[i], c.Mem[i])
		cmpF("rnn_state["+itoa(i)+"]", g.RnnState[i], c.RnnState[i])
	}
	for i := 0; i < 8; i++ {
		cmpF("cmean["+itoa(i)+"]", g.Cmean[i], c.Cmean[i])
	}
	for i := 0; i < 9; i++ {
		cmpF("std["+itoa(i)+"]", g.Std[i], c.Std[i])
	}
	for i := 0; i < 3; i++ {
		cmpF("downmix_state["+itoa(i)+"]", g.DownmixState[i], c.DownmixState[i])
	}
	for i := 0; i < 720; i++ {
		cmpF("inmem["+itoa(i)+"]", g.Inmem[i], c.Inmem[i])
	}
	for f := 0; f < 8; f++ {
		for b := 0; b < 18; b++ {
			cmpF("E["+itoa(f)+"]["+itoa(b)+"]", g.E[f*18+b], c.E[f*18+b])
			cmpF("logE["+itoa(f)+"]["+itoa(b)+"]", g.LogE[f*18+b], c.LogE[f*18+b])
		}
	}
	for i := 0; i < 100; i++ {
		if g.InfoValid[i] != c.InfoValid[i] {
			t.Fatalf("frame=%d info[%d].valid: go=%d c=%d",
				frame, i, g.InfoValid[i], c.InfoValid[i])
		}
		cmpF("info["+itoa(i)+"].tonality", g.InfoTonality[i], c.InfoTonality[i])
		cmpF("info["+itoa(i)+"].music_prob", g.InfoMusicProb[i], c.InfoMusicProb[i])
		cmpF("info["+itoa(i)+"].activity_prob", g.InfoActivityProb[i], c.InfoActivityProb[i])
		cmpF("info["+itoa(i)+"].tonality_slope", g.InfoTonalitySlope[i], c.InfoTonalitySlope[i])
		cmpF("info["+itoa(i)+"].noisiness", g.InfoNoisiness[i], c.InfoNoisiness[i])
		cmpF("info["+itoa(i)+"].activity", g.InfoActivity[i], c.InfoActivity[i])
		if g.InfoBandwidth[i] != c.InfoBandwidth[i] {
			t.Fatalf("frame=%d info[%d].bandwidth: go=%d c=%d",
				frame, i, g.InfoBandwidth[i], c.InfoBandwidth[i])
		}
	}
}

// ─── helpers ────────────────────────────────────────────────────────

func randomSeed(r *rand.Rand) cAnalysisStateSeed {
	fsChoices := []int32{48000, 24000, 16000}
	Fs := fsChoices[r.Intn(len(fsChoices))]
	count := r.Intn(200)
	writePos := r.Intn(nativeopus.ExportDETECT_SIZE)
	readPos := r.Intn(nativeopus.ExportDETECT_SIZE)
	readSub := r.Intn(16)
	prevBw := r.Intn(21)

	valid := make([]int32, nativeopus.ExportDETECT_SIZE)
	tonality := make([]float32, nativeopus.ExportDETECT_SIZE)
	bandwidth := make([]int32, nativeopus.ExportDETECT_SIZE)
	musicProb := make([]float32, nativeopus.ExportDETECT_SIZE)
	actProb := make([]float32, nativeopus.ExportDETECT_SIZE)
	for i := range valid {
		if r.Float64() < 0.9 {
			valid[i] = 1
		}
		tonality[i] = float32(r.Float64())
		bandwidth[i] = int32(r.Intn(21))
		musicProb[i] = float32(r.Float64())
		actProb[i] = float32(r.Float64())
	}
	return cAnalysisStateSeed{
		Count:                   count,
		WritePos:                writePos,
		ReadPos:                 readPos,
		ReadSubframe:            readSub,
		Fs:                      Fs,
		PrevBandwidth:           prevBw,
		InfoValid:               valid,
		InfoTonality:            tonality,
		InfoBandwidth:           bandwidth,
		InfoMusicProb:           musicProb,
		InfoActivityProbability: actProb,
	}
}

func toLibSeed(s cAnalysisStateSeed) nativeopus.AnalysisStateSeed {
	return nativeopus.AnalysisStateSeed{
		Count:                   s.Count,
		WritePos:                s.WritePos,
		ReadPos:                 s.ReadPos,
		ReadSubframe:            s.ReadSubframe,
		Fs:                      s.Fs,
		PrevBandwidth:           s.PrevBandwidth,
		InfoValid:               s.InfoValid,
		InfoTonality:            s.InfoTonality,
		InfoBandwidth:           s.InfoBandwidth,
		InfoMusicProb:           s.InfoMusicProb,
		InfoActivityProbability: s.InfoActivityProbability,
	}
}

func checkF(t *testing.T, name string, a, b float32) {
	t.Helper()
	if !bitExactF32(a, b) {
		t.Errorf("%s: go=%g (0x%08x) c=%g (0x%08x) ULP=%d",
			name, a, math.Float32bits(a), b, math.Float32bits(b),
			int64(math.Float32bits(a))-int64(math.Float32bits(b)))
	}
}

func checkI(t *testing.T, name string, a, b int) {
	t.Helper()
	if a != b {
		t.Errorf("%s: go=%d c=%d", name, a, b)
	}
}

func fSFormat(Fs int32) string {
	switch Fs {
	case 48000:
		return "Fs48000"
	case 24000:
		return "Fs24000"
	case 16000:
		return "Fs16000"
	default:
		return "Fs?"
	}
}

func idxName(base string, i int) string {
	return base + "[" + itoa(i) + "]"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
