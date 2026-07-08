//go:build cgo

// Package benchcmp holds the Go-vs-C comparison benchmarks for the loudness
// meter: the same 60 s stereo 48 kHz workload driven through the pure-Go
// r128.State and through the vendored libebur128 oracle. Run via the
// //loudness:bench mise task (benchmarks only, -run=^$).
//
// It compiles its OWN copy of the vendored ebur128.c (benchcmp_cgo_src.c),
// not package loudness, to avoid colliding on the non-static C symbols. cgo
// call sites live here because cgo forbids `import "C"` in a _test.go file.
package benchcmp

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libebur128 -I${SRCDIR}/../../../libebur128/queue
#cgo CFLAGS: -Wno-cpp
#cgo LDFLAGS: -lm

#include "ebur128.h"

extern double bench_run(unsigned int channels, unsigned long rate, int mode,
                        const double* data, size_t frames);
*/
import "C"

import "unsafe"

// cBenchRun drives one full C meter pass over the interleaved data.
func cBenchRun(channels, rate, mode int, data []float64) float64 {
	frames := len(data) / channels
	if frames == 0 {
		return 0
	}
	return float64(C.bench_run(C.uint(channels), C.ulong(rate), C.int(mode),
		(*C.double)(unsafe.Pointer(&data[0])), C.size_t(frames)))
}
