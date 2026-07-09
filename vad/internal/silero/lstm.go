package silero

import "math"

// LSTM cell of the decoder RNN (VERSION step 3). The vendored
// weights are PyTorch LSTMCell-packed — four hidden-size row blocks
// in i, f, g, o order in both weight matrices and both bias vectors —
// and are consumed directly with the standard cell equations. (The
// onnx graph permutes the same initializers to ONNX's i,o,f,c order
// before its LSTM node; doing the permutation is the node's concern,
// not the math's — see VERSION. The packing is pinned by a
// hand-computed 2-unit cell test.)
//
//	i = σ(W_i·x + b_i + U_i·h + v_i)
//	f = σ(W_f·x + b_f + U_f·h + v_f)
//	g = tanh(W_g·x + b_g + U_g·h + v_g)
//	o = σ(W_o·x + b_o + U_o·h + v_o)
//	c' = f∘c + i∘g
//	h' = o∘tanh(c')

// lstmStep advances one cell step in place: wih/whh are
// [4·hidden × inSize] / [4·hidden × hidden] row-major, bih/bhh
// [4·hidden], x [inSize], h/c [hidden] (updated), gates a
// [4·hidden] scratch. Accumulation is fixed sequential float32:
// both biases, then the input dot, then the recurrent dot.
func lstmStep(wih, whh, bih, bhh, x, h, c, gates []float32, hidden int) {
	inSize := len(x)
	for r := 0; r < 4*hidden; r++ {
		acc := bih[r] + bhh[r]
		wRow := wih[r*inSize : (r+1)*inSize]
		for j := 0; j < inSize; j++ {
			acc += wRow[j] * x[j]
		}
		uRow := whh[r*hidden : (r+1)*hidden]
		for j := 0; j < hidden; j++ {
			acc += uRow[j] * h[j]
		}
		gates[r] = acc
	}
	for j := 0; j < hidden; j++ {
		i := sigmoid32(gates[j])
		f := sigmoid32(gates[hidden+j])
		g := tanh32(gates[2*hidden+j])
		o := sigmoid32(gates[3*hidden+j])
		c[j] = f*c[j] + i*g
		h[j] = o * tanh32(c[j])
	}
}

// sigmoid32 is the logistic function on a float32, computed through
// float64 transcendentals (Go's only portable ones) and rounded back.
// onnxruntime uses its own fp32 polynomial approximations, so the two
// agree to ~1e-7 — inside the parity budget, per the tolerance
// justification in the silero_ort slice.
func sigmoid32(x float32) float32 {
	return float32(1 / (1 + math.Exp(-float64(x))))
}

// tanh32 is float32 tanh via float64, same rationale as sigmoid32.
func tanh32(x float32) float32 {
	return float32(math.Tanh(float64(x)))
}
