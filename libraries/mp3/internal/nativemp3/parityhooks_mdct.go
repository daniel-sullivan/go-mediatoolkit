// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Exported test hooks for the mdct-analysis parity oracle
// (internal/parity_tests/mdct-analysis).
//
// The polyphase analysis filterbank and the long/short MDCTs are the entire
// floating-point surface of LAME's encoder analysis front end (newmdct.c):
// window_subband applies the 32-band overlapping analysis window + Takehiro's
// fast IDCT to produce 32 subband samples, and mdct_short / mdct_long run the
// 6-line / 18-line MDCTs that turn windowed subband samples into MDCT lines.
// These three are the per-granule FP kernels mdct_sub48 drives, so they are the
// bit-exactness pins for the "mdct-analysis" area.
//
// All three are `inline static` in newmdct.c, so they have no public C API; the
// mdct-analysis oracle TU (oracle.c) #includes the vendored newmdct.c directly
// — bringing the statics into scope — and re-exports them through thin
// oracle_* wrappers in the same translation unit, exactly as the
// psychoacoustic-model slice re-exports fht. The cgo parity package lives in
// its own package (it compiles the vendored C oracle and so cannot sit inside
// nativemp3) and reaches the Go port through the pass-through wrappers below.
//
// windowSubband / mdctShort / mdctLong are unexported here because they are 1:1
// translations of LAME `static` functions with no place in the public surface;
// the WindowSubband / MdctShort / MdctLong wrappers exist solely so the parity
// suite can assert the Go port matches the vendored C bit-for-bit under the
// mp3_strict build. They are mp3lame-gated like the slice they expose: a bare
// `go build ./...` never compiles the LGPL-fenced encoder analysis front end.

// WindowSubband exposes window_subband (newmdct.c:430) for the mdct-analysis
// parity oracle. x1 is the PCM window buffer and base is the index of C's
// x1[0]; window_subband reads x1[base-286 .. base+256] (the look-behind history
// plus the 32 fresh samples) and writes 32 subband samples into a[0..31].
func WindowSubband(x1 []float32, base int, a []float32) { windowSubband(x1, base, a) }

// MdctShort exposes mdct_short (newmdct.c:832) for the mdct-analysis parity
// oracle. It runs the three short-block 6-line MDCTs in place over the 18-line
// inout buffer.
func MdctShort(inout []float32) { mdctShort(inout) }

// MdctLong exposes mdct_long (newmdct.c:869) for the mdct-analysis parity
// oracle. It runs the long-block 18-line MDCT, reading 18 windowed inputs from
// in and writing 18 MDCT lines to out.
func MdctLong(out, in []float32) { mdctLong(out, in) }
