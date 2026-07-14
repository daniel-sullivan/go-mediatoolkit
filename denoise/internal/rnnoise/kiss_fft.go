package rnnoise

import "math"

// FFT types and the static 960-pt state, ported from
// librnnoise/src/kiss_fft.h. RNNoise's float build sets
// kiss_fft_scalar = kiss_twiddle_scalar = float, so both the sample and
// twiddle components are float32. Unlike the opus CELT port, RNNoise's
// bitrev table is opus_int32 (kiss_fft.h: `const opus_int32 *bitrev`).
//
// RNNoise never allocates an FFT at runtime: rnnoise_tables.c ships the
// twiddles, bitrev, and factors as static constants (rnn_kfft), so the
// alloc/compute_twiddles path is not part of the shipping data path. We
// embed those constants (tables_gen.go) and build the state below,
// exactly as the C `const kiss_fft_state rnn_kfft` does.

const maxFactors = 8

// fftCpx is kiss_fft_cpx — a complex sample (float r/i).
type fftCpx struct {
	r, i float32
}

// twiddleCpx is kiss_twiddle_cpx — a complex twiddle factor (float r/i).
type twiddleCpx struct {
	r, i float32
}

// fftState is kiss_fft_state (float build). scale is opus_val16 (float);
// factors are (radix, stride) pairs; bitrev/twiddles point at the static
// tables. arch_fft is a no-op in the generic build and is omitted.
type fftState struct {
	nfft     int
	scale    float32
	shift    int
	factors  [2 * maxFactors]int16
	bitrev   []int32
	twiddles []twiddleCpx
}

// rnnKFFT mirrors rnnoise_tables.c's `const kiss_fft_state rnn_kfft`:
// nfft 960, scale 1/960, shift -1, factors {5,192,3,64,4,16,4,4,4,1,...}.
var rnnKFFT = fftState{
	nfft:     960,
	scale:    0.0010416667,
	shift:    -1,
	factors:  [2 * maxFactors]int16{5, 192, 3, 64, 4, 16, 4, 4, 4, 1, 0, 0, 0, 0, 0, 0},
	bitrev:   fftBitrev[:],
	twiddles: fftTwiddles[:],
}

// fftTwiddles holds the 960-pt twiddle table exp(-2*pi*i*k/960). Unlike
// the other tables (embedded from rnnoise_tables.c), the twiddles are
// generated at init from math.Cos/Sin. This is proven bit-identical to
// the C baked constants (denoise/internal/parity_tests/tables:
// TestTrigResolution reports 0/960 mismatches between Go stdlib trig and
// both this machine's libm and the upstream-baked table), and generating
// them also produces the correct signed zero at k=0 (twiddles[0].i =
// -0.0), which a Go decimal constant literal cannot express.
var fftTwiddles [960]twiddleCpx

func init() {
	computeTwiddles(fftTwiddles[:], 960)
}

// computeTwiddles fills twiddles[0..nfft) with exp(-2*pi*i*k/nfft),
// mirroring kiss_fft.c compute_twiddles + kf_cexp (float scalar branch):
//
//	const double pi = 3.14159265358979323846264338327;
//	double phase = (-2*pi/nfft) * k;
//	twiddles[k].r = (float)cos(phase);
//	twiddles[k].i = (float)sin(phase);
//
// The phase is pure float64 arithmetic with no fusible multiply-add, so
// this is bit-identical under both the strict and default builds.
func computeTwiddles(twiddles []twiddleCpx, nfft int) {
	const pi = 3.14159265358979323846264338327
	for k := 0; k < nfft; k++ {
		phase := (-2 * pi / float64(nfft)) * float64(k)
		twiddles[k].r = float32(math.Cos(phase))
		twiddles[k].i = float32(math.Sin(phase))
	}
}
