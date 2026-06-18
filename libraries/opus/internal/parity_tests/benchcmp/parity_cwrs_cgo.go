//go:build cgo

package benchcmp

/*
#include "config.h"
#include "cwrs.h"

static void c_encode_pulses(const int *y, int n, int k, ec_enc *e) {
    encode_pulses(y, n, k, e);
}
static float c_decode_pulses(int *y, int n, int k, ec_dec *d) {
    return decode_pulses(y, n, k, d);
}
*/
import "C"
import "unsafe"

func cEncodePulses(y []int32, n, k int, h cEc) {
	C.c_encode_pulses((*C.int)(unsafe.Pointer(&y[0])),
		C.int(n), C.int(k), h.p)
}
func cDecodePulses(y []int32, n, k int, h cEc) float32 {
	return float32(C.c_decode_pulses((*C.int)(unsafe.Pointer(&y[0])),
		C.int(n), C.int(k), h.p))
}
