/* Oracle TU for the bands parity slice — see rnncgo/librnnoise_units.h.
 * compute_band_energy / compute_band_corr / interp_band_gain / dct are
 * static in denoise.c (included here), so the accessors can call them. */
#include "librnnoise_units.h"

#define FP_FREQ (FRAME_SIZE + 1)
#define FP_NB   NB_BANDS

void fparity_band_energy(float *bandE, const float *xr, const float *xi) {
	kiss_fft_cpx X[FP_FREQ];
	for (int i = 0; i < FP_FREQ; i++) { X[i].r = xr[i]; X[i].i = xi[i]; }
	compute_band_energy(bandE, X);
}

void fparity_band_corr(float *bandE, const float *xr, const float *xi,
                       const float *pr, const float *pi) {
	kiss_fft_cpx X[FP_FREQ], P[FP_FREQ];
	for (int i = 0; i < FP_FREQ; i++) {
		X[i].r = xr[i]; X[i].i = xi[i];
		P[i].r = pr[i]; P[i].i = pi[i];
	}
	compute_band_corr(bandE, X, P);
}

void fparity_interp_band_gain(float *g, const float *bandE) {
	interp_band_gain(g, bandE);
}

void fparity_dct(float *out, const float *in) {
	dct(out, in);
}

int fparity_eband(int i) { return eband20ms[i]; }
