//go:build cgo

// Package benchcmp benchmarks the pure-Go nativemp3 port against the vendored
// C minimp3 (compiled inline via Cgo), mirroring the libraries/flac and
// libraries/opus benchcmp suites. The C path is the same scalar reference the
// parity_tests use — minimp3 compiled with MINIMP3_NO_SIMD in-source and
// -ffp-contract=off plus no auto-vectorization/unrolling (set via CGO_CFLAGS
// in the mise bench tasks) — so it is an apples-to-apples scalar comparison,
// not a native-vs-production-C one.
//
// SCOPE. The nativemp3 port currently covers the integer "main-bits" slices
// (bit reader, frame sync, side-info, reservoir). The full granule decode /
// IMDCT / synthesis filterbank are not yet ported, so there is no end-to-end
// native-vs-cgo frame decode to benchmark here yet. This harness therefore
// measures the ported slice that has a runnable native AND cgo path: minimp3's
// MSB-first bit reader (bs_t / get_bits), which is integer-only and bit-exact
// in both build modes. It is the FLAC-benchcmp analog scoped to the partial
// port; add Huffman-dequant / IMDCT / synthesis columns here as those slices
// land.
//
// minimp3's get_bits/bs_init are `static`, so they can only be called from a
// TU that #includes the implementation. This package compiles its OWN private
// copy of minimp3 (via MINIMP3_IMPLEMENTATION below) and surfaces each static
// behind a mp3parity_* trampoline — the same discipline the parity oracle
// packages use. It never imports libraries/mp3 (which would compile minimp3 a
// second time and clash on its statics); it does drive the pure-Go
// internal/nativemp3 directly.
package benchcmp

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libminimp3
#cgo LDFLAGS: -lm
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable

#define MINIMP3_IMPLEMENTATION
#define MINIMP3_NO_SIMD
#include "minimp3.h"

#include <stdint.h>
#include <stdlib.h>

// bs_init / get_bits are static inside minimp3.h; wrap them behind stable,
// non-static linkage names. The trampolines are verbatim calls into minimp3.
static void     bench_bs_init(bs_t *bs, const uint8_t *data, int bytes) { bs_init(bs, data, bytes); }
static uint32_t bench_get_bits(bs_t *bs, int n)                         { return get_bits(bs, n); }

// bench_getbits_sweep drives the minimp3 bit reader over `data` exactly as the
// Go benchmark does: re-init, then read each width in `widths` (length
// `nwidths`) repeatedly until the reader overruns its limit, restarting at the
// top of the buffer. Returns an XOR accumulator of every value read so the
// optimizer cannot elide the loop. `reps` outer passes are run so a single cgo
// call covers a meaningful chunk of work (cgo call overhead would otherwise
// dominate a per-read crossing).
static uint32_t bench_getbits_sweep(const uint8_t *data, int bytes,
                                    const int *widths, int nwidths, int reps) {
    uint32_t acc = 0;
    for (int r = 0; r < reps; r++) {
        bs_t bs;
        bench_bs_init(&bs, data, bytes);
        int wi = 0;
        // Stop before an overrun read so we exercise the in-bounds fast path,
        // matching the Go loop's guard.
        while (bs.pos + 24 <= bs.limit) {
            int n = widths[wi];
            acc ^= bench_get_bits(&bs, n);
            wi++;
            if (wi == nwidths) wi = 0;
        }
    }
    return acc;
}
*/
import "C"

import "unsafe"

// cgoGetBitsSweep runs minimp3's get_bits over data, cycling through widths,
// for reps passes. It returns an XOR of every value read (used only to defeat
// dead-code elimination). data is copied into C memory so bs_t->buf is a C
// pointer the cgo pointer checker accepts.
func cgoGetBitsSweep(data []byte, widths []int32, reps int) uint32 {
	var dptr *C.uint8_t
	var cbuf unsafe.Pointer
	if len(data) > 0 {
		cbuf = C.CBytes(data)
		dptr = (*C.uint8_t)(cbuf)
		defer C.free(cbuf)
	}
	var wptr *C.int
	if len(widths) > 0 {
		wptr = (*C.int)(unsafe.Pointer(&widths[0]))
	}
	return uint32(C.bench_getbits_sweep(
		dptr, C.int(len(data)),
		wptr, C.int(len(widths)), C.int(reps)))
}
