/* Oracle TU for the network (compute_rnn) parity slice. This slice needs
 * the REAL float weights, so it defines RNNCGO_WITH_WEIGHTS to compile
 * rnnoise_data.c (instead of the stub) before including the shared units
 * header. compute_rnn is non-static (rnn.h). */
#define RNNCGO_WITH_WEIGHTS
#include "librnnoise_units.h"

void *fparity_rnn_create(void) {
	return (void *)rnnoise_create(NULL);
}

void fparity_rnn_destroy(void *st) {
	rnnoise_destroy((DenoiseState *)st);
}

void fparity_rnn_step(void *st, float *gains, float *vad, const float *input) {
	DenoiseState *d = (DenoiseState *)st;
	compute_rnn(&d->model, &d->rnn, gains, vad, input, d->arch);
}
