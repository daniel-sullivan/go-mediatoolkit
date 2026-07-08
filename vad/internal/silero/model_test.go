package silero

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEmbeddedWeights(t *testing.T) {
	tensors, err := Tensors()
	require.NoError(t, err)
	require.Len(t, tensors, len(requiredDim))
	for name, want := range requiredDim {
		tn := tensors[name]
		assert.Equal(t, want, tn.Shape, name)
		n := 1
		for _, d := range want {
			n *= d
		}
		assert.Len(t, tn.Data, n, name)
	}
}

func TestParseSafetensorsErrors(t *testing.T) {
	mk := func(header string, data []byte) []byte {
		raw := make([]byte, 8, 8+len(header)+len(data))
		binary.LittleEndian.PutUint64(raw, uint64(len(header)))
		raw = append(raw, header...)
		return append(raw, data...)
	}
	f32 := make([]byte, 8) // two float32 zeros

	cases := []struct {
		name string
		raw  []byte
	}{
		{"truncated header length field", []byte{1, 2, 3}},
		{"header length beyond payload", mk("", nil)[:8]},
		{"invalid JSON", mk("{nope", nil)},
		{"unsupported dtype", mk(`{"t":{"dtype":"F16","shape":[2],"data_offsets":[0,4]}}`, f32)},
		{"offsets out of range", mk(`{"t":{"dtype":"F32","shape":[2],"data_offsets":[0,64]}}`, f32)},
		{"size/shape mismatch", mk(`{"t":{"dtype":"F32","shape":[3],"data_offsets":[0,8]}}`, f32)},
		{"non-positive dimension", mk(`{"t":{"dtype":"F32","shape":[0],"data_offsets":[0,0]}}`, f32)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSafetensors(tc.raw)
			assert.Error(t, err)
		})
	}

	// A well-formed tiny payload parses, and __metadata__ is skipped.
	good := mk(`{"__metadata__":{"k":"v"},"t":{"dtype":"F32","shape":[2],"data_offsets":[0,8]}}`, f32)
	tensors, err := parseSafetensors(good)
	require.NoError(t, err)
	require.Len(t, tensors, 1)
	assert.Equal(t, []float32{0, 0}, tensors["t"].Data)
}

// TestModelPinnedProbabilities pins Process against oracle
// probabilities produced by onnxruntime 1.27.0 on the vendored onnx
// model (three consecutive windows each, state carried), at the golden
// tolerance. The full-signal parity/golden suites subsume this; the
// pins keep the model package self-diagnosing.
func TestModelPinnedProbabilities(t *testing.T) {
	m, err := New()
	require.NoError(t, err)

	// Three all-zero windows.
	wantZeros := []float64{0.0016698241233825684, 0.006883949041366577, 0.00891074538230896}
	win := make([]float32, WindowSize)
	for i, want := range wantZeros {
		p, err := m.Process(win)
		require.NoError(t, err)
		assert.InDelta(t, want, float64(p), 1e-4, "zeros window %d", i)
	}

	// Three windows of a 440 Hz half-scale sine, fresh state.
	m.Reset()
	wantSine := []float64{0.0023629069328308105, 0.00047701597213745117, 0.0001926422119140625}
	for i, want := range wantSine {
		for j := range win {
			win[j] = float32(0.5 * math.Sin(2*math.Pi*440*float64(i*WindowSize+j)/SampleRate))
		}
		p, err := m.Process(win)
		require.NoError(t, err)
		assert.InDelta(t, want, float64(p), 1e-4, "sine window %d", i)
	}
}

func TestModelResetDeterminism(t *testing.T) {
	m, err := New()
	require.NoError(t, err)

	win := make([]float32, WindowSize)
	for j := range win {
		win[j] = float32(math.Sin(float64(j) * 0.05))
	}
	run := func() []float32 {
		var out []float32
		for i := 0; i < 5; i++ {
			p, err := m.Process(win)
			require.NoError(t, err)
			out = append(out, p)
		}
		return out
	}
	first := run()
	m.Reset()
	second := run()
	assert.Equal(t, first, second, "Reset must restore bit-identical behaviour")

	// Two independent instances agree bit-exactly too (shared
	// read-only weights, private state).
	m2, err := New()
	require.NoError(t, err)
	var third []float32
	for i := 0; i < 5; i++ {
		p, err := m2.Process(win)
		require.NoError(t, err)
		third = append(third, p)
	}
	assert.Equal(t, first, third)
}

func TestModelWindowSizeError(t *testing.T) {
	m, err := New()
	require.NoError(t, err)
	_, err = m.Process(make([]float32, WindowSize-1))
	assert.ErrorIs(t, err, ErrWindowSize)
	_, err = m.Process(make([]float32, WindowSize+1))
	assert.ErrorIs(t, err, ErrWindowSize)
	_, err = m.Process(nil)
	assert.ErrorIs(t, err, ErrWindowSize)
}

// TestEmbeddedHeaderIsCanonical guards the vendored file's header
// against accidental re-generation drift: names must be the onnx
// initializer names (weights.go depends on them) and the payload must
// parse with zero unused tensors.
func TestEmbeddedHeaderIsCanonical(t *testing.T) {
	require.GreaterOrEqual(t, len(embeddedSafetensors), 8)
	headerLen := binary.LittleEndian.Uint64(embeddedSafetensors)
	var header map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(embeddedSafetensors[8:8+headerLen], &header))
	for name := range header {
		if name == "__metadata__" {
			continue
		}
		_, ok := requiredDim[name]
		assert.True(t, ok, "unexpected tensor %q in embedded weights", name)
	}
}
