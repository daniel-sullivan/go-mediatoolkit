//go:build cgo

// Package lra is the Phase-B parity slice for the loudness range (EBU Tech
// 3342) reading. It checks r128.State.LoudnessRange against libebur128
// v1.2.6's public ebur128_loudness_range across short-term block counts of
// 1, 2, 3, and 31, in both list and histogram modes, and with
// set_max_history trimming.
//
// The LRA assertion runs in one of two modes (see the strictparity
// package): under strictparity.Enabled — set by building with
// -tags=loudness_strict, as the //loudness:parity / //loudness:test mise
// tasks do, which also builds the C oracle with -ffp-contract=off — the
// bound is a tight 1e-9 LU. Without that tag (e.g. plain `go test ./...` in
// CI), the oracle's short-term block energies may carry fused-FMA drift, so
// the bound widens to 1e-6 LU.
//
// It compiles its OWN copy of the vendored ebur128.c (lra_cgo_src.c), not
// package loudness, to avoid colliding on the non-static C symbols. cgo call
// sites live here because cgo forbids `import "C"` in a _test.go file.
package lra

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libebur128 -I${SRCDIR}/../../../libebur128/queue
#cgo CFLAGS: -Wno-cpp
#cgo LDFLAGS: -lm

// -Wno-cpp silences ebur128.c's manual-FTZ #warning (see the
// smoke slice's cgo.go). Scalar/FMA-free FP flags arrive via CGO_CFLAGS
// from the //loudness:parity mise task.

#include "ebur128.h"

extern int lra_st_count(ebur128_state* st);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// cLoudnessRange drives libebur128's public LRA path: init, optionally
// set_max_history(historyMs) AFTER feeding (to exercise the trim), feed the
// chunks, and read ebur128_loudness_range plus the retained short-term
// block count.
func cLoudnessRange(channels, rate, mode, historyMs int, chunks [][]float64) (lra float64, stCount int, err error) {
	st := C.ebur128_init(C.uint(channels), C.ulong(rate), C.int(mode))
	if st == nil {
		return 0, 0, fmt.Errorf("ebur128_init failed")
	}
	defer C.ebur128_destroy(&st)

	for i, chunk := range chunks {
		frames := len(chunk) / channels
		if frames == 0 {
			continue
		}
		if ret := C.ebur128_add_frames_double(st,
			(*C.double)(unsafe.Pointer(&chunk[0])), C.size_t(frames)); ret != C.EBUR128_SUCCESS {
			return 0, 0, fmt.Errorf("add_frames chunk %d failed: %d", i, int(ret))
		}
	}

	if historyMs > 0 {
		ret := C.ebur128_set_max_history(st, C.ulong(historyMs))
		if int(ret) != int(C.EBUR128_SUCCESS) && int(ret) != 4 { // 4 == NO_CHANGE
			return 0, 0, fmt.Errorf("set_max_history(%d) failed: %d", historyMs, int(ret))
		}
	}

	var out C.double
	if ret := C.ebur128_loudness_range(st, &out); ret != C.EBUR128_SUCCESS {
		return 0, 0, fmt.Errorf("loudness_range failed: %d", int(ret))
	}
	return float64(out), int(C.lra_st_count(st)), nil
}
