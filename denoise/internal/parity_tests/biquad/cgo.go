//go:build cgo && rnnoise_strict

// Package biquad bit-compares the Go port of rnn_biquad (the RNNoise
// input high-pass, denoise/internal/rnnoise/biquad.go) against the
// vendored C rnn_biquad. The subtlety under test is the mixed-precision
// state update: float32 state, float64 multiply-accumulate.
package biquad

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo CFLAGS: -I${SRCDIR}/../rnncgo
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/src
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/include
#cgo CFLAGS: -DDISABLE_NEON
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable -Wno-unused-parameter -Wno-sign-compare
#cgo LDFLAGS: -lm

extern void fparity_biquad(float *y, float *mem, const float *x,
                           const float *b, const float *a, int n);

#include "_oracle_src.c"
*/
import "C"

import "unsafe"

// cBiquad runs the vendored rnn_biquad over x with coefficients b/a and
// state mem (2 words, updated in place), returning y.
func cBiquad(x []float32, b, a [2]float32, mem *[2]float32) []float32 {
	y := make([]float32, len(x))
	if len(x) == 0 {
		return y
	}
	C.fparity_biquad(
		(*C.float)(unsafe.Pointer(&y[0])),
		(*C.float)(unsafe.Pointer(&mem[0])),
		(*C.float)(unsafe.Pointer(&x[0])),
		(*C.float)(unsafe.Pointer(&b[0])),
		(*C.float)(unsafe.Pointer(&a[0])),
		C.int(len(x)),
	)
	return y
}
