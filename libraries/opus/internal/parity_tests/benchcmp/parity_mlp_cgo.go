//go:build cgo

package benchcmp

/*
#include "config.h"

// Include the libopus MLP runtime directly. mlp.c has no dependency on
// the rest of the codec; it just needs the float helpers from arch.h
// which come in via mlp.h -> opus_types.h. mlp_data.c supplies the
// extern tables (layer0, layer1, layer2) referenced here.
//
// We rename the public symbols to avoid colliding with any production
// opus dylib that might also be linked, and pull in the .c sources so
// definitions are visible to the wrapper functions below.
#define analysis_compute_dense analysis_compute_dense_mlp_parity
#define analysis_compute_gru   analysis_compute_gru_mlp_parity
#define layer0                 layer0_mlp_parity
#define layer1                 layer1_mlp_parity
#define layer2                 layer2_mlp_parity

#include "mlp.h"
#include "mlp.c"
#include "mlp_data.c"

// Thin wrappers: the C entry points are ordinary externs, but we still
// wrap them to pin types (avoids cgo struggling with const-qualified
// pointer parameters).

static float c_mlp_tansig_approx(float x) {
    // Reproduce the static inline so we can reach it from outside mlp.c.
    // The body must match mlp.c verbatim.
    const float N0 = 952.52801514f;
    const float N1 = 96.39235687f;
    const float N2 = 0.60863042f;
    const float D0 = 952.72399902f;
    const float D1 = 413.36801147f;
    const float D2 = 11.88600922f;
    float X2, num, den;
    X2 = x*x;
    num = ((N2*X2)+N1)*X2 + N0;
    den = ((D2*X2)+D1)*X2 + D0;
    num = num*x/den;
    if (num > 1.f) num = 1.f;
    if (num < -1.f) num = -1.f;
    return num;
}

static float c_mlp_sigmoid_approx(float x) {
    return .5f + .5f*c_mlp_tansig_approx(.5f*x);
}

static void c_mlp_compute_dense_layer0(float *output, const float *input) {
    analysis_compute_dense(&layer0, output, input);
}

static void c_mlp_compute_dense_layer2(float *output, const float *input) {
    analysis_compute_dense(&layer2, output, input);
}

static void c_mlp_compute_gru_layer1(float *state, const float *input) {
    analysis_compute_gru(&layer1, state, input);
}
*/
import "C"
import "unsafe"

func cMLPTansigApprox(x float32) float32 {
	return float32(C.c_mlp_tansig_approx(C.float(x)))
}

func cMLPSigmoidApprox(x float32) float32 {
	return float32(C.c_mlp_sigmoid_approx(C.float(x)))
}

// cMLPComputeDenseLayer0 runs the C oracle for layer0 (25 in -> 32 out).
func cMLPComputeDenseLayer0(input []float32) []float32 {
	if len(input) != 25 {
		panic("layer0 expects 25 inputs")
	}
	out := make([]float32, 32)
	C.c_mlp_compute_dense_layer0(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&input[0])))
	return out
}

// cMLPComputeDenseLayer2 runs the C oracle for layer2 (24 in -> 2 out).
func cMLPComputeDenseLayer2(input []float32) []float32 {
	if len(input) != 24 {
		panic("layer2 expects 24 inputs")
	}
	out := make([]float32, 2)
	C.c_mlp_compute_dense_layer2(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&input[0])))
	return out
}

// cMLPComputeGRULayer1 runs one step of the C oracle for layer1.
// state must be length 24, input must be length 32.
func cMLPComputeGRULayer1(state, input []float32) {
	if len(state) != 24 {
		panic("layer1 state must be 24")
	}
	if len(input) != 32 {
		panic("layer1 input must be 32")
	}
	C.c_mlp_compute_gru_layer1(
		(*C.float)(unsafe.Pointer(&state[0])),
		(*C.float)(unsafe.Pointer(&input[0])))
}
