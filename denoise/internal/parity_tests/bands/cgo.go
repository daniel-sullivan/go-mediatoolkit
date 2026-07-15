//go:build cgo && rnnoise_strict

// Package bands bit-compares the Go port of denoise.c's
// compute_band_energy / compute_band_corr / interp_band_gain / dct (and
// the eband20ms table) against the vendored C.
package bands

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo CFLAGS: -I${SRCDIR}/../rnncgo
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/src
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/include
#cgo CFLAGS: -DDISABLE_NEON
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable -Wno-unused-parameter -Wno-sign-compare
#cgo LDFLAGS: -lm

extern void fparity_band_energy(float *bandE, const float *xr, const float *xi);
extern void fparity_band_corr(float *bandE, const float *xr, const float *xi, const float *pr, const float *pi);
extern void fparity_interp_band_gain(float *g, const float *bandE);
extern void fparity_dct(float *out, const float *in);
extern int fparity_eband(int i);

#include "_oracle_src.c"
*/
import "C"

import "unsafe"

func cBandEnergy(xr, xi []float32, nb int) []float32 {
	out := make([]float32, nb)
	C.fparity_band_energy((*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&xr[0])), (*C.float)(unsafe.Pointer(&xi[0])))
	return out
}

func cBandCorr(xr, xi, pr, pi []float32, nb int) []float32 {
	out := make([]float32, nb)
	C.fparity_band_corr((*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&xr[0])), (*C.float)(unsafe.Pointer(&xi[0])),
		(*C.float)(unsafe.Pointer(&pr[0])), (*C.float)(unsafe.Pointer(&pi[0])))
	return out
}

func cInterpBandGain(bandE []float32, freq int) []float32 {
	g := make([]float32, freq)
	C.fparity_interp_band_gain((*C.float)(unsafe.Pointer(&g[0])),
		(*C.float)(unsafe.Pointer(&bandE[0])))
	return g
}

func cDct(in []float32, nb int) []float32 {
	out := make([]float32, nb)
	C.fparity_dct((*C.float)(unsafe.Pointer(&out[0])), (*C.float)(unsafe.Pointer(&in[0])))
	return out
}

func cEband(i int) int { return int(C.fparity_eband(C.int(i))) }
