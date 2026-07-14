//go:build cgo && rnnoise_strict

// Package features bit-compares the Go port of denoise.c's
// rnn_compute_frame_features (the full analysis: window/FFT, band
// energies, pitch chain, correlations, log10 compression, and the two
// DCTs) against the vendored C, driven statefully across many frames.
package features

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo CFLAGS: -I${SRCDIR}/../rnncgo
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/src
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/include
#cgo CFLAGS: -DDISABLE_NEON
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable -Wno-unused-parameter -Wno-sign-compare
#cgo LDFLAGS: -lm

extern void *fparity_create_state(void);
extern void fparity_destroy_state(void *st);
extern int fparity_compute_features(void *st, float *features, float *Ex, float *Ep, float *Exp, const float *in);

#include "_oracle_src.c"
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// cState wraps a C DenoiseState for the streaming comparison.
type cState struct{ p unsafe.Pointer }

func newCState() *cState {
	s := &cState{p: C.fparity_create_state()}
	runtime.SetFinalizer(s, func(s *cState) { C.fparity_destroy_state(s.p) })
	return s
}

func (s *cState) computeFeatures(in []float32) (features, ex, ep, exp []float32, silence bool) {
	features = make([]float32, 65)
	ex = make([]float32, 32)
	ep = make([]float32, 32)
	exp = make([]float32, 32)
	sil := C.fparity_compute_features(s.p,
		(*C.float)(unsafe.Pointer(&features[0])),
		(*C.float)(unsafe.Pointer(&ex[0])),
		(*C.float)(unsafe.Pointer(&ep[0])),
		(*C.float)(unsafe.Pointer(&exp[0])),
		(*C.float)(unsafe.Pointer(&in[0])))
	return features, ex, ep, exp, sil != 0
}
