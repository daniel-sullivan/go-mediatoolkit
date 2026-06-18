//go:build cgo

package benchcmp

/*
// See parity_kiss_fft_cgo.go for why we replicate alloc helpers
// rather than enabling CUSTOM_MODES globally.
#include "config.h"
#include "kiss_fft.h"
#include "mdct.h"
#include <math.h>
#include <stdlib.h>
#include <string.h>

// Forward-decl to let this TU share the fft alloc from parity_kiss_fft_cgo.
// Redeclare locally since each Cgo TU compiles independently.
static void mdct_compute_bitrev(int Fout, opus_int16 *f, size_t fstride,
    int in_stride, opus_int16 *factors) {
    int p = *factors++;
    int m = *factors++;
    if (m == 1) {
        for (int j = 0; j < p; j++) { *f = Fout+j; f += fstride*in_stride; }
    } else {
        for (int j = 0; j < p; j++) {
            mdct_compute_bitrev(Fout, f, fstride*p, in_stride, factors);
            f += fstride*in_stride;
            Fout += m;
        }
    }
}

static int mdct_kf_factor(int n, opus_int16 *facbuf) {
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

static kiss_fft_state* mdct_fft_alloc_base(int nfft) {
    kiss_fft_state *st = (kiss_fft_state*)calloc(1, sizeof(kiss_fft_state));
    st->nfft = nfft;
    st->scale = 1.f / nfft;
    st->shift = -1;
    kiss_twiddle_cpx *tw = (kiss_twiddle_cpx*)malloc(sizeof(kiss_twiddle_cpx)*nfft);
    for (int i = 0; i < nfft; i++) {
        const double pi = 3.14159265358979323846264338327;
        double phase = (-2*pi/nfft) * i;
        tw[i].r = (float)cos(phase);
        tw[i].i = (float)sin(phase);
    }
    st->twiddles = tw;
    if (!mdct_kf_factor(nfft, st->factors)) { free(tw); free(st); return NULL; }
    opus_int16 *br = (opus_int16*)malloc(sizeof(opus_int16)*nfft);
    mdct_compute_bitrev(0, br, 1, 1, st->factors);
    st->bitrev = br;
    return st;
}

static kiss_fft_state* mdct_fft_alloc_sub(int nfft, const kiss_fft_state *base) {
    kiss_fft_state *st = (kiss_fft_state*)calloc(1, sizeof(kiss_fft_state));
    st->nfft = nfft;
    st->scale = 1.f / nfft;
    st->shift = 0;
    while (st->shift < 32 && (nfft << st->shift) != base->nfft) st->shift++;
    if (st->shift >= 32) { free(st); return NULL; }
    st->twiddles = base->twiddles;
    if (!mdct_kf_factor(nfft, st->factors)) { free(st); return NULL; }
    opus_int16 *br = (opus_int16*)malloc(sizeof(opus_int16)*nfft);
    mdct_compute_bitrev(0, br, 1, 1, st->factors);
    st->bitrev = br;
    return st;
}

static void mdct_fft_free(kiss_fft_state *st, int is_base) {
    if (!st) return;
    free((void*)st->bitrev);
    if (is_base) free((void*)st->twiddles);
    free(st);
}

typedef struct {
    mdct_lookup l;
} mdct_test_wrap;

static mdct_test_wrap* test_mdct_alloc(int N, int maxshift) {
    mdct_test_wrap *w = (mdct_test_wrap*)calloc(1, sizeof(*w));
    int N2 = N >> 1;
    w->l.n = N;
    w->l.maxshift = maxshift;
    for (int i = 0; i <= maxshift; i++) {
        if (i == 0)
            w->l.kfft[i] = mdct_fft_alloc_base(N>>2>>i);
        else
            w->l.kfft[i] = mdct_fft_alloc_sub(N>>2>>i, w->l.kfft[0]);
    }
    int total = N - (N2 >> maxshift);
    kiss_twiddle_scalar *trig = (kiss_twiddle_scalar*)malloc(sizeof(*trig)*total);
    w->l.trig = trig;
    int off = 0;
    int curN = N, curN2 = N2;
    for (int shift = 0; shift <= maxshift; shift++) {
        for (int i = 0; i < curN2; i++) {
            double phase = 2*M_PI*(i + 0.125)/curN;
            trig[off+i] = (float)cos(phase);
        }
        off += curN2;
        curN2 >>= 1;
        curN >>= 1;
    }
    return w;
}

static void test_mdct_free(mdct_test_wrap *w) {
    if (!w) return;
    for (int i = 0; i <= w->l.maxshift; i++)
        mdct_fft_free((kiss_fft_state*)w->l.kfft[i], i == 0);
    free((void*)w->l.trig);
    free(w);
}

static void c_mdct_forward(mdct_test_wrap *w, float *in, float *out,
    const float *window, int overlap, int shift, int stride) {
    clt_mdct_forward_c(&w->l, in, out, window, overlap, shift, stride, 0);
}
static void c_mdct_backward(mdct_test_wrap *w, float *in, float *out,
    const float *window, int overlap, int shift, int stride) {
    clt_mdct_backward_c(&w->l, in, out, window, overlap, shift, stride, 0);
}

// Accessors for state mirroring into Go.
static int   mdct_nfft(mdct_test_wrap *w, int s)   { return w->l.kfft[s]->nfft; }
static float mdct_scale(mdct_test_wrap *w, int s)  { return w->l.kfft[s]->scale; }
static int   mdct_shift(mdct_test_wrap *w, int s)  { return w->l.kfft[s]->shift; }
static short mdct_factor(mdct_test_wrap *w, int s, int i) { return w->l.kfft[s]->factors[i]; }
static short mdct_bitrev(mdct_test_wrap *w, int s, int i) { return w->l.kfft[s]->bitrev[i]; }
static float mdct_twr(mdct_test_wrap *w, int s, int i)    { return w->l.kfft[s]->twiddles[i].r; }
static float mdct_twi(mdct_test_wrap *w, int s, int i)    { return w->l.kfft[s]->twiddles[i].i; }
static float mdct_trig(mdct_test_wrap *w, int i)          { return w->l.trig[i]; }
*/
import "C"
import "unsafe"

type cMdct struct{ w *C.mdct_test_wrap }

func cMdctAlloc(N, maxshift int) cMdct {
	return cMdct{w: C.test_mdct_alloc(C.int(N), C.int(maxshift))}
}
func (m cMdct) Free() { C.test_mdct_free(m.w) }

func (m cMdct) Nfft(s int) int { return int(C.mdct_nfft(m.w, C.int(s))) }
func (m cMdct) Scale(s int) float32 {
	return float32(C.mdct_scale(m.w, C.int(s)))
}
func (m cMdct) Shift(s int) int { return int(C.mdct_shift(m.w, C.int(s))) }
func (m cMdct) Factor(s, i int) int16 {
	return int16(C.mdct_factor(m.w, C.int(s), C.int(i)))
}
func (m cMdct) Bitrev(s, i int) int16 {
	return int16(C.mdct_bitrev(m.w, C.int(s), C.int(i)))
}
func (m cMdct) Twr(s, i int) float32 {
	return float32(C.mdct_twr(m.w, C.int(s), C.int(i)))
}
func (m cMdct) Twi(s, i int) float32 {
	return float32(C.mdct_twi(m.w, C.int(s), C.int(i)))
}
func (m cMdct) Trig(i int) float32 {
	return float32(C.mdct_trig(m.w, C.int(i)))
}

func (m cMdct) Forward(in, out, window []float32, overlap, shift, stride int) {
	C.c_mdct_forward(m.w,
		(*C.float)(unsafe.Pointer(&in[0])),
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&window[0])),
		C.int(overlap), C.int(shift), C.int(stride))
}
func (m cMdct) Backward(in, out, window []float32, overlap, shift, stride int) {
	C.c_mdct_backward(m.w,
		(*C.float)(unsafe.Pointer(&in[0])),
		(*C.float)(unsafe.Pointer(&out[0])),
		(*C.float)(unsafe.Pointer(&window[0])),
		C.int(overlap), C.int(shift), C.int(stride))
}
