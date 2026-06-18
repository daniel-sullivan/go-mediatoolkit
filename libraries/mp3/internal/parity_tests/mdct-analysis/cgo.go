// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package mdctanalysis holds the mdct-analysis parity slice: it pins the
// pure-Go nativemp3 port of LAME 3.100's encoder analysis front end — the
// polyphase analysis filterbank window_subband (newmdct.c:430) and the
// long/short MDCTs mdct_long (newmdct.c:869) / mdct_short (newmdct.c:832) — against
// the vendored C LAME reference compiled inline via cgo.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference (oracle.c, which #includes
// the committed libmp3lame/newmdct.c) so each go-test binary is symbol-self-
// contained, and it NEVER imports libraries/mp3 (which would duplicate the
// LAME symbols at link time) — only the pure-Go internal/nativemp3 port.
//
// LAME's window_subband / mdct_short / mdct_long are file-static; oracle.c
// re-exports them through thin oracle_* wrappers in the same translation unit
// so the C side of every assertion is the genuine vendored code (see oracle.h).
// The flat driver mdct_sub48 reaches through lame_internal_flags and is pure
// plumbing over these three kernels, so it is not wrapped; the kernels are the
// bit-exact contract for the slice.
//
// This slice IS floating-point-bearing: every filterbank/MDCT term is a
// separately rounded a*b ± c*d, so the result is only bit-exact under the
// mp3_strict build (FMA-free Go) against the -ffp-contract=off cgo oracle. The
// strict gate lives in parity_test.go.
//
// Build tags: this package is gated by `mp3lame` (in addition to `cgo`) because
// newmdct.c is LGPL LAME source and the Go port slice it pins is itself
// mp3lame-gated; the canonical strict run is therefore `-tags=mp3_strict,mp3lame`
// (the additive //libraries/mp3:parity-lame mise task).
package mdctanalysis

/*
#cgo CFLAGS: -I${SRCDIR}/../../../liblame
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/libmp3lame
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/mpglib
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/include
#cgo CFLAGS: -DHAVE_CONFIG_H
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable
#cgo CFLAGS: -Wno-shift-negative-value -Wno-absolute-value -Wno-tautological-pointer-compare

#include "oracle.h"
*/
import "C"

import "unsafe"

// cgoWindowSubband runs the vendored static window_subband over a copy of x1
// (the PCM window buffer) with base as the index of the C x1[0] cursor, and
// returns the 32 subband samples it writes. x1 is copied so the call does not
// mutate the Go caller's slice (window_subband only reads x1, but copying keeps
// the two drivers symmetric).
func cgoWindowSubband(x1 []float32, base int) []float32 {
	buf := make([]float32, len(x1))
	copy(buf, x1)
	a := make([]float32, 32)
	C.oracle_window_subband((*C.float)(unsafe.Pointer(&buf[0])), C.int(base), (*C.float)(unsafe.Pointer(&a[0])))
	return a
}

// cgoMdctShort runs the vendored static mdct_short in place over a copy of the
// 18-line inout buffer and returns the transformed copy.
func cgoMdctShort(inout []float32) []float32 {
	buf := make([]float32, len(inout))
	copy(buf, inout)
	C.oracle_mdct_short((*C.float)(unsafe.Pointer(&buf[0])))
	return buf
}

// cgoMdctLong runs the vendored static mdct_long over a copy of the 18-line in
// buffer and returns the 18 MDCT lines it writes.
func cgoMdctLong(in []float32) []float32 {
	src := make([]float32, len(in))
	copy(src, in)
	out := make([]float32, 18)
	C.oracle_mdct_long((*C.float)(unsafe.Pointer(&out[0])), (*C.float)(unsafe.Pointer(&src[0])))
	return out
}
