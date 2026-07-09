//go:build cgo

package silero_ort

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ort "github.com/yalue/onnxruntime_go"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/goldensignals"
	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/silero"
)

const modelPath = "testdata/silero_vad_16k_op15.onnx"

// parityTolerance is the per-window bound — justification in doc.go.
const parityTolerance = 1e-4

// requireORT initialises onnxruntime or skips the test when the
// shared library is not configured (the opt-in gate).
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

// TestGoPortMatchesORT is the oracle gate: per-window
// |p_go − p_ort| ≤ 1e-4 over the full goldensignals set.
func TestGoPortMatchesORT(t *testing.T) {
	requireORT(t)

	oracle, err := NewOracle(modelPath)
	require.NoError(t, err)
	defer oracle.Destroy()

	model, err := silero.New()
	require.NoError(t, err)

	overallMax := 0.0
	for _, sig := range goldensignals.Signals() {
		want, err := oracle.Run(sig.Samples)
		require.NoError(t, err, sig.Name)

		model.Reset()
		sigMax := 0.0
		win := sig.Samples
		for w := range want {
			p, err := model.Process(win[w*silero.WindowSize : (w+1)*silero.WindowSize])
			require.NoError(t, err)
			d := float64(p) - want[w]
			if d < 0 {
				d = -d
			}
			if d > sigMax {
				sigMax = d
			}
			if d > parityTolerance {
				t.Errorf("%s window %d: p_go=%.9f p_ort=%.9f |Δ|=%.3e > %g",
					sig.Name, w, p, want[w], d, parityTolerance)
			}
		}
		t.Logf("%s: %d windows, max |Δp| = %.3e", sig.Name, len(want), sigMax)
		if sigMax > overallMax {
			overallMax = sigMax
		}
	}
	t.Logf("overall max |Δp| = %.3e (tolerance %g)", overallMax, parityTolerance)
}

// TestResetMatchesFreshState pins the state-carry contract both sides:
// after a Reset mid-stream, both the oracle and the Go port must
// reproduce their fresh-state outputs exactly.
func TestResetMatchesFreshState(t *testing.T) {
	requireORT(t)

	oracle, err := NewOracle(modelPath)
	require.NoError(t, err)
	defer oracle.Destroy()

	sig := goldensignals.Signals()[1] // speech-pulses
	head := sig.Samples[:20*silero.WindowSize]

	first, err := oracle.Run(head)
	require.NoError(t, err)
	second, err := oracle.Run(head) // Run resets carried state
	require.NoError(t, err)
	assert.Equal(t, first, second, "oracle state reset must be exact")

	model, err := silero.New()
	require.NoError(t, err)
	run := func() []float32 {
		var out []float32
		for w := 0; w < 20; w++ {
			p, err := model.Process(head[w*silero.WindowSize : (w+1)*silero.WindowSize])
			require.NoError(t, err)
			out = append(out, p)
		}
		return out
	}
	a := run()
	model.Reset()
	assert.Equal(t, a, run(), "Go model Reset must be exact")
}
