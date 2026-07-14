//go:build cgo

package gtcrn_ort

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/denoise/internal/gtcrn"
)

// TestGoPortMatchesORT is the core parity gate: it feeds identical STFT
// spectrum frames to the pure-Go Model.Forward and to the onnxruntime
// oracle, streaming through the three recurrent caches, and asserts the
// per-frame enhanced spectrum matches within the mixed tolerance
// max(|Δ|≤1e-4, rel≤1e-3). It also checkpoints the three caches (whole
// tensors) every N frames and runs a drift-slope check: the per-frame
// max error must not grow monotonically (bounded drift is fine; a
// steadily rising error is a bug).
func TestGoPortMatchesORT(t *testing.T) {
	requireORT(t)

	var inputs stftInputs
	raw, err := os.ReadFile("../../gtcrn/testdata/stft_golden.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &inputs))

	oracle, err := NewOracle(modelPath)
	require.NoError(t, err)
	defer oracle.Destroy()

	model, err := gtcrn.NewModel()
	require.NoError(t, err)

	const checkpointEvery = 25
	names := []string{"white-noise", "sine-sweep", "impulse-train", "silence"}
	for _, name := range names {
		sig, ok := inputs.Signals[name]
		if !ok {
			continue
		}
		x := make([]float32, len(sig.Input.Data))
		for i, v := range sig.Input.Data {
			x[i] = float32(v)
		}
		re, im, frames := gtcrn.STFT(x)

		oracle.Reset()
		model.Reset()

		specRe := make([]float32, Bins)
		specIm := make([]float32, Bins)
		frame := make([]float32, frameLen)

		overallAbs, overallRel := 0.0, 0.0
		var firstHalf, secondHalf, halfN float64
		for f := 0; f < frames; f++ {
			for k := 0; k < Bins; k++ {
				specRe[k] = re[k*frames+f]
				specIm[k] = im[k*frames+f]
				frame[k*2] = specRe[k]
				frame[k*2+1] = specIm[k]
			}
			want, err := oracle.RunFrame(frame)
			require.NoError(t, err)
			got := model.Forward(specRe, specIm)

			frameMax := 0.0
			for j := 0; j < frameLen; j++ {
				d := math.Abs(float64(got[j]) - float64(want[j]))
				if d > overallAbs {
					overallAbs = d
				}
				if d > frameMax {
					frameMax = d
				}
				if aw := math.Abs(float64(want[j])); aw > 1e-6 {
					if r := d / aw; r > overallRel {
						overallRel = r
					}
				}
				if !within(float64(got[j]), float64(want[j])) {
					t.Errorf("%s frame %d idx %d: go %.7f vs ort %.7f", name, f, j, got[j], want[j])
				}
			}
			if f < frames/2 {
				firstHalf += frameMax
			} else {
				secondHalf += frameMax
			}
			if f == frames/2 {
				halfN = float64(frames / 2)
			}

			// Cache checkpoint parity.
			if f%checkpointEvery == 0 || f == frames-1 {
				oc, ot, oi := oracle.Caches()
				gc, gt, gi := model.Caches()
				checkCache(t, name, f, "conv", gc, oc)
				checkCache(t, name, f, "tra", gt, ot)
				checkCache(t, name, f, "inter", gi, oi)
			}
		}

		// Drift-slope guard: mean per-frame error must not blow up over time.
		if halfN > 0 {
			a1 := firstHalf / halfN
			a2 := secondHalf / (float64(frames) - halfN)
			t.Logf("%s: %d frames, enh max|Δ|=%.3e maxRel=%.3e; mean|Δ| firstHalf=%.3e secondHalf=%.3e",
				name, frames, overallAbs, overallRel, a1, a2)
			if a1 > 1e-9 && a2 > 10*a1 {
				t.Errorf("%s: drift slope — secondHalf mean %.3e >> firstHalf %.3e (monotonic growth = bug)", name, a2, a1)
			}
		}
	}
}

func checkCache(t *testing.T, name string, f int, which string, got, want []float32) {
	t.Helper()
	require.Len(t, got, len(want), "%s frame %d %s cache length", name, f, which)
	maxAbs := 0.0
	for i := range want {
		if !within(float64(got[i]), float64(want[i])) {
			t.Errorf("%s frame %d %s cache idx %d: go %.7f vs ort %.7f", name, f, which, i, got[i], want[i])
		}
		if d := math.Abs(float64(got[i]) - float64(want[i])); d > maxAbs {
			maxAbs = d
		}
	}
	t.Logf("%s frame %d %s cache max|Δ|=%.3e", name, f, which, maxAbs)
}
