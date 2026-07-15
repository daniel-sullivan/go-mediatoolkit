//go:build cgo && rnnoise_strict

// Package e2e bit-compares the Go port's full per-frame denoiser
// (rnnoise.State.ProcessFrame) against the vendored C
// rnnoise_process_frame, driven statefully over minutes of audio: the
// end-to-end gate for the RNNoise track.
package e2e

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo CFLAGS: -I${SRCDIR}/../rnncgo
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/src
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/include
#cgo CFLAGS: -DDISABLE_NEON
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable -Wno-unused-parameter -Wno-sign-compare
#cgo LDFLAGS: -lm

extern void *fparity_e2e_create(void);
extern void fparity_e2e_destroy(void *st);
extern float fparity_e2e_frame(void *st, float *out, const float *in);

#include "_oracle_src.c"
*/
import "C"

import (
	"runtime"
	"unsafe"
)

type cDenoiser struct{ p unsafe.Pointer }

func newCDenoiser() *cDenoiser {
	s := &cDenoiser{p: C.fparity_e2e_create()}
	runtime.SetFinalizer(s, func(s *cDenoiser) { C.fparity_e2e_destroy(s.p) })
	return s
}

func (s *cDenoiser) frame(in []float32) (out []float32, vad float32) {
	out = make([]float32, len(in))
	v := C.fparity_e2e_frame(s.p,
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&in[0])))
	return out, float32(v)
}
