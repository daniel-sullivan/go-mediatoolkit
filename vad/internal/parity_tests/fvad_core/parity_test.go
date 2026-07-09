//go:build cgo

package fvad_core

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/fvad"
)

// goSnapshot mirrors cCoreInst.snapshot for the Go port's Inst.
func goSnapshot(inst *fvad.Inst) cCoreSnapshot {
	var s cCoreSnapshot
	s.Vad = inst.Vad
	s.DownsamplingFilterStates = inst.DownsamplingFilterStates
	s.S48To24 = inst.State48To8.S48To24
	s.S24To24 = inst.State48To8.S24To24
	s.S24To16 = inst.State48To8.S24To16
	s.S16To8 = inst.State48To8.S16To8
	s.NoiseMeans = inst.NoiseMeans
	s.SpeechMeans = inst.SpeechMeans
	s.NoiseStds = inst.NoiseStds
	s.SpeechStds = inst.SpeechStds
	s.FrameCounter = inst.FrameCounter
	s.OverHang = inst.OverHang
	s.NumOfSpeech = inst.NumOfSpeech
	s.IndexVector = inst.IndexVector
	s.LowValueVector = inst.LowValueVector
	s.MeanValue = inst.MeanValue
	s.UpperState = inst.UpperState
	s.LowerState = inst.LowerState
	s.HpFilterState = inst.HpFilterState
	s.OverHangMax1 = inst.OverHangMax1
	s.OverHangMax2 = inst.OverHangMax2
	s.Individual = inst.Individual
	s.Total = inst.Total
	s.FeatureVector = inst.FeatureVector
	s.TotalPower = inst.TotalPower
	return s
}

func goCalcVad(inst *fvad.Inst, rate int, frame []int16) int {
	switch rate {
	case 8000:
		return fvad.CalcVad8khz(inst, frame)
	case 16000:
		return fvad.CalcVad16khz(inst, frame)
	case 32000:
		return fvad.CalcVad32khz(inst, frame)
	case 48000:
		return fvad.CalcVad48khz(inst, frame)
	}
	panic("bad rate")
}

// makeFrame cycles through signal classes designed to walk the GMM
// through both hypotheses and the model-update paths: silence (total
// power gate), low noise, speech-band tones with amplitude modulation
// (speech-like), broadband PRNG, and full-scale extremes.
func makeFrame(rng *rand.Rand, n, rate, frame int) []int16 {
	out := make([]int16, n)
	switch frame % 5 {
	case 0: // silence
	case 1: // low-level noise
		for i := range out {
			out[i] = int16(rng.IntN(129) - 64)
		}
	case 2: // AM speech-like tone burst
		f := 120 + rng.Float64()*300
		for i := range out {
			tt := float64(frame*n + i)
			env := 0.5 + 0.5*math.Sin(2*math.Pi*4*tt/float64(rate))
			v := env * 12000 * (math.Sin(2*math.Pi*f*tt/float64(rate)) +
				0.5*math.Sin(2*math.Pi*3*f*tt/float64(rate)))
			out[i] = int16(v)
		}
	case 3: // broadband PRNG
		for i := range out {
			out[i] = int16(rng.IntN(65536) - 32768)
		}
	default: // extremes
		for i := range out {
			if (i/16)%2 == 0 {
				out[i] = -32768
			} else {
				out[i] = 32767
			}
		}
	}
	return out
}

// TestInitCoreParity pins the freshly initialized state.
func TestInitCoreParity(t *testing.T) {
	c := newCCoreInst()
	g := new(fvad.Inst)
	g.InitCore()
	require.Equal(t, c.snapshot(), goSnapshot(g))
}

// TestSetModeCoreParity pins the threshold tables per mode plus the
// invalid-mode return.
func TestSetModeCoreParity(t *testing.T) {
	for mode := -2; mode <= 5; mode++ {
		c := newCCoreInst()
		g := new(fvad.Inst)
		g.InitCore()
		require.Equal(t, c.setMode(mode), g.SetModeCore(mode), "return for mode %d", mode)
		require.Equal(t, c.snapshot(), goSnapshot(g), "state after mode %d", mode)
	}
}

// TestCalcVadParity runs every rate × frame duration × mode combination
// over a mixed-signal stream, comparing the decision and the COMPLETE
// core state after every frame.
func TestCalcVadParity(t *testing.T) {
	for _, rate := range []int{8000, 16000, 32000, 48000} {
		for _, ms := range []int{10, 20, 30} {
			for mode := 0; mode <= 3; mode++ {
				rate, ms, mode := rate, ms, mode
				t.Run(fmt.Sprintf("%dHz/%dms/mode%d", rate, ms, mode), func(t *testing.T) {
					rng := rand.New(rand.NewPCG(uint64(rate), uint64(ms*10+mode)))
					n := rate / 1000 * ms

					c := newCCoreInst()
					g := new(fvad.Inst)
					g.InitCore()
					require.Equal(t, c.setMode(mode), g.SetModeCore(mode))

					for frame := 0; frame < 300; frame++ {
						in := makeFrame(rng, n, rate, frame)
						cVad := c.calcVad(rate, in)
						gVad := goCalcVad(g, rate, in)
						require.Equal(t, cVad, gVad, "decision frame=%d", frame)
						require.Equal(t, c.snapshot(), goSnapshot(g), "state frame=%d", frame)
					}
				})
			}
		}
	}
}
