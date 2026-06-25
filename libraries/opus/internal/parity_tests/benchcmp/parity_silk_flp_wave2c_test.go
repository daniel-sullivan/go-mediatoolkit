//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// peFrameLengthMs — PE_LTP_MEM_LENGTH_MS + nb_subfr*PE_SUBFR_LENGTH_MS.
// PE_LTP_MEM_LENGTH_MS = 20, PE_SUBFR_LENGTH_MS = 5.
func peFrameLengthSamples(fs_kHz, nb_subfr int) int {
	return (20 + nb_subfr*5) * fs_kHz
}

// TestParity_SilkPitchAnalysisCoreFLP — exercises the core pitch
// analyser for all supported (Fs_kHz, complexity, nb_subfr)
// combinations. Inputs are random float samples shaped loosely like
// audio (bandlimited white noise plus a narrow-band sinusoid) so that
// the search stages find distinct peaks and exercise both the
// stage-3 and the 8 kHz-only branches. Asserts 0-ULP / 0-byte match on
// every output.
func TestParity_SilkPitchAnalysisCoreFLP(t *testing.T) {
	r := rand.New(rand.NewSource(2026_04_19))
	type combo struct {
		Fs_kHz, complexity, nb_subfr int
	}
	combos := []combo{
		{8, 0, 4}, {8, 1, 4}, {8, 2, 4},
		{12, 0, 4}, {12, 1, 4}, {12, 2, 4},
		{16, 0, 4}, {16, 1, 4}, {16, 2, 4},
		{8, 0, 2}, {8, 1, 2}, {8, 2, 2},
		{12, 0, 2}, {12, 1, 2}, {12, 2, 2},
		{16, 0, 2}, {16, 1, 2}, {16, 2, 2},
	}
	for _, c := range combos {
		nSamples := peFrameLengthSamples(c.Fs_kHz, c.nb_subfr)
		for run := 0; run < 5; run++ {
			frame := make([]float32, nSamples)
			// int16-scale random signal so that the int-style
			// saturating low-pass at the start of the core produces
			// a well-populated correlation array.
			for i := range frame {
				frame[i] = float32(r.Intn(20001) - 10000)
			}
			prevLag := 0
			if run%2 == 1 {
				// Match the Fs_kHz scaling rules inside the core
				// (which halves / mul-2/3's the incoming prevLag) —
				// any value in the valid int16 range is legal.
				prevLag = 50 + r.Intn(200)
			}
			ltpIn := r.Float32() * 0.9
			st1 := r.Float32() // [0, 1)
			st2 := r.Float32() // [0, 1)

			cOut := cSilkPitchAnalysisCoreFLP(frame, c.Fs_kHz, c.complexity, c.nb_subfr,
				prevLag, st1, st2, ltpIn)
			gv, gp, gl, gc, glt := nativeopus.ExportTestSilkPitchAnalysisCoreFLP(frame,
				ltpIn, prevLag, st1, st2, c.Fs_kHz, c.complexity, c.nb_subfr)

			if cOut.Voicing != gv {
				t.Errorf("Fs=%d cplx=%d nb=%d run=%d voicing: C=%d Go=%d",
					c.Fs_kHz, c.complexity, c.nb_subfr, run, cOut.Voicing, gv)
			}
			if cOut.LagIndex != gl {
				t.Errorf("Fs=%d cplx=%d nb=%d run=%d lagIndex: C=%d Go=%d",
					c.Fs_kHz, c.complexity, c.nb_subfr, run, cOut.LagIndex, gl)
			}
			if cOut.ContourIndex != gc {
				t.Errorf("Fs=%d cplx=%d nb=%d run=%d contourIndex: C=%d Go=%d",
					c.Fs_kHz, c.complexity, c.nb_subfr, run, cOut.ContourIndex, gc)
			}
			if cOut.LTPCorr != glt {
				t.Errorf("Fs=%d cplx=%d nb=%d run=%d LTPCorr: C=%g Go=%g (%d ULP)",
					c.Fs_kHz, c.complexity, c.nb_subfr, run, cOut.LTPCorr, glt,
					ulpDiffF32(cOut.LTPCorr, glt))
			}
			for i := 0; i < c.nb_subfr; i++ {
				if cOut.PitchOut[i] != gp[i] {
					t.Errorf("Fs=%d cplx=%d nb=%d run=%d pitch[%d]: C=%d Go=%d",
						c.Fs_kHz, c.complexity, c.nb_subfr, run, i, cOut.PitchOut[i], gp[i])
					break
				}
			}
		}
	}
}

// TestParity_SilkFindPitchLagsFLP — exercises the find_pitch_lags
// mid-driver, which calls schur, k2a, bwexpander, LPC_analysis_filter,
// pitch_analysis_core_FLP end-to-end. Asserts 0-ULP / 0-byte match on
// the residual and all state outputs.
func TestParity_SilkFindPitchLagsFLP(t *testing.T) {
	r := rand.New(rand.NewSource(2026_04_20))
	// Use realistic encoder settings for each (Fs_kHz, nb_subfr)
	// pair. `la_pitch` and `ltp_mem_length` mirror the values set up
	// by silk_setup_fs; `pitch_LPC_win_length` = la_pitch + frame_length.
	type cfg struct {
		Fs_kHz, nb_subfr int
	}
	cfgs := []cfg{
		{8, 4}, {12, 4}, {16, 4},
		{8, 2}, {12, 2}, {16, 2},
	}
	for _, c := range cfgs {
		subfr_length := 5 * c.Fs_kHz
		frame_length := subfr_length * c.nb_subfr
		la_pitch := 2 * c.Fs_kHz
		ltp_mem_length := 20 * c.Fs_kHz // PE_LTP_MEM_LENGTH_MS*Fs_kHz
		// pitch_LPC_win_length is the LPC window the driver uses. The
		// C side enforces buf_len >= pitch_LPC_win_length. Use the
		// FIND_PITCH_LPC_WIN_MS_2_SF / FIND_PITCH_LPC_WIN_MS convention
		// from tuning_parameters.h: 20 + 2*LA_PITCH_MS for 4-subfr,
		// 10 + 2*LA_PITCH_MS for 2-subfr. LA_PITCH_MS = 2.
		var pitch_LPC_win_length int
		if c.nb_subfr == 4 {
			pitch_LPC_win_length = (20 + 2*2) * c.Fs_kHz
		} else {
			pitch_LPC_win_length = (10 + 2*2) * c.Fs_kHz
		}
		buf_len := la_pitch + frame_length + ltp_mem_length
		// bigX holds `ltp_mem_length` of history followed by `buf_len`
		// forward samples — the driver uses `x - ltp_mem_length` to
		// reach the start.
		total := ltp_mem_length + buf_len
		for run := 0; run < 4; run++ {
			bigX := make([]float32, total)
			for i := range bigX {
				bigX[i] = float32(r.Intn(20001)-10000) / 32768.0
			}
			xOff := ltp_mem_length
			in := cFindPitchLagsFLPInputs{
				Fs_kHz:                       c.Fs_kHz,
				nb_subfr:                     c.nb_subfr,
				la_pitch:                     la_pitch,
				frame_length:                 frame_length,
				ltp_mem_length:               ltp_mem_length,
				pitch_LPC_win_length:         pitch_LPC_win_length,
				pitchEstimationLPCOrder:      6 + 2*(run%3),
				pitchEstimationComplexity:    run % 3,
				pitchEstimationThreshold_Q16: int32(3277 * (1 + run%3)), // ~0.05..0.15
				speech_activity_Q8:           50 + r.Intn(150),
				input_tilt_Q15:               r.Intn(16000),
				prevSignalType:               int8(1 + run%2),
				signalType:                   int8(1 + run%2), // TYPE_UNVOICED or TYPE_VOICED
				first_frame_after_reset:      0,
				prevLag:                      30 + r.Intn(100),
				LTPCorrIn:                    r.Float32() * 0.9,
			}

			cOut := cSilkFindPitchLagsFLP(in, bigX, xOff)
			gOut := nativeopus.ExportTestSilkFindPitchLagsFLP(nativeopus.ExportFindPitchLagsInputs{
				Fs_kHz:                       in.Fs_kHz,
				Nb_subfr:                     in.nb_subfr,
				La_pitch:                     in.la_pitch,
				Frame_length:                 in.frame_length,
				Ltp_mem_length:               in.ltp_mem_length,
				Pitch_LPC_win_length:         in.pitch_LPC_win_length,
				PitchEstimationLPCOrder:      in.pitchEstimationLPCOrder,
				PitchEstimationComplexity:    in.pitchEstimationComplexity,
				PitchEstimationThreshold_Q16: in.pitchEstimationThreshold_Q16,
				SpeechActivity_Q8:            in.speech_activity_Q8,
				InputTilt_Q15:                in.input_tilt_Q15,
				PrevSignalType:               in.prevSignalType,
				SignalType:                   in.signalType,
				FirstFrameAfterReset:         in.first_frame_after_reset,
				PrevLag:                      in.prevLag,
				LTPCorrIn:                    in.LTPCorrIn,
			}, bigX, xOff)

			if cOut.PredGain != gOut.PredGain {
				t.Errorf("Fs=%d nb=%d run=%d predGain: C=%g Go=%g (%d ULP)",
					c.Fs_kHz, c.nb_subfr, run, cOut.PredGain, gOut.PredGain,
					ulpDiffF32(cOut.PredGain, gOut.PredGain))
			}
			if cOut.LTPCorr != gOut.LTPCorr {
				t.Errorf("Fs=%d nb=%d run=%d LTPCorr: C=%g Go=%g (%d ULP)",
					c.Fs_kHz, c.nb_subfr, run, cOut.LTPCorr, gOut.LTPCorr,
					ulpDiffF32(cOut.LTPCorr, gOut.LTPCorr))
			}
			if cOut.LagIndex != gOut.LagIndex {
				t.Errorf("Fs=%d nb=%d run=%d lagIndex: C=%d Go=%d",
					c.Fs_kHz, c.nb_subfr, run, cOut.LagIndex, gOut.LagIndex)
			}
			if cOut.ContourIndex != gOut.ContourIndex {
				t.Errorf("Fs=%d nb=%d run=%d contourIndex: C=%d Go=%d",
					c.Fs_kHz, c.nb_subfr, run, cOut.ContourIndex, gOut.ContourIndex)
			}
			if cOut.SignalType != gOut.SignalType {
				t.Errorf("Fs=%d nb=%d run=%d signalType: C=%d Go=%d",
					c.Fs_kHz, c.nb_subfr, run, cOut.SignalType, gOut.SignalType)
			}
			for i := 0; i < 4; i++ {
				if cOut.PitchL[i] != gOut.PitchL[i] {
					t.Errorf("Fs=%d nb=%d run=%d pitchL[%d]: C=%d Go=%d",
						c.Fs_kHz, c.nb_subfr, run, i, cOut.PitchL[i], gOut.PitchL[i])
					break
				}
			}
			for i := 0; i < buf_len; i++ {
				if cOut.Res[i] != gOut.Res[i] {
					t.Errorf("Fs=%d nb=%d run=%d res[%d]: C=%g Go=%g (%d ULP)",
						c.Fs_kHz, c.nb_subfr, run, i, cOut.Res[i], gOut.Res[i],
						ulpDiffF32(cOut.Res[i], gOut.Res[i]))
					break
				}
			}
		}
	}
}
