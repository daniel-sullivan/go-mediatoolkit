//go:build cgo && aec_oracle

package bandsplit

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// lcg is the same PRNG the other slices use; both sides consume
// identical Go-generated inputs, so quality is irrelevant.
type lcg uint32

func (l *lcg) next() float32 {
	*l = *l*1664525 + 1013904223
	return float32(int32(*l)) / float32(math.MaxInt32)
}

func (l *lcg) nextS16() int16 {
	*l = *l*1664525 + 1013904223
	return int16(*l >> 16)
}

func requireBitExactF32(t *testing.T, what string, iter int, got, want []float32) {
	t.Helper()
	for i := range want {
		if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
			t.Fatalf("%s: iter %d: index %d: got %v (0x%08x), want %v (0x%08x)",
				what, iter, i, got[i], math.Float32bits(got[i]),
				want[i], math.Float32bits(want[i]))
		}
	}
}

func requireEqualS16(t *testing.T, what string, iter int, got, want []int16) {
	t.Helper()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: iter %d: index %d: got %d, want %d", what, iter, i, got[i], want[i])
		}
	}
}

// TestThreeBandFilterBankParity runs 48kHz analysis+synthesis over
// many PRNG frames with state carried across calls (separate banks for
// each direction, as in AudioBuffer) and requires bit-exact agreement.
func TestThreeBandFilterBankParity(t *testing.T) {
	rng := lcg(3)

	goAnalysis := aec3.NewThreeBandFilterBank()
	goSynthesis := aec3.NewThreeBandFilterBank()
	cAnalysis := newThreeBandC()
	cSynthesis := newThreeBandC()
	defer cAnalysis.close()
	defer cSynthesis.close()

	for iter := 0; iter < 100; iter++ {
		var in [480]float32
		for i := range in {
			// FloatS16-domain magnitudes, as AudioBuffer feeds it.
			in[i] = rng.next() * 32768
		}

		goBandsFlat := make([]float32, 3*160)
		var goBands [3][]float32
		for band := 0; band < 3; band++ {
			goBands[band] = goBandsFlat[band*160 : (band+1)*160]
		}
		goAnalysis.Analysis(&in, &goBands)
		cBands := cAnalysis.analysis(&in)
		requireBitExactF32(t, "Analysis", iter, goBandsFlat, cBands)

		var goOut [480]float32
		goSynthesis.Synthesis(&goBands, &goOut)
		cOut := cSynthesis.synthesis(cBands)
		requireBitExactF32(t, "Synthesis", iter, goOut[:], cOut)
	}
}

// TestQMFParity runs the fixed-point two-band QMF (32kHz path:
// 320-sample frames) over many PRNG frames with the four state vectors
// carried across calls, including full-scale samples to exercise the
// saturation helpers.
func TestQMFParity(t *testing.T) {
	rng := lcg(4)

	var goA1, goA2, goS1, goS2 [6]int32
	var cA1, cA2, cS1, cS2 [6]int32

	for iter := 0; iter < 200; iter++ {
		in := make([]int16, 320)
		for i := range in {
			in[i] = rng.nextS16()
		}
		if iter%10 == 0 {
			// Bursts of full-scale input to hit saturation paths.
			for i := 0; i < 32; i++ {
				in[i] = -32768
				in[i+32] = 32767
			}
		}

		goLow := make([]int16, 160)
		goHigh := make([]int16, 160)
		aec3.AnalysisQMF(in, goLow, goHigh, goA1[:], goA2[:])
		cLow, cHigh := analysisQMFC(in, &cA1, &cA2)
		requireEqualS16(t, "AnalysisQMF low", iter, goLow, cLow)
		requireEqualS16(t, "AnalysisQMF high", iter, goHigh, cHigh)
		if goA1 != cA1 || goA2 != cA2 {
			t.Fatalf("iter %d: analysis state diverged: go %v/%v, c %v/%v", iter, goA1, goA2, cA1, cA2)
		}

		goOut := make([]int16, 320)
		aec3.SynthesisQMF(goLow, goHigh, goOut, goS1[:], goS2[:])
		cOut := synthesisQMFC(cLow, cHigh, &cS1, &cS2)
		requireEqualS16(t, "SynthesisQMF", iter, goOut, cOut)
		if goS1 != cS1 || goS2 != cS2 {
			t.Fatalf("iter %d: synthesis state diverged", iter)
		}
	}
}

// TestFloatS16ConversionParity checks the FloatS16<->S16 helpers,
// including the clamp edges and round-half-away-from-zero behavior.
func TestFloatS16ConversionParity(t *testing.T) {
	src := []float32{
		0, 0.4, 0.5, 0.6, -0.4, -0.5, -0.6, 1.5, -1.5, 2.5, -2.5,
		32766.4, 32766.5, 32767, 40000, -32767.4, -32767.5, -32768, -40000,
		123.75, -123.75,
	}
	rng := lcg(5)
	for i := 0; i < 1000; i++ {
		src = append(src, rng.next()*33000)
	}

	got := make([]int16, len(src))
	for i, v := range src {
		got[i] = aec3.FloatS16ToS16(v)
	}
	requireEqualS16(t, "FloatS16ToS16", 0, got, floatS16ToS16C(src))

	s16 := make([]int16, 0, 1000)
	for i := 0; i < 1000; i++ {
		s16 = append(s16, rng.nextS16())
	}
	gotF := make([]float32, len(s16))
	for i, v := range s16 {
		gotF[i] = aec3.S16ToFloatS16(v)
	}
	requireBitExactF32(t, "S16ToFloatS16", 0, gotF, s16ToFloatS16C(s16))
}
