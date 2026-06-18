//go:build cgo

package benchcmp

/*
#include "config.h"
#include "structs.h"
#include "control.h"
#include "main.h"
#include "main_FLP.h"
#include "structs_FLP.h"
#include "tuning_parameters.h"
#include "SigProc_FIX.h"
#include <string.h>

int c_check_control_input(int nChannelsAPI, int nChannelsInternal,
    int APISampleRate, int maxInternal, int minInternal, int desiredInternal,
    int payloadSize_ms, int bitRate,
    int packetLossPercentage, int complexity,
    int useInBandFEC, int useDTX, int useCBR, int maxBits, int toMono,
    int opusCanSwitch, int reducedDependency) {
    silk_EncControlStruct ec;
    memset(&ec, 0, sizeof(ec));
    ec.nChannelsAPI = nChannelsAPI;
    ec.nChannelsInternal = nChannelsInternal;
    ec.API_sampleRate = APISampleRate;
    ec.maxInternalSampleRate = maxInternal;
    ec.minInternalSampleRate = minInternal;
    ec.desiredInternalSampleRate = desiredInternal;
    ec.payloadSize_ms = payloadSize_ms;
    ec.bitRate = bitRate;
    ec.packetLossPercentage = packetLossPercentage;
    ec.complexity = complexity;
    ec.useInBandFEC = useInBandFEC;
    ec.useDTX = useDTX;
    ec.useCBR = useCBR;
    ec.maxBits = maxBits;
    ec.toMono = toMono;
    ec.opusCanSwitch = opusCanSwitch;
    ec.reducedDependency = reducedDependency;
    return check_control_input(&ec);
}

void c_control_SNR(int fs_kHz, int nb_subfr, opus_int32 TargetRate_bps,
                   int *snr_dB_Q7, opus_int32 *storedTargetRate) {
    silk_encoder_state s;
    memset(&s, 0, sizeof(s));
    s.fs_kHz = fs_kHz;
    s.nb_subfr = nb_subfr;
    silk_control_SNR(&s, TargetRate_bps);
    *snr_dB_Q7 = s.SNR_dB_Q7;
    *storedTargetRate = s.TargetRate_bps;
}

int c_control_audio_bandwidth(
    int fs_kHz_in, opus_int32 savedFsKHz, opus_int32 transFrameNo, int mode,
    int allowBandwidthSwitch, opus_int32 APIfsHz, int maxInternalfsHz,
    int minInternalfsHz, int desiredInternalfsHz,
    opus_int32 inLPState0, opus_int32 inLPState1,
    int opusCanSwitch, int switchReady, int maxBits, int payloadSize_ms,
    // out
    int *fs_kHz_out, int *Mode_out, opus_int32 *TransFrameNo_out,
    opus_int32 *InLP0_out, opus_int32 *InLP1_out,
    int *SwitchReady_out, int *MaxBits_out) {
    silk_encoder_state s;
    memset(&s, 0, sizeof(s));
    s.fs_kHz = fs_kHz_in;
    s.sLP.saved_fs_kHz = savedFsKHz;
    s.sLP.transition_frame_no = transFrameNo;
    s.sLP.mode = mode;
    s.sLP.In_LP_State[0] = inLPState0;
    s.sLP.In_LP_State[1] = inLPState1;
    s.allow_bandwidth_switch = allowBandwidthSwitch;
    s.API_fs_Hz = APIfsHz;
    s.maxInternal_fs_Hz = maxInternalfsHz;
    s.minInternal_fs_Hz = minInternalfsHz;
    s.desiredInternal_fs_Hz = desiredInternalfsHz;
    silk_EncControlStruct ec;
    memset(&ec, 0, sizeof(ec));
    ec.opusCanSwitch = opusCanSwitch;
    ec.switchReady = switchReady;
    ec.maxBits = maxBits;
    ec.payloadSize_ms = payloadSize_ms;
    int fs = silk_control_audio_bandwidth(&s, &ec);
    *fs_kHz_out = s.fs_kHz;
    *Mode_out = s.sLP.mode;
    *TransFrameNo_out = s.sLP.transition_frame_no;
    *InLP0_out = s.sLP.In_LP_State[0];
    *InLP1_out = s.sLP.In_LP_State[1];
    *SwitchReady_out = ec.switchReady;
    *MaxBits_out = ec.maxBits;
    return fs;
}

void c_VAD_Init(opus_int32 *NL, opus_int32 *inv_NL, opus_int32 *bias,
                opus_int32 *smth, opus_int32 *counter) {
    silk_VAD_state v;
    silk_VAD_Init(&v);
    int i;
    for (i=0;i<VAD_N_BANDS;i++) { NL[i]=v.NL[i]; inv_NL[i]=v.inv_NL[i];
        bias[i]=v.NoiseLevelBias[i]; smth[i]=v.NrgRatioSmth_Q8[i]; }
    *counter = v.counter;
}

void c_VAD_GetSA_Q8(const opus_int16 *frame, int frame_length, int fs_kHz,
                    int *speech_activity_Q8, int *input_tilt_Q15,
                    int *input_quality_bands_Q15,
                    opus_int32 *NL_out, opus_int32 *inv_NL_out,
                    opus_int32 *Xnrg_out, opus_int32 *Smth_out) {
    silk_encoder_state s;
    memset(&s, 0, sizeof(s));
    silk_VAD_Init(&s.sVAD);
    s.fs_kHz = fs_kHz;
    s.frame_length = frame_length;
    silk_VAD_GetSA_Q8_c(&s, frame);
    *speech_activity_Q8 = s.speech_activity_Q8;
    *input_tilt_Q15 = s.input_tilt_Q15;
    int i;
    for (i=0;i<VAD_N_BANDS;i++) {
        input_quality_bands_Q15[i] = s.input_quality_bands_Q15[i];
        NL_out[i] = s.sVAD.NL[i];
        inv_NL_out[i] = s.sVAD.inv_NL[i];
        Xnrg_out[i] = s.sVAD.XnrgSubfr[i];
        Smth_out[i] = s.sVAD.NrgRatioSmth_Q8[i];
    }
}

opus_int32 c_HP_variable_cutoff(int prevSignalType, int prevLag, int fs_kHz,
                                int input_quality_0, int speech_activity_Q8,
                                opus_int32 smth1_in) {
    silk_encoder_state_FLP state_Fxx[2];
    memset(state_Fxx, 0, sizeof(state_Fxx));
    state_Fxx[0].sCmn.prevSignalType = (opus_int8)prevSignalType;
    state_Fxx[0].sCmn.prevLag = prevLag;
    state_Fxx[0].sCmn.fs_kHz = fs_kHz;
    state_Fxx[0].sCmn.input_quality_bands_Q15[0] = input_quality_0;
    state_Fxx[0].sCmn.speech_activity_Q8 = speech_activity_Q8;
    state_Fxx[0].sCmn.variable_HP_smth1_Q15 = smth1_in;
    silk_HP_variable_cutoff(state_Fxx);
    return state_Fxx[0].sCmn.variable_HP_smth1_Q15;
}

void c_init_encoder(int arch, int *firstFrame, opus_int32 *smth1, opus_int32 *smth2,
                    opus_int32 *vadCounter, opus_int32 *vadNL) {
    silk_encoder_state_FLP e;
    silk_init_encoder(&e, arch);
    *firstFrame = e.sCmn.first_frame_after_reset;
    *smth1 = e.sCmn.variable_HP_smth1_Q15;
    *smth2 = e.sCmn.variable_HP_smth2_Q15;
    *vadCounter = e.sCmn.sVAD.counter;
    int i;
    for (i=0;i<VAD_N_BANDS;i++) vadNL[i] = e.sCmn.sVAD.NL[i];
}

void c_VQ_WMat_EC(const opus_int32 *XX_Q17, const opus_int32 *xX_Q17,
                  const opus_int8 *cb_Q7, const opus_uint8 *cb_gain_Q7,
                  const opus_uint8 *cl_Q5, int subfr_len, opus_int32 max_gain_Q7,
                  int L,
                  opus_int8 *ind, opus_int32 *res_nrg_Q15, opus_int32 *rate_dist_Q8,
                  int *gain_Q7) {
    silk_VQ_WMat_EC_c(ind, res_nrg_Q15, rate_dist_Q8, gain_Q7,
        XX_Q17, xX_Q17, cb_Q7, cb_gain_Q7, cl_Q5, subfr_len, max_gain_Q7, L);
}

void c_quant_LTP_gains(const opus_int32 *XX_Q17, const opus_int32 *xX_Q17,
                       opus_int32 sum_log_gain_in, int subfr_len, int nb_subfr,
                       opus_int16 *B_Q14, opus_int8 *cbk_index,
                       opus_int8 *periodicity_index, opus_int32 *sum_log_gain_out,
                       int *pred_gain_dB_Q7) {
    opus_int32 slg = sum_log_gain_in;
    int pg = 0;
    silk_quant_LTP_gains(B_Q14, cbk_index, periodicity_index, &slg, &pg,
        XX_Q17, xX_Q17, subfr_len, nb_subfr, 0);
    *sum_log_gain_out = slg;
    *pred_gain_dB_Q7 = pg;
}

*/
import "C"
import "unsafe"

func cCheckControlInput(nAPI, nInt int32, fs, maxFs, minFs, desFs int32,
	pl_ms int, bitrate int32, loss, complexity int,
	useFEC, useDTX, useCBR, maxBits, toMono, opusCanSwitch, reducedDep int) int {
	return int(C.c_check_control_input(C.int(nAPI), C.int(nInt),
		C.int(fs), C.int(maxFs), C.int(minFs), C.int(desFs),
		C.int(pl_ms), C.int(bitrate), C.int(loss), C.int(complexity),
		C.int(useFEC), C.int(useDTX), C.int(useCBR),
		C.int(maxBits), C.int(toMono), C.int(opusCanSwitch), C.int(reducedDep)))
}

func cControlSNR(fs_kHz, nb_subfr int, targetRate int32) (snr int, stored int32) {
	var s C.int
	var t C.opus_int32
	C.c_control_SNR(C.int(fs_kHz), C.int(nb_subfr), C.opus_int32(targetRate), &s, &t)
	return int(s), int32(t)
}

type CABState struct {
	Fs_kHz               int
	SavedFsKHz           int32
	TransitionFrameNo    int32
	Mode                 int
	AllowBandwidthSwitch int
	APIfsHz              int32
	MaxInternalfsHz      int
	MinInternalfsHz      int
	DesiredInternalfsHz  int
	InLPState0           int32
	InLPState1           int32
}

type CABCtrl struct {
	OpusCanSwitch  int
	SwitchReady    int
	MaxBits        int
	PayloadSize_ms int
}

func cControlAudioBandwidth(stIn CABState, ecIn CABCtrl) (fs int, stOut CABState, ecOut CABCtrl) {
	var (
		fsOut                   C.int
		modeOut                 C.int
		transFNo                C.opus_int32
		inLP0, inLP1            C.opus_int32
		switchReady, maxBitsOut C.int
	)
	ret := C.c_control_audio_bandwidth(
		C.int(stIn.Fs_kHz), C.opus_int32(stIn.SavedFsKHz), C.opus_int32(stIn.TransitionFrameNo),
		C.int(stIn.Mode), C.int(stIn.AllowBandwidthSwitch), C.opus_int32(stIn.APIfsHz),
		C.int(stIn.MaxInternalfsHz), C.int(stIn.MinInternalfsHz), C.int(stIn.DesiredInternalfsHz),
		C.opus_int32(stIn.InLPState0), C.opus_int32(stIn.InLPState1),
		C.int(ecIn.OpusCanSwitch), C.int(ecIn.SwitchReady), C.int(ecIn.MaxBits), C.int(ecIn.PayloadSize_ms),
		&fsOut, &modeOut, &transFNo, &inLP0, &inLP1, &switchReady, &maxBitsOut)

	fs = int(ret)
	stOut = stIn
	stOut.Fs_kHz = int(fsOut)
	stOut.Mode = int(modeOut)
	stOut.TransitionFrameNo = int32(transFNo)
	stOut.InLPState0 = int32(inLP0)
	stOut.InLPState1 = int32(inLP1)
	ecOut = ecIn
	ecOut.SwitchReady = int(switchReady)
	ecOut.MaxBits = int(maxBitsOut)
	return
}

func cVADInit() (NL, inv_NL, bias, smth []int32, counter int32) {
	NL = make([]int32, 4)
	inv_NL = make([]int32, 4)
	bias = make([]int32, 4)
	smth = make([]int32, 4)
	var cc C.opus_int32
	C.c_VAD_Init(
		(*C.opus_int32)(unsafe.Pointer(&NL[0])),
		(*C.opus_int32)(unsafe.Pointer(&inv_NL[0])),
		(*C.opus_int32)(unsafe.Pointer(&bias[0])),
		(*C.opus_int32)(unsafe.Pointer(&smth[0])),
		&cc)
	counter = int32(cc)
	return
}

func cVADGetSAQ8(frame []int16, fs_kHz int) (
	speechActQ8, inputTiltQ15 int, iqb [4]int, NL, inv_NL, Xnrg, Smth [4]int32) {
	var (
		spa, tlt         C.int
		iqbArr           [4]C.int
		NLArr, invArr    [4]C.opus_int32
		XnrgArr, SmthArr [4]C.opus_int32
	)
	C.c_VAD_GetSA_Q8(
		(*C.opus_int16)(unsafe.Pointer(&frame[0])),
		C.int(len(frame)), C.int(fs_kHz),
		&spa, &tlt,
		&iqbArr[0],
		&NLArr[0], &invArr[0], &XnrgArr[0], &SmthArr[0])
	speechActQ8 = int(spa)
	inputTiltQ15 = int(tlt)
	for i := 0; i < 4; i++ {
		iqb[i] = int(iqbArr[i])
		NL[i] = int32(NLArr[i])
		inv_NL[i] = int32(invArr[i])
		Xnrg[i] = int32(XnrgArr[i])
		Smth[i] = int32(SmthArr[i])
	}
	return
}

func cHPVariableCutoff(prevSignalType, prevLag, fs_kHz, iq0, saQ8 int, smth1 int32) int32 {
	return int32(C.c_HP_variable_cutoff(C.int(prevSignalType), C.int(prevLag), C.int(fs_kHz),
		C.int(iq0), C.int(saQ8), C.opus_int32(smth1)))
}

func cInitEncoder(arch int) (firstFrame int, smth1, smth2 int32, vadCounter int32, vadNL [4]int32) {
	var (
		ff     C.int
		s1, s2 C.opus_int32
		vc     C.opus_int32
		vnl    [4]C.opus_int32
	)
	C.c_init_encoder(C.int(arch), &ff, &s1, &s2, &vc, &vnl[0])
	firstFrame = int(ff)
	smth1 = int32(s1)
	smth2 = int32(s2)
	vadCounter = int32(vc)
	for i := 0; i < 4; i++ {
		vadNL[i] = int32(vnl[i])
	}
	return
}

func cVQWMatEC(XX_Q17, xX_Q17 []int32, cb []int8, cbg, cl []uint8,
	subfr_len int, max_gain int32, L int) (ind int8, res, rate int32, g int) {
	var (
		iidx   C.opus_int8
		rn, rd C.opus_int32
		gQ7    C.int
	)
	C.c_VQ_WMat_EC(
		(*C.opus_int32)(unsafe.Pointer(&XX_Q17[0])),
		(*C.opus_int32)(unsafe.Pointer(&xX_Q17[0])),
		(*C.opus_int8)(unsafe.Pointer(&cb[0])),
		(*C.opus_uint8)(unsafe.Pointer(&cbg[0])),
		(*C.opus_uint8)(unsafe.Pointer(&cl[0])),
		C.int(subfr_len), C.opus_int32(max_gain), C.int(L),
		&iidx, &rn, &rd, &gQ7)
	ind = int8(iidx)
	res = int32(rn)
	rate = int32(rd)
	g = int(gQ7)
	return
}

func cQuantLTPGains(XX_Q17, xX_Q17 []int32, slgIn int32, subfr_len, nb_subfr int) (
	B []int16, cbk [4]int8, per int8, slgOut int32, pg int) {
	B = make([]int16, 4*5) // MAX_NB_SUBFR * LTP_ORDER
	var (
		cbki  [4]C.opus_int8
		perr  C.opus_int8
		slg   C.opus_int32
		pgOut C.int
	)
	C.c_quant_LTP_gains(
		(*C.opus_int32)(unsafe.Pointer(&XX_Q17[0])),
		(*C.opus_int32)(unsafe.Pointer(&xX_Q17[0])),
		C.opus_int32(slgIn), C.int(subfr_len), C.int(nb_subfr),
		(*C.opus_int16)(unsafe.Pointer(&B[0])),
		&cbki[0], &perr, &slg, &pgOut)
	for i := 0; i < 4; i++ {
		cbk[i] = int8(cbki[i])
	}
	per = int8(perr)
	slgOut = int32(slg)
	pg = int(pgOut)
	return
}
