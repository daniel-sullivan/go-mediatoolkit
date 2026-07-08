package loudness

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── gain-math exactness ─────────────────────────────────────────────────

func TestNewNormalizerGainMath(t *testing.T) {
	cases := []struct {
		name     string
		measured float64
		target   float64
	}{
		{"cut to streaming target", -18.0, -14.0},
		{"boost to podcast target", -30.0, -19.0},
		{"already on target", -23.0, -23.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := NewNormalizer(tc.measured, tc.target)
			require.NoError(t, err)
			require.NotNil(t, n)
			assert.InDelta(t, tc.target-tc.measured, n.GainDB(), 1e-9, "GainDB == target - measured")
		})
	}
}

// ── Process applies the exact constant gain regardless of chunking ─────

func TestNormalizerProcessConstantGain(t *testing.T) {
	const (
		measured = -20.0
		target   = -16.0
	)
	n, err := NewNormalizer(measured, target)
	require.NoError(t, err)

	data := testSine(0.25, 997, 48000, 0, 48000, 2)
	whole := append([]float64(nil), data...)
	n.Process(whole)

	gainLin := math.Pow(10, n.GainDB()/20)
	for i, v := range data {
		assert.InDelta(t, v*gainLin, whole[i], 1e-12, "sample %d", i)
	}

	// Re-run over the same data split into arbitrary chunks: result must
	// be identical, since the gain is stateless and constant.
	chunked := append([]float64(nil), data...)
	sizes := []int{1, 7, 100, 4001}
	pos := 0
	for len(chunked[pos:]) > 0 {
		size := sizes[pos%len(sizes)]
		if size > len(chunked)-pos {
			size = len(chunked) - pos
		}
		n.Process(chunked[pos : pos+size])
		pos += size
	}
	assert.InDeltaSlice(t, whole, chunked, 1e-12, "chunked Process must match whole-buffer Process")
}

// ── Reset is a no-op: gain is unaffected by repeated Reset/Process ─────

func TestNormalizerResetNoOp(t *testing.T) {
	n, err := NewNormalizer(-20, -16)
	require.NoError(t, err)
	before := n.GainDB()

	n.Reset()
	assert.Equal(t, before, n.GainDB(), "Reset must not change the configured gain")

	data := testSine(0.1, 997, 48000, 0, 4800, 2)
	first := append([]float64(nil), data...)
	n.Process(first)

	n.Reset()
	second := append([]float64(nil), data...)
	n.Process(second)

	assert.InDeltaSlice(t, first, second, 1e-12, "Reset must not change subsequent Process output")
}

// ── error paths ─────────────────────────────────────────────────────────

func TestNewNormalizerSilentInput(t *testing.T) {
	n, err := NewNormalizer(math.Inf(-1), -23)
	assert.ErrorIs(t, err, ErrSilentInput)
	assert.Nil(t, n)
}

func TestNewNormalizerBadConfig(t *testing.T) {
	for _, measured := range []float64{math.NaN(), math.Inf(1)} {
		n, err := NewNormalizer(measured, -23)
		assert.ErrorIsf(t, err, ErrBadConfig, "measured=%v", measured)
		assert.Nil(t, n)
	}
}

func TestNewNormalizerBadTarget(t *testing.T) {
	// Matches Normalize's own `!(target < 0)` convention (see
	// normalize.go): zero, positive, NaN, and +Inf all fail; -Inf
	// happens to satisfy `< 0` and is not rejected here either, for
	// the same reason Normalize doesn't special-case it.
	for _, target := range []float64{0, 1.0, math.NaN(), math.Inf(1)} {
		n, err := NewNormalizer(-18, target)
		assert.ErrorIsf(t, err, ErrBadTarget, "target=%v", target)
		assert.Nil(t, n)
	}
}

// ── interface satisfaction ───────────────────────────────────────────────

func TestNormalizerImplementsProcessor(t *testing.T) {
	n, err := NewNormalizer(-20, -16)
	require.NoError(t, err)
	var _ interface {
		Process([]float64)
		Reset()
	} = n
}
