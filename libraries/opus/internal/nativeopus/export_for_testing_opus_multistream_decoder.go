package nativeopus

// Test shims for opus_multistream_decoder. The Go-side multistream
// decoder wraps per-stream OpusDecoder instances, each of which needs
// its CELT mode populated with the full MDCT/FFT/trig tables before
// decoding. Callers pass in a fully-built CeltModeHandle mirror
// (as used by buildFullGoMode in benchcmp) so every sub-decoder runs
// with bit-exact state.

// OpusMSDecoderHandle — opaque wrapper for tests.
type OpusMSDecoderHandle struct{ p *OpusMSDecoder }

// NewOpusMSDecoder constructs an OpusMSDecoder attached to the supplied
// CELT mode mirror. Returns (handle, return_code).
func NewOpusMSDecoder(mode CeltModeHandle, Fs int32, channels, streams, coupledStreams int, mapping []byte) (OpusMSDecoderHandle, int) {
	st := &OpusMSDecoder{}
	ret := opus_multistream_decoder_init(st, opus_int32(Fs), channels, streams, coupledStreams, mapping)
	if ret != OPUS_OK {
		return OpusMSDecoderHandle{p: st}, ret
	}
	// Re-install the fully-populated CELT mode mirror on each sub-decoder
	// so tables match the C oracle bit-for-bit.
	for _, dec := range st.decoders {
		if rc := celt_decoder_init(dec.celt_dec, mode.p, opus_int32(Fs), dec.channels); rc != OPUS_OK {
			return OpusMSDecoderHandle{p: st}, rc
		}
		dec.celt_dec.signalling = 0
	}
	return OpusMSDecoderHandle{p: st}, OPUS_OK
}

// Decode — int16 output. Mirrors opus_multistream_decode.
func (h OpusMSDecoderHandle) Decode(data []byte, pcm []int16, frameSize, decodeFEC int) int {
	length := int32(len(data))
	if data == nil {
		length = 0
	}
	return opus_multistream_decode(h.p, data, opus_int32(length),
		pcm, frameSize, decodeFEC)
}

// DecodeFloat — float output. Mirrors opus_multistream_decode_float.
func (h OpusMSDecoderHandle) DecodeFloat(data []byte, pcm []float32, frameSize, decodeFEC int) int {
	length := int32(len(data))
	if data == nil {
		length = 0
	}
	return opus_multistream_decode_float(h.p, data, opus_int32(length),
		pcm, frameSize, decodeFEC)
}

// Decode24 — int32 (24-bit) output. Mirrors opus_multistream_decode24.
func (h OpusMSDecoderHandle) Decode24(data []byte, pcm []int32, frameSize, decodeFEC int) int {
	length := int32(len(data))
	if data == nil {
		length = 0
	}
	return opus_multistream_decode24(h.p, data, opus_int32(length),
		pcm, frameSize, decodeFEC)
}

// Reset — invoke OPUS_RESET_STATE on every sub-decoder.
func (h OpusMSDecoderHandle) Reset() int {
	return opus_multistream_decoder_ctl(h.p, OPUS_RESET_STATE)
}

// FinalRange — read OPUS_GET_FINAL_RANGE (XOR across all streams).
func (h OpusMSDecoderHandle) FinalRange() uint32 {
	var v opus_uint32
	opus_multistream_decoder_ctl(h.p, OPUS_GET_FINAL_RANGE_REQUEST, &v)
	return uint32(v)
}

// SampleRate — read OPUS_GET_SAMPLE_RATE on the first stream.
func (h OpusMSDecoderHandle) SampleRate() int32 {
	var v opus_int32
	opus_multistream_decoder_ctl(h.p, OPUS_GET_SAMPLE_RATE_REQUEST, &v)
	return int32(v)
}

// StreamFinalRange returns the final range of the given sub-stream.
func (h OpusMSDecoderHandle) StreamFinalRange(stream int) uint32 {
	var sub *OpusDecoder
	opus_multistream_decoder_ctl(h.p, OPUS_MULTISTREAM_GET_DECODER_STATE_REQUEST, opus_int32(stream), &sub)
	var v opus_uint32
	opus_decoder_ctl(sub, OPUS_GET_FINAL_RANGE_REQUEST, &v)
	return uint32(v)
}

// NumStreams returns the total number of streams.
func (h OpusMSDecoderHandle) NumStreams() int { return h.p.layout.nb_streams }

// NumCoupled returns the number of coupled (stereo) streams.
func (h OpusMSDecoderHandle) NumCoupled() int { return h.p.layout.nb_coupled_streams }

// NumChannels returns the total number of output channels.
func (h OpusMSDecoderHandle) NumChannels() int { return h.p.layout.nb_channels }

// Mapping returns a copy of the first nb_channels bytes of the mapping.
func (h OpusMSDecoderHandle) Mapping() []byte {
	out := make([]byte, h.p.layout.nb_channels)
	copy(out, h.p.layout.mapping[:h.p.layout.nb_channels])
	return out
}

// GetSize returns the C arena size for get_size parity.
func GetMSDecoderSize(streams, coupledStreams int) int32 {
	return int32(opus_multistream_decoder_get_size(streams, coupledStreams))
}
