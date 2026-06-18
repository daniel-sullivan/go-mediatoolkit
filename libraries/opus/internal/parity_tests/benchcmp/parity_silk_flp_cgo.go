//go:build cgo

package benchcmp

/*
#include "config.h"
#include "SigProc_FLP.h"
#include "main_FLP.h"
#include "tuning_parameters.h"
#include "structs.h"
#include "tables.h"
#include <string.h>
#include <stdio.h>

// ---- Leaf float kernels ----
static void c_silk_scale_copy_vector(float *out, const float *in, float gain, int n) {
    silk_scale_copy_vector_FLP(out, in, gain, n);
}
static void c_silk_scale_vector(float *data, float gain, int n) {
    silk_scale_vector_FLP(data, gain, n);
}
static void c_silk_insertion_sort_decreasing(float *a, opus_int *idx, int L, int K) {
    silk_insertion_sort_decreasing_FLP(a, idx, L, K);
}
static void c_silk_bwexpander_flp(float *ar, int d, float chirp) {
    silk_bwexpander_FLP(ar, d, chirp);
}

static double c_silk_inner_product(const float *a, const float *b, int n) {
    return silk_inner_product_FLP(a, b, n, 0);
}
static double c_silk_energy(const float *a, int n) {
    return silk_energy_FLP(a, n);
}
static void c_silk_apply_sine_window(float *out, const float *in, int win_type, int length) {
    silk_apply_sine_window_FLP(out, in, win_type, length);
}

static void c_silk_k2a(float *A, const float *rc, int order) {
    silk_k2a_FLP(A, rc, order);
}
static float c_silk_schur(float *refl, const float *ac, int order) {
    return silk_schur_FLP(refl, ac, order);
}

// LTP_scale_ctrl — needs a full encoder state; stub one.
static void c_silk_LTP_scale_ctrl(int condCoding,
    int packetLoss, int nFramesPerPacket, int SNR_dB_Q7, int LBRR_flag,
    float LTPredCodGain, int *out_idx, float *out_scale) {
    silk_encoder_state_FLP psEnc;
    silk_encoder_control_FLP psEncCtrl;
    memset(&psEnc, 0, sizeof(psEnc));
    memset(&psEncCtrl, 0, sizeof(psEncCtrl));
    psEnc.sCmn.PacketLoss_perc = packetLoss;
    psEnc.sCmn.nFramesPerPacket = nFramesPerPacket;
    psEnc.sCmn.SNR_dB_Q7 = SNR_dB_Q7;
    psEnc.sCmn.LBRR_flag = (opus_int8)LBRR_flag;
    psEncCtrl.LTPredCodGain = LTPredCodGain;
    silk_LTP_scale_ctrl_FLP(&psEnc, &psEncCtrl, condCoding);
    *out_idx = psEnc.sCmn.indices.LTP_scaleIndex;
    *out_scale = psEncCtrl.LTP_scale;
}

static void c_silk_autocorrelation(float *results, const float *input, int inputDataSize,
                                   int correlationCount) {
    silk_autocorrelation_FLP(results, input, inputDataSize, correlationCount, 0);
}
static void c_silk_warped_autocorrelation(float *corr, const float *input, float warping,
                                          int length, int order) {
    silk_warped_autocorrelation_FLP(corr, input, warping, length, order);
}

static float c_silk_LPC_inverse_pred_gain_flp(const float *A, int order) {
    return silk_LPC_inverse_pred_gain_FLP(A, order);
}
static void c_silk_LPC_analysis_filter_flp(float *r_LPC, const float *PredCoef,
                                       const float *s, int length, int Order) {
    silk_LPC_analysis_filter_FLP(r_LPC, PredCoef, s, length, Order);
}
static void c_silk_LTP_analysis_filter(float *LTP_res, const float *xBuf, int xOff,
    const float *B, const opus_int *pitchL, const float *invGains,
    int subfr_length, int nb_subfr, int pre_length) {
    silk_LTP_analysis_filter_FLP(LTP_res, xBuf + xOff, B, pitchL, invGains,
        subfr_length, nb_subfr, pre_length);
}

static void c_silk_corrVector(const float *x, const float *t, int L, int Order,
                              float *Xt) {
    silk_corrVector_FLP(x, t, L, Order, Xt, 0);
}
static void c_silk_corrMatrix(const float *x, int L, int Order, float *XX) {
    silk_corrMatrix_FLP(x, L, Order, XX, 0);
}
extern void silk_regularize_correlations_FLP(float *XX, float *xx, float noise, int D);
static void c_silk_regularize_correlations(float *XX, float *xx, float noise, int D) {
    silk_regularize_correlations_FLP(XX, xx, noise, D);
}
static float c_silk_residual_energy_covar(const float *c, float *wXX, const float *wXx,
                                          float wxx, int D) {
    return silk_residual_energy_covar_FLP(c, wXX, wXx, wxx, D);
}
static void c_silk_residual_energy(float *nrgs, const float *x,
    const float *a0, const float *a1, const float *gains,
    int subfr_length, int nb_subfr, int LPC_order) {
    float a[2][MAX_LPC_ORDER];
    memset(a, 0, sizeof(a));
    for (int i = 0; i < MAX_LPC_ORDER; i++) {
        a[0][i] = a0[i];
        a[1][i] = a1[i];
    }
    silk_residual_energy_FLP(nrgs, x, a, gains, subfr_length, nb_subfr, LPC_order);
}

static float c_silk_burg_modified(float *A, const float *x, float minInvGain,
    int subfr_length, int nb_subfr, int D) {
    return silk_burg_modified_FLP(A, x, minInvGain, subfr_length, nb_subfr, D, 0);
}
*/
import "C"
import "unsafe"

// ---- Go-side wrappers ----

func cSilkScaleCopyVectorFLP(in []float32, gain float32) []float32 {
	out := make([]float32, len(in))
	if len(in) == 0 {
		return out
	}
	C.c_silk_scale_copy_vector(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&in[0])),
		C.float(gain), C.int(len(in)))
	return out
}

func cSilkScaleVectorFLP(in []float32, gain float32) []float32 {
	out := append([]float32(nil), in...)
	if len(out) == 0 {
		return out
	}
	C.c_silk_scale_vector((*C.float)(unsafe.Pointer(&out[0])), C.float(gain), C.int(len(out)))
	return out
}

func cSilkInsertionSortDecreasingFLP(a []float32, K int) ([]float32, []int) {
	out := append([]float32(nil), a...)
	idx := make([]C.opus_int, K)
	if len(out) == 0 {
		return out, nil
	}
	C.c_silk_insertion_sort_decreasing(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.opus_int)(unsafe.Pointer(&idx[0])),
		C.int(len(out)), C.int(K))
	ri := make([]int, K)
	for i := 0; i < K; i++ {
		ri[i] = int(idx[i])
	}
	return out, ri
}

func cSilkBwexpanderFLP(ar []float32, chirp float32) []float32 {
	out := append([]float32(nil), ar...)
	if len(out) == 0 {
		return out
	}
	C.c_silk_bwexpander_flp((*C.float)(unsafe.Pointer(&out[0])), C.int(len(out)), C.float(chirp))
	return out
}

func cSilkInnerProductFLP(a, b []float32) float64 {
	if len(a) == 0 {
		return 0
	}
	return float64(C.c_silk_inner_product(
		(*C.float)(unsafe.Pointer(&a[0])),
		(*C.float)(unsafe.Pointer(&b[0])),
		C.int(len(a))))
}
func cSilkEnergyFLP(a []float32) float64 {
	if len(a) == 0 {
		return 0
	}
	return float64(C.c_silk_energy((*C.float)(unsafe.Pointer(&a[0])), C.int(len(a))))
}
func cSilkApplySineWindowFLP(in []float32, winType int) []float32 {
	out := make([]float32, len(in))
	if len(in) == 0 {
		return out
	}
	C.c_silk_apply_sine_window(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&in[0])),
		C.int(winType), C.int(len(in)))
	return out
}

func cSilkK2aFLP(rc []float32) []float32 {
	out := make([]float32, len(rc))
	if len(rc) == 0 {
		return out
	}
	C.c_silk_k2a(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&rc[0])),
		C.int(len(rc)))
	return out
}
func cSilkSchurFLP(ac []float32) ([]float32, float32) {
	order := len(ac) - 1
	refl := make([]float32, order)
	if order == 0 {
		return refl, ac[0]
	}
	res := C.c_silk_schur(
		(*C.float)(unsafe.Pointer(&refl[0])),
		(*C.float)(unsafe.Pointer(&ac[0])),
		C.int(order))
	return refl, float32(res)
}

func cSilkLTPScaleCtrlFLP(condCoding, packetLoss, nFramesPerPacket, SNR_dB_Q7, LBRR_flag int,
	LTPredCodGain float32) (int8, float32) {
	var idx C.int
	var sc C.float
	C.c_silk_LTP_scale_ctrl(C.int(condCoding), C.int(packetLoss), C.int(nFramesPerPacket),
		C.int(SNR_dB_Q7), C.int(LBRR_flag), C.float(LTPredCodGain), &idx, &sc)
	return int8(idx), float32(sc)
}

func cSilkAutocorrelationFLP(input []float32, correlationCount int) []float32 {
	out := make([]float32, correlationCount)
	if len(input) == 0 || correlationCount == 0 {
		return out
	}
	C.c_silk_autocorrelation(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&input[0])),
		C.int(len(input)), C.int(correlationCount))
	return out
}
func cSilkWarpedAutocorrelationFLP(input []float32, warping float32, order int) []float32 {
	out := make([]float32, order+1)
	if len(input) == 0 {
		return out
	}
	C.c_silk_warped_autocorrelation(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&input[0])),
		C.float(warping), C.int(len(input)), C.int(order))
	return out
}

func cSilkLPCInvPredGainFLP(A []float32) float32 {
	if len(A) == 0 {
		return 0
	}
	return float32(C.c_silk_LPC_inverse_pred_gain_flp(
		(*C.float)(unsafe.Pointer(&A[0])), C.int(len(A))))
}
func cSilkLPCAnalysisFilterFLP(PredCoef, s []float32, Order int) []float32 {
	out := make([]float32, len(s))
	if len(s) == 0 {
		return out
	}
	C.c_silk_LPC_analysis_filter_flp(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&PredCoef[0])),
		(*C.float)(unsafe.Pointer(&s[0])),
		C.int(len(s)), C.int(Order))
	return out
}
func cSilkLTPAnalysisFilterFLP(x []float32, xOff int, B []float32, pitchL []int, invGains []float32,
	subfr_length, nb_subfr, pre_length int) []float32 {
	out := make([]float32, nb_subfr*(subfr_length+pre_length))
	p := make([]C.opus_int, len(pitchL))
	for i, v := range pitchL {
		p[i] = C.opus_int(v)
	}
	C.c_silk_LTP_analysis_filter(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&x[0])),
		C.int(xOff),
		(*C.float)(unsafe.Pointer(&B[0])),
		(*C.opus_int)(unsafe.Pointer(&p[0])),
		(*C.float)(unsafe.Pointer(&invGains[0])),
		C.int(subfr_length), C.int(nb_subfr), C.int(pre_length))
	return out
}

func cSilkCorrVectorFLP(x, t []float32, L, Order int) []float32 {
	Xt := make([]float32, Order)
	if Order == 0 {
		return Xt
	}
	C.c_silk_corrVector(
		(*C.float)(unsafe.Pointer(&x[0])),
		(*C.float)(unsafe.Pointer(&t[0])),
		C.int(L), C.int(Order),
		(*C.float)(unsafe.Pointer(&Xt[0])))
	return Xt
}
func cSilkCorrMatrixFLP(x []float32, L, Order int) []float32 {
	XX := make([]float32, Order*Order)
	if Order == 0 {
		return XX
	}
	C.c_silk_corrMatrix(
		(*C.float)(unsafe.Pointer(&x[0])),
		C.int(L), C.int(Order),
		(*C.float)(unsafe.Pointer(&XX[0])))
	return XX
}
func cSilkRegularizeCorrelationsFLP(XX, xx []float32, noise float32, D int) ([]float32, []float32) {
	XXo := append([]float32(nil), XX...)
	xxo := append([]float32(nil), xx...)
	C.c_silk_regularize_correlations(
		(*C.float)(unsafe.Pointer(&XXo[0])),
		(*C.float)(unsafe.Pointer(&xxo[0])),
		C.float(noise), C.int(D))
	return XXo, xxo
}
func cSilkResidualEnergyCovarFLP(c []float32, wXX []float32, wXx []float32, wxx float32,
	D int) (float32, []float32) {
	wXXo := append([]float32(nil), wXX...)
	nrg := C.c_silk_residual_energy_covar(
		(*C.float)(unsafe.Pointer(&c[0])),
		(*C.float)(unsafe.Pointer(&wXXo[0])),
		(*C.float)(unsafe.Pointer(&wXx[0])),
		C.float(wxx), C.int(D))
	return float32(nrg), wXXo
}

func cSilkResidualEnergyFLP(x, a0, a1, gains []float32,
	subfr_length, nb_subfr, LPC_order int) []float32 {
	nrgs := make([]float32, 4)
	C.c_silk_residual_energy(
		(*C.float)(unsafe.Pointer(&nrgs[0])),
		(*C.float)(unsafe.Pointer(&x[0])),
		(*C.float)(unsafe.Pointer(&a0[0])),
		(*C.float)(unsafe.Pointer(&a1[0])),
		(*C.float)(unsafe.Pointer(&gains[0])),
		C.int(subfr_length), C.int(nb_subfr), C.int(LPC_order))
	return nrgs[:nb_subfr]
}

func cSilkBurgModifiedFLP(x []float32, minInvGain float32, subfr_length, nb_subfr, D int) (float32, []float32) {
	A := make([]float32, D)
	res := C.c_silk_burg_modified(
		(*C.float)(unsafe.Pointer(&A[0])),
		(*C.float)(unsafe.Pointer(&x[0])),
		C.float(minInvGain),
		C.int(subfr_length), C.int(nb_subfr), C.int(D))
	return float32(res), A
}
