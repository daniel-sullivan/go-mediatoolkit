//go:build cgo

package benchcmp

/*
// Production libopus builds use static_modes_float.h tables for
// kiss_fft_state, and the alloc helpers (opus_fft_alloc,
// compute_twiddles, kf_factor, compute_bitrev_table) are all gated
// on CUSTOM_MODES which our build does not define. Defining
// CUSTOM_MODES globally pulls in headers/prototypes from other
// files (celt_encoder.c wants opus_custom_encoder_init, etc.) and
// breaks the build. So for tests we replicate the tiny subset of
// alloc logic we need directly in this TU under a private namespace,
// invoking the butterfly / entry-point functions from the vendored
// source unchanged.
#include "config.h"
#include "kiss_fft.h"
#include <math.h>
#include <stdlib.h>
#include <string.h>

// Replicas of kiss_fft.c's static CUSTOM_MODES helpers.
static void test_compute_bitrev_table(int Fout, opus_int16 *f, size_t fstride,
    int in_stride, opus_int16 *factors) {
    int p = *factors++;
    int m = *factors++;
    if (m == 1) {
        for (int j = 0; j < p; j++) { *f = Fout+j; f += fstride*in_stride; }
    } else {
        for (int j = 0; j < p; j++) {
            test_compute_bitrev_table(Fout, f, fstride*p, in_stride, factors);
            f += fstride*in_stride;
            Fout += m;
        }
    }
}

static int test_kf_factor(int n, opus_int16 *facbuf) {
    int p = 4;
    int stages = 0;
    int nbak = n;
    do {
        while (n % p) {
            switch (p) { case 4: p = 2; break; case 2: p = 3; break;
                         default: p += 2; break; }
            if (p > 32000 || p*p > n) p = n;
        }
        n /= p;
        if (p > 5) return 0;
        facbuf[2*stages] = p;
        if (p == 2 && stages > 1) { facbuf[2*stages] = 4; facbuf[2] = 2; }
        stages++;
    } while (n > 1);
    n = nbak;
    for (int i = 0; i < stages/2; i++) {
        int tmp = facbuf[2*i];
        facbuf[2*i] = facbuf[2*(stages-i-1)];
        facbuf[2*(stages-i-1)] = tmp;
    }
    for (int i = 0; i < stages; i++) {
        n /= facbuf[2*i];
        facbuf[2*i+1] = n;
    }
    return 1;
}

static void test_compute_twiddles(kiss_twiddle_cpx *twiddles, int nfft) {
    for (int i = 0; i < nfft; i++) {
        const double pi = 3.14159265358979323846264338327;
        double phase = (-2*pi/nfft) * i;
        twiddles[i].r = (float)cos(phase);
        twiddles[i].i = (float)sin(phase);
    }
}

static kiss_fft_state* test_fft_alloc(int nfft) {
    kiss_fft_state *st = (kiss_fft_state*)calloc(1, sizeof(kiss_fft_state));
    st->nfft = nfft;
    st->scale = 1.f / nfft;
    st->shift = -1;
    kiss_twiddle_cpx *tw = (kiss_twiddle_cpx*)malloc(sizeof(kiss_twiddle_cpx)*nfft);
    test_compute_twiddles(tw, nfft);
    st->twiddles = tw;
    if (!test_kf_factor(nfft, st->factors)) { free(tw); free(st); return NULL; }
    opus_int16 *br = (opus_int16*)malloc(sizeof(opus_int16)*nfft);
    test_compute_bitrev_table(0, br, 1, 1, st->factors);
    st->bitrev = br;
    return st;
}

static void test_fft_free(kiss_fft_state *st) {
    if (!st) return;
    free((void*)st->bitrev);
    free((void*)st->twiddles);
    free(st);
}

static int   c_fft_nfft(kiss_fft_state *st)    { return st->nfft; }
static float c_fft_scale(kiss_fft_state *st)   { return st->scale; }
static int   c_fft_shift(kiss_fft_state *st)   { return st->shift; }
static short c_fft_factor(kiss_fft_state *st, int i) { return st->factors[i]; }
static short c_fft_bitrev(kiss_fft_state *st, int i) { return st->bitrev[i]; }
static float c_fft_twr(kiss_fft_state *st, int i)    { return st->twiddles[i].r; }
static float c_fft_twi(kiss_fft_state *st, int i)    { return st->twiddles[i].i; }

static void c_opus_fft(kiss_fft_state *st, const float *in, float *out) {
    opus_fft_c(st, (const kiss_fft_cpx*)in, (kiss_fft_cpx*)out);
}
static void c_opus_ifft(kiss_fft_state *st, const float *in, float *out) {
    opus_ifft_c(st, (const kiss_fft_cpx*)in, (kiss_fft_cpx*)out);
}
*/
import "C"
import "unsafe"

type cFFT struct{ p *C.kiss_fft_state }

func cFFTAlloc(nfft int) cFFT {
	return cFFT{p: C.test_fft_alloc(C.int(nfft))}
}
func (s cFFT) Free()              { C.test_fft_free(s.p) }
func (s cFFT) Nfft() int          { return int(C.c_fft_nfft(s.p)) }
func (s cFFT) Scale() float32     { return float32(C.c_fft_scale(s.p)) }
func (s cFFT) Shift() int         { return int(C.c_fft_shift(s.p)) }
func (s cFFT) Factor(i int) int16 { return int16(C.c_fft_factor(s.p, C.int(i))) }
func (s cFFT) Bitrev(i int) int16 { return int16(C.c_fft_bitrev(s.p, C.int(i))) }
func (s cFFT) Twr(i int) float32  { return float32(C.c_fft_twr(s.p, C.int(i))) }
func (s cFFT) Twi(i int) float32  { return float32(C.c_fft_twi(s.p, C.int(i))) }

func (s cFFT) Fft(in, out []float32) {
	C.c_opus_fft(s.p,
		(*C.float)(unsafe.Pointer(&in[0])),
		(*C.float)(unsafe.Pointer(&out[0])))
}
func (s cFFT) IFft(in, out []float32) {
	C.c_opus_ifft(s.p,
		(*C.float)(unsafe.Pointer(&in[0])),
		(*C.float)(unsafe.Pointer(&out[0])))
}
