package nativeopus

// Thin exports for the SILK float (_FLP) noise-shape-analysis mid-driver
// parity test — Phase 8 Wave 2e. Covers silk_noise_shape_analysis_FLP,
// which exercises warped / unwarped autocorrelation, schur, k2a,
// bwexpander, warped_gain/limit coefficient limiters, gain tweaking, and
// subframe smoothing of harmonic-shaping and tilt parameters.
//
// The payload carries the subset of silk_encoder_state_FLP /
// silk_encoder_control_FLP fields the driver reads or writes, plus the
// surrounding shape-state smoothing memory.

// SilkNoiseShapeAnalysisFLPPayload — flat payload for the driver parity
// test. Input fields are used to populate psEnc / psEncCtrl before the
// call; output fields are filled from the post-call state.
type SilkNoiseShapeAnalysisFLPPayload struct {
	// --- sCmn fields read by the driver. ---
	FsKHz                int
	NbSubfr              int
	LaShape              int
	ShapeWinLength       int
	ShapingLPCOrder      int
	SubfrLength          int
	SNR_dB_Q7            int
	InputQualityBandsQ15 [VAD_N_BANDS]int
	UseCBR               int
	SpeechActivityQ8     int
	SignalType           int8
	WarpingQ16           int
	Arch                 int

	// --- encoder control inputs. ---
	LTPCorrIn float32
	PredGain  float32
	PitchL    [MAX_NB_SUBFR]int32

	// --- shape-state smoothing memory (in/out). ---
	HarmShapeGainSmthIn  float32
	TiltSmthIn           float32
	HarmShapeGainSmthOut float32
	TiltSmthOut          float32

	// --- input buffers. Backing slice plus offset so negative
	// pointer arithmetic (x - la_shape) works. ---
	PitchRes []float32
	X        []float32
	XOff     int

	// --- outputs. ---
	QuantOffsetType int8
	InputQuality    float32
	CodingQuality   float32
	Gains           [MAX_NB_SUBFR]float32
	AR              [MAX_NB_SUBFR * MAX_SHAPE_LPC_ORDER]float32
	LF_MA_shp       [MAX_NB_SUBFR]float32
	LF_AR_shp       [MAX_NB_SUBFR]float32
	Tilt            [MAX_NB_SUBFR]float32
	HarmShapeGain   [MAX_NB_SUBFR]float32
}

// ExportTestSilkNoiseShapeAnalysisFLP — runs silk_noise_shape_analysis_FLP
// against the Go port and returns the post-call payload state.
func ExportTestSilkNoiseShapeAnalysisFLP(in SilkNoiseShapeAnalysisFLPPayload) SilkNoiseShapeAnalysisFLPPayload {
	var psEnc silk_encoder_state_FLP
	var psEncCtrl silk_encoder_control_FLP

	psEnc.sCmn.fs_kHz = opus_int(in.FsKHz)
	psEnc.sCmn.nb_subfr = opus_int(in.NbSubfr)
	psEnc.sCmn.la_shape = opus_int(in.LaShape)
	psEnc.sCmn.shapeWinLength = opus_int(in.ShapeWinLength)
	psEnc.sCmn.shapingLPCOrder = opus_int(in.ShapingLPCOrder)
	psEnc.sCmn.subfr_length = opus_int(in.SubfrLength)
	psEnc.sCmn.SNR_dB_Q7 = opus_int(in.SNR_dB_Q7)
	for i := 0; i < VAD_N_BANDS; i++ {
		psEnc.sCmn.input_quality_bands_Q15[i] = opus_int(in.InputQualityBandsQ15[i])
	}
	psEnc.sCmn.useCBR = opus_int(in.UseCBR)
	psEnc.sCmn.speech_activity_Q8 = opus_int(in.SpeechActivityQ8)
	psEnc.sCmn.indices.signalType = opus_int8(in.SignalType)
	psEnc.sCmn.warping_Q16 = opus_int(in.WarpingQ16)
	psEnc.sCmn.arch = in.Arch

	psEnc.LTPCorr = silk_float(in.LTPCorrIn)
	psEncCtrl.predGain = silk_float(in.PredGain)
	for i := 0; i < MAX_NB_SUBFR; i++ {
		psEncCtrl.pitchL[i] = opus_int(in.PitchL[i])
	}

	psEnc.sShape.HarmShapeGain_smth = silk_float(in.HarmShapeGainSmthIn)
	psEnc.sShape.Tilt_smth = silk_float(in.TiltSmthIn)

	silk_noise_shape_analysis_FLP_withBase(&psEnc, &psEncCtrl, in.PitchRes, in.X, opus_int(in.XOff))

	out := in
	out.QuantOffsetType = int8(psEnc.sCmn.indices.quantOffsetType)
	out.InputQuality = float32(psEncCtrl.input_quality)
	out.CodingQuality = float32(psEncCtrl.coding_quality)
	out.HarmShapeGainSmthOut = float32(psEnc.sShape.HarmShapeGain_smth)
	out.TiltSmthOut = float32(psEnc.sShape.Tilt_smth)
	for i := 0; i < MAX_NB_SUBFR; i++ {
		out.Gains[i] = float32(psEncCtrl.Gains[i])
		out.LF_MA_shp[i] = float32(psEncCtrl.LF_MA_shp[i])
		out.LF_AR_shp[i] = float32(psEncCtrl.LF_AR_shp[i])
		out.Tilt[i] = float32(psEncCtrl.Tilt[i])
		out.HarmShapeGain[i] = float32(psEncCtrl.HarmShapeGain[i])
	}
	for i := 0; i < MAX_NB_SUBFR*MAX_SHAPE_LPC_ORDER; i++ {
		out.AR[i] = float32(psEncCtrl.AR[i])
	}
	return out
}
