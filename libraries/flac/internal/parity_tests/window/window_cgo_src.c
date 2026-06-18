/* Parity harness for libFLAC's window.c apodization generators.
 *
 * window.c's FLAC__window_* functions are NOT static (declared in
 * private/window.h), so this TU simply compiles window.c plus its
 * minimal dependencies (format.c for FLAC__* validators it does not
 * actually use at runtime — window.c only needs math.h, but we keep
 * the include set self-contained so no other parity TU's symbols are
 * required) and dispatches by a typ code matching the Go WindowType
 * enum.
 *
 * Bit-exactness: window.c writes FLAC__real (= float). We hand back
 * the raw float array; the Go test compares math.Float32bits.
 *
 * FP-parity transcendental shim: window.c calls the single-precision
 * libm functions cosf and fabsf, which are neither correctly-rounded
 * nor portable across platforms. We redirect each to its double kernel
 * narrowed to float, so the oracle computes them the same way the Go
 * port does (float32(math.Cos(float64(x))) etc.) and they match
 * bit-for-bit on every platform. <math.h> is included first so its own
 * (un-macroed) cosf/fabsf declarations are processed before the macros
 * rewrite the call sites in window.c's body.
 */

#ifdef HAVE_CONFIG_H
#  include <config.h>
#endif

#include <math.h>
#define cosf(x) ((float)cos((double)(x)))
#define fabsf(x) ((float)fabs((double)(x)))

#include "src/libFLAC/window.c"

#include "_cgo_export.h"
#include <stdint.h>

/* typ codes — must match nativeflac.WindowType iota order. */
#define W_BARTLETT        0
#define W_BARTLETT_HANN   1
#define W_BLACKMAN        2
#define W_BLACKMAN_HARRIS 3
#define W_CONNES          4
#define W_FLATTOP         5
#define W_GAUSS           6
#define W_HAMMING         7
#define W_HANN            8
#define W_KAISER_BESSEL   9
#define W_NUTTALL         10
#define W_RECTANGLE       11
#define W_TRIANGLE        12
#define W_TUKEY           13
#define W_PARTIAL_TUKEY   14
#define W_PUNCHOUT_TUKEY  15
#define W_WELCH           16

void fparity_window(int typ, float *out, int32_t L, float p, float start, float end) {
	switch (typ) {
		case W_BARTLETT:        FLAC__window_bartlett(out, L); break;
		case W_BARTLETT_HANN:   FLAC__window_bartlett_hann(out, L); break;
		case W_BLACKMAN:        FLAC__window_blackman(out, L); break;
		case W_BLACKMAN_HARRIS: FLAC__window_blackman_harris_4term_92db_sidelobe(out, L); break;
		case W_CONNES:          FLAC__window_connes(out, L); break;
		case W_FLATTOP:         FLAC__window_flattop(out, L); break;
		case W_GAUSS:           FLAC__window_gauss(out, L, p); break;
		case W_HAMMING:         FLAC__window_hamming(out, L); break;
		case W_HANN:            FLAC__window_hann(out, L); break;
		case W_KAISER_BESSEL:   FLAC__window_kaiser_bessel(out, L); break;
		case W_NUTTALL:         FLAC__window_nuttall(out, L); break;
		case W_RECTANGLE:       FLAC__window_rectangle(out, L); break;
		case W_TRIANGLE:        FLAC__window_triangle(out, L); break;
		case W_TUKEY:           FLAC__window_tukey(out, L, p); break;
		case W_PARTIAL_TUKEY:   FLAC__window_partial_tukey(out, L, p, start, end); break;
		case W_PUNCHOUT_TUKEY:  FLAC__window_punchout_tukey(out, L, p, start, end); break;
		case W_WELCH:           FLAC__window_welch(out, L); break;
		default:                FLAC__window_hann(out, L); break;
	}
}
