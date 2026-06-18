//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_SilkNoiseShapeAnalysisFLP — exercises the noise-shape-analysis
// mid-driver end-to-end against the cgo oracle. Covers the warped and
// unwarped autocorrelation branches, warped_true2monic_coefs vs
// limit_coefs, voiced vs unvoiced shaping logic, useCBR=0 vs 1 SNR
// reduction, and harmonic shaping smoothing. Asserts 0-ULP / 0-byte
// match on every output field.
func TestParity_SilkNoiseShapeAnalysisFLP(t *testing.T) {
	r := rand.New(rand.NewSource(2026_04_21))

	// Realistic (fs_kHz, la_shape, shapingLPCOrder) combos mirror the
	// cases in silk/control_codec.c:
	//   fs=8,  la_shape=3*fs, order=12   (complexity 1)
	//   fs=8,  la_shape=5*fs, order=14   (complexity 2)
	//   fs=12, la_shape=3*fs, order=12   (complexity 1)
	//   fs=12, la_shape=5*fs, order=14   (complexity 2)
	//   fs=16, la_shape=5*fs, order=16   (complexity 3)
	//   fs=16, la_shape=5*fs, order=20   (complexity 4)
	//   fs=16, la_shape=5*fs, order=24   (complexity 5)
	type combo struct {
		fs_kHz, la_shape_ms, shapingLPCOrder int
	}
	combos := []combo{
		{8, 3, 12},
		{8, 5, 14},
		{12, 3, 12},
		{12, 5, 14},
		{16, 5, 16},
		{16, 5, 20},
		{16, 5, 24},
	}
	nb_subfrs := []int{2, 4}

	for _, c := range combos {
		for _, nb_subfr := range nb_subfrs {
			la_shape := c.la_shape_ms * c.fs_kHz
			subfr_length := 5 * c.fs_kHz
			shapeWinLength := subfr_length + 2*la_shape
			frame_length := subfr_length * nb_subfr

			for run := 0; run < 6; run++ {
				// x buffer: la_shape of history + frame_length + la_shape
				// of lookahead. The driver starts at x_ptr = x - la_shape
				// and reads shapeWinLength = subfr_length + 2*la_shape
				// samples per subframe, advancing by subfr_length. The
				// last subframe ends at x + frame_length + la_shape.
				xTotal := 2*la_shape + frame_length
				bigX := make([]float32, xTotal)
				for i := range bigX {
					bigX[i] = float32(r.Intn(20001)-10000) / 32768.0
				}
				xOff := la_shape

				// pitch_res used only when signalType != TYPE_VOICED, length
				// nSegs * nSamples = (SUB_FRAME_LENGTH_MS * nb_subfr / 2) *
				// (2 * fs_kHz) samples. Always populate, it's harmless if
				// unused.
				nSegs := (5 * nb_subfr) / 2
				nSamples := 2 * c.fs_kHz
				pitchRes := make([]float32, nSegs*nSamples)
				for i := range pitchRes {
					pitchRes[i] = float32(r.Intn(20001)-10000) / 32768.0
				}

				// Vary the knobs that select distinct code paths:
				//   run%3 == 0 -> TYPE_VOICED, warping_Q16 > 0  (warped branch)
				//   run%3 == 1 -> TYPE_UNVOICED, warping_Q16 > 0 (warped + sparseness)
				//   run%3 == 2 -> TYPE_VOICED, warping_Q16 == 0  (unwarped branch)
				var signalType int8
				var warping_Q16 int
				switch run % 3 {
				case 0:
					signalType = 2 // TYPE_VOICED
					warping_Q16 = 3277 + r.Intn(5000)
				case 1:
					signalType = 1 // TYPE_UNVOICED
					warping_Q16 = 3277 + r.Intn(5000)
				case 2:
					signalType = 2 // TYPE_VOICED
					warping_Q16 = 0
				}
				useCBR := run % 2

				// Realistic pitch lags: base + small jitter, never larger
				// than base at k=0 (driver divides 3.0f / pitchL[k] so
				// any nonzero value works, but stay within plausible range).
				baseLag := 40 + r.Intn(100)
				var pitchL [4]int32
				pitchL[0] = int32(baseLag)
				for k := 1; k < 4; k++ {
					jitter := r.Intn(21) - 10
					lag := baseLag + jitter
					if lag < 20 {
						lag = 20
					}
					if lag > int(pitchL[0]) {
						lag = int(pitchL[0])
					}
					pitchL[k] = int32(lag)
				}

				var iqb [4]int // VAD_N_BANDS = 4
				for i := range iqb {
					iqb[i] = r.Intn(32768)
				}

				payload := nativeopus.SilkNoiseShapeAnalysisFLPPayload{
					FsKHz:                c.fs_kHz,
					NbSubfr:              nb_subfr,
					LaShape:              la_shape,
					ShapeWinLength:       shapeWinLength,
					ShapingLPCOrder:      c.shapingLPCOrder,
					SubfrLength:          subfr_length,
					SNR_dB_Q7:            (15 + r.Intn(30)) * 128, // ~15..45 dB
					InputQualityBandsQ15: iqb,
					UseCBR:               useCBR,
					SpeechActivityQ8:     r.Intn(257),
					SignalType:           signalType,
					WarpingQ16:           warping_Q16,
					Arch:                 0,
					LTPCorrIn:            r.Float32() * 0.9,
					PredGain:             r.Float32() * 100,
					PitchL:               pitchL,
					HarmShapeGainSmthIn:  r.Float32() * 0.3,
					TiltSmthIn:           -r.Float32() * 0.3,
					PitchRes:             pitchRes,
					X:                    bigX,
					XOff:                 xOff,
				}

				cOut := cSilkNoiseShapeAnalysisFLP(payload)
				gOut := nativeopus.ExportTestSilkNoiseShapeAnalysisFLP(payload)

				label := func(field string) string {
					return "" // set by caller
				}
				_ = label

				if cOut.QuantOffsetType != gOut.QuantOffsetType {
					t.Errorf("fs=%d ord=%d nb=%d run=%d quantOffsetType: C=%d Go=%d",
						c.fs_kHz, c.shapingLPCOrder, nb_subfr, run, cOut.QuantOffsetType, gOut.QuantOffsetType)
				}
				if cOut.InputQuality != gOut.InputQuality {
					t.Errorf("fs=%d ord=%d nb=%d run=%d input_quality: C=%g Go=%g (%d ULP)",
						c.fs_kHz, c.shapingLPCOrder, nb_subfr, run,
						cOut.InputQuality, gOut.InputQuality, ulpDiffF32(cOut.InputQuality, gOut.InputQuality))
				}
				if cOut.CodingQuality != gOut.CodingQuality {
					t.Errorf("fs=%d ord=%d nb=%d run=%d coding_quality: C=%g Go=%g (%d ULP)",
						c.fs_kHz, c.shapingLPCOrder, nb_subfr, run,
						cOut.CodingQuality, gOut.CodingQuality, ulpDiffF32(cOut.CodingQuality, gOut.CodingQuality))
				}
				if cOut.HarmShapeGainSmthOut != gOut.HarmShapeGainSmthOut {
					t.Errorf("fs=%d ord=%d nb=%d run=%d HarmShapeGain_smth: C=%g Go=%g (%d ULP)",
						c.fs_kHz, c.shapingLPCOrder, nb_subfr, run,
						cOut.HarmShapeGainSmthOut, gOut.HarmShapeGainSmthOut,
						ulpDiffF32(cOut.HarmShapeGainSmthOut, gOut.HarmShapeGainSmthOut))
				}
				if cOut.TiltSmthOut != gOut.TiltSmthOut {
					t.Errorf("fs=%d ord=%d nb=%d run=%d Tilt_smth: C=%g Go=%g (%d ULP)",
						c.fs_kHz, c.shapingLPCOrder, nb_subfr, run,
						cOut.TiltSmthOut, gOut.TiltSmthOut,
						ulpDiffF32(cOut.TiltSmthOut, gOut.TiltSmthOut))
				}
				failed := false
				for i := 0; i < nb_subfr; i++ {
					if cOut.Gains[i] != gOut.Gains[i] {
						t.Errorf("fs=%d ord=%d nb=%d run=%d Gains[%d]: C=%g Go=%g (%d ULP)",
							c.fs_kHz, c.shapingLPCOrder, nb_subfr, run, i,
							cOut.Gains[i], gOut.Gains[i], ulpDiffF32(cOut.Gains[i], gOut.Gains[i]))
						failed = true
						break
					}
					if cOut.LF_MA_shp[i] != gOut.LF_MA_shp[i] {
						t.Errorf("fs=%d ord=%d nb=%d run=%d LF_MA_shp[%d]: C=%g Go=%g (%d ULP)",
							c.fs_kHz, c.shapingLPCOrder, nb_subfr, run, i,
							cOut.LF_MA_shp[i], gOut.LF_MA_shp[i], ulpDiffF32(cOut.LF_MA_shp[i], gOut.LF_MA_shp[i]))
						failed = true
						break
					}
					if cOut.LF_AR_shp[i] != gOut.LF_AR_shp[i] {
						t.Errorf("fs=%d ord=%d nb=%d run=%d LF_AR_shp[%d]: C=%g Go=%g (%d ULP)",
							c.fs_kHz, c.shapingLPCOrder, nb_subfr, run, i,
							cOut.LF_AR_shp[i], gOut.LF_AR_shp[i], ulpDiffF32(cOut.LF_AR_shp[i], gOut.LF_AR_shp[i]))
						failed = true
						break
					}
					if cOut.Tilt[i] != gOut.Tilt[i] {
						t.Errorf("fs=%d ord=%d nb=%d run=%d Tilt[%d]: C=%g Go=%g (%d ULP)",
							c.fs_kHz, c.shapingLPCOrder, nb_subfr, run, i,
							cOut.Tilt[i], gOut.Tilt[i], ulpDiffF32(cOut.Tilt[i], gOut.Tilt[i]))
						failed = true
						break
					}
					if cOut.HarmShapeGain[i] != gOut.HarmShapeGain[i] {
						t.Errorf("fs=%d ord=%d nb=%d run=%d HarmShapeGain[%d]: C=%g Go=%g (%d ULP)",
							c.fs_kHz, c.shapingLPCOrder, nb_subfr, run, i,
							cOut.HarmShapeGain[i], gOut.HarmShapeGain[i], ulpDiffF32(cOut.HarmShapeGain[i], gOut.HarmShapeGain[i]))
						failed = true
						break
					}
				}
				if failed {
					continue
				}
				for i := 0; i < nb_subfr*c.shapingLPCOrder; i++ {
					if cOut.AR[i] != gOut.AR[i] {
						t.Errorf("fs=%d ord=%d nb=%d run=%d AR[%d]: C=%g Go=%g (%d ULP)",
							c.fs_kHz, c.shapingLPCOrder, nb_subfr, run, i,
							cOut.AR[i], gOut.AR[i], ulpDiffF32(cOut.AR[i], gOut.AR[i]))
						break
					}
				}
			}
		}
	}
}
