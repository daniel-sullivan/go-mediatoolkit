//go:build cgo

package silero_ort

import (
	"errors"
	"fmt"
	"os"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/silero"
)

// SharedLibEnv names the environment variable that must point at the
// onnxruntime shared library (libonnxruntime.{dylib,so}) for the
// oracle to run. Unset → the parity tests skip and the generator
// refuses to run.
const SharedLibEnv = "ONNXRUNTIME_SHARED_LIB"

// ModelSHA256 is the pinned digest of the vendored oracle model —
// silero-vad v6.2.1 silero_vad_16k_op15.onnx (see
// vad/internal/silero/VERSION). Tests assert the testdata file still
// hashes to this, and the golden file records it.
const ModelSHA256 = "7ed98ddbad84ccac4cd0aeb3099049280713df825c610a8ed34543318f1b2c49"

var (
	envOnce sync.Once
	envErr  error
)

// InitEnvironment initialises the onnxruntime environment from
// SharedLibEnv, once per process (onnxruntime_go keeps a global
// environment; per-test init/destroy churn is pointless). Returns an
// error wrapping ErrNoSharedLib when the variable is unset.
func InitEnvironment() error {
	envOnce.Do(func() {
		lib := os.Getenv(SharedLibEnv)
		if lib == "" {
			envErr = fmt.Errorf("%w: set %s to the onnxruntime shared library path", ErrNoSharedLib, SharedLibEnv)
			return
		}
		ort.SetSharedLibraryPath(lib)
		if err := ort.InitializeEnvironment(); err != nil {
			envErr = fmt.Errorf("silero_ort: initialising onnxruntime from %s=%s: %w", SharedLibEnv, lib, err)
		}
	})
	return envErr
}

// ErrNoSharedLib signals that SharedLibEnv is unset — callers turn it
// into a test skip or a generator usage error.
var ErrNoSharedLib = errors.New("silero_ort: ONNXRUNTIME_SHARED_LIB not set")

// Oracle drives the vendored onnx model exactly the way the upstream
// utils_vad.py OnnxWrapper does: per 512-sample window, input =
// 64 carried context samples ++ window (context zero at start/reset),
// state [2,1,128] carried between runs, sr = 16000.
type Oracle struct {
	session *ort.AdvancedSession
	input   *ort.Tensor[float32] // [1, 576]
	state   *ort.Tensor[float32] // [2, 1, 128]
	sr      *ort.Scalar[int64]
	output  *ort.Tensor[float32] // [1, 1]
	stateN  *ort.Tensor[float32] // [2, 1, 128]
}

// NewOracle opens the model at modelPath. InitEnvironment must have
// succeeded first.
func NewOracle(modelPath string) (*Oracle, error) {
	o := &Oracle{}
	var err error
	if o.input, err = ort.NewEmptyTensor[float32](ort.NewShape(1, silero.ContextSize+silero.WindowSize)); err != nil {
		return nil, err
	}
	if o.state, err = ort.NewEmptyTensor[float32](ort.NewShape(2, 1, 128)); err != nil {
		o.Destroy()
		return nil, err
	}
	if o.sr, err = ort.NewScalar[int64](silero.SampleRate); err != nil {
		o.Destroy()
		return nil, err
	}
	if o.output, err = ort.NewEmptyTensor[float32](ort.NewShape(1, 1)); err != nil {
		o.Destroy()
		return nil, err
	}
	if o.stateN, err = ort.NewEmptyTensor[float32](ort.NewShape(2, 1, 128)); err != nil {
		o.Destroy()
		return nil, err
	}
	o.session, err = ort.NewAdvancedSession(modelPath,
		[]string{"input", "state", "sr"},
		[]string{"output", "stateN"},
		[]ort.Value{o.input, o.state, o.sr},
		[]ort.Value{o.output, o.stateN},
		nil)
	if err != nil {
		o.Destroy()
		return nil, err
	}
	return o, nil
}

// Run resets the oracle's carried state and returns one probability
// per full 512-sample window of sig (any trailing partial window is
// ignored, matching the Go model's consumers).
func (o *Oracle) Run(sig []float32) ([]float64, error) {
	in := o.input.GetData()
	clear(in[:silero.ContextSize]) // zero initial context
	clear(o.state.GetData())

	n := len(sig) / silero.WindowSize
	probs := make([]float64, 0, n)
	for w := 0; w < n; w++ {
		copy(in[silero.ContextSize:], sig[w*silero.WindowSize:(w+1)*silero.WindowSize])
		if err := o.session.Run(); err != nil {
			return nil, fmt.Errorf("silero_ort: window %d: %w", w, err)
		}
		probs = append(probs, float64(o.output.GetData()[0]))
		copy(o.state.GetData(), o.stateN.GetData())
		copy(in[:silero.ContextSize], in[len(in)-silero.ContextSize:])
	}
	return probs, nil
}

// Destroy releases the session and tensors.
func (o *Oracle) Destroy() {
	if o.session != nil {
		_ = o.session.Destroy()
	}
	if o.input != nil {
		_ = o.input.Destroy()
	}
	if o.state != nil {
		_ = o.state.Destroy()
	}
	if o.sr != nil {
		_ = o.sr.Destroy()
	}
	if o.output != nil {
		_ = o.output.Destroy()
	}
	if o.stateN != nil {
		_ = o.stateN.Destroy()
	}
}
