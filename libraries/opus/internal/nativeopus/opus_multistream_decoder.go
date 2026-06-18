package nativeopus

// Port of libopus/src/opus_multistream_decoder.c. Float path only.
//
// The multistream decoder wraps N mono/coupled OpusDecoder instances.
// C stores them in a flat byte arena attached to the OpusMSDecoder
// struct (ChannelLayout + N serialized OpusDecoder blobs sized via
// opus_decoder_get_size). The Go port replaces that arena with a
// slice of heap-allocated *OpusDecoder values since the bit-exact
// parity properties of this layer do not depend on memory layout.
//
// Skipped (matches config.h): ENABLE_HARDENING / ENABLE_ASSERTIONS.

// OPUS_MULTISTREAM_GET_DECODER_STATE_REQUEST — C: opus_multistream.h:56.
const OPUS_MULTISTREAM_GET_DECODER_STATE_REQUEST = 5122

// OpusMSDecoder — C: opus_private.h:113.
// In C the struct contains only `layout`; the per-stream decoders
// follow in the arena. The Go port stores them in a separate slice.
type OpusMSDecoder struct {
	layout ChannelLayout
	// decoders[s] is a 2-channel OpusDecoder for s < nb_coupled_streams,
	// and a 1-channel OpusDecoder otherwise.
	decoders []*OpusDecoder
}

// opus_copy_channel_out_func — C: opus_private.h:138.
//
// The C typedef takes a `void *dst` plus opaque `user_data`. In Go we
// dispatch on a concrete destination slice type via a closure built
// at the call site. `src==nil` means "muted channel".
type opus_copy_channel_out_func func(
	dst interface{},
	dst_stride int,
	dst_channel int,
	src []opus_res,
	src_stride int,
	frame_size int,
	user_data interface{},
)

// opus_multistream_decoder_get_size — C: opus_multistream_decoder.c:53.
//
// Returns the C arena size for API parity. Callers in Go do not need
// this value for allocation; it remains for tests and ctl parity.
func opus_multistream_decoder_get_size(nb_streams, nb_coupled_streams int) opus_int32 {
	var coupled_size int
	var mono_size int

	if nb_streams < 1 || nb_coupled_streams > nb_streams || nb_coupled_streams < 0 {
		return 0
	}
	coupled_size = opus_decoder_get_size(2)
	mono_size = opus_decoder_get_size(1)
	return opus_int32(alignInt(opusMSDecoderSizeOfStruct()) +
		nb_coupled_streams*alignInt(coupled_size) +
		(nb_streams-nb_coupled_streams)*alignInt(mono_size))
}

// opusMSDecoderSizeOfStruct — placeholder for `sizeof(OpusMSDecoder)`
// mirroring opusDecoderSizeOfStruct. The Go port does not share the
// C arena layout so the exact value does not matter for parity.
func opusMSDecoderSizeOfStruct() int { return 1 }

// opus_multistream_decoder_init — C: opus_multistream_decoder.c:66.
func opus_multistream_decoder_init(
	st *OpusMSDecoder,
	Fs opus_int32,
	channels int,
	streams int,
	coupled_streams int,
	mapping []byte,
) int {
	var i, ret int

	if (channels > 255) || (channels < 1) || (coupled_streams > streams) ||
		(streams < 1) || (coupled_streams < 0) || (streams > 255-coupled_streams) {
		return OPUS_BAD_ARG
	}

	st.layout.nb_channels = channels
	st.layout.nb_streams = streams
	st.layout.nb_coupled_streams = coupled_streams

	for i = 0; i < st.layout.nb_channels; i++ {
		st.layout.mapping[i] = mapping[i]
	}
	if validate_layout(&st.layout) == 0 {
		return OPUS_BAD_ARG
	}

	st.decoders = make([]*OpusDecoder, streams)

	for i = 0; i < st.layout.nb_coupled_streams; i++ {
		dec := &OpusDecoder{}
		ret = opus_decoder_init(dec, Fs, 2)
		if ret != OPUS_OK {
			return ret
		}
		st.decoders[i] = dec
	}
	for ; i < st.layout.nb_streams; i++ {
		dec := &OpusDecoder{}
		ret = opus_decoder_init(dec, Fs, 1)
		if ret != OPUS_OK {
			return ret
		}
		st.decoders[i] = dec
	}
	return OPUS_OK
}

// opus_multistream_decoder_create — C: opus_multistream_decoder.c:113.
func opus_multistream_decoder_create(
	Fs opus_int32,
	channels int,
	streams int,
	coupled_streams int,
	mapping []byte,
	error_ *int,
) *OpusMSDecoder {
	var ret int
	if (channels > 255) || (channels < 1) || (coupled_streams > streams) ||
		(streams < 1) || (coupled_streams < 0) || (streams > 255-coupled_streams) {
		if error_ != nil {
			*error_ = OPUS_BAD_ARG
		}
		return nil
	}
	st := &OpusMSDecoder{}
	ret = opus_multistream_decoder_init(st, Fs, channels, streams, coupled_streams, mapping)
	if error_ != nil {
		*error_ = ret
	}
	if ret != OPUS_OK {
		return nil
	}
	return st
}

// opus_multistream_packet_validate — C: opus_multistream_decoder.c:149.
func opus_multistream_packet_validate(data []byte, length opus_int32,
	nb_streams int, Fs opus_int32) int {
	var s int
	var count int
	var toc byte
	var size [48]opus_int16
	samples := 0
	var packet_offset opus_int32

	for s = 0; s < nb_streams; s++ {
		var tmp_samples int
		if length <= 0 {
			return OPUS_INVALID_PACKET
		}
		var selfDelim int
		if s != nb_streams-1 {
			selfDelim = 1
		}
		count = opus_packet_parse_impl(data, length, selfDelim, &toc, nil,
			size[:], nil, &packet_offset, nil, nil)
		if count < 0 {
			return count
		}
		tmp_samples = opus_packet_get_nb_samples(data, packet_offset, Fs)
		if s != 0 && samples != tmp_samples {
			return OPUS_INVALID_PACKET
		}
		samples = tmp_samples
		data = data[packet_offset:]
		length -= packet_offset
	}
	return samples
}

// opus_multistream_decode_native — C: opus_multistream_decoder.c:178.
func opus_multistream_decode_native(
	st *OpusMSDecoder,
	data []byte,
	length opus_int32,
	pcm interface{},
	copy_channel_out opus_copy_channel_out_func,
	frame_size int,
	decode_fec int,
	soft_clip int,
	user_data interface{},
) int {
	var Fs opus_int32
	var s, c int
	do_plc := 0

	if frame_size <= 0 {
		return OPUS_BAD_ARG
	}
	// Limit frame_size to avoid excessive stack allocations.
	if r := opus_multistream_decoder_ctl(st, OPUS_GET_SAMPLE_RATE_REQUEST, &Fs); r != OPUS_OK {
		return r
	}
	// IMIN(frame_size, Fs/25*3) — matches C left-to-right evaluation.
	limit := int(Fs) / 25 * 3
	if limit < frame_size {
		frame_size = limit
	}
	buf := make([]opus_res, 2*frame_size)

	if length == 0 {
		do_plc = 1
	}
	if length < 0 {
		return OPUS_BAD_ARG
	}
	if do_plc == 0 && length < opus_int32(2*st.layout.nb_streams-1) {
		return OPUS_INVALID_PACKET
	}
	if do_plc == 0 {
		ret := opus_multistream_packet_validate(data, length, st.layout.nb_streams, Fs)
		if ret < 0 {
			return ret
		} else if ret > frame_size {
			return OPUS_BUFFER_TOO_SMALL
		}
	}
	for s = 0; s < st.layout.nb_streams; s++ {
		var dec *OpusDecoder
		var packet_offset opus_int32
		var ret int

		dec = st.decoders[s]

		if do_plc == 0 && length <= 0 {
			return OPUS_INTERNAL_ERROR
		}
		packet_offset = 0
		var selfDelim int
		if s != st.layout.nb_streams-1 {
			selfDelim = 1
		}
		// In C: opus_decode_native(dec, data, len, buf, frame_size,
		//   decode_fec, self_delimited, &packet_offset, soft_clip,
		//   NULL /*DRED*/, 0);
		// The Go port's opus_decode_native signature omits the DRED
		// args but otherwise matches.
		var dataArg []byte
		var lenArg opus_int32
		if do_plc != 0 {
			dataArg = nil
			lenArg = 0
		} else {
			dataArg = data
			lenArg = length
		}
		ret = opus_decode_native(dec, dataArg, lenArg, buf, frame_size,
			decode_fec, selfDelim, &packet_offset, soft_clip)
		if do_plc == 0 {
			data = data[packet_offset:]
			length -= packet_offset
		}
		if ret <= 0 {
			return ret
		}
		frame_size = ret
		if s < st.layout.nb_coupled_streams {
			var chan_, prev int
			prev = -1
			// Copy "left" audio to the channel(s) where it belongs
			for {
				chan_ = get_left_channel(&st.layout, s, prev)
				if chan_ == -1 {
					break
				}
				copy_channel_out(pcm, st.layout.nb_channels, chan_,
					buf, 2, frame_size, user_data)
				prev = chan_
			}
			prev = -1
			// Copy "right" audio to the channel(s) where it belongs
			for {
				chan_ = get_right_channel(&st.layout, s, prev)
				if chan_ == -1 {
					break
				}
				copy_channel_out(pcm, st.layout.nb_channels, chan_,
					buf[1:], 2, frame_size, user_data)
				prev = chan_
			}
		} else {
			var chan_, prev int
			prev = -1
			// Copy audio to the channel(s) where it belongs
			for {
				chan_ = get_mono_channel(&st.layout, s, prev)
				if chan_ == -1 {
					break
				}
				copy_channel_out(pcm, st.layout.nb_channels, chan_,
					buf, 1, frame_size, user_data)
				prev = chan_
			}
		}
	}
	// Handle muted channels
	for c = 0; c < st.layout.nb_channels; c++ {
		if st.layout.mapping[c] == 255 {
			copy_channel_out(pcm, st.layout.nb_channels, c,
				nil, 0, frame_size, user_data)
		}
	}
	return frame_size
}

// opus_copy_channel_out_float — C: opus_multistream_decoder.c:310.
func opus_copy_channel_out_float(
	dst interface{},
	dst_stride int,
	dst_channel int,
	src []opus_res,
	src_stride int,
	frame_size int,
	user_data interface{},
) {
	_ = user_data
	float_dst := dst.([]float32)
	if src != nil {
		for i := 0; i < frame_size; i++ {
			float_dst[i*dst_stride+dst_channel] = RES2FLOAT(src[i*src_stride])
		}
	} else {
		for i := 0; i < frame_size; i++ {
			float_dst[i*dst_stride+dst_channel] = 0
		}
	}
}

// opus_copy_channel_out_short — C: opus_multistream_decoder.c:337.
func opus_copy_channel_out_short(
	dst interface{},
	dst_stride int,
	dst_channel int,
	src []opus_res,
	src_stride int,
	frame_size int,
	user_data interface{},
) {
	_ = user_data
	short_dst := dst.([]opus_int16)
	if src != nil {
		for i := 0; i < frame_size; i++ {
			// RES2INT16 in float build == FLOAT2INT16.
			short_dst[i*dst_stride+dst_channel] = FLOAT2INT16(float32(src[i*src_stride]))
		}
	} else {
		for i := 0; i < frame_size; i++ {
			short_dst[i*dst_stride+dst_channel] = 0
		}
	}
}

// opus_copy_channel_out_int24 — C: opus_multistream_decoder.c:363.
func opus_copy_channel_out_int24(
	dst interface{},
	dst_stride int,
	dst_channel int,
	src []opus_res,
	src_stride int,
	frame_size int,
	user_data interface{},
) {
	_ = user_data
	int24_dst := dst.([]opus_int32)
	if src != nil {
		for i := 0; i < frame_size; i++ {
			// RES2INT24 in float build == FLOAT2INT24.
			int24_dst[i*dst_stride+dst_channel] = FLOAT2INT24(float32(src[i*src_stride]))
		}
	} else {
		for i := 0; i < frame_size; i++ {
			int24_dst[i*dst_stride+dst_channel] = 0
		}
	}
}

// OPTIONAL_CLIP_MS — C: opus_multistream_decoder.c:389 (non-FIXED_POINT: 1).
const OPTIONAL_CLIP_MS = 1

// opus_multistream_decode — C: opus_multistream_decoder.c:395. int16 output.
func opus_multistream_decode(
	st *OpusMSDecoder,
	data []byte,
	length opus_int32,
	pcm []opus_int16,
	frame_size int,
	decode_fec int,
) int {
	return opus_multistream_decode_native(st, data, length,
		pcm, opus_copy_channel_out_short, frame_size, decode_fec, OPTIONAL_CLIP_MS, nil)
}

// opus_multistream_decode24 — C: opus_multistream_decoder.c:408. int24 output.
func opus_multistream_decode24(
	st *OpusMSDecoder,
	data []byte,
	length opus_int32,
	pcm []opus_int32,
	frame_size int,
	decode_fec int,
) int {
	return opus_multistream_decode_native(st, data, length,
		pcm, opus_copy_channel_out_int24, frame_size, decode_fec, 0, nil)
}

// opus_multistream_decode_float — C: opus_multistream_decoder.c:422. float output.
func opus_multistream_decode_float(
	st *OpusMSDecoder,
	data []byte,
	length opus_int32,
	pcm []float32,
	frame_size int,
	decode_fec int,
) int {
	return opus_multistream_decode_native(st, data, length,
		pcm, opus_copy_channel_out_float, frame_size, decode_fec, 0, nil)
}

// opus_multistream_decoder_ctl — C: opus_multistream_decoder.c:552.
//
// The C API uses va_list. The Go variant collects remaining args
// and delegates to the switch below.
func opus_multistream_decoder_ctl(st *OpusMSDecoder, request int, args ...interface{}) int {
	ret := OPUS_OK

	switch request {
	case OPUS_GET_BANDWIDTH_REQUEST,
		OPUS_GET_SAMPLE_RATE_REQUEST,
		OPUS_GET_GAIN_REQUEST,
		OPUS_GET_LAST_PACKET_DURATION_REQUEST,
		OPUS_GET_PHASE_INVERSION_DISABLED_REQUEST,
		OPUS_GET_COMPLEXITY_REQUEST:
		// For int32* GET params, just query the first stream.
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_int32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		ret = opus_decoder_ctl(st.decoders[0], request, value)
	case OPUS_GET_FINAL_RANGE_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_uint32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		var tmp opus_uint32
		*value = 0
		for s := 0; s < st.layout.nb_streams; s++ {
			ret = opus_decoder_ctl(st.decoders[s], request, &tmp)
			if ret != OPUS_OK {
				break
			}
			*value ^= tmp
		}
	case OPUS_RESET_STATE:
		for s := 0; s < st.layout.nb_streams; s++ {
			ret = opus_decoder_ctl(st.decoders[s], OPUS_RESET_STATE)
			if ret != OPUS_OK {
				break
			}
		}
	case OPUS_MULTISTREAM_GET_DECODER_STATE_REQUEST:
		if len(args) < 2 {
			return OPUS_BAD_ARG
		}
		stream_id, ok := args[0].(opus_int32)
		if !ok {
			return OPUS_BAD_ARG
		}
		if stream_id < 0 || stream_id >= opus_int32(st.layout.nb_streams) {
			return OPUS_BAD_ARG
		}
		value, ok := args[1].(**OpusDecoder)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = st.decoders[stream_id]
	case OPUS_SET_GAIN_REQUEST,
		OPUS_SET_COMPLEXITY_REQUEST,
		OPUS_SET_PHASE_INVERSION_DISABLED_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(opus_int32)
		if !ok {
			return OPUS_BAD_ARG
		}
		for s := 0; s < st.layout.nb_streams; s++ {
			ret = opus_decoder_ctl(st.decoders[s], request, value)
			if ret != OPUS_OK {
				break
			}
		}
	default:
		ret = OPUS_UNIMPLEMENTED
	}
	return ret
}

// opus_multistream_decoder_destroy — C: opus_multistream_decoder.c:562.
// Go relies on GC; kept for API parity.
func opus_multistream_decoder_destroy(st *OpusMSDecoder) {
	_ = st
}
