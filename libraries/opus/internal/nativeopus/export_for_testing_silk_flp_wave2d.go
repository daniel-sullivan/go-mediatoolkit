package nativeopus

// Thin exports for the SILK float (_FLP) mid-driver parity test — Phase 8
// Wave 2d. Covers silk_find_pred_coefs_FLP, which calls into
// silk_find_LTP_FLP, silk_quant_LTP_gains_FLP, silk_LTP_scale_ctrl_FLP,
// silk_LTP_analysis_filter_FLP, silk_scale_copy_vector_FLP,
// silk_find_LPC_FLP, silk_process_NLSFs_FLP and silk_residual_energy_FLP.
//
// The test uses a flat payload that mirrors the subset of
// silk_encoder_state_FLP / silk_encoder_control_FLP fields the driver
// actually reads or writes.

// SilkFindPredCoefsFLPPayload carries the full driver state.
type SilkFindPredCoefsFLPPayload struct {
	// --- sCmn fields read by the driver or its callees. ---
	SignalType           int8
	NbSubfr              int
	SubfrLength          int
	PredictLPCOrder      int
	LtpMemLength         int
	FirstFrameAfterReset int
	SumLogGainQ7In       int32
	SNRdBQ7              int
	PacketLossPerc       int
	NFramesPerPacket     int
	LBRRFlag             int8
	UseInterpolatedNLSFs int
	SpeechActivityQ8     int
	NLSFMSVQSurvivors    int
	WB                   bool // select psNLSF_CB: true→WB (order=16), false→NB_MB (order=10).
	NLSFInterpCoefQ2In   int8
	PrevNLSFqQ15         [MAX_LPC_ORDER]int16
	Arch                 int
	CondCoding           int

	// --- encoder control inputs. ---
	Gains         [MAX_NB_SUBFR]float32
	PitchL        [MAX_NB_SUBFR]int32
	CodingQuality float32

	// --- input buffers (backing slices + offsets). ---
	ResPitch    []float32
	ResPitchOff int
	X           []float32
	XOff        int

	// --- outputs. ---
	LTPCoef          [LTP_ORDER * MAX_NB_SUBFR]float32
	LTPIndex         [MAX_NB_SUBFR]int8
	PERIndex         int8
	SumLogGainQ7     int32
	LTPredCodGain    float32
	LTPScaleIndex    int8
	LTPScale         float32
	NLSFInterpCoefQ2 int8
	NLSFIndices      [MAX_LPC_ORDER + 1]int8
	PrevNLSFqOut     [MAX_LPC_ORDER]int16
	PredCoefA        [MAX_LPC_ORDER]float32
	PredCoefB        [MAX_LPC_ORDER]float32
	ResNrg           [MAX_NB_SUBFR]float32
}

// ExportTestSilkFindPredCoefsFLP runs silk_find_pred_coefs_FLP against a
// silk_encoder_state_FLP / silk_encoder_control_FLP synthesized from p.
func ExportTestSilkFindPredCoefsFLP(p SilkFindPredCoefsFLPPayload) SilkFindPredCoefsFLPPayload {
	var psEnc silk_encoder_state_FLP
	var psEncCtrl silk_encoder_control_FLP

	psEnc.sCmn.indices.signalType = opus_int8(p.SignalType)
	psEnc.sCmn.nb_subfr = opus_int(p.NbSubfr)
	psEnc.sCmn.subfr_length = opus_int(p.SubfrLength)
	psEnc.sCmn.predictLPCOrder = opus_int(p.PredictLPCOrder)
	psEnc.sCmn.ltp_mem_length = opus_int(p.LtpMemLength)
	psEnc.sCmn.first_frame_after_reset = opus_int(p.FirstFrameAfterReset)
	psEnc.sCmn.sum_log_gain_Q7 = opus_int32(p.SumLogGainQ7In)
	psEnc.sCmn.SNR_dB_Q7 = opus_int(p.SNRdBQ7)
	psEnc.sCmn.PacketLoss_perc = opus_int(p.PacketLossPerc)
	psEnc.sCmn.nFramesPerPacket = opus_int(p.NFramesPerPacket)
	psEnc.sCmn.LBRR_flag = opus_int8(p.LBRRFlag)
	psEnc.sCmn.useInterpolatedNLSFs = opus_int(p.UseInterpolatedNLSFs)
	psEnc.sCmn.speech_activity_Q8 = opus_int(p.SpeechActivityQ8)
	psEnc.sCmn.NLSF_MSVQ_Survivors = opus_int(p.NLSFMSVQSurvivors)
	psEnc.sCmn.indices.NLSFInterpCoef_Q2 = opus_int8(p.NLSFInterpCoefQ2In)
	if p.WB {
		psEnc.sCmn.psNLSF_CB = &silk_NLSF_CB_WB
	} else {
		psEnc.sCmn.psNLSF_CB = &silk_NLSF_CB_NB_MB
	}
	psEnc.sCmn.arch = p.Arch
	for i := 0; i < MAX_LPC_ORDER; i++ {
		psEnc.sCmn.prev_NLSFq_Q15[i] = opus_int16(p.PrevNLSFqQ15[i])
	}
	for i := 0; i < MAX_NB_SUBFR; i++ {
		psEncCtrl.Gains[i] = p.Gains[i]
		psEncCtrl.pitchL[i] = opus_int(p.PitchL[i])
	}
	psEncCtrl.coding_quality = p.CodingQuality

	silk_find_pred_coefs_FLP(&psEnc, &psEncCtrl,
		p.ResPitch, opus_int(p.ResPitchOff),
		p.X, opus_int(p.XOff),
		opus_int(p.CondCoding))

	out := p
	for i := 0; i < LTP_ORDER*MAX_NB_SUBFR; i++ {
		out.LTPCoef[i] = psEncCtrl.LTPCoef[i]
	}
	for i := 0; i < MAX_NB_SUBFR; i++ {
		out.LTPIndex[i] = int8(psEnc.sCmn.indices.LTPIndex[i])
		out.ResNrg[i] = psEncCtrl.ResNrg[i]
	}
	out.PERIndex = int8(psEnc.sCmn.indices.PERIndex)
	out.SumLogGainQ7 = int32(psEnc.sCmn.sum_log_gain_Q7)
	out.LTPredCodGain = psEncCtrl.LTPredCodGain
	out.LTPScaleIndex = int8(psEnc.sCmn.indices.LTP_scaleIndex)
	out.LTPScale = psEncCtrl.LTP_scale
	out.NLSFInterpCoefQ2 = int8(psEnc.sCmn.indices.NLSFInterpCoef_Q2)
	for i := 0; i < MAX_LPC_ORDER+1; i++ {
		out.NLSFIndices[i] = int8(psEnc.sCmn.indices.NLSFIndices[i])
	}
	for i := 0; i < MAX_LPC_ORDER; i++ {
		out.PrevNLSFqOut[i] = int16(psEnc.sCmn.prev_NLSFq_Q15[i])
		out.PredCoefA[i] = psEncCtrl.PredCoef[0][i]
		out.PredCoefB[i] = psEncCtrl.PredCoef[1][i]
	}
	return out
}
