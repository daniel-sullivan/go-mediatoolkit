//go:build cgo

package benchcmp

/*
#include "config.h"
#include "structs.h"
#include "control.h"
#include "main.h"
#include "main_FLP.h"
#include "structs_FLP.h"
#include "SigProc_FIX.h"
#include "tables.h"
#include "entenc.h"
#include <string.h>

static const silk_NLSF_CB_struct *pick_enc_cb(int wb) {
    if (wb) return &silk_NLSF_CB_WB;
    return &silk_NLSF_CB_NB_MB;
}

void c_process_NLSFs(int wb, int speech_activity_Q8, int useInterpolatedNLSFs,
                     int NLSFInterpCoef_Q2, int signalType, int nb_subfr,
                     int NLSF_MSVQ_Survivors,
                     opus_int16 *pNLSF, const opus_int16 *prev_NLSFq,
                     opus_int16 *predA, opus_int16 *predB, opus_int8 *indices) {
    silk_encoder_state s;
    memset(&s, 0, sizeof(s));
    s.speech_activity_Q8 = speech_activity_Q8;
    s.useInterpolatedNLSFs = useInterpolatedNLSFs;
    s.indices.NLSFInterpCoef_Q2 = (opus_int8)NLSFInterpCoef_Q2;
    s.indices.signalType = (opus_int8)signalType;
    s.nb_subfr = nb_subfr;
    s.NLSF_MSVQ_Survivors = NLSF_MSVQ_Survivors;
    s.psNLSF_CB = pick_enc_cb(wb);
    s.predictLPCOrder = s.psNLSF_CB->order;
    opus_int16 pc[2][MAX_LPC_ORDER];
    silk_process_NLSFs(&s, pc, pNLSF, prev_NLSFq);
    memcpy(predA, pc[0], MAX_LPC_ORDER * sizeof(opus_int16));
    memcpy(predB, pc[1], MAX_LPC_ORDER * sizeof(opus_int16));
    memcpy(indices, s.indices.NLSFIndices, (MAX_LPC_ORDER+1) * sizeof(opus_int8));
}

void c_stereo_LR_to_MS(opus_int16 *x1Buf, opus_int16 *x2Buf,
                       opus_int32 total_rate_bps, int prev_speech_act_Q8,
                       int toMono, int fs_kHz, int frame_length,
                       // state inputs
                       opus_int16 predPrev0, opus_int16 predPrev1,
                       opus_int16 sMid0, opus_int16 sMid1,
                       opus_int16 sSide0, opus_int16 sSide1,
                       const opus_int32 *midSideAmp,
                       opus_int16 smthWidth, opus_int16 widthPrev, opus_int16 silentSideLen,
                       // outputs
                       opus_int8 *ix_flat, opus_int8 *mid_only,
                       opus_int32 *midRate, opus_int32 *sideRate,
                       opus_int16 *predPrev0_out, opus_int16 *predPrev1_out,
                       opus_int16 *sMid0_out, opus_int16 *sMid1_out,
                       opus_int16 *sSide0_out, opus_int16 *sSide1_out,
                       opus_int32 *midSideAmp_out,
                       opus_int16 *smthWidth_out, opus_int16 *widthPrev_out,
                       opus_int16 *silentSideLen_out) {
    stereo_enc_state st;
    memset(&st, 0, sizeof(st));
    st.pred_prev_Q13[0] = predPrev0;
    st.pred_prev_Q13[1] = predPrev1;
    st.sMid[0] = sMid0;
    st.sMid[1] = sMid1;
    st.sSide[0] = sSide0;
    st.sSide[1] = sSide1;
    memcpy(st.mid_side_amp_Q0, midSideAmp, 4 * sizeof(opus_int32));
    st.smth_width_Q14 = smthWidth;
    st.width_prev_Q14 = widthPrev;
    st.silent_side_len = silentSideLen;

    // C needs x1 to point at frame_start (our x1Buf[2..]). The caller
    // passes the full buffer starting at index 0 (pre-history).
    opus_int8 ix[2][3];
    opus_int32 mid_side_rates[2];
    silk_stereo_LR_to_MS(&st, x1Buf + 2, x2Buf + 2, ix, mid_only,
                         mid_side_rates, total_rate_bps, prev_speech_act_Q8,
                         toMono, fs_kHz, frame_length);
    int i, j;
    for (i = 0; i < 2; i++) for (j = 0; j < 3; j++) ix_flat[i*3+j] = ix[i][j];
    *midRate = mid_side_rates[0];
    *sideRate = mid_side_rates[1];
    *predPrev0_out = st.pred_prev_Q13[0];
    *predPrev1_out = st.pred_prev_Q13[1];
    *sMid0_out = st.sMid[0]; *sMid1_out = st.sMid[1];
    *sSide0_out = st.sSide[0]; *sSide1_out = st.sSide[1];
    memcpy(midSideAmp_out, st.mid_side_amp_Q0, 4 * sizeof(opus_int32));
    *smthWidth_out = st.smth_width_Q14;
    *widthPrev_out = st.width_prev_Q14;
    *silentSideLen_out = st.silent_side_len;
}

int c_encode_indices(int wb, int nb_subfr, int fs_kHz,
                     int signalType, int quantOffsetType,
                     const opus_int8 *gainsIdx, const opus_int8 *NLSFIdx,
                     int lagIndex, int contourIdx, int NLSFInterpCoef_Q2,
                     int PERIndex, const opus_int8 *LTPIdx, int LTPscaleIdx,
                     int seed, int ec_prevSignalType, int ec_prevLagIndex,
                     int encode_LBRR, int condCoding,
                     unsigned char *buf, int bufSize) {
    silk_encoder_state s;
    memset(&s, 0, sizeof(s));
    s.nb_subfr = nb_subfr;
    s.fs_kHz = fs_kHz;
    s.psNLSF_CB = pick_enc_cb(wb);
    s.predictLPCOrder = s.psNLSF_CB->order;
    s.ec_prevSignalType = ec_prevSignalType;
    s.ec_prevLagIndex = ec_prevLagIndex;
    if (fs_kHz == 8 && nb_subfr == 4) {
        s.pitch_lag_low_bits_iCDF = silk_uniform4_iCDF;
        s.pitch_contour_iCDF = silk_pitch_contour_NB_iCDF;
    } else if (fs_kHz == 8) {
        s.pitch_lag_low_bits_iCDF = silk_uniform4_iCDF;
        s.pitch_contour_iCDF = silk_pitch_contour_10_ms_NB_iCDF;
    } else if (nb_subfr == 4) {
        s.pitch_lag_low_bits_iCDF = silk_uniform8_iCDF;
        s.pitch_contour_iCDF = silk_pitch_contour_iCDF;
    } else {
        s.pitch_lag_low_bits_iCDF = silk_uniform8_iCDF;
        s.pitch_contour_iCDF = silk_pitch_contour_10_ms_iCDF;
    }
    s.indices.signalType = (opus_int8)signalType;
    s.indices.quantOffsetType = (opus_int8)quantOffsetType;
    memcpy(s.indices.GainsIndices, gainsIdx, MAX_NB_SUBFR);
    memcpy(s.indices.NLSFIndices, NLSFIdx, MAX_LPC_ORDER+1);
    s.indices.lagIndex = (opus_int16)lagIndex;
    s.indices.contourIndex = (opus_int8)contourIdx;
    s.indices.NLSFInterpCoef_Q2 = (opus_int8)NLSFInterpCoef_Q2;
    s.indices.PERIndex = (opus_int8)PERIndex;
    memcpy(s.indices.LTPIndex, LTPIdx, MAX_NB_SUBFR);
    s.indices.LTP_scaleIndex = (opus_int8)LTPscaleIdx;
    s.indices.Seed = (opus_int8)seed;
    ec_enc enc;
    ec_enc_init(&enc, buf, bufSize);
    silk_encode_indices(&s, &enc, 0, encode_LBRR, condCoding);
    ec_enc_done(&enc);
    return (int)enc.offs;
}

int c_encode_silk_pulses(int signalType, int quantOffsetType,
                    opus_int8 *pulses, int frame_length,
                    unsigned char *buf, int bufSize) {
    ec_enc enc;
    ec_enc_init(&enc, buf, bufSize);
    silk_encode_pulses(&enc, signalType, quantOffsetType, pulses, frame_length);
    ec_enc_done(&enc);
    return (int)enc.offs;
}
*/
import "C"
import "unsafe"

func cProcessNLSFs(wb bool, speechAct, useInterp, interpCoef, sigType, nbSubfr, survivors int,
	nlsf, prev []int16) (predA, predB, nlsfOut []int16, indices []int8) {
	w := 0
	if wb {
		w = 1
	}
	nlsfLocal := make([]int16, len(nlsf))
	copy(nlsfLocal, nlsf)
	prevLocal := make([]int16, 16)
	for i := 0; i < len(prev) && i < 16; i++ {
		prevLocal[i] = prev[i]
	}
	predA = make([]int16, 16)
	predB = make([]int16, 16)
	indices = make([]int8, 17)
	C.c_process_NLSFs(C.int(w), C.int(speechAct), C.int(useInterp),
		C.int(interpCoef), C.int(sigType), C.int(nbSubfr), C.int(survivors),
		(*C.opus_int16)(unsafe.Pointer(&nlsfLocal[0])),
		(*C.opus_int16)(unsafe.Pointer(&prevLocal[0])),
		(*C.opus_int16)(unsafe.Pointer(&predA[0])),
		(*C.opus_int16)(unsafe.Pointer(&predB[0])),
		(*C.opus_int8)(unsafe.Pointer(&indices[0])))
	nlsfOut = nlsfLocal
	return
}

type cStereoState struct {
	PredPrev0, PredPrev1 int16
	SMid0, SMid1         int16
	SSide0, SSide1       int16
	MidSideAmp           [4]int32
	SmthWidth, WidthPrev int16
	SilentSideLen        int16
}

func cStereoLRToMS(x1Buf, x2Buf []int16, totalRate int32, prevSAQ8, toMono, fs_kHz, frame_length int,
	st cStereoState) (x1, x2 []int16, ix [2][3]int8, midOnly int8, midRate, sideRate int32, outSt cStereoState) {
	x1 = append([]int16(nil), x1Buf...)
	x2 = append([]int16(nil), x2Buf...)
	var ixFlat [6]C.opus_int8
	var moRaw C.opus_int8
	var mr, sr C.opus_int32
	var pp0, pp1 C.opus_int16
	var sm0, sm1, ss0, ss1 C.opus_int16
	var amp [4]C.opus_int32
	var smW, wP, ssl C.opus_int16
	inAmp := [4]C.opus_int32{
		C.opus_int32(st.MidSideAmp[0]), C.opus_int32(st.MidSideAmp[1]),
		C.opus_int32(st.MidSideAmp[2]), C.opus_int32(st.MidSideAmp[3]),
	}
	C.c_stereo_LR_to_MS(
		(*C.opus_int16)(unsafe.Pointer(&x1[0])),
		(*C.opus_int16)(unsafe.Pointer(&x2[0])),
		C.opus_int32(totalRate), C.int(prevSAQ8),
		C.int(toMono), C.int(fs_kHz), C.int(frame_length),
		C.opus_int16(st.PredPrev0), C.opus_int16(st.PredPrev1),
		C.opus_int16(st.SMid0), C.opus_int16(st.SMid1),
		C.opus_int16(st.SSide0), C.opus_int16(st.SSide1),
		&inAmp[0],
		C.opus_int16(st.SmthWidth), C.opus_int16(st.WidthPrev), C.opus_int16(st.SilentSideLen),
		&ixFlat[0], &moRaw, &mr, &sr,
		&pp0, &pp1, &sm0, &sm1, &ss0, &ss1, &amp[0], &smW, &wP, &ssl,
	)
	for i := 0; i < 2; i++ {
		for j := 0; j < 3; j++ {
			ix[i][j] = int8(ixFlat[i*3+j])
		}
	}
	midOnly = int8(moRaw)
	midRate = int32(mr)
	sideRate = int32(sr)
	outSt.PredPrev0 = int16(pp0)
	outSt.PredPrev1 = int16(pp1)
	outSt.SMid0 = int16(sm0)
	outSt.SMid1 = int16(sm1)
	outSt.SSide0 = int16(ss0)
	outSt.SSide1 = int16(ss1)
	for i := 0; i < 4; i++ {
		outSt.MidSideAmp[i] = int32(amp[i])
	}
	outSt.SmthWidth = int16(smW)
	outSt.WidthPrev = int16(wP)
	outSt.SilentSideLen = int16(ssl)
	return
}

func cEncodeIndices(wb bool, nb_subfr, fs_kHz, sigType, quantOff int,
	gainsIdx, NLSFIdx []int8, lagIndex, contourIdx, interpCoef, PERIndex int,
	LTPIdx []int8, LTPScale, seed, ecPrevST, ecPrevLag, encodeLBRR, condCoding, bufSize int) []byte {
	w := 0
	if wb {
		w = 1
	}
	g := [4]C.opus_int8{}
	for i := 0; i < len(gainsIdx) && i < 4; i++ {
		g[i] = C.opus_int8(gainsIdx[i])
	}
	n := [17]C.opus_int8{}
	for i := 0; i < len(NLSFIdx) && i < 17; i++ {
		n[i] = C.opus_int8(NLSFIdx[i])
	}
	l := [4]C.opus_int8{}
	for i := 0; i < len(LTPIdx) && i < 4; i++ {
		l[i] = C.opus_int8(LTPIdx[i])
	}
	buf := make([]byte, bufSize)
	sz := C.c_encode_indices(C.int(w), C.int(nb_subfr), C.int(fs_kHz),
		C.int(sigType), C.int(quantOff),
		&g[0], &n[0], C.int(lagIndex), C.int(contourIdx), C.int(interpCoef),
		C.int(PERIndex), &l[0], C.int(LTPScale), C.int(seed),
		C.int(ecPrevST), C.int(ecPrevLag), C.int(encodeLBRR), C.int(condCoding),
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufSize))
	return buf[:int(sz)]
}

func cSilkEncodePulses(signalType, quantOffsetType int, pulses []int8, frame_length, bufSize int) []byte {
	p := make([]int8, frame_length+16)
	for i := 0; i < len(pulses) && i < frame_length; i++ {
		p[i] = pulses[i]
	}
	buf := make([]byte, bufSize)
	sz := C.c_encode_silk_pulses(C.int(signalType), C.int(quantOffsetType),
		(*C.opus_int8)(unsafe.Pointer(&p[0])), C.int(frame_length),
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufSize))
	return buf[:int(sz)]
}
