//go:build arm64 && !opus_nosimd && !opus_strict

package nativeopus

// celtFloat2Int16SIMD — arm64 NEON port of celt_float2int16_neon
// (libopus/celt/arm/celt_neon_intr.c). See the .s file for encoding
// notes. Uses FCVTAS (round-to-nearest, ties-away); NOT bit-exact
// with math.RoundToEven on exact half-integer inputs, so excluded
// under -tags=opus_strict.
//
//go:noescape
func celtFloat2Int16SIMD(in *float32, out *opus_int16, cnt int)

const float2int16SIMDAvailable = true
