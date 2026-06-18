//go:build cgo

package benchcmp

/*
#include "config.h"
#include "SigProc_FIX.h"

static void c_silk_bwexpander(opus_int16 *ar, int d, opus_int32 chirp_Q16) {
    silk_bwexpander(ar, d, chirp_Q16);
}
static void c_silk_bwexpander_32(opus_int32 *ar, int d, opus_int32 chirp_Q16) {
    silk_bwexpander_32(ar, d, chirp_Q16);
}
static void c_silk_LPC_fit(opus_int16 *a_QOUT, opus_int32 *a_QIN,
                           int QOUT, int QIN, int d) {
    silk_LPC_fit(a_QOUT, a_QIN, QOUT, QIN, d);
}

// silk_interpolate lives in main.h which pulls in everything; inline a copy.
static void c_silk_interpolate(opus_int16 *xi, const opus_int16 *x0, const opus_int16 *x1,
                               int ifact_Q2, int d) {
    int i;
    for (i = 0; i < d; i++) {
        xi[i] = (opus_int16)silk_ADD_RSHIFT(x0[i], silk_SMULBB(x1[i] - x0[i], ifact_Q2), 2);
    }
}

static opus_int32 c_silk_LPC_inverse_pred_gain(const opus_int16 *A_Q12, int order) {
    return silk_LPC_inverse_pred_gain_c(A_Q12, order);
}

static void c_silk_LPC_analysis_filter(opus_int16 *out, const opus_int16 *in,
                                       const opus_int16 *B, int len, int d) {
    silk_LPC_analysis_filter(out, in, B, len, d, 0);
}
*/
import "C"
import "unsafe"

func cSilkBWExpander(ar []int16, chirp_Q16 int32) []int16 {
	ac := append([]int16(nil), ar...)
	C.c_silk_bwexpander((*C.opus_int16)(unsafe.Pointer(&ac[0])),
		C.int(len(ac)), C.opus_int32(chirp_Q16))
	return ac
}
func cSilkBWExpander32(ar []int32, chirp_Q16 int32) []int32 {
	ac := append([]int32(nil), ar...)
	C.c_silk_bwexpander_32((*C.opus_int32)(unsafe.Pointer(&ac[0])),
		C.int(len(ac)), C.opus_int32(chirp_Q16))
	return ac
}
func cSilkLPCFit(aIn []int32, QOUT, QIN int) ([]int16, []int32) {
	aCopy := append([]int32(nil), aIn...)
	aOut := make([]int16, len(aCopy))
	C.c_silk_LPC_fit((*C.opus_int16)(unsafe.Pointer(&aOut[0])),
		(*C.opus_int32)(unsafe.Pointer(&aCopy[0])),
		C.int(QOUT), C.int(QIN), C.int(len(aCopy)))
	return aOut, aCopy
}
func cSilkInterpolate(x0, x1 []int16, ifact_Q2 int) []int16 {
	xi := make([]int16, len(x0))
	C.c_silk_interpolate((*C.opus_int16)(unsafe.Pointer(&xi[0])),
		(*C.opus_int16)(unsafe.Pointer(&x0[0])),
		(*C.opus_int16)(unsafe.Pointer(&x1[0])),
		C.int(ifact_Q2), C.int(len(x0)))
	return xi
}
func cSilkLPCInversePredGain(A_Q12 []int16) int32 {
	return int32(C.c_silk_LPC_inverse_pred_gain(
		(*C.opus_int16)(unsafe.Pointer(&A_Q12[0])), C.int(len(A_Q12))))
}
func cSilkLPCAnalysisFilter(in_, B []int16, d int) []int16 {
	out := make([]int16, len(in_))
	C.c_silk_LPC_analysis_filter((*C.opus_int16)(unsafe.Pointer(&out[0])),
		(*C.opus_int16)(unsafe.Pointer(&in_[0])),
		(*C.opus_int16)(unsafe.Pointer(&B[0])),
		C.int(len(in_)), C.int(d))
	return out
}
