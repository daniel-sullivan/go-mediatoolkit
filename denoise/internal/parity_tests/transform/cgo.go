//go:build cgo && rnnoise_strict

// Package transform bit-compares the Go port of denoise.c's
// forward_transform / inverse_transform / apply_window (the 960-pt FFT
// analysis/synthesis) against the vendored C.
package transform

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo CFLAGS: -I${SRCDIR}/../rnncgo
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/src
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/include
#cgo CFLAGS: -DDISABLE_NEON
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable -Wno-unused-parameter -Wno-sign-compare
#cgo LDFLAGS: -lm

extern void fparity_forward_transform(float *outR, float *outI, const float *in);
extern void fparity_inverse_transform(float *out, const float *inR, const float *inI);
extern void fparity_apply_window(float *x);

#include "_oracle_src.c"
*/
import "C"

import "unsafe"

func cForwardTransform(in []float32, freqSize int) (r, i []float32) {
	r = make([]float32, freqSize)
	i = make([]float32, freqSize)
	C.fparity_forward_transform(
		(*C.float)(unsafe.Pointer(&r[0])),
		(*C.float)(unsafe.Pointer(&i[0])),
		(*C.float)(unsafe.Pointer(&in[0])),
	)
	return r, i
}

func cInverseTransform(inR, inI []float32, windowSize int) []float32 {
	out := make([]float32, windowSize)
	C.fparity_inverse_transform(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&inR[0])),
		(*C.float)(unsafe.Pointer(&inI[0])),
	)
	return out
}

func cApplyWindow(x []float32) []float32 {
	out := make([]float32, len(x))
	copy(out, x)
	C.fparity_apply_window((*C.float)(unsafe.Pointer(&out[0])))
	return out
}
