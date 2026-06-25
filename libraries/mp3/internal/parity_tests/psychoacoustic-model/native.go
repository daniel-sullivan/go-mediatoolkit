// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package psymodel

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 port the same way oracle.c drives the
// vendored C: run the fast Hartley transform in place over a copy of the input
// buffer. Keeping the Go driver beside the cgo bridge (rather than inline in
// the test) mirrors the C oracle structure one-to-one so the two sides of each
// assertion are visibly symmetric.

// goFht runs the Go port's nativemp3.Fht over a copy of fz (a buffer of 2*n
// floats) and returns the transformed buffer, so the in-place transform does
// not mutate the caller's slice.
func goFht(fz []float32, n int) []float32 {
	buf := make([]float32, len(fz))
	copy(buf, fz)
	nativemp3.Fht(buf, n)
	return buf
}
