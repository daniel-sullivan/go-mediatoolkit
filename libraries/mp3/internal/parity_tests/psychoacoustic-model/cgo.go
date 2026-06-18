// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package psymodel holds the psychoacoustic-model parity slice: it pins the
// pure-Go nativemp3 port of LAME 3.100's FFT/FHT — fht(), the fast Hartley
// transform that is the floating-point foundation of the psychoacoustic model
// (fft_long / fft_short window a granule and run fht to produce the real
// spectra the model squares into per-line energies) — against the vendored C
// LAME reference compiled inline via cgo.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference (oracle.c, which #includes
// the committed libmp3lame/fft.c) so each go-test binary is symbol-self-
// contained, and it NEVER imports libraries/mp3 (which would duplicate the
// LAME symbols at link time) — only the pure-Go internal/nativemp3 port.
//
// LAME's fht is file-static; oracle.c re-exports it through the thin oracle_fht
// wrapper in the same translation unit so the C side of every assertion is the
// genuine vendored code (see oracle.h). Inputs are the raw FLOAT working buffer
// fht operates on in place — fft_long calls fht(x, BLKSIZE/2) over a
// BLKSIZE-long buffer, fft_short calls fht(x, BLKSIZE_s/2) over a
// BLKSIZE_s-long buffer — so the parity test fills 2*n random floats and runs
// both fht implementations over identical bytes.
//
// This slice IS floating-point-bearing: every butterfly term is a separately
// rounded c*x + s*y, so the result is only bit-exact under the mp3_strict
// build (FMA-free Go) against the -ffp-contract=off cgo oracle. The strict gate
// lives in parity_test.go.
package psymodel

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

// cgoFht runs the vendored static LAME fht over a copy of fz (a buffer of 2*n
// floats) and returns the transformed buffer. fz is copied into a local C array
// so the in-place transform does not mutate the Go caller's slice, mirroring
// how the Go side runs over its own copy.
func cgoFht(fz []float32, n int) []float32 {
	buf := make([]float32, len(fz))
	copy(buf, fz)
	if len(buf) == 0 {
		return buf
	}
	C.oracle_fht((*C.float)(unsafe.Pointer(&buf[0])), C.int(n))
	return buf
}
