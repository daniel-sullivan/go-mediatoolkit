//go:build cgo

// Package integrated is the Phase-B parity slice for gated integrated
// loudness and the relative gating threshold: it checks r128.State against
// libebur128 v1.2.6's public ebur128_loudness_global / ebur128_relative_
// threshold over constructed and random streams, in both the list and
// histogram gating modes.
//
// The LUFS assertions run in one of two modes (see the strictparity
// package): under strictparity.Enabled — set by building with
// -tags=loudness_strict, as the //loudness:parity / //loudness:test mise
// tasks do, which also builds the C oracle with -ffp-contract=off — the
// bound is a tight 1e-9 LU. Without that tag (e.g. plain `go test ./...` in
// CI), the oracle may contain fused FMAs in the gated energy accumulation,
// so the bound widens to 1e-6 LU. The gate constants, histogram tables, and
// the -Inf / -70.0 structural cases stay unconditional in both modes (see
// their own comments for why).
//
// Like every loudness parity slice it compiles its OWN copy of the vendored
// ebur128.c (see integrated_cgo_src.c) instead of importing package
// loudness. cgo call sites live here because cgo forbids `import "C"` in a
// _test.go file.
package integrated

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libebur128 -I${SRCDIR}/../../../libebur128/queue
#cgo CFLAGS: -Wno-cpp
#cgo LDFLAGS: -lm

// -Wno-cpp silences ebur128.c's manual-FTZ #warning (see the
// smoke slice's cgo.go). Scalar/FMA-free FP flags arrive via CGO_CFLAGS
// from the //loudness:parity mise task.

#include "ebur128.h"

extern void gate_constants(double* rel_gate_factor, double* minus_twenty,
                           double* boundary0);
extern double hist_energy(int i);
extern double hist_boundary(int i);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// cgoIntegrated drives the public libebur128 integrated-loudness path over
// the given interleaved chunks and returns the integrated loudness (LUFS)
// and relative threshold (LUFS). mode is a raw ebur128 mode bitmask.
func cgoIntegrated(channels, rate, mode int, chunks [][]float64) (integrated, relThreshold float64, err error) {
	st := C.ebur128_init(C.uint(channels), C.ulong(rate), C.int(mode))
	if st == nil {
		return 0, 0, fmt.Errorf("ebur128_init(%d,%d,%#x) failed", channels, rate, mode)
	}
	defer C.ebur128_destroy(&st)

	for i, chunk := range chunks {
		frames := len(chunk) / channels
		if frames == 0 {
			continue
		}
		ret := C.ebur128_add_frames_double(st,
			(*C.double)(unsafe.Pointer(&chunk[0])), C.size_t(frames))
		if int(ret) != int(C.EBUR128_SUCCESS) {
			return 0, 0, fmt.Errorf("add_frames chunk %d failed: %d", i, int(ret))
		}
	}

	var loud, rel C.double
	if ret := C.ebur128_loudness_global(st, &loud); ret != C.EBUR128_SUCCESS {
		return 0, 0, fmt.Errorf("loudness_global failed: %d", int(ret))
	}
	if ret := C.ebur128_relative_threshold(st, &rel); ret != C.EBUR128_SUCCESS {
		return 0, 0, fmt.Errorf("relative_threshold failed: %d", int(ret))
	}
	return float64(loud), float64(rel), nil
}

// cgoGateConstants returns libebur128's static gating constants.
func cgoGateConstants() (relGateFactor, minusTwenty, boundary0 float64) {
	var rg, mt, b0 C.double
	C.gate_constants(&rg, &mt, &b0)
	return float64(rg), float64(mt), float64(b0)
}

// cgoHistEnergy / cgoHistBoundary read libebur128's histogram tables.
func cgoHistEnergy(i int) float64   { return float64(C.hist_energy(C.int(i))) }
func cgoHistBoundary(i int) float64 { return float64(C.hist_boundary(C.int(i))) }
