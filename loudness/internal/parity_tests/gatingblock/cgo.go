//go:build cgo

// Package gatingblock is the Phase-B parity slice for the 400 ms
// gating-block energy: it checks r128.State's channel-weighted mean-square
// energy (the core of momentary/short-term/integrated loudness) bit-for-bit
// against the vendored libebur128 v1.2.6 C oracle across channel counts and
// channel-map permutations, including dual-mono and UNUSED channels applied
// via ebur128_set_channel.
//
// Assertions run in one of two modes (see the strictparity package):
// under strictparity.Enabled — set by building with -tags=loudness_strict, as the
// //loudness:parity / //loudness:test mise tasks do, which also builds the
// C oracle with -ffp-contract=off — the comparison is bit-exact. Without
// that tag (e.g. plain `go test ./...` in CI), the oracle may contain fused
// FMAs in its weighted-sum-of-squares accumulation, so the comparison falls
// back to a tight relative tolerance.
//
// Like every loudness parity slice it compiles its OWN copy of the vendored
// ebur128.c (see gatingblock_cgo_src.c) instead of importing package
// loudness, whose cgo build compiles the same non-static C symbols and
// would collide at link time. The Go side under test is the cgo-free
// package loudness/internal/r128. cgo call sites live here (not in
// parity_test.go) because cgo forbids `import "C"` in a _test.go file.
package gatingblock

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libebur128 -I${SRCDIR}/../../../libebur128/queue
#cgo CFLAGS: -Wno-cpp
#cgo LDFLAGS: -lm

// -Wno-cpp silences ebur128.c's manual-FTZ #warning on non-SSE2 targets
// (see the smoke slice's cgo.go). The parity oracle's FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops)
// come in out-of-band via CGO_CFLAGS from the //loudness:parity mise task.

#include <stddef.h>

extern double gb_block_energy(unsigned int channels, unsigned long rate,
                              const int* chmap, const double* src,
                              size_t frames);
*/
import "C"

import "unsafe"

// cgoBlockEnergy returns libebur128's trailing-400ms gating-block energy
// for the given channel map and interleaved PCM.
func cgoBlockEnergy(channels, rate int, chmap []int, src []float64) float64 {
	frames := len(src) / channels
	cmap := make([]C.int, channels)
	for i, v := range chmap {
		cmap[i] = C.int(v)
	}
	var srcPtr *C.double
	if len(src) > 0 {
		srcPtr = (*C.double)(unsafe.Pointer(&src[0]))
	}
	return float64(C.gb_block_energy(
		C.uint(channels), C.ulong(rate),
		(*C.int)(unsafe.Pointer(&cmap[0])),
		srcPtr, C.size_t(frames)))
}
