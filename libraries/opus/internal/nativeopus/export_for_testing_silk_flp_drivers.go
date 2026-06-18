package nativeopus

// Thin exports for the SILK float mid-driver parity tests — Phase 8
// Wave 2. These drivers mutate larger chunks of encoder state, so the
// exports take full silk_encoder_state_FLP / silk_encoder_control_FLP
// structs across the boundary. The Go tests (which live in benchcmp)
// stick to primitive types, so we use ExportTestSilkEncoderStateFLP to
// marshal between a flat payload struct and the nested encoder state.

// SilkEncoderStateFLPPayload — minimal mirror of the encoder state
// fields that silk_process_gains_FLP reads/writes, plus any fields we
// need to seed.
type SilkEncoderStateFLPPayload struct {
	// Inputs / modifiable scalar fields on sCmn.
	SignalType             int8
	QuantOffsetType        int8
	NbSubfr                int
	SubfrLength            int
	SNR_dB_Q7              int
	NStatesDelayedDecision int
	InputTiltQ15           int
	SpeechActivityQ8       int
	// Shape state.
	LastGainIndex int8
	// Per-subframe I/O on psEncCtrl.
	Gains          [MAX_NB_SUBFR]float32
	ResNrg         [MAX_NB_SUBFR]float32
	LTPredCodGain  float32
	InputQuality   float32
	CodingQuality  float32
	GainsIndicesIn [MAX_NB_SUBFR]int8 // not used as input, just a place to read results
	// Outputs written (read back after call).
	Lambda            float32
	GainsUnqQ16       [MAX_NB_SUBFR]int32
	LastGainIndexPrev int8
	// CondCoding input to the driver.
	CondCoding int
}

// ExportTestSilkProcessGainsFLP runs silk_process_gains_FLP against a
// valid encoder state seeded from p, and returns the mutated fields.
func ExportTestSilkProcessGainsFLP(p SilkEncoderStateFLPPayload) SilkEncoderStateFLPPayload {
	var psEnc silk_encoder_state_FLP
	var psEncCtrl silk_encoder_control_FLP

	psEnc.sCmn.indices.signalType = opus_int8(p.SignalType)
	psEnc.sCmn.indices.quantOffsetType = opus_int8(p.QuantOffsetType)
	psEnc.sCmn.nb_subfr = opus_int(p.NbSubfr)
	psEnc.sCmn.subfr_length = opus_int(p.SubfrLength)
	psEnc.sCmn.SNR_dB_Q7 = opus_int(p.SNR_dB_Q7)
	psEnc.sCmn.nStatesDelayedDecision = opus_int(p.NStatesDelayedDecision)
	psEnc.sCmn.input_tilt_Q15 = opus_int(p.InputTiltQ15)
	psEnc.sCmn.speech_activity_Q8 = opus_int(p.SpeechActivityQ8)
	psEnc.sShape.LastGainIndex = opus_int8(p.LastGainIndex)
	for i := 0; i < MAX_NB_SUBFR; i++ {
		psEncCtrl.Gains[i] = p.Gains[i]
		psEncCtrl.ResNrg[i] = p.ResNrg[i]
	}
	psEncCtrl.LTPredCodGain = p.LTPredCodGain
	psEncCtrl.input_quality = p.InputQuality
	psEncCtrl.coding_quality = p.CodingQuality

	silk_process_gains_FLP(&psEnc, &psEncCtrl, opus_int(p.CondCoding))

	out := p
	out.SignalType = int8(psEnc.sCmn.indices.signalType)
	out.QuantOffsetType = int8(psEnc.sCmn.indices.quantOffsetType)
	out.LastGainIndex = int8(psEnc.sShape.LastGainIndex)
	for i := 0; i < MAX_NB_SUBFR; i++ {
		out.Gains[i] = psEncCtrl.Gains[i]
		out.GainsUnqQ16[i] = int32(psEncCtrl.GainsUnq_Q16[i])
		out.GainsIndicesIn[i] = int8(psEnc.sCmn.indices.GainsIndices[i])
	}
	out.Lambda = psEncCtrl.Lambda
	out.LastGainIndexPrev = int8(psEncCtrl.lastGainIndexPrev)
	return out
}

// -------------------- wrappers_FLP --------------------
// Note: ExportTestSilkA2NLSFFLP / ExportTestSilkNLSF2AFLP live in
// export_for_testing_silk_flp_wave2b.go since they're colocated with
// their existing find_LPC_FLP caller.

// ExportTestSilkProcessNLSFsFLP wraps silk_process_NLSFs_FLP.
// Inputs similar to ExportTestProcessNLSFs; returns PredCoef float arrays
// along with mutated nlsf and NLSFIndices.
func ExportTestSilkProcessNLSFsFLP(
	wb bool,
	speech_activity_Q8, useInterpolatedNLSFs, NLSFInterpCoef_Q2, signalType, nb_subfr, NLSF_MSVQ_Survivors int,
	nlsf, prev []int16,
) (predA, predB []float32, nlsfOut []int16, indices []int8) {
	var s silk_encoder_state
	s.speech_activity_Q8 = opus_int(speech_activity_Q8)
	s.useInterpolatedNLSFs = opus_int(useInterpolatedNLSFs)
	s.indices.NLSFInterpCoef_Q2 = opus_int8(NLSFInterpCoef_Q2)
	s.indices.signalType = opus_int8(signalType)
	s.nb_subfr = opus_int(nb_subfr)
	s.NLSF_MSVQ_Survivors = opus_int(NLSF_MSVQ_Survivors)
	if wb {
		s.psNLSF_CB = &silk_NLSF_CB_WB
	} else {
		s.psNLSF_CB = &silk_NLSF_CB_NB_MB
	}
	s.predictLPCOrder = opus_int(s.psNLSF_CB.order)

	pNLSF := make([]opus_int16, MAX_LPC_ORDER)
	for i := 0; i < len(nlsf) && i < MAX_LPC_ORDER; i++ {
		pNLSF[i] = opus_int16(nlsf[i])
	}
	prevQ := make([]opus_int16, MAX_LPC_ORDER)
	for i := 0; i < len(prev) && i < MAX_LPC_ORDER; i++ {
		prevQ[i] = opus_int16(prev[i])
	}

	var pc [2][MAX_LPC_ORDER]silk_float
	silk_process_NLSFs_FLP(&s, &pc, pNLSF, prevQ)

	predA = make([]float32, MAX_LPC_ORDER)
	predB = make([]float32, MAX_LPC_ORDER)
	for i := 0; i < MAX_LPC_ORDER; i++ {
		predA[i] = pc[0][i]
		predB[i] = pc[1][i]
	}
	nlsfOut = make([]int16, MAX_LPC_ORDER)
	for i := 0; i < MAX_LPC_ORDER; i++ {
		nlsfOut[i] = int16(pNLSF[i])
	}
	indices = make([]int8, MAX_LPC_ORDER+1)
	for i := 0; i <= MAX_LPC_ORDER; i++ {
		indices[i] = int8(s.indices.NLSFIndices[i])
	}
	return
}

// SilkQuantLTPGainsFLPOut bundles the outputs of silk_quant_LTP_gains_FLP.
type SilkQuantLTPGainsFLPOut struct {
	B              []float32 // MAX_NB_SUBFR * LTP_ORDER
	CbkIndex       []int8    // MAX_NB_SUBFR
	PeriodicityIdx int8
	SumLogGainQ7   int32
	PredGainDB     float32
}

// ExportTestSilkQuantLTPGainsFLP wraps silk_quant_LTP_gains_FLP.
func ExportTestSilkQuantLTPGainsFLP(
	XX []float32, // [nb_subfr * LTP_ORDER*LTP_ORDER]
	xX []float32, // [nb_subfr * LTP_ORDER]
	subfr_len, nb_subfr int,
	sumLogGainQ7 int32,
) SilkQuantLTPGainsFLPOut {
	B := make([]silk_float, MAX_NB_SUBFR*LTP_ORDER)
	cbk := make([]opus_int8, MAX_NB_SUBFR)
	periodicityIdx := opus_int8(0)
	s := opus_int32(sumLogGainQ7)
	var predGainDB silk_float

	// Pad XX / xX up to MAX_NB_SUBFR * ... sized but pass only nb_subfr worth.
	xxBuf := make([]silk_float, MAX_NB_SUBFR*LTP_ORDER*LTP_ORDER)
	copy(xxBuf, XX)
	xXBuf := make([]silk_float, MAX_NB_SUBFR*LTP_ORDER)
	copy(xXBuf, xX)

	silk_quant_LTP_gains_FLP(B, cbk, &periodicityIdx, &s, &predGainDB,
		xxBuf, xXBuf, opus_int(subfr_len), opus_int(nb_subfr), 0)

	out := SilkQuantLTPGainsFLPOut{
		B:              make([]float32, len(B)),
		CbkIndex:       make([]int8, len(cbk)),
		PeriodicityIdx: int8(periodicityIdx),
		SumLogGainQ7:   int32(s),
		PredGainDB:     predGainDB,
	}
	for i, v := range B {
		out.B[i] = v
	}
	for i, v := range cbk {
		out.CbkIndex[i] = int8(v)
	}
	return out
}

// -------------------- NSQ_wrapper_FLP --------------------

// SilkNSQWrapperFLPPayload carries all the per-frame state that the NSQ
// wrapper reads/writes, plus the public-facing x[] / pulses[] buffers.
type SilkNSQWrapperFLPPayload struct {
	// Encoder state (sCmn) relevant fields.
	SignalType             int8
	QuantOffsetType        int8
	LTPScaleIndex          int8
	Seed                   int8
	NLSFInterpCoefQ2       int8
	PERIndex               int8
	NbSubfr                int
	FrameLength            int
	SubfrLength            int
	LtpMemLength           int
	ShapingLPCOrder        int
	PredictLPCOrder        int
	NStatesDelayedDecision int
	WarpingQ16             int
	Arch                   int

	// Encoder control fields (all quantized after marshalling).
	AR            [MAX_NB_SUBFR * MAX_SHAPE_LPC_ORDER]float32
	LFMAShp       [MAX_NB_SUBFR]float32
	LFARShp       [MAX_NB_SUBFR]float32
	Tilt          [MAX_NB_SUBFR]float32
	HarmShapeGain [MAX_NB_SUBFR]float32
	Lambda        float32
	LTPCoef       [LTP_ORDER * MAX_NB_SUBFR]float32
	PredCoef      [2][MAX_LPC_ORDER]float32
	Gains         [MAX_NB_SUBFR]float32
	PitchL        [MAX_NB_SUBFR]int32

	// NSQ state (mostly zero for a fresh state but seeded for parity).
	NSQRandSeed      int32
	NSQLagPrev       int32
	NSQPrevGainQ16   int32
	NSQSLTPBufIdx    int32
	NSQSLTPShpBufIdx int32
	NSQRewhiteFlag   int32
	NSQSLFARShpQ14   int32
	NSQSDiffShpQ14   int32

	// Input signal (float); outputs: quantized pulses.
	X      []float32
	Pulses []int8

	// Outputs (post-call mirrors).
	OutNSQRandSeed      int32
	OutNSQLagPrev       int32
	OutNSQPrevGainQ16   int32
	OutNSQSLTPBufIdx    int32
	OutNSQSLTPShpBufIdx int32
	OutNSQRewhiteFlag   int32
	OutNSQSLFARShpQ14   int32
	OutNSQSDiffShpQ14   int32
}

// ExportTestSilkNSQWrapperFLP runs silk_NSQ_wrapper_FLP.
func ExportTestSilkNSQWrapperFLP(p SilkNSQWrapperFLPPayload) SilkNSQWrapperFLPPayload {
	var psEnc silk_encoder_state_FLP
	var psEncCtrl silk_encoder_control_FLP
	var psIndices SideInfoIndices
	var psNSQ silk_nsq_state

	psEnc.sCmn.nb_subfr = opus_int(p.NbSubfr)
	psEnc.sCmn.frame_length = opus_int(p.FrameLength)
	psEnc.sCmn.subfr_length = opus_int(p.SubfrLength)
	psEnc.sCmn.ltp_mem_length = opus_int(p.LtpMemLength)
	psEnc.sCmn.shapingLPCOrder = opus_int(p.ShapingLPCOrder)
	psEnc.sCmn.predictLPCOrder = opus_int(p.PredictLPCOrder)
	psEnc.sCmn.nStatesDelayedDecision = opus_int(p.NStatesDelayedDecision)
	psEnc.sCmn.warping_Q16 = opus_int(p.WarpingQ16)
	psEnc.sCmn.arch = p.Arch

	psIndices.signalType = opus_int8(p.SignalType)
	psIndices.quantOffsetType = opus_int8(p.QuantOffsetType)
	psIndices.LTP_scaleIndex = opus_int8(p.LTPScaleIndex)
	psIndices.Seed = opus_int8(p.Seed)
	psIndices.NLSFInterpCoef_Q2 = opus_int8(p.NLSFInterpCoefQ2)
	psIndices.PERIndex = opus_int8(p.PERIndex)

	for i := 0; i < MAX_NB_SUBFR*MAX_SHAPE_LPC_ORDER; i++ {
		psEncCtrl.AR[i] = p.AR[i]
	}
	for i := 0; i < MAX_NB_SUBFR; i++ {
		psEncCtrl.LF_MA_shp[i] = p.LFMAShp[i]
		psEncCtrl.LF_AR_shp[i] = p.LFARShp[i]
		psEncCtrl.Tilt[i] = p.Tilt[i]
		psEncCtrl.HarmShapeGain[i] = p.HarmShapeGain[i]
		psEncCtrl.Gains[i] = p.Gains[i]
		psEncCtrl.pitchL[i] = opus_int(p.PitchL[i])
	}
	psEncCtrl.Lambda = p.Lambda
	for i := 0; i < LTP_ORDER*MAX_NB_SUBFR; i++ {
		psEncCtrl.LTPCoef[i] = p.LTPCoef[i]
	}
	for j := 0; j < 2; j++ {
		for i := 0; i < MAX_LPC_ORDER; i++ {
			psEncCtrl.PredCoef[j][i] = p.PredCoef[j][i]
		}
	}

	psNSQ.rand_seed = opus_int32(p.NSQRandSeed)
	psNSQ.lagPrev = opus_int(p.NSQLagPrev)
	psNSQ.prev_gain_Q16 = opus_int32(p.NSQPrevGainQ16)
	psNSQ.sLTP_buf_idx = opus_int(p.NSQSLTPBufIdx)
	psNSQ.sLTP_shp_buf_idx = opus_int(p.NSQSLTPShpBufIdx)
	psNSQ.rewhite_flag = opus_int(p.NSQRewhiteFlag)
	psNSQ.sLF_AR_shp_Q14 = opus_int32(p.NSQSLFARShpQ14)
	psNSQ.sDiff_shp_Q14 = opus_int32(p.NSQSDiffShpQ14)

	pulses := make([]opus_int8, len(p.Pulses))
	copy(pulses, toOpusInt8(p.Pulses))

	silk_NSQ_wrapper_FLP(&psEnc, &psEncCtrl, &psIndices, &psNSQ, pulses, p.X)

	out := p
	out.Pulses = make([]int8, len(pulses))
	for i, v := range pulses {
		out.Pulses[i] = int8(v)
	}
	out.SignalType = int8(psIndices.signalType)
	out.QuantOffsetType = int8(psIndices.quantOffsetType)
	out.LTPScaleIndex = int8(psIndices.LTP_scaleIndex)
	out.Seed = int8(psIndices.Seed)
	out.OutNSQRandSeed = int32(psNSQ.rand_seed)
	out.OutNSQLagPrev = int32(psNSQ.lagPrev)
	out.OutNSQPrevGainQ16 = int32(psNSQ.prev_gain_Q16)
	out.OutNSQSLTPBufIdx = int32(psNSQ.sLTP_buf_idx)
	out.OutNSQSLTPShpBufIdx = int32(psNSQ.sLTP_shp_buf_idx)
	out.OutNSQRewhiteFlag = int32(psNSQ.rewhite_flag)
	out.OutNSQSLFARShpQ14 = int32(psNSQ.sLF_AR_shp_Q14)
	out.OutNSQSDiffShpQ14 = int32(psNSQ.sDiff_shp_Q14)
	return out
}

func toOpusInt8(s []int8) []opus_int8 {
	out := make([]opus_int8, len(s))
	for i, v := range s {
		out[i] = opus_int8(v)
	}
	return out
}
