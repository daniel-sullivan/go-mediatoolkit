// Package silero is the hand-ported Silero VAD v6.2.1 fixed graph
// (silero_vad_16k_op15.onnx) used by vad.NewSileroDetector: a pure-Go,
// dependency-free implementation of the exact operator sequence
// recorded in VERSION, running the vendored MIT-licensed weights
// embedded from silero_vad_16k_op15.safetensors.
//
// The port is parity-gated against the original ONNX model executed
// by onnxruntime (per-window |Δp| ≤ 1e-4): the opt-in cgo oracle lives
// in vad/internal/parity_tests/silero_ort, and a default-CI golden
// gate (vad/silero_golden_test.go) pins oracle-produced probabilities
// without needing onnxruntime installed. See VERSION for the full
// graph dump, weight provenance, and the upstream-safetensors
// divergence note.
package silero

import (
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
)

// Embedded runtime weights: converted BY US from the onnx oracle's
// initializers (the upstream silero_vad_16k.safetensors is a different
// checkpoint — see VERSION). Tensor names are the onnx initializer
// names verbatim.
//
//go:embed silero_vad_16k_op15.safetensors
var embeddedSafetensors []byte

// Tensor is one parsed weight tensor: row-major float32 data plus its
// shape. Data aliases the embedded payload's decoded values and must
// be treated as read-only.
type Tensor struct {
	Shape []int
	Data  []float32
}

// safetensorsEntry is one header record of the safetensors format:
// 8-byte little-endian header length, a JSON object mapping tensor
// name → {dtype, shape, data_offsets}, then the raw data region the
// offsets index into.
type safetensorsEntry struct {
	Dtype       string `json:"dtype"`
	Shape       []int  `json:"shape"`
	DataOffsets [2]int `json:"data_offsets"`
}

// parseSafetensors decodes a safetensors payload into named tensors.
// Only what the vendored file uses is supported: little-endian F32
// tensors. The optional __metadata__ entry is skipped.
func parseSafetensors(raw []byte) (map[string]Tensor, error) {
	if len(raw) < 8 {
		return nil, errors.New("silero: safetensors payload shorter than its header length field")
	}
	headerLen := binary.LittleEndian.Uint64(raw)
	if headerLen > uint64(len(raw)-8) {
		return nil, fmt.Errorf("silero: safetensors header length %d exceeds payload", headerLen)
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(raw[8:8+headerLen], &header); err != nil {
		return nil, fmt.Errorf("silero: safetensors header: %w", err)
	}
	data := raw[8+headerLen:]

	tensors := make(map[string]Tensor, len(header))
	for name, rawEntry := range header {
		if name == "__metadata__" {
			continue
		}
		var e safetensorsEntry
		if err := json.Unmarshal(rawEntry, &e); err != nil {
			return nil, fmt.Errorf("silero: safetensors entry %q: %w", name, err)
		}
		if e.Dtype != "F32" {
			return nil, fmt.Errorf("silero: tensor %q has dtype %q, want F32", name, e.Dtype)
		}
		n := 1
		for _, d := range e.Shape {
			if d <= 0 {
				return nil, fmt.Errorf("silero: tensor %q has non-positive dimension %d", name, d)
			}
			n *= d
		}
		start, end := e.DataOffsets[0], e.DataOffsets[1]
		if start < 0 || end < start || end > len(data) {
			return nil, fmt.Errorf("silero: tensor %q offsets [%d,%d) outside data region of %d bytes", name, start, end, len(data))
		}
		if end-start != 4*n {
			return nil, fmt.Errorf("silero: tensor %q has %d bytes for %d float32 elements", name, end-start, n)
		}
		vals := make([]float32, n)
		for i := range vals {
			vals[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[start+4*i:]))
		}
		tensors[name] = Tensor{Shape: e.Shape, Data: vals}
	}
	return tensors, nil
}

// Tensors parses the embedded safetensors payload. Exported for the
// silero_ort parity slice, which byte-verifies these tensors against
// the onnx oracle's initializers. The result is shared and read-only.
func Tensors() (map[string]Tensor, error) {
	return loadTensors()
}

// modelWeights is the typed view of the fifteen graph initializers.
type modelWeights struct {
	stftBasis Tensor // model.stft.forward_basis_buffer [258,1,256]

	// Encoder stages 0..3: weight [outC, inC, 3], bias [outC].
	encW [4]Tensor
	encB [4]Tensor

	lstmWih Tensor // model.decoder.rnn.weight_ih [512,128] (PyTorch i,f,g,o rows)
	lstmWhh Tensor // model.decoder.rnn.weight_hh [512,128]
	lstmBih Tensor // model.decoder.rnn.bias_ih   [512]
	lstmBhh Tensor // model.decoder.rnn.bias_hh   [512]

	headW Tensor // model.decoder.decoder.2.weight [1,128,1]
	headB Tensor // model.decoder.decoder.2.bias   [1]
}

// encoderChannels[i] / encoderChannels[i+1] are stage i's in/out
// channel counts; encoderStrides its convolution strides.
var (
	encoderChannels = [5]int{stftBins, 128, 64, 64, 128}
	encoderStrides  = [4]int{1, 2, 2, 1}
)

var (
	loadOnce    sync.Once
	loadedT     map[string]Tensor
	loadedW     *modelWeights
	loadedErr   error
	requiredDim = map[string][]int{
		"model.stft.forward_basis_buffer":     {2 * stftBins, 1, stftKernel},
		"model.encoder.0.reparam_conv.weight": {128, stftBins, 3},
		"model.encoder.0.reparam_conv.bias":   {128},
		"model.encoder.1.reparam_conv.weight": {64, 128, 3},
		"model.encoder.1.reparam_conv.bias":   {64},
		"model.encoder.2.reparam_conv.weight": {64, 64, 3},
		"model.encoder.2.reparam_conv.bias":   {64},
		"model.encoder.3.reparam_conv.weight": {128, 64, 3},
		"model.encoder.3.reparam_conv.bias":   {128},
		"model.decoder.rnn.weight_ih":         {4 * hiddenSize, hiddenSize},
		"model.decoder.rnn.weight_hh":         {4 * hiddenSize, hiddenSize},
		"model.decoder.rnn.bias_ih":           {4 * hiddenSize},
		"model.decoder.rnn.bias_hh":           {4 * hiddenSize},
		"model.decoder.decoder.2.weight":      {1, hiddenSize, 1},
		"model.decoder.decoder.2.bias":        {1},
	}
)

// loadTensors parses and shape-checks the embedded payload once;
// every Model shares the read-only result.
func loadTensors() (map[string]Tensor, error) {
	loadOnce.Do(func() {
		tensors, err := parseSafetensors(embeddedSafetensors)
		if err != nil {
			loadedErr = err
			return
		}
		for name, want := range requiredDim {
			t, ok := tensors[name]
			if !ok {
				loadedErr = fmt.Errorf("silero: embedded weights missing tensor %q", name)
				return
			}
			if !equalShape(t.Shape, want) {
				loadedErr = fmt.Errorf("silero: tensor %q has shape %v, want %v", name, t.Shape, want)
				return
			}
		}
		loadedT = tensors
		loadedW = &modelWeights{
			stftBasis: tensors["model.stft.forward_basis_buffer"],
			lstmWih:   tensors["model.decoder.rnn.weight_ih"],
			lstmWhh:   tensors["model.decoder.rnn.weight_hh"],
			lstmBih:   tensors["model.decoder.rnn.bias_ih"],
			lstmBhh:   tensors["model.decoder.rnn.bias_hh"],
			headW:     tensors["model.decoder.decoder.2.weight"],
			headB:     tensors["model.decoder.decoder.2.bias"],
		}
		for i := 0; i < 4; i++ {
			loadedW.encW[i] = tensors[fmt.Sprintf("model.encoder.%d.reparam_conv.weight", i)]
			loadedW.encB[i] = tensors[fmt.Sprintf("model.encoder.%d.reparam_conv.bias", i)]
		}
	})
	return loadedT, loadedErr
}

func equalShape(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
