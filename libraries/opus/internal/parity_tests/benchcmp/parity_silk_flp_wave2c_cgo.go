//go:build cgo

package benchcmp

/*
#include "config.h"
#include "SigProc_FLP.h"
#include "main_FLP.h"
#include "tuning_parameters.h"
#include "structs.h"
#include "pitch_est_defines.h"
#include <string.h>

// ---- silk_pitch_analysis_core_FLP harness ----
//
// Straightforward wrapper: the core takes scalar arguments plus
// pointers to output slots. We copy the C outputs into caller-
// provided buffers.
static int c_silk_pitch_analysis_core_flp(
    const float *frame,
    int          Fs_kHz,
    int          complexity,
    int          nb_subfr,
    int          prevLag,
    float        search_thres1,
    float        search_thres2,
    float        LTPCorrIn,
    int         *pitch_out_i32,  // len nb_subfr
    short       *lagIndex_out,
    signed char *contourIndex_out,
    float       *LTPCorr_out
) {
    opus_int pitchL[PE_MAX_NB_SUBFR];
    opus_int16 lagIndex;
    opus_int8  contourIndex;
    silk_float LTPCorr = LTPCorrIn;
    int voicing = silk_pitch_analysis_core_FLP(
        frame, pitchL, &lagIndex, &contourIndex,
        &LTPCorr, prevLag, search_thres1, search_thres2,
        Fs_kHz, complexity, nb_subfr, 0);
    for (int i = 0; i < nb_subfr; i++) {
        pitch_out_i32[i] = (int)pitchL[i];
    }
    *lagIndex_out     = lagIndex;
    *contourIndex_out = contourIndex;
    *LTPCorr_out      = LTPCorr;
    return voicing;
}

// ---- silk_find_pitch_lags_FLP harness ----

struct find_pitch_lags_payload {
    // Inputs (scalar state fields).
    int   Fs_kHz;
    int   nb_subfr;
    int   la_pitch;
    int   frame_length;
    int   ltp_mem_length;
    int   pitch_LPC_win_length;
    int   pitchEstimationLPCOrder;
    int   pitchEstimationComplexity;
    int   pitchEstimationThreshold_Q16;
    int   speech_activity_Q8;
    int   input_tilt_Q15;
    signed char prevSignalType;
    signed char signalType;
    int   first_frame_after_reset;
    int   prevLag;
    float LTPCorrIn;
    // Outputs.
    float        predGain;
    float        LTPCorr;
    int          pitchL[MAX_NB_SUBFR];
    short        lagIndex;
    signed char  contourIndex;
    signed char  signalType_out;
};

static void c_silk_find_pitch_lags_flp(
    struct find_pitch_lags_payload *p,
    const float *x,          // pointer to x[0] inside a larger buffer
    float       *res_out,    // length = la_pitch + frame_length + ltp_mem_length
    int          buf_len
) {
    silk_encoder_state_FLP psEnc;
    silk_encoder_control_FLP psEncCtrl;
    memset(&psEnc, 0, sizeof(psEnc));
    memset(&psEncCtrl, 0, sizeof(psEncCtrl));

    psEnc.sCmn.fs_kHz                       = p->Fs_kHz;
    psEnc.sCmn.nb_subfr                     = p->nb_subfr;
    psEnc.sCmn.la_pitch                     = p->la_pitch;
    psEnc.sCmn.frame_length                 = p->frame_length;
    psEnc.sCmn.ltp_mem_length               = p->ltp_mem_length;
    psEnc.sCmn.pitch_LPC_win_length         = p->pitch_LPC_win_length;
    psEnc.sCmn.pitchEstimationLPCOrder      = p->pitchEstimationLPCOrder;
    psEnc.sCmn.pitchEstimationComplexity    = p->pitchEstimationComplexity;
    psEnc.sCmn.pitchEstimationThreshold_Q16 = p->pitchEstimationThreshold_Q16;
    psEnc.sCmn.speech_activity_Q8           = p->speech_activity_Q8;
    psEnc.sCmn.input_tilt_Q15               = p->input_tilt_Q15;
    psEnc.sCmn.prevSignalType               = p->prevSignalType;
    psEnc.sCmn.indices.signalType           = p->signalType;
    psEnc.sCmn.first_frame_after_reset      = p->first_frame_after_reset;
    psEnc.sCmn.prevLag                      = p->prevLag;
    psEnc.LTPCorr                           = p->LTPCorrIn;

    silk_find_pitch_lags_FLP(&psEnc, &psEncCtrl, res_out, x, 0);

    p->predGain       = psEncCtrl.predGain;
    p->LTPCorr        = psEnc.LTPCorr;
    for (int i = 0; i < MAX_NB_SUBFR; i++) {
        p->pitchL[i] = (int)psEncCtrl.pitchL[i];
    }
    p->lagIndex       = psEnc.sCmn.indices.lagIndex;
    p->contourIndex   = psEnc.sCmn.indices.contourIndex;
    p->signalType_out = psEnc.sCmn.indices.signalType;
    (void)buf_len; // silence unused warning
}
*/
import "C"
import "unsafe"

type cPitchAnalysisCoreFLPOut struct {
	Voicing      int
	PitchOut     []int32
	LagIndex     int16
	ContourIndex int8
	LTPCorr      float32
}

func cSilkPitchAnalysisCoreFLP(
	frame []float32,
	Fs_kHz, complexity, nb_subfr, prevLag int,
	searchThres1, searchThres2, LTPCorrIn float32,
) cPitchAnalysisCoreFLPOut {
	pitchOut := make([]C.int, nb_subfr)
	var lag C.short
	var contour C.schar
	var ltp C.float
	voicing := C.c_silk_pitch_analysis_core_flp(
		(*C.float)(unsafe.Pointer(&frame[0])),
		C.int(Fs_kHz), C.int(complexity), C.int(nb_subfr), C.int(prevLag),
		C.float(searchThres1), C.float(searchThres2), C.float(LTPCorrIn),
		&pitchOut[0], &lag, &contour, &ltp)
	out := cPitchAnalysisCoreFLPOut{
		Voicing:      int(voicing),
		PitchOut:     make([]int32, nb_subfr),
		LagIndex:     int16(lag),
		ContourIndex: int8(contour),
		LTPCorr:      float32(ltp),
	}
	for i := 0; i < nb_subfr; i++ {
		out.PitchOut[i] = int32(pitchOut[i])
	}
	return out
}

type cFindPitchLagsFLPOut struct {
	Res          []float32
	PredGain     float32
	LTPCorr      float32
	PitchL       []int32
	LagIndex     int16
	ContourIndex int8
	SignalType   int8
}

type cFindPitchLagsFLPInputs struct {
	Fs_kHz                       int
	nb_subfr                     int
	la_pitch                     int
	frame_length                 int
	ltp_mem_length               int
	pitch_LPC_win_length         int
	pitchEstimationLPCOrder      int
	pitchEstimationComplexity    int
	pitchEstimationThreshold_Q16 int32
	speech_activity_Q8           int
	input_tilt_Q15               int
	prevSignalType               int8
	signalType                   int8
	first_frame_after_reset      int
	prevLag                      int
	LTPCorrIn                    float32
}

func cSilkFindPitchLagsFLP(in cFindPitchLagsFLPInputs, bigX []float32, xOff int) cFindPitchLagsFLPOut {
	buf_len := in.la_pitch + in.frame_length + in.ltp_mem_length
	res := make([]float32, buf_len)

	var p C.struct_find_pitch_lags_payload
	p.Fs_kHz = C.int(in.Fs_kHz)
	p.nb_subfr = C.int(in.nb_subfr)
	p.la_pitch = C.int(in.la_pitch)
	p.frame_length = C.int(in.frame_length)
	p.ltp_mem_length = C.int(in.ltp_mem_length)
	p.pitch_LPC_win_length = C.int(in.pitch_LPC_win_length)
	p.pitchEstimationLPCOrder = C.int(in.pitchEstimationLPCOrder)
	p.pitchEstimationComplexity = C.int(in.pitchEstimationComplexity)
	p.pitchEstimationThreshold_Q16 = C.int(in.pitchEstimationThreshold_Q16)
	p.speech_activity_Q8 = C.int(in.speech_activity_Q8)
	p.input_tilt_Q15 = C.int(in.input_tilt_Q15)
	p.prevSignalType = C.schar(in.prevSignalType)
	p.signalType = C.schar(in.signalType)
	p.first_frame_after_reset = C.int(in.first_frame_after_reset)
	p.prevLag = C.int(in.prevLag)
	p.LTPCorrIn = C.float(in.LTPCorrIn)

	xPtr := (*C.float)(unsafe.Pointer(&bigX[xOff]))
	resPtr := (*C.float)(unsafe.Pointer(&res[0]))
	C.c_silk_find_pitch_lags_flp(&p, xPtr, resPtr, C.int(buf_len))

	out := cFindPitchLagsFLPOut{
		Res:          res,
		PredGain:     float32(p.predGain),
		LTPCorr:      float32(p.LTPCorr),
		PitchL:       make([]int32, 4), // MAX_NB_SUBFR
		LagIndex:     int16(p.lagIndex),
		ContourIndex: int8(p.contourIndex),
		SignalType:   int8(p.signalType_out),
	}
	for i := 0; i < 4; i++ {
		out.PitchL[i] = int32(p.pitchL[i])
	}
	return out
}
