//go:build arm64 && !opus_nosimd && !opus_strict

package nativeopus

// shortNSQAllpassSIMD — arm64 NEON 4-lane parallel implementation of
// the noise-shape feedback allpass chain inside the delayed-decision
// quantizer's per-sample inner loop. See silk_NSQ_allpass_simd_arm64.s
// for the encoding notes.
//
// Bit-exact (in the unsaturated int32 domain) with
// silk_noise_shape_allpass_soa, and hence with four serial copies of
// the scalar reference at silk_NSQ_del_dec.go:403-421.
//
// Arguments:
//   - soaSAR2   pointer to soa.sAR2_Q14[0][0] — treats sAR2_Q14 as a
//     flat array of length shapingLPCOrder * MAX_DEL_DEC_STATES with
//     lane-innermost layout. The kernel both reads and writes this
//     array in place.
//   - diffQ14   pointer to soa.Diff_Q14[0] — 4-lane int32 read only.
//   - warping_Q16 scalar warping coefficient (only its low 16 bits
//     are consumed, matching silk_SMLAWB semantics).
//   - ARshp     pointer to AR_shp_Q13[0] — shapingLPCOrder int16s.
//   - order     shapingLPCOrder (must be even and >= 2).
//   - out       pointer to a 4-lane int32 write-only buffer that
//     receives the per-lane n_AR_Q14 accumulator.
//
//go:noescape
func shortNSQAllpassSIMD(soaSAR2 *opus_int32, diffQ14 *opus_int32, warping_Q16 int32, ARshp *opus_int16, order int, out *[MAX_DEL_DEC_STATES]opus_int32)

const nsqAllpassSIMDAvailable = true

// ShortNSQAllpassSIMDAvailable is the compile-time flag that reports
// whether the 4-lane allpass SIMD kernel is linked into the build.
// Exposed as a const so callers downstream can statically gate
// future-fusion code paths that intend to invoke shortNSQAllpassSIMD
// directly.
const ShortNSQAllpassSIMDAvailable = nsqAllpassSIMDAvailable
