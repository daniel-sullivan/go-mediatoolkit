package silero

import (
	"errors"
	"fmt"
)

// Model dimensions, fixed by the vendored 16 kHz graph.
const (
	// SampleRate is the only rate the vendored model supports.
	SampleRate = 16000

	// WindowSize is the number of new samples per Process call.
	WindowSize = 512

	// ContextSize is how many trailing samples of the previous
	// network input are prepended to each window (zeros at start /
	// after Reset) — the utils_vad.py OnnxWrapper convention.
	ContextSize = 64

	// netInput is the network's actual input length per step.
	netInput = ContextSize + WindowSize

	// hiddenSize is the LSTM (and encoder-output) width.
	hiddenSize = 128
)

// ErrWindowSize is returned by Process for a window whose length is
// not exactly WindowSize.
var ErrWindowSize = errors.New("silero: window must be exactly 512 samples")

// Model is one streaming instance of the Silero VAD graph: the shared
// read-only weights plus this stream's carried state (64-sample
// context, LSTM h/c) and scratch buffers. Each Model serves one
// stream; create one per detector. Not safe for concurrent use.
type Model struct {
	w *modelWeights

	// Carried stream state.
	context [ContextSize]float32
	h, c    [hiddenSize]float32

	// Scratch (sized once; Process is allocation-free).
	input  [netInput]float32
	padded [netInput + stftPad]float32
	mag    [stftBins * stftFrames]float32
	enc    [4][]float32
	gates  [4 * hiddenSize]float32
}

// New builds a Model over the embedded weights (parsed and
// shape-checked once, shared by all instances). It fails only if the
// embedded payload is corrupt.
func New() (*Model, error) {
	if _, err := loadTensors(); err != nil {
		return nil, err
	}
	m := &Model{w: loadedW}
	t := stftFrames
	for i := range m.enc {
		t = (t-1)/encoderStrides[i] + 1
		m.enc[i] = make([]float32, encoderChannels[i+1]*t)
	}
	if len(m.enc[3]) != hiddenSize {
		// The stride pyramid must land on exactly one frame of 128
		// channels — the graph squeezes that axis before the LSTM.
		return nil, fmt.Errorf("silero: encoder output is %d values, want %d", len(m.enc[3]), hiddenSize)
	}
	return m, nil
}

// Process consumes one WindowSize-sample 16 kHz mono window (nominal
// [-1, 1] float32) and returns the model's speech probability for it,
// advancing the carried context and LSTM state. It is the exact
// operator sequence recorded in VERSION: context‖window → reflect-pad
// STFT magnitudes → four Conv1d+ReLU encoder stages → LSTM cell →
// ReLU → 1×1 conv → sigmoid.
func (m *Model) Process(window []float32) (float32, error) {
	if len(window) != WindowSize {
		return 0, ErrWindowSize
	}

	copy(m.input[:ContextSize], m.context[:])
	copy(m.input[ContextSize:], window)

	stftMagnitude(m.w.stftBasis.Data, m.input[:], m.padded[:], m.mag[:])

	x, t := m.mag[:], stftFrames
	for i := 0; i < 4; i++ {
		t = conv1dK3(x, encoderChannels[i], t,
			m.w.encW[i].Data, m.w.encB[i].Data, encoderChannels[i+1], encoderStrides[i], m.enc[i])
		relu(m.enc[i][:encoderChannels[i+1]*t])
		x = m.enc[i]
	}

	lstmStep(m.w.lstmWih.Data, m.w.lstmWhh.Data, m.w.lstmBih.Data, m.w.lstmBhh.Data,
		x, m.h[:], m.c[:], m.gates[:], hiddenSize)

	// Output head: ReLU on h feeds the 1×1 conv (h itself is carried
	// un-rectified), then sigmoid.
	acc := m.w.headB.Data[0]
	for j := 0; j < hiddenSize; j++ {
		v := m.h[j]
		if v < 0 {
			v = 0
		}
		acc += m.w.headW.Data[j] * v
	}
	p := sigmoid32(acc)

	copy(m.context[:], m.input[netInput-ContextSize:])
	return p, nil
}

// Reset clears the carried stream state (context and LSTM h/c) back
// to the just-constructed zeros. Weights are shared and unaffected.
func (m *Model) Reset() {
	clear(m.context[:])
	clear(m.h[:])
	clear(m.c[:])
}
