//go:build arm64 && !opus_nosimd && !opus_strict

package nativeopus

// shortPredictionSoASIMD — arm64 NEON 4-lane parallel implementation
// of the SoA LPC short-prediction. See silk_NSQ_simd_arm64.s for
// encoding notes. Bit-exact with the pure-Go SoA (and thus with 4
// serial silk_SMLAWB chains) in the unsaturated int32 domain.
//
//go:noescape
func shortPredictionSoASIMD(sLPCAtBase *opus_int32, coef16 *opus_int16, order opus_int, out *[MAX_DEL_DEC_STATES]opus_int32)

const nsqSIMDAvailable = true
