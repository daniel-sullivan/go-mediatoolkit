//go:build arm64 && !flac_strict

package nativeflac

// windowDataMulNEON computes out[i] = float32(in[i]) * window[i] for the bulk
// of a contiguous run, four samples at a time, via the arm64 NEON kernel in
// window_mul_arm64.s. It is a single per-lane float32 multiply (no fused
// multiply-add), so it matches the default-build scalar f32mul path's rounding
// contract. It is compiled into the DEFAULT build only; the flac_strict build
// keeps the //go:noinline scalar f32mul so the -ffp-contract=off parity oracle
// stays bit-exact. Returns the number of samples consumed (a multiple of 4);
// the caller computes the remaining tail with the scalar path.
//
//go:noescape
func windowDataMulNEON(in *int32, window *float32, out *float32, n int) int

// windowMulNEONAvailable reports that the NEON window-multiply kernel is
// present (arm64 default build).
const windowMulNEONAvailable = true
