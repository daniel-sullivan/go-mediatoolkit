//go:build cgo

package benchcmp

/*
#include "config.h"
#include "SigProc_FLP.h"
#include "main_FLP.h"
#include "tuning_parameters.h"
#include "structs.h"
#include <string.h>

// ---- find_LTP_FLP ----

static void c_silk_find_LTP(float *XX, float *xX,
    const float *rBuf, int rOff,
    const opus_int *lag, int subfr_length, int nb_subfr) {
    silk_find_LTP_FLP(XX, xX, rBuf + rOff, lag, subfr_length, nb_subfr, 0);
}

// ---- A2NLSF_FLP / NLSF2A_FLP wrappers ----

static void c_silk_A2NLSF_FLP(opus_int16 *NLSF_Q15, const float *pAR, int LPC_order) {
    silk_A2NLSF_FLP(NLSF_Q15, pAR, LPC_order);
}

static void c_silk_NLSF2A_FLP(float *pAR, const opus_int16 *NLSF_Q15, int LPC_order) {
    silk_NLSF2A_FLP(pAR, NLSF_Q15, LPC_order, 0);
}

// ---- find_LPC_FLP driver ----

typedef struct {
    int predictLPCOrder;
    int nb_subfr;
    int subfr_length;
    int useInterpolatedNLSFs;
    int first_frame_after_reset;
    opus_int16 prev_NLSFq_Q15[MAX_LPC_ORDER];
    opus_int8 initialNLSFInterpCoefQ2;
} find_lpc_in;

typedef struct {
    opus_int16 NLSF_Q15[MAX_LPC_ORDER];
    opus_int8 NLSFInterpCoef_Q2;
} find_lpc_out;

static void c_silk_find_LPC_FLP(const find_lpc_in *in,
    const float *x, float minInvGain, find_lpc_out *out) {
    silk_encoder_state psEncC;
    memset(&psEncC, 0, sizeof(psEncC));
    psEncC.predictLPCOrder = in->predictLPCOrder;
    psEncC.nb_subfr = in->nb_subfr;
    psEncC.subfr_length = in->subfr_length;
    psEncC.useInterpolatedNLSFs = in->useInterpolatedNLSFs;
    psEncC.first_frame_after_reset = in->first_frame_after_reset;
    memcpy(psEncC.prev_NLSFq_Q15, in->prev_NLSFq_Q15, sizeof(psEncC.prev_NLSFq_Q15));
    psEncC.indices.NLSFInterpCoef_Q2 = in->initialNLSFInterpCoefQ2;
    psEncC.arch = 0;

    opus_int16 NLSF_Q15[MAX_LPC_ORDER] = {0};
    silk_find_LPC_FLP(&psEncC, NLSF_Q15, x, minInvGain, 0);

    memcpy(out->NLSF_Q15, NLSF_Q15, sizeof(NLSF_Q15));
    out->NLSFInterpCoef_Q2 = psEncC.indices.NLSFInterpCoef_Q2;
}
*/
import "C"
import "unsafe"

// ---- Go-side wrappers ----

func cSilkFindLTPFLP(rBuf []float32, rOff int, lag []int,
	subfr_length, nb_subfr int) (XX []float32, xX []float32) {
	XX = make([]float32, nb_subfr*5*5) // LTP_ORDER == 5
	xX = make([]float32, nb_subfr*5)
	lg := make([]C.opus_int, len(lag))
	for i, v := range lag {
		lg[i] = C.opus_int(v)
	}
	C.c_silk_find_LTP(
		(*C.float)(unsafe.Pointer(&XX[0])),
		(*C.float)(unsafe.Pointer(&xX[0])),
		(*C.float)(unsafe.Pointer(&rBuf[0])),
		C.int(rOff),
		(*C.opus_int)(unsafe.Pointer(&lg[0])),
		C.int(subfr_length), C.int(nb_subfr))
	return
}

func cSilkA2NLSFFLP(pAR []float32) []int16 {
	out := make([]C.opus_int16, len(pAR))
	if len(pAR) == 0 {
		return nil
	}
	C.c_silk_A2NLSF_FLP(
		(*C.opus_int16)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&pAR[0])),
		C.int(len(pAR)))
	r := make([]int16, len(pAR))
	for i, v := range out {
		r[i] = int16(v)
	}
	return r
}

func cSilkNLSF2AFLP(NLSF []int16) []float32 {
	in := make([]C.opus_int16, len(NLSF))
	for i, v := range NLSF {
		in[i] = C.opus_int16(v)
	}
	out := make([]float32, len(NLSF))
	if len(NLSF) == 0 {
		return out
	}
	C.c_silk_NLSF2A_FLP(
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.opus_int16)(unsafe.Pointer(&in[0])),
		C.int(len(NLSF)))
	return out
}

// cFindLPCInput / cFindLPCOutput mirror the Go-side driver types.
type cFindLPCInput struct {
	PredictLPCOrder         int
	NbSubfr                 int
	SubfrLength             int
	UseInterpolatedNLSFs    int
	FirstFrameAfterReset    int
	PrevNLSFqQ15            []int16
	InitialNLSFInterpCoefQ2 int8
	X                       []float32
	MinInvGain              float32
}

type cFindLPCOutput struct {
	NLSFQ15          []int16
	NLSFInterpCoefQ2 int8
}

func cSilkFindLPCFLP(in cFindLPCInput) cFindLPCOutput {
	var cin C.find_lpc_in
	cin.predictLPCOrder = C.int(in.PredictLPCOrder)
	cin.nb_subfr = C.int(in.NbSubfr)
	cin.subfr_length = C.int(in.SubfrLength)
	cin.useInterpolatedNLSFs = C.int(in.UseInterpolatedNLSFs)
	cin.first_frame_after_reset = C.int(in.FirstFrameAfterReset)
	for i, v := range in.PrevNLSFqQ15 {
		if i < int(C.MAX_LPC_ORDER) {
			cin.prev_NLSFq_Q15[i] = C.opus_int16(v)
		}
	}
	cin.initialNLSFInterpCoefQ2 = C.opus_int8(in.InitialNLSFInterpCoefQ2)

	var cout C.find_lpc_out
	C.c_silk_find_LPC_FLP(&cin,
		(*C.float)(unsafe.Pointer(&in.X[0])),
		C.float(in.MinInvGain),
		&cout)

	out := cFindLPCOutput{
		NLSFQ15:          make([]int16, int(C.MAX_LPC_ORDER)),
		NLSFInterpCoefQ2: int8(cout.NLSFInterpCoef_Q2),
	}
	for i := 0; i < int(C.MAX_LPC_ORDER); i++ {
		out.NLSFQ15[i] = int16(cout.NLSF_Q15[i])
	}
	return out
}
