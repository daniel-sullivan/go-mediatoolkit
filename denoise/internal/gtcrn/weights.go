// Package gtcrn is the pure-Go hand-port of the GTCRN streaming speech
// enhancement model (github.com/Xiaobin-Rong/gtcrn, MIT code + MIT DNS3
// weights). It reproduces the exact operator sequence of the vendored
// gtcrn.onnx streaming graph (see VERSION for the full porting spec):
// an STFT front end (stft.go, gated against torch goldens), ERB band
// merge/split, a ShuffleNet-style encoder with SFE + grouped temporal
// convolutions + TRA attention, two grouped dual-path RNNs, a mirrored
// decoder, and a complex ratio mask — carrying three recurrent caches
// frame to frame.
//
// The port is parity-gated against the original ONNX model executed by
// onnxruntime (mixed tolerance max(|Δ| ≤ 1e-4, rel ≤ 1e-3)): the opt-in
// cgo oracle lives in denoise/internal/parity_tests/gtcrn_ort, and a
// default-CI golden gate pins oracle-produced tensors without needing
// onnxruntime installed. Runtime weights are the oracle's own float32
// initializers, embedded verbatim from gtcrn_weights.safetensors and
// byte-verified against the onnx graph (the Silero precedent).
package gtcrn

import (
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
)

// Embedded runtime weights: every float32 initializer of the vendored
// gtcrn.onnx, verbatim names (VERSION artifact 4). Opaque names
// (onnx::Conv_*, onnx::GRU_*, onnx::MatMul_*) are decoded by manifest.json.
//
//go:embed gtcrn_weights.safetensors
var embeddedSafetensors []byte

// Tensor is one parsed weight tensor: row-major float32 data plus its
// shape. Data aliases the decoded payload and must be treated read-only.
type Tensor struct {
	Shape []int
	Data  []float32
}

// safetensorsEntry is one header record: little-endian F32 tensor with a
// shape and a [start,end) slice into the raw data region.
type safetensorsEntry struct {
	Dtype       string `json:"dtype"`
	Shape       []int  `json:"shape"`
	DataOffsets [2]int `json:"data_offsets"`
}

// parseSafetensors decodes a little-endian F32 safetensors payload into
// named tensors (the only dtype the vendored file uses).
func parseSafetensors(raw []byte) (map[string]Tensor, error) {
	if len(raw) < 8 {
		return nil, errors.New("gtcrn: safetensors payload shorter than its header length field")
	}
	headerLen := binary.LittleEndian.Uint64(raw)
	if headerLen > uint64(len(raw)-8) {
		return nil, fmt.Errorf("gtcrn: safetensors header length %d exceeds payload", headerLen)
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(raw[8:8+headerLen], &header); err != nil {
		return nil, fmt.Errorf("gtcrn: safetensors header: %w", err)
	}
	data := raw[8+headerLen:]

	tensors := make(map[string]Tensor, len(header))
	for name, rawEntry := range header {
		if name == "__metadata__" {
			continue
		}
		var e safetensorsEntry
		if err := json.Unmarshal(rawEntry, &e); err != nil {
			return nil, fmt.Errorf("gtcrn: safetensors entry %q: %w", name, err)
		}
		if e.Dtype != "F32" {
			return nil, fmt.Errorf("gtcrn: tensor %q has dtype %q, want F32", name, e.Dtype)
		}
		n := 1
		for _, d := range e.Shape {
			if d <= 0 {
				return nil, fmt.Errorf("gtcrn: tensor %q has non-positive dimension %d", name, d)
			}
			n *= d
		}
		start, end := e.DataOffsets[0], e.DataOffsets[1]
		if start < 0 || end < start || end > len(data) {
			return nil, fmt.Errorf("gtcrn: tensor %q offsets [%d,%d) outside data region of %d bytes", name, start, end, len(data))
		}
		if end-start != 4*n {
			return nil, fmt.Errorf("gtcrn: tensor %q has %d bytes for %d float32 elements", name, end-start, n)
		}
		vals := make([]float32, n)
		for i := range vals {
			vals[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[start+4*i:]))
		}
		tensors[name] = Tensor{Shape: e.Shape, Data: vals}
	}
	return tensors, nil
}

var (
	loadOnce  sync.Once
	loadedT   map[string]Tensor
	loadedErr error
)

// loadTensors parses the embedded payload once; the read-only result is
// shared by every Model.
func loadTensors() (map[string]Tensor, error) {
	loadOnce.Do(func() {
		loadedT, loadedErr = parseSafetensors(embeddedSafetensors)
	})
	return loadedT, loadedErr
}

// Tensors parses the embedded safetensors payload. Exported for the
// gtcrn_ort parity slice, which byte-verifies these tensors against the
// onnx oracle's initializers. The result is shared and read-only.
func Tensors() (map[string]Tensor, error) {
	return loadTensors()
}
