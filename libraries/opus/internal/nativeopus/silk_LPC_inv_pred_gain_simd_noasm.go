//go:build !(arm64 && !opus_nosimd && !opus_strict)

package nativeopus

// Fallback when the silk_LPC_inverse_pred_gain NEON asm path is
// compiled out (non-arm64, opus_nosimd, or opus_strict). The public
// entry point silk_LPC_inverse_pred_gain_c falls back to the scalar
// kernel directly; this file only supplies the compile-time
// availability flag and an unreachable stub so the arm64 .s file's
// Go-side declaration has a consistent signature when referenced from
// tests.

const silkLPCInvPredGainSIMDAvailable = false

// silk_LPC_inverse_pred_gain_QA_simd is unreachable here; dispatch in
// silk_LPC_inverse_pred_gain.go gates on silkLPCInvPredGainSIMDAvailable.
func silk_LPC_inverse_pred_gain_QA_simd(A_QA []opus_int32, order opus_int) opus_int32 {
	return silk_LPC_inverse_pred_gain_QA_c(A_QA, order)
}
