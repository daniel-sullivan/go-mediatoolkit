//go:build cgo

package benchcmp

/*
#include "config.h"
#include "typedef.h"
#include "structs.h"
#include "main.h"
#include "API.h"
#include "control.h"
#include "entdec.h"
#include <string.h>
#include <stdlib.h>

static void c_PLC_Reset(int frame_length, int fs_kHz,
    opus_int32 *pitchL_Q8, opus_int32 *prevGain0, opus_int32 *prevGain1,
    opus_int *subfr_length, opus_int *nb_subfr) {
    silk_decoder_state st;
    memset(&st, 0, sizeof(st));
    st.frame_length = frame_length;
    st.fs_kHz = fs_kHz;
    silk_PLC_Reset(&st);
    *pitchL_Q8 = st.sPLC.pitchL_Q8;
    *prevGain0 = st.sPLC.prevGain_Q16[0];
    *prevGain1 = st.sPLC.prevGain_Q16[1];
    *subfr_length = st.sPLC.subfr_length;
    *nb_subfr = st.sPLC.nb_subfr;
}

static void c_CNG_Reset(int LPC_order, opus_int16 *smthNLSF,
    opus_int32 *smthGain, opus_int32 *randSeed) {
    silk_decoder_state st;
    memset(&st, 0, sizeof(st));
    st.LPC_order = LPC_order;
    silk_CNG_Reset(&st);
    int i;
    for (i = 0; i < LPC_order; i++) smthNLSF[i] = st.sCNG.CNG_smth_NLSF_Q15[i];
    *smthGain = st.sCNG.CNG_smth_Gain_Q16;
    *randSeed = st.sCNG.rand_seed;
}

static void c_init_decoder(opus_int32 *prevGainQ16, opus_int *firstFrame) {
    silk_decoder_state st;
    silk_init_decoder(&st);
    *prevGainQ16 = st.prev_gain_Q16;
    *firstFrame = st.first_frame_after_reset;
}

static int c_decoder_set_fs(int nb_subfr, int fs_kHz, opus_int32 fs_API_Hz,
    opus_int *subfr_length, opus_int *frame_length, opus_int *ltp_mem_length,
    opus_int *LPC_order, opus_int *lagPrev, opus_int *LastGainIndex,
    opus_int *prevSignalType, opus_int *resamplerFn) {
    silk_decoder_state st;
    silk_init_decoder(&st);
    st.nb_subfr = nb_subfr;
    int r = silk_decoder_set_fs(&st, fs_kHz, fs_API_Hz);
    *subfr_length = st.subfr_length;
    *frame_length = st.frame_length;
    *ltp_mem_length = st.ltp_mem_length;
    *LPC_order = st.LPC_order;
    *lagPrev = st.lagPrev;
    *LastGainIndex = st.LastGainIndex;
    *prevSignalType = st.prevSignalType;
    *resamplerFn = st.resampler_state.resampler_function;
    return r;
}

// c_silk_decode drives silk_Decode with the given encoded SILK payload
// and returns the PCM output (as opus_res / float), the final rng, and
// the silk_Decode return code. The caller preallocates pcm with
// nSamplesOut*nChannelsAPI slots.
static int c_silk_decode(const unsigned char *pkt, int pktLen,
    int nChannelsAPI, int nChannelsInternal,
    opus_int32 apiFsHz, opus_int32 internalFsHz,
    int payloadSizeMs, int lostFlag, int firstFrame,
    opus_res *pcm, opus_int32 *nSamplesOut, opus_uint32 *rng) {

    opus_int decSize = 0;
    silk_Get_Decoder_Size(&decSize);
    void *dec = calloc(1, decSize);
    silk_InitDecoder(dec);

    silk_DecControlStruct dc;
    memset(&dc, 0, sizeof(dc));
    dc.nChannelsAPI = nChannelsAPI;
    dc.nChannelsInternal = nChannelsInternal;
    dc.API_sampleRate = apiFsHz;
    dc.internalSampleRate = internalFsHz;
    dc.payloadSize_ms = payloadSizeMs;

    ec_dec ecd;
    ec_dec_init(&ecd, (unsigned char*)pkt, pktLen);

    int r = silk_Decode(dec, &dc, lostFlag, firstFrame, &ecd, pcm, nSamplesOut,
#ifdef ENABLE_DEEP_PLC
        NULL,
#endif
        0);
    *rng = ecd.rng;
    free(dec);
    return r;
}
*/
import "C"
import "unsafe"

func cSilkPLCReset(frameLength, fsKhz int) (pitchL_Q8, prevGain0, prevGain1 int32, subfr, nbSubfr int) {
	var pl, pg0, pg1 C.opus_int32
	var sl, ns C.opus_int
	C.c_PLC_Reset(C.int(frameLength), C.int(fsKhz), &pl, &pg0, &pg1, &sl, &ns)
	return int32(pl), int32(pg0), int32(pg1), int(sl), int(ns)
}

func cSilkCNGReset(LPC_order int) ([]int16, int32, int32) {
	smth := make([]C.opus_int16, LPC_order)
	var gain, seed C.opus_int32
	C.c_CNG_Reset(C.int(LPC_order), (*C.opus_int16)(unsafe.Pointer(&smth[0])), &gain, &seed)
	out := make([]int16, LPC_order)
	for i, v := range smth {
		out[i] = int16(v)
	}
	return out, int32(gain), int32(seed)
}

func cSilkInitDecoder() (prevGainQ16 int32, firstFrame int) {
	var pg C.opus_int32
	var ff C.opus_int
	C.c_init_decoder(&pg, &ff)
	return int32(pg), int(ff)
}

func cSilkDecoderSetFs(nbSubfr, fsKhz int, fsAPIHz int32) (
	ret, subfrLen, frameLen, ltpMemLen, LPCorder, lagPrev, lastGainIndex, prevSignalType, resamplerFn int) {
	var sl, fl, ll, lo, lp, lg, ps, rf C.opus_int
	r := C.c_decoder_set_fs(C.int(nbSubfr), C.int(fsKhz), C.opus_int32(fsAPIHz),
		&sl, &fl, &ll, &lo, &lp, &lg, &ps, &rf)
	return int(r), int(sl), int(fl), int(ll), int(lo), int(lp), int(lg), int(ps), int(rf)
}

func cSilkDecodeFull(packet []byte, nChannelsAPI, nChannelsInternal int,
	apiFsHz, internalFsHz int32, payloadSizeMs, lostFlag, firstFrame int) (
	pcm []float32, rng uint32, ret int) {

	nSamples := int(apiFsHz) * payloadSizeMs / 1000
	out := make([]C.float, nSamples*nChannelsAPI)
	var nso C.opus_int32
	var r uint32
	rr := C.c_silk_decode(
		(*C.uchar)(unsafe.Pointer(&packet[0])),
		C.int(len(packet)),
		C.int(nChannelsAPI), C.int(nChannelsInternal),
		C.opus_int32(apiFsHz), C.opus_int32(internalFsHz),
		C.int(payloadSizeMs), C.int(lostFlag), C.int(firstFrame),
		(*C.float)(unsafe.Pointer(&out[0])), &nso,
		(*C.opus_uint32)(unsafe.Pointer(&r)))
	pcm = make([]float32, len(out))
	for i, v := range out {
		pcm[i] = float32(v)
	}
	return pcm, r, int(rr)
}
