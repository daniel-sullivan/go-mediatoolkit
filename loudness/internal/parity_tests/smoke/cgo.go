//go:build cgo

// Package smoke is a Phase-A parity slice proving the vendored
// libebur128 cgo oracle links and runs, ahead of the bit-exact
// Go-port parity slices that land in Phase B (kweight, gatingblock,
// integrated, windows, lra, truepeak, e2e).
//
// It compiles its own copy of the vendored ebur128.c amalgamation —
// independent of the pure-Go r128 port — mirroring the
// self-contained-translation-unit-per-slice shape used by
// libraries/flac/internal/parity_tests/foundation (own #cgo CFLAGS,
// own C source file, own include paths back into the vendored tree).
// This package deliberately does NOT import the root loudness
// package (nor any package that compiles ebur128.c): doing so would
// pull a second compiled copy of ebur128.c into this test binary
// alongside this package's own copy, and since libebur128's public API
// (ebur128_init, ebur128_destroy, ...) is compiled as ordinary
// non-static C symbols, two independent compilations of the same
// symbols linked into one binary would collide at link time. The
// root loudness package sidesteps this by delegating to the cgo-free
// r128 port and compiling no C of its own; every cgo parity slice
// follows this same self-contained shape.
//
// This slice's own assertion is a loose [-12, -6] LUFS plausibility range,
// not a bit-exact comparison, so it needs no strictparity gating and does
// not import the strictparity package that the bit-exact slices consult.
//
// cgo call sites live in this file rather than parity_test.go: Go's
// cgo does not support `import "C"` inside a _test.go file, so the C
// calls are wrapped in plain Go functions here and parity_test.go
// only calls those.
package smoke

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libebur128 -I${SRCDIR}/../../../libebur128/queue
#cgo CFLAGS: -Wno-cpp
#cgo LDFLAGS: -lm

// -Wno-cpp (clang/gcc's plain-ASCII alias for the "-W#warnings" group)
// silences ebur128.c's own `#warning "manual FTZ is being used,
// please enable SSE2 ..."`. On any non-x86 target (arm64 macOS/Linux,
// the common case here), or x86 without -msse2/-mfpmath=sse, libebur128
// takes its portable fallback that flushes denormals to zero by hand
// (explicit fabs(x) < DBL_MIN checks) instead of the SSE2 MXCSR FTZ
// bit — numerically correct, just not hardware-accelerated, so the
// warning is expected noise, not a defect.

#include "ebur128.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"math"
	"unsafe"
)

// smokeMeasureIntegratedLoudness runs the full
// ebur128_init/add_frames_double/loudness_global/destroy call
// sequence over an interleaved buffer and returns the integrated
// loudness in LUFS, or an error if any C call fails.
func smokeMeasureIntegratedLoudness(channels, sampleRate int, samples []float64) (float64, error) {
	mode := C.int(C.EBUR128_MODE_I)
	st := C.ebur128_init(C.uint(channels), C.ulong(sampleRate), mode)
	if st == nil {
		return 0, fmt.Errorf("ebur128_init failed")
	}
	defer C.ebur128_destroy(&st)

	frames := len(samples) / channels
	addRet := C.ebur128_add_frames_double(st, (*C.double)(unsafe.Pointer(&samples[0])), C.size_t(frames))
	if int(addRet) != int(C.EBUR128_SUCCESS) {
		return 0, fmt.Errorf("ebur128_add_frames_double failed: %d", int(addRet))
	}

	var loudness C.double
	globalRet := C.ebur128_loudness_global(st, &loudness)
	if int(globalRet) != int(C.EBUR128_SUCCESS) {
		return 0, fmt.Errorf("ebur128_loudness_global failed: %d", int(globalRet))
	}

	got := float64(loudness)
	if math.IsInf(got, 0) || math.IsNaN(got) {
		return got, fmt.Errorf("non-finite loudness: %v", got)
	}
	return got, nil
}
