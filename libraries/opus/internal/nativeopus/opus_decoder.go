package nativeopus

// Port of libopus/src/opus_decoder.c. Float path only.
//
// Skipped (matches our config.h):
//   - ENABLE_DEEP_PLC: LPCNet-backed PLC and lpcnet_* hooks.
//   - ENABLE_OSCE / ENABLE_OSCE_BWE: perceptual enhancement DNN paths.
//   - ENABLE_DRED: redundancy-based deep-redundancy paths.
//   - ENABLE_QEXT: extended-quality CELT hybrid layer.
//   - ENABLE_RES24 / FIXED_POINT: we build the float path with 16-bit
//     nominal PCM range, so opus_res == float32.
//   - OPUS_PRINT_INT / OPUS_CHECK_ARRAY instrumentation: debug-only.
//   - validate_opus_decoder: hardening asserts.
//
// The TOC-inspection helpers (opus_packet_get_bandwidth / _nb_channels /
// _nb_frames / _nb_samples) were ported earlier in opus.go (Wave 9a).
// This file intentionally does not re-declare them.

// MODE_* — C: opus_private.h:148-150.
const (
	MODE_SILK_ONLY = 1000
	MODE_HYBRID    = 1001
	MODE_CELT_ONLY = 1002
)

// OPUS_*_REQUEST — ctl request IDs, C: opus_defines.h.
const (
	OPUS_GET_BANDWIDTH_REQUEST                = 4009
	OPUS_SET_COMPLEXITY_REQUEST               = 4010
	OPUS_GET_COMPLEXITY_REQUEST               = 4011
	OPUS_RESET_STATE                          = 4028
	OPUS_GET_SAMPLE_RATE_REQUEST              = 4029
	OPUS_GET_FINAL_RANGE_REQUEST              = 4031
	OPUS_GET_PITCH_REQUEST                    = 4033
	OPUS_SET_GAIN_REQUEST                     = 4034
	OPUS_GET_GAIN_REQUEST                     = 4045
	OPUS_GET_LAST_PACKET_DURATION_REQUEST     = 4039
	OPUS_SET_PHASE_INVERSION_DISABLED_REQUEST = 4046
	OPUS_GET_PHASE_INVERSION_DISABLED_REQUEST = 4047
	OPUS_SET_IGNORE_EXTENSIONS_REQUEST        = 4058
	OPUS_GET_IGNORE_EXTENSIONS_REQUEST        = 4059
)

// OpusDecoder — Go port of the C struct. C: opus_decoder.c:65-94.
// The C layout interleaves the SILK and CELT sub-decoders inline via
// offsets; Go uses separate fields since allocation is heap-managed
// and the offsets are irrelevant to bit-exact parity.
type OpusDecoder struct {
	// Sub-decoders. In C these live in a flat arena reached via
	// `silk_dec_offset` and `celt_dec_offset`. The Go port keeps them
	// as plain pointers; the offset fields are retained for API parity
	// but left as zero.
	silk_dec        *silk_decoder
	celt_dec        *OpusCustomDecoder
	celt_dec_offset int
	silk_dec_offset int

	channels          int
	Fs                opus_int32
	DecControl        silk_DecControlStruct
	decode_gain       int
	complexity        int
	ignore_extensions int
	arch              int

	// Fields below are cleared on OPUS_RESET_STATE.
	// C: opus_decoder.c:80 (OPUS_DECODER_RESET_START = stream_channels).
	stream_channels int

	bandwidth            int
	mode                 int
	prev_mode            int
	frame_size           int
	prev_redundancy      int
	last_packet_duration int
	softclip_mem         [2]opus_val16

	rangeFinal opus_uint32
}

// opus_decoder_get_size — C: opus_decoder.c:121. Returns a byte count
// matching the C arena layout for API compatibility. Go allocates a
// native struct regardless.
func opus_decoder_get_size(channels int) int {
	var silkDecSizeBytes opus_int
	if channels < 1 || channels > 2 {
		return 0
	}
	ret := silk_Get_Decoder_Size(&silkDecSizeBytes)
	if ret != 0 {
		return 0
	}
	silkDecSizeBytes = alignOpusInt(silkDecSizeBytes)
	celtDecSizeBytes := celt_decoder_get_size(channels)
	return alignInt(opusDecoderSizeOfStruct()) + int(silkDecSizeBytes) + celtDecSizeBytes
}

// alignInt / alignOpusInt mirror the C `align(x) = (x+3)&~3` macro. In
// our port only opus_decoder_get_size consults these values.
func alignInt(x int) int               { return (x + 3) &^ 3 }
func alignOpusInt(x opus_int) opus_int { return (x + 3) &^ 3 }

// opusDecoderSizeOfStruct — placeholder for `sizeof(OpusDecoder)` in C.
// Returns 1 since the Go port does not share the C arena layout.
func opusDecoderSizeOfStruct() int { return 1 }

// opus_decoder_init — C: opus_decoder.c:135.
func opus_decoder_init(st *OpusDecoder, Fs opus_int32, channels int) int {
	if (Fs != 48000 && Fs != 24000 && Fs != 16000 && Fs != 12000 && Fs != 8000) ||
		(channels != 1 && channels != 2) {
		return OPUS_BAD_ARG
	}

	// OPUS_CLEAR((char*)st, opus_decoder_get_size(channels));
	*st = OpusDecoder{}

	var silkDecSizeBytes opus_int
	ret := silk_Get_Decoder_Size(&silkDecSizeBytes)
	if ret != 0 {
		return OPUS_INTERNAL_ERROR
	}
	silkDecSizeBytes = alignOpusInt(silkDecSizeBytes)
	st.silk_dec_offset = alignInt(opusDecoderSizeOfStruct())
	st.celt_dec_offset = st.silk_dec_offset + int(silkDecSizeBytes)
	st.silk_dec = &silk_decoder{}
	st.celt_dec = &OpusCustomDecoder{}
	st.stream_channels = channels
	st.channels = channels
	st.complexity = 0

	st.Fs = Fs
	st.DecControl.API_sampleRate = st.Fs
	st.DecControl.nChannelsAPI = opus_int32(st.channels)

	// Reset SILK decoder.
	if silk_InitDecoder(st.silk_dec) != 0 {
		return OPUS_INTERNAL_ERROR
	}

	// Initialize CELT decoder. C passes a NULL mode and lets the stock
	// celt_decoder_init() resolve the static mode via
	// opus_custom_mode_create(). We install the ported static mode
	// descriptor (static_modes_float.go) here so celt_decoder_init sees
	// a non-nil mode and runs the normal init path.
	if st.celt_dec.mode == nil {
		st.celt_dec.mode = StaticMode48000_960_120()
	}
	if rc := celt_decoder_init(st.celt_dec, st.celt_dec.mode, Fs, channels); rc != OPUS_OK {
		return OPUS_INTERNAL_ERROR
	}

	// celt_decoder_ctl(celt_dec, CELT_SET_SIGNALLING(0));
	st.celt_dec.signalling = 0

	st.prev_mode = 0
	st.frame_size = int(Fs) / 400
	st.arch = 0
	return OPUS_OK
}

// opus_decoder_create — C: opus_decoder.c:186.
func opus_decoder_create(Fs opus_int32, channels int, error_ *int) *OpusDecoder {
	if (Fs != 48000 && Fs != 24000 && Fs != 16000 && Fs != 12000 && Fs != 8000) ||
		(channels != 1 && channels != 2) {
		if error_ != nil {
			*error_ = OPUS_BAD_ARG
		}
		return nil
	}
	st := &OpusDecoder{}
	ret := opus_decoder_init(st, Fs, channels)
	if error_ != nil {
		*error_ = ret
	}
	if ret != OPUS_OK {
		return nil
	}
	return st
}

// smooth_fade — fade-out/in of two frames through the CELT window.
// C: opus_decoder.c:237 (float path, non-RES24).
func smooth_fade(in1, in2, out []opus_res, overlap, channels int,
	window []celt_coef, Fs opus_int32) {
	inc := int(48000 / Fs)
	for c := 0; c < channels; c++ {
		for i := 0; i < overlap; i++ {
			w := COEF2VAL16(window[i*inc])
			w = opus_val16(MULT16_16_Q15(w, w))
			// SHR32(MAC16_16(MULT16_16(w,in2), Q15ONE-w, in1), 15)
			out[i*channels+c] = opus_res(SHR32(MAC16_16(MULT16_16(w, in2[i*channels+c]),
				Q15ONE-w, in1[i*channels+c]), 15))
		}
	}
}

// opus_packet_get_mode — C: opus_decoder.c:256.
func opus_packet_get_mode(data []byte) int {
	if data[0]&0x80 != 0 {
		return MODE_CELT_ONLY
	}
	if (data[0] & 0x60) == 0x60 {
		return MODE_HYBRID
	}
	return MODE_SILK_ONLY
}

// opus_decode_frame — C: opus_decoder.c:271.
func opus_decode_frame(st *OpusDecoder, data []byte, length opus_int32,
	pcm []opus_res, frame_size int, decode_fec int) int {
	silk_dec := st.silk_dec
	celt_dec := st.celt_dec
	var silk_ret, celt_ret int
	var dec ec_dec
	var silk_frame_size opus_int32
	var pcm_transition []opus_res
	var redundant_audio []opus_res

	var audiosize, mode, bandwidth int
	var transition int = 0
	var start_band int
	var redundancy int = 0
	var redundancy_bytes int = 0
	var celt_to_silk int = 0
	var window []celt_coef
	var redundant_rng opus_uint32 = 0
	var celt_accum int

	F20 := int(st.Fs) / 50
	F10 := F20 >> 1
	F5 := F10 >> 1
	F2_5 := F5 >> 1
	if frame_size < F2_5 {
		return OPUS_BUFFER_TOO_SMALL
	}
	// Limit frame_size to avoid excessive stack allocations.
	if fs := int(st.Fs) / 25 * 3; frame_size > fs {
		frame_size = fs
	}
	// Payloads of 1 (2 including ToC) or 0 trigger the PLC/DTX
	if length <= 1 {
		data = nil
		if frame_size > st.frame_size {
			frame_size = st.frame_size
		}
	}
	if data != nil {
		audiosize = st.frame_size
		mode = st.mode
		bandwidth = st.bandwidth
		ec_dec_init(&dec, data, opus_uint32(length))
	} else {
		audiosize = frame_size
		if st.prev_redundancy != 0 {
			mode = MODE_CELT_ONLY
		} else {
			mode = st.prev_mode
		}
		bandwidth = 0

		if mode == 0 {
			for i := 0; i < audiosize*st.channels; i++ {
				pcm[i] = 0
			}
			return audiosize
		}

		// Avoids trying to run the PLC on sizes other than 2.5, 5,
		// 10, or 20 ms.
		if audiosize > F20 {
			for {
				n := F20
				if audiosize < n {
					n = audiosize
				}
				ret := opus_decode_frame(st, nil, 0, pcm, n, 0)
				if ret < 0 {
					return ret
				}
				pcm = pcm[ret*st.channels:]
				audiosize -= ret
				if audiosize <= 0 {
					break
				}
			}
			return frame_size
		} else if audiosize < F20 {
			if audiosize > F10 {
				audiosize = F10
			} else if mode != MODE_SILK_ONLY && audiosize > F5 && audiosize < F10 {
				audiosize = F5
			}
		}
	}

	// In fixed-point, we can tell CELT to do the accumulation on top of the
	// SILK PCM buffer. This saves some stack space.
	if mode != MODE_CELT_ONLY {
		celt_accum = 1
	} else {
		celt_accum = 0
	}

	pcm_transition_silk_size := 0
	pcm_transition_celt_size := 0
	if data != nil && st.prev_mode > 0 &&
		((mode == MODE_CELT_ONLY && st.prev_mode != MODE_CELT_ONLY && st.prev_redundancy == 0) ||
			(mode != MODE_CELT_ONLY && st.prev_mode == MODE_CELT_ONLY)) {
		transition = 1
		if mode == MODE_CELT_ONLY {
			pcm_transition_celt_size = F5 * st.channels
		} else {
			pcm_transition_silk_size = F5 * st.channels
		}
	}
	pcm_transition_celt := make([]opus_res, pcm_transition_celt_size)
	if transition != 0 && mode == MODE_CELT_ONLY {
		pcm_transition = pcm_transition_celt
		n := F5
		if audiosize < n {
			n = audiosize
		}
		opus_decode_frame(st, nil, 0, pcm_transition, n, 0)
	}
	if audiosize > frame_size {
		return OPUS_BAD_ARG
	} else {
		frame_size = audiosize
	}

	// SILK processing.
	if mode != MODE_CELT_ONLY {
		var lost_flag, decoded_samples int
		var pcm_ptr []opus_res
		pcm_silk_size := 0
		pcm_too_small := frame_size < F10
		if pcm_too_small {
			pcm_silk_size = F10 * st.channels
		}
		pcm_silk := make([]opus_res, pcm_silk_size)
		if pcm_too_small {
			pcm_ptr = pcm_silk
		} else {
			pcm_ptr = pcm
		}

		if st.prev_mode == MODE_CELT_ONLY {
			silk_ResetDecoder(silk_dec)
		}

		// The SILK PLC cannot produce frames of less than 10 ms.
		v := 1000 * audiosize / int(st.Fs)
		if v < 10 {
			v = 10
		}
		st.DecControl.payloadSize_ms = opus_int(v)

		if data != nil {
			st.DecControl.nChannelsInternal = opus_int32(st.stream_channels)
			if mode == MODE_SILK_ONLY {
				if bandwidth == OPUS_BANDWIDTH_NARROWBAND {
					st.DecControl.internalSampleRate = 8000
				} else if bandwidth == OPUS_BANDWIDTH_MEDIUMBAND {
					st.DecControl.internalSampleRate = 12000
				} else if bandwidth == OPUS_BANDWIDTH_WIDEBAND {
					st.DecControl.internalSampleRate = 16000
				} else {
					st.DecControl.internalSampleRate = 16000
					celt_assert(false)
				}
			} else {
				// Hybrid mode.
				st.DecControl.internalSampleRate = 16000
			}
		}
		if st.complexity >= 5 {
			st.DecControl.enable_deep_plc = 1
		} else {
			st.DecControl.enable_deep_plc = 0
		}

		if data == nil {
			lost_flag = 1
		} else if decode_fec != 0 {
			lost_flag = 2
		} else {
			lost_flag = 0
		}
		decoded_samples = 0
		for {
			first_frame := 0
			if decoded_samples == 0 {
				first_frame = 1
			}
			silk_ret = int(silk_Decode(silk_dec, &st.DecControl,
				opus_int(lost_flag), opus_int(first_frame), &dec, pcm_ptr, &silk_frame_size, st.arch))
			if silk_ret != 0 {
				if lost_flag != 0 {
					// PLC failure should not be fatal.
					silk_frame_size = opus_int32(frame_size)
					for i := 0; i < frame_size*st.channels; i++ {
						pcm_ptr[i] = 0
					}
				} else {
					return OPUS_INTERNAL_ERROR
				}
			}
			pcm_ptr = pcm_ptr[int(silk_frame_size)*st.channels:]
			decoded_samples += int(silk_frame_size)
			if decoded_samples >= frame_size {
				break
			}
		}
		if pcm_too_small {
			OPUS_COPY(pcm, pcm_silk, frame_size*st.channels)
		}
	}

	start_band = 0
	if decode_fec == 0 && mode != MODE_CELT_ONLY && data != nil {
		extra := 0
		if mode == MODE_HYBRID {
			extra = 20
		}
		if ec_tell(&dec)+17+extra <= 8*int(length) {
			// Check if we have a redundant 0-8 kHz band.
			if mode == MODE_HYBRID {
				redundancy = ec_dec_bit_logp(&dec, 12)
			} else {
				redundancy = 1
			}
			if redundancy != 0 {
				celt_to_silk = ec_dec_bit_logp(&dec, 1)
				if mode == MODE_HYBRID {
					redundancy_bytes = int(ec_dec_uint(&dec, 256)) + 2
				} else {
					redundancy_bytes = int(length) - ((ec_tell(&dec) + 7) >> 3)
				}
				length -= opus_int32(redundancy_bytes)
				// Sanity check.
				if int(length)*8 < ec_tell(&dec) {
					length = 0
					redundancy_bytes = 0
					redundancy = 0
				}
				// Shrink decoder because of raw bits.
				dec.storage -= opus_uint32(redundancy_bytes)
			}
		}
	}
	if mode != MODE_CELT_ONLY {
		start_band = 17
	}

	if redundancy != 0 {
		transition = 0
		pcm_transition_silk_size = 0
	}

	pcm_transition_silk := make([]opus_res, pcm_transition_silk_size)

	if transition != 0 && mode != MODE_CELT_ONLY {
		pcm_transition = pcm_transition_silk
		n := F5
		if audiosize < n {
			n = audiosize
		}
		opus_decode_frame(st, nil, 0, pcm_transition, n, 0)
	}

	if bandwidth != 0 {
		endband := 21
		switch bandwidth {
		case OPUS_BANDWIDTH_NARROWBAND:
			endband = 13
		case OPUS_BANDWIDTH_MEDIUMBAND, OPUS_BANDWIDTH_WIDEBAND:
			endband = 17
		case OPUS_BANDWIDTH_SUPERWIDEBAND:
			endband = 19
		case OPUS_BANDWIDTH_FULLBAND:
			endband = 21
		default:
			celt_assert(false)
		}
		// celt_decoder_ctl(celt_dec, CELT_SET_END_BAND(endband));
		celt_dec.end = endband
	}
	// celt_decoder_ctl(celt_dec, CELT_SET_CHANNELS(st->stream_channels));
	celt_dec.stream_channels = st.stream_channels

	// Only allocate memory for redundancy if/when needed.
	redundant_audio_size := 0
	if redundancy != 0 {
		redundant_audio_size = F5 * st.channels
	}
	redundant_audio = make([]opus_res, redundant_audio_size)

	// 5 ms redundant frame for CELT->SILK.
	if redundancy != 0 && celt_to_silk != 0 {
		celt_dec.start = 0
		celt_decode_with_ec(celt_dec, data[length:], redundancy_bytes,
			redundant_audio, F5, nil, 0)
		redundant_rng = celt_dec.rng
	}

	// MUST be after PLC.
	celt_dec.start = start_band

	if mode != MODE_SILK_ONLY {
		celt_frame_size := F20
		if frame_size < celt_frame_size {
			celt_frame_size = frame_size
		}
		// Make sure to discard any previous CELT state.
		if mode != st.prev_mode && st.prev_mode > 0 && st.prev_redundancy == 0 {
			celt_decoder_reset(celt_dec)
		}
		// Decode CELT.
		var celt_data []byte
		var celt_len int
		if decode_fec == 0 {
			celt_data = data
			celt_len = int(length)
		}
		celt_ret = celt_decode_with_ec(celt_dec, celt_data, celt_len,
			pcm, celt_frame_size, &dec, celt_accum)
		st.rangeFinal = celt_dec.rng
	} else {
		silence := [2]byte{0xFF, 0xFF}
		if celt_accum == 0 {
			for i := 0; i < frame_size*st.channels; i++ {
				pcm[i] = 0
			}
		}
		// For hybrid -> SILK transitions, let the CELT MDCT do a fade-out.
		if st.prev_mode == MODE_HYBRID && !(redundancy != 0 && celt_to_silk != 0 && st.prev_redundancy != 0) {
			celt_dec.start = 0
			celt_decode_with_ec(celt_dec, silence[:], 2, pcm, F2_5, nil, celt_accum)
		}
		st.rangeFinal = dec.rng
	}

	// CELT_GET_MODE: window = celt_mode->window.
	if celt_dec.mode != nil {
		window = celt_dec.mode.window
	}

	// 5 ms redundant frame for SILK->CELT.
	if redundancy != 0 && celt_to_silk == 0 {
		celt_decoder_reset(celt_dec)
		celt_dec.start = 0

		celt_decode_with_ec(celt_dec, data[length:], redundancy_bytes, redundant_audio, F5, nil, 0)
		redundant_rng = celt_dec.rng
		smooth_fade(pcm[st.channels*(frame_size-F2_5):], redundant_audio[st.channels*F2_5:],
			pcm[st.channels*(frame_size-F2_5):], F2_5, st.channels, window, st.Fs)
	}
	// 5 ms redundant frame for CELT->SILK; ignore if the previous frame did not
	// use CELT.
	if redundancy != 0 && celt_to_silk != 0 && (st.prev_mode != MODE_SILK_ONLY || st.prev_redundancy != 0) {
		for c := 0; c < st.channels; c++ {
			for i := 0; i < F2_5; i++ {
				pcm[st.channels*i+c] = redundant_audio[st.channels*i+c]
			}
		}
		smooth_fade(redundant_audio[st.channels*F2_5:], pcm[st.channels*F2_5:],
			pcm[st.channels*F2_5:], F2_5, st.channels, window, st.Fs)
	}
	if transition != 0 {
		if audiosize >= F5 {
			for i := 0; i < st.channels*F2_5; i++ {
				pcm[i] = pcm_transition[i]
			}
			smooth_fade(pcm_transition[st.channels*F2_5:], pcm[st.channels*F2_5:],
				pcm[st.channels*F2_5:], F2_5, st.channels, window, st.Fs)
		} else {
			smooth_fade(pcm_transition, pcm, pcm, F2_5,
				st.channels, window, st.Fs)
		}
	}

	if st.decode_gain != 0 {
		gain := celt_exp2(float32(MULT16_16_P15(QCONST16(6.48814081e-4, 25), opus_val16(st.decode_gain))))
		for i := 0; i < frame_size*st.channels; i++ {
			x := MULT16_32_P16(opus_val16(pcm[i]), opus_val32(gain))
			pcm[i] = opus_res(SATURATE(x, 32767))
		}
	}

	if length <= 1 {
		st.rangeFinal = 0
	} else {
		st.rangeFinal ^= redundant_rng
	}

	st.prev_mode = mode
	if redundancy != 0 && celt_to_silk == 0 {
		st.prev_redundancy = 1
	} else {
		st.prev_redundancy = 0
	}

	_ = silk_ret
	if celt_ret < 0 {
		return celt_ret
	}
	return audiosize
}

// opus_decode_native — C: opus_decoder.c:716.
func opus_decode_native(st *OpusDecoder, data []byte, length opus_int32,
	pcm []opus_res, frame_size int, decode_fec int,
	self_delimited int, packet_offset *opus_int32, soft_clip int) int {
	var nb_samples int
	var count, offset int
	var toc byte
	var size [48]opus_int16
	var padding []byte
	var padding_len opus_int32
	var iter OpusExtensionIterator

	if decode_fec < 0 || decode_fec > 1 {
		return OPUS_BAD_ARG
	}
	// For FEC/PLC, frame_size has to be a multiple of 2.5 ms.
	if (decode_fec != 0 || length == 0 || data == nil) && frame_size%(int(st.Fs)/400) != 0 {
		return OPUS_BAD_ARG
	}
	if length == 0 || data == nil {
		pcm_count := 0
		for {
			ret := opus_decode_frame(st, nil, 0, pcm[pcm_count*st.channels:], frame_size-pcm_count, 0)
			if ret < 0 {
				return ret
			}
			pcm_count += ret
			if pcm_count >= frame_size {
				break
			}
		}
		celt_assert(pcm_count == frame_size)
		st.last_packet_duration = pcm_count
		return pcm_count
	} else if length < 0 {
		return OPUS_BAD_ARG
	}

	packet_mode := opus_packet_get_mode(data)
	packet_bandwidth := opus_packet_get_bandwidth(data)
	packet_frame_size := opus_packet_get_samples_per_frame(data, st.Fs)
	packet_stream_channels := opus_packet_get_nb_channels(data)

	var packet_offset_local opus_int32
	count = opus_packet_parse_impl(data, length, self_delimited, &toc, nil,
		size[:], &offset, &packet_offset_local, &padding, &padding_len)
	if packet_offset != nil {
		*packet_offset = packet_offset_local
	}
	if st.ignore_extensions != 0 {
		padding = nil
		padding_len = 0
	}
	if count < 0 {
		return count
	}
	opus_extension_iterator_init(&iter, padding, padding_len, opus_int32(count))

	data = data[offset:]

	if decode_fec != 0 {
		// If no FEC can be present, run the PLC (recursive call).
		if frame_size < packet_frame_size || packet_mode == MODE_CELT_ONLY || st.mode == MODE_CELT_ONLY {
			return opus_decode_native(st, nil, 0, pcm, frame_size, 0, 0, nil, soft_clip)
		}
		// Otherwise, run the PLC on everything except the size for which we might have FEC.
		duration_copy := st.last_packet_duration
		if frame_size-packet_frame_size != 0 {
			ret := opus_decode_native(st, nil, 0, pcm, frame_size-packet_frame_size, 0, 0, nil, soft_clip)
			if ret < 0 {
				st.last_packet_duration = duration_copy
				return ret
			}
			celt_assert(ret == frame_size-packet_frame_size)
		}
		// Complete with FEC.
		st.mode = packet_mode
		st.bandwidth = packet_bandwidth
		st.frame_size = packet_frame_size
		st.stream_channels = packet_stream_channels
		ret := opus_decode_frame(st, data, opus_int32(size[0]), pcm[st.channels*(frame_size-packet_frame_size):],
			packet_frame_size, 1)
		if ret < 0 {
			return ret
		}
		st.last_packet_duration = frame_size
		return frame_size
	}

	if count*packet_frame_size > frame_size {
		return OPUS_BUFFER_TOO_SMALL
	}

	// Update the state as the last step to avoid updating it on an invalid packet.
	st.mode = packet_mode
	st.bandwidth = packet_bandwidth
	st.frame_size = packet_frame_size
	st.stream_channels = packet_stream_channels

	nb_samples = 0
	for i := 0; i < count; i++ {
		ret := opus_decode_frame(st, data, opus_int32(size[i]), pcm[nb_samples*st.channels:], frame_size-nb_samples, 0)
		if ret < 0 {
			return ret
		}
		celt_assert(ret == packet_frame_size)
		data = data[size[i]:]
		nb_samples += ret
	}
	st.last_packet_duration = nb_samples
	if soft_clip != 0 {
		opus_pcm_soft_clip_impl(pcm, nb_samples, st.channels, st.softclip_mem[:], st.arch)
	} else {
		st.softclip_mem[0] = 0
		st.softclip_mem[1] = 0
	}
	return nb_samples
}

// OPTIONAL_CLIP — C: opus_decoder.c:881. Non-FIXED_POINT build enables soft clip.
const OPTIONAL_CLIP = 1

// opus_decode — int16 output, with soft-clip. C: opus_decoder.c:896.
func opus_decode(st *OpusDecoder, data []byte, length opus_int32,
	pcm []opus_int16, frame_size int, decode_fec int) int {
	if frame_size <= 0 {
		return OPUS_BAD_ARG
	}
	if data != nil && length > 0 && decode_fec == 0 {
		nb_samples := opus_decoder_get_nb_samples(st, data, length)
		if nb_samples > 0 {
			if nb_samples < frame_size {
				frame_size = nb_samples
			}
		} else {
			return OPUS_INVALID_PACKET
		}
	}
	celt_assert(st.channels == 1 || st.channels == 2)
	out := make([]opus_res, frame_size*st.channels)

	ret := opus_decode_native(st, data, length, out, frame_size, decode_fec, 0, nil, OPTIONAL_CLIP)
	if ret > 0 {
		celt_float2int16_c(out, pcm, ret*st.channels)
	}
	return ret
}

// opus_decode24 — 24-bit-in-int32 output. C: opus_decoder.c:945.
func opus_decode24(st *OpusDecoder, data []byte, length opus_int32,
	pcm []opus_int32, frame_size int, decode_fec int) int {
	if frame_size <= 0 {
		return OPUS_BAD_ARG
	}
	if data != nil && length > 0 && decode_fec == 0 {
		nb_samples := opus_decoder_get_nb_samples(st, data, length)
		if nb_samples > 0 {
			if nb_samples < frame_size {
				frame_size = nb_samples
			}
		} else {
			return OPUS_INVALID_PACKET
		}
	}
	celt_assert(st.channels == 1 || st.channels == 2)
	out := make([]opus_res, frame_size*st.channels)

	ret := opus_decode_native(st, data, length, out, frame_size, decode_fec, 0, nil, 0)
	if ret > 0 {
		for i := 0; i < ret*st.channels; i++ {
			pcm[i] = FLOAT2INT24(float32(out[i]))
		}
	}
	return ret
}

// opus_decode_float — direct float output. C: opus_decoder.c:985 (non-FIXED_POINT path).
func opus_decode_float(st *OpusDecoder, data []byte, length opus_int32,
	pcm []opus_val16, frame_size int, decode_fec int) int {
	if frame_size <= 0 {
		return OPUS_BAD_ARG
	}
	return opus_decode_native(st, data, length, pcm, frame_size, decode_fec, 0, nil, 0)
}

// opus_decoder_ctl — variadic control API. C: opus_decoder.c:1031.
//
// The C API uses va_list; the Go port accepts the request code plus a
// variable number of typed arguments. Pointer arguments are passed as
// `*opus_int32` / `*opus_uint32` like in C.
func opus_decoder_ctl(st *OpusDecoder, request int, args ...interface{}) int {
	silk_dec := st.silk_dec
	celt_dec := st.celt_dec

	switch request {
	case OPUS_GET_BANDWIDTH_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_int32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.bandwidth)
	case OPUS_SET_COMPLEXITY_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(opus_int32)
		if !ok || value < 0 || value > 10 {
			return OPUS_BAD_ARG
		}
		st.complexity = int(value)
		celt_dec.complexity = int(value)
	case OPUS_GET_COMPLEXITY_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_int32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.complexity)
	case OPUS_GET_FINAL_RANGE_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_uint32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = st.rangeFinal
	case OPUS_RESET_STATE:
		// Reset everything past OPUS_DECODER_RESET_START (stream_channels).
		st.stream_channels = 0
		st.bandwidth = 0
		st.mode = 0
		st.prev_mode = 0
		st.frame_size = 0
		st.prev_redundancy = 0
		st.last_packet_duration = 0
		st.softclip_mem = [2]opus_val16{}
		st.rangeFinal = 0
		celt_decoder_reset(celt_dec)
		silk_ResetDecoder(silk_dec)
		st.stream_channels = st.channels
		st.frame_size = int(st.Fs) / 400
	case OPUS_GET_SAMPLE_RATE_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_int32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = st.Fs
	case OPUS_GET_PITCH_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_int32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		if st.prev_mode == MODE_CELT_ONLY {
			// celt_decoder_ctl(celt_dec, OPUS_GET_PITCH(value))
			*value = opus_int32(celt_dec.postfilter_period)
		} else {
			*value = opus_int32(st.DecControl.prevPitchLag)
		}
	case OPUS_GET_GAIN_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_int32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.decode_gain)
	case OPUS_SET_GAIN_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(opus_int32)
		if !ok || value < -32768 || value > 32767 {
			return OPUS_BAD_ARG
		}
		st.decode_gain = int(value)
	case OPUS_GET_LAST_PACKET_DURATION_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_int32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.last_packet_duration)
	case OPUS_SET_PHASE_INVERSION_DISABLED_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(opus_int32)
		if !ok || value < 0 || value > 1 {
			return OPUS_BAD_ARG
		}
		celt_dec.disable_inv = int(value)
	case OPUS_GET_PHASE_INVERSION_DISABLED_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_int32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(celt_dec.disable_inv)
	case OPUS_SET_IGNORE_EXTENSIONS_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(opus_int32)
		if !ok || value < 0 || value > 1 {
			return OPUS_BAD_ARG
		}
		st.ignore_extensions = int(value)
	case OPUS_GET_IGNORE_EXTENSIONS_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_int32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.ignore_extensions)
	default:
		return OPUS_UNIMPLEMENTED
	}
	return OPUS_OK
}

// opus_decoder_destroy — C: opus_decoder.c:1244. Go relies on GC so
// the function is a no-op; kept for API parity.
func opus_decoder_destroy(st *OpusDecoder) {
	_ = st
}

// opus_packet_has_lbrr — C: opus_decoder.c:1306.
func opus_packet_has_lbrr(packet []byte, length opus_int32) int {
	frames := make([][]byte, 48)
	var size [48]opus_int16
	nb_frames := 1

	packet_mode := opus_packet_get_mode(packet)
	if packet_mode == MODE_CELT_ONLY {
		return 0
	}
	packet_frame_size := opus_packet_get_samples_per_frame(packet, 48000)
	if packet_frame_size > 960 {
		nb_frames = packet_frame_size / 960
	}
	packet_stream_channels := opus_packet_get_nb_channels(packet)
	ret := opus_packet_parse(packet, length, nil, frames, size[:], nil)
	if ret <= 0 {
		return ret
	}
	if size[0] == 0 {
		return 0
	}
	lbrr := int((frames[0][0] >> (7 - nb_frames)) & 0x1)
	if packet_stream_channels == 2 {
		alt := int((frames[0][0] >> (6 - 2*nb_frames)) & 0x1)
		if alt != 0 {
			lbrr = 1
		}
	}
	return lbrr
}

// opus_decoder_get_nb_samples — C: opus_decoder.c:1333.
func opus_decoder_get_nb_samples(dec *OpusDecoder, packet []byte, length opus_int32) int {
	return opus_packet_get_nb_samples(packet, length, dec.Fs)
}
