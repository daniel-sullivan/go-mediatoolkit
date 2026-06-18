//go:build cgo

package benchcmp

/*
#include "config.h"
#include "SigProc_FIX.h"
#include "define.h"

// The QA-domain kernel is marked OPUS_INLINE and has file-scope
// visibility only in the libopus tree, so we inline a copy that
// matches the upstream silk/LPC_inv_pred_gain.c (lines ~34-118) 1:1.
// Used as the cgo ground-truth for the arm64 NEON port's inner
// loop. Do NOT diverge from the upstream sequence — parity of
// saturation/rounding with the C reference is how SILK keeps its
// bit-exact spec.

#include <limits.h>

#define _QA 24
#define _A_LIMIT SILK_FIX_CONST(0.99975, _QA)
#define _MUL32_FRAC_Q(a32, b32, Q) ((opus_int32)(silk_RSHIFT_ROUND64(silk_SMULL(a32, b32), Q)))

static opus_int32 c_silk_LPC_inverse_pred_gain_QA(opus_int32 *A_QA, int order) {
    int k, n, mult2Q;
    opus_int32 invGain_Q30, rc_Q31, rc_mult1_Q30, rc_mult2, tmp1, tmp2;

    invGain_Q30 = SILK_FIX_CONST(1, 30);
    for (k = order - 1; k > 0; k--) {
        if ((A_QA[k] > _A_LIMIT) || (A_QA[k] < -_A_LIMIT)) return 0;
        rc_Q31 = -silk_LSHIFT(A_QA[k], 31 - _QA);
        rc_mult1_Q30 = silk_SUB32(SILK_FIX_CONST(1, 30), silk_SMMUL(rc_Q31, rc_Q31));
        invGain_Q30 = silk_LSHIFT(silk_SMMUL(invGain_Q30, rc_mult1_Q30), 2);
        if (invGain_Q30 < SILK_FIX_CONST(1.0f / MAX_PREDICTION_POWER_GAIN, 30)) return 0;
        mult2Q = 32 - silk_CLZ32(silk_abs(rc_mult1_Q30));
        rc_mult2 = silk_INVERSE32_varQ(rc_mult1_Q30, mult2Q + 30);
        for (n = 0; n < (k + 1) >> 1; n++) {
            opus_int64 tmp64;
            tmp1 = A_QA[n];
            tmp2 = A_QA[k - n - 1];
            tmp64 = silk_RSHIFT_ROUND64(silk_SMULL(silk_SUB_SAT32(tmp1,
                _MUL32_FRAC_Q(tmp2, rc_Q31, 31)), rc_mult2), mult2Q);
            if (tmp64 > silk_int32_MAX || tmp64 < silk_int32_MIN) return 0;
            A_QA[n] = (opus_int32)tmp64;
            tmp64 = silk_RSHIFT_ROUND64(silk_SMULL(silk_SUB_SAT32(tmp2,
                _MUL32_FRAC_Q(tmp1, rc_Q31, 31)), rc_mult2), mult2Q);
            if (tmp64 > silk_int32_MAX || tmp64 < silk_int32_MIN) return 0;
            A_QA[k - n - 1] = (opus_int32)tmp64;
        }
    }
    if ((A_QA[k] > _A_LIMIT) || (A_QA[k] < -_A_LIMIT)) return 0;
    rc_Q31 = -silk_LSHIFT(A_QA[0], 31 - _QA);
    rc_mult1_Q30 = silk_SUB32(SILK_FIX_CONST(1, 30), silk_SMMUL(rc_Q31, rc_Q31));
    invGain_Q30 = silk_LSHIFT(silk_SMMUL(invGain_Q30, rc_mult1_Q30), 2);
    if (invGain_Q30 < SILK_FIX_CONST(1.0f / MAX_PREDICTION_POWER_GAIN, 30)) return 0;
    return invGain_Q30;
}
*/
import "C"
import "unsafe"

// cSilkLPCInversePredGainQA calls the file-scope upstream QA kernel.
// Mutates A_QA in place (matches scalar C semantics).
func cSilkLPCInversePredGainQA(A_QA []int32, order int) int32 {
	return int32(C.c_silk_LPC_inverse_pred_gain_QA(
		(*C.opus_int32)(unsafe.Pointer(&A_QA[0])), C.int(order)))
}
