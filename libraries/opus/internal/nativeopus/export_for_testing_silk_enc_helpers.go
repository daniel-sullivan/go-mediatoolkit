package nativeopus

// Thin exports for SILK encoder-helper parity tests.

// ExportTestCheckControlInput runs check_control_input on a caller-supplied
// set of fields. Returns the C-compatible error code.
func ExportTestCheckControlInput(
	nChannelsAPI, nChannelsInternal int32,
	APISampleRate, maxInternalSampleRate, minInternalSampleRate, desiredInternalSampleRate int32,
	payloadSize_ms int,
	bitRate int32,
	packetLossPercentage, complexity int,
	useInBandFEC, useDTX, useCBR, maxBits, toMono, opusCanSwitch, reducedDependency int,
) int {
	ec := silk_EncControlStruct{
		nChannelsAPI:              opus_int32(nChannelsAPI),
		nChannelsInternal:         opus_int32(nChannelsInternal),
		API_sampleRate:            opus_int32(APISampleRate),
		maxInternalSampleRate:     opus_int32(maxInternalSampleRate),
		minInternalSampleRate:     opus_int32(minInternalSampleRate),
		desiredInternalSampleRate: opus_int32(desiredInternalSampleRate),
		payloadSize_ms:            opus_int(payloadSize_ms),
		bitRate:                   opus_int32(bitRate),
		packetLossPercentage:      opus_int(packetLossPercentage),
		complexity:                opus_int(complexity),
		useInBandFEC:              opus_int(useInBandFEC),
		useDTX:                    opus_int(useDTX),
		useCBR:                    opus_int(useCBR),
		maxBits:                   opus_int(maxBits),
		toMono:                    opus_int(toMono),
		opusCanSwitch:             opus_int(opusCanSwitch),
		reducedDependency:         opus_int(reducedDependency),
	}
	return int(check_control_input(&ec))
}

// ExportTestControlSNR drives silk_control_SNR on a minimal state and
// returns the resulting SNR_dB_Q7 plus TargetRate_bps.
func ExportTestControlSNR(fs_kHz, nb_subfr int, targetRate int32) (snr_dB_Q7 int, storedTargetRate int32) {
	var s silk_encoder_state
	s.fs_kHz = opus_int(fs_kHz)
	s.nb_subfr = opus_int(nb_subfr)
	silk_control_SNR(&s, opus_int32(targetRate))
	return int(s.SNR_dB_Q7), int32(s.TargetRate_bps)
}

// ExportTestControlAudioBandwidth runs silk_control_audio_bandwidth
// with a caller-built state and control struct. Returns the chosen
// internal fs_kHz and the mutated LP-state fields + maxBits + switchReady
// so the test can compare against C.
type TestABState struct {
	Fs_kHz                 int
	SavedFsKHz             int32
	TransitionFrameNo      int32
	Mode                   int
	AllowBandwidthSwitch   int
	APIfsHz                int32
	MaxInternalfsHz        int
	MinInternalfsHz        int
	DesiredInternalfsHz    int
	InLPState0, InLPState1 int32
}

type TestABCtrl struct {
	OpusCanSwitch   int
	SwitchReady     int
	MaxBits         int
	PayloadSize_ms  int
	DesiredInternal int32
}

func ExportTestControlAudioBandwidth(stIn TestABState, ecIn TestABCtrl) (
	fs_kHz int, stOut TestABState, ecOut TestABCtrl,
) {
	var s silk_encoder_state
	s.fs_kHz = opus_int(stIn.Fs_kHz)
	s.sLP.saved_fs_kHz = opus_int32(stIn.SavedFsKHz)
	s.sLP.transition_frame_no = opus_int32(stIn.TransitionFrameNo)
	s.sLP.mode = opus_int(stIn.Mode)
	s.sLP.In_LP_State[0] = opus_int32(stIn.InLPState0)
	s.sLP.In_LP_State[1] = opus_int32(stIn.InLPState1)
	s.allow_bandwidth_switch = opus_int(stIn.AllowBandwidthSwitch)
	s.API_fs_Hz = opus_int32(stIn.APIfsHz)
	s.maxInternal_fs_Hz = opus_int(stIn.MaxInternalfsHz)
	s.minInternal_fs_Hz = opus_int(stIn.MinInternalfsHz)
	s.desiredInternal_fs_Hz = opus_int(stIn.DesiredInternalfsHz)

	var ec silk_EncControlStruct
	ec.opusCanSwitch = opus_int(ecIn.OpusCanSwitch)
	ec.switchReady = opus_int(ecIn.SwitchReady)
	ec.maxBits = opus_int(ecIn.MaxBits)
	ec.payloadSize_ms = opus_int(ecIn.PayloadSize_ms)

	fs := silk_control_audio_bandwidth(&s, &ec)

	stOut = TestABState{
		Fs_kHz:               int(s.fs_kHz),
		SavedFsKHz:           int32(s.sLP.saved_fs_kHz),
		TransitionFrameNo:    int32(s.sLP.transition_frame_no),
		Mode:                 int(s.sLP.mode),
		AllowBandwidthSwitch: int(s.allow_bandwidth_switch),
		APIfsHz:              int32(s.API_fs_Hz),
		MaxInternalfsHz:      int(s.maxInternal_fs_Hz),
		MinInternalfsHz:      int(s.minInternal_fs_Hz),
		DesiredInternalfsHz:  int(s.desiredInternal_fs_Hz),
		InLPState0:           int32(s.sLP.In_LP_State[0]),
		InLPState1:           int32(s.sLP.In_LP_State[1]),
	}
	ecOut = TestABCtrl{
		OpusCanSwitch:   int(ec.opusCanSwitch),
		SwitchReady:     int(ec.switchReady),
		MaxBits:         int(ec.maxBits),
		PayloadSize_ms:  int(ec.payloadSize_ms),
		DesiredInternal: int32(ec.desiredInternalSampleRate),
	}
	return int(fs), stOut, ecOut
}

// ExportTestVADInit initializes a silk_VAD_state and returns its
// non-zero fields as slices so the test can compare them with the C
// reference.
func ExportTestVADInit() (NL, inv_NL, NoiseLevelBias, NrgRatioSmth_Q8 []int32, counter int32) {
	var v silk_VAD_state
	silk_VAD_Init(&v)
	NL = make([]int32, VAD_N_BANDS)
	inv_NL = make([]int32, VAD_N_BANDS)
	NoiseLevelBias = make([]int32, VAD_N_BANDS)
	NrgRatioSmth_Q8 = make([]int32, VAD_N_BANDS)
	for i := 0; i < VAD_N_BANDS; i++ {
		NL[i] = int32(v.NL[i])
		inv_NL[i] = int32(v.inv_NL[i])
		NoiseLevelBias[i] = int32(v.NoiseLevelBias[i])
		NrgRatioSmth_Q8[i] = int32(v.NrgRatioSmth_Q8[i])
	}
	counter = int32(v.counter)
	return
}

// ExportTestVADGetSAQ8 runs silk_VAD_GetSA_Q8_c on an initialized state
// with the caller-provided frame. Returns the state's speech_activity_Q8,
// input_tilt_Q15, and input_quality_bands_Q15.
func ExportTestVADGetSAQ8(frame []int16, frame_length, fs_kHz int) (
	speech_activity_Q8, input_tilt_Q15 int,
	input_quality_bands_Q15 [4]int,
	NL [4]int32,
	inv_NL [4]int32,
	XnrgSubfr [4]int32,
	NrgRatioSmth_Q8 [4]int32,
) {
	var s silk_encoder_state
	silk_VAD_Init(&s.sVAD)
	s.fs_kHz = opus_int(fs_kHz)
	s.frame_length = opus_int(frame_length)
	silk_VAD_GetSA_Q8_c(&s, frame)
	speech_activity_Q8 = int(s.speech_activity_Q8)
	input_tilt_Q15 = int(s.input_tilt_Q15)
	for i := 0; i < 4; i++ {
		input_quality_bands_Q15[i] = int(s.input_quality_bands_Q15[i])
		NL[i] = int32(s.sVAD.NL[i])
		inv_NL[i] = int32(s.sVAD.inv_NL[i])
		XnrgSubfr[i] = int32(s.sVAD.XnrgSubfr[i])
		NrgRatioSmth_Q8[i] = int32(s.sVAD.NrgRatioSmth_Q8[i])
	}
	return
}

// ExportTestHPVariableCutoff runs silk_HP_variable_cutoff with a
// minimal encoder state. Returns the updated variable_HP_smth1_Q15
// (smth2 is unchanged by this function).
func ExportTestHPVariableCutoff(
	prevSignalType, prevLag int,
	fs_kHz int,
	input_quality_bands_Q15_0 int,
	speech_activity_Q8 int,
	variable_HP_smth1_Q15_in int32,
) int32 {
	var state [2]silk_encoder_state_FLP
	state[0].sCmn.prevSignalType = opus_int8(prevSignalType)
	state[0].sCmn.prevLag = opus_int(prevLag)
	state[0].sCmn.fs_kHz = opus_int(fs_kHz)
	state[0].sCmn.input_quality_bands_Q15[0] = opus_int(input_quality_bands_Q15_0)
	state[0].sCmn.speech_activity_Q8 = opus_int(speech_activity_Q8)
	state[0].sCmn.variable_HP_smth1_Q15 = opus_int32(variable_HP_smth1_Q15_in)
	silk_HP_variable_cutoff(state[:])
	return int32(state[0].sCmn.variable_HP_smth1_Q15)
}

// ExportTestInitEncoder calls silk_init_encoder and returns some
// state fields the C reference also sets.
func ExportTestInitEncoder(arch int) (firstFrameAfterReset int, variableHPSmth1Q15, variableHPSmth2Q15 int32,
	vadCounter int32, vadNL [4]int32) {
	var e silk_encoder_state_FLP
	silk_init_encoder(&e, arch)
	firstFrameAfterReset = int(e.sCmn.first_frame_after_reset)
	variableHPSmth1Q15 = int32(e.sCmn.variable_HP_smth1_Q15)
	variableHPSmth2Q15 = int32(e.sCmn.variable_HP_smth2_Q15)
	vadCounter = int32(e.sCmn.sVAD.counter)
	for i := 0; i < 4; i++ {
		vadNL[i] = int32(e.sCmn.sVAD.NL[i])
	}
	return
}

// ExportTestVQWMatEC wraps silk_VQ_WMat_EC_c.
func ExportTestVQWMatEC(
	XX_Q17 []int32, xX_Q17 []int32,
	cb_Q7 []int8, cb_gain_Q7 []uint8, cl_Q5 []uint8,
	subfr_len int, max_gain_Q7 int32, L int,
) (ind int8, res_nrg_Q15, rate_dist_Q8 int32, gain_Q7 int) {
	xx := make([]opus_int32, len(XX_Q17))
	for i, v := range XX_Q17 {
		xx[i] = opus_int32(v)
	}
	xX := make([]opus_int32, len(xX_Q17))
	for i, v := range xX_Q17 {
		xX[i] = opus_int32(v)
	}
	cb := make([]opus_int8, len(cb_Q7))
	for i, v := range cb_Q7 {
		cb[i] = opus_int8(v)
	}
	cbg := make([]opus_uint8, len(cb_gain_Q7))
	for i, v := range cb_gain_Q7 {
		cbg[i] = opus_uint8(v)
	}
	cl := make([]opus_uint8, len(cl_Q5))
	for i, v := range cl_Q5 {
		cl[i] = opus_uint8(v)
	}
	var iidx opus_int8
	var rn, rd opus_int32
	var g opus_int
	silk_VQ_WMat_EC_c(&iidx, &rn, &rd, &g, xx, xX, cb, cbg, cl, opus_int(subfr_len), opus_int32(max_gain_Q7), opus_int(L))
	return int8(iidx), int32(rn), int32(rd), int(g)
}

// ExportTestQuantLTPGains wraps silk_quant_LTP_gains.
func ExportTestQuantLTPGains(
	XX_Q17, xX_Q17 []int32,
	sum_log_gain_Q7_in int32,
	subfr_len, nb_subfr int,
) (B_Q14 []int16, cbk_index [4]int8, periodicity_index int8, sum_log_gain_Q7_out int32, pred_gain_dB_Q7 int) {
	xx := make([]opus_int32, len(XX_Q17))
	for i, v := range XX_Q17 {
		xx[i] = opus_int32(v)
	}
	xX := make([]opus_int32, len(xX_Q17))
	for i, v := range xX_Q17 {
		xX[i] = opus_int32(v)
	}
	B := make([]opus_int16, MAX_NB_SUBFR*LTP_ORDER)
	var cbki [MAX_NB_SUBFR]opus_int8
	var per opus_int8
	slg := opus_int32(sum_log_gain_Q7_in)
	var pg opus_int
	silk_quant_LTP_gains(B, cbki[:], &per, &slg, &pg, xx, xX, opus_int(subfr_len), opus_int(nb_subfr), 0)
	B_Q14 = make([]int16, MAX_NB_SUBFR*LTP_ORDER)
	for i, v := range B {
		B_Q14[i] = int16(v)
	}
	for i := 0; i < MAX_NB_SUBFR; i++ {
		cbk_index[i] = int8(cbki[i])
	}
	periodicity_index = int8(per)
	sum_log_gain_Q7_out = int32(slg)
	pred_gain_dB_Q7 = int(pg)
	return
}
