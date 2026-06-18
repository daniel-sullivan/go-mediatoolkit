package nativeopus

// Port of libopus/src/opus_projection_encoder.c. Float path only.
//
// The projection encoder is a thin wrapper around OpusMSEncoder that
// pre-applies a "mixing matrix" (input PCM -> per-stream PCM) before
// handing the result to the multistream encode path. The auxiliary
// demixing matrix is stashed next to the mixing matrix so the caller
// can retrieve it via ctl (it travels with the compressed bitstream
// so the decoder can undo the projection).
//
// C stores { OpusProjectionEncoder, mixing_matrix, demixing_matrix,
//   OpusMSEncoder } flat in one allocation via align() arithmetic.
// The Go port replaces that layout with explicit fields; every public
// function follows the same algorithm step-for-step.

// OPUS_PROJECTION_GET_DEMIXING_MATRIX_GAIN_REQUEST — C: opus_projection.h:48.
const OPUS_PROJECTION_GET_DEMIXING_MATRIX_GAIN_REQUEST = 6001

// OPUS_PROJECTION_GET_DEMIXING_MATRIX_SIZE_REQUEST — C: opus_projection.h:49.
const OPUS_PROJECTION_GET_DEMIXING_MATRIX_SIZE_REQUEST = 6003

// OPUS_PROJECTION_GET_DEMIXING_MATRIX_REQUEST — C: opus_projection.h:50.
const OPUS_PROJECTION_GET_DEMIXING_MATRIX_REQUEST = 6005

// OpusProjectionEncoder — C: struct OpusProjectionEncoder
// (opus_projection_encoder.c:41). The C struct only stores
// mixing/demixing_matrix_size_in_bytes; in Go we also keep the
// concrete MappingMatrix values and the sub-OpusMSEncoder.
type OpusProjectionEncoder struct {
	mixing_matrix_size_in_bytes   opus_int32
	demixing_matrix_size_in_bytes opus_int32

	mixing_matrix   MappingMatrix
	demixing_matrix MappingMatrix
	ms_encoder      *OpusMSEncoder
}

// opus_projection_copy_channel_in_float — C: opus_projection_encoder.c:49.
func opus_projection_copy_channel_in_float(
	dst []opus_res,
	dst_stride int,
	src interface{},
	src_stride int,
	src_channel int,
	frame_size int,
	user_data interface{},
) {
	matrix := user_data.(*MappingMatrix)
	mapping_matrix_multiply_channel_in_float(matrix,
		src.([]float32), src_stride, dst, src_channel, dst_stride, frame_size)
}

// opus_projection_copy_channel_in_short — C: opus_projection_encoder.c:64.
func opus_projection_copy_channel_in_short(
	dst []opus_res,
	dst_stride int,
	src interface{},
	src_stride int,
	src_channel int,
	frame_size int,
	user_data interface{},
) {
	matrix := user_data.(*MappingMatrix)
	mapping_matrix_multiply_channel_in_short(matrix,
		src.([]opus_int16), src_stride, dst, src_channel, dst_stride, frame_size)
}

// opus_projection_copy_channel_in_int24 — C: opus_projection_encoder.c:78.
func opus_projection_copy_channel_in_int24(
	dst []opus_res,
	dst_stride int,
	src interface{},
	src_stride int,
	src_channel int,
	frame_size int,
	user_data interface{},
) {
	matrix := user_data.(*MappingMatrix)
	mapping_matrix_multiply_channel_in_int24(matrix,
		src.([]opus_int32), src_stride, dst, src_channel, dst_stride, frame_size)
}

// get_order_plus_one_from_channels — C: opus_projection_encoder.c:92.
//
// Ambisonic channel count must be (order+1)^2 + 2j, with j in {0, 1}.
// Returns OPUS_BAD_ARG otherwise.
func get_order_plus_one_from_channels(channels int, order_plus_one *int) int {
	var order_plus_one_, acn_channels, nondiegetic_channels int

	if channels < 1 || channels > 227 {
		return OPUS_BAD_ARG
	}

	order_plus_one_ = int(isqrt32(opus_uint32(channels)))
	acn_channels = order_plus_one_ * order_plus_one_
	nondiegetic_channels = channels - acn_channels
	if nondiegetic_channels != 0 && nondiegetic_channels != 2 {
		return OPUS_BAD_ARG
	}

	if order_plus_one != nil {
		*order_plus_one = order_plus_one_
	}
	return OPUS_OK
}

// get_streams_from_channels — C: opus_projection_encoder.c:115.
func get_streams_from_channels(channels, mapping_family int,
	streams, coupled_streams, order_plus_one *int) int {
	if mapping_family == 3 {
		if get_order_plus_one_from_channels(channels, order_plus_one) != OPUS_OK {
			return OPUS_BAD_ARG
		}
		if streams != nil {
			*streams = (channels + 1) / 2
		}
		if coupled_streams != nil {
			*coupled_streams = channels / 2
		}
		return OPUS_OK
	}
	return OPUS_BAD_ARG
}

// projection_tables_for — selects the mixing and demixing MappingMatrix
// pair + _data[] slices for a given order_plus_one. Returns nil for
// orders outside [2, 6].
func projection_tables_for(order_plus_one int) (
	mixing *MappingMatrix, mixing_data []opus_int16,
	demixing *MappingMatrix, demixing_data []opus_int16,
) {
	switch order_plus_one {
	case 2:
		return &mapping_matrix_foa_mixing, mapping_matrix_foa_mixing_data[:],
			&mapping_matrix_foa_demixing, mapping_matrix_foa_demixing_data[:]
	case 3:
		return &mapping_matrix_soa_mixing, mapping_matrix_soa_mixing_data[:],
			&mapping_matrix_soa_demixing, mapping_matrix_soa_demixing_data[:]
	case 4:
		return &mapping_matrix_toa_mixing, mapping_matrix_toa_mixing_data[:],
			&mapping_matrix_toa_demixing, mapping_matrix_toa_demixing_data[:]
	case 5:
		return &mapping_matrix_fourthoa_mixing, mapping_matrix_fourthoa_mixing_data[:],
			&mapping_matrix_fourthoa_demixing, mapping_matrix_fourthoa_demixing_data[:]
	case 6:
		return &mapping_matrix_fifthoa_mixing, mapping_matrix_fifthoa_mixing_data[:],
			&mapping_matrix_fifthoa_demixing, mapping_matrix_fifthoa_demixing_data[:]
	}
	return nil, nil, nil, nil
}

// opus_projection_ambisonics_encoder_get_size — C: opus_projection_encoder.c:156.
func opus_projection_ambisonics_encoder_get_size(channels, mapping_family int) opus_int32 {
	var nb_streams, nb_coupled_streams, order_plus_one int

	if get_streams_from_channels(channels, mapping_family,
		&nb_streams, &nb_coupled_streams, &order_plus_one) != OPUS_OK {
		return 0
	}

	mixing, _, demixing, _ := projection_tables_for(order_plus_one)
	if mixing == nil {
		return 0
	}

	mixing_matrix_size := mapping_matrix_get_size(mixing.rows, mixing.cols)
	if mixing_matrix_size == 0 {
		return 0
	}
	demixing_matrix_size := mapping_matrix_get_size(demixing.rows, demixing.cols)
	if demixing_matrix_size == 0 {
		return 0
	}
	encoder_size := opus_multistream_encoder_get_size(nb_streams, nb_coupled_streams)
	if encoder_size == 0 {
		return 0
	}
	return opus_int32(mm_align(opusProjectionEncoderSizeofStruct())) +
		mixing_matrix_size + demixing_matrix_size + encoder_size
}

// opusProjectionEncoderSizeofStruct — placeholder for
// `sizeof(OpusProjectionEncoder)` in C (two opus_int32 → 8 bytes).
func opusProjectionEncoderSizeofStruct() int { return 8 }

// opus_projection_ambisonics_encoder_init — C: opus_projection_encoder.c:230.
func opus_projection_ambisonics_encoder_init(st *OpusProjectionEncoder, Fs opus_int32,
	channels, mapping_family int, streams, coupled_streams *int, application int) int {
	var order_plus_one int
	var mapping [255]byte

	if streams == nil || coupled_streams == nil {
		return OPUS_BAD_ARG
	}

	if get_streams_from_channels(channels, mapping_family, streams,
		coupled_streams, &order_plus_one) != OPUS_OK {
		return OPUS_BAD_ARG
	}

	if mapping_family == 3 {
		mixing, mixing_data, demixing, demixing_data := projection_tables_for(order_plus_one)
		if mixing == nil {
			return OPUS_BAD_ARG
		}
		mapping_matrix_init(&st.mixing_matrix, mixing.rows, mixing.cols, mixing.gain,
			mixing_data, opus_int32(len(mixing_data)*2))

		st.mixing_matrix_size_in_bytes = mapping_matrix_get_size(
			st.mixing_matrix.rows, st.mixing_matrix.cols)
		if st.mixing_matrix_size_in_bytes == 0 {
			return OPUS_BAD_ARG
		}

		mapping_matrix_init(&st.demixing_matrix, demixing.rows, demixing.cols, demixing.gain,
			demixing_data, opus_int32(len(demixing_data)*2))

		st.demixing_matrix_size_in_bytes = mapping_matrix_get_size(
			st.demixing_matrix.rows, st.demixing_matrix.cols)
		if st.demixing_matrix_size_in_bytes == 0 {
			return OPUS_BAD_ARG
		}
	} else {
		return OPUS_UNIMPLEMENTED
	}

	// Ensure matrices are large enough for desired coding scheme.
	if *streams+*coupled_streams > st.mixing_matrix.rows ||
		channels > st.mixing_matrix.cols ||
		channels > st.demixing_matrix.rows ||
		*streams+*coupled_streams > st.demixing_matrix.cols {
		return OPUS_BAD_ARG
	}

	// Set trivial mapping so each input channel pairs with a matrix column.
	for i := 0; i < channels; i++ {
		mapping[i] = byte(i)
	}

	// Initialize multistream encoder with provided settings.
	st.ms_encoder = &OpusMSEncoder{}
	return opus_multistream_encoder_init(st.ms_encoder, Fs, channels, *streams,
		*coupled_streams, mapping[:], application)
}

// opus_projection_ambisonics_encoder_create — C: opus_projection_encoder.c:364.
func opus_projection_ambisonics_encoder_create(
	Fs opus_int32, channels, mapping_family int,
	streams, coupled_streams *int, application int, error_ *int) *OpusProjectionEncoder {
	size := opus_projection_ambisonics_encoder_get_size(channels, mapping_family)
	if size == 0 {
		if error_ != nil {
			*error_ = OPUS_ALLOC_FAIL
		}
		return nil
	}
	st := &OpusProjectionEncoder{}
	ret := opus_projection_ambisonics_encoder_init(st, Fs, channels, mapping_family,
		streams, coupled_streams, application)
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

// opus_projection_encode — C: opus_projection_encoder.c:400.
func opus_projection_encode(st *OpusProjectionEncoder, pcm []opus_int16,
	frame_size int, data []byte, max_data_bytes opus_int32) int {
	return opus_multistream_encode_native(st.ms_encoder,
		opus_projection_copy_channel_in_short, pcm, frame_size, data,
		max_data_bytes, 16, downmix_int, 0, &st.mixing_matrix)
}

// opus_projection_encode24 — C: opus_projection_encoder.c:409.
func opus_projection_encode24(st *OpusProjectionEncoder, pcm []opus_int32,
	frame_size int, data []byte, max_data_bytes opus_int32) int {
	return opus_multistream_encode_native(st.ms_encoder,
		opus_projection_copy_channel_in_int24, pcm, frame_size, data,
		max_data_bytes, MAX_ENCODING_DEPTH, downmix_int24, 0, &st.mixing_matrix)
}

// opus_projection_encode_float — C: opus_projection_encoder.c:419.
func opus_projection_encode_float(st *OpusProjectionEncoder, pcm []float32,
	frame_size int, data []byte, max_data_bytes opus_int32) int {
	return opus_multistream_encode_native(st.ms_encoder,
		opus_projection_copy_channel_in_float, pcm, frame_size, data,
		max_data_bytes, MAX_ENCODING_DEPTH, downmix_float, 1, &st.mixing_matrix)
}

// opus_projection_encoder_destroy — C: opus_projection_encoder.c:429.
// No-op in Go; kept for API parity.
func opus_projection_encoder_destroy(st *OpusProjectionEncoder) { _ = st }

// opus_projection_encoder_ctl — C: opus_projection_encoder.c:434.
//
// Go's variadic args substitute for va_list. The standard multistream
// ctls fall through to opus_multistream_encoder_ctl_va_list.
func opus_projection_encoder_ctl(st *OpusProjectionEncoder, request int, args ...interface{}) int {
	ms_encoder := st.ms_encoder
	demixing_matrix := &st.demixing_matrix

	switch request {
	case OPUS_PROJECTION_GET_DEMIXING_MATRIX_SIZE_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_int32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		// C: nb_channels * (nb_streams + nb_coupled_streams) * sizeof(opus_int16)
		*value = opus_int32(ms_encoder.layout.nb_channels) *
			opus_int32(ms_encoder.layout.nb_streams+ms_encoder.layout.nb_coupled_streams) * 2
		return OPUS_OK

	case OPUS_PROJECTION_GET_DEMIXING_MATRIX_GAIN_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_int32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(demixing_matrix.gain)
		return OPUS_OK

	case OPUS_PROJECTION_GET_DEMIXING_MATRIX_REQUEST:
		if len(args) < 2 {
			return OPUS_BAD_ARG
		}
		external_char, ok := args[0].([]byte)
		if !ok || external_char == nil {
			return OPUS_BAD_ARG
		}
		external_size, ok2 := args[1].(opus_int32)
		if !ok2 {
			// Tolerate int32 for convenience.
			if v, ok3 := args[1].(int32); ok3 {
				external_size = opus_int32(v)
			} else if v, ok4 := args[1].(int); ok4 {
				external_size = opus_int32(v)
			} else {
				return OPUS_BAD_ARG
			}
		}

		nb_input_streams := ms_encoder.layout.nb_streams + ms_encoder.layout.nb_coupled_streams
		nb_output_streams := ms_encoder.layout.nb_channels

		internal_short := mapping_matrix_get_data(demixing_matrix)
		internal_size := opus_int32(nb_input_streams) * opus_int32(nb_output_streams) * 2
		if external_size != internal_size {
			return OPUS_BAD_ARG
		}

		// Copy demixing matrix subset to output destination.
		l := 0
		for i := 0; i < nb_input_streams; i++ {
			for j := 0; j < nb_output_streams; j++ {
				k := demixing_matrix.rows*i + j
				external_char[2*l] = byte(internal_short[k])
				external_char[2*l+1] = byte(opus_uint16(internal_short[k]) >> 8)
				l++
			}
		}
		return OPUS_OK

	default:
		return opus_multistream_encoder_ctl_va_list(ms_encoder, request, args...)
	}
}
