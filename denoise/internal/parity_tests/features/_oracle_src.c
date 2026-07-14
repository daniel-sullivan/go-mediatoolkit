/* Oracle TU for the features parity slice — see rnncgo/librnnoise_units.h.
 * rnn_compute_frame_features is non-static (denoise.h). We drive a real
 * DenoiseState (rnnoise_create(NULL) — init_rnnoise is the non-neural
 * stub, which is fine because compute_frame_features never runs the
 * network). */
#include "librnnoise_units.h"

void *fparity_create_state(void) {
	return (void *)rnnoise_create(NULL);
}

void fparity_destroy_state(void *st) {
	rnnoise_destroy((DenoiseState *)st);
}

int fparity_compute_features(void *st, float *features, float *Ex, float *Ep,
                             float *Exp, const float *in) {
	kiss_fft_cpx X[FREQ_SIZE];
	kiss_fft_cpx P[FREQ_SIZE];
	return rnn_compute_frame_features((DenoiseState *)st, X, P, Ex, Ep, Exp, features, in);
}
