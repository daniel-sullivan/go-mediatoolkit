//go:build !(arm64 && !opus_nosimd && !opus_strict)

package nativeopus

// Fallback for the float2int16 SIMD path when compiled out:
//   - non-arm64 platforms (no NEON kernel yet)
//   - -tags=opus_nosimd
//   - -tags=opus_strict (FCVTAS ties-away differs from ties-to-even)

func celtFloat2Int16SIMD(in *float32, out *opus_int16, cnt int) {
	_, _, _ = in, out, cnt
}

const float2int16SIMDAvailable = false
