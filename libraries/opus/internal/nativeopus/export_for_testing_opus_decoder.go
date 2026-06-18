package nativeopus

// Test shims for opus_decoder. Because our CELT decoder requires an
// explicit static mode pointer (the static_modes_float.h tables are
// not yet ported — Phase 11), the Go opus_decoder_init cannot auto-
// allocate a mode the way the C reference does. Tests therefore hand
// in a mode mirror (built from the C oracle via Cgo getters) and we
// finish CELT initialisation here.

// OpusDecoderHandle — opaque wrapper for tests.
type OpusDecoderHandle struct{ p *OpusDecoder }

// NewOpusDecoder constructs an OpusDecoder attached to the supplied
// CELT mode mirror. Returns (handle, return_code).
func NewOpusDecoder(mode CeltModeHandle, Fs int32, channels int) (OpusDecoderHandle, int) {
	st := &OpusDecoder{}
	ret := opus_decoder_init(st, opus_int32(Fs), channels)
	if ret != OPUS_OK {
		return OpusDecoderHandle{p: st}, ret
	}
	// Now that SILK is initialised, attach the CELT mode and finish
	// the CELT init that opus_decoder_init skipped (because the
	// default celt_dec has no mode installed in the Go port).
	if rc := celt_decoder_init(st.celt_dec, mode.p, opus_int32(Fs), channels); rc != OPUS_OK {
		return OpusDecoderHandle{p: st}, rc
	}
	// Mirror `celt_decoder_ctl(celt_dec, CELT_SET_SIGNALLING(0))`.
	st.celt_dec.signalling = 0
	return OpusDecoderHandle{p: st}, OPUS_OK
}

// Decode — int16 output. Mirrors opus_decode.
func (h OpusDecoderHandle) Decode(data []byte, pcm []int16, frameSize, decodeFEC int) int {
	length := int32(len(data))
	if data == nil {
		length = 0
	}
	return opus_decode(h.p, data, opus_int32(length), pcm, frameSize, decodeFEC)
}

// DecodeFloat — float output. Mirrors opus_decode_float.
func (h OpusDecoderHandle) DecodeFloat(data []byte, pcm []float32, frameSize, decodeFEC int) int {
	length := int32(len(data))
	if data == nil {
		length = 0
	}
	return opus_decode_float(h.p, data, opus_int32(length), pcm, frameSize, decodeFEC)
}

// Reset — invoke OPUS_RESET_STATE.
func (h OpusDecoderHandle) Reset() int {
	return opus_decoder_ctl(h.p, OPUS_RESET_STATE)
}

// FinalRange — read OPUS_GET_FINAL_RANGE.
func (h OpusDecoderHandle) FinalRange() uint32 {
	var v opus_uint32
	opus_decoder_ctl(h.p, OPUS_GET_FINAL_RANGE_REQUEST, &v)
	return uint32(v)
}

// LastPacketDuration — read OPUS_GET_LAST_PACKET_DURATION.
func (h OpusDecoderHandle) LastPacketDuration() int {
	var v opus_int32
	opus_decoder_ctl(h.p, OPUS_GET_LAST_PACKET_DURATION_REQUEST, &v)
	return int(v)
}

// Bandwidth — read OPUS_GET_BANDWIDTH.
func (h OpusDecoderHandle) Bandwidth() int {
	var v opus_int32
	opus_decoder_ctl(h.p, OPUS_GET_BANDWIDTH_REQUEST, &v)
	return int(v)
}
