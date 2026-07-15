/* Oracle TU for the biquad parity slice — see rnncgo/librnnoise_units.h. */
#include "librnnoise_units.h"

void fparity_biquad(float *y, float *mem, const float *x,
                    const float *b, const float *a, int n) {
	rnn_biquad(y, mem, x, b, a, n);
}
