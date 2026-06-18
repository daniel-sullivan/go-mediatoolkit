package nativeopus

// Thin exports for the SILK LPC inverse-prediction-gain SIMD parity
// tests. See silk_LPC_inv_pred_gain_simd.go for the pure-Go SIMD-style
// reference and silk_LPC_inv_pred_gain_simd_arm64.* for the NEON asm
// port (when enabled).

// ExportTestSilkLPCInversePredGainQASIMDRef — run the pure-Go
// 4-lane SIMD-style reference kernel on A_QA (in-place), returning
// the Q30 inverse gain. The caller supplies A_QA already shifted into
// QA domain; it is mutated in place (matching the C kernel's
// signature).
func ExportTestSilkLPCInversePredGainQASIMDRef(A_QA []int32, order int) int32 {
	buf := make([]opus_int32, len(A_QA))
	for i, v := range A_QA {
		buf[i] = opus_int32(v)
	}
	out := silk_LPC_inverse_pred_gain_QA_simd_ref(buf, opus_int(order))
	for i := range buf {
		A_QA[i] = int32(buf[i])
	}
	return int32(out)
}

// ExportTestSilkLPCInversePredGainSIMDRef — Q12 entry point that
// mirrors silk_LPC_inverse_pred_gain_c but routed through the
// SIMD-style reference kernel.
func ExportTestSilkLPCInversePredGainSIMDRef(A_Q12 []int16) int32 {
	return int32(silk_LPC_inverse_pred_gain_simd_ref(
		opusInt16Slice(A_Q12), opus_int(len(A_Q12))))
}

// ExportTestSilkLPCInversePredGainSIMDAvailable — true when the
// compile-time NEON asm path is wired in.
func ExportTestSilkLPCInversePredGainSIMDAvailable() bool {
	return silkLPCInvPredGainSIMDAvailable
}

// ExportTestSilkLPCInversePredGainArch — go through the arch-aware
// dispatcher: SIMD asm when compiled in, scalar C otherwise. Same
// signature as silk_LPC_inverse_pred_gain_c.
func ExportTestSilkLPCInversePredGainArch(A_Q12 []int16) int32 {
	return int32(silk_LPC_inverse_pred_gain_c(opusInt16Slice(A_Q12), opus_int(len(A_Q12))))
}

func opusInt16Slice(in []int16) []opus_int16 {
	out := make([]opus_int16, len(in))
	for i, v := range in {
		out[i] = opus_int16(v)
	}
	return out
}
