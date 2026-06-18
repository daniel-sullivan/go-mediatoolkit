//go:build cgo

package benchcmp

/*
#include "config.h"
#include "typedef.h"
#include "structs.h"
#include "control.h"
#include "main.h"
#include "main_FLP.h"
#include "structs_FLP.h"
#include "tuning_parameters.h"
#include "SigProc_FIX.h"
#include "define.h"
#include <string.h>

// The setup_resamplers / setup_fs / setup_complexity / setup_LBRR
// helpers are static inside control_codec.c. Rebuild them with a
// file-scope visibility by re-including the source with static removed;
// silk_control_encoder itself is suppressed via a macro rename to avoid
// colliding with the dylib symbol.
#define silk_control_encoder silk_control_encoder_unused_duplicate
#define static
#include "control_codec.c"
#undef static
#undef silk_control_encoder

static void c_setup_complexity(int fs_kHz, int predictLPCOrder, int complexity,
    int *pitchEstComplexity, opus_int32 *pitchEstThreshold, int *pitchEstLPCOrder,
    int *shapingLPCOrder, int *laShape, int *nStatesDelayedDecision,
    int *useInterpolatedNLSFs, int *NLSF_MSVQ_Survivors, int *warping_Q16,
    int *shapeWinLength, int *Complexity) {
    silk_encoder_state s;
    memset(&s, 0, sizeof(s));
    s.fs_kHz = fs_kHz;
    s.predictLPCOrder = predictLPCOrder;
    silk_setup_complexity(&s, complexity);
    *pitchEstComplexity = s.pitchEstimationComplexity;
    *pitchEstThreshold = s.pitchEstimationThreshold_Q16;
    *pitchEstLPCOrder = s.pitchEstimationLPCOrder;
    *shapingLPCOrder = s.shapingLPCOrder;
    *laShape = s.la_shape;
    *nStatesDelayedDecision = s.nStatesDelayedDecision;
    *useInterpolatedNLSFs = s.useInterpolatedNLSFs;
    *NLSF_MSVQ_Survivors = s.NLSF_MSVQ_Survivors;
    *warping_Q16 = s.warping_Q16;
    *shapeWinLength = s.shapeWinLength;
    *Complexity = s.Complexity;
}

static int c_setup_LBRR(int prevEnabled, int coded, int packetLossPerc,
    int *enabled, int *gainIncreases) {
    silk_encoder_state s;
    memset(&s, 0, sizeof(s));
    s.LBRR_enabled = prevEnabled;
    s.PacketLoss_perc = packetLossPerc;
    silk_EncControlStruct ec;
    memset(&ec, 0, sizeof(ec));
    ec.LBRR_coded = coded;
    int r = silk_setup_LBRR(&s, &ec);
    *enabled = s.LBRR_enabled;
    *gainIncreases = s.LBRR_GainIncreases;
    return r;
}

static int c_setup_fs(int curFsKHz, int curNbSubfr, int curPacketSize,
    int newFsKHz, int packetSizeMs,
    int *nFramesPerPacket, int *nbSubfr, int *frameLength, int *pitchLPCWinLen,
    int *subfrLength, int *ltpMemLength, int *laPitch, int *maxPitchLag,
    int *predictLPCOrder, int *fsKHz, int *prevLag, int *firstFrameReset,
    int *lastGainIndex, int *lagPrev, opus_int32 *prevGainQ16, int *prevSignalType,
    int *inputBufIx, int *nFramesEncoded, opus_int32 *targetRateBps, int *packetSizeMs_out) {
    silk_encoder_state_FLP s;
    memset(&s, 0, sizeof(s));
    s.sCmn.fs_kHz = curFsKHz;
    s.sCmn.nb_subfr = curNbSubfr;
    s.sCmn.PacketSize_ms = curPacketSize;
    // Preinitialize subfr_length/frame_length so the celt_assert at the
    // end of setup_fs (subfr_length * nb_subfr == frame_length) holds on
    // the no-fs-change path too.
    s.sCmn.subfr_length = 5 * curFsKHz;
    s.sCmn.frame_length = s.sCmn.subfr_length * curNbSubfr;
    int r = silk_setup_fs(&s, newFsKHz, packetSizeMs);
    *nFramesPerPacket = s.sCmn.nFramesPerPacket;
    *nbSubfr = s.sCmn.nb_subfr;
    *frameLength = s.sCmn.frame_length;
    *pitchLPCWinLen = s.sCmn.pitch_LPC_win_length;
    *subfrLength = s.sCmn.subfr_length;
    *ltpMemLength = s.sCmn.ltp_mem_length;
    *laPitch = s.sCmn.la_pitch;
    *maxPitchLag = s.sCmn.max_pitch_lag;
    *predictLPCOrder = s.sCmn.predictLPCOrder;
    *fsKHz = s.sCmn.fs_kHz;
    *prevLag = s.sCmn.prevLag;
    *firstFrameReset = s.sCmn.first_frame_after_reset;
    *lastGainIndex = s.sShape.LastGainIndex;
    *lagPrev = s.sCmn.sNSQ.lagPrev;
    *prevGainQ16 = s.sCmn.sNSQ.prev_gain_Q16;
    *prevSignalType = s.sCmn.prevSignalType;
    *inputBufIx = s.sCmn.inputBufIx;
    *nFramesEncoded = s.sCmn.nFramesEncoded;
    *targetRateBps = s.sCmn.TargetRate_bps;
    *packetSizeMs_out = s.sCmn.PacketSize_ms;
    return r;
}

// SilkNSQ wrapper — builds a minimal silk_encoder_state + silk_nsq_state,
// runs silk_NSQ_c, and returns mutated state through out-parameters.
// Arrays are sized for the maximal frame length so the caller doesn't
// have to know the layout exactly.
static void c_silk_NSQ(
    int fs_kHz, int nb_subfr, int predictLPCOrder, int shapingLPCOrder, int warping_Q16,
    const opus_int16 *x16,
    const opus_int16 *PredCoef_Q12, const opus_int16 *LTPCoef_Q14,
    const opus_int16 *AR_Q13,
    const int *HarmShapeGain_Q14, const int *Tilt_Q14,
    const opus_int32 *LF_shp_Q14, const opus_int32 *Gains_Q16,
    const int *pitchL,
    int Lambda_Q10, int LTP_scale_Q14,
    signed char Seed, signed char signalType, signed char quantOffsetType, signed char NLSFInterpCoef_Q2,
    int initLagPrev, opus_int32 initPrevGainQ16,
    const opus_int32 *initSLTPShpQ14,
    const opus_int16 *initXq,
    const opus_int32 *initSLPCQ14,
    const opus_int32 *initSAR2Q14,
    opus_int32 initSLFARShpQ14, opus_int32 initSDiffShpQ14,
    signed char *outPulses,
    opus_int16 *outXq, opus_int32 *outSLTPShpQ14,
    opus_int32 *outSLPCQ14, opus_int32 *outSAR2Q14,
    opus_int32 *outSLFAR, opus_int32 *outSDiff,
    int *outLagPrev, int *outSLTPBufIdx, int *outSLTPShpBufIdx,
    opus_int32 *outRandSeed, opus_int32 *outPrevGainQ16, int *outRewhiteFlag) {

    silk_encoder_state s;
    memset(&s, 0, sizeof(s));
    s.fs_kHz = fs_kHz;
    s.nb_subfr = nb_subfr;
    s.subfr_length = 5 * fs_kHz;
    s.frame_length = s.subfr_length * nb_subfr;
    s.ltp_mem_length = 20 * fs_kHz;
    s.predictLPCOrder = predictLPCOrder;
    s.shapingLPCOrder = shapingLPCOrder;
    s.warping_Q16 = warping_Q16;
    s.arch = 0;

    silk_nsq_state nsq;
    memset(&nsq, 0, sizeof(nsq));
    nsq.lagPrev = initLagPrev;
    nsq.prev_gain_Q16 = initPrevGainQ16;
    nsq.sLF_AR_shp_Q14 = initSLFARShpQ14;
    nsq.sDiff_shp_Q14 = initSDiffShpQ14;
    memcpy(nsq.sLTP_shp_Q14, initSLTPShpQ14, sizeof(nsq.sLTP_shp_Q14));
    memcpy(nsq.xq, initXq, sizeof(nsq.xq));
    memcpy(nsq.sLPC_Q14, initSLPCQ14, NSQ_LPC_BUF_LENGTH * sizeof(opus_int32));
    memcpy(nsq.sAR2_Q14, initSAR2Q14, MAX_SHAPE_LPC_ORDER * sizeof(opus_int32));

    SideInfoIndices idx;
    memset(&idx, 0, sizeof(idx));
    idx.Seed = Seed;
    idx.signalType = signalType;
    idx.quantOffsetType = quantOffsetType;
    idx.NLSFInterpCoef_Q2 = NLSFInterpCoef_Q2;

    silk_NSQ_c(&s, &nsq, &idx, x16, outPulses, PredCoef_Q12, LTPCoef_Q14, AR_Q13,
        HarmShapeGain_Q14, Tilt_Q14, LF_shp_Q14, Gains_Q16, pitchL,
        Lambda_Q10, LTP_scale_Q14);

    memcpy(outXq, nsq.xq, s.frame_length * sizeof(opus_int16));
    memcpy(outSLTPShpQ14, nsq.sLTP_shp_Q14, s.frame_length * sizeof(opus_int32));
    memcpy(outSLPCQ14, nsq.sLPC_Q14, NSQ_LPC_BUF_LENGTH * sizeof(opus_int32));
    memcpy(outSAR2Q14, nsq.sAR2_Q14, MAX_SHAPE_LPC_ORDER * sizeof(opus_int32));
    *outSLFAR = nsq.sLF_AR_shp_Q14;
    *outSDiff = nsq.sDiff_shp_Q14;
    *outLagPrev = nsq.lagPrev;
    *outSLTPBufIdx = nsq.sLTP_buf_idx;
    *outSLTPShpBufIdx = nsq.sLTP_shp_buf_idx;
    *outRandSeed = nsq.rand_seed;
    *outPrevGainQ16 = nsq.prev_gain_Q16;
    *outRewhiteFlag = nsq.rewhite_flag;
}

static void c_silk_NSQ_del_dec(
    int fs_kHz, int nb_subfr, int predictLPCOrder, int shapingLPCOrder, int warping_Q16,
    int nStatesDelayedDecision,
    const opus_int16 *x16,
    const opus_int16 *PredCoef_Q12, const opus_int16 *LTPCoef_Q14,
    const opus_int16 *AR_Q13,
    const int *HarmShapeGain_Q14, const int *Tilt_Q14,
    const opus_int32 *LF_shp_Q14, const opus_int32 *Gains_Q16,
    const int *pitchL,
    int Lambda_Q10, int LTP_scale_Q14,
    signed char Seed, signed char signalType, signed char quantOffsetType, signed char NLSFInterpCoef_Q2,
    int initLagPrev, opus_int32 initPrevGainQ16,
    const opus_int32 *initSLTPShpQ14,
    const opus_int16 *initXq,
    const opus_int32 *initSLPCQ14,
    const opus_int32 *initSAR2Q14,
    opus_int32 initSLFARShpQ14, opus_int32 initSDiffShpQ14,
    signed char *outPulses,
    opus_int16 *outXq, opus_int32 *outSLTPShpQ14,
    opus_int32 *outSLPCQ14, opus_int32 *outSAR2Q14,
    opus_int32 *outSLFAR, opus_int32 *outSDiff,
    int *outLagPrev, int *outSLTPBufIdx, int *outSLTPShpBufIdx,
    opus_int32 *outRandSeed, opus_int32 *outPrevGainQ16, int *outRewhiteFlag,
    signed char *outSeed) {

    silk_encoder_state s;
    memset(&s, 0, sizeof(s));
    s.fs_kHz = fs_kHz;
    s.nb_subfr = nb_subfr;
    s.subfr_length = 5 * fs_kHz;
    s.frame_length = s.subfr_length * nb_subfr;
    s.ltp_mem_length = 20 * fs_kHz;
    s.predictLPCOrder = predictLPCOrder;
    s.shapingLPCOrder = shapingLPCOrder;
    s.warping_Q16 = warping_Q16;
    s.nStatesDelayedDecision = nStatesDelayedDecision;
    s.arch = 0;

    silk_nsq_state nsq;
    memset(&nsq, 0, sizeof(nsq));
    nsq.lagPrev = initLagPrev;
    nsq.prev_gain_Q16 = initPrevGainQ16;
    nsq.sLF_AR_shp_Q14 = initSLFARShpQ14;
    nsq.sDiff_shp_Q14 = initSDiffShpQ14;
    memcpy(nsq.sLTP_shp_Q14, initSLTPShpQ14, sizeof(nsq.sLTP_shp_Q14));
    memcpy(nsq.xq, initXq, sizeof(nsq.xq));
    memcpy(nsq.sLPC_Q14, initSLPCQ14, NSQ_LPC_BUF_LENGTH * sizeof(opus_int32));
    memcpy(nsq.sAR2_Q14, initSAR2Q14, MAX_SHAPE_LPC_ORDER * sizeof(opus_int32));

    SideInfoIndices idx;
    memset(&idx, 0, sizeof(idx));
    idx.Seed = Seed;
    idx.signalType = signalType;
    idx.quantOffsetType = quantOffsetType;
    idx.NLSFInterpCoef_Q2 = NLSFInterpCoef_Q2;

    silk_NSQ_del_dec_c(&s, &nsq, &idx, x16, outPulses, PredCoef_Q12, LTPCoef_Q14, AR_Q13,
        HarmShapeGain_Q14, Tilt_Q14, LF_shp_Q14, Gains_Q16, pitchL,
        Lambda_Q10, LTP_scale_Q14);

    memcpy(outXq, nsq.xq, s.frame_length * sizeof(opus_int16));
    memcpy(outSLTPShpQ14, nsq.sLTP_shp_Q14, s.frame_length * sizeof(opus_int32));
    memcpy(outSLPCQ14, nsq.sLPC_Q14, NSQ_LPC_BUF_LENGTH * sizeof(opus_int32));
    memcpy(outSAR2Q14, nsq.sAR2_Q14, MAX_SHAPE_LPC_ORDER * sizeof(opus_int32));
    *outSLFAR = nsq.sLF_AR_shp_Q14;
    *outSDiff = nsq.sDiff_shp_Q14;
    *outLagPrev = nsq.lagPrev;
    *outSLTPBufIdx = nsq.sLTP_buf_idx;
    *outSLTPShpBufIdx = nsq.sLTP_shp_buf_idx;
    *outRandSeed = nsq.rand_seed;
    *outPrevGainQ16 = nsq.prev_gain_Q16;
    *outRewhiteFlag = nsq.rewhite_flag;
    *outSeed = idx.Seed;
}
*/
import "C"
import "unsafe"

// SilkComplexityOutC mirrors SilkComplexityOut on the C side.
type SilkComplexityOutC struct {
	PitchEstimationComplexity    int
	PitchEstimationThreshold_Q16 int32
	PitchEstimationLPCOrder      int
	ShapingLPCOrder              int
	LaShape                      int
	NStatesDelayedDecision       int
	UseInterpolatedNLSFs         int
	NLSF_MSVQ_Survivors          int
	Warping_Q16                  int
	ShapeWinLength               int
	Complexity                   int
}

func cSilkSetupComplexity(fs_kHz, predictLPCOrder, complexity int) SilkComplexityOutC {
	var o SilkComplexityOutC
	var pec, peL, slo, las, nsd, uin, nms, w, swl, c C.int
	var pet C.opus_int32
	C.c_setup_complexity(C.int(fs_kHz), C.int(predictLPCOrder), C.int(complexity),
		&pec, &pet, &peL, &slo, &las, &nsd, &uin, &nms, &w, &swl, &c)
	o.PitchEstimationComplexity = int(pec)
	o.PitchEstimationThreshold_Q16 = int32(pet)
	o.PitchEstimationLPCOrder = int(peL)
	o.ShapingLPCOrder = int(slo)
	o.LaShape = int(las)
	o.NStatesDelayedDecision = int(nsd)
	o.UseInterpolatedNLSFs = int(uin)
	o.NLSF_MSVQ_Survivors = int(nms)
	o.Warping_Q16 = int(w)
	o.ShapeWinLength = int(swl)
	o.Complexity = int(c)
	return o
}

func cSilkSetupLBRR(prevEnabled, coded, packetLossPerc int) (enabled, gainIncreases, ret int) {
	var e, g C.int
	r := C.c_setup_LBRR(C.int(prevEnabled), C.int(coded), C.int(packetLossPerc), &e, &g)
	return int(e), int(g), int(r)
}

// SilkSetupFsOutC mirrors SilkSetupFsOut on the C side.
type SilkSetupFsOutC struct {
	Ret              int
	NFramesPerPacket int
	NbSubfr          int
	FrameLength      int
	PitchLPCWinLen   int
	SubfrLength      int
	LtpMemLength     int
	LaPitch          int
	MaxPitchLag      int
	PredictLPCOrder  int
	FsKHz            int
	PrevLag          int
	FirstFrameReset  int
	LastGainIndex    int
	LagPrev          int
	PrevGainQ16      int32
	PrevSignalType   int
	InputBufIx       int
	NFramesEncoded   int
	TargetRateBps    int32
	PacketSizeMs     int
}

func cSilkSetupFS(curFsKHz, curNbSubfr, curPacketSize, newFsKHz, packetSizeMs int) SilkSetupFsOutC {
	var nfpp, nbs, fl, plpc, sl, ltpl, lap, mpl, plo, fs, pl, ffr, lgi, lpr, pst, ibix, nfe, psms C.int
	var pgQ, trbps C.opus_int32
	r := C.c_setup_fs(C.int(curFsKHz), C.int(curNbSubfr), C.int(curPacketSize),
		C.int(newFsKHz), C.int(packetSizeMs),
		&nfpp, &nbs, &fl, &plpc, &sl, &ltpl, &lap, &mpl, &plo, &fs, &pl, &ffr,
		&lgi, &lpr, &pgQ, &pst, &ibix, &nfe, &trbps, &psms)
	return SilkSetupFsOutC{
		Ret:              int(r),
		NFramesPerPacket: int(nfpp),
		NbSubfr:          int(nbs),
		FrameLength:      int(fl),
		PitchLPCWinLen:   int(plpc),
		SubfrLength:      int(sl),
		LtpMemLength:     int(ltpl),
		LaPitch:          int(lap),
		MaxPitchLag:      int(mpl),
		PredictLPCOrder:  int(plo),
		FsKHz:            int(fs),
		PrevLag:          int(pl),
		FirstFrameReset:  int(ffr),
		LastGainIndex:    int(lgi),
		LagPrev:          int(lpr),
		PrevGainQ16:      int32(pgQ),
		PrevSignalType:   int(pst),
		InputBufIx:       int(ibix),
		NFramesEncoded:   int(nfe),
		TargetRateBps:    int32(trbps),
		PacketSizeMs:     int(psms),
	}
}

// SilkNSQOutC mirrors SilkNSQIO on the Go side.
type SilkNSQOutC struct {
	Pulses           []int8
	XQ               []int16
	SLTP_shp_Q14     []int32
	SLPC_Q14         []int32
	SAR2_Q14         []int32
	SLF_AR_shp_Q14   int32
	SDiff_shp_Q14    int32
	LagPrev          int
	SLTP_buf_idx     int
	SLTP_shp_buf_idx int
	RandSeed         int32
	PrevGainQ16      int32
	RewhiteFlag      int
}

func cSilkNSQ(fs_kHz, nb_subfr, predictLPCOrder, shapingLPCOrder, warping_Q16 int,
	x16 []int16, predCoef, ltpCoef, arQ13 []int16,
	harm []int, tilt []int, lfShp, gains []int32, pitchL []int,
	lambdaQ10, ltpScaleQ14 int, seed, signalType, quantOffsetType, nlsfInterp int8,
	initLagPrev int, initPrevGainQ16 int32,
	initSLTPShpQ14 []int32, initXq []int16,
	initSLPCQ14, initSAR2Q14 []int32,
	initSLFAR, initSDiff int32,
) SilkNSQOutC {
	frameLength := 5 * fs_kHz * nb_subfr
	pulses := make([]C.schar, frameLength)
	outXq := make([]C.opus_int16, frameLength)
	outSLTPShp := make([]C.opus_int32, frameLength)
	outSLPC := make([]C.opus_int32, 16) // NSQ_LPC_BUF_LENGTH
	outSAR2 := make([]C.opus_int32, 24) // MAX_SHAPE_LPC_ORDER

	var outSLFAR, outSDiff, outRand, outPrevGain C.opus_int32
	var outLagPrev, outSLTPBuf, outSLTPShpBuf, outRewhite C.int

	harmC := make([]C.int, len(harm))
	for i, v := range harm {
		harmC[i] = C.int(v)
	}
	tiltC := make([]C.int, len(tilt))
	for i, v := range tilt {
		tiltC[i] = C.int(v)
	}
	pitchC := make([]C.int, len(pitchL))
	for i, v := range pitchL {
		pitchC[i] = C.int(v)
	}

	C.c_silk_NSQ(
		C.int(fs_kHz), C.int(nb_subfr), C.int(predictLPCOrder), C.int(shapingLPCOrder), C.int(warping_Q16),
		(*C.opus_int16)(unsafe.Pointer(&x16[0])),
		(*C.opus_int16)(unsafe.Pointer(&predCoef[0])),
		(*C.opus_int16)(unsafe.Pointer(&ltpCoef[0])),
		(*C.opus_int16)(unsafe.Pointer(&arQ13[0])),
		&harmC[0], &tiltC[0],
		(*C.opus_int32)(unsafe.Pointer(&lfShp[0])),
		(*C.opus_int32)(unsafe.Pointer(&gains[0])),
		&pitchC[0],
		C.int(lambdaQ10), C.int(ltpScaleQ14),
		C.schar(seed), C.schar(signalType), C.schar(quantOffsetType), C.schar(nlsfInterp),
		C.int(initLagPrev), C.opus_int32(initPrevGainQ16),
		(*C.opus_int32)(unsafe.Pointer(&initSLTPShpQ14[0])),
		(*C.opus_int16)(unsafe.Pointer(&initXq[0])),
		(*C.opus_int32)(unsafe.Pointer(&initSLPCQ14[0])),
		(*C.opus_int32)(unsafe.Pointer(&initSAR2Q14[0])),
		C.opus_int32(initSLFAR), C.opus_int32(initSDiff),
		&pulses[0], &outXq[0], &outSLTPShp[0], &outSLPC[0], &outSAR2[0],
		&outSLFAR, &outSDiff,
		&outLagPrev, &outSLTPBuf, &outSLTPShpBuf,
		&outRand, &outPrevGain, &outRewhite)

	out := SilkNSQOutC{
		Pulses:           make([]int8, frameLength),
		XQ:               make([]int16, frameLength),
		SLTP_shp_Q14:     make([]int32, frameLength),
		SLPC_Q14:         make([]int32, 16),
		SAR2_Q14:         make([]int32, 24),
		SLF_AR_shp_Q14:   int32(outSLFAR),
		SDiff_shp_Q14:    int32(outSDiff),
		LagPrev:          int(outLagPrev),
		SLTP_buf_idx:     int(outSLTPBuf),
		SLTP_shp_buf_idx: int(outSLTPShpBuf),
		RandSeed:         int32(outRand),
		PrevGainQ16:      int32(outPrevGain),
		RewhiteFlag:      int(outRewhite),
	}
	for i := 0; i < frameLength; i++ {
		out.Pulses[i] = int8(pulses[i])
		out.XQ[i] = int16(outXq[i])
		out.SLTP_shp_Q14[i] = int32(outSLTPShp[i])
	}
	for i := 0; i < 16; i++ {
		out.SLPC_Q14[i] = int32(outSLPC[i])
	}
	for i := 0; i < 24; i++ {
		out.SAR2_Q14[i] = int32(outSAR2[i])
	}
	return out
}

func cSilkNSQDelDec(fs_kHz, nb_subfr, predictLPCOrder, shapingLPCOrder, warping_Q16, nStatesDelayedDecision int,
	x16 []int16, predCoef, ltpCoef, arQ13 []int16,
	harm []int, tilt []int, lfShp, gains []int32, pitchL []int,
	lambdaQ10, ltpScaleQ14 int, seed, signalType, quantOffsetType, nlsfInterp int8,
	initLagPrev int, initPrevGainQ16 int32,
	initSLTPShpQ14 []int32, initXq []int16,
	initSLPCQ14, initSAR2Q14 []int32,
	initSLFAR, initSDiff int32,
) (SilkNSQOutC, int8) {
	frameLength := 5 * fs_kHz * nb_subfr
	pulses := make([]C.schar, frameLength)
	outXq := make([]C.opus_int16, frameLength)
	outSLTPShp := make([]C.opus_int32, frameLength)
	outSLPC := make([]C.opus_int32, 16)
	outSAR2 := make([]C.opus_int32, 24)

	var outSLFAR, outSDiff, outRand, outPrevGain C.opus_int32
	var outLagPrev, outSLTPBuf, outSLTPShpBuf, outRewhite C.int
	var outSeed C.schar

	harmC := make([]C.int, len(harm))
	for i, v := range harm {
		harmC[i] = C.int(v)
	}
	tiltC := make([]C.int, len(tilt))
	for i, v := range tilt {
		tiltC[i] = C.int(v)
	}
	pitchC := make([]C.int, len(pitchL))
	for i, v := range pitchL {
		pitchC[i] = C.int(v)
	}

	C.c_silk_NSQ_del_dec(
		C.int(fs_kHz), C.int(nb_subfr), C.int(predictLPCOrder), C.int(shapingLPCOrder), C.int(warping_Q16),
		C.int(nStatesDelayedDecision),
		(*C.opus_int16)(unsafe.Pointer(&x16[0])),
		(*C.opus_int16)(unsafe.Pointer(&predCoef[0])),
		(*C.opus_int16)(unsafe.Pointer(&ltpCoef[0])),
		(*C.opus_int16)(unsafe.Pointer(&arQ13[0])),
		&harmC[0], &tiltC[0],
		(*C.opus_int32)(unsafe.Pointer(&lfShp[0])),
		(*C.opus_int32)(unsafe.Pointer(&gains[0])),
		&pitchC[0],
		C.int(lambdaQ10), C.int(ltpScaleQ14),
		C.schar(seed), C.schar(signalType), C.schar(quantOffsetType), C.schar(nlsfInterp),
		C.int(initLagPrev), C.opus_int32(initPrevGainQ16),
		(*C.opus_int32)(unsafe.Pointer(&initSLTPShpQ14[0])),
		(*C.opus_int16)(unsafe.Pointer(&initXq[0])),
		(*C.opus_int32)(unsafe.Pointer(&initSLPCQ14[0])),
		(*C.opus_int32)(unsafe.Pointer(&initSAR2Q14[0])),
		C.opus_int32(initSLFAR), C.opus_int32(initSDiff),
		&pulses[0], &outXq[0], &outSLTPShp[0], &outSLPC[0], &outSAR2[0],
		&outSLFAR, &outSDiff,
		&outLagPrev, &outSLTPBuf, &outSLTPShpBuf,
		&outRand, &outPrevGain, &outRewhite, &outSeed)

	out := SilkNSQOutC{
		Pulses:           make([]int8, frameLength),
		XQ:               make([]int16, frameLength),
		SLTP_shp_Q14:     make([]int32, frameLength),
		SLPC_Q14:         make([]int32, 16),
		SAR2_Q14:         make([]int32, 24),
		SLF_AR_shp_Q14:   int32(outSLFAR),
		SDiff_shp_Q14:    int32(outSDiff),
		LagPrev:          int(outLagPrev),
		SLTP_buf_idx:     int(outSLTPBuf),
		SLTP_shp_buf_idx: int(outSLTPShpBuf),
		RandSeed:         int32(outRand),
		PrevGainQ16:      int32(outPrevGain),
		RewhiteFlag:      int(outRewhite),
	}
	for i := 0; i < frameLength; i++ {
		out.Pulses[i] = int8(pulses[i])
		out.XQ[i] = int16(outXq[i])
		out.SLTP_shp_Q14[i] = int32(outSLTPShp[i])
	}
	for i := 0; i < 16; i++ {
		out.SLPC_Q14[i] = int32(outSLPC[i])
	}
	for i := 0; i < 24; i++ {
		out.SAR2_Q14[i] = int32(outSAR2[i])
	}
	return out, int8(outSeed)
}
