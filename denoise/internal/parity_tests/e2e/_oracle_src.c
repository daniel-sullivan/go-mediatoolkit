/* Oracle TU for the end-to-end parity slice: the full
 * rnnoise_process_frame (rnnoise.h, non-static) with real weights. */
#define RNNCGO_WITH_WEIGHTS
#include "librnnoise_units.h"

void *fparity_e2e_create(void) {
	return (void *)rnnoise_create(NULL);
}

void fparity_e2e_destroy(void *st) {
	rnnoise_destroy((DenoiseState *)st);
}

float fparity_e2e_frame(void *st, float *out, const float *in) {
	return rnnoise_process_frame((DenoiseState *)st, out, in);
}
