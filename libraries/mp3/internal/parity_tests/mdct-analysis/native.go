// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package mdctanalysis

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 port the same way oracle.c drives the
// vendored C: run each filterbank/MDCT kernel over a copy of the input buffer.
// Keeping the Go drivers beside the cgo bridge (rather than inline in the test)
// mirrors the C oracle structure one-to-one so the two sides of each assertion
// are visibly symmetric. These import only internal/nativemp3 (never
// libraries/mp3).

// goWindowSubband runs nativemp3.WindowSubband over a copy of x1 with base as
// the index of the C x1[0] cursor, returning the 32 subband samples.
func goWindowSubband(x1 []float32, base int) []float32 {
	buf := make([]float32, len(x1))
	copy(buf, x1)
	a := make([]float32, 32)
	nativemp3.WindowSubband(buf, base, a)
	return a
}

// goMdctShort runs nativemp3.MdctShort in place over a copy of the 18-line
// inout buffer and returns the transformed copy.
func goMdctShort(inout []float32) []float32 {
	buf := make([]float32, len(inout))
	copy(buf, inout)
	nativemp3.MdctShort(buf)
	return buf
}

// goMdctLong runs nativemp3.MdctLong over a copy of the 18-line in buffer and
// returns the 18 MDCT lines.
func goMdctLong(in []float32) []float32 {
	src := make([]float32, len(in))
	copy(src, in)
	out := make([]float32, 18)
	nativemp3.MdctLong(out, src)
	return out
}
