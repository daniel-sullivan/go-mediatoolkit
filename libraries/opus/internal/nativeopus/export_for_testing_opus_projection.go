package nativeopus

// Test shims for opus_projection_encoder / opus_projection_decoder.
// The projection encode/decode paths both reach into the underlying
// multistream encoder/decoder, which in turn reaches into each
// per-stream OpusEncoder / OpusDecoder. Tests can inject the
// fully-populated CELT mode mirror so bit-exact parity with the C
// oracle is achievable.

// OpusProjectionEncoderHandle wraps an OpusProjectionEncoder for tests.
type OpusProjectionEncoderHandle struct{ p *OpusProjectionEncoder }

// OpusProjectionDecoderHandle wraps an OpusProjectionDecoder for tests.
type OpusProjectionDecoderHandle struct{ p *OpusProjectionDecoder }

// ExportProjectionAmbisonicsEncoderGetSize mirrors
// opus_projection_ambisonics_encoder_get_size for get_size parity.
func ExportProjectionAmbisonicsEncoderGetSize(channels, mappingFamily int) int32 {
	return int32(opus_projection_ambisonics_encoder_get_size(channels, mappingFamily))
}

// ExportProjectionDecoderGetSize mirrors opus_projection_decoder_get_size.
func ExportProjectionDecoderGetSize(channels, streams, coupledStreams int) int32 {
	return int32(opus_projection_decoder_get_size(channels, streams, coupledStreams))
}

// ExportProjectionAmbisonicsEncoderInit drives
// opus_projection_ambisonics_encoder_init and returns the resulting
// handle plus the chosen (streams, coupled) pair.
func ExportProjectionAmbisonicsEncoderInit(Fs int32, channels, mappingFamily, application int,
	installMode func(*OpusEncoder) int,
) (OpusProjectionEncoderHandle, int, int, int) {
	var streams, coupled int
	st := &OpusProjectionEncoder{}
	ret := opus_projection_ambisonics_encoder_init(st, opus_int32(Fs),
		channels, mappingFamily, &streams, &coupled, application)
	if ret != OPUS_OK {
		return OpusProjectionEncoderHandle{p: st}, streams, coupled, ret
	}
	if installMode != nil && st.ms_encoder != nil {
		for _, enc := range st.ms_encoder.encoders {
			if r := installMode(enc); r != OPUS_OK {
				return OpusProjectionEncoderHandle{p: st}, streams, coupled, r
			}
		}
	}
	return OpusProjectionEncoderHandle{p: st}, streams, coupled, OPUS_OK
}

// NumStreams returns nb_streams from the underlying MS encoder.
func (h OpusProjectionEncoderHandle) NumStreams() int {
	return h.p.ms_encoder.layout.nb_streams
}

// NumCoupled returns nb_coupled_streams from the underlying MS encoder.
func (h OpusProjectionEncoderHandle) NumCoupled() int {
	return h.p.ms_encoder.layout.nb_coupled_streams
}

// NumChannels returns nb_channels from the underlying MS encoder.
func (h OpusProjectionEncoderHandle) NumChannels() int {
	return h.p.ms_encoder.layout.nb_channels
}

// MixingRows returns the number of rows in the selected mixing matrix.
func (h OpusProjectionEncoderHandle) MixingRows() int { return h.p.mixing_matrix.rows }

// MixingCols returns the number of cols in the selected mixing matrix.
func (h OpusProjectionEncoderHandle) MixingCols() int { return h.p.mixing_matrix.cols }

// DemixingRows returns the number of rows in the selected demixing matrix.
func (h OpusProjectionEncoderHandle) DemixingRows() int { return h.p.demixing_matrix.rows }

// DemixingCols returns the number of cols in the selected demixing matrix.
func (h OpusProjectionEncoderHandle) DemixingCols() int { return h.p.demixing_matrix.cols }

// DemixingGain returns the gain field of the selected demixing matrix.
func (h OpusProjectionEncoderHandle) DemixingGain() int { return h.p.demixing_matrix.gain }

// DemixingMatrixSizeBytes — serialized demixing matrix size.
func (h OpusProjectionEncoderHandle) DemixingMatrixSizeBytes() int32 {
	return int32(h.p.demixing_matrix_size_in_bytes)
}

// Ctl forwards a projection-encoder ctl request.
func (h OpusProjectionEncoderHandle) Ctl(request int, args ...interface{}) int {
	return opus_projection_encoder_ctl(h.p, request, args...)
}

// EncodeFloat wraps opus_projection_encode_float.
func (h OpusProjectionEncoderHandle) EncodeFloat(pcm []float32, frameSize int,
	data []byte) int {
	return opus_projection_encode_float(h.p, pcm, frameSize, data, opus_int32(len(data)))
}

// EncodeInt16 wraps opus_projection_encode.
func (h OpusProjectionEncoderHandle) EncodeInt16(pcm []int16, frameSize int,
	data []byte) int {
	p := make([]opus_int16, len(pcm))
	for i, v := range pcm {
		p[i] = opus_int16(v)
	}
	return opus_projection_encode(h.p, p, frameSize, data, opus_int32(len(data)))
}

// Decode wraps opus_projection_decode (int16).
func (h OpusProjectionDecoderHandle) Decode(data []byte, pcm []int16, frameSize, decodeFEC int) int {
	p := make([]opus_int16, len(pcm))
	length := opus_int32(len(data))
	if data == nil {
		length = 0
	}
	ret := opus_projection_decode(h.p, data, length, p, frameSize, decodeFEC)
	for i := range pcm {
		pcm[i] = int16(p[i])
	}
	return ret
}

// DecodeFloat wraps opus_projection_decode_float.
func (h OpusProjectionDecoderHandle) DecodeFloat(data []byte, pcm []float32, frameSize, decodeFEC int) int {
	length := opus_int32(len(data))
	if data == nil {
		length = 0
	}
	return opus_projection_decode_float(h.p, data, length, pcm, frameSize, decodeFEC)
}

// Ctl forwards a projection-decoder ctl request.
func (h OpusProjectionDecoderHandle) Ctl(request int, args ...interface{}) int {
	return opus_projection_decoder_ctl(h.p, request, args...)
}

// NumStreams returns the underlying MS decoder's stream count.
func (h OpusProjectionDecoderHandle) NumStreams() int {
	return h.p.ms_decoder.layout.nb_streams
}

// NumCoupled returns the underlying MS decoder's coupled-stream count.
func (h OpusProjectionDecoderHandle) NumCoupled() int {
	return h.p.ms_decoder.layout.nb_coupled_streams
}

// NumChannels returns the underlying MS decoder's nb_channels.
func (h OpusProjectionDecoderHandle) NumChannels() int {
	return h.p.ms_decoder.layout.nb_channels
}

// DemixingRows / DemixingCols expose the stored demixing matrix shape.
func (h OpusProjectionDecoderHandle) DemixingRows() int { return h.p.demixing_matrix.rows }
func (h OpusProjectionDecoderHandle) DemixingCols() int { return h.p.demixing_matrix.cols }

// ExportProjectionDecoderInit creates an OpusProjectionDecoder attached
// to the supplied CELT mode mirror. Returns (handle, return_code).
func ExportProjectionDecoderInit(mode CeltModeHandle, Fs int32, channels, streams, coupledStreams int,
	demixingMatrix []byte) (OpusProjectionDecoderHandle, int) {
	st := &OpusProjectionDecoder{}
	ret := opus_projection_decoder_init(st, opus_int32(Fs), channels, streams, coupledStreams,
		demixingMatrix, opus_int32(len(demixingMatrix)))
	if ret != OPUS_OK {
		return OpusProjectionDecoderHandle{p: st}, ret
	}
	for _, dec := range st.ms_decoder.decoders {
		if rc := celt_decoder_init(dec.celt_dec, mode.p, opus_int32(Fs), dec.channels); rc != OPUS_OK {
			return OpusProjectionDecoderHandle{p: st}, rc
		}
		dec.celt_dec.signalling = 0
	}
	return OpusProjectionDecoderHandle{p: st}, OPUS_OK
}
