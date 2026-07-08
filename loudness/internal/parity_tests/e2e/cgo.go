//go:build cgo

// Package e2e is the Phase-B end-to-end parity slice: it drives the whole
// meter core (loudness/internal/r128.State) and the full public libebur128
// v1.2.6 API through the same sampled matrix of sample rates, channel
// layouts (including SetChannel overrides and dual-mono), modes (list and
// histogram), chunkings, and SetMaxWindow / SetMaxHistory settings, then
// compares every reading: momentary, short-term, integrated, LRA, relative
// threshold, and the per-channel sample / prev-sample / true / prev-true
// peaks.
//
// The LUFS/LRA readings run in one of two modes (see the strictparity
// package and loudnessTol): under strictparity.Enabled — set by building with
// -tags=loudness_strict, as the //loudness:parity / //loudness:test mise
// tasks do, which also builds the C oracle with -ffp-contract=off — the
// bound is tight (1e-9 LU, 1e-5 at 192kHz). Without that tag (e.g. plain
// `go test ./...` in CI), the bound widens to absorb possible FMA-fused
// oracle drift in the gated energy accumulation. The peak readings
// (comparePeak) stay bit-exact in both modes.
//
// The Go side under test is r128.State (the numeric core) rather than the
// root loudness.Meter, because a cgo parity slice must not import package
// loudness (that would link a second copy of the non-static ebur128 C
// symbols and collide). loudness.Meter is a thin, allocation-free pass-
// through over r128.State whose own logic (validation, errcode translation,
// time.Duration<->ms) is covered by the pure-Go loudness/meter_test.go.
//
// cgo call sites live here because cgo forbids `import "C"` in a _test.go
// file.
package e2e

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libebur128 -I${SRCDIR}/../../../libebur128/queue
#cgo CFLAGS: -Wno-cpp
#cgo LDFLAGS: -lm

// -Wno-cpp silences ebur128.c's manual-FTZ #warning (see the
// smoke slice's cgo.go). Scalar/FMA-free FP flags arrive via CGO_CFLAGS
// from the //loudness:parity mise task.

#include "ebur128.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// cReadings holds every reading the oracle produces for one case.
type cReadings struct {
	momentary    float64
	shortTerm    float64
	integrated   float64
	lra          float64
	relThreshold float64
	samplePeak   []float64
	prevSample   []float64
	truePeak     []float64
	prevTrue     []float64
}

// cRunMeter drives libebur128's public API for one case: init, apply
// per-channel overrides (overrides[c] >= 0 => set_channel), optionally
// set_max_window / set_max_history before feeding, feed the chunks, and read
// everything back.
func cRunMeter(channels, rate, mode int, overrides []int, setMaxWindowMs, setMaxHistoryMs int, chunks [][]float64) (cReadings, error) {
	st := C.ebur128_init(C.uint(channels), C.ulong(rate), C.int(mode))
	if st == nil {
		return cReadings{}, fmt.Errorf("ebur128_init failed")
	}
	defer C.ebur128_destroy(&st)

	for c, v := range overrides {
		if v >= 0 {
			if ret := C.ebur128_set_channel(st, C.uint(c), C.int(v)); ret != C.EBUR128_SUCCESS {
				return cReadings{}, fmt.Errorf("set_channel(%d,%d) failed: %d", c, v, int(ret))
			}
		}
	}
	if setMaxWindowMs > 0 {
		ret := C.ebur128_set_max_window(st, C.ulong(setMaxWindowMs))
		if int(ret) != int(C.EBUR128_SUCCESS) && int(ret) != 4 {
			return cReadings{}, fmt.Errorf("set_max_window failed: %d", int(ret))
		}
	}
	if setMaxHistoryMs > 0 {
		ret := C.ebur128_set_max_history(st, C.ulong(setMaxHistoryMs))
		if int(ret) != int(C.EBUR128_SUCCESS) && int(ret) != 4 {
			return cReadings{}, fmt.Errorf("set_max_history failed: %d", int(ret))
		}
	}

	for i, chunk := range chunks {
		frames := len(chunk) / channels
		if frames == 0 {
			continue
		}
		if ret := C.ebur128_add_frames_double(st,
			(*C.double)(unsafe.Pointer(&chunk[0])), C.size_t(frames)); ret != C.EBUR128_SUCCESS {
			return cReadings{}, fmt.Errorf("add_frames chunk %d failed: %d", i, int(ret))
		}
	}

	var out cReadings
	var d C.double
	if ret := C.ebur128_loudness_momentary(st, &d); ret != C.EBUR128_SUCCESS {
		return cReadings{}, fmt.Errorf("momentary failed: %d", int(ret))
	}
	out.momentary = float64(d)
	if ret := C.ebur128_loudness_shortterm(st, &d); ret != C.EBUR128_SUCCESS {
		return cReadings{}, fmt.Errorf("shortterm failed: %d", int(ret))
	}
	out.shortTerm = float64(d)
	if ret := C.ebur128_loudness_global(st, &d); ret != C.EBUR128_SUCCESS {
		return cReadings{}, fmt.Errorf("global failed: %d", int(ret))
	}
	out.integrated = float64(d)
	if ret := C.ebur128_loudness_range(st, &d); ret != C.EBUR128_SUCCESS {
		return cReadings{}, fmt.Errorf("range failed: %d", int(ret))
	}
	out.lra = float64(d)
	if ret := C.ebur128_relative_threshold(st, &d); ret != C.EBUR128_SUCCESS {
		return cReadings{}, fmt.Errorf("relative_threshold failed: %d", int(ret))
	}
	out.relThreshold = float64(d)

	out.samplePeak = make([]float64, channels)
	out.prevSample = make([]float64, channels)
	out.truePeak = make([]float64, channels)
	out.prevTrue = make([]float64, channels)
	for c := 0; c < channels; c++ {
		var sp, psp, tp, ptp C.double
		if ret := C.ebur128_sample_peak(st, C.uint(c), &sp); ret != C.EBUR128_SUCCESS {
			return cReadings{}, fmt.Errorf("sample_peak(%d) failed: %d", c, int(ret))
		}
		if ret := C.ebur128_prev_sample_peak(st, C.uint(c), &psp); ret != C.EBUR128_SUCCESS {
			return cReadings{}, fmt.Errorf("prev_sample_peak(%d) failed: %d", c, int(ret))
		}
		if ret := C.ebur128_true_peak(st, C.uint(c), &tp); ret != C.EBUR128_SUCCESS {
			return cReadings{}, fmt.Errorf("true_peak(%d) failed: %d", c, int(ret))
		}
		if ret := C.ebur128_prev_true_peak(st, C.uint(c), &ptp); ret != C.EBUR128_SUCCESS {
			return cReadings{}, fmt.Errorf("prev_true_peak(%d) failed: %d", c, int(ret))
		}
		out.samplePeak[c] = float64(sp)
		out.prevSample[c] = float64(psp)
		out.truePeak[c] = float64(tp)
		out.prevTrue[c] = float64(ptp)
	}
	return out, nil
}
