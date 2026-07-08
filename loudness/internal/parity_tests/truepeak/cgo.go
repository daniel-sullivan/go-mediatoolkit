//go:build cgo

// Package truepeak is the Phase-B parity slice for the pure-Go true-peak
// interpolator (loudness/internal/r128.TruePeaker). It proves the Go port
// is bit-exact against the vendored C libebur128 oracle at two levels:
//
//   - the raw polyphase interpolator (interp_process) output, float32 for
//     float32, over many rates/channels/chunkings; and
//   - the full public-API true-peak reading (ebur128_true_peak, which
//     folds in the sample peak), float64 for float64.
//
// Unlike the other parity slices under loudness/internal/parity_tests,
// both assertions here are unconditional on strictparity.Enabled (see the
// strictparity package): they were verified empirically to pass identically with
// and without -tags=loudness_strict / the CGO_CFLAGS the //loudness:parity
// mise task supplies, since float32's ~7 decimal digits of precision leave
// no room for a double-rounding or FMA-fusion difference to survive
// truncation back to float32 (see each test's doc comment for detail).
//
// Like every parity slice under loudness/internal/parity_tests, it
// compiles its OWN copy of the vendored ebur128.c amalgamation (see
// truepeak_cgo_src.c) rather than importing the cgo-bearing root loudness
// package: libebur128's public symbols are ordinary non-static C symbols,
// so linking two independent compilations of them into one test binary
// would collide. The pure-Go port under test lives in the cgo-free
// loudness/internal/r128 package precisely so this slice can import it
// without dragging any C object code along. See the smoke slice's cgo.go
// for the fuller rationale.
//
// cgo call sites live in this file rather than parity_test.go because
// Go's cgo does not allow `import "C"` inside a _test.go file; the tests
// call the plain-Go wrappers defined here.
package truepeak

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libebur128 -I${SRCDIR}/../../../libebur128/queue
#cgo CFLAGS: -Wno-cpp
#cgo LDFLAGS: -lm

// -Wno-cpp silences ebur128.c's own manual-FTZ #warning on non-SSE2
// targets — see the smoke slice's cgo.go for why it fires and why it is
// numerically harmless. The parity oracle's FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops)
// are supplied out-of-band via CGO_CFLAGS by the //loudness:parity mise
// task, so the C is built as scalar, FMA-free code matching the Go port.

#include "ebur128.h"
#include <stdlib.h>

// Shims defined in truepeak_cgo_src.c, exposing ebur128.c's static
// polyphase interpolator through opaque handles.
void* tp_interp_create(unsigned int taps, unsigned int factor, unsigned int channels);
size_t tp_interp_process(void* h, size_t frames, float* in, float* out);
void tp_interp_destroy(void* h);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// cInterp wraps a C libebur128 interpolator so a chunked stream can be
// pushed through one persistent instance (matching the Go port's
// persistent delay-line state).
type cInterp struct {
	h        unsafe.Pointer
	channels int
	factor   int
}

// cInterpCreate builds a C interpolator with the given prototype tap
// count, oversampling factor, and channel count.
func cInterpCreate(taps, factor, channels int) (*cInterp, error) {
	h := C.tp_interp_create(C.uint(taps), C.uint(factor), C.uint(channels))
	if h == nil {
		return nil, fmt.Errorf("tp_interp_create(%d,%d,%d) returned NULL", taps, factor, channels)
	}
	return &cInterp{h: h, channels: channels, factor: factor}, nil
}

// process runs one interleaved float32 chunk through the C interpolator,
// returning the interleaved float32 output (len(in)/channels*factor*channels
// samples). The interpolator's delay-line state persists between calls.
func (ci *cInterp) process(in []float32) []float32 {
	frames := len(in) / ci.channels
	out := make([]float32, frames*ci.factor*ci.channels)
	if frames == 0 {
		return out
	}
	C.tp_interp_process(ci.h, C.size_t(frames),
		(*C.float)(unsafe.Pointer(&in[0])),
		(*C.float)(unsafe.Pointer(&out[0])))
	return out
}

// destroy frees the C interpolator.
func (ci *cInterp) destroy() { C.tp_interp_destroy(ci.h) }

// cTruePeakFull drives the full public libebur128 true-peak path over the
// given per-chunk interleaved float64 stream and returns, per channel,
// ebur128_true_peak — which libebur128 defines as
// max(true_peak, sample_peak) (linear amplitude). Mode is
// EBUR128_MODE_TRUE_PEAK, which implicitly enables SAMPLE_PEAK and M.
func cTruePeakFull(channels, sampleRate int, chunks [][]float64) ([]float64, error) {
	st := C.ebur128_init(C.uint(channels), C.ulong(sampleRate), C.int(C.EBUR128_MODE_TRUE_PEAK))
	if st == nil {
		return nil, fmt.Errorf("ebur128_init(%d,%d) failed", channels, sampleRate)
	}
	defer C.ebur128_destroy(&st)

	for ci, chunk := range chunks {
		frames := len(chunk) / channels
		if frames == 0 {
			continue
		}
		ret := C.ebur128_add_frames_double(st,
			(*C.double)(unsafe.Pointer(&chunk[0])), C.size_t(frames))
		if int(ret) != int(C.EBUR128_SUCCESS) {
			return nil, fmt.Errorf("ebur128_add_frames_double chunk %d failed: %d", ci, int(ret))
		}
	}

	out := make([]float64, channels)
	for c := 0; c < channels; c++ {
		var val C.double
		ret := C.ebur128_true_peak(st, C.uint(c), &val)
		if int(ret) != int(C.EBUR128_SUCCESS) {
			return nil, fmt.Errorf("ebur128_true_peak(ch=%d) failed: %d", c, int(ret))
		}
		out[c] = float64(val)
	}
	return out, nil
}
