package nativeopus

// Exported accessors for the MLP runtime parity tests.
// The C oracle lives in libopus/src/mlp.c; these call the Go port
// with the shipped weight tables (layer0, layer1, layer2).

// ExportTestMLPTansigApprox exposes tansig_approx for direct bit-exact
// comparison against the C static inline.
func ExportTestMLPTansigApprox(x float32) float32 { return tansig_approx(x) }

// ExportTestMLPSigmoidApprox exposes sigmoid_approx likewise.
func ExportTestMLPSigmoidApprox(x float32) float32 { return sigmoid_approx(x) }

// ExportTestMLPComputeDenseLayer0 runs analysis_compute_dense against the
// shipped layer0 weights (25 inputs -> 32 neurons, tansig activation).
func ExportTestMLPComputeDenseLayer0(input []float32) []float32 {
	out := make([]float32, layer0.nb_neurons)
	analysis_compute_dense(&layer0, out, input)
	return out
}

// ExportTestMLPComputeDenseLayer2 runs analysis_compute_dense against the
// shipped layer2 weights (24 inputs -> 2 neurons, sigmoid activation).
func ExportTestMLPComputeDenseLayer2(input []float32) []float32 {
	out := make([]float32, layer2.nb_neurons)
	analysis_compute_dense(&layer2, out, input)
	return out
}

// ExportTestMLPComputeGRULayer1 runs one step of analysis_compute_gru for
// layer1 (32 inputs, 24 neurons). The caller provides the input vector
// and a mutable state vector of length 24; the state is updated in place.
func ExportTestMLPComputeGRULayer1(state []float32, input []float32) {
	analysis_compute_gru(&layer1, state, input)
}
