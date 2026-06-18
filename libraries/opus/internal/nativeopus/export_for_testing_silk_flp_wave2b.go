package nativeopus

// Thin exports for SILK float (_FLP) mid-driver parity tests — Phase 8 Wave 2b.
// Covers find_LPC_FLP and find_LTP_FLP.

// ExportTestSilkFindLTPFLP exercises silk_find_LTP_FLP.
//
// Callers pass `rBuf` as the backing slice and `rOff` as the absolute
// starting index for r_ptr; `rOff - max(lag[k]) - LTP_ORDER/2 >= 0`
// must hold. Returns the XX and xX output arrays.
func ExportTestSilkFindLTPFLP(
	rBuf []float32, rOff int, lag []int,
	subfr_length, nb_subfr int,
) (XX []float32, xX []float32) {
	XX = make([]float32, nb_subfr*LTP_ORDER*LTP_ORDER)
	xX = make([]float32, nb_subfr*LTP_ORDER)
	lg := make([]opus_int, len(lag))
	for i, v := range lag {
		lg[i] = opus_int(v)
	}
	silk_find_LTP_FLP(XX, xX, rBuf, opus_int(rOff), lg,
		opus_int(subfr_length), opus_int(nb_subfr), 0)
	return
}

// ExportTestSilkA2NLSFFLP — convert AR coefficients to NLSF (float wrapper).
func ExportTestSilkA2NLSFFLP(pAR []float32) []int16 {
	NLSF_Q15 := make([]opus_int16, len(pAR))
	silk_A2NLSF_FLP(NLSF_Q15, pAR, opus_int(len(pAR)))
	out := make([]int16, len(pAR))
	for i, v := range NLSF_Q15 {
		out[i] = int16(v)
	}
	return out
}

// ExportTestSilkNLSF2AFLP — convert NLSFs to AR coefficients (float wrapper).
func ExportTestSilkNLSF2AFLP(NLSF []int16) []float32 {
	nl := make([]opus_int16, len(NLSF))
	for i, v := range NLSF {
		nl[i] = opus_int16(v)
	}
	pAR := make([]float32, len(NLSF))
	silk_NLSF2A_FLP(pAR, nl, opus_int(len(NLSF)), 0)
	return pAR
}

// FindLPCInput packages the driver inputs for silk_find_LPC_FLP.
type FindLPCInput struct {
	PredictLPCOrder         int
	NbSubfr                 int
	SubfrLength             int
	UseInterpolatedNLSFs    int
	FirstFrameAfterReset    int
	PrevNLSFqQ15            []int16 // length MAX_LPC_ORDER
	InitialNLSFInterpCoefQ2 int8
	X                       []float32
	MinInvGain              float32
}

// FindLPCOutput captures all observable outputs of silk_find_LPC_FLP.
type FindLPCOutput struct {
	NLSFQ15          []int16 // length MAX_LPC_ORDER
	NLSFInterpCoefQ2 int8
}

// ExportTestSilkFindLPCFLP exercises silk_find_LPC_FLP via a minimal
// silk_encoder_state with just the fields it reads/writes populated.
func ExportTestSilkFindLPCFLP(in FindLPCInput) FindLPCOutput {
	var s silk_encoder_state
	s.predictLPCOrder = opus_int(in.PredictLPCOrder)
	s.nb_subfr = opus_int(in.NbSubfr)
	s.subfr_length = opus_int(in.SubfrLength)
	s.useInterpolatedNLSFs = opus_int(in.UseInterpolatedNLSFs)
	s.first_frame_after_reset = opus_int(in.FirstFrameAfterReset)
	for i, v := range in.PrevNLSFqQ15 {
		if i < MAX_LPC_ORDER {
			s.prev_NLSFq_Q15[i] = opus_int16(v)
		}
	}
	s.indices.NLSFInterpCoef_Q2 = opus_int8(in.InitialNLSFInterpCoefQ2)
	s.arch = 0

	NLSF_Q15 := make([]opus_int16, MAX_LPC_ORDER)
	silk_find_LPC_FLP(&s, NLSF_Q15, in.X, silk_float(in.MinInvGain), 0)

	out := FindLPCOutput{
		NLSFQ15:          make([]int16, MAX_LPC_ORDER),
		NLSFInterpCoefQ2: int8(s.indices.NLSFInterpCoef_Q2),
	}
	for i, v := range NLSF_Q15 {
		out.NLSFQ15[i] = int16(v)
	}
	return out
}
