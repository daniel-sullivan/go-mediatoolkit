//go:build !(arm64 && !opus_nosimd && !opus_strict)

package nativeopus

// Fallback when the silk_biquad_alt_stride2 SIMD path is compiled out
// (non-arm64, opus_nosimd, or opus_strict). The dispatch in the test
// export gates on silkBiquadAltStride2SIMDAvailable and falls back to
// the scalar kernel directly; this file only supplies the compile-
// time availability flag and a signature-compatible stub that
// delegates to the scalar reference so cross-platform tests can still
// reference the SIMD entry point without a link error.

const silkBiquadAltStride2SIMDAvailable = false

// silk_biquad_alt_stride2_simd — unreachable under the test dispatch
// on non-SIMD builds; kept here so the arm64 build-tagged twin has a
// matching signature everywhere.
func silk_biquad_alt_stride2_simd(
	in_ []opus_int16, B_Q28, A_Q28 []opus_int32,
	S []opus_int32, out []opus_int16, len_ opus_int32,
) {
	silk_biquad_alt_stride2_c(in_, B_Q28, A_Q28, S, out, len_)
}
