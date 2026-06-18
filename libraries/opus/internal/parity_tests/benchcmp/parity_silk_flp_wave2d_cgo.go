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
#include <stdlib.h>

// Parity harness for silk_find_pred_coefs_FLP.
//
// Layout mirrors SilkFindPredCoefsFLPPayload on the Go side. The res_pitch
// and x buffers are stored flat with a per-struct length cap so we can
// pass them through a fixed-size cgo struct. Caller-supplied offsets into
// those buffers are preserved verbatim — mirrors the Go port, which takes
// a backing slice plus an absolute offset so that `x - predictLPCOrder`
// and `r_ptr - (lag[k] + LTP_ORDER/2)` stay in-bounds.

#define FPC_RESPITCH_MAX 4096
#define FPC_X_MAX        4096

struct fpc_payload {
    signed char signalType;
    int         nb_subfr;
    int         subfr_length;
    int         predictLPCOrder;
    int         ltp_mem_length;
    int         first_frame_after_reset;
    int         sum_log_gain_Q7_in;
    int         SNR_dB_Q7;
    int         PacketLoss_perc;
    int         nFramesPerPacket;
    signed char LBRR_flag;
    int         useInterpolatedNLSFs;
    int         speech_activity_Q8;
    int         NLSF_MSVQ_Survivors;
    int         wb;  // 1 = WB, 0 = NB_MB.
    signed char NLSFInterpCoef_Q2_in;
    opus_int16  prev_NLSFq_Q15[MAX_LPC_ORDER];
    int         arch;
    int         condCoding;

    float Gains[MAX_NB_SUBFR];
    int   pitchL[MAX_NB_SUBFR];
    float coding_quality;

    int   res_pitch_len;
    int   res_pitch_off;
    float res_pitch[FPC_RESPITCH_MAX];
    int   x_len;
    int   x_off;
    float x[FPC_X_MAX];

    // Outputs.
    float LTPCoef[LTP_ORDER * MAX_NB_SUBFR];
    signed char LTPIndex[MAX_NB_SUBFR];
    signed char PERIndex;
    int sum_log_gain_Q7_out;
    float LTPredCodGain;
    signed char LTP_scaleIndex;
    float LTP_scale;
    signed char NLSFInterpCoef_Q2_out;
    signed char NLSFIndices[MAX_LPC_ORDER + 1];
    opus_int16 prev_NLSFq_Q15_out[MAX_LPC_ORDER];
    float PredCoefA[MAX_LPC_ORDER];
    float PredCoefB[MAX_LPC_ORDER];
    float ResNrg[MAX_NB_SUBFR];
};

static void c_silk_find_pred_coefs_flp(struct fpc_payload *p) {
    silk_encoder_state_FLP psEnc;
    silk_encoder_control_FLP psEncCtrl;
    memset(&psEnc, 0, sizeof(psEnc));
    memset(&psEncCtrl, 0, sizeof(psEncCtrl));

    psEnc.sCmn.indices.signalType       = p->signalType;
    psEnc.sCmn.nb_subfr                 = p->nb_subfr;
    psEnc.sCmn.subfr_length             = p->subfr_length;
    psEnc.sCmn.predictLPCOrder          = p->predictLPCOrder;
    psEnc.sCmn.ltp_mem_length           = p->ltp_mem_length;
    psEnc.sCmn.first_frame_after_reset  = p->first_frame_after_reset;
    psEnc.sCmn.sum_log_gain_Q7          = p->sum_log_gain_Q7_in;
    psEnc.sCmn.SNR_dB_Q7                = p->SNR_dB_Q7;
    psEnc.sCmn.PacketLoss_perc          = p->PacketLoss_perc;
    psEnc.sCmn.nFramesPerPacket         = p->nFramesPerPacket;
    psEnc.sCmn.LBRR_flag                = p->LBRR_flag;
    psEnc.sCmn.useInterpolatedNLSFs     = p->useInterpolatedNLSFs;
    psEnc.sCmn.speech_activity_Q8       = p->speech_activity_Q8;
    psEnc.sCmn.NLSF_MSVQ_Survivors      = p->NLSF_MSVQ_Survivors;
    psEnc.sCmn.indices.NLSFInterpCoef_Q2 = p->NLSFInterpCoef_Q2_in;
    psEnc.sCmn.psNLSF_CB = p->wb ? &silk_NLSF_CB_WB : &silk_NLSF_CB_NB_MB;
    psEnc.sCmn.arch = p->arch;
    memcpy(psEnc.sCmn.prev_NLSFq_Q15, p->prev_NLSFq_Q15, sizeof(p->prev_NLSFq_Q15));

    for (int i = 0; i < MAX_NB_SUBFR; i++) {
        psEncCtrl.Gains[i]  = p->Gains[i];
        psEncCtrl.pitchL[i] = p->pitchL[i];
    }
    psEncCtrl.coding_quality = p->coding_quality;

    silk_find_pred_coefs_FLP(&psEnc, &psEncCtrl,
        p->res_pitch + p->res_pitch_off,
        p->x + p->x_off,
        p->condCoding);

    memcpy(p->LTPCoef, psEncCtrl.LTPCoef, sizeof(p->LTPCoef));
    memcpy(p->LTPIndex, psEnc.sCmn.indices.LTPIndex, sizeof(p->LTPIndex));
    p->PERIndex               = psEnc.sCmn.indices.PERIndex;
    p->sum_log_gain_Q7_out    = psEnc.sCmn.sum_log_gain_Q7;
    p->LTPredCodGain          = psEncCtrl.LTPredCodGain;
    p->LTP_scaleIndex         = psEnc.sCmn.indices.LTP_scaleIndex;
    p->LTP_scale              = psEncCtrl.LTP_scale;
    p->NLSFInterpCoef_Q2_out  = psEnc.sCmn.indices.NLSFInterpCoef_Q2;
    memcpy(p->NLSFIndices, psEnc.sCmn.indices.NLSFIndices, sizeof(p->NLSFIndices));
    memcpy(p->prev_NLSFq_Q15_out, psEnc.sCmn.prev_NLSFq_Q15, sizeof(p->prev_NLSFq_Q15_out));
    for (int i = 0; i < MAX_LPC_ORDER; i++) {
        p->PredCoefA[i] = psEncCtrl.PredCoef[0][i];
        p->PredCoefB[i] = psEncCtrl.PredCoef[1][i];
    }
    memcpy(p->ResNrg, psEncCtrl.ResNrg, sizeof(p->ResNrg));
}
*/
import "C"
import (
	"unsafe"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func cSilkFindPredCoefsFLP(p nativeopus.SilkFindPredCoefsFLPPayload) nativeopus.SilkFindPredCoefsFLPPayload {
	cp := (*C.struct_fpc_payload)(C.calloc(1, C.size_t(unsafe.Sizeof(C.struct_fpc_payload{}))))
	defer C.free(unsafe.Pointer(cp))

	cp.signalType = C.schar(p.SignalType)
	cp.nb_subfr = C.int(p.NbSubfr)
	cp.subfr_length = C.int(p.SubfrLength)
	cp.predictLPCOrder = C.int(p.PredictLPCOrder)
	cp.ltp_mem_length = C.int(p.LtpMemLength)
	cp.first_frame_after_reset = C.int(p.FirstFrameAfterReset)
	cp.sum_log_gain_Q7_in = C.int(p.SumLogGainQ7In)
	cp.SNR_dB_Q7 = C.int(p.SNRdBQ7)
	cp.PacketLoss_perc = C.int(p.PacketLossPerc)
	cp.nFramesPerPacket = C.int(p.NFramesPerPacket)
	cp.LBRR_flag = C.schar(p.LBRRFlag)
	cp.useInterpolatedNLSFs = C.int(p.UseInterpolatedNLSFs)
	cp.speech_activity_Q8 = C.int(p.SpeechActivityQ8)
	cp.NLSF_MSVQ_Survivors = C.int(p.NLSFMSVQSurvivors)
	if p.WB {
		cp.wb = 1
	}
	cp.NLSFInterpCoef_Q2_in = C.schar(p.NLSFInterpCoefQ2In)
	for i := 0; i < 16; i++ {
		cp.prev_NLSFq_Q15[i] = C.opus_int16(p.PrevNLSFqQ15[i])
	}
	cp.arch = C.int(p.Arch)
	cp.condCoding = C.int(p.CondCoding)
	for i := 0; i < 4; i++ {
		cp.Gains[i] = C.float(p.Gains[i])
		cp.pitchL[i] = C.int(p.PitchL[i])
	}
	cp.coding_quality = C.float(p.CodingQuality)

	cp.res_pitch_len = C.int(len(p.ResPitch))
	cp.res_pitch_off = C.int(p.ResPitchOff)
	for i, v := range p.ResPitch {
		if i >= 4096 {
			break
		}
		cp.res_pitch[i] = C.float(v)
	}
	cp.x_len = C.int(len(p.X))
	cp.x_off = C.int(p.XOff)
	for i, v := range p.X {
		if i >= 4096 {
			break
		}
		cp.x[i] = C.float(v)
	}

	C.c_silk_find_pred_coefs_flp(cp)

	out := p
	for i := 0; i < 5*4; i++ {
		out.LTPCoef[i] = float32(cp.LTPCoef[i])
	}
	for i := 0; i < 4; i++ {
		out.LTPIndex[i] = int8(cp.LTPIndex[i])
		out.ResNrg[i] = float32(cp.ResNrg[i])
	}
	out.PERIndex = int8(cp.PERIndex)
	out.SumLogGainQ7 = int32(cp.sum_log_gain_Q7_out)
	out.LTPredCodGain = float32(cp.LTPredCodGain)
	out.LTPScaleIndex = int8(cp.LTP_scaleIndex)
	out.LTPScale = float32(cp.LTP_scale)
	out.NLSFInterpCoefQ2 = int8(cp.NLSFInterpCoef_Q2_out)
	for i := 0; i < 17; i++ {
		out.NLSFIndices[i] = int8(cp.NLSFIndices[i])
	}
	for i := 0; i < 16; i++ {
		out.PrevNLSFqOut[i] = int16(cp.prev_NLSFq_Q15_out[i])
		out.PredCoefA[i] = float32(cp.PredCoefA[i])
		out.PredCoefB[i] = float32(cp.PredCoefB[i])
	}
	return out
}
