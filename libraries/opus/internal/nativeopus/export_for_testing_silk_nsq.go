package nativeopus

// Parity test exports for silk_control_codec.go, silk_NSQ.go and
// silk_NSQ_del_dec.go.

// ExportTestSilkSetupComplexity drives silk_setup_complexity on a
// minimal silk_encoder_state and returns the fields it mutates.
type SilkComplexityOut struct {
	PitchEstimationComplexity    int
	PitchEstimationThreshold_Q16 int32
	PitchEstimationLPCOrder      int
	ShapingLPCOrder              int
	LaShape                      int
	NStatesDelayedDecision       int
	UseInterpolatedNLSFs         int
	NLSF_MSVQ_Survivors          int
	Warping_Q16                  int
	ShapeWinLength               int
	Complexity                   int
}

func ExportTestSilkSetupComplexity(fs_kHz, predictLPCOrder, complexity int) SilkComplexityOut {
	var s silk_encoder_state
	s.fs_kHz = opus_int(fs_kHz)
	s.predictLPCOrder = opus_int(predictLPCOrder)
	silk_setup_complexity(&s, opus_int(complexity))
	return SilkComplexityOut{
		PitchEstimationComplexity:    int(s.pitchEstimationComplexity),
		PitchEstimationThreshold_Q16: int32(s.pitchEstimationThreshold_Q16),
		PitchEstimationLPCOrder:      int(s.pitchEstimationLPCOrder),
		ShapingLPCOrder:              int(s.shapingLPCOrder),
		LaShape:                      int(s.la_shape),
		NStatesDelayedDecision:       int(s.nStatesDelayedDecision),
		UseInterpolatedNLSFs:         int(s.useInterpolatedNLSFs),
		NLSF_MSVQ_Survivors:          int(s.NLSF_MSVQ_Survivors),
		Warping_Q16:                  int(s.warping_Q16),
		ShapeWinLength:               int(s.shapeWinLength),
		Complexity:                   int(s.Complexity),
	}
}

// ExportTestSilkSetupLBRR exercises silk_setup_LBRR with a caller-
// supplied encoder state + encControl, returning the LBRR outputs.
func ExportTestSilkSetupLBRR(prevEnabled, coded int, packetLossPerc int) (enabled int, gainIncreases int, ret int) {
	var s silk_encoder_state
	s.LBRR_enabled = opus_int(prevEnabled)
	s.PacketLoss_perc = opus_int(packetLossPerc)
	ec := silk_EncControlStruct{LBRR_coded: opus_int(coded)}
	r := silk_setup_LBRR(&s, &ec)
	return int(s.LBRR_enabled), int(s.LBRR_GainIncreases), int(r)
}

// ExportTestSilkSetupFS covers silk_setup_fs on a fresh FLP state.
// We return the integer-side fields it touches.
type SilkSetupFsOut struct {
	Ret              int
	NFramesPerPacket int
	NbSubfr          int
	FrameLength      int
	PitchLPCWinLen   int
	SubfrLength      int
	LtpMemLength     int
	LaPitch          int
	MaxPitchLag      int
	PredictLPCOrder  int
	FsKHz            int
	PrevLag          int
	FirstFrameReset  int
	LastGainIndex    int
	LagPrev          int
	PrevGainQ16      int32
	PrevSignalType   int
	InputBufIx       int
	NFramesEncoded   int
	TargetRateBps    int32
	PacketSizeMs     int
	// Count of pitch_contour_iCDF / pitch_lag_low_bits_iCDF contents (size 1..3 entries fine).
}

func ExportTestSilkSetupFS(curFsKHz, curNbSubfr, curPacketSize int, newFsKHz, packetSizeMs int) SilkSetupFsOut {
	var s silk_encoder_state_FLP
	s.sCmn.fs_kHz = opus_int(curFsKHz)
	s.sCmn.nb_subfr = opus_int(curNbSubfr)
	s.sCmn.PacketSize_ms = opus_int(curPacketSize)
	// Preinitialize subfr_length/frame_length so the celt_assert at the
	// end of setup_fs holds on the no-fs-change path.
	s.sCmn.subfr_length = 5 * opus_int(curFsKHz)
	s.sCmn.frame_length = s.sCmn.subfr_length * opus_int(curNbSubfr)
	r := silk_setup_fs(&s, opus_int(newFsKHz), opus_int(packetSizeMs))
	return SilkSetupFsOut{
		Ret:              int(r),
		NFramesPerPacket: int(s.sCmn.nFramesPerPacket),
		NbSubfr:          int(s.sCmn.nb_subfr),
		FrameLength:      int(s.sCmn.frame_length),
		PitchLPCWinLen:   int(s.sCmn.pitch_LPC_win_length),
		SubfrLength:      int(s.sCmn.subfr_length),
		LtpMemLength:     int(s.sCmn.ltp_mem_length),
		LaPitch:          int(s.sCmn.la_pitch),
		MaxPitchLag:      int(s.sCmn.max_pitch_lag),
		PredictLPCOrder:  int(s.sCmn.predictLPCOrder),
		FsKHz:            int(s.sCmn.fs_kHz),
		PrevLag:          int(s.sCmn.prevLag),
		FirstFrameReset:  int(s.sCmn.first_frame_after_reset),
		LastGainIndex:    int(s.sShape.LastGainIndex),
		LagPrev:          int(s.sCmn.sNSQ.lagPrev),
		PrevGainQ16:      int32(s.sCmn.sNSQ.prev_gain_Q16),
		PrevSignalType:   int(s.sCmn.prevSignalType),
		InputBufIx:       int(s.sCmn.inputBufIx),
		NFramesEncoded:   int(s.sCmn.nFramesEncoded),
		TargetRateBps:    int32(s.sCmn.TargetRate_bps),
		PacketSizeMs:     int(s.sCmn.PacketSize_ms),
	}
}

// SilkNSQIO captures the state fields both the Go and C sides mutate
// during a silk_NSQ_c call, plus the pulses output.
type SilkNSQIO struct {
	Pulses           []int8
	XQ               []int16 // first frame_length samples of NSQ.xq
	SLTP_shp_Q14     []int32 // first frame_length samples
	SLPC_Q14         []int32 // NSQ_LPC_BUF_LENGTH entries
	SAR2_Q14         []int32
	SLF_AR_shp_Q14   int32
	SDiff_shp_Q14    int32
	LagPrev          int
	SLTP_buf_idx     int
	SLTP_shp_buf_idx int
	RandSeed         int32
	PrevGainQ16      int32
	RewhiteFlag      int
}

// SilkNSQInputs bundles the constant-per-call inputs for a silk_NSQ_c
// parity test. Lengths are implicit (subfr*nb_subfr).
type SilkNSQInputs struct {
	FsKHz             int
	NbSubfr           int
	PredictLPCOrder   int
	ShapingLPCOrder   int
	Warping_Q16       int
	X16               []int16
	PredCoef_Q12      []int16 // 2*MAX_LPC_ORDER
	LTPCoef_Q14       []int16 // LTP_ORDER*MAX_NB_SUBFR
	AR_Q13            []int16 // MAX_NB_SUBFR*MAX_SHAPE_LPC_ORDER
	HarmShapeGain_Q14 []int
	Tilt_Q14          []int
	LF_shp_Q14        []int32
	Gains_Q16         []int32
	PitchL            []int
	Lambda_Q10        int
	LTP_scale_Q14     int
	SignalType        int
	QuantOffsetType   int
	NLSFInterpCoef_Q2 int
	Seed              int8

	// Initial NSQ state.
	InitLagPrev     int
	InitPrevGainQ16 int32
	InitSLTPShpQ14  []int32 // length 2*MAX_FRAME_LENGTH
	InitXq          []int16 // length 2*MAX_FRAME_LENGTH
	InitSLPCQ14     []int32 // length NSQ_LPC_BUF_LENGTH
	InitSAR2Q14     []int32 // length MAX_SHAPE_LPC_ORDER
	InitSLFARShpQ14 int32
	InitSDiffShpQ14 int32
}

// ExportTestSilkNSQ runs silk_NSQ_c on the Go side and returns mutated state.
func ExportTestSilkNSQ(in SilkNSQInputs) SilkNSQIO {
	var s silk_encoder_state
	s.fs_kHz = opus_int(in.FsKHz)
	s.nb_subfr = opus_int(in.NbSubfr)
	s.subfr_length = SUB_FRAME_LENGTH_MS * s.fs_kHz
	s.frame_length = s.subfr_length * s.nb_subfr
	s.ltp_mem_length = opus_int(LTP_MEM_LENGTH_MS) * s.fs_kHz
	s.predictLPCOrder = opus_int(in.PredictLPCOrder)
	s.shapingLPCOrder = opus_int(in.ShapingLPCOrder)
	s.warping_Q16 = opus_int(in.Warping_Q16)
	s.arch = 0

	var nsq silk_nsq_state
	nsq.lagPrev = opus_int(in.InitLagPrev)
	nsq.prev_gain_Q16 = in.InitPrevGainQ16
	nsq.sLF_AR_shp_Q14 = in.InitSLFARShpQ14
	nsq.sDiff_shp_Q14 = in.InitSDiffShpQ14
	copy(nsq.sLTP_shp_Q14[:], in.InitSLTPShpQ14)
	copy(nsq.xq[:], in.InitXq)
	copy(nsq.sLPC_Q14[:NSQ_LPC_BUF_LENGTH], in.InitSLPCQ14)
	copy(nsq.sAR2_Q14[:], in.InitSAR2Q14)

	idx := SideInfoIndices{
		Seed:              opus_int8(in.Seed),
		signalType:        opus_int8(in.SignalType),
		quantOffsetType:   opus_int8(in.QuantOffsetType),
		NLSFInterpCoef_Q2: opus_int8(in.NLSFInterpCoef_Q2),
	}

	pulses := make([]opus_int8, int(s.frame_length))
	x16 := make([]opus_int16, len(in.X16))
	for i, v := range in.X16 {
		x16[i] = opus_int16(v)
	}
	predCoef := make([]opus_int16, len(in.PredCoef_Q12))
	for i, v := range in.PredCoef_Q12 {
		predCoef[i] = opus_int16(v)
	}
	ltpCoef := make([]opus_int16, len(in.LTPCoef_Q14))
	for i, v := range in.LTPCoef_Q14 {
		ltpCoef[i] = opus_int16(v)
	}
	arQ13 := make([]opus_int16, len(in.AR_Q13))
	for i, v := range in.AR_Q13 {
		arQ13[i] = opus_int16(v)
	}
	harm := make([]opus_int, len(in.HarmShapeGain_Q14))
	for i, v := range in.HarmShapeGain_Q14 {
		harm[i] = opus_int(v)
	}
	tilt := make([]opus_int, len(in.Tilt_Q14))
	for i, v := range in.Tilt_Q14 {
		tilt[i] = opus_int(v)
	}
	lfShp := make([]opus_int32, len(in.LF_shp_Q14))
	for i, v := range in.LF_shp_Q14 {
		lfShp[i] = opus_int32(v)
	}
	gains := make([]opus_int32, len(in.Gains_Q16))
	for i, v := range in.Gains_Q16 {
		gains[i] = opus_int32(v)
	}
	pitchL := make([]opus_int, len(in.PitchL))
	for i, v := range in.PitchL {
		pitchL[i] = opus_int(v)
	}

	silk_NSQ_c(&s, &nsq, &idx, x16, pulses, predCoef, ltpCoef, arQ13, harm, tilt, lfShp, gains, pitchL,
		opus_int(in.Lambda_Q10), opus_int(in.LTP_scale_Q14))

	out := SilkNSQIO{
		Pulses:           make([]int8, len(pulses)),
		XQ:               make([]int16, int(s.frame_length)),
		SLTP_shp_Q14:     make([]int32, int(s.frame_length)),
		SLPC_Q14:         make([]int32, NSQ_LPC_BUF_LENGTH),
		SAR2_Q14:         make([]int32, MAX_SHAPE_LPC_ORDER),
		SLF_AR_shp_Q14:   int32(nsq.sLF_AR_shp_Q14),
		SDiff_shp_Q14:    int32(nsq.sDiff_shp_Q14),
		LagPrev:          int(nsq.lagPrev),
		SLTP_buf_idx:     int(nsq.sLTP_buf_idx),
		SLTP_shp_buf_idx: int(nsq.sLTP_shp_buf_idx),
		RandSeed:         int32(nsq.rand_seed),
		PrevGainQ16:      int32(nsq.prev_gain_Q16),
		RewhiteFlag:      int(nsq.rewhite_flag),
	}
	for i, v := range pulses {
		out.Pulses[i] = int8(v)
	}
	for i := 0; i < int(s.frame_length); i++ {
		out.XQ[i] = int16(nsq.xq[i])
		out.SLTP_shp_Q14[i] = int32(nsq.sLTP_shp_Q14[i])
	}
	for i := 0; i < NSQ_LPC_BUF_LENGTH; i++ {
		out.SLPC_Q14[i] = int32(nsq.sLPC_Q14[i])
	}
	for i := 0; i < MAX_SHAPE_LPC_ORDER; i++ {
		out.SAR2_Q14[i] = int32(nsq.sAR2_Q14[i])
	}
	return out
}

// ExportTestSilkNSQDelDec runs silk_NSQ_del_dec_c similarly.
func ExportTestSilkNSQDelDec(in SilkNSQInputs, nStatesDelayedDecision int) (SilkNSQIO, int8) {
	var s silk_encoder_state
	s.fs_kHz = opus_int(in.FsKHz)
	s.nb_subfr = opus_int(in.NbSubfr)
	s.subfr_length = SUB_FRAME_LENGTH_MS * s.fs_kHz
	s.frame_length = s.subfr_length * s.nb_subfr
	s.ltp_mem_length = opus_int(LTP_MEM_LENGTH_MS) * s.fs_kHz
	s.predictLPCOrder = opus_int(in.PredictLPCOrder)
	s.shapingLPCOrder = opus_int(in.ShapingLPCOrder)
	s.warping_Q16 = opus_int(in.Warping_Q16)
	s.nStatesDelayedDecision = opus_int(nStatesDelayedDecision)
	s.arch = 0

	var nsq silk_nsq_state
	nsq.lagPrev = opus_int(in.InitLagPrev)
	nsq.prev_gain_Q16 = in.InitPrevGainQ16
	nsq.sLF_AR_shp_Q14 = in.InitSLFARShpQ14
	nsq.sDiff_shp_Q14 = in.InitSDiffShpQ14
	copy(nsq.sLTP_shp_Q14[:], in.InitSLTPShpQ14)
	copy(nsq.xq[:], in.InitXq)
	copy(nsq.sLPC_Q14[:NSQ_LPC_BUF_LENGTH], in.InitSLPCQ14)
	copy(nsq.sAR2_Q14[:], in.InitSAR2Q14)

	idx := SideInfoIndices{
		Seed:              opus_int8(in.Seed),
		signalType:        opus_int8(in.SignalType),
		quantOffsetType:   opus_int8(in.QuantOffsetType),
		NLSFInterpCoef_Q2: opus_int8(in.NLSFInterpCoef_Q2),
	}

	pulses := make([]opus_int8, int(s.frame_length))
	x16 := make([]opus_int16, len(in.X16))
	for i, v := range in.X16 {
		x16[i] = opus_int16(v)
	}
	predCoef := make([]opus_int16, len(in.PredCoef_Q12))
	for i, v := range in.PredCoef_Q12 {
		predCoef[i] = opus_int16(v)
	}
	ltpCoef := make([]opus_int16, len(in.LTPCoef_Q14))
	for i, v := range in.LTPCoef_Q14 {
		ltpCoef[i] = opus_int16(v)
	}
	arQ13 := make([]opus_int16, len(in.AR_Q13))
	for i, v := range in.AR_Q13 {
		arQ13[i] = opus_int16(v)
	}
	harm := make([]opus_int, len(in.HarmShapeGain_Q14))
	for i, v := range in.HarmShapeGain_Q14 {
		harm[i] = opus_int(v)
	}
	tilt := make([]opus_int, len(in.Tilt_Q14))
	for i, v := range in.Tilt_Q14 {
		tilt[i] = opus_int(v)
	}
	lfShp := make([]opus_int32, len(in.LF_shp_Q14))
	for i, v := range in.LF_shp_Q14 {
		lfShp[i] = opus_int32(v)
	}
	gains := make([]opus_int32, len(in.Gains_Q16))
	for i, v := range in.Gains_Q16 {
		gains[i] = opus_int32(v)
	}
	pitchL := make([]opus_int, len(in.PitchL))
	for i, v := range in.PitchL {
		pitchL[i] = opus_int(v)
	}

	silk_NSQ_del_dec_c(&s, &nsq, &idx, x16, pulses, predCoef, ltpCoef, arQ13, harm, tilt, lfShp, gains, pitchL,
		opus_int(in.Lambda_Q10), opus_int(in.LTP_scale_Q14))

	out := SilkNSQIO{
		Pulses:           make([]int8, len(pulses)),
		XQ:               make([]int16, int(s.frame_length)),
		SLTP_shp_Q14:     make([]int32, int(s.frame_length)),
		SLPC_Q14:         make([]int32, NSQ_LPC_BUF_LENGTH),
		SAR2_Q14:         make([]int32, MAX_SHAPE_LPC_ORDER),
		SLF_AR_shp_Q14:   int32(nsq.sLF_AR_shp_Q14),
		SDiff_shp_Q14:    int32(nsq.sDiff_shp_Q14),
		LagPrev:          int(nsq.lagPrev),
		SLTP_buf_idx:     int(nsq.sLTP_buf_idx),
		SLTP_shp_buf_idx: int(nsq.sLTP_shp_buf_idx),
		RandSeed:         int32(nsq.rand_seed),
		PrevGainQ16:      int32(nsq.prev_gain_Q16),
		RewhiteFlag:      int(nsq.rewhite_flag),
	}
	for i, v := range pulses {
		out.Pulses[i] = int8(v)
	}
	for i := 0; i < int(s.frame_length); i++ {
		out.XQ[i] = int16(nsq.xq[i])
		out.SLTP_shp_Q14[i] = int32(nsq.sLTP_shp_Q14[i])
	}
	for i := 0; i < NSQ_LPC_BUF_LENGTH; i++ {
		out.SLPC_Q14[i] = int32(nsq.sLPC_Q14[i])
	}
	for i := 0; i < MAX_SHAPE_LPC_ORDER; i++ {
		out.SAR2_Q14[i] = int32(nsq.sAR2_Q14[i])
	}
	return out, int8(idx.Seed)
}

// ExportTestSilkNSQDelDecForceScalar is identical to
// ExportTestSilkNSQDelDec except it toggles
// testingForceScalarNSQDelDec around the silk_NSQ_del_dec_c call so the
// dispatch falls through to silk_noise_shape_quantizer_del_dec even if
// nsqSIMDAvailable && nStates==MAX_DEL_DEC_STATES would otherwise pick
// the SoA path. Used by the SoA-vs-scalar integration parity test.
func ExportTestSilkNSQDelDecForceScalar(in SilkNSQInputs, nStatesDelayedDecision int) (SilkNSQIO, int8) {
	prev := testingForceScalarNSQDelDec
	testingForceScalarNSQDelDec = true
	defer func() { testingForceScalarNSQDelDec = prev }()
	return ExportTestSilkNSQDelDec(in, nStatesDelayedDecision)
}
