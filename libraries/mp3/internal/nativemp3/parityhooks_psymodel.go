// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Exported test hooks for the psychoacoustic-model parity oracle
// (internal/parity_tests/psychoacoustic-model).
//
// The FFT/FHT is the floating-point foundation of LAME's psychoacoustic
// model: fftLong / fftShort window the PCM and drive fht (Ron Mayer's fast
// Hartley transform, fft.c:62) to produce the real spectra the model squares
// into per-line energies. fht is unexported here because it is a 1:1
// translation of a LAME `static` function with no place in the public
// surface, but the cgo parity package lives in its own package — it compiles
// the vendored fft.c oracle and so cannot sit inside nativemp3 — and reaches
// the Go port through the thin pass-through wrapper below.
//
// Fht is a verbatim call to the unexported fht it shadows; it exists solely so
// the parity suite can assert the Go port matches the vendored C fht
// bit-for-bit under the mp3_strict build.

// Fht exposes fht for the psychoacoustic-model parity oracle. It runs the
// in-place fast Hartley transform of 2*n points held in fz (the caller sizes
// fz to BLKSIZE for the long FFT, BLKSIZE_s for the short FFT, matching
// fft_long / fft_short which call fht with n = BLKSIZE/2 and BLKSIZE_s/2).
func Fht(fz []float32, n int) { fht(fz, n) }
