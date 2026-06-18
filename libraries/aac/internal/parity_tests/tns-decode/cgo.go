// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package tns_decode pins the Go port of the Fraunhofer FDK-AAC decoder's
// Temporal Noise Shaping (TNS) filter application — the all-pole synthesis
// lattice CLpc_SynthesisLattice (the FIXP_DBL coefficient overload that
// CTns_Apply dispatches) — against the vendored libFDK/src/FDK_lpc.cpp,
// compiled into this test binary via cgo. Random Q1.31 spectra, lattice
// reflection coefficients, exponents and filter directions are fabricated on
// the Go side and filtered on both sides; the in-place int32 (FIXP_DBL) output
// is compared bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C source and NEVER
// imports libraries/aac — importing it would link a second copy of the whole
// FDK reference and clash on static symbols (the same amalgamation-split reason
// the flac/huffman parity packages document). It MAY, and does, import the
// pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the TNS lattice is a pure INTEGER fixed-point kernel. libfdk-aac
// implements TNS in FIXP_DBL (Q1.31): the lattice MAC chain is fMultDiv2 (an
// int64 product arithmetic-shifted right by 32) plus integer saturating shifts
// — no `float`/`double` appears, so it is bit-identical regardless of
// -ffp-contract / vectorization and needs no transcendental shim. The
// strict-gate on the Go assertion is kept only for convention (the area lives
// under the aac_strict parity discipline); the kernel itself matches in any
// build. The oracle carries a VERBATIM copy of CLpc_SynthesisLattice's FIXP_DBL
// body (FDK_lpc.cpp:168-209, only the symbol name suffixed _oracle) so the link
// does not drag the rest of FDK_lpc.cpp (CLpc_AutoToParcor pulls schur_div /
// fDivNormSigned from fixpoint_math.cpp, a needless cascade). The body is
// byte-for-byte the vendored source; it depends only on the fixmath inlines in
// common_fix.h + scale.h, which ARE the genuine headers.
package tns_decode

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/tns-decode).
// Mirrors the set in the sibling huffman-spectral-decode oracle.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer kernel in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo LDFLAGS: -lm

#include <stdint.h>

// fparity_synthesis_lattice_dbl runs the vendored CLpc_SynthesisLattice
// (FIXP_DBL coeff overload) in place over signal[0:signalSize] with the given
// lattice coefficients, state buffer (caller-zeroed), exponents and direction.
// signal is mutated in place; state is scratch of length `order`.
extern void fparity_synthesis_lattice_dbl(int32_t *signal, int signalSize,
                                          int signalE, int signalEOut, int inc,
                                          const int32_t *coeff, int order,
                                          int32_t *state);
*/
import "C"

import "unsafe"

// cSynthesisLatticeDBL runs the vendored CLpc_SynthesisLattice (DBL overload)
// in place over a copy of signal and returns the filtered result. state is
// allocated and zeroed by this wrapper (CTns_Apply clears it before each
// filter).
func cSynthesisLatticeDBL(signal []int32, signalE, signalEOut, inc int, coeff []int32, order int) []int32 {
	out := append([]int32(nil), signal...)
	state := make([]int32, order)
	C.fparity_synthesis_lattice_dbl(
		(*C.int32_t)(unsafe.Pointer(&out[0])), C.int(len(out)),
		C.int(signalE), C.int(signalEOut), C.int(inc),
		(*C.int32_t)(unsafe.Pointer(&coeff[0])), C.int(order),
		(*C.int32_t)(unsafe.Pointer(&state[0])))
	return out
}
