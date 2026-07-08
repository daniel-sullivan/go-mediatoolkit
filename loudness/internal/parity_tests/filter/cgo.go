//go:build cgo

// Package filter is the Phase-B parity slice for the K-weighting filter:
// it checks the pure-Go port in loudness/internal/r128 against the
// vendored libebur128 v1.2.6 C oracle, bit-for-bit, for both the derived
// coefficients and the filtered output.
//
// Assertions run in one of two modes (see the strictparity package):
// under strictparity.Enabled — set by building with -tags=loudness_strict, as the
// //loudness:parity / //loudness:test mise tasks do, which also builds the
// C oracle with -ffp-contract=off — comparisons are bit-exact (modulo the
// documented 1-ULP tan/pow libm exception). Without that tag (e.g. plain
// `go test ./...` in CI), the oracle may contain fused FMAs, so comparisons
// fall back to a tight relative tolerance.
//
// Like every loudness parity slice it compiles its OWN copy of the
// vendored ebur128.c (see filter_cgo_src.c). Because ebur128.c's public
// API is compiled as ordinary non-static C symbols, two slices (or a
// slice and any other package) that each compiled it would collide at
// link time — so the Go side under test is the cgo-FREE package
// loudness/internal/r128, which carries no C symbols and imports cleanly
// alongside this slice's oracle. (The root loudness package likewise
// delegates to r128 and compiles no C of its own.) This mirrors how
// libraries/flac's parity slices import libraries/flac/internal/
// nativeflac. parity_test.go does that import; this file only wraps the C
// oracle, because cgo does not allow `import "C"` inside a _test.go file.
package filter

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libebur128 -I${SRCDIR}/../../../libebur128/queue
#cgo CFLAGS: -Wno-cpp
#cgo LDFLAGS: -lm

// -Wno-cpp silences ebur128.c's own manual-FTZ `#warning` on non-SSE2
// targets — the same expected, numerically-harmless noise documented in
// the smoke slice's cgo.go. The manual flush-to-zero branch
// it warns about is exactly the one the Go port reproduces.

#include <stddef.h>
#include <stdlib.h>

// Parity shims defined in filter_cgo_src.c (that TU #includes ebur128.c
// so it can reach libebur128's static internals; this preamble is a
// separate TU that only needs the prototypes).
extern int   fparity_kfilter_coeffs(unsigned int channels, unsigned long rate,
                                     double b_out[5], double a_out[5]);
extern void* fparity_kfilter_new(unsigned int channels, unsigned long rate);
extern void  fparity_kfilter_free(void* handle);
extern void  fparity_kfilter_chunk(void* handle, const double* src,
                                    size_t frames, double* dst);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// cgoKFilterCoeffs runs libebur128's ebur128_init_filter at the given
// channel count and sample rate and returns the derived convolved
// numerator (b) and denominator (a) coefficients.
func cgoKFilterCoeffs(channels, sampleRate int) (b, a [5]float64, err error) {
	var cb, ca [5]C.double
	ret := C.fparity_kfilter_coeffs(
		C.uint(channels), C.ulong(sampleRate),
		(*C.double)(unsafe.Pointer(&cb[0])), (*C.double)(unsafe.Pointer(&ca[0])))
	if int(ret) != 0 {
		return b, a, fmt.Errorf("fparity_kfilter_coeffs(ch=%d, rate=%d) failed: %d", channels, sampleRate, int(ret))
	}
	for i := 0; i < 5; i++ {
		b[i] = float64(cb[i])
		a[i] = float64(ca[i])
	}
	return b, a, nil
}

// cgoKFilter is a persistent C-side ebur128 filter handle used to mirror
// the Go KFilter's chunked, state-carrying filtering. Parity-test-only;
// callers must Free it.
type cgoKFilter struct {
	handle   unsafe.Pointer
	channels int
}

// newCgoKFilter constructs a C filter for the given channel count and
// sample rate.
func newCgoKFilter(channels, sampleRate int) *cgoKFilter {
	h := C.fparity_kfilter_new(C.uint(channels), C.ulong(sampleRate))
	return &cgoKFilter{handle: unsafe.Pointer(h), channels: channels}
}

// chunk filters one interleaved chunk (len(src) a multiple of channels)
// through the C oracle, carrying filter state from previous calls, and
// returns the filtered output in a fresh slice of the same length.
func (f *cgoKFilter) chunk(src []float64) []float64 {
	dst := make([]float64, len(src))
	if len(src) == 0 {
		return dst
	}
	frames := len(src) / f.channels
	C.fparity_kfilter_chunk(
		f.handle,
		(*C.double)(unsafe.Pointer(&src[0])),
		C.size_t(frames),
		(*C.double)(unsafe.Pointer(&dst[0])))
	return dst
}

// Free releases the underlying ebur128 state.
func (f *cgoKFilter) Free() {
	C.fparity_kfilter_free(f.handle)
	f.handle = nil
}
