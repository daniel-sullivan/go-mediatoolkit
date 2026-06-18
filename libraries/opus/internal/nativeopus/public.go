package nativeopus

// Exported entry points for the parent libraries/opus public API.
//
// internal/nativeopus is a Go-private port of the vendored C libopus; the
// non-test exports live here so the wrapping Encoder / Decoder types in
// the parent package can bind to them without pulling in the test shims.

// Error codes re-exported so the parent package can map to its error
// sentinel values without re-declaring them.
const (
	ErrorOK             = OPUS_OK
	ErrorBadArg         = OPUS_BAD_ARG
	ErrorBufferTooSmall = OPUS_BUFFER_TOO_SMALL
	ErrorInternalError  = OPUS_INTERNAL_ERROR
	ErrorInvalidPacket  = OPUS_INVALID_PACKET
	ErrorUnimplemented  = OPUS_UNIMPLEMENTED
	ErrorInvalidState   = OPUS_INVALID_STATE
	ErrorAllocFail      = OPUS_ALLOC_FAIL
)

// Application constants.
const (
	ApplicationVoIP               = OPUS_APPLICATION_VOIP
	ApplicationAudio              = OPUS_APPLICATION_AUDIO
	ApplicationRestrictedLowdelay = OPUS_APPLICATION_RESTRICTED_LOWDELAY
)

// CTL request codes (only the ones the public API drives).
const (
	CtlSetBitrate             = OPUS_SET_BITRATE_REQUEST
	CtlSetComplexity          = OPUS_SET_COMPLEXITY_REQUEST
	CtlSetApplication         = OPUS_SET_APPLICATION_REQUEST
	CtlSetLSBDepth            = OPUS_SET_LSB_DEPTH_REQUEST
	CtlSetExpertFrameDuration = OPUS_SET_EXPERT_FRAME_DURATION_REQUEST
	CtlSetVBR                 = OPUS_SET_VBR_REQUEST
	CtlSetVBRConstraint       = OPUS_SET_VBR_CONSTRAINT_REQUEST
	CtlSetBandwidth           = OPUS_SET_BANDWIDTH_REQUEST
	CtlSetInbandFEC           = OPUS_SET_INBAND_FEC_REQUEST
	CtlSetForceChannels       = OPUS_SET_FORCE_CHANNELS_REQUEST
	CtlSetDTX                 = OPUS_SET_DTX_REQUEST
	CtlSetPacketLossPerc      = OPUS_SET_PACKET_LOSS_PERC_REQUEST
	CtlResetState             = OPUS_RESET_STATE

	// AutoValue is the "automatic / default" sentinel used by CTLs
	// like SET_BANDWIDTH or SET_FORCE_CHANNELS.
	AutoValue = OPUS_AUTO
)

// Frame-size signalling constants for OPUS_SET_EXPERT_FRAME_DURATION.
const (
	FrameSizeArg   = OPUS_FRAMESIZE_ARG
	FrameSize2_5ms = OPUS_FRAMESIZE_2_5_MS
	FrameSize5ms   = OPUS_FRAMESIZE_5_MS
	FrameSize10ms  = OPUS_FRAMESIZE_10_MS
	FrameSize20ms  = OPUS_FRAMESIZE_20_MS
	FrameSize40ms  = OPUS_FRAMESIZE_40_MS
	FrameSize60ms  = OPUS_FRAMESIZE_60_MS
	FrameSize80ms  = OPUS_FRAMESIZE_80_MS
	FrameSize100ms = OPUS_FRAMESIZE_100_MS
	FrameSize120ms = OPUS_FRAMESIZE_120_MS
)

// NewEncoder allocates and initializes a Go OpusEncoder for Fs/channels/app.
// Returns nil and the opus error code on failure.
func NewEncoder(Fs int32, channels, application int) (*OpusEncoder, int) {
	var err int
	st := opus_encoder_create(opus_int32(Fs), channels, application, &err)
	if err != OPUS_OK {
		return nil, err
	}
	return st, OPUS_OK
}

// DestroyEncoder releases any resources (currently a no-op in Go).
func DestroyEncoder(st *OpusEncoder) { opus_encoder_destroy(st) }

// EncoderCtl dispatches a CTL request against the encoder. Matches C's
// variadic opus_encoder_ctl API: callers supply the request code plus any
// SET-value / GET-pointer arguments the request expects.
func EncoderCtl(st *OpusEncoder, request int, args ...interface{}) int {
	return opus_encoder_ctl(st, request, args...)
}

// EncodeFloat encodes one frame of float32 PCM into data. Returns the
// number of bytes written, or a negative opus error code.
func EncodeFloat(st *OpusEncoder, pcm []float32, analysisFrameSize int, data []byte) int {
	ret := opus_encode_float(st, pcm, analysisFrameSize, data, opus_int32(len(data)))
	return int(ret)
}

// EncodeInt16 encodes one frame of int16 PCM into data. Returns the
// number of bytes written, or a negative opus error code.
func EncodeInt16(st *OpusEncoder, pcm []int16, analysisFrameSize int, data []byte) int {
	conv := make([]opus_int16, len(pcm))
	for i, v := range pcm {
		conv[i] = opus_int16(v)
	}
	ret := opus_encode(st, conv, analysisFrameSize, data, opus_int32(len(data)))
	return int(ret)
}

// ResetEncoder issues OPUS_RESET_STATE.
func ResetEncoder(st *OpusEncoder) int {
	return opus_encoder_ctl(st, OPUS_RESET_STATE)
}

// EncoderSampleRate returns the configured input sample rate.
func EncoderSampleRate(st *OpusEncoder) int { return int(st.Fs) }

// EncoderChannels returns the configured channel count.
func EncoderChannels(st *OpusEncoder) int { return st.channels }

// NewDecoder allocates and initializes a Go OpusDecoder for Fs/channels.
// Returns nil and the opus error code on failure.
func NewDecoder(Fs int32, channels int) (*OpusDecoder, int) {
	var err int
	st := opus_decoder_create(opus_int32(Fs), channels, &err)
	if err != OPUS_OK {
		return nil, err
	}
	return st, OPUS_OK
}

// DestroyDecoder releases any resources (no-op in Go).
func DestroyDecoder(st *OpusDecoder) { /* no-op in Go */ }

// DecodeFloat decodes data into float32 PCM. Pass data == nil for PLC.
// Returns the number of samples per channel decoded, or a negative opus
// error code.
func DecodeFloat(st *OpusDecoder, data []byte, pcm []float32, frameSize int, decodeFec int) int {
	length := opus_int32(0)
	if data != nil {
		length = opus_int32(len(data))
	}
	return opus_decode_float(st, data, length, pcm, frameSize, decodeFec)
}

// DecodeInt16 decodes data into int16 PCM. Pass data == nil for PLC.
// Returns the number of samples per channel decoded, or a negative opus
// error code.
func DecodeInt16(st *OpusDecoder, data []byte, pcm []int16, frameSize int, decodeFec int) int {
	length := opus_int32(0)
	if data != nil {
		length = opus_int32(len(data))
	}
	conv := make([]opus_int16, len(pcm))
	n := opus_decode(st, data, length, conv, frameSize, decodeFec)
	if n > 0 {
		for i, v := range conv[:n*st.channels] {
			pcm[i] = int16(v)
		}
	}
	return n
}

// DecodeInt24 decodes data into 24-bit PCM packed in int32 (sign-extended,
// range [-2^24, 2^24]). Pass data == nil for PLC. Useful for parity
// comparisons against opus_demo's -24 / -f32 output modes, which expose
// the float decode at ~24-bit precision without going through the
// soft-clip + float2int16 conversion applied by opus_decode.
func DecodeInt24(st *OpusDecoder, data []byte, pcm []int32, frameSize int, decodeFec int) int {
	length := opus_int32(0)
	if data != nil {
		length = opus_int32(len(data))
	}
	conv := make([]opus_int32, len(pcm))
	n := opus_decode24(st, data, length, conv, frameSize, decodeFec)
	if n > 0 {
		for i, v := range conv[:n*st.channels] {
			pcm[i] = int32(v)
		}
	}
	return n
}

// ResetDecoder issues OPUS_RESET_STATE.
func ResetDecoder(st *OpusDecoder) int {
	return opus_decoder_ctl(st, OPUS_RESET_STATE)
}

// DecoderSampleRate returns the configured output sample rate.
func DecoderSampleRate(st *OpusDecoder) int { return int(st.Fs) }

// DecoderChannels returns the configured channel count.
func DecoderChannels(st *OpusDecoder) int { return st.channels }

// DecoderLastPacketDuration returns the last packet's samples/channel.
func DecoderLastPacketDuration(st *OpusDecoder) int { return int(st.last_packet_duration) }
