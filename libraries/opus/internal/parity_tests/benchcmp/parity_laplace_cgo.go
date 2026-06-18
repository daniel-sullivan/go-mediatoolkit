//go:build cgo

package benchcmp

/*
#include "config.h"
#include "laplace.h"

static void c_ec_laplace_encode(ec_enc *e, int *v, unsigned fs, int decay) {
    ec_laplace_encode(e, v, fs, decay);
}
static int c_ec_laplace_decode(ec_dec *d, unsigned fs, int decay) {
    return ec_laplace_decode(d, fs, decay);
}
static void c_ec_laplace_encode_p0(ec_enc *e, int v, opus_uint16 p0, opus_uint16 decay) {
    ec_laplace_encode_p0(e, v, p0, decay);
}
static int c_ec_laplace_decode_p0(ec_dec *d, opus_uint16 p0, opus_uint16 decay) {
    return ec_laplace_decode_p0(d, p0, decay);
}

// ec_laplace_get_freq1 is a file-static helper — reproduce its body
// here so the test can compare it directly.
#define LAPLACE_LOG_MINP_LOCAL (0)
#define LAPLACE_MINP_LOCAL (1<<LAPLACE_LOG_MINP_LOCAL)
#define LAPLACE_NMIN_LOCAL (16)
static unsigned c_ec_laplace_get_freq1(unsigned fs0, int decay) {
    unsigned ft = 32768 - LAPLACE_MINP_LOCAL*(2*LAPLACE_NMIN_LOCAL) - fs0;
    return ft*(opus_int32)(16384-decay)>>15;
}
*/
import "C"
import "unsafe"

func cEcLaplaceEncode(h cEc, v *int, fs uint32, decay int) {
	cv := C.int(*v)
	C.c_ec_laplace_encode(h.p, (*C.int)(unsafe.Pointer(&cv)),
		C.uint(fs), C.int(decay))
	*v = int(cv)
}
func cEcLaplaceDecode(h cEc, fs uint32, decay int) int {
	return int(C.c_ec_laplace_decode(h.p, C.uint(fs), C.int(decay)))
}
func cEcLaplaceEncodeP0(h cEc, v int, p0, decay uint16) {
	C.c_ec_laplace_encode_p0(h.p, C.int(v), C.opus_uint16(p0), C.opus_uint16(decay))
}
func cEcLaplaceDecodeP0(h cEc, p0, decay uint16) int {
	return int(C.c_ec_laplace_decode_p0(h.p, C.opus_uint16(p0), C.opus_uint16(decay)))
}
func cEcLaplaceGetFreq1(fs0 uint32, decay int) uint32 {
	return uint32(C.c_ec_laplace_get_freq1(C.uint(fs0), C.int(decay)))
}
