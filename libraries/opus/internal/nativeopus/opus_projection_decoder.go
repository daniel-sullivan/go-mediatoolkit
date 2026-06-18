package nativeopus

// Port of libopus/src/opus_projection_decoder.c. Float path only.
//
// Symmetric to opus_projection_encoder: a thin wrapper that post-applies
// a "demixing matrix" (per-stream PCM -> output channel PCM) after the
// multistream decode path produces its per-stream samples. The matrix
// arrives in the bitstream (encoder-side) and is stored unmodified
// here for use by every decode call.

// OpusProjectionDecoder — C: struct OpusProjectionDecoder
// (opus_projection_decoder.c:41). The C struct only stores
// demixing_matrix_size_in_bytes; in Go we also keep the concrete
// MappingMatrix value and the sub-OpusMSDecoder.
type OpusProjectionDecoder struct {
	demixing_matrix_size_in_bytes opus_int32

	demixing_matrix MappingMatrix
	ms_decoder      *OpusMSDecoder
}

// opus_projection_copy_channel_out_float — C: opus_projection_decoder.c:48.
func opus_projection_copy_channel_out_float(
	dst interface{},
	dst_stride int,
	dst_channel int,
	src []opus_res,
	src_stride int,
	frame_size int,
	user_data interface{},
) {
	float_dst := dst.([]float32)
	matrix := user_data.(*MappingMatrix)

	if dst_channel == 0 {
		// OPUS_CLEAR(float_dst, frame_size * dst_stride).
		n := frame_size * dst_stride
		for i := 0; i < n; i++ {
			float_dst[i] = 0
		}
	}

	if src != nil {
		mapping_matrix_multiply_channel_out_float(matrix, src, dst_channel,
			src_stride, float_dst, dst_stride, frame_size)
	}
}

// opus_projection_copy_channel_out_short — C: opus_projection_decoder.c:71.
func opus_projection_copy_channel_out_short(
	dst interface{},
	dst_stride int,
	dst_channel int,
	src []opus_res,
	src_stride int,
	frame_size int,
	user_data interface{},
) {
	short_dst := dst.([]opus_int16)
	matrix := user_data.(*MappingMatrix)

	if dst_channel == 0 {
		n := frame_size * dst_stride
		for i := 0; i < n; i++ {
			short_dst[i] = 0
		}
	}

	if src != nil {
		mapping_matrix_multiply_channel_out_short(matrix, src, dst_channel,
			src_stride, short_dst, dst_stride, frame_size)
	}
}

// opus_projection_copy_channel_out_int24 — C: opus_projection_decoder.c:92.
func opus_projection_copy_channel_out_int24(
	dst interface{},
	dst_stride int,
	dst_channel int,
	src []opus_res,
	src_stride int,
	frame_size int,
	user_data interface{},
) {
	int24_dst := dst.([]opus_int32)
	matrix := user_data.(*MappingMatrix)

	if dst_channel == 0 {
		n := frame_size * dst_stride
		for i := 0; i < n; i++ {
			int24_dst[i] = 0
		}
	}

	if src != nil {
		mapping_matrix_multiply_channel_out_int24(matrix, src, dst_channel,
			src_stride, int24_dst, dst_stride, frame_size)
	}
}

// opus_projection_decoder_get_size — C: opus_projection_decoder.c:128.
func opus_projection_decoder_get_size(channels, streams, coupled_streams int) opus_int32 {
	matrix_size := mapping_matrix_get_size(streams+coupled_streams, channels)
	if matrix_size == 0 {
		return 0
	}
	decoder_size := opus_multistream_decoder_get_size(streams, coupled_streams)
	if decoder_size == 0 {
		return 0
	}
	return opus_int32(mm_align(opusProjectionDecoderSizeofStruct())) +
		matrix_size + decoder_size
}

// opusProjectionDecoderSizeofStruct — placeholder for
// `sizeof(OpusProjectionDecoder)` (one opus_int32 → 4 bytes).
func opusProjectionDecoderSizeofStruct() int { return 4 }

// opus_projection_decoder_init — C: opus_projection_decoder.c:146.
func opus_projection_decoder_init(st *OpusProjectionDecoder, Fs opus_int32,
	channels, streams, coupled_streams int,
	demixing_matrix []byte, demixing_matrix_size opus_int32) int {
	var mapping [255]byte

	// Verify supplied matrix size.
	nb_input_streams := streams + coupled_streams
	expected_matrix_size := opus_int32(nb_input_streams) * opus_int32(channels) * 2
	if expected_matrix_size != demixing_matrix_size {
		return OPUS_BAD_ARG
	}

	// Convert demixing matrix input into internal format (sign-extended
	// little-endian opus_int16).
	buf := make([]opus_int16, nb_input_streams*channels)
	for i := 0; i < nb_input_streams*channels; i++ {
		s := int(demixing_matrix[2*i+1])<<8 | int(demixing_matrix[2*i])
		s = ((s & 0xFFFF) ^ 0x8000) - 0x8000
		buf[i] = opus_int16(s)
	}

	// Assign demixing matrix.
	st.demixing_matrix_size_in_bytes = mapping_matrix_get_size(channels, nb_input_streams)
	if st.demixing_matrix_size_in_bytes == 0 {
		return OPUS_BAD_ARG
	}

	mapping_matrix_init(&st.demixing_matrix, channels, nb_input_streams, 0,
		buf, demixing_matrix_size)

	// Set trivial mapping so each input channel pairs with a matrix column.
	for i := 0; i < channels; i++ {
		mapping[i] = byte(i)
	}

	st.ms_decoder = &OpusMSDecoder{}
	return opus_multistream_decoder_init(
		st.ms_decoder, Fs, channels, streams, coupled_streams, mapping[:])
}

// opus_projection_decoder_create — C: opus_projection_decoder.c:197.
func opus_projection_decoder_create(Fs opus_int32, channels, streams, coupled_streams int,
	demixing_matrix []byte, demixing_matrix_size opus_int32, error_ *int) *OpusProjectionDecoder {
	size := opus_projection_decoder_get_size(channels, streams, coupled_streams)
	if size == 0 {
		if error_ != nil {
			*error_ = OPUS_ALLOC_FAIL
		}
		return nil
	}
	st := &OpusProjectionDecoder{}
	ret := opus_projection_decoder_init(st, Fs, channels, streams, coupled_streams,
		demixing_matrix, demixing_matrix_size)
	if ret != OPUS_OK {
		if error_ != nil {
			*error_ = ret
		}
		return nil
	}
	if error_ != nil {
		*error_ = ret
	}
	return st
}

// OPTIONAL_CLIP_PROJECTION — C: opus_projection_decoder.c:233 (non-FIXED_POINT: 1).
const OPTIONAL_CLIP_PROJECTION = 1

// opus_projection_decode — C: opus_projection_decoder.c:239.
func opus_projection_decode(st *OpusProjectionDecoder, data []byte, length opus_int32,
	pcm []opus_int16, frame_size, decode_fec int) int {
	return opus_multistream_decode_native(st.ms_decoder, data, length,
		pcm, opus_projection_copy_channel_out_short, frame_size, decode_fec,
		OPTIONAL_CLIP_PROJECTION, &st.demixing_matrix)
}

// opus_projection_decode24 — C: opus_projection_decoder.c:248.
func opus_projection_decode24(st *OpusProjectionDecoder, data []byte, length opus_int32,
	pcm []opus_int32, frame_size, decode_fec int) int {
	return opus_multistream_decode_native(st.ms_decoder, data, length,
		pcm, opus_projection_copy_channel_out_int24, frame_size, decode_fec,
		0, &st.demixing_matrix)
}

// opus_projection_decode_float — C: opus_projection_decoder.c:258.
func opus_projection_decode_float(st *OpusProjectionDecoder, data []byte, length opus_int32,
	pcm []float32, frame_size, decode_fec int) int {
	return opus_multistream_decode_native(st.ms_decoder, data, length,
		pcm, opus_projection_copy_channel_out_float, frame_size, decode_fec,
		0, &st.demixing_matrix)
}

// opus_projection_decoder_ctl — C: opus_projection_decoder.c:267.
func opus_projection_decoder_ctl(st *OpusProjectionDecoder, request int, args ...interface{}) int {
	return opus_multistream_decoder_ctl(st.ms_decoder, request, args...)
}

// opus_projection_decoder_destroy — C: opus_projection_decoder.c:279.
// No-op in Go; kept for API parity.
func opus_projection_decoder_destroy(st *OpusProjectionDecoder) { _ = st }
