//go:build cgo

package benchcmp

/*
#include "config.h"
#include "pitch.h"
#include "celt_lpc.h"

static void c_xcorr_kernel(const float *x, const float *y, float *sum, int len) {
    xcorr_kernel_c(x, y, sum, len);
}
static float c_celt_inner_prod(const float *x, const float *y, int N) {
    return celt_inner_prod_c(x, y, N);
}
static void c_dual_inner_prod(const float *x, const float *y01, const float *y02,
    int N, float *xy1, float *xy2) {
    dual_inner_prod_c(x, y01, y02, N, xy1, xy2);
}
static void c_celt_pitch_xcorr(const float *x, const float *y, float *xcorr,
    int len, int max_pitch) {
    celt_pitch_xcorr_c(x, y, xcorr, len, max_pitch, 0);
}
static void c_celt_lpc(float *lpc, const float *ac, int p) {
    _celt_lpc(lpc, ac, p);
}
static void c_celt_fir(const float *x, const float *num, float *y,
    int N, int ord) {
    celt_fir_c(x, num, y, N, ord, 0);
}
static void c_celt_iir(const float *x, const float *den, float *y,
    int N, int ord, float *mem) {
    celt_iir(x, den, y, N, ord, mem, 0);
}
static int c_celt_autocorr(const float *x, float *ac, const float *window,
    int overlap, int lag, int n) {
    return _celt_autocorr(x, ac, window, overlap, lag, n, 0);
}
static void c_pitch_downsample(float **x, float *xlp, int len, int C, int factor) {
    pitch_downsample((celt_sig **)x, xlp, len, C, factor, 0);
}
static int c_pitch_search(const float *xlp, float *y, int len, int max_pitch) {
    int p;
    pitch_search(xlp, y, len, max_pitch, &p, 0);
    return p;
}
static float c_remove_doubling(float *x, int maxperiod, int minperiod, int N,
    int *T0, int prev_period, float prev_gain) {
    return remove_doubling(x, maxperiod, minperiod, N, T0,
                           prev_period, prev_gain, 0);
}
*/
import "C"
import (
	"runtime"
	"unsafe"
)

func cXcorrKernel(x, y []float32, sum []float32, ln int) {
	C.c_xcorr_kernel((*C.float)(unsafe.Pointer(&x[0])),
		(*C.float)(unsafe.Pointer(&y[0])),
		(*C.float)(unsafe.Pointer(&sum[0])), C.int(ln))
}
func cCeltInnerProd(x, y []float32, N int) float32 {
	return float32(C.c_celt_inner_prod((*C.float)(unsafe.Pointer(&x[0])),
		(*C.float)(unsafe.Pointer(&y[0])), C.int(N)))
}
func cDualInnerProd(x, y01, y02 []float32, N int) (float32, float32) {
	var xy1, xy2 C.float
	C.c_dual_inner_prod((*C.float)(unsafe.Pointer(&x[0])),
		(*C.float)(unsafe.Pointer(&y01[0])),
		(*C.float)(unsafe.Pointer(&y02[0])),
		C.int(N), &xy1, &xy2)
	return float32(xy1), float32(xy2)
}
func cCeltPitchXcorr(x, y []float32, xcorr []float32, ln, max int) {
	C.c_celt_pitch_xcorr((*C.float)(unsafe.Pointer(&x[0])),
		(*C.float)(unsafe.Pointer(&y[0])),
		(*C.float)(unsafe.Pointer(&xcorr[0])),
		C.int(ln), C.int(max))
}
func cCeltLPC(lpc, ac []float32, p int) {
	C.c_celt_lpc((*C.float)(unsafe.Pointer(&lpc[0])),
		(*C.float)(unsafe.Pointer(&ac[0])), C.int(p))
}
func cCeltFir(x, num, y []float32, N, ord int) {
	C.c_celt_fir((*C.float)(unsafe.Pointer(&x[0])),
		(*C.float)(unsafe.Pointer(&num[0])),
		(*C.float)(unsafe.Pointer(&y[0])),
		C.int(N), C.int(ord))
}
func cCeltIir(x, den, y []float32, N, ord int, mem []float32) {
	C.c_celt_iir((*C.float)(unsafe.Pointer(&x[0])),
		(*C.float)(unsafe.Pointer(&den[0])),
		(*C.float)(unsafe.Pointer(&y[0])),
		C.int(N), C.int(ord),
		(*C.float)(unsafe.Pointer(&mem[0])))
}
func cCeltAutocorr(x, ac, window []float32, overlap, lag, n int) {
	var wptr *C.float
	if window != nil && len(window) > 0 {
		wptr = (*C.float)(unsafe.Pointer(&window[0]))
	}
	C.c_celt_autocorr((*C.float)(unsafe.Pointer(&x[0])),
		(*C.float)(unsafe.Pointer(&ac[0])),
		wptr, C.int(overlap), C.int(lag), C.int(n))
}
func cPitchDownsample(x [][]float32, xlp []float32, ln, C_, factor int) {
	// cgo forbids passing a Go slice containing Go pointers without
	// pinning; runtime.Pinner (Go 1.21+) holds the channel pointers
	// fixed for the duration of the C call.
	var pinner runtime.Pinner
	defer pinner.Unpin()
	ptrs := make([]*C.float, len(x))
	for i := range x {
		pinner.Pin(&x[i][0])
		ptrs[i] = (*C.float)(unsafe.Pointer(&x[i][0]))
	}
	pinner.Pin(&ptrs[0])
	C.c_pitch_downsample((**C.float)(unsafe.Pointer(&ptrs[0])),
		(*C.float)(unsafe.Pointer(&xlp[0])),
		C.int(ln), C.int(C_), C.int(factor))
}
func cPitchSearch(xlp, y []float32, ln, maxp int) int {
	return int(C.c_pitch_search((*C.float)(unsafe.Pointer(&xlp[0])),
		(*C.float)(unsafe.Pointer(&y[0])),
		C.int(ln), C.int(maxp)))
}
func cRemoveDoubling(x []float32, maxperiod, minperiod, N int,
	T0 *int, prevPeriod int, prevGain float32) float32 {
	ct0 := C.int(*T0)
	v := C.c_remove_doubling((*C.float)(unsafe.Pointer(&x[0])),
		C.int(maxperiod), C.int(minperiod), C.int(N),
		&ct0, C.int(prevPeriod), C.float(prevGain))
	*T0 = int(ct0)
	return float32(v)
}
