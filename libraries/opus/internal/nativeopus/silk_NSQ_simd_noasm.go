//go:build !(arm64 && !opus_nosimd && !opus_strict)

package nativeopus

// Fallback when the NSQ SIMD path is compiled out.

func shortPredictionSoASIMD(sLPCAtBase *opus_int32, coef16 *opus_int16, order opus_int, out *[MAX_DEL_DEC_STATES]opus_int32) {
	_, _, _, _ = sLPCAtBase, coef16, order, out
}

const nsqSIMDAvailable = false
