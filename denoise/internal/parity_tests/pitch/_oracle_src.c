/* Oracle TU for the pitch parity slice — see rnncgo/librnnoise_units.h.
 * rnn_pitch_downsample / rnn_pitch_search / rnn_remove_doubling are
 * non-static (declared in pitch.h), so they are called directly. */
#include "librnnoise_units.h"

void fparity_pitch_downsample(float *xlp_out, const float *pitchbuf, int len) {
	celt_sig *pre[1];
	pre[0] = (celt_sig *)pitchbuf;
	rnn_pitch_downsample(pre, xlp_out, len, 1);
}

int fparity_pitch_search(const float *xlp, float *y, int len, int maxpitch) {
	int pitch = 0;
	rnn_pitch_search(xlp, y, len, maxpitch, &pitch);
	return pitch;
}

float fparity_remove_doubling(float *x, int maxperiod, int minperiod, int N,
                              int *T0, int prev_period, float prev_gain) {
	return rnn_remove_doubling(x, maxperiod, minperiod, N, T0, prev_period, prev_gain);
}
