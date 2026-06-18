//go:build cgo

package benchcmp

/*
#include "config.h"
#include "celt.h"
#include "arch.h"
#include "modes.h"
#include "opus_private.h"
#include "SigProc_FIX.h"
#include <string.h>

// silk_biquad_res / hp_cutoff / dc_reject / stereo_fade / gain_fade are
// file-static inside opus_encoder.c, so we cannot call them directly
// from Cgo. Re-implement them here using the same logic — bodies are
// copied verbatim from libopus/src/opus_encoder.c — so the bit pattern
// remains the reference. These are test-only wrappers; the production
// package uses the vendored versions unchanged.

static void c_silk_biquad_res(
    const opus_res *in, const opus_int32 *B_Q28, const opus_int32 *A_Q28,
    opus_val32 *S, opus_res *out, const opus_int32 len, int stride) {
    opus_int   k;
    opus_val32 vout;
    opus_val32 inval;
    opus_val32 A[2], B[3];

    A[0] = (opus_val32)(A_Q28[0] * (1.f/((opus_int32)1<<28)));
    A[1] = (opus_val32)(A_Q28[1] * (1.f/((opus_int32)1<<28)));
    B[0] = (opus_val32)(B_Q28[0] * (1.f/((opus_int32)1<<28)));
    B[1] = (opus_val32)(B_Q28[1] * (1.f/((opus_int32)1<<28)));
    B[2] = (opus_val32)(B_Q28[2] * (1.f/((opus_int32)1<<28)));

    for( k = 0; k < len; k++ ) {
        inval = in[ k*stride ];
        vout = S[ 0 ] + B[0]*inval;
        S[ 0 ] = S[1] - vout*A[0] + B[1]*inval;
        S[ 1 ] = - vout*A[1] + B[2]*inval + VERY_SMALL;
        out[ k*stride ] = vout;
    }
}

static void c_hp_cutoff(
    const opus_res *in, opus_int32 cutoff_Hz, opus_res *out,
    opus_val32 *hp_mem, int len, int channels, opus_int32 Fs) {
    opus_int32 B_Q28[ 3 ], A_Q28[ 2 ];
    opus_int32 Fc_Q19, r_Q28, r_Q22;

    Fc_Q19 = silk_DIV32_16( silk_SMULBB( SILK_FIX_CONST( 1.5 * 3.14159 / 1000, 19 ), cutoff_Hz ), Fs/1000 );
    r_Q28 = SILK_FIX_CONST( 1.0, 28 ) - silk_MUL( SILK_FIX_CONST( 0.92, 9 ), Fc_Q19 );

    B_Q28[ 0 ] = r_Q28;
    B_Q28[ 1 ] = silk_LSHIFT( -r_Q28, 1 );
    B_Q28[ 2 ] = r_Q28;

    r_Q22  = silk_RSHIFT( r_Q28, 6 );
    A_Q28[ 0 ] = silk_SMULWW( r_Q22, silk_SMULWW( Fc_Q19, Fc_Q19 ) - SILK_FIX_CONST( 2.0,  22 ) );
    A_Q28[ 1 ] = silk_SMULWW( r_Q22, r_Q22 );

    c_silk_biquad_res( in, B_Q28, A_Q28, hp_mem, out, len, channels );
    if( channels == 2 ) {
        c_silk_biquad_res( in+1, B_Q28, A_Q28, hp_mem+2, out+1, len, channels );
    }
}

static void c_dc_reject(
    const opus_val16 *in, opus_int32 cutoff_Hz, opus_val16 *out,
    opus_val32 *hp_mem, int len, int channels, opus_int32 Fs) {
    int i;
    float coef, coef2;
    coef = 6.3f*cutoff_Hz/Fs;
    coef2 = 1-coef;
    if (channels==2) {
        float m0, m2;
        m0 = hp_mem[0];
        m2 = hp_mem[2];
        for (i=0;i<len;i++) {
            opus_val32 x0, x1, out0, out1;
            x0 = in[2*i+0];
            x1 = in[2*i+1];
            out0 = x0-m0;
            out1 = x1-m2;
            m0 = coef*x0 + VERY_SMALL + coef2*m0;
            m2 = coef*x1 + VERY_SMALL + coef2*m2;
            out[2*i+0] = out0;
            out[2*i+1] = out1;
        }
        hp_mem[0] = m0;
        hp_mem[2] = m2;
    } else {
        float m0;
        m0 = hp_mem[0];
        for (i=0;i<len;i++) {
            opus_val32 x, y;
            x = in[i];
            y = x-m0;
            m0 = coef*x + VERY_SMALL + coef2*m0;
            out[i] = y;
        }
        hp_mem[0] = m0;
    }
}

static void c_stereo_fade(
    const opus_res *in, opus_res *out, opus_val16 g1, opus_val16 g2,
    int overlap48, int frame_size, int channels, const celt_coef *window, opus_int32 Fs) {
    int i;
    int overlap;
    int inc;
    inc = IMAX(1, 48000/Fs);
    overlap=overlap48/inc;
    g1 = Q15ONE-g1;
    g2 = Q15ONE-g2;
    for (i=0;i<overlap;i++) {
        opus_val32 diff;
        opus_val16 g, w;
        w = COEF2VAL16(window[i*inc]);
        w = MULT16_16_Q15(w, w);
        g = SHR32(MAC16_16(MULT16_16(w,g2),
              Q15ONE-w, g1), 15);
        diff = HALF32((opus_val32)in[i*channels] - (opus_val32)in[i*channels+1]);
        diff = MULT16_RES_Q15(g, diff);
        out[i*channels] = out[i*channels] - diff;
        out[i*channels+1] = out[i*channels+1] + diff;
    }
    for (;i<frame_size;i++) {
        opus_val32 diff;
        diff = HALF32((opus_val32)in[i*channels] - (opus_val32)in[i*channels+1]);
        diff = MULT16_RES_Q15(g2, diff);
        out[i*channels] = out[i*channels] - diff;
        out[i*channels+1] = out[i*channels+1] + diff;
    }
}

static void c_gain_fade(
    const opus_res *in, opus_res *out, opus_val16 g1, opus_val16 g2,
    int overlap48, int frame_size, int channels, const celt_coef *window, opus_int32 Fs) {
    int i;
    int inc;
    int overlap;
    int c;
    inc = IMAX(1, 48000/Fs);
    overlap=overlap48/inc;
    if (channels==1) {
        for (i=0;i<overlap;i++) {
            opus_val16 g, w;
            w = COEF2VAL16(window[i*inc]);
            w = MULT16_16_Q15(w, w);
            g = SHR32(MAC16_16(MULT16_16(w,g2),
                  Q15ONE-w, g1), 15);
            out[i] = MULT16_RES_Q15(g, in[i]);
        }
    } else {
        for (i=0;i<overlap;i++) {
            opus_val16 g, w;
            w = COEF2VAL16(window[i*inc]);
            w = MULT16_16_Q15(w, w);
            g = SHR32(MAC16_16(MULT16_16(w,g2),
                  Q15ONE-w, g1), 15);
            out[i*2] = MULT16_RES_Q15(g, in[i*2]);
            out[i*2+1] = MULT16_RES_Q15(g, in[i*2+1]);
        }
    }
    c=0; do {
        for (i=overlap;i<frame_size;i++) {
            out[i*channels+c] = MULT16_RES_Q15(g2, in[i*channels+c]);
        }
    } while (++c<channels);
}

// StereoWidthState is defined inside opus_encoder.c (not a header).
// Redeclare it locally with the identical field layout so we can call
// compute_stereo_width via a forward-declared prototype.
typedef struct {
   opus_val32 XX, XY, YY;
   opus_val16 smoothed_width;
   opus_val16 max_follower;
} StereoWidthState_t;

extern opus_val16 compute_stereo_width(const opus_res *pcm, int frame_size,
    opus_int32 Fs, StereoWidthState_t *mem);

static opus_val16 c_compute_stereo_width(
    const opus_res *pcm, int frame_size, opus_int32 Fs,
    opus_val32 *XX, opus_val32 *XY, opus_val32 *YY,
    opus_val16 *smoothed, opus_val16 *max_follower) {
    StereoWidthState_t mem;
    mem.XX = *XX;
    mem.XY = *XY;
    mem.YY = *YY;
    mem.smoothed_width = *smoothed;
    mem.max_follower = *max_follower;
    opus_val16 r = compute_stereo_width(pcm, frame_size, Fs, &mem);
    *XX = mem.XX;
    *XY = mem.XY;
    *YY = mem.YY;
    *smoothed = mem.smoothed_width;
    *max_follower = mem.max_follower;
    return r;
}
*/
import "C"
import "unsafe"

func cHpCutoff(in_ []float32, cutoff int32, hp_mem []float32, length, channels int, Fs int32) (out []float32, memOut []float32) {
	out = make([]float32, len(in_))
	mem := make([]float32, 4)
	copy(mem, hp_mem)
	C.c_hp_cutoff(
		(*C.opus_res)(unsafe.Pointer(&in_[0])),
		C.opus_int32(cutoff),
		(*C.opus_res)(unsafe.Pointer(&out[0])),
		(*C.opus_val32)(unsafe.Pointer(&mem[0])),
		C.int(length), C.int(channels), C.opus_int32(Fs))
	return out, mem
}

func cDcReject(in_ []float32, cutoff int32, hp_mem []float32, length, channels int, Fs int32) (out []float32, memOut []float32) {
	out = make([]float32, len(in_))
	mem := make([]float32, 4)
	copy(mem, hp_mem)
	C.c_dc_reject(
		(*C.opus_val16)(unsafe.Pointer(&in_[0])),
		C.opus_int32(cutoff),
		(*C.opus_val16)(unsafe.Pointer(&out[0])),
		(*C.opus_val32)(unsafe.Pointer(&mem[0])),
		C.int(length), C.int(channels), C.opus_int32(Fs))
	return out, mem
}

func cStereoFade(in_ []float32, g1, g2 float32, overlap48, frame_size, channels int, window []float32, Fs int32) (out []float32) {
	out = append([]float32(nil), in_...)
	C.c_stereo_fade(
		(*C.opus_res)(unsafe.Pointer(&in_[0])),
		(*C.opus_res)(unsafe.Pointer(&out[0])),
		C.opus_val16(g1), C.opus_val16(g2),
		C.int(overlap48), C.int(frame_size), C.int(channels),
		(*C.celt_coef)(unsafe.Pointer(&window[0])),
		C.opus_int32(Fs))
	return out
}

func cGainFade(in_ []float32, g1, g2 float32, overlap48, frame_size, channels int, window []float32, Fs int32) (out []float32) {
	out = make([]float32, len(in_))
	C.c_gain_fade(
		(*C.opus_res)(unsafe.Pointer(&in_[0])),
		(*C.opus_res)(unsafe.Pointer(&out[0])),
		C.opus_val16(g1), C.opus_val16(g2),
		C.int(overlap48), C.int(frame_size), C.int(channels),
		(*C.celt_coef)(unsafe.Pointer(&window[0])),
		C.opus_int32(Fs))
	return out
}

func cComputeStereoWidth(pcm []float32, frame_size int, Fs int32,
	XX, XY, YY, Smoothed, Max float32,
) (ret float32, xxOut, xyOut, yyOut, smoothedOut, maxOut float32) {
	cXX := C.opus_val32(XX)
	cXY := C.opus_val32(XY)
	cYY := C.opus_val32(YY)
	cSm := C.opus_val16(Smoothed)
	cMax := C.opus_val16(Max)
	r := C.c_compute_stereo_width(
		(*C.opus_res)(unsafe.Pointer(&pcm[0])),
		C.int(frame_size), C.opus_int32(Fs),
		&cXX, &cXY, &cYY, &cSm, &cMax)
	return float32(r), float32(cXX), float32(cXY), float32(cYY), float32(cSm), float32(cMax)
}
