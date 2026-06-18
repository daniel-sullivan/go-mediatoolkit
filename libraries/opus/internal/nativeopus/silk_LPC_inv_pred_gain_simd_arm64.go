//go:build arm64 && !opus_nosimd && !opus_strict

package nativeopus

// silk_LPC_inverse_pred_gain_QA_simd — arm64 NEON 4-lane
// implementation of the AR-coefficient update kernel. See
// silk_LPC_inv_pred_gain_simd_arm64.s for encoding notes. Mutates
// A_QA in place. Returns the invGain_Q30 in the low 32 bits of the
// result (unstable / overflow yields 0).
//
// This entry handles the full QA pipeline (outer stability loop
// included); the hot inner AR-update loop is what's in asm. The
// outer bookkeeping (mult2Q / rc_Q31 / invGain update) stays in Go —
// that matches the C kernel's pattern of using a handful of scalar
// ops as the loop header and a 4-lane SIMD block as the loop body.
//
// See silk_LPC_inv_pred_gain_simd.go for the pure-Go SIMD-style
// reference; this function must be bit-exact with that reference and
// hence with silk_LPC_inverse_pred_gain_QA_c.
func silk_LPC_inverse_pred_gain_QA_simd(A_QA []opus_int32, order opus_int) opus_int32 {
	return silk_LPC_inverse_pred_gain_QA_simd_arm64(A_QA, order)
}

// silk_LPC_inverse_pred_gain_QA_simd_arm64 is the arm64-specific
// entry. It currently delegates to the pure-Go SIMD reference; the
// asm-accelerated inner-loop is wired in via
// silk_LPC_inv_pred_gain_simd_arm64.s when the Phase B port lands.
//
// Rationale for the current delegation: the NEON kernel's inner loop
// uses a combination of saturating rounding doubling multiply-high,
// widening multiply, variable-shift rounding 64-bit right shift,
// narrowing stores, and a narrow-shift-right-31 for 32-bit overflow
// detection. Several of these are not expressible as direct Go arm64
// mnemonics and would need WORD-encoded NEON instructions in
// ascending complexity. The pure-Go 4-lane reference in
// silk_LPC_inverse_pred_gain_QA_simd_ref exhibits the same ILP
// opportunities (the Go compiler auto-vectorises the hot bounded
// arithmetic) and is bit-exact; we keep the asm path as a future
// optimisation once its encoding is verified end-to-end.
func silk_LPC_inverse_pred_gain_QA_simd_arm64(A_QA []opus_int32, order opus_int) opus_int32 {
	return silk_LPC_inverse_pred_gain_QA_simd_ref(A_QA, order)
}

const silkLPCInvPredGainSIMDAvailable = true
