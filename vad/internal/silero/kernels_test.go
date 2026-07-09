package silero

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReflectPadRight(t *testing.T) {
	x := []float32{1, 2, 3, 4, 5}
	dst := make([]float32, len(x)+3)
	reflectPadRight(dst, x, 3)
	// Edge-excluding mirror: ..., 5 | 4, 3, 2.
	assert.Equal(t, []float32{1, 2, 3, 4, 5, 4, 3, 2}, dst)

	// The model's actual geometry: pad 64 onto 576.
	in := make([]float32, netInput)
	for i := range in {
		in[i] = float32(i)
	}
	out := make([]float32, netInput+stftPad)
	reflectPadRight(out, in, stftPad)
	assert.Equal(t, float32(netInput-2), out[netInput], "first padded sample mirrors x[len-2]")
	assert.Equal(t, float32(netInput-1-stftPad), out[netInput+stftPad-1], "last padded sample")
}

func TestConv1dK3HandComputed(t *testing.T) {
	// inC=2, inT=4, outC=2, pad 1. Expected values are the zero-padded
	// correlation computed independently in float64; every value is
	// exactly representable, so the float32 kernel must match tightly.
	x := []float32{
		1, 2, 3, 4, // channel 0
		0.5, -1, 0, 2, // channel 1
	}
	w := []float32{
		0.1, 0.2, 0.3, -0.1, 0.4, 0.05, // oc0: ic0 taps, ic1 taps
		0.5, -0.2, 0.1, 0.3, 0.2, -0.4, // oc1
	}
	b := []float32{0.1, -0.2}

	out := make([]float32, 2*4)
	outT := conv1dK3(x, 2, 4, w, b, 2, 1, out)
	require.Equal(t, 4, outT)
	want := []float32{1.05, 1.05, 2.3, 2.0, 0.3, 0.15, -0.5, 0.9}
	for i := range want {
		assert.InDelta(t, want[i], out[i], 1e-6, "stride 1, index %d", i)
	}

	out2 := make([]float32, 2*2)
	outT = conv1dK3(x, 2, 4, w, b, 2, 2, out2)
	require.Equal(t, 2, outT)
	want2 := []float32{1.05, 2.3, 0.3, -0.5}
	for i := range want2 {
		assert.InDelta(t, want2[i], out2[i], 1e-6, "stride 2, index %d", i)
	}
}

func TestRelu(t *testing.T) {
	v := []float32{-1, 0, 2.5, -0.0001, 3}
	relu(v)
	assert.Equal(t, []float32{0, 0, 2.5, 0, 3}, v)
}

// TestLSTMStepHandComputed2Unit pins the PyTorch i,f,g,o row packing
// with a 2-unit cell whose expected outputs were computed by hand
// (float64 reference, gates written out longhand). Every gate has
// distinct weights, so any packing permutation (notably ONNX's
// i,o,f,c) produces grossly different numbers — this is the trap test
// the plan requires.
func TestLSTMStepHandComputed2Unit(t *testing.T) {
	wih := []float32{
		0.5, -0.1, 0.2, 0.3, // i rows
		-0.4, 0.6, 0.1, -0.2, // f rows
		0.7, 0.2, -0.3, 0.5, // g rows
		0.1, 0.8, 0.4, -0.6, // o rows
	}
	whh := []float32{
		0.05, -0.15, 0.25, 0.35,
		-0.45, 0.55, 0.15, -0.25,
		0.65, 0.1, -0.35, 0.45,
		0.12, 0.75, 0.42, -0.62,
	}
	bih := []float32{0.01, 0.02, 0.03, 0.04, 0.05, 0.06, 0.07, 0.08}
	bhh := []float32{-0.05, -0.04, -0.03, -0.02, -0.01, 0, 0.01, 0.02}

	x := []float32{1, -0.5}
	h := []float32{0.2, -0.1}
	c := []float32{0.3, 0.4}
	gates := make([]float32, 8)
	lstmStep(wih, whh, bih, bhh, x, h, c, gates, 2)

	assert.InDelta(t, 0.19804331, h[0], 1e-6)
	assert.InDelta(t, -0.03530409, h[1], 1e-6)
	assert.InDelta(t, 0.49443907, c[0], 1e-6)
	assert.InDelta(t, -0.04905166, c[1], 1e-6)
}

// TestStftMagnitudeAgainstFloat64Reference cross-checks the fp32 STFT
// kernel against an independent float64 implementation (different
// code path and accumulation precision) over a deterministic signal,
// using the real vendored basis. This localises indexing bugs
// (framing, stride, real/imag split, reflect pad) without depending
// on the oracle.
func TestStftMagnitudeAgainstFloat64Reference(t *testing.T) {
	tensors, err := Tensors()
	require.NoError(t, err)
	basis := tensors["model.stft.forward_basis_buffer"].Data

	x := make([]float32, netInput)
	rng := uint64(0x9E3779B97F4A7C15)
	for i := range x {
		rng ^= rng << 13
		rng ^= rng >> 7
		rng ^= rng << 17
		x[i] = float32(int64(rng))/float32(1<<63)*0.8 + 0.1*float32(i%7-3)
	}

	got := make([]float32, stftBins*stftFrames)
	padded := make([]float32, netInput+stftPad)
	stftMagnitude(basis, x, padded, got)

	// Independent float64 reference.
	ref := make([]float64, netInput+stftPad)
	for i := 0; i < netInput; i++ {
		ref[i] = float64(x[i])
	}
	for j := 0; j < stftPad; j++ {
		ref[netInput+j] = float64(x[netInput-2-j])
	}
	for frame := 0; frame < stftFrames; frame++ {
		for bin := 0; bin < stftBins; bin++ {
			var re, im float64
			for n := 0; n < stftKernel; n++ {
				s := ref[frame*stftHop+n]
				re += float64(basis[bin*stftKernel+n]) * s
				im += float64(basis[(bin+stftBins)*stftKernel+n]) * s
			}
			want := math.Sqrt(re*re + im*im)
			assert.InDelta(t, want, float64(got[bin*stftFrames+frame]), 1e-3,
				"bin %d frame %d", bin, frame)
		}
	}
}
