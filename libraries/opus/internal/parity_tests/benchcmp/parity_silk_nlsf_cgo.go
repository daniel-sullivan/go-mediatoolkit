//go:build cgo

package benchcmp

/*
#include "config.h"
#include "SigProc_FIX.h"
#include "structs.h"
#include "tables.h"
#include "main.h"

// NLSF_VQ_weights_laroia is declared in SigProc_FIX.h.
static void c_nlsf_vq_weights(opus_int16 *out, const opus_int16 *in, int D) {
    silk_NLSF_VQ_weights_laroia(out, in, D);
}
static void c_nlsf_stabilize(opus_int16 *NLSF, const opus_int16 *NDeltaMin, int L) {
    silk_NLSF_stabilize(NLSF, NDeltaMin, L);
}

// pick_cb returns pointer to the NB_MB or WB codebook struct.
static const silk_NLSF_CB_struct *pick_cb(int wb) {
    if (wb) return &silk_NLSF_CB_WB;
    return &silk_NLSF_CB_NB_MB;
}

static int cb_order(int wb) { return pick_cb(wb)->order; }

static void c_nlsf_decode(opus_int16 *out, const opus_int8 *idx, int wb) {
    silk_NLSF_decode(out, (opus_int8*)idx, pick_cb(wb));
}

static opus_int32 c_nlsf_encode(opus_int8 *idx, opus_int16 *nlsf, int wb,
                                int mu, int nSurvivors, int signalType) {
    const silk_NLSF_CB_struct *cb = pick_cb(wb);
    opus_int16 pW_Q2[16];
    silk_NLSF_VQ_weights_laroia(pW_Q2, nlsf, cb->order);
    return silk_NLSF_encode(idx, nlsf, cb, pW_Q2, mu, nSurvivors, signalType);
}

static void c_A2NLSF(opus_int16 *NLSF, opus_int32 *a_Q16, int d) {
    silk_A2NLSF(NLSF, a_Q16, d);
}
static void c_NLSF2A(opus_int16 *a_Q12, const opus_int16 *NLSF, int d) {
    silk_NLSF2A(a_Q12, NLSF, d, 0);
}
*/
import "C"
import "unsafe"

func cSilkNLSFVQWeightsLaroia(pNLSF []int16) []int16 {
	w := make([]int16, len(pNLSF))
	C.c_nlsf_vq_weights(
		(*C.opus_int16)(unsafe.Pointer(&w[0])),
		(*C.opus_int16)(unsafe.Pointer(&pNLSF[0])),
		C.int(len(pNLSF)))
	return w
}
func cSilkNLSFStabilize(NLSF, NDeltaMin []int16) []int16 {
	out := append([]int16(nil), NLSF...)
	C.c_nlsf_stabilize(
		(*C.opus_int16)(unsafe.Pointer(&out[0])),
		(*C.opus_int16)(unsafe.Pointer(&NDeltaMin[0])),
		C.int(len(out)))
	return out
}

func cCBOrder(wb bool) int {
	w := 0
	if wb {
		w = 1
	}
	return int(C.cb_order(C.int(w)))
}

func cSilkNLSFDecode(idx []int8, wb bool) []int16 {
	w := 0
	if wb {
		w = 1
	}
	out := make([]int16, cCBOrder(wb))
	C.c_nlsf_decode(
		(*C.opus_int16)(unsafe.Pointer(&out[0])),
		(*C.opus_int8)(unsafe.Pointer(&idx[0])),
		C.int(w))
	return out
}

func cSilkNLSFEncode(pNLSF []int16, wb bool, mu, nSurvivors, signalType int) (int32, []int8, []int16) {
	w := 0
	if wb {
		w = 1
	}
	order := cCBOrder(wb)
	idx := make([]int8, order+1)
	nlsfCopy := append([]int16(nil), pNLSF...)
	rd := int32(C.c_nlsf_encode(
		(*C.opus_int8)(unsafe.Pointer(&idx[0])),
		(*C.opus_int16)(unsafe.Pointer(&nlsfCopy[0])),
		C.int(w), C.int(mu), C.int(nSurvivors), C.int(signalType)))
	return rd, idx, nlsfCopy
}

func cSilkA2NLSF(a_Q16 []int32, d int) ([]int16, []int32) {
	NLSF := make([]int16, d)
	aCopy := append([]int32(nil), a_Q16...)
	C.c_A2NLSF(
		(*C.opus_int16)(unsafe.Pointer(&NLSF[0])),
		(*C.opus_int32)(unsafe.Pointer(&aCopy[0])),
		C.int(d))
	return NLSF, aCopy
}

func cSilkNLSF2A(NLSF []int16, d int) []int16 {
	out := make([]int16, d)
	C.c_NLSF2A(
		(*C.opus_int16)(unsafe.Pointer(&out[0])),
		(*C.opus_int16)(unsafe.Pointer(&NLSF[0])),
		C.int(d))
	return out
}
