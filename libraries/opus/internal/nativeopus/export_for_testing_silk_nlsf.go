package nativeopus

// Exports for SILK NLSF parity tests.

// pickCB returns &silk_NLSF_CB_NB_MB when wb is false, &silk_NLSF_CB_WB otherwise.
func pickCB(wb bool) *silk_NLSF_CB_struct {
	if wb {
		return &silk_NLSF_CB_WB
	}
	return &silk_NLSF_CB_NB_MB
}

func ExportTestSilkNLSFVQWeightsLaroia(pNLSF []int16) []int16 {
	w := make([]int16, len(pNLSF))
	silk_NLSF_VQ_weights_laroia(w, pNLSF, opus_int(len(pNLSF)))
	return w
}

func ExportTestSilkNLSFStabilize(NLSF, NDeltaMin []int16) []int16 {
	out := append([]int16(nil), NLSF...)
	silk_NLSF_stabilize(out, NDeltaMin, opus_int(len(out)))
	return out
}

func ExportTestSilkNLSFDecode(idx []int8, wb bool) []int16 {
	cb := pickCB(wb)
	out := make([]int16, cb.order)
	silk_NLSF_decode(out, idx, cb)
	return out
}

func ExportTestSilkNLSFEncode(pNLSF []int16, wb bool, mu, nSurvivors, signalType int) (int32, []int8, []int16) {
	cb := pickCB(wb)
	idx := make([]int8, int(cb.order)+1)
	nlsfCopy := append([]int16(nil), pNLSF...)
	pW_Q2 := make([]int16, cb.order)
	// Compute Laroia weights as input (matches encoder usage).
	silk_NLSF_VQ_weights_laroia(pW_Q2, nlsfCopy, opus_int(cb.order))
	rd := silk_NLSF_encode(idx, nlsfCopy, cb, pW_Q2, opus_int(mu), opus_int(nSurvivors), opus_int(signalType))
	return rd, idx, nlsfCopy
}

func ExportTestSilkA2NLSF(a_Q16 []int32, d int) ([]int16, []int32) {
	NLSF := make([]int16, d)
	aCopy := append([]int32(nil), a_Q16...)
	silk_A2NLSF(NLSF, aCopy, opus_int(d))
	return NLSF, aCopy
}

func ExportTestSilkNLSF2A(NLSF []int16, d int) []int16 {
	out := make([]int16, d)
	silk_NLSF2A(out, NLSF, opus_int(d), 0)
	return out
}
