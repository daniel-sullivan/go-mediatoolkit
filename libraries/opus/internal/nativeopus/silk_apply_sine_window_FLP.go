package nativeopus

// 1:1 port of libopus/silk/float/apply_sine_window_FLP.c.
// Apply a sine window to a signal vector.
// win_type 1: sine from 0..pi/2    win_type 2: sine from pi/2..pi.

// silkPI_FLP mirrors C's `#define PI (3.1415926536f)` in SigProc_FLP.h.
const silkPI_FLP silk_float = 3.1415926536

func silk_apply_sine_window_FLP(px_win, px []silk_float, win_type, length opus_int) {
	var S0, S1 silk_float

	freq := silkPI_FLP / silk_float(length+1)
	// Approximation of 2*cos(f). C: c = 2.0f - freq*freq.
	c := fma_sub(2.0, freq, freq)

	if win_type < 2 {
		S0 = 0.0
		// Approximation of sin(f).
		S1 = freq
	} else {
		S0 = 1.0
		// Approximation of cos(f). 0.5f * c — single mul, no FMA concern.
		S1 = 0.5 * c
	}

	// Uses recurrence sin(n*f) = 2*cos(f)*sin((n-1)*f) - sin((n-2)*f).
	// Four samples per iteration.
	for k := opus_int(0); k < length; k += 4 {
		// px_win[k+0] = px[k+0] * 0.5f * (S0 + S1)  — left-to-right.
		px_win[k+0] = px[k+0] * 0.5 * (S0 + S1)
		px_win[k+1] = px[k+1] * S1
		// C: S0 = c * S1 - S0  ==  (c*S1) - S0  -> fma_rsub(S0, c, S1).
		S0 = fma_rsub(S0, c, S1)
		px_win[k+2] = px[k+2] * 0.5 * (S1 + S0)
		px_win[k+3] = px[k+3] * S0
		// C: S1 = c * S0 - S1.
		S1 = fma_rsub(S1, c, S0)
	}
}
