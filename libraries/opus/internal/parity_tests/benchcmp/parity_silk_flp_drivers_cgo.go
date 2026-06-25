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

// Parity harness for silk_process_gains_FLP.
//
// Layout mirrors SilkEncoderStateFLPPayload on the Go side.
struct pg_payload {
    signed char  signalType;
    signed char  quantOffsetType;
    int          nb_subfr;
    int          subfr_length;
    int          SNR_dB_Q7;
    int          nStatesDelayedDecision;
    int          input_tilt_Q15;
    int          speech_activity_Q8;
    signed char  LastGainIndex;
    float        Gains[4];
    float        ResNrg[4];
    float        LTPredCodGain;
    float        input_quality;
    float        coding_quality;
    signed char  GainsIndices[4];
    float        Lambda;
    int          GainsUnq_Q16[4];
    signed char  lastGainIndexPrev;
    int          condCoding;
};

static void c_silk_process_gains(struct pg_payload *p) {
    silk_encoder_state_FLP psEnc;
    silk_encoder_control_FLP psEncCtrl;
    memset(&psEnc, 0, sizeof(psEnc));
    memset(&psEncCtrl, 0, sizeof(psEncCtrl));

    psEnc.sCmn.indices.signalType      = p->signalType;
    psEnc.sCmn.indices.quantOffsetType = p->quantOffsetType;
    psEnc.sCmn.nb_subfr                = p->nb_subfr;
    psEnc.sCmn.subfr_length            = p->subfr_length;
    psEnc.sCmn.SNR_dB_Q7               = p->SNR_dB_Q7;
    psEnc.sCmn.nStatesDelayedDecision  = p->nStatesDelayedDecision;
    psEnc.sCmn.input_tilt_Q15          = p->input_tilt_Q15;
    psEnc.sCmn.speech_activity_Q8      = p->speech_activity_Q8;
    psEnc.sShape.LastGainIndex         = p->LastGainIndex;
    for (int i = 0; i < 4; i++) {
        psEncCtrl.Gains[i]  = p->Gains[i];
        psEncCtrl.ResNrg[i] = p->ResNrg[i];
    }
    psEncCtrl.LTPredCodGain = p->LTPredCodGain;
    psEncCtrl.input_quality  = p->input_quality;
    psEncCtrl.coding_quality = p->coding_quality;

    silk_process_gains_FLP(&psEnc, &psEncCtrl, p->condCoding);

    p->signalType      = psEnc.sCmn.indices.signalType;
    p->quantOffsetType = psEnc.sCmn.indices.quantOffsetType;
    p->LastGainIndex   = psEnc.sShape.LastGainIndex;
    for (int i = 0; i < 4; i++) {
        p->Gains[i]        = psEncCtrl.Gains[i];
        p->GainsUnq_Q16[i] = psEncCtrl.GainsUnq_Q16[i];
        p->GainsIndices[i] = psEnc.sCmn.indices.GainsIndices[i];
    }
    p->Lambda            = psEncCtrl.Lambda;
    p->lastGainIndexPrev = psEncCtrl.lastGainIndexPrev;
}

// ----- silk_process_NLSFs_FLP harness -----

struct proc_nlsfs_flp_payload {
    int  wb;
    int  speech_activity_Q8;
    int  useInterpolatedNLSFs;
    int  NLSFInterpCoef_Q2;
    int  signalType;
    int  nb_subfr;
    int  NLSF_MSVQ_Survivors;
    opus_int16 pNLSF[MAX_LPC_ORDER];
    opus_int16 prev[MAX_LPC_ORDER];
    // Outputs
    float predA[MAX_LPC_ORDER];
    float predB[MAX_LPC_ORDER];
    opus_int16 pNLSFout[MAX_LPC_ORDER];
    opus_int8 NLSFIndices[MAX_LPC_ORDER+1];
};

static void c_silk_process_NLSFs_flp(struct proc_nlsfs_flp_payload *p) {
    silk_encoder_state s;
    memset(&s, 0, sizeof(s));
    s.speech_activity_Q8 = p->speech_activity_Q8;
    s.useInterpolatedNLSFs = p->useInterpolatedNLSFs;
    s.indices.NLSFInterpCoef_Q2 = p->NLSFInterpCoef_Q2;
    s.indices.signalType = p->signalType;
    s.nb_subfr = p->nb_subfr;
    s.NLSF_MSVQ_Survivors = p->NLSF_MSVQ_Survivors;
    if (p->wb) {
        s.psNLSF_CB = &silk_NLSF_CB_WB;
    } else {
        s.psNLSF_CB = &silk_NLSF_CB_NB_MB;
    }
    s.predictLPCOrder = s.psNLSF_CB->order;

    opus_int16 nlsf[MAX_LPC_ORDER];
    opus_int16 prev[MAX_LPC_ORDER];
    memcpy(nlsf, p->pNLSF, sizeof(nlsf));
    memcpy(prev, p->prev, sizeof(prev));

    float PredCoef[2][MAX_LPC_ORDER];
    silk_process_NLSFs_FLP(&s, PredCoef, nlsf, prev);

    memcpy(p->pNLSFout, nlsf, sizeof(nlsf));
    memcpy(p->NLSFIndices, s.indices.NLSFIndices, sizeof(s.indices.NLSFIndices));
    for (int i = 0; i < MAX_LPC_ORDER; i++) {
        p->predA[i] = PredCoef[0][i];
        p->predB[i] = PredCoef[1][i];
    }
}

// ----- silk_quant_LTP_gains_FLP harness -----

struct qltp_flp_payload {
    float XX[MAX_NB_SUBFR * LTP_ORDER * LTP_ORDER];
    float xX[MAX_NB_SUBFR * LTP_ORDER];
    int subfr_len;
    int nb_subfr;
    int sumLogGainQ7_in;
    // Outputs.
    float B[MAX_NB_SUBFR * LTP_ORDER];
    signed char cbk[MAX_NB_SUBFR];
    signed char periodicity_index;
    int sumLogGainQ7_out;
    float pred_gain_dB;
};

static void c_silk_quant_LTP_gains_flp(struct qltp_flp_payload *p) {
    opus_int8 cbk[MAX_NB_SUBFR];
    opus_int8 per = 0;
    opus_int32 slog = p->sumLogGainQ7_in;
    float pred_gain_dB = 0;
    float B[MAX_NB_SUBFR * LTP_ORDER];
    silk_quant_LTP_gains_FLP(B, cbk, &per, &slog, &pred_gain_dB,
        p->XX, p->xX, p->subfr_len, p->nb_subfr, 0);
    memcpy(p->B, B, sizeof(B));
    for (int i = 0; i < MAX_NB_SUBFR; i++) p->cbk[i] = cbk[i];
    p->periodicity_index = per;
    p->sumLogGainQ7_out  = slog;
    p->pred_gain_dB      = pred_gain_dB;
}

// ----- silk_NSQ_wrapper_FLP harness -----

struct nsq_wrapper_payload {
    // Indices.
    signed char signalType;
    signed char quantOffsetType;
    signed char LTP_scaleIndex;
    signed char Seed;
    signed char NLSFInterpCoef_Q2;
    signed char PERIndex;
    // State (sCmn).
    int nb_subfr;
    int frame_length;
    int subfr_length;
    int ltp_mem_length;
    int shapingLPCOrder;
    int predictLPCOrder;
    int nStatesDelayedDecision;
    int warping_Q16;
    int arch;
    // Encoder control.
    float AR[MAX_NB_SUBFR * MAX_SHAPE_LPC_ORDER];
    float LF_MA_shp[MAX_NB_SUBFR];
    float LF_AR_shp[MAX_NB_SUBFR];
    float Tilt[MAX_NB_SUBFR];
    float HarmShapeGain[MAX_NB_SUBFR];
    float Lambda;
    float LTPCoef[LTP_ORDER * MAX_NB_SUBFR];
    float PredCoef[2 * MAX_LPC_ORDER]; // flat: [0..MAX-1] = row 0, [MAX..] = row 1.
    float Gains[MAX_NB_SUBFR];
    int   pitchL[MAX_NB_SUBFR];
    // NSQ state seeded.
    int nsq_rand_seed;
    int nsq_lagPrev;
    int nsq_prev_gain_Q16;
    int nsq_sLTP_buf_idx;
    int nsq_sLTP_shp_buf_idx;
    int nsq_rewhite_flag;
    int nsq_sLF_AR_shp_Q14;
    int nsq_sDiff_shp_Q14;
    // I/O.
    int x_len;         // frame_length
    int pulses_len;    // frame_length
    float x[MAX_FRAME_LENGTH];
    signed char pulses[MAX_FRAME_LENGTH];
    // Outputs.
    int out_nsq_rand_seed;
    int out_nsq_lagPrev;
    int out_nsq_prev_gain_Q16;
    int out_nsq_sLTP_buf_idx;
    int out_nsq_sLTP_shp_buf_idx;
    int out_nsq_rewhite_flag;
    int out_nsq_sLF_AR_shp_Q14;
    int out_nsq_sDiff_shp_Q14;
};

static void c_silk_NSQ_wrapper_flp(struct nsq_wrapper_payload *p) {
    silk_encoder_state_FLP psEnc;
    silk_encoder_control_FLP psEncCtrl;
    SideInfoIndices psIndices;
    silk_nsq_state psNSQ;
    memset(&psEnc, 0, sizeof(psEnc));
    memset(&psEncCtrl, 0, sizeof(psEncCtrl));
    memset(&psIndices, 0, sizeof(psIndices));
    memset(&psNSQ, 0, sizeof(psNSQ));

    psEnc.sCmn.nb_subfr = p->nb_subfr;
    psEnc.sCmn.frame_length = p->frame_length;
    psEnc.sCmn.subfr_length = p->subfr_length;
    psEnc.sCmn.ltp_mem_length = p->ltp_mem_length;
    psEnc.sCmn.shapingLPCOrder = p->shapingLPCOrder;
    psEnc.sCmn.predictLPCOrder = p->predictLPCOrder;
    psEnc.sCmn.nStatesDelayedDecision = p->nStatesDelayedDecision;
    psEnc.sCmn.warping_Q16 = p->warping_Q16;
    psEnc.sCmn.arch = p->arch;

    psIndices.signalType = p->signalType;
    psIndices.quantOffsetType = p->quantOffsetType;
    psIndices.LTP_scaleIndex = p->LTP_scaleIndex;
    psIndices.Seed = p->Seed;
    psIndices.NLSFInterpCoef_Q2 = p->NLSFInterpCoef_Q2;
    psIndices.PERIndex = p->PERIndex;

    for (int i = 0; i < MAX_NB_SUBFR * MAX_SHAPE_LPC_ORDER; i++) {
        psEncCtrl.AR[i] = p->AR[i];
    }
    for (int i = 0; i < MAX_NB_SUBFR; i++) {
        psEncCtrl.LF_MA_shp[i] = p->LF_MA_shp[i];
        psEncCtrl.LF_AR_shp[i] = p->LF_AR_shp[i];
        psEncCtrl.Tilt[i] = p->Tilt[i];
        psEncCtrl.HarmShapeGain[i] = p->HarmShapeGain[i];
        psEncCtrl.Gains[i] = p->Gains[i];
        psEncCtrl.pitchL[i] = p->pitchL[i];
    }
    psEncCtrl.Lambda = p->Lambda;
    for (int i = 0; i < LTP_ORDER * MAX_NB_SUBFR; i++) psEncCtrl.LTPCoef[i] = p->LTPCoef[i];
    for (int j = 0; j < 2; j++)
        for (int i = 0; i < MAX_LPC_ORDER; i++)
            psEncCtrl.PredCoef[j][i] = p->PredCoef[j*MAX_LPC_ORDER+i];

    psNSQ.rand_seed = p->nsq_rand_seed;
    psNSQ.lagPrev = p->nsq_lagPrev;
    psNSQ.prev_gain_Q16 = p->nsq_prev_gain_Q16;
    psNSQ.sLTP_buf_idx = p->nsq_sLTP_buf_idx;
    psNSQ.sLTP_shp_buf_idx = p->nsq_sLTP_shp_buf_idx;
    psNSQ.rewhite_flag = p->nsq_rewhite_flag;
    psNSQ.sLF_AR_shp_Q14 = p->nsq_sLF_AR_shp_Q14;
    psNSQ.sDiff_shp_Q14 = p->nsq_sDiff_shp_Q14;

    silk_NSQ_wrapper_FLP(&psEnc, &psEncCtrl, &psIndices, &psNSQ,
        (opus_int8*)p->pulses, p->x);

    p->signalType = psIndices.signalType;
    p->quantOffsetType = psIndices.quantOffsetType;
    p->LTP_scaleIndex = psIndices.LTP_scaleIndex;
    p->Seed = psIndices.Seed;
    p->out_nsq_rand_seed = psNSQ.rand_seed;
    p->out_nsq_lagPrev = psNSQ.lagPrev;
    p->out_nsq_prev_gain_Q16 = psNSQ.prev_gain_Q16;
    p->out_nsq_sLTP_buf_idx = psNSQ.sLTP_buf_idx;
    p->out_nsq_sLTP_shp_buf_idx = psNSQ.sLTP_shp_buf_idx;
    p->out_nsq_rewhite_flag = psNSQ.rewhite_flag;
    p->out_nsq_sLF_AR_shp_Q14 = psNSQ.sLF_AR_shp_Q14;
    p->out_nsq_sDiff_shp_Q14 = psNSQ.sDiff_shp_Q14;
}
*/
import "C"
import (
	"unsafe"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func cSilkProcessGainsFLP(p nativeopus.SilkEncoderStateFLPPayload) nativeopus.SilkEncoderStateFLPPayload {
	var cp C.struct_pg_payload
	cp.signalType = C.schar(p.SignalType)
	cp.quantOffsetType = C.schar(p.QuantOffsetType)
	cp.nb_subfr = C.int(p.NbSubfr)
	cp.subfr_length = C.int(p.SubfrLength)
	cp.SNR_dB_Q7 = C.int(p.SNR_dB_Q7)
	cp.nStatesDelayedDecision = C.int(p.NStatesDelayedDecision)
	cp.input_tilt_Q15 = C.int(p.InputTiltQ15)
	cp.speech_activity_Q8 = C.int(p.SpeechActivityQ8)
	cp.LastGainIndex = C.schar(p.LastGainIndex)
	for i := 0; i < 4; i++ {
		cp.Gains[i] = C.float(p.Gains[i])
		cp.ResNrg[i] = C.float(p.ResNrg[i])
	}
	cp.LTPredCodGain = C.float(p.LTPredCodGain)
	cp.input_quality = C.float(p.InputQuality)
	cp.coding_quality = C.float(p.CodingQuality)
	cp.condCoding = C.int(p.CondCoding)

	C.c_silk_process_gains((*C.struct_pg_payload)(unsafe.Pointer(&cp)))

	out := p
	out.SignalType = int8(cp.signalType)
	out.QuantOffsetType = int8(cp.quantOffsetType)
	out.LastGainIndex = int8(cp.LastGainIndex)
	for i := 0; i < 4; i++ {
		out.Gains[i] = float32(cp.Gains[i])
		out.GainsUnqQ16[i] = int32(cp.GainsUnq_Q16[i])
		out.GainsIndicesIn[i] = int8(cp.GainsIndices[i])
	}
	out.Lambda = float32(cp.Lambda)
	out.LastGainIndexPrev = int8(cp.lastGainIndexPrev)
	return out
}

// ----- process_NLSFs_FLP wrapper -----
// A2NLSF / NLSF2A helpers live in parity_silk_flp_wave2b_cgo.go; we
// reuse those directly in the drivers test. The c_silk_A2NLSF_FLP and
// c_silk_NLSF2A_FLP static helpers are defined in both .c comments —
// harmless because cgo concatenates them at compile time.

type cProcessNLSFsFLPOut struct {
	PredA, PredB []float32
	NLSFOut      []int16
	Indices      []int8
}

func cSilkProcessNLSFsFLP(
	wb bool,
	speech_activity_Q8, useInterpolatedNLSFs, NLSFInterpCoef_Q2, signalType, nb_subfr, NLSF_MSVQ_Survivors int,
	nlsf, prev []int16,
) cProcessNLSFsFLPOut {
	var cp C.struct_proc_nlsfs_flp_payload
	if wb {
		cp.wb = 1
	}
	cp.speech_activity_Q8 = C.int(speech_activity_Q8)
	cp.useInterpolatedNLSFs = C.int(useInterpolatedNLSFs)
	cp.NLSFInterpCoef_Q2 = C.int(NLSFInterpCoef_Q2)
	cp.signalType = C.int(signalType)
	cp.nb_subfr = C.int(nb_subfr)
	cp.NLSF_MSVQ_Survivors = C.int(NLSF_MSVQ_Survivors)
	for i := 0; i < len(nlsf) && i < 16; i++ {
		cp.pNLSF[i] = C.opus_int16(nlsf[i])
	}
	for i := 0; i < len(prev) && i < 16; i++ {
		cp.prev[i] = C.opus_int16(prev[i])
	}
	C.c_silk_process_NLSFs_flp(&cp)

	out := cProcessNLSFsFLPOut{
		PredA:   make([]float32, 16),
		PredB:   make([]float32, 16),
		NLSFOut: make([]int16, 16),
		Indices: make([]int8, 17),
	}
	for i := 0; i < 16; i++ {
		out.PredA[i] = float32(cp.predA[i])
		out.PredB[i] = float32(cp.predB[i])
		out.NLSFOut[i] = int16(cp.pNLSFout[i])
	}
	for i := 0; i < 17; i++ {
		out.Indices[i] = int8(cp.NLSFIndices[i])
	}
	return out
}

// ----- quant_LTP_gains_FLP wrapper -----

type cQuantLTPGainsFLPOut struct {
	B              []float32
	CbkIndex       []int8
	PeriodicityIdx int8
	SumLogGainQ7   int32
	PredGainDB     float32
}

func cSilkQuantLTPGainsFLP(XX, xX []float32, subfr_len, nb_subfr int, sumLogGainQ7In int32) cQuantLTPGainsFLPOut {
	var cp C.struct_qltp_flp_payload
	for i := 0; i < len(XX) && i < 4*5*5; i++ {
		cp.XX[i] = C.float(XX[i])
	}
	for i := 0; i < len(xX) && i < 4*5; i++ {
		cp.xX[i] = C.float(xX[i])
	}
	cp.subfr_len = C.int(subfr_len)
	cp.nb_subfr = C.int(nb_subfr)
	cp.sumLogGainQ7_in = C.int(sumLogGainQ7In)
	C.c_silk_quant_LTP_gains_flp(&cp)

	out := cQuantLTPGainsFLPOut{
		B:              make([]float32, 4*5),
		CbkIndex:       make([]int8, 4),
		PeriodicityIdx: int8(cp.periodicity_index),
		SumLogGainQ7:   int32(cp.sumLogGainQ7_out),
		PredGainDB:     float32(cp.pred_gain_dB),
	}
	for i := 0; i < 4*5; i++ {
		out.B[i] = float32(cp.B[i])
	}
	for i := 0; i < 4; i++ {
		out.CbkIndex[i] = int8(cp.cbk[i])
	}
	return out
}

// ----- NSQ_wrapper_FLP wrapper -----

type NSQWrapperPayload struct {
	SignalType, QuantOffsetType, LTPScaleIndex, Seed, NLSFInterpCoefQ2, PERIndex int8
	NbSubfr, FrameLength, SubfrLength, LtpMemLength                              int
	ShapingLPCOrder, PredictLPCOrder, NStatesDelayedDecision, WarpingQ16, Arch   int

	AR            [4 * 24]float32 // MAX_NB_SUBFR*MAX_SHAPE_LPC_ORDER
	LFMAShp       [4]float32
	LFARShp       [4]float32
	Tilt          [4]float32
	HarmShapeGain [4]float32
	Lambda        float32
	LTPCoef       [5 * 4]float32
	PredCoef      [2 * 16]float32 // flat: [0..15] row 0, [16..31] row 1.
	Gains         [4]float32
	PitchL        [4]int32

	NSQRandSeed, NSQLagPrev, NSQPrevGainQ16, NSQSLTPBufIdx, NSQSLTPShpBufIdx, NSQRewhiteFlag int32
	NSQSLFARShpQ14, NSQSDiffShpQ14                                                           int32

	X      []float32
	Pulses []int8

	OutNSQRandSeed, OutNSQLagPrev, OutNSQPrevGainQ16, OutNSQSLTPBufIdx, OutNSQSLTPShpBufIdx int32
	OutNSQRewhiteFlag, OutNSQSLFARShpQ14, OutNSQSDiffShpQ14                                 int32
}

func cSilkNSQWrapperFLP(p NSQWrapperPayload) NSQWrapperPayload {
	var cp C.struct_nsq_wrapper_payload
	cp.signalType = C.schar(p.SignalType)
	cp.quantOffsetType = C.schar(p.QuantOffsetType)
	cp.LTP_scaleIndex = C.schar(p.LTPScaleIndex)
	cp.Seed = C.schar(p.Seed)
	cp.NLSFInterpCoef_Q2 = C.schar(p.NLSFInterpCoefQ2)
	cp.PERIndex = C.schar(p.PERIndex)
	cp.nb_subfr = C.int(p.NbSubfr)
	cp.frame_length = C.int(p.FrameLength)
	cp.subfr_length = C.int(p.SubfrLength)
	cp.ltp_mem_length = C.int(p.LtpMemLength)
	cp.shapingLPCOrder = C.int(p.ShapingLPCOrder)
	cp.predictLPCOrder = C.int(p.PredictLPCOrder)
	cp.nStatesDelayedDecision = C.int(p.NStatesDelayedDecision)
	cp.warping_Q16 = C.int(p.WarpingQ16)
	cp.arch = C.int(p.Arch)

	for i := 0; i < 4*24; i++ {
		cp.AR[i] = C.float(p.AR[i])
	}
	for i := 0; i < 4; i++ {
		cp.LF_MA_shp[i] = C.float(p.LFMAShp[i])
		cp.LF_AR_shp[i] = C.float(p.LFARShp[i])
		cp.Tilt[i] = C.float(p.Tilt[i])
		cp.HarmShapeGain[i] = C.float(p.HarmShapeGain[i])
		cp.Gains[i] = C.float(p.Gains[i])
		cp.pitchL[i] = C.int(p.PitchL[i])
	}
	cp.Lambda = C.float(p.Lambda)
	for i := 0; i < 5*4; i++ {
		cp.LTPCoef[i] = C.float(p.LTPCoef[i])
	}
	for i := 0; i < 2*16; i++ {
		cp.PredCoef[i] = C.float(p.PredCoef[i])
	}
	cp.nsq_rand_seed = C.int(p.NSQRandSeed)
	cp.nsq_lagPrev = C.int(p.NSQLagPrev)
	cp.nsq_prev_gain_Q16 = C.int(p.NSQPrevGainQ16)
	cp.nsq_sLTP_buf_idx = C.int(p.NSQSLTPBufIdx)
	cp.nsq_sLTP_shp_buf_idx = C.int(p.NSQSLTPShpBufIdx)
	cp.nsq_rewhite_flag = C.int(p.NSQRewhiteFlag)
	cp.nsq_sLF_AR_shp_Q14 = C.int(p.NSQSLFARShpQ14)
	cp.nsq_sDiff_shp_Q14 = C.int(p.NSQSDiffShpQ14)
	cp.x_len = C.int(len(p.X))
	cp.pulses_len = C.int(len(p.Pulses))
	for i, v := range p.X {
		cp.x[i] = C.float(v)
	}
	for i, v := range p.Pulses {
		cp.pulses[i] = C.schar(v)
	}

	C.c_silk_NSQ_wrapper_flp(&cp)

	out := p
	out.Pulses = make([]int8, len(p.Pulses))
	for i := range out.Pulses {
		out.Pulses[i] = int8(cp.pulses[i])
	}
	out.SignalType = int8(cp.signalType)
	out.QuantOffsetType = int8(cp.quantOffsetType)
	out.LTPScaleIndex = int8(cp.LTP_scaleIndex)
	out.Seed = int8(cp.Seed)
	out.OutNSQRandSeed = int32(cp.out_nsq_rand_seed)
	out.OutNSQLagPrev = int32(cp.out_nsq_lagPrev)
	out.OutNSQPrevGainQ16 = int32(cp.out_nsq_prev_gain_Q16)
	out.OutNSQSLTPBufIdx = int32(cp.out_nsq_sLTP_buf_idx)
	out.OutNSQSLTPShpBufIdx = int32(cp.out_nsq_sLTP_shp_buf_idx)
	out.OutNSQRewhiteFlag = int32(cp.out_nsq_rewhite_flag)
	out.OutNSQSLFARShpQ14 = int32(cp.out_nsq_sLF_AR_shp_Q14)
	out.OutNSQSDiffShpQ14 = int32(cp.out_nsq_sDiff_shp_Q14)
	return out
}
