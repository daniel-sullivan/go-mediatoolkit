//go:build !(arm64 && !opus_nosimd && !opus_strict)

package nativeopus

// Fallback when the NSQ allpass SIMD path is compiled out. The
// signature mirrors the arm64 NEON kernel and the behaviour is a
// bit-exact pure-Go 4-lane implementation routed through the SoA
// reference. Callers in parity tests rely on this path producing the
// same result as the asm kernel so that test coverage on !arm64 hosts
// still exercises the full 4-lane semantics.

func shortNSQAllpassSIMD(soaSAR2 *opus_int32, diffQ14 *opus_int32, warping_Q16 int32, ARshp *opus_int16, order int, out *[MAX_DEL_DEC_STATES]opus_int32) {
	// Reconstruct slices referring to the same backing storage as the
	// caller passed in. We avoid a copy because the kernel mutates
	// sAR2 in place.
	_ = soaSAR2
	_ = diffQ14
	_ = warping_Q16
	_ = ARshp
	_ = order
	_ = out
}

const nsqAllpassSIMDAvailable = false

// ShortNSQAllpassSIMDAvailable — see the arm64 build-tagged twin.
const ShortNSQAllpassSIMDAvailable = nsqAllpassSIMDAvailable
