//go:build cgo

package gtcrn_ort

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ort "github.com/yalue/onnxruntime_go"

	"github.com/daniel-sullivan/go-mediatoolkit/denoise/internal/gtcrn"
)

// parityAbs/parityRel are the mixed acceptance criterion (VERSION):
// accept when |Δ| ≤ 1e-4 OR rel ≤ 1e-3.
const (
	parityAbs = 1e-4
	parityRel = 1e-3
)

func within(got, want float64) bool {
	d := math.Abs(got - want)
	if d <= parityAbs {
		return true
	}
	return d <= parityRel*math.Abs(want)
}

func requireORT(t *testing.T) {
	t.Helper()
	if err := InitEnvironment(); err != nil {
		if errors.Is(err, ErrNoSharedLib) {
			t.Skipf("skipping onnxruntime oracle: %v", err)
		}
		t.Fatal(err)
	}
	t.Logf("onnxruntime %s via %s", ort.GetVersion(), os.Getenv(SharedLibEnv))
}

func TestVendoredModelDigest(t *testing.T) {
	raw, err := os.ReadFile(modelPath)
	require.NoError(t, err)
	sum := sha256.Sum256(raw)
	assert.Equal(t, ModelSHA256, hex.EncodeToString(sum[:]),
		"vendored onnx model drifted from the VERSION pin")
}

// stftInputs schema (the cross-package STFT golden carries the
// deterministic input signals the E2E golden was built from).
type stftInputs struct {
	Signals map[string]struct {
		Input struct {
			Data []float64 `json:"data"`
		} `json:"input"`
	} `json:"signals"`
}

// e2eGolden schema (subset): committed ORT-streamed enh spectra.
type e2eGolden struct {
	Signals map[string]struct {
		NFrames int `json:"n_frames"`
		EnhSpec struct {
			Shape []int           `json:"shape"` // [1,257,T,2]
			Data  [][][][]float64 `json:"data"`
		} `json:"enh_spec"`
	} `json:"signals"`
}

// TestOracleReproducesGolden is the ORT gate: it re-runs the hardened
// oracle over the deterministic signals and asserts the committed E2E
// golden's enh spectra match within the mixed tolerance — proving the
// oracle harness, the vendored model, and the golden are mutually
// consistent (the fresh-oracle drift guard, cross-machine safe). The
// Go-port-vs-oracle gate lands with the forward-pass port.
func TestOracleReproducesGolden(t *testing.T) {
	requireORT(t)

	var inputs stftInputs
	raw, err := os.ReadFile("../../gtcrn/testdata/stft_golden.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &inputs))

	var golden e2eGolden
	raw, err = os.ReadFile("testdata/e2e_golden.json")
	if os.IsNotExist(err) {
		t.Skip("testdata/e2e_golden.json not present (kept out of git for size) — " +
			"regenerate it with tools/gtcrndump before running the ORT E2E parity gate")
	}
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &golden))

	oracle, err := NewOracle(modelPath)
	require.NoError(t, err)
	defer oracle.Destroy()

	for name, in := range inputs.Signals {
		g, ok := golden.Signals[name]
		if !ok {
			continue
		}
		x := make([]float32, len(in.Input.Data))
		for i, v := range in.Input.Data {
			x[i] = float32(v)
		}
		re, im, frames := gtcrn.STFT(x)
		require.Equal(t, g.NFrames, frames, "%s frame count", name)

		oracle.Reset()
		maxAbs, maxRel := 0.0, 0.0
		frame := make([]float32, frameLen)
		for f := 0; f < frames; f++ {
			for k := 0; k < Bins; k++ {
				frame[k*2] = re[k*frames+f]
				frame[k*2+1] = im[k*frames+f]
			}
			enh, err := oracle.RunFrame(frame)
			require.NoError(t, err)
			for k := 0; k < Bins; k++ {
				for c := 0; c < 2; c++ {
					got := float64(enh[k*2+c])
					want := g.EnhSpec.Data[0][k][f][c]
					if d := math.Abs(got - want); d > maxAbs {
						maxAbs = d
					}
					if aw := math.Abs(want); aw > 1e-6 {
						if r := math.Abs(got-want) / aw; r > maxRel {
							maxRel = r
						}
					}
					if !within(got, want) {
						t.Errorf("%s frame %d bin %d c%d: oracle %.7f vs golden %.7f", name, f, k, c, got, want)
					}
				}
			}
		}
		t.Logf("%s: %d frames, oracle vs golden max|Δ|=%.3e maxRel=%.3e", name, frames, maxAbs, maxRel)
	}
}
