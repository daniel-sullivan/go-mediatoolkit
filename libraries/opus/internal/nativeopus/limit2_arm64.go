//go:build arm64 && !opus_nosimd && !opus_strict

package nativeopus

// opusLimit2CheckWithin1SIMD — arm64 NEON port of
// opus_limit2_checkwithin1_neon (libopus/celt/arm/celt_neon_intr.c).
// See the .s file for encoding notes. Returns 1 iff every original
// sample was within [-1, 1]; any clipping into [-2, 2] still happens
// in place.
//
//go:noescape
func opusLimit2CheckWithin1SIMD(samples *float32, cnt int) int32

const limit2SIMDAvailable = true
