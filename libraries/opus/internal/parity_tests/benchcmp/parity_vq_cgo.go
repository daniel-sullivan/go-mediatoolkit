//go:build cgo

package benchcmp

/*
#include "config.h"
#include "vq.h"
#include "cwrs.h"
#include "entenc.h"
#include "entdec.h"

static void c_exp_rotation(celt_norm *X, int len, int dir, int stride, int K, int spread) {
    exp_rotation(X, len, dir, stride, K, spread);
}
static opus_val16 c_op_pvq_search_c(celt_norm *X, int *iy, int K, int N) {
    return op_pvq_search_c(X, iy, K, N, 0);
}
static void c_renormalise_vector(celt_norm *X, int N, opus_val32 gain) {
    renormalise_vector(X, N, gain, 0);
}
static opus_int32 c_stereo_itheta(const celt_norm *X, const celt_norm *Y,
    int stereo, int N) {
    return stereo_itheta(X, Y, stereo, N, 0);
}
static unsigned c_alg_quant(celt_norm *X, int N, int K, int spread, int B,
    ec_enc *enc, opus_val32 gain, int resynth) {
    return alg_quant(X, N, K, spread, B, enc, gain, resynth, 0);
}
static unsigned c_alg_unquant(celt_norm *X, int N, int K, int spread, int B,
    ec_dec *dec, opus_val32 gain) {
    return alg_unquant(X, N, K, spread, B, dec, gain);
}
*/
import "C"
import "unsafe"

func cExpRotation(X []float32, length, dir, stride, K, spread int) {
	if len(X) == 0 {
		return
	}
	C.c_exp_rotation((*C.float)(unsafe.Pointer(&X[0])),
		C.int(length), C.int(dir), C.int(stride),
		C.int(K), C.int(spread))
}

func cOpPvqSearch(X []float32, iy []int32, K, N int) float32 {
	return float32(C.c_op_pvq_search_c(
		(*C.float)(unsafe.Pointer(&X[0])),
		(*C.int)(unsafe.Pointer(&iy[0])),
		C.int(K), C.int(N)))
}

func cRenormaliseVector(X []float32, N int, gain float32) {
	C.c_renormalise_vector((*C.float)(unsafe.Pointer(&X[0])),
		C.int(N), C.opus_val32(gain))
}

func cStereoItheta(X, Y []float32, stereo, N int) int32 {
	return int32(C.c_stereo_itheta(
		(*C.float)(unsafe.Pointer(&X[0])),
		(*C.float)(unsafe.Pointer(&Y[0])),
		C.int(stereo), C.int(N)))
}

func cAlgQuant(X []float32, N, K, spread, B int, enc cEc,
	gain float32, resynth int) uint {
	return uint(C.c_alg_quant((*C.float)(unsafe.Pointer(&X[0])),
		C.int(N), C.int(K), C.int(spread), C.int(B),
		enc.p, C.opus_val32(gain), C.int(resynth)))
}

func cAlgUnquant(X []float32, N, K, spread, B int, dec cEc, gain float32) uint {
	return uint(C.c_alg_unquant((*C.float)(unsafe.Pointer(&X[0])),
		C.int(N), C.int(K), C.int(spread), C.int(B),
		dec.p, C.opus_val32(gain)))
}
