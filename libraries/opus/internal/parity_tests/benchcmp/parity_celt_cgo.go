//go:build cgo

package benchcmp

/*
#include "config.h"
#include "celt.h"

static int c_resampling_factor(int rate) { return resampling_factor(rate); }

static void c_init_caps(const CELTMode *m, int *cap, int LM, int C) {
    init_caps(m, cap, LM, C);
}

// Wrapper to invoke comb_filter with explicit offsets — caller passes
// already-padded buffers.
static void c_comb_filter(float *y, int yOff, float *x, int xOff,
    int T0, int T1, int N, float g0, float g1, int tapset0, int tapset1,
    const float *window, int overlap) {
    comb_filter(y + yOff, x + xOff, T0, T1, N, g0, g1, tapset0, tapset1,
        window, overlap, 0);
}
*/
import "C"
import "unsafe"

func cResamplingFactor(rate int32) int { return int(C.c_resampling_factor(C.int(rate))) }

func cInitCaps(m cMode, cap_ []int, LM, C_ int) {
	cCap := make([]C.int, len(cap_))
	C.c_init_caps(m.p, (*C.int)(unsafe.Pointer(&cCap[0])), C.int(LM), C.int(C_))
	for i := range cap_ {
		cap_[i] = int(cCap[i])
	}
}

func cCombFilter(y []float32, yOff int, x []float32, xOff int,
	T0, T1, N int, g0, g1 float32, tapset0, tapset1 int,
	window []float32, overlap int) {
	var winPtr *C.float
	if len(window) > 0 {
		winPtr = (*C.float)(unsafe.Pointer(&window[0]))
	}
	C.c_comb_filter((*C.float)(unsafe.Pointer(&y[0])), C.int(yOff),
		(*C.float)(unsafe.Pointer(&x[0])), C.int(xOff),
		C.int(T0), C.int(T1), C.int(N),
		C.float(g0), C.float(g1), C.int(tapset0), C.int(tapset1),
		winPtr, C.int(overlap))
}
