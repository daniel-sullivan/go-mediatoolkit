//go:build cgo

package benchcmp

/*
#include "config.h"
#include "SigProc_FIX.h"
#include "structs.h"
#include "main.h"

static void c_gains_quant(signed char *ind, opus_int32 *gain, signed char *prev,
                          int conditional, int nb) {
    silk_gains_quant(ind, gain, prev, conditional, nb);
}
static void c_gains_dequant(opus_int32 *gain, const signed char *ind, signed char *prev,
                            int conditional, int nb) {
    silk_gains_dequant(gain, ind, prev, conditional, nb);
}
static opus_int32 c_gains_ID(const signed char *ind, int nb) {
    return silk_gains_ID(ind, nb);
}

static void c_decode_pitch(short lagIndex, signed char contourIndex,
                           int *pitch_lags, int Fs_kHz, int nb_subfr) {
    silk_decode_pitch(lagIndex, contourIndex, pitch_lags, Fs_kHz, nb_subfr);
}

// Stereo pred roundtrip: self-contained helper that owns the ec_enc/dec.
static void c_stereo_pred_roundtrip(signed char ix_arr[2][3], unsigned char *buf, int bufSize,
                                    opus_int32 *pred_out) {
    ec_enc enc;
    ec_enc_init(&enc, buf, bufSize);
    silk_stereo_encode_pred(&enc, ix_arr);
    ec_enc_done(&enc);
    ec_dec dec;
    ec_dec_init(&dec, buf, bufSize);
    silk_stereo_decode_pred(&dec, pred_out);
}
static void c_stereo_mid_only_roundtrip(signed char flag, unsigned char *buf, int bufSize,
                                        int *flag_out) {
    ec_enc enc;
    ec_enc_init(&enc, buf, bufSize);
    silk_stereo_encode_mid_only(&enc, flag);
    ec_enc_done(&enc);
    ec_dec dec;
    ec_dec_init(&dec, buf, bufSize);
    silk_stereo_decode_mid_only(&dec, flag_out);
}

static void c_stereo_quant_pred(opus_int32 *pred, signed char ix[2][3]) {
    silk_stereo_quant_pred(pred, ix);
}

static opus_int32 c_stereo_find_predictor(opus_int32 *ratio, const short *x, const short *y,
                                          opus_int32 *mid_res_amp, int length, int smooth) {
    return silk_stereo_find_predictor(ratio, x, y, mid_res_amp, length, smooth);
}

static void c_stereo_MS_to_LR(short *x1, short *x2, const opus_int32 *pred,
                              const short *predPrev, const short *sMid, const short *sSide,
                              short *outPredPrev, short *outSMid, short *outSSide,
                              int fs_kHz, int frame_length) {
    stereo_dec_state st;
    st.pred_prev_Q13[0] = predPrev[0]; st.pred_prev_Q13[1] = predPrev[1];
    st.sMid[0] = sMid[0]; st.sMid[1] = sMid[1];
    st.sSide[0] = sSide[0]; st.sSide[1] = sSide[1];
    silk_stereo_MS_to_LR(&st, x1, x2, pred, fs_kHz, frame_length);
    outPredPrev[0] = st.pred_prev_Q13[0]; outPredPrev[1] = st.pred_prev_Q13[1];
    outSMid[0] = st.sMid[0]; outSMid[1] = st.sMid[1];
    outSSide[0] = st.sSide[0]; outSSide[1] = st.sSide[1];
}
*/
import "C"
import "unsafe"

func cSilkGainsQuant(gain []int32, prev int8, conditional int) ([]int8, []int32, int8) {
	gc := append([]int32(nil), gain...)
	ind := make([]int8, len(gain))
	p := C.schar(prev)
	C.c_gains_quant((*C.schar)(unsafe.Pointer(&ind[0])),
		(*C.opus_int32)(unsafe.Pointer(&gc[0])),
		&p, C.int(conditional), C.int(len(gain)))
	return ind, gc, int8(p)
}
func cSilkGainsDequant(ind []int8, prev int8, conditional int) ([]int32, int8) {
	gain := make([]int32, len(ind))
	p := C.schar(prev)
	C.c_gains_dequant((*C.opus_int32)(unsafe.Pointer(&gain[0])),
		(*C.schar)(unsafe.Pointer(&ind[0])),
		&p, C.int(conditional), C.int(len(ind)))
	return gain, int8(p)
}
func cSilkGainsID(ind []int8) int32 {
	return int32(C.c_gains_ID((*C.schar)(unsafe.Pointer(&ind[0])), C.int(len(ind))))
}

func cSilkDecodePitch(lagIndex int16, contourIndex int8, fsKhz, nbSubfr int) []int {
	lags := make([]C.int, nbSubfr)
	C.c_decode_pitch(C.short(lagIndex), C.schar(contourIndex),
		(*C.int)(unsafe.Pointer(&lags[0])), C.int(fsKhz), C.int(nbSubfr))
	out := make([]int, nbSubfr)
	for i, v := range lags {
		out[i] = int(v)
	}
	return out
}

func cSilkStereoPredRoundtrip(ix [2][3]int8, bufSize int) ([]byte, []int32) {
	buf := make([]byte, bufSize)
	var cix [2][3]C.schar
	for i := 0; i < 2; i++ {
		for j := 0; j < 3; j++ {
			cix[i][j] = C.schar(ix[i][j])
		}
	}
	var pred [2]C.opus_int32
	C.c_stereo_pred_roundtrip(&cix[0],
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufSize),
		(*C.opus_int32)(unsafe.Pointer(&pred[0])))
	return buf, []int32{int32(pred[0]), int32(pred[1])}
}

func cSilkStereoMidOnlyRoundtrip(flag int8, bufSize int) ([]byte, int) {
	buf := make([]byte, bufSize)
	var v C.int
	C.c_stereo_mid_only_roundtrip(C.schar(flag),
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufSize), &v)
	return buf, int(v)
}

func cSilkStereoQuantPred(pred []int32) ([]int32, [2][3]int8) {
	in := append([]int32(nil), pred...)
	var ix [2][3]C.schar
	C.c_stereo_quant_pred(
		(*C.opus_int32)(unsafe.Pointer(&in[0])), &ix[0])
	var out [2][3]int8
	for i := 0; i < 2; i++ {
		for j := 0; j < 3; j++ {
			out[i][j] = int8(ix[i][j])
		}
	}
	return in, out
}

func cSilkStereoFindPredictor(x, y []int16, amp []int32, smooth int) (int32, int32, []int32) {
	a := append([]int32(nil), amp...)
	var ratio C.opus_int32
	p := int32(C.c_stereo_find_predictor(&ratio,
		(*C.short)(unsafe.Pointer(&x[0])),
		(*C.short)(unsafe.Pointer(&y[0])),
		(*C.opus_int32)(unsafe.Pointer(&a[0])),
		C.int(len(x)), C.int(smooth)))
	return p, int32(ratio), a
}

func cSilkStereoMSToLR(pred []int32, x1, x2 []int16, predPrev, sMid, sSide [2]int16, fsKhz, frameLen int) ([]int16, []int16, [2]int16, [2]int16, [2]int16) {
	out1 := append([]int16(nil), x1...)
	out2 := append([]int16(nil), x2...)
	var oPP, oSM, oSS [2]C.short
	C.c_stereo_MS_to_LR(
		(*C.short)(unsafe.Pointer(&out1[0])),
		(*C.short)(unsafe.Pointer(&out2[0])),
		(*C.opus_int32)(unsafe.Pointer(&pred[0])),
		(*C.short)(unsafe.Pointer(&predPrev[0])),
		(*C.short)(unsafe.Pointer(&sMid[0])),
		(*C.short)(unsafe.Pointer(&sSide[0])),
		&oPP[0], &oSM[0], &oSS[0],
		C.int(fsKhz), C.int(frameLen))
	var pp, sm, ss [2]int16
	for i := 0; i < 2; i++ {
		pp[i] = int16(oPP[i])
		sm[i] = int16(oSM[i])
		ss[i] = int16(oSS[i])
	}
	return out1, out2, pp, sm, ss
}
