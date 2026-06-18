package nativeopus

// Thin exports for SILK LPC-helper parity tests.

func ExportTestSilkBWExpander(ar []int16, chirp_Q16 int32) []int16 {
	silk_bwexpander(ar, opus_int(len(ar)), chirp_Q16)
	return ar
}
func ExportTestSilkBWExpander32(ar []int32, chirp_Q16 int32) []int32 {
	silk_bwexpander_32(ar, opus_int(len(ar)), chirp_Q16)
	return ar
}
func ExportTestSilkLPCFit(aIn []int32, QOUT, QIN int) ([]int16, []int32) {
	aOut := make([]int16, len(aIn))
	aCopy := append([]int32(nil), aIn...)
	silk_LPC_fit(aOut, aCopy, opus_int(QOUT), opus_int(QIN), opus_int(len(aCopy)))
	return aOut, aCopy
}
func ExportTestSilkInterpolate(x0, x1 []int16, ifact_Q2 int) []int16 {
	xi := make([]int16, len(x0))
	silk_interpolate(xi, x0, x1, opus_int(ifact_Q2), opus_int(len(x0)))
	return xi
}
func ExportTestSilkLPCInversePredGain(A_Q12 []int16) int32 {
	return silk_LPC_inverse_pred_gain_c(A_Q12, opus_int(len(A_Q12)))
}

// ExportTestSilkLPCAnalysisFilter — in_ must contain d samples of
// history before the signal proper. Returns the output slice of length
// len(in_).
func ExportTestSilkLPCAnalysisFilter(in_, B []int16, d int) []int16 {
	out := make([]int16, len(in_))
	silk_LPC_analysis_filter(out, in_, B, int32(len(in_)), int32(d), 0)
	return out
}
