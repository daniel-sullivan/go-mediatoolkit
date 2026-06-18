//go:build !(arm64 && !opus_nosimd && !opus_strict)

package nativeopus

// Fallback for the opus_limit2_checkwithin1 SIMD path when compiled
// out: non-arm64, -tags=opus_nosimd, or -tags=opus_strict.

func opusLimit2CheckWithin1SIMD(samples *float32, cnt int) int32 {
	_, _ = samples, cnt
	return 0
}

const limit2SIMDAvailable = false
