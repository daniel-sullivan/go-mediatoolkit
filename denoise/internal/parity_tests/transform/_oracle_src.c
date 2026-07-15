/* Oracle TU for the transform parity slice — see rnncgo/librnnoise_units.h.
 * forward_transform / inverse_transform / apply_window are static in
 * denoise.c, which this TU #includes, so the accessors can call them. */
#include "librnnoise_units.h"

#define FP_FREQ_SIZE (FRAME_SIZE + 1)

void fparity_forward_transform(float *outR, float *outI, const float *in) {
	kiss_fft_cpx X[FP_FREQ_SIZE];
	forward_transform(X, in);
	for (int i = 0; i < FP_FREQ_SIZE; i++) {
		outR[i] = X[i].r;
		outI[i] = X[i].i;
	}
}

void fparity_inverse_transform(float *out, const float *inR, const float *inI) {
	kiss_fft_cpx in[FP_FREQ_SIZE];
	for (int i = 0; i < FP_FREQ_SIZE; i++) {
		in[i].r = inR[i];
		in[i].i = inI[i];
	}
	inverse_transform(out, in);
}

void fparity_apply_window(float *x) {
	apply_window(x);
}
