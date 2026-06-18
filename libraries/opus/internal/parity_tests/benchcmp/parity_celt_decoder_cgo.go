//go:build cgo

package benchcmp

/*
#include "config.h"
#include "celt.h"
#include "entenc.h"
#include "entdec.h"
#include "modes.h"
#include "kiss_fft.h"
#include "mdct.h"
#include <stdlib.h>

static void* c_celt_encoder_new(int sampling_rate, int channels) {
    int sz = celt_encoder_get_size(channels);
    CELTEncoder *st = (CELTEncoder*)calloc(1, sz);
    if (celt_encoder_init(st, sampling_rate, channels, 0) != OPUS_OK) {
        free(st); return NULL;
    }
    return st;
}
static void* c_celt_decoder_new(int sampling_rate, int channels) {
    int sz = celt_decoder_get_size(channels);
    CELTDecoder *st = (CELTDecoder*)calloc(1, sz);
    if (celt_decoder_init(st, sampling_rate, channels) != OPUS_OK) {
        free(st); return NULL;
    }
    return st;
}
static void c_free(void *p) { free(p); }

static int c_encode_with_ec(CELTEncoder *enc, const float *pcm, int frame_size,
    unsigned char *compressed, int nb, ec_enc *ec) {
    return celt_encode_with_ec(enc, (const opus_res*)pcm, frame_size, compressed, nb, ec);
}
static int c_decode_with_ec(CELTDecoder *dec, const unsigned char *data, int len,
    float *pcm, int frame_size, ec_dec *ec, int accum) {
    return celt_decode_with_ec(dec, data, len, (opus_res*)pcm, frame_size, ec, accum);
}

static int c_dec_set_start_band(CELTDecoder *dec, int v) {
    return opus_custom_decoder_ctl(dec, CELT_SET_START_BAND_REQUEST, v);
}
static int c_dec_set_end_band(CELTDecoder *dec, int v) {
    return opus_custom_decoder_ctl(dec, CELT_SET_END_BAND_REQUEST, v);
}
static int c_enc_set_start_band(CELTEncoder *enc, int v) {
    return opus_custom_encoder_ctl(enc, CELT_SET_START_BAND_REQUEST, v);
}
static int c_enc_set_end_band(CELTEncoder *enc, int v) {
    return opus_custom_encoder_ctl(enc, CELT_SET_END_BAND_REQUEST, v);
}
static int c_enc_set_bitrate(CELTEncoder *enc, int v) {
    return opus_custom_encoder_ctl(enc, OPUS_SET_BITRATE_REQUEST, v);
}
static int c_enc_set_complexity(CELTEncoder *enc, int v) {
    return opus_custom_encoder_ctl(enc, OPUS_SET_COMPLEXITY_REQUEST, v);
}
static int c_enc_set_signalling(CELTEncoder *enc, int v) {
    return opus_custom_encoder_ctl(enc, CELT_SET_SIGNALLING_REQUEST, v);
}
static int c_dec_set_signalling(CELTDecoder *dec, int v) {
    return opus_custom_decoder_ctl(dec, CELT_SET_SIGNALLING_REQUEST, v);
}

// Mode accessors for mirror: preemph, window, and MDCT sub-state.
static float c_mode_preemph(const CELTMode *m, int i) { return m->preemph[i]; }
static float c_mode_window(const CELTMode *m, int i) { return m->window[i]; }
static int c_mode_mdct_n(const CELTMode *m) { return m->mdct.n; }
static int c_mode_mdct_maxshift(const CELTMode *m) { return m->mdct.maxshift; }
static int c_mode_mdct_trig_len(const CELTMode *m) {
    return m->mdct.n - (m->mdct.n>>1 >> m->mdct.maxshift);
}
static float c_mode_mdct_trig(const CELTMode *m, int i) { return m->mdct.trig[i]; }

static int c_mode_fft_nfft(const CELTMode *m, int s) { return m->mdct.kfft[s]->nfft; }
static float c_mode_fft_scale(const CELTMode *m, int s) { return m->mdct.kfft[s]->scale; }
static int c_mode_fft_shift(const CELTMode *m, int s) { return m->mdct.kfft[s]->shift; }
static short c_mode_fft_factor(const CELTMode *m, int s, int i) { return m->mdct.kfft[s]->factors[i]; }
static short c_mode_fft_bitrev(const CELTMode *m, int s, int i) { return m->mdct.kfft[s]->bitrev[i]; }
static float c_mode_fft_twr(const CELTMode *m, int s, int i) { return m->mdct.kfft[s]->twiddles[i].r; }
static float c_mode_fft_twi(const CELTMode *m, int s, int i) { return m->mdct.kfft[s]->twiddles[i].i; }
*/
import "C"
import "unsafe"

type cCeltEnc struct{ p *C.CELTEncoder }
type cCeltDec struct{ p *C.CELTDecoder }

func cCeltEncoderNew(rate, channels int) cCeltEnc {
	return cCeltEnc{p: (*C.CELTEncoder)(C.c_celt_encoder_new(C.int(rate), C.int(channels)))}
}
func cCeltDecoderNew(rate, channels int) cCeltDec {
	return cCeltDec{p: (*C.CELTDecoder)(C.c_celt_decoder_new(C.int(rate), C.int(channels)))}
}
func (e cCeltEnc) Free() { C.c_free(unsafe.Pointer(e.p)) }
func (d cCeltDec) Free() { C.c_free(unsafe.Pointer(d.p)) }

func (e cCeltEnc) SetStartBand(v int)  { C.c_enc_set_start_band(e.p, C.int(v)) }
func (e cCeltEnc) SetEndBand(v int)    { C.c_enc_set_end_band(e.p, C.int(v)) }
func (e cCeltEnc) SetBitrate(v int)    { C.c_enc_set_bitrate(e.p, C.int(v)) }
func (e cCeltEnc) SetComplexity(v int) { C.c_enc_set_complexity(e.p, C.int(v)) }
func (e cCeltEnc) SetSignalling(v int) { C.c_enc_set_signalling(e.p, C.int(v)) }
func (d cCeltDec) SetStartBand(v int)  { C.c_dec_set_start_band(d.p, C.int(v)) }
func (d cCeltDec) SetEndBand(v int)    { C.c_dec_set_end_band(d.p, C.int(v)) }
func (d cCeltDec) SetSignalling(v int) { C.c_dec_set_signalling(d.p, C.int(v)) }

func (e cCeltEnc) EncodeWithEc(pcm []float32, frameSize int, pkt []byte, ec cEc) int {
	return int(C.c_encode_with_ec(e.p,
		(*C.float)(unsafe.Pointer(&pcm[0])), C.int(frameSize),
		(*C.uchar)(unsafe.Pointer(&pkt[0])), C.int(len(pkt)), ec.p))
}

func (d cCeltDec) DecodeWithEc(data []byte, pcm []float32, frameSize, accum int) int {
	var dataPtr *C.uchar
	if len(data) > 0 {
		dataPtr = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	return int(C.c_decode_with_ec(d.p, dataPtr, C.int(len(data)),
		(*C.float)(unsafe.Pointer(&pcm[0])), C.int(frameSize), nil, C.int(accum)))
}

// cModePreemph returns the 4 preemphasis coefficients.
func cModePreemph(m cMode) [4]float32 {
	var p [4]float32
	for i := 0; i < 4; i++ {
		p[i] = float32(C.c_mode_preemph(m.p, C.int(i)))
	}
	return p
}

// cModeWindow returns the overlap-length window coefficient slice.
func cModeWindow(m cMode) []float32 {
	n := m.Overlap()
	w := make([]float32, n)
	for i := 0; i < n; i++ {
		w[i] = float32(C.c_mode_window(m.p, C.int(i)))
	}
	return w
}

// cModeMdctN / maxshift / trig.
func cModeMdctN(m cMode) int        { return int(C.c_mode_mdct_n(m.p)) }
func cModeMdctMaxshift(m cMode) int { return int(C.c_mode_mdct_maxshift(m.p)) }
func cModeMdctTrig(m cMode) []float32 {
	n := int(C.c_mode_mdct_trig_len(m.p))
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(C.c_mode_mdct_trig(m.p, C.int(i)))
	}
	return out
}

// cModeFftState returns the per-shift FFT state tables.
type cFftStateData struct {
	Nfft     int
	Scale    float32
	Shift    int
	Factors  []int16
	Bitrev   []int16
	TwiddleR []float32
	TwiddleI []float32
}

func cModeFftState(m cMode, shift int) cFftStateData {
	nfft := int(C.c_mode_fft_nfft(m.p, C.int(shift)))
	scale := float32(C.c_mode_fft_scale(m.p, C.int(shift)))
	sh := int(C.c_mode_fft_shift(m.p, C.int(shift)))
	factors := make([]int16, 16)
	for i := 0; i < 16; i++ {
		factors[i] = int16(C.c_mode_fft_factor(m.p, C.int(shift), C.int(i)))
	}
	bitrev := make([]int16, nfft)
	for i := 0; i < nfft; i++ {
		bitrev[i] = int16(C.c_mode_fft_bitrev(m.p, C.int(shift), C.int(i)))
	}
	// The twiddle table of the base FFT state (shift 0) has length nfft.
	// Sub-states share the base's table, so we expose the full length
	// of the base here and reuse it for the sub-shifts.
	baseN := int(C.c_mode_fft_nfft(m.p, 0))
	tr := make([]float32, baseN)
	ti := make([]float32, baseN)
	for i := 0; i < baseN; i++ {
		tr[i] = float32(C.c_mode_fft_twr(m.p, 0, C.int(i)))
		ti[i] = float32(C.c_mode_fft_twi(m.p, 0, C.int(i)))
	}
	return cFftStateData{
		Nfft: nfft, Scale: scale, Shift: sh,
		Factors: factors, Bitrev: bitrev,
		TwiddleR: tr, TwiddleI: ti,
	}
}
