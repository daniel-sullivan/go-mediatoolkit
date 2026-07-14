//go:build cgo && rnnoise_strict

// Package pitch bit-compares the Go port of the RNNoise pitch chain
// (rnn_pitch_downsample -> rnn_pitch_search -> rnn_remove_doubling)
// against the vendored C. This is the highest-risk slice: remove_doubling
// selects the pitch lag via float comparisons, so a 1-ulp divergence can
// change the chosen integer lag.
package pitch

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo CFLAGS: -I${SRCDIR}/../rnncgo
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/src
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/include
#cgo CFLAGS: -DDISABLE_NEON
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable -Wno-unused-parameter -Wno-sign-compare
#cgo LDFLAGS: -lm

extern void fparity_pitch_downsample(float *xlp_out, const float *pitchbuf, int len);
extern int fparity_pitch_search(const float *xlp, float *y, int len, int maxpitch);
extern float fparity_remove_doubling(float *x, int maxperiod, int minperiod, int N, int *T0, int prev_period, float prev_gain);

#include "_oracle_src.c"
*/
import "C"

import "unsafe"

func cPitchDownsample(pitchbuf []float32, length int) []float32 {
	out := make([]float32, length>>1)
	C.fparity_pitch_downsample((*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&pitchbuf[0])), C.int(length))
	return out
}

// cPitchSearch runs the C search over a base buffer, with xLp starting at
// xlpOff samples into it (as the pipeline does).
func cPitchSearch(buf []float32, xlpOff, length, maxPitch int) int {
	return int(C.fparity_pitch_search(
		(*C.float)(unsafe.Pointer(&buf[xlpOff])),
		(*C.float)(unsafe.Pointer(&buf[0])),
		C.int(length), C.int(maxPitch)))
}

func cRemoveDoubling(x []float32, maxperiod, minperiod, n, t0, prevPeriod int, prevGain float32) (float32, int) {
	ct0 := C.int(t0)
	g := C.fparity_remove_doubling((*C.float)(unsafe.Pointer(&x[0])),
		C.int(maxperiod), C.int(minperiod), C.int(n), &ct0,
		C.int(prevPeriod), C.float(prevGain))
	return float32(g), int(ct0)
}
