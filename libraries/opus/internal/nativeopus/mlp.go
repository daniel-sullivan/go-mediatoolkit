package nativeopus

// Port of libopus/src/mlp.c and mlp.h — MLP runtime used by analysis.c.
//
// 1:1 port. Every multiply-accumulate uses fma_add (or equivalent) to
// match the C oracle compiled with -ffp-contract=off. The C source uses
// a local `fmadd(a,b,c) ((a)*(b)+(c))` macro inside tansig_approx only
// (sigmoid_approx, gemm_accum, and the GRU recurrence expressions all
// use bare `a + b*c`). We wrap every such expression through fma_add to
// defeat Go arm64 FMADDS fusion and stay bit-exact with C.
//
// Types follow the header: weights and biases are opus_int8, activations
// and accumulators are float (opus_val16/opus_val32 here would also be
// float32, but mlp.c uses `float` directly so we do the same).

const WEIGHTS_SCALE float32 = 1.0 / 128

const MAX_NEURONS = 32

type AnalysisDenseLayer struct {
	bias          []opus_int8
	input_weights []opus_int8
	nb_inputs     int
	nb_neurons    int
	sigmoid       int
}

type AnalysisGRULayer struct {
	bias              []opus_int8
	input_weights     []opus_int8
	recurrent_weights []opus_int8
	nb_inputs         int
	nb_neurons        int
}

// tansig_approx — literal port of the C static inline.
// C:
//
//	const float N0 = 952.52801514f; ...
//	X2 = x*x;
//	num = fmadd(fmadd(N2, X2, N1), X2, N0);
//	den = fmadd(fmadd(D2, X2, D1), X2, D0);
//	num = num*x/den;
//	return MAX32(-1.f, MIN32(1.f, num));
//
// fmadd(a,b,c) = a*b + c. We translate each fmadd as fma_add(c, a, b)
// (note: fma_add(a, b, c) returns a + b*c).
func tansig_approx(x float32) float32 {
	const N0 float32 = 952.52801514
	const N1 float32 = 96.39235687
	const N2 float32 = 0.60863042
	const D0 float32 = 952.72399902
	const D1 float32 = 413.36801147
	const D2 float32 = 11.88600922
	var X2, num, den float32
	X2 = mul_f32(x, x)
	// num = fmadd(fmadd(N2, X2, N1), X2, N0) == ((N2*X2)+N1)*X2 + N0
	num = fma_add(N0, fma_add(N1, N2, X2), X2)
	// den = fmadd(fmadd(D2, X2, D1), X2, D0)
	den = fma_add(D0, fma_add(D1, D2, X2), X2)
	// num = num*x/den
	num = mul_f32(num, x) / den
	// MAX32(-1.f, MIN32(1.f, num))
	if num > 1.0 {
		num = 1.0
	}
	if num < -1.0 {
		num = -1.0
	}
	return num
}

// sigmoid_approx — C:
//
//	return .5f + .5f*tansig_approx(.5f*x);
func sigmoid_approx(x float32) float32 {
	return fma_add(0.5, 0.5, tansig_approx(mul_f32(0.5, x)))
}

// gemm_accum — C:
//
//	for (i=0;i<rows;i++)
//	   for (j=0;j<cols;j++)
//	      out[i] += weights[j*col_stride + i]*x[j];
func gemm_accum(out []float32, weights []opus_int8, rows, cols, col_stride int, x []float32) {
	var i, j int
	for i = 0; i < rows; i++ {
		for j = 0; j < cols; j++ {
			// out[i] += weights[j*col_stride+i] * x[j]
			out[i] = fma_add(out[i], float32(weights[j*col_stride+i]), x[j])
		}
	}
}

// analysis_compute_dense — literal port of C.
func analysis_compute_dense(layer *AnalysisDenseLayer, output []float32, input []float32) {
	var i int
	var N, M int
	var stride int
	M = layer.nb_inputs
	N = layer.nb_neurons
	stride = N
	for i = 0; i < N; i++ {
		output[i] = float32(layer.bias[i])
	}
	gemm_accum(output, layer.input_weights, N, M, stride, input)
	for i = 0; i < N; i++ {
		output[i] = mul_f32(output[i], WEIGHTS_SCALE)
	}
	if layer.sigmoid != 0 {
		for i = 0; i < N; i++ {
			output[i] = sigmoid_approx(output[i])
		}
	} else {
		for i = 0; i < N; i++ {
			output[i] = tansig_approx(output[i])
		}
	}
}

// analysis_compute_gru — literal port of C.
func analysis_compute_gru(gru *AnalysisGRULayer, state []float32, input []float32) {
	var i int
	var N, M int
	var stride int
	var tmp [MAX_NEURONS]float32
	var z [MAX_NEURONS]float32
	var r [MAX_NEURONS]float32
	var h [MAX_NEURONS]float32
	M = gru.nb_inputs
	N = gru.nb_neurons
	stride = 3 * N
	/* Compute update gate. */
	for i = 0; i < N; i++ {
		z[i] = float32(gru.bias[i])
	}
	gemm_accum(z[:], gru.input_weights, N, M, stride, input)
	gemm_accum(z[:], gru.recurrent_weights, N, N, stride, state)
	for i = 0; i < N; i++ {
		z[i] = sigmoid_approx(mul_f32(WEIGHTS_SCALE, z[i]))
	}

	/* Compute reset gate. */
	for i = 0; i < N; i++ {
		r[i] = float32(gru.bias[N+i])
	}
	gemm_accum(r[:], gru.input_weights[N:], N, M, stride, input)
	gemm_accum(r[:], gru.recurrent_weights[N:], N, N, stride, state)
	for i = 0; i < N; i++ {
		r[i] = sigmoid_approx(mul_f32(WEIGHTS_SCALE, r[i]))
	}

	/* Compute output. */
	for i = 0; i < N; i++ {
		h[i] = float32(gru.bias[2*N+i])
	}
	for i = 0; i < N; i++ {
		tmp[i] = mul_f32(state[i], r[i])
	}
	gemm_accum(h[:], gru.input_weights[2*N:], N, M, stride, input)
	gemm_accum(h[:], gru.recurrent_weights[2*N:], N, N, stride, tmp[:])
	for i = 0; i < N; i++ {
		// C: h[i] = z[i]*state[i] + (1-z[i])*tansig_approx(WEIGHTS_SCALE*h[i]);
		// Evaluated left-to-right as in C: first z[i]*state[i], then
		// add (1-z[i])*tansig_approx(...).
		ts := tansig_approx(mul_f32(WEIGHTS_SCALE, h[i]))
		// one_minus_z = 1 - z[i]
		one_minus_z := sub_f32(1.0, z[i])
		// z[i]*state[i] first
		acc := mul_f32(z[i], state[i])
		// acc + one_minus_z * ts
		h[i] = fma_add(acc, one_minus_z, ts)
	}
	for i = 0; i < N; i++ {
		state[i] = h[i]
	}
}
