//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_MLP_TansigApprox — rational approximation of tansig used
// inside MLP activations.
func TestParity_MLP_TansigApprox(t *testing.T) {
	inputs := []float32{}
	for x := float32(-12); x <= 12; x += 0.0137 {
		inputs = append(inputs, x)
	}
	// A few explicit edges plus random samples across the whole plausible
	// input domain (accumulator outputs of a 25-input int8 gemm scaled by
	// 1/128 can still produce large magnitudes).
	inputs = append(inputs, 0, -0, 1e-20, -1e-20, 1e-6, -1e-6, 100, -100, 500, -500)
	r := rand.New(rand.NewSource(0xBEEF))
	for i := 0; i < 2000; i++ {
		inputs = append(inputs, float32(r.Float64()*200-100))
	}
	for _, x := range inputs {
		want := cMLPTansigApprox(x)
		got := nativeopus.ExportTestMLPTansigApprox(x)
		if !bitExactF32(want, got) {
			reportMismatch(t, "mlp_tansig_approx", formatF(x), want, got)
		}
	}
}

// TestParity_MLP_SigmoidApprox — simple linear transform of tansig.
func TestParity_MLP_SigmoidApprox(t *testing.T) {
	inputs := []float32{}
	for x := float32(-24); x <= 24; x += 0.0273 {
		inputs = append(inputs, x)
	}
	inputs = append(inputs, 0, -0, 1e-20, -1e-20, 1e-6, -1e-6, 100, -100, 1000, -1000)
	r := rand.New(rand.NewSource(0xF00D))
	for i := 0; i < 2000; i++ {
		inputs = append(inputs, float32(r.Float64()*400-200))
	}
	for _, x := range inputs {
		want := cMLPSigmoidApprox(x)
		got := nativeopus.ExportTestMLPSigmoidApprox(x)
		if !bitExactF32(want, got) {
			reportMismatch(t, "mlp_sigmoid_approx", formatF(x), want, got)
		}
	}
}

// TestParity_MLP_ComputeDenseLayer0 — drives the shipped analysis MLP
// layer0 (25 inputs, 32 neurons, tansig activation). Random inputs
// cover a realistic range for analysis features.
func TestParity_MLP_ComputeDenseLayer0(t *testing.T) {
	r := rand.New(rand.NewSource(0xABCD))
	for trial := 0; trial < 500; trial++ {
		in := make([]float32, 25)
		for i := range in {
			in[i] = float32(r.Float64()*4 - 2)
		}
		want := cMLPComputeDenseLayer0(in)
		got := nativeopus.ExportTestMLPComputeDenseLayer0(in)
		for i := range want {
			if !bitExactF32(want[i], got[i]) {
				t.Errorf("trial=%d layer0 out[%d]: want %g (0x%08x) got %g (0x%08x) ULP=%d",
					trial, i, want[i], math.Float32bits(want[i]),
					got[i], math.Float32bits(got[i]),
					int64(math.Float32bits(got[i]))-int64(math.Float32bits(want[i])))
				break
			}
		}
	}
}

// TestParity_MLP_ComputeDenseLayer2 — final output layer (24 in, 2 out,
// sigmoid activation).
func TestParity_MLP_ComputeDenseLayer2(t *testing.T) {
	r := rand.New(rand.NewSource(0xDEAD))
	for trial := 0; trial < 500; trial++ {
		in := make([]float32, 24)
		for i := range in {
			in[i] = float32(r.Float64()*2 - 1)
		}
		want := cMLPComputeDenseLayer2(in)
		got := nativeopus.ExportTestMLPComputeDenseLayer2(in)
		for i := range want {
			if !bitExactF32(want[i], got[i]) {
				t.Errorf("trial=%d layer2 out[%d]: want %g (0x%08x) got %g (0x%08x) ULP=%d",
					trial, i, want[i], math.Float32bits(want[i]),
					got[i], math.Float32bits(got[i]),
					int64(math.Float32bits(got[i]))-int64(math.Float32bits(want[i])))
				break
			}
		}
	}
}

// TestParity_MLP_ComputeGRULayer1 — the recurrent layer. The GRU update
// writes state in place, so we re-run both sides from the same fresh
// state per trial and compare the post-step state.
func TestParity_MLP_ComputeGRULayer1(t *testing.T) {
	r := rand.New(rand.NewSource(0xFEED))
	for trial := 0; trial < 200; trial++ {
		in := make([]float32, 32)
		for i := range in {
			in[i] = float32(r.Float64()*2 - 1)
		}
		seed := make([]float32, 24)
		for i := range seed {
			seed[i] = float32(r.Float64()*2 - 1)
		}
		stateC := make([]float32, 24)
		stateG := make([]float32, 24)
		copy(stateC, seed)
		copy(stateG, seed)
		cMLPComputeGRULayer1(stateC, in)
		nativeopus.ExportTestMLPComputeGRULayer1(stateG, in)
		for i := range stateC {
			if !bitExactF32(stateC[i], stateG[i]) {
				t.Errorf("trial=%d gru state[%d]: want %g (0x%08x) got %g (0x%08x) ULP=%d",
					trial, i, stateC[i], math.Float32bits(stateC[i]),
					stateG[i], math.Float32bits(stateG[i]),
					int64(math.Float32bits(stateG[i]))-int64(math.Float32bits(stateC[i])))
				break
			}
		}
	}
}

// TestParity_MLP — alias test that drives the three layers as a chain.
// This is the "end-to-end MLP inference" check the task spec names in
// the -run regex.
func TestParity_MLP(t *testing.T) {
	t.Run("TansigApprox", TestParity_MLP_TansigApprox)
	t.Run("SigmoidApprox", TestParity_MLP_SigmoidApprox)
	t.Run("DenseLayer0", TestParity_MLP_ComputeDenseLayer0)
	t.Run("DenseLayer2", TestParity_MLP_ComputeDenseLayer2)
	t.Run("GRULayer1", TestParity_MLP_ComputeGRULayer1)
}
