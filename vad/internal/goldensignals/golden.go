// Package goldensignals also defines the golden-file schema shared by
// the generator (silero_ort/gen) and the default-CI golden test, so
// the two can never drift structurally.

package goldensignals

import (
	"encoding/json"
	"fmt"
	"os"
)

// GoldenFile is the on-disk schema of vad/testdata/silero_golden.json:
// oracle-produced per-window speech probabilities for every signal in
// Signals(), plus the provenance needed to regenerate it.
type GoldenFile struct {
	// OnnxRuntimeVersion is the onnxruntime that produced the values.
	OnnxRuntimeVersion string `json:"onnxruntime_version"`

	// ModelSHA256 is the sha256 of the onnx model file used.
	ModelSHA256 string `json:"model_sha256"`

	// SampleRate and WindowSize pin the walk geometry.
	SampleRate int `json:"sample_rate"`
	WindowSize int `json:"window_size"`

	// Probabilities maps Signal.Name → per-window oracle speech
	// probabilities, in window order.
	Probabilities map[string][]float64 `json:"probabilities"`
}

// LoadGolden reads and decodes a golden file.
func LoadGolden(path string) (*GoldenFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var g GoldenFile
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("goldensignals: decoding %s: %w", path, err)
	}
	return &g, nil
}
