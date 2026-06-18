package nativeopus

// 9f-E test shim: ExportOpusEncodeFrameNative exposes the internal
// opus_encode_frame_native for parity testing. The encoder state must
// be fully populated by the caller (via opus_encoder_init + ctl) before
// calling this.

// ExportOpusEncodeFrameNative wraps opus_encode_frame_native for tests.
// analysisInfo may be nil (equivalent to C passing a stack-allocated
// AnalysisInfo with valid=0).
func ExportOpusEncodeFrameNative(
	st *OpusEncoder,
	pcm []float32,
	frameSize int,
	data []byte,
	maxDataBytes int32,
	floatAPI, firstFrame int,
	analysisInfo *AnalysisInfo,
	isSilence, redundancy, celtToSilk, prefill int,
	equivRate int32,
	toCelt int,
) int32 {
	ret := opus_encode_frame_native(st, pcm, frameSize, data,
		opus_int32(maxDataBytes), floatAPI, firstFrame,
		analysisInfo, isSilence, redundancy, celtToSilk, prefill,
		opus_int32(equivRate), toCelt)
	return int32(ret)
}

// ExportNewOpusEncoderForTest creates a fully-initialized Go OpusEncoder
// for direct-invocation parity tests.
func ExportNewOpusEncoderForTest(Fs int32, channels, application int) (*OpusEncoder, int) {
	st := &OpusEncoder{}
	ret := opus_encoder_init(st, opus_int32(Fs), channels, application)
	return st, ret
}

// ExportSetEncoderMode sets the mode/bandwidth fields directly (they are
// normally assigned inside opus_encode_native; for 9f-E tests we inject
// them manually).
func ExportSetEncoderMode(st *OpusEncoder, mode, bandwidth int) {
	st.mode = mode
	st.bandwidth = bandwidth
}

// ExportSetEncoderBitrateBps sets bitrate_bps directly.
func ExportSetEncoderBitrateBps(st *OpusEncoder, bps int32) {
	st.bitrate_bps = opus_int32(bps)
}

// ExportSetEncoderCeltMode installs the given CeltMode on the encoder's
// CELT sub-encoder and re-runs celt_encoder_init so the mode-dependent
// arrays are the right size. Required because opus_encoder_init lazily
// installs the mode.
//
// Applies opus_encoder_init's post-init CTL tweaks (signalling=0 and
// complexity mirrored from silk_mode.complexity) to match what
// opus_encoder_init would have done if a built-in mode were available.
func ExportSetEncoderCeltMode(st *OpusEncoder, h CeltModeHandle) int {
	if st.celt_enc == nil {
		return OPUS_INTERNAL_ERROR
	}
	ret := celt_encoder_init(st.celt_enc, h.p, st.Fs, st.channels, st.arch)
	if ret == OPUS_OK {
		st.celt_enc.signalling = 0
		st.celt_enc.complexity = int(st.silk_mode.complexity)
	}
	return ret
}

// ExportEncoderGetRangeFinal returns rangeFinal for state comparison.
func ExportEncoderGetRangeFinal(st *OpusEncoder) uint32 {
	return uint32(st.rangeFinal)
}

// ExportEncoderGetPrevMode returns prev_mode for state comparison.
func ExportEncoderGetPrevMode(st *OpusEncoder) int { return st.prev_mode }

// ExportEncoderGetFirst returns the `first` field.
func ExportEncoderGetFirst(st *OpusEncoder) int { return st.first }
