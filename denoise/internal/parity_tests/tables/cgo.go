//go:build cgo && rnnoise_strict

// Package tables is the RNNoise Slice-0 parity gate. It compiles the
// vendored rnnoise_tables.c into the test binary (forced onto vec.h's
// generic scalar branch via -DDISABLE_NEON, with -ffp-contract=off,
// both supplied through CGO_CFLAGS by libraries/rnnoise/mise.toml) and
// bit-compares every static table the Go port embeds (tables_gen.go)
// against the C arrays: fft_bitrev, fft_twiddles, rnn_half_window,
// rnn_dct_table, and the rnn_kfft scalar parameters.
//
// It additionally answers the load-bearing question for the whole port:
// does Go's stdlib trig (math.Cos/Sin) reproduce the oracle's libm
// bit-for-bit for the exact 960-pt twiddle phases? The c_live_twiddle_*
// accessors recompute the twiddles at runtime with this machine's libm,
// exactly as RNNoise's compute_twiddles would.
package tables

/*
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/src
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable
#cgo LDFLAGS: -lm

#include <math.h>
#include "kiss_fft.h"
#include "rnnoise_tables.c"

extern const float rnn_half_window[];
extern const float rnn_dct_table[];

static int    c_nfft(void)        { return rnn_kfft.nfft; }
static float  c_scale(void)       { return rnn_kfft.scale; }
static int    c_shift(void)       { return rnn_kfft.shift; }
static short  c_factor(int i)     { return rnn_kfft.factors[i]; }
static int    c_bitrev(int i)     { return rnn_kfft.bitrev[i]; }
static float  c_twiddle_r(int i)  { return rnn_kfft.twiddles[i].r; }
static float  c_twiddle_i(int i)  { return rnn_kfft.twiddles[i].i; }
static float  c_half_window(int i){ return rnn_half_window[i]; }
static float  c_dct(int i)        { return rnn_dct_table[i]; }

// c_live_twiddle_* recompute twiddle i at runtime with this platform's
// libm, mirroring RNNoise's compute_twiddles float branch exactly:
//   pi     = 3.14159265358979323846264338327 (double)
//   phase  = (-2*pi/nfft) * i                 (double)
//   r/i    = (float)cos(phase) / (float)sin(phase)
static float c_live_twiddle_r(int i) {
	const double pi = 3.14159265358979323846264338327;
	double phase = (-2*pi / 960) * i;
	return (float)cos(phase);
}
static float c_live_twiddle_i(int i) {
	const double pi = 3.14159265358979323846264338327;
	double phase = (-2*pi / 960) * i;
	return (float)sin(phase);
}
*/
import "C"

// Scalar accessors bridged to Go.
func cNFFT() int          { return int(C.c_nfft()) }
func cScale() float32     { return float32(C.c_scale()) }
func cShift() int         { return int(C.c_shift()) }
func cFactor(i int) int16 { return int16(C.c_factor(C.int(i))) }
func cBitrev(i int) int32 { return int32(C.c_bitrev(C.int(i))) }

func cTwiddleR(i int) float32   { return float32(C.c_twiddle_r(C.int(i))) }
func cTwiddleI(i int) float32   { return float32(C.c_twiddle_i(C.int(i))) }
func cHalfWindow(i int) float32 { return float32(C.c_half_window(C.int(i))) }
func cDct(i int) float32        { return float32(C.c_dct(C.int(i))) }

func cLiveTwiddleR(i int) float32 { return float32(C.c_live_twiddle_r(C.int(i))) }
func cLiveTwiddleI(i int) float32 { return float32(C.c_live_twiddle_i(C.int(i))) }
