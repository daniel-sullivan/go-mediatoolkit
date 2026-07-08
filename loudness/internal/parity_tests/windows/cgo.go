//go:build cgo

// Package windows is the Phase-B parity slice for the trailing-window
// loudness readings — momentary (400 ms), short-term (3 s), and the
// arbitrary-window ebur128_loudness_window — including the SetMaxWindow
// reallocation path. It checks r128.State against libebur128 v1.2.6's
// public API across prime chunkings.
//
// Assertions run in one of two modes (see the strictparity package):
// under strictparity.Enabled — set by building with -tags=loudness_strict, as the
// //loudness:parity / //loudness:test mise tasks do, which also builds the
// C oracle with -ffp-contract=off — the trailing-window energies are
// bit-exact and the LUFS readings use a tight absolute bound. Without that
// tag (e.g. plain `go test ./...` in CI), the oracle may contain fused
// FMAs, so both fall back to wider tolerances.
//
// It compiles its OWN copy of the vendored ebur128.c (windows_cgo_src.c),
// not package loudness, to avoid colliding on the non-static C symbols. cgo
// call sites live here because cgo forbids `import "C"` in a _test.go file.
package windows

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libebur128 -I${SRCDIR}/../../../libebur128/queue
#cgo CFLAGS: -Wno-cpp
#cgo LDFLAGS: -lm

// -Wno-cpp silences ebur128.c's manual-FTZ #warning (see the
// smoke slice's cgo.go). Scalar/FMA-free FP flags arrive via CGO_CFLAGS
// from the //loudness:parity mise task.

#include "ebur128.h"

extern double win_energy(ebur128_state* st, size_t interval_frames);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// windowReadings holds one meter's readings: the momentary / short-term
// LUFS (and their pre-log energies), and per query-window LUFS + energy.
type windowReadings struct {
	momentary       float64
	momentaryEnergy float64
	shortTerm       float64
	shortTermEnergy float64
	windows         []float64 // LUFS per queryWindows entry
	windowEnergies  []float64 // energy per queryWindows entry
}

// cWindowMeter drives libebur128's public window readers over a chunked
// interleaved stream, optionally calling ebur128_set_max_window(maxWindow
// ms) before feeding audio (0 = leave the default). Alongside each public
// LUFS reading it captures the corresponding trailing-window energy via the
// win_energy shim (same interval frame counts the readers use).
func cWindowMeter(channels, rate, mode, maxWindow int, chunks [][]float64, queryWindows []int) (windowReadings, error) {
	st := C.ebur128_init(C.uint(channels), C.ulong(rate), C.int(mode))
	if st == nil {
		return windowReadings{}, fmt.Errorf("ebur128_init failed")
	}
	defer C.ebur128_destroy(&st)

	if maxWindow > 0 {
		ret := C.ebur128_set_max_window(st, C.ulong(maxWindow))
		if int(ret) != int(C.EBUR128_SUCCESS) && int(ret) != 4 { // 4 == NO_CHANGE
			return windowReadings{}, fmt.Errorf("set_max_window(%d) failed: %d", maxWindow, int(ret))
		}
	}

	for i, chunk := range chunks {
		frames := len(chunk) / channels
		if frames == 0 {
			continue
		}
		if ret := C.ebur128_add_frames_double(st,
			(*C.double)(unsafe.Pointer(&chunk[0])), C.size_t(frames)); ret != C.EBUR128_SUCCESS {
			return windowReadings{}, fmt.Errorf("add_frames chunk %d failed: %d", i, int(ret))
		}
	}

	si := (rate + 5) / 10
	var out windowReadings
	var m C.double
	if ret := C.ebur128_loudness_momentary(st, &m); ret != C.EBUR128_SUCCESS {
		return windowReadings{}, fmt.Errorf("loudness_momentary failed: %d", int(ret))
	}
	out.momentary = float64(m)
	out.momentaryEnergy = float64(C.win_energy(st, C.size_t(si*4)))

	if mode&C.EBUR128_MODE_S == C.EBUR128_MODE_S {
		var s C.double
		if ret := C.ebur128_loudness_shortterm(st, &s); ret != C.EBUR128_SUCCESS {
			return windowReadings{}, fmt.Errorf("loudness_shortterm failed: %d", int(ret))
		}
		out.shortTerm = float64(s)
		out.shortTermEnergy = float64(C.win_energy(st, C.size_t(si*30)))
	}

	for _, w := range queryWindows {
		var lw C.double
		if ret := C.ebur128_loudness_window(st, C.ulong(w), &lw); ret != C.EBUR128_SUCCESS {
			return windowReadings{}, fmt.Errorf("loudness_window(%d) failed: %d", w, int(ret))
		}
		out.windows = append(out.windows, float64(lw))
		interval := rate * w / 1000
		out.windowEnergies = append(out.windowEnergies, float64(C.win_energy(st, C.size_t(interval))))
	}
	return out, nil
}
