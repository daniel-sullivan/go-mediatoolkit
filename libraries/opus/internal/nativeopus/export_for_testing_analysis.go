package nativeopus

// Export shims for Phase 9d analysis parity tests.

// DETECT_SIZE is exported for test sizing.
const ExportDETECT_SIZE = DETECT_SIZE

// ExportSilkResamplerDown2HP exposes silk_resampler_down2_hp.
// S length 3, out length len(in)/2.
func ExportSilkResamplerDown2HP(S, out, in []float32) float32 {
	return silk_resampler_down2_hp(S, out, in, len(in))
}

// ExportIsDigitalSilence exposes is_digital_silence (single channel).
func ExportIsDigitalSilence(pcm []float32, lsbDepth int) int {
	return is_digital_silence(pcm, len(pcm), 1, lsbDepth)
}

// ExportTonalityAnalysisInit wraps tonality_analysis_init so the test
// harness can build a deterministically-initialized TonalityAnalysisState
// from outside the package.
func ExportTonalityAnalysisInit(Fs int32) *TonalityAnalysisState {
	st := &TonalityAnalysisState{}
	tonality_analysis_init(st, opus_int32(Fs))
	return st
}

// ExportTonalityAnalysisSnapshot returns a flat representation of
// every field of a TonalityAnalysisState for bit-exact comparison
// against the C oracle.
type AnalysisStateSnapshot struct {
	Arch              int
	Application       int
	Fs                int32
	Angle             []float32
	DAngle            []float32
	D2Angle           []float32
	Inmem             []float32
	MemFill           int
	PrevBandTonality  []float32
	PrevTonality      float32
	PrevBandwidth     int
	E                 []float32 // [NB_FRAMES*NB_TBANDS]
	LogE              []float32
	LowE              []float32
	HighE             []float32
	MeanE             []float32
	Mem               []float32
	Cmean             []float32
	Std               []float32
	Etracker          float32
	LowECount         float32
	ECount            int
	Count             int
	AnalysisOffset    int
	WritePos          int
	ReadPos           int
	ReadSubframe      int
	HpEnerAccum       float32
	Initialized       int
	RnnState          []float32
	DownmixState      []float32
	InfoValid         []int32
	InfoTonality      []float32
	InfoTonalitySlope []float32
	InfoNoisiness     []float32
	InfoActivity      []float32
	InfoMusicProb     []float32
	InfoMusicProbMin  []float32
	InfoMusicProbMax  []float32
	InfoBandwidth     []int32
	InfoActivityProb  []float32
	InfoMaxPitchRatio []float32
	InfoLeakBoost     []byte // DETECT_SIZE*LEAK_BANDS
}

// ExportSnapshotAnalysisState flattens a state into a comparable form.
func ExportSnapshotAnalysisState(st *TonalityAnalysisState) AnalysisStateSnapshot {
	snap := AnalysisStateSnapshot{
		Arch:             st.arch,
		Application:      st.application,
		Fs:               int32(st.Fs),
		Angle:            append([]float32{}, st.angle[:]...),
		DAngle:           append([]float32{}, st.d_angle[:]...),
		D2Angle:          append([]float32{}, st.d2_angle[:]...),
		Inmem:            append([]float32{}, st.inmem[:]...),
		MemFill:          st.mem_fill,
		PrevBandTonality: append([]float32{}, st.prev_band_tonality[:]...),
		PrevTonality:     st.prev_tonality,
		PrevBandwidth:    st.prev_bandwidth,
		LowE:             append([]float32{}, st.lowE[:]...),
		HighE:            append([]float32{}, st.highE[:]...),
		MeanE:            append([]float32{}, st.meanE[:]...),
		Mem:              append([]float32{}, st.mem[:]...),
		Cmean:            append([]float32{}, st.cmean[:]...),
		Std:              append([]float32{}, st.std[:]...),
		Etracker:         st.Etracker,
		LowECount:        st.lowECount,
		ECount:           st.E_count,
		Count:            st.count,
		AnalysisOffset:   st.analysis_offset,
		WritePos:         st.write_pos,
		ReadPos:          st.read_pos,
		ReadSubframe:     st.read_subframe,
		HpEnerAccum:      st.hp_ener_accum,
		Initialized:      st.initialized,
		RnnState:         append([]float32{}, st.rnn_state[:]...),
		DownmixState:     append([]float32{}, st.downmix_state[:]...),
	}
	// Flatten E and logE (NB_FRAMES x NB_TBANDS).
	for f := 0; f < NB_FRAMES; f++ {
		snap.E = append(snap.E, st.E[f][:]...)
		snap.LogE = append(snap.LogE, st.logE[f][:]...)
	}
	// Flatten info.
	for i := 0; i < DETECT_SIZE; i++ {
		snap.InfoValid = append(snap.InfoValid, int32(st.info[i].valid))
		snap.InfoTonality = append(snap.InfoTonality, st.info[i].tonality)
		snap.InfoTonalitySlope = append(snap.InfoTonalitySlope, st.info[i].tonality_slope)
		snap.InfoNoisiness = append(snap.InfoNoisiness, st.info[i].noisiness)
		snap.InfoActivity = append(snap.InfoActivity, st.info[i].activity)
		snap.InfoMusicProb = append(snap.InfoMusicProb, st.info[i].music_prob)
		snap.InfoMusicProbMin = append(snap.InfoMusicProbMin, st.info[i].music_prob_min)
		snap.InfoMusicProbMax = append(snap.InfoMusicProbMax, st.info[i].music_prob_max)
		snap.InfoBandwidth = append(snap.InfoBandwidth, int32(st.info[i].bandwidth))
		snap.InfoActivityProb = append(snap.InfoActivityProb, st.info[i].activity_probability)
		snap.InfoMaxPitchRatio = append(snap.InfoMaxPitchRatio, st.info[i].max_pitch_ratio)
		snap.InfoLeakBoost = append(snap.InfoLeakBoost, st.info[i].leak_boost[:]...)
	}
	return snap
}

// AnalysisInfoSnapshot is a plain-value copy of an AnalysisInfo for
// bit-exact comparison.
type AnalysisInfoSnapshot struct {
	Valid               int32
	Tonality            float32
	TonalitySlope       float32
	Noisiness           float32
	Activity            float32
	MusicProb           float32
	MusicProbMin        float32
	MusicProbMax        float32
	Bandwidth           int32
	ActivityProbability float32
	MaxPitchRatio       float32
	LeakBoost           [LEAK_BANDS]byte
}

// ExportSnapshotAnalysisInfo extracts a comparable snapshot of an
// AnalysisInfo.
func ExportSnapshotAnalysisInfo(a *AnalysisInfo) AnalysisInfoSnapshot {
	return AnalysisInfoSnapshot{
		Valid:               int32(a.valid),
		Tonality:            a.tonality,
		TonalitySlope:       a.tonality_slope,
		Noisiness:           a.noisiness,
		Activity:            a.activity,
		MusicProb:           a.music_prob,
		MusicProbMin:        a.music_prob_min,
		MusicProbMax:        a.music_prob_max,
		Bandwidth:           int32(a.bandwidth),
		ActivityProbability: a.activity_probability,
		MaxPitchRatio:       a.max_pitch_ratio,
		LeakBoost:           a.leak_boost,
	}
}

// ExportRestoreAnalysisState takes a byte pattern (e.g. rand-filled)
// and dumps it straight into the state's raw memory. Used for seeding
// both Go and C states to identical contents for tonality_get_info
// parity. Here we accept explicit fields.
type AnalysisStateSeed struct {
	Count         int
	WritePos      int
	ReadPos       int
	ReadSubframe  int
	Fs            int32
	PrevBandwidth int
	// Per-info frame fields. Only these drive tonality_get_info logic.
	InfoValid               []int32
	InfoTonality            []float32
	InfoBandwidth           []int32
	InfoMusicProb           []float32
	InfoActivityProbability []float32
}

// ExportTonalityGetInfoFromSeed builds a minimal Go state from a seed,
// runs tonality_get_info, and returns the resulting AnalysisInfo snapshot.
func ExportTonalityGetInfoFromSeed(seed AnalysisStateSeed, frameLen int) AnalysisInfoSnapshot {
	st := &TonalityAnalysisState{}
	tonality_analysis_init(st, opus_int32(seed.Fs))
	st.count = seed.Count
	st.write_pos = seed.WritePos
	st.read_pos = seed.ReadPos
	st.read_subframe = seed.ReadSubframe
	st.prev_bandwidth = seed.PrevBandwidth
	for i := 0; i < DETECT_SIZE && i < len(seed.InfoValid); i++ {
		st.info[i].valid = int(seed.InfoValid[i])
		st.info[i].tonality = seed.InfoTonality[i]
		st.info[i].bandwidth = int(seed.InfoBandwidth[i])
		st.info[i].music_prob = seed.InfoMusicProb[i]
		st.info[i].activity_probability = seed.InfoActivityProbability[i]
	}
	var info AnalysisInfo
	tonality_get_info(st, &info, frameLen)
	return ExportSnapshotAnalysisInfo(&info)
}

// ExportRunAnalysisFloat runs the full analysis pipeline on a float PCM
// buffer using downmix_float. The mode parameter must have mdct.kfft[0]
// populated with a 480-point FFT state. Returns both the final
// AnalysisInfo (as returned by run_analysis) and the post-run state
// snapshot for deep comparison.
func ExportRunAnalysisFloat(st *TonalityAnalysisState, mode *OpusCustomMode,
	pcm []float32, analysisFrameSize, frameSize, c1, c2, C int,
	Fs int32, lsbDepth int) (AnalysisInfoSnapshot, AnalysisStateSnapshot) {
	var info AnalysisInfo
	run_analysis(st, mode, pcm, analysisFrameSize, frameSize,
		c1, c2, C, opus_int32(Fs), lsbDepth, downmix_float, &info)
	return ExportSnapshotAnalysisInfo(&info), ExportSnapshotAnalysisState(st)
}

// ExportTonalityAnalysisStep runs a single call to tonality_analysis
// for tight unit comparison.
func ExportTonalityAnalysisStep(st *TonalityAnalysisState, mode *OpusCustomMode,
	pcm []float32, length, offset, c1, c2, C, lsbDepth int) AnalysisStateSnapshot {
	tonality_analysis(st, mode, pcm, length, offset, c1, c2, C, lsbDepth, downmix_float)
	return ExportSnapshotAnalysisState(st)
}

// ExportNewCELTModeFor480FFT builds a minimal OpusCustomMode whose only
// populated field is mdct.kfft[0] — the 480-point FFT used by
// tonality_analysis. The test harness is expected to inject the same
// FFT state data used on the C side.
func ExportNewCELTModeFor480FFT(st FftStateHandle) *OpusCustomMode {
	m := &OpusCustomMode{}
	m.mdct.kfft[0] = st.p
	return m
}
