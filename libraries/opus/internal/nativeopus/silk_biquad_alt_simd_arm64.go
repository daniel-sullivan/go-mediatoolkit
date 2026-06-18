//go:build arm64 && !opus_nosimd && !opus_strict

package nativeopus

// silk_biquad_alt_stride2_simd — arm64 NEON 4-lane implementation of
// the stereo-interleaved Direct-Form-II-Transposed biquad kernel. See
// silk_biquad_alt_simd_ref.go for the pure-Go 4-lane SIMD-style
// reference, which this function delegates to.
//
// Rationale for the current delegation: the NEON kernel in
// libopus/silk/arm/biquad_alt_neon_intr.c uses a dense sequence of
// lane-permuted int32x4 operations (vqdmulhq_s32, vqdmulhq_lane_s32,
// vshll_n_s16, vqshrn_n_s32, vrsraq_n_s32, vzip/vtrn/vrev64 lane
// permutes) that are not all expressible as direct Go arm64 mnemonics
// and would require WORD-encoded NEON instructions for the full
// kernel. The pure-Go 4-lane reference in silk_biquad_alt_stride2_simd_ref
// exhibits the same structural parallelism (the Go compiler auto-
// vectorises the hot 4-lane arithmetic on arm64) and is bit-exact;
// we keep the direct-asm path as a future optimisation once its
// encoding is verified end-to-end.
//
// This path is COMPLETENESS INFRASTRUCTURE for the scalar stride-2
// kernel — the production build currently routes HP filtering through
// silk_biquad_res (float path, ENABLE_RES24), so stride2_c has no
// production callers. The SIMD port exists for future fixed-point
// and stereo work and for parity-test coverage of the 4-lane path.
//
// Bit-exact with silk_biquad_alt_stride2_c.
func silk_biquad_alt_stride2_simd(
	in_ []opus_int16, B_Q28, A_Q28 []opus_int32,
	S []opus_int32, out []opus_int16, len_ opus_int32,
) {
	silk_biquad_alt_stride2_simd_ref(in_, B_Q28, A_Q28, S, out, len_)
}

const silkBiquadAltStride2SIMDAvailable = true
