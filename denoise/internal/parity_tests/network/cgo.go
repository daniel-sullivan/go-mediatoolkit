//go:build cgo && rnnoise_strict

// Package network bit-compares the Go port of rnn.c compute_rnn (the
// conv1/conv2 + 3 GRU + dense_out/vad_dense forward pass, with the real
// float weights) against the vendored C, driven recurrently across
// frames. The oracle compiles rnnoise_data.c (RNNCGO_WITH_WEIGHTS).
package network

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo CFLAGS: -I${SRCDIR}/../rnncgo
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/src
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/include
#cgo CFLAGS: -DDISABLE_NEON
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable -Wno-unused-parameter -Wno-sign-compare
#cgo LDFLAGS: -lm

extern void *fparity_rnn_create(void);
extern void fparity_rnn_destroy(void *st);
extern void fparity_rnn_step(void *st, float *gains, float *vad, const float *input);

#include "_oracle_src.c"
*/
import "C"

import (
	"runtime"
	"unsafe"
)

type cRnn struct{ p unsafe.Pointer }

func newCRnn() *cRnn {
	s := &cRnn{p: C.fparity_rnn_create()}
	runtime.SetFinalizer(s, func(s *cRnn) { C.fparity_rnn_destroy(s.p) })
	return s
}

func (s *cRnn) step(input []float32) (gains []float32, vad float32) {
	gains = make([]float32, 32)
	v := make([]float32, 1)
	C.fparity_rnn_step(s.p,
		(*C.float)(unsafe.Pointer(&gains[0])),
		(*C.float)(unsafe.Pointer(&v[0])),
		(*C.float)(unsafe.Pointer(&input[0])))
	return gains, v[0]
}
