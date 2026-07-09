//go:build cgo && aec_oracle

// Package fft is the bit-exact parity slice for aec/internal/aec3's
// fft.go (Ooura 128-point real FFT + the Aec3Fft wrapper) against the
// fetched C++ oracle.
//
// The oracle's include/library paths are host-dependent (fetched into
// a per-user cache, not vendored), so they can't be static #cgo
// directives — ../run.sh resolves them into CGO_CXXFLAGS/CGO_LDFLAGS
// for every slice under aec/internal/parity_tests/. See the smoke
// slice's cgo.go for the full rationale; the aec_oracle build tag
// gates this file out of any build that hasn't set that env up.
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package fft

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
*/
import "C"

// oouraFftC runs the oracle's 128-point forward FFT in place.
func oouraFftC(a *[128]float32) {
	C.aec3_ooura_fft((*C.float)(&a[0]))
}

// oouraIfftC runs the oracle's 128-point inverse FFT in place.
func oouraIfftC(a *[128]float32) {
	C.aec3_ooura_ifft((*C.float)(&a[0]))
}

// zeroPaddedFftC runs the oracle's Aec3Fft::ZeroPaddedFft.
func zeroPaddedFftC(x *[64]float32, window int) (re, im [65]float32) {
	C.aec3_fft_zero_padded((*C.float)(&x[0]), C.int(window),
		(*C.float)(&re[0]), (*C.float)(&im[0]))
	return re, im
}

// paddedFftC runs the oracle's Aec3Fft::PaddedFft.
func paddedFftC(x, xOld *[64]float32, window int) (re, im [65]float32) {
	C.aec3_fft_padded((*C.float)(&x[0]), (*C.float)(&xOld[0]), C.int(window),
		(*C.float)(&re[0]), (*C.float)(&im[0]))
	return re, im
}
