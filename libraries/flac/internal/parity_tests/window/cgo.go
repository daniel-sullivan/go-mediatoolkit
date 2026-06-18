//go:build cgo

// Package window pins the Go ports of the FLAC__window_* apodization
// generators against libFLAC's window.c, compiled into this test
// binary. For each window type and several block sizes the C window is
// computed and compared bit-for-bit (float32 / IEEE-754 bit pattern)
// against the nativeflac port built under -tags flac_strict.
package window

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DFLAC__NO_DLL
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../libflac
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/include
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/src/libFLAC/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable -Wno-static-in-inline

#include <stdint.h>
#include <stdlib.h>

// fparity_window fills out[0..L-1] with the window of the given type.
// p is the tukey p / gauss stddev; start/end are the partial/punchout
// tukey bounds. typ matches the Go WindowType enum ordering.
extern void fparity_window(int typ, float *out, int32_t L, float p, float start, float end);
*/
import "C"

import (
	"unsafe"
)

// CWindow computes the libFLAC window of the given type into a fresh
// float32 slice of length L.
func CWindow(typ int, L int32, p, start, end float32) []float32 {
	out := make([]float32, L)
	var op *C.float
	if L > 0 {
		op = (*C.float)(unsafe.Pointer(&out[0]))
	}
	C.fparity_window(C.int(typ), op, C.int32_t(L), C.float(p), C.float(start), C.float(end))
	return out
}
