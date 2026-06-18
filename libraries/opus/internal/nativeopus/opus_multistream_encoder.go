package nativeopus

import "math"

// Port of libopus/src/opus_multistream_encoder.c.
//
// Multichannel encoder wrapper: interleaves multiple mono/stereo opus
// streams into a single bitstream. Also includes the surround analysis
// and rate allocation helpers used to drive per-stream bitrates.
//
// The C file stores the sub-encoders and window/preemph memory inline
// after the OpusMSEncoder struct via pointer arithmetic. In Go we
// represent them as explicit slices / slices-of-encoders, matching the
// approach used for OpusEncoder. Byte-exact parity is preserved by
// reproducing the exact arithmetic in every path; only the storage
// layout changes.
//
// fma_add / mul_f32 / add_f32 / sub_f32 wrap every `a ± b*c` and every
// bare multiply / add the C compiler would emit separately under
// `-ffp-contract=off`. See rules 2 & 3.

// MAX_OVERLAP — C: opus_multistream_encoder.c:67. ENABLE_QEXT off.
const MAX_OVERLAP_MS = 120

// VorbisLayout — C: opus_multistream_encoder.c:46-50.
type vorbisLayoutEntry struct {
	nb_streams         int
	nb_coupled_streams int
	mapping            [8]byte
}

// vorbis_mappings — C: opus_multistream_encoder.c:53-62. Index is
// nb_channels-1.
var vorbis_mappings = [8]vorbisLayoutEntry{
	/* 1: mono           */ {1, 0, [8]byte{0}},
	/* 2: stereo         */ {1, 1, [8]byte{0, 1}},
	/* 3: 1-d surround   */ {2, 1, [8]byte{0, 2, 1}},
	/* 4: quadraphonic   */ {2, 2, [8]byte{0, 1, 2, 3}},
	/* 5: 5-channel surr */ {3, 2, [8]byte{0, 4, 1, 2, 3}},
	/* 6: 5.1 surround   */ {4, 2, [8]byte{0, 4, 1, 2, 3, 5}},
	/* 7: 6.1 surround   */ {4, 3, [8]byte{0, 4, 1, 2, 3, 5, 6}},
	/* 8: 7.1 surround   */ {5, 3, [8]byte{0, 6, 1, 2, 3, 4, 5, 7}},
}

// OpusMSEncoder — Go port of struct OpusMSEncoder.
// C: opus_private.h:99-111.
//
// Sub-encoders and window/preemph memory are explicit Go-owned
// allocations instead of the C flat arena.
type OpusMSEncoder struct {
	layout            ChannelLayout
	arch              int
	lfe_stream        int
	application       int
	Fs                opus_int32
	variable_duration int
	mapping_type      MappingType
	bitrate_bps       opus_int32

	// Sub-encoders. The C arena treats coupled streams (stereo) as
	// prefix and mono streams as suffix; we keep them in one ordered
	// slice with `nb_coupled_streams` as the boundary.
	encoders []*OpusEncoder

	// window_mem[channels * MAX_OVERLAP]. Only allocated for
	// MAPPING_TYPE_SURROUND.
	window_mem []opus_val32
	// preemph_mem[channels]. Only allocated for MAPPING_TYPE_SURROUND.
	preemph_mem []opus_val32
}

// opus_multistream_encoder_get_size — C: opus_multistream_encoder.c:383.
// Returns a positive size value (the exact byte arena count is not
// observable by Go callers).
func opus_multistream_encoder_get_size(nb_streams, nb_coupled_streams int) opus_int32 {
	if nb_streams < 1 || nb_coupled_streams > nb_streams || nb_coupled_streams < 0 {
		return 0
	}
	coupled_size := opus_encoder_get_size(2)
	mono_size := opus_encoder_get_size(1)
	return opus_int32(alignInt(1) +
		nb_coupled_streams*alignInt(coupled_size) +
		(nb_streams-nb_coupled_streams)*alignInt(mono_size))
}

// opus_multistream_surround_encoder_get_size — C: opus_multistream_encoder.c:396.
func opus_multistream_surround_encoder_get_size(channels, mapping_family int) opus_int32 {
	var nb_streams, nb_coupled_streams int

	if mapping_family == 0 {
		if channels == 1 {
			nb_streams = 1
			nb_coupled_streams = 0
		} else if channels == 2 {
			nb_streams = 1
			nb_coupled_streams = 1
		} else {
			return 0
		}
	} else if mapping_family == 1 && channels <= 8 && channels >= 1 {
		nb_streams = vorbis_mappings[channels-1].nb_streams
		nb_coupled_streams = vorbis_mappings[channels-1].nb_coupled_streams
	} else if mapping_family == 255 {
		nb_streams = channels
		nb_coupled_streams = 0
	} else if mapping_family == 2 {
		if !validate_ambisonics(channels, &nb_streams, &nb_coupled_streams) {
			return 0
		}
	} else {
		return 0
	}
	size := opus_multistream_encoder_get_size(nb_streams, nb_coupled_streams)
	if channels > 2 {
		size += opus_int32(channels * (MAX_OVERLAP_MS*4 + 4))
	}
	return size
}

// validate_ambisonics — C: opus_multistream_encoder.c:110.
func validate_ambisonics(nb_channels int, nb_streams, nb_coupled_streams *int) bool {
	if nb_channels < 1 || nb_channels > 227 {
		return false
	}

	order_plus_one := int(isqrt32(opus_uint32(nb_channels)))
	acn_channels := order_plus_one * order_plus_one
	nondiegetic_channels := nb_channels - acn_channels

	if nondiegetic_channels != 0 && nondiegetic_channels != 2 {
		return false
	}

	if nb_streams != nil {
		nd := 0
		if nondiegetic_channels != 0 {
			nd = 1
		}
		*nb_streams = acn_channels + nd
	}
	if nb_coupled_streams != nil {
		if nondiegetic_channels != 0 {
			*nb_coupled_streams = 1
		} else {
			*nb_coupled_streams = 0
		}
	}
	return true
}

// validate_encoder_layout — C: opus_multistream_encoder.c:133.
func validate_encoder_layout(layout *ChannelLayout) bool {
	for s := 0; s < layout.nb_streams; s++ {
		if s < layout.nb_coupled_streams {
			if get_left_channel(layout, s, -1) == -1 {
				return false
			}
			if get_right_channel(layout, s, -1) == -1 {
				return false
			}
		} else {
			if get_mono_channel(layout, s, -1) == -1 {
				return false
			}
		}
	}
	return true
}

// channel_pos — C: opus_multistream_encoder.c:152.
// Position in the mix: 0 don't mix, 1: left, 2: center, 3: right.
func channel_pos(channels int, pos []int) {
	if channels == 4 {
		pos[0] = 1
		pos[1] = 3
		pos[2] = 1
		pos[3] = 3
	} else if channels == 3 || channels == 5 || channels == 6 {
		pos[0] = 1
		pos[1] = 2
		pos[2] = 3
		pos[3] = 1
		pos[4] = 3
		pos[5] = 0
	} else if channels == 7 {
		pos[0] = 1
		pos[1] = 2
		pos[2] = 3
		pos[3] = 1
		pos[4] = 3
		pos[5] = 2
		pos[6] = 0
	} else if channels == 8 {
		pos[0] = 1
		pos[1] = 2
		pos[2] = 3
		pos[3] = 1
		pos[4] = 3
		pos[5] = 1
		pos[6] = 3
		pos[7] = 0
	}
}

// logSum_diff_table — C: opus_multistream_encoder.c:198.
var logSum_diff_table = [17]celt_glog{
	GCONST(0.5000000), GCONST(0.2924813), GCONST(0.1609640), GCONST(0.0849625),
	GCONST(0.0437314), GCONST(0.0221971), GCONST(0.0111839), GCONST(0.0056136),
	GCONST(0.0028123),
}

// logSum — C: opus_multistream_encoder.c:193. Rough log2(2^a + 2^b).
func logSum(a, b celt_glog) opus_val16 {
	var max, diff, frac celt_glog
	if a > b {
		max = a
		diff = SUB32(a, b)
	} else {
		max = b
		diff = SUB32(b, a)
	}
	// Inverted to catch NaNs.
	if !(diff < GCONST(8.0)) {
		return max
	}
	// Float path. C: `(int)floor(2*diff)`; the product is a bare float
	// mul, and the subsequent `2*diff - low` subtraction is bare. We
	// wrap the final `a + b*c` form in fma_add to match clang's
	// non-fused evaluation.
	low := int(math.Floor(float64(mul_f32(2, diff))))
	frac = sub_f32(mul_f32(2, diff), celt_glog(low))
	// max + diff_table[low] + MULT16_32_Q15(frac, d[low+1]-d[low])
	base := add_f32(max, logSum_diff_table[low])
	return fma_add(base, frac, SUB32(logSum_diff_table[low+1], logSum_diff_table[low]))
}

// opus_copy_channel_in_func — C: opus_private.h:128.
type opus_copy_channel_in_func func(dst []opus_res, dst_stride int, src interface{},
	src_stride, src_channel, frame_size int, user_data interface{})

// opus_copy_channel_in_float — C: opus_multistream_encoder.c:1056.
func opus_copy_channel_in_float(dst []opus_res, dst_stride int, src interface{},
	src_stride, src_channel, frame_size int, user_data interface{}) {
	_ = user_data
	float_src := src.([]float32)
	for i := 0; i < frame_size; i++ {
		dst[i*dst_stride] = FLOAT2RES(float_src[i*src_stride+src_channel])
	}
}

// opus_copy_channel_in_short — C: opus_multistream_encoder.c:1075.
func opus_copy_channel_in_short(dst []opus_res, dst_stride int, src interface{},
	src_stride, src_channel, frame_size int, user_data interface{}) {
	_ = user_data
	short_src := src.([]opus_int16)
	for i := 0; i < frame_size; i++ {
		dst[i*dst_stride] = INT16TORES(short_src[i*src_stride+src_channel])
	}
}

// opus_copy_channel_in_int24 — C: opus_multistream_encoder.c:1093.
func opus_copy_channel_in_int24(dst []opus_res, dst_stride int, src interface{},
	src_stride, src_channel, frame_size int, user_data interface{}) {
	_ = user_data
	short_src := src.([]opus_int32)
	for i := 0; i < frame_size; i++ {
		dst[i*dst_stride] = INT24TORES(short_src[i*src_stride+src_channel])
	}
}

// surround_analysis — C: opus_multistream_encoder.c:230.
//
// Every float `a ± b*c` is an fma_add/fma_sub here. The inner
// frame-max accumulator uses MAX32 (no multiply), the MULT16_32_Q15
// inside the spread step has its result added separately.
func surround_analysis(celt_mode *CELTMode, pcm interface{}, bandLogE []celt_glog,
	mem []opus_val32, preemph_mem []opus_val32,
	len_ int, overlap int, channels int, rate opus_int32,
	copy_channel_in opus_copy_channel_in_func, arch int) {

	var LM int
	var pos [8]int

	upsample := resampling_factor(rate)
	frame_size := len_ * upsample

	// LM = log2(frame_size / 120)
	for LM = 0; LM < celt_mode.maxLM; LM++ {
		if (celt_mode.shortMdctSize << LM) == frame_size {
			break
		}
	}

	freq_size := celt_mode.shortMdctSize << LM

	in := make([]opus_val32, frame_size+overlap)
	x := make([]opus_res, len_)
	freq := make([]opus_val32, freq_size)

	channel_pos(channels, pos[:])

	var maskLogE [3][21]celt_glog
	for c := 0; c < 3; c++ {
		for i := 0; i < 21; i++ {
			maskLogE[c][i] = -GCONST(28.0)
		}
	}

	var bandE [21]opus_val32

	for c := 0; c < channels; c++ {
		nb_frames := frame_size / freq_size
		celt_assert(nb_frames*freq_size == frame_size)
		// OPUS_COPY(in, mem+c*overlap, overlap);
		copy(in[:overlap], mem[c*overlap:c*overlap+overlap])
		copy_channel_in(x, 1, pcm, channels, c, len_, nil)
		celt_preemphasis(x, in[overlap:], frame_size, 1, upsample,
			celt_mode.preemph[:], &preemph_mem[c], 0)

		// Float-only NaN / ridiculous-signal guard.
		{
			sum := celt_inner_prod(in, in, frame_size+overlap, 0)
			if !(sum < 1e18) || celt_isnan(sum) != 0 {
				OPUS_CLEAR(in, frame_size+overlap)
				preemph_mem[c] = 0
			}
		}
		OPUS_CLEAR(bandE[:], 21)
		for frame := 0; frame < nb_frames; frame++ {
			var tmpE [21]opus_val32
			clt_mdct_forward(&celt_mode.mdct, in[freq_size*frame:], freq,
				celt_mode.window, overlap, celt_mode.maxLM-LM, 1, arch)
			if upsample != 1 {
				bound := freq_size / upsample
				var i int
				for i = 0; i < bound; i++ {
					freq[i] = mul_f32(freq[i], opus_val32(upsample))
				}
				for ; i < freq_size; i++ {
					freq[i] = 0
				}
			}

			compute_band_energies(celt_mode, freq, tmpE[:], 21, 1, LM, arch)
			// If we have multiple frames, take the max energy.
			for i := 0; i < 21; i++ {
				bandE[i] = MAX32(bandE[i], tmpE[i])
			}
		}
		amp2Log2(celt_mode, 21, 21, bandE[:], bandLogE[21*c:], 1)
		// Apply spreading: -6 dB going up, -12 dB going down.
		for i := 1; i < 21; i++ {
			bandLogE[21*c+i] = MAXG(bandLogE[21*c+i], sub_f32(bandLogE[21*c+i-1], GCONST(1.0)))
		}
		for i := 19; i >= 0; i-- {
			bandLogE[21*c+i] = MAXG(bandLogE[21*c+i], sub_f32(bandLogE[21*c+i+1], GCONST(2.0)))
		}
		if pos[c] == 1 {
			for i := 0; i < 21; i++ {
				maskLogE[0][i] = logSum(maskLogE[0][i], bandLogE[21*c+i])
			}
		} else if pos[c] == 3 {
			for i := 0; i < 21; i++ {
				maskLogE[2][i] = logSum(maskLogE[2][i], bandLogE[21*c+i])
			}
		} else if pos[c] == 2 {
			for i := 0; i < 21; i++ {
				tmp := sub_f32(bandLogE[21*c+i], GCONST(0.5))
				maskLogE[0][i] = logSum(maskLogE[0][i], tmp)
				maskLogE[2][i] = logSum(maskLogE[2][i], tmp)
			}
		}
		// OPUS_COPY(mem+c*overlap, in+frame_size, overlap);
		copy(mem[c*overlap:c*overlap+overlap], in[frame_size:frame_size+overlap])
	}
	for i := 0; i < 21; i++ {
		maskLogE[1][i] = MIN32(maskLogE[0][i], maskLogE[2][i])
	}
	channel_offset := HALF16(celt_log2(float32(QCONST32(2.0, 14)) / float32(channels-1)))
	for c := 0; c < 3; c++ {
		for i := 0; i < 21; i++ {
			maskLogE[c][i] = add_f32(maskLogE[c][i], channel_offset)
		}
	}
	for c := 0; c < channels; c++ {
		if pos[c] != 0 {
			mask := maskLogE[pos[c]-1][:]
			for i := 0; i < 21; i++ {
				bandLogE[21*c+i] = sub_f32(bandLogE[21*c+i], mask[i])
			}
		} else {
			for i := 0; i < 21; i++ {
				bandLogE[21*c+i] = 0
			}
		}
	}
}

// opus_multistream_encoder_init_impl — C: opus_multistream_encoder.c:436.
//
// When st == nil returns the arena size (opaque, but positive) via the
// same encoder_init pattern.
func opus_multistream_encoder_init_impl(
	st *OpusMSEncoder, Fs opus_int32, channels, streams, coupled_streams int,
	mapping []byte, application int, mapping_type MappingType,
) int {
	if channels > 255 || channels < 1 || coupled_streams > streams ||
		streams < 1 || coupled_streams < 0 || streams > 255-coupled_streams ||
		streams+coupled_streams > channels {
		return OPUS_BAD_ARG
	}

	coupled_size := opus_encoder_init(nil, Fs, 2, application)
	if coupled_size < 0 {
		return coupled_size
	}
	mono_size := opus_encoder_init(nil, Fs, 1, application)
	if mono_size < 0 {
		return mono_size
	}
	if st == nil {
		surround_size := 0
		if mapping_type == MAPPING_TYPE_SURROUND {
			surround_size = channels * (MAX_OVERLAP_MS*4 + 4)
		}
		return alignInt(1) + coupled_streams*alignInt(coupled_size) +
			(streams-coupled_streams)*alignInt(mono_size) + surround_size
	}

	st.arch = opus_select_arch()
	st.layout.nb_channels = channels
	st.layout.nb_streams = streams
	st.layout.nb_coupled_streams = coupled_streams
	if mapping_type != MAPPING_TYPE_SURROUND {
		st.lfe_stream = -1
	}
	st.bitrate_bps = OPUS_AUTO
	st.application = application
	st.Fs = Fs
	st.variable_duration = OPUS_FRAMESIZE_ARG
	for i := 0; i < st.layout.nb_channels; i++ {
		st.layout.mapping[i] = mapping[i]
	}
	if validate_layout(&st.layout) == 0 {
		return OPUS_BAD_ARG
	}
	if !validate_encoder_layout(&st.layout) {
		return OPUS_BAD_ARG
	}
	if mapping_type == MAPPING_TYPE_AMBISONICS &&
		!validate_ambisonics(st.layout.nb_channels, nil, nil) {
		return OPUS_BAD_ARG
	}

	st.encoders = make([]*OpusEncoder, streams)
	var i int
	for i = 0; i < st.layout.nb_coupled_streams; i++ {
		enc := &OpusEncoder{}
		ret := opus_encoder_init(enc, Fs, 2, application)
		if ret != OPUS_OK {
			return ret
		}
		if i == st.lfe_stream {
			opus_encoder_ctl(enc, OPUS_SET_LFE_REQUEST, opus_int32(1))
		}
		st.encoders[i] = enc
	}
	for ; i < st.layout.nb_streams; i++ {
		enc := &OpusEncoder{}
		ret := opus_encoder_init(enc, Fs, 1, application)
		if ret != OPUS_OK {
			return ret
		}
		if i == st.lfe_stream {
			opus_encoder_ctl(enc, OPUS_SET_LFE_REQUEST, opus_int32(1))
		}
		st.encoders[i] = enc
	}
	if mapping_type == MAPPING_TYPE_SURROUND {
		st.preemph_mem = make([]opus_val32, channels)
		st.window_mem = make([]opus_val32, channels*MAX_OVERLAP_MS)
	}
	st.mapping_type = mapping_type
	return OPUS_OK
}

// opus_multistream_encoder_init — C: opus_multistream_encoder.c:519.
func opus_multistream_encoder_init(st *OpusMSEncoder, Fs opus_int32, channels,
	streams, coupled_streams int, mapping []byte, application int) int {
	return opus_multistream_encoder_init_impl(st, Fs, channels, streams,
		coupled_streams, mapping, application, MAPPING_TYPE_NONE)
}

// opus_multistream_surround_encoder_init — C: opus_multistream_encoder.c:534.
//
// streams / coupled_streams are output parameters; the mapping byte
// array is filled in-place. When st != nil, lfe_stream is set.
func opus_multistream_surround_encoder_init(st *OpusMSEncoder, Fs opus_int32,
	channels, mapping_family int,
	streams, coupled_streams *int, mapping []byte, application int) int {

	lfe_stream := -1
	if channels > 255 || channels < 1 {
		return OPUS_BAD_ARG
	}

	var mapping_type MappingType

	if mapping_family == 0 {
		if channels == 1 {
			*streams = 1
			*coupled_streams = 0
			mapping[0] = 0
		} else if channels == 2 {
			*streams = 1
			*coupled_streams = 1
			mapping[0] = 0
			mapping[1] = 1
		} else {
			return OPUS_UNIMPLEMENTED
		}
	} else if mapping_family == 1 && channels <= 8 && channels >= 1 {
		*streams = vorbis_mappings[channels-1].nb_streams
		*coupled_streams = vorbis_mappings[channels-1].nb_coupled_streams
		for i := 0; i < channels; i++ {
			mapping[i] = vorbis_mappings[channels-1].mapping[i]
		}
		if channels >= 6 {
			lfe_stream = *streams - 1
		}
	} else if mapping_family == 255 {
		*streams = channels
		*coupled_streams = 0
		for i := 0; i < channels; i++ {
			mapping[i] = byte(i)
		}
	} else if mapping_family == 2 {
		if !validate_ambisonics(channels, streams, coupled_streams) {
			return OPUS_BAD_ARG
		}
		for i := 0; i < (*streams - *coupled_streams); i++ {
			mapping[i] = byte(i + (*coupled_streams * 2))
		}
		for i := 0; i < *coupled_streams*2; i++ {
			mapping[i+(*streams-*coupled_streams)] = byte(i)
		}
	} else {
		return OPUS_UNIMPLEMENTED
	}

	if channels > 2 && mapping_family == 1 {
		mapping_type = MAPPING_TYPE_SURROUND
	} else if mapping_family == 2 {
		mapping_type = MAPPING_TYPE_AMBISONICS
	} else {
		mapping_type = MAPPING_TYPE_NONE
	}
	if st != nil {
		st.lfe_stream = lfe_stream
	}
	return opus_multistream_encoder_init_impl(st, Fs, channels, *streams,
		*coupled_streams, mapping, application, mapping_type)
}

// opus_multistream_encoder_create — C: opus_multistream_encoder.c:611.
func opus_multistream_encoder_create(Fs opus_int32, channels, streams,
	coupled_streams int, mapping []byte, application int, error_ *int) *OpusMSEncoder {
	if channels > 255 || channels < 1 || coupled_streams > streams ||
		streams < 1 || coupled_streams < 0 || streams > 255-coupled_streams ||
		streams+coupled_streams > channels {
		if error_ != nil {
			*error_ = OPUS_BAD_ARG
		}
		return nil
	}
	size := opus_multistream_encoder_init(nil, Fs, channels, streams, coupled_streams, mapping, application)
	if size < 0 {
		if error_ != nil {
			*error_ = size
		}
		return nil
	}
	st := &OpusMSEncoder{}
	ret := opus_multistream_encoder_init(st, Fs, channels, streams, coupled_streams, mapping, application)
	if error_ != nil {
		*error_ = ret
	}
	if ret != OPUS_OK {
		return nil
	}
	return st
}

// opus_multistream_surround_encoder_create — C: opus_multistream_encoder.c:657.
func opus_multistream_surround_encoder_create(Fs opus_int32, channels, mapping_family int,
	streams, coupled_streams *int, mapping []byte, application int, error_ *int) *OpusMSEncoder {
	if channels > 255 || channels < 1 {
		if error_ != nil {
			*error_ = OPUS_BAD_ARG
		}
		return nil
	}
	size := opus_multistream_surround_encoder_init(nil, Fs, channels, mapping_family,
		streams, coupled_streams, mapping, application)
	if size < 0 {
		if error_ != nil {
			*error_ = size
		}
		return nil
	}
	st := &OpusMSEncoder{}
	ret := opus_multistream_surround_encoder_init(st, Fs, channels, mapping_family,
		streams, coupled_streams, mapping, application)
	if error_ != nil {
		*error_ = ret
	}
	if ret != OPUS_OK {
		return nil
	}
	return st
}

// surround_rate_allocation — C: opus_multistream_encoder.c:702.
func surround_rate_allocation(st *OpusMSEncoder, rate []opus_int32, frame_size int, Fs opus_int32) {
	var channel_rate opus_int32
	var bitrate opus_int32

	nb_lfe := 0
	if st.lfe_stream != -1 {
		nb_lfe = 1
	}
	nb_coupled := st.layout.nb_coupled_streams
	nb_uncoupled := st.layout.nb_streams - nb_coupled - nb_lfe
	nb_normal := 2*nb_coupled + nb_uncoupled

	// Bits per channel for coding band energy.
	channel_offset := 40 * imaxI32(50, Fs/opus_int32(frame_size))

	if st.bitrate_bps == OPUS_AUTO {
		bitrate = opus_int32(nb_normal)*(channel_offset+Fs+10000) + 8000*opus_int32(nb_lfe)
	} else if st.bitrate_bps == OPUS_BITRATE_MAX {
		bitrate = opus_int32(nb_normal)*750000 + opus_int32(nb_lfe)*128000
	} else {
		bitrate = st.bitrate_bps
	}

	// LFE floor.
	lfe_offset := iminI32(bitrate/20, 3000) + 15*imaxI32(50, Fs/opus_int32(frame_size))

	// Starting bitrate per stream.
	stream_offset := (bitrate - channel_offset*opus_int32(nb_normal) - lfe_offset*opus_int32(nb_lfe)) / opus_int32(nb_normal) / 2
	stream_offset = imaxI32(0, iminI32(20000, stream_offset))

	coupled_ratio := opus_int32(512) // Q8
	lfe_ratio := opus_int32(32)      // Q8

	total := opus_int32(nb_uncoupled<<8) +
		coupled_ratio*opus_int32(nb_coupled) +
		opus_int32(nb_lfe)*lfe_ratio
	// 256*(int64)(...) / total — cast to int64 to match C's
	// (opus_int64) promotion before the division.
	num := int64(256) * int64(bitrate-lfe_offset*opus_int32(nb_lfe)-
		stream_offset*opus_int32(nb_coupled+nb_uncoupled)-
		channel_offset*opus_int32(nb_normal))
	channel_rate = opus_int32(num / int64(total))

	for i := 0; i < st.layout.nb_streams; i++ {
		if i < st.layout.nb_coupled_streams {
			rate[i] = 2*channel_offset + imaxI32(0, stream_offset+(channel_rate*coupled_ratio>>8))
		} else if i != st.lfe_stream {
			rate[i] = channel_offset + imaxI32(0, stream_offset+channel_rate)
		} else {
			rate[i] = imaxI32(0, lfe_offset+(channel_rate*lfe_ratio>>8))
		}
	}
}

// ambisonics_rate_allocation — C: opus_multistream_encoder.c:771.
func ambisonics_rate_allocation(st *OpusMSEncoder, rate []opus_int32, frame_size int, Fs opus_int32) {
	var total_rate opus_int32
	nb_channels := st.layout.nb_streams + st.layout.nb_coupled_streams

	if st.bitrate_bps == OPUS_AUTO {
		total_rate = opus_int32(st.layout.nb_coupled_streams+st.layout.nb_streams)*
			(Fs+60*Fs/opus_int32(frame_size)) +
			opus_int32(st.layout.nb_streams)*opus_int32(15000)
	} else if st.bitrate_bps == OPUS_BITRATE_MAX {
		total_rate = opus_int32(nb_channels) * 750000
	} else {
		total_rate = st.bitrate_bps
	}
	per_stream_rate := total_rate / opus_int32(st.layout.nb_streams)
	for i := 0; i < st.layout.nb_streams; i++ {
		rate[i] = per_stream_rate
	}
}

// rate_allocation — C: opus_multistream_encoder.c:805.
func rate_allocation(st *OpusMSEncoder, rate []opus_int32, frame_size int) opus_int32 {
	var rate_sum opus_int32 = 0
	var Fs opus_int32
	opus_encoder_ctl(st.encoders[0], OPUS_GET_SAMPLE_RATE_REQUEST, &Fs)

	if st.mapping_type == MAPPING_TYPE_AMBISONICS {
		ambisonics_rate_allocation(st, rate, frame_size, Fs)
	} else {
		surround_rate_allocation(st, rate, frame_size, Fs)
	}

	for i := 0; i < st.layout.nb_streams; i++ {
		rate[i] = imaxI32(rate[i], 500)
		rate_sum += rate[i]
	}
	return rate_sum
}

// MS_FRAME_TMP — C: opus_multistream_encoder.c:836 (ENABLE_QEXT off).
const MS_FRAME_TMP = 6*1275 + 12

// opus_multistream_encode_native — C: opus_multistream_encoder.c:841.
//
// data is the output buffer (size max_data_bytes); the function returns
// the number of bytes written or a negative error. Every float `a±b*c`
// in the downstream calls is wrapped by the callee; there are no raw
// floats in this function.
func opus_multistream_encode_native(
	st *OpusMSEncoder,
	copy_channel_in opus_copy_channel_in_func,
	pcm interface{},
	analysis_frame_size int,
	data []byte,
	max_data_bytes opus_int32,
	lsb_depth int,
	downmix downmix_func,
	float_api int,
	user_data interface{},
) int {
	var Fs opus_int32
	var vbr opus_int32
	var celt_mode *CELTMode
	var bitrates [256]opus_int32
	var bandLogE [42]celt_glog
	var mem []opus_val32
	var preemph_mem []opus_val32
	var rate_sum opus_int32
	tmp_data := make([]byte, MS_FRAME_TMP)
	var rp OpusRepacketizer

	if st.mapping_type == MAPPING_TYPE_SURROUND {
		preemph_mem = st.preemph_mem
		mem = st.window_mem
	}

	opus_encoder_ctl(st.encoders[0], OPUS_GET_SAMPLE_RATE_REQUEST, &Fs)
	opus_encoder_ctl(st.encoders[0], OPUS_GET_VBR_REQUEST, &vbr)
	if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
		opus_encoder_ctl(st.encoders[0], CELT_GET_MODE_REQUEST, &celt_mode)
	}

	frame_size := int(frame_size_select(st.application, opus_int32(analysis_frame_size),
		st.variable_duration, Fs))
	if frame_size <= 0 {
		return OPUS_BAD_ARG
	}

	// Smallest packet the encoder can produce.
	smallest_packet := opus_int32(st.layout.nb_streams*2 - 1)
	// 100 ms needs an extra byte per stream for the ToC.
	if Fs/opus_int32(frame_size) == 10 {
		smallest_packet += opus_int32(st.layout.nb_streams)
	}
	if max_data_bytes < smallest_packet {
		return OPUS_BUFFER_TOO_SMALL
	}
	buf := make([]opus_res, 2*frame_size)

	bandSMR := make([]celt_glog, 21*st.layout.nb_channels)
	if st.mapping_type == MAPPING_TYPE_SURROUND && st.application != OPUS_APPLICATION_RESTRICTED_SILK {
		surround_analysis(celt_mode, pcm, bandSMR, mem, preemph_mem,
			frame_size, celt_mode.overlap, st.layout.nb_channels, Fs, copy_channel_in, st.arch)
	}

	// Compute bitrate allocation between streams.
	rate_sum = rate_allocation(st, bitrates[:], frame_size)

	if vbr == 0 {
		if st.bitrate_bps == OPUS_AUTO {
			max_data_bytes = iminI32(max_data_bytes, (bitrate_to_bits(rate_sum, Fs, opus_int32(frame_size))+4)/8)
		} else if st.bitrate_bps != OPUS_BITRATE_MAX {
			max_data_bytes = iminI32(max_data_bytes, imaxI32(smallest_packet,
				(bitrate_to_bits(st.bitrate_bps, Fs, opus_int32(frame_size))+4)/8))
		}
	}

	// Per-stream CTL setup.
	for s := 0; s < st.layout.nb_streams; s++ {
		enc := st.encoders[s]
		opus_encoder_ctl(enc, OPUS_SET_BITRATE_REQUEST, bitrates[s])
		if st.mapping_type == MAPPING_TYPE_SURROUND {
			equiv_rate := st.bitrate_bps
			if opus_int32(frame_size)*50 < Fs {
				equiv_rate -= 60 * (Fs/opus_int32(frame_size) - 50) * opus_int32(st.layout.nb_channels)
			}
			if equiv_rate > 10000*opus_int32(st.layout.nb_channels) {
				opus_encoder_ctl(enc, OPUS_SET_BANDWIDTH_REQUEST, opus_int32(OPUS_BANDWIDTH_FULLBAND))
			} else if equiv_rate > 7000*opus_int32(st.layout.nb_channels) {
				opus_encoder_ctl(enc, OPUS_SET_BANDWIDTH_REQUEST, opus_int32(OPUS_BANDWIDTH_SUPERWIDEBAND))
			} else if equiv_rate > 5000*opus_int32(st.layout.nb_channels) {
				opus_encoder_ctl(enc, OPUS_SET_BANDWIDTH_REQUEST, opus_int32(OPUS_BANDWIDTH_WIDEBAND))
			} else {
				opus_encoder_ctl(enc, OPUS_SET_BANDWIDTH_REQUEST, opus_int32(OPUS_BANDWIDTH_NARROWBAND))
			}
			if s < st.layout.nb_coupled_streams {
				// To preserve the spatial image, force stereo CELT on coupled streams.
				opus_encoder_ctl(enc, OPUS_SET_FORCE_MODE_REQUEST, opus_int32(MODE_CELT_ONLY))
				opus_encoder_ctl(enc, OPUS_SET_FORCE_CHANNELS_REQUEST, opus_int32(2))
			}
		} else if st.mapping_type == MAPPING_TYPE_AMBISONICS {
			opus_encoder_ctl(enc, OPUS_SET_FORCE_MODE_REQUEST, opus_int32(MODE_CELT_ONLY))
		}
	}

	// Counting ToC.
	tot_size := opus_int32(0)
	dataOff := 0
	for s := 0; s < st.layout.nb_streams; s++ {
		var c1, c2 int
		enc := st.encoders[s]

		opus_repacketizer_init(&rp)
		if s < st.layout.nb_coupled_streams {
			left := get_left_channel(&st.layout, s, -1)
			right := get_right_channel(&st.layout, s, -1)
			copy_channel_in(buf, 2, pcm, st.layout.nb_channels, left, frame_size, user_data)
			copy_channel_in(buf[1:], 2, pcm, st.layout.nb_channels, right, frame_size, user_data)
			if st.mapping_type == MAPPING_TYPE_SURROUND && st.application != OPUS_APPLICATION_RESTRICTED_SILK {
				for i := 0; i < 21; i++ {
					bandLogE[i] = bandSMR[21*left+i]
					bandLogE[21+i] = bandSMR[21*right+i]
				}
			}
			c1 = left
			c2 = right
		} else {
			chn := get_mono_channel(&st.layout, s, -1)
			copy_channel_in(buf, 1, pcm, st.layout.nb_channels, chn, frame_size, user_data)
			if st.mapping_type == MAPPING_TYPE_SURROUND && st.application != OPUS_APPLICATION_RESTRICTED_SILK {
				for i := 0; i < 21; i++ {
					bandLogE[i] = bandSMR[21*chn+i]
				}
			}
			c1 = chn
			c2 = -1
		}
		if st.mapping_type == MAPPING_TYPE_SURROUND && st.application != OPUS_APPLICATION_RESTRICTED_SILK {
			// Reuse the bandLogE tail as the mask buffer.
			opus_encoder_ctl(enc, OPUS_SET_ENERGY_MASK_REQUEST, []celt_glog(bandLogE[:]))
		}
		// Number of bytes left (+ToC).
		curr_max := max_data_bytes - tot_size
		// Reserve one byte for the last stream and two for the others.
		curr_max -= imaxI32(0, opus_int32(2*(st.layout.nb_streams-s-1)-1))
		// For 100 ms, reserve an extra byte per stream for the ToC.
		if Fs/opus_int32(frame_size) == 10 {
			curr_max -= opus_int32(st.layout.nb_streams - s - 1)
		}
		curr_max = iminI32(curr_max, MS_FRAME_TMP)
		// Repacketizer will add one or two bytes for self-delimited frames.
		if s != st.layout.nb_streams-1 {
			if curr_max > 253 {
				curr_max -= 2
			} else {
				curr_max -= 1
			}
		}
		if vbr == 0 && s == st.layout.nb_streams-1 {
			opus_encoder_ctl(enc, OPUS_SET_BITRATE_REQUEST,
				bits_to_bitrate(curr_max*8, Fs, opus_int32(frame_size)))
		}
		// Route via the existing opus_encode_native call. C passes
		// (enc, buf, frame_size, tmp_data, curr_max, lsb_depth, pcm,
		//  analysis_frame_size, c1, c2, nb_channels, downmix, float_api).
		var bufForEnc []opus_res
		if s < st.layout.nb_coupled_streams {
			bufForEnc = buf[:2*frame_size]
		} else {
			bufForEnc = buf[:frame_size]
		}
		length := int(opus_encode_native(enc, bufForEnc, frame_size, tmp_data, curr_max,
			lsb_depth, pcm, opus_int32(analysis_frame_size),
			c1, c2, st.layout.nb_channels, downmix, float_api))
		if length < 0 {
			return length
		}
		// Use the repacketizer to add self-delimiting lengths; the encoder can
		// return more than one frame (e.g. 60 ms CELT-only).
		ret := opus_repacketizer_cat(&rp, tmp_data, opus_int32(length))
		if ret != OPUS_OK {
			return OPUS_INTERNAL_ERROR
		}
		selfDelim := 0
		if s != st.layout.nb_streams-1 {
			selfDelim = 1
		}
		padFlag := 0
		if vbr == 0 && s == st.layout.nb_streams-1 {
			padFlag = 1
		}
		length = int(opus_repacketizer_out_range_impl(&rp, 0, opus_repacketizer_get_nb_frames(&rp),
			data[dataOff:], max_data_bytes-tot_size, selfDelim, padFlag, nil, 0))
		dataOff += length
		tot_size += opus_int32(length)
	}
	return int(tot_size)
}

// opus_multistream_encode — C: opus_multistream_encoder.c:1111.
func opus_multistream_encode(st *OpusMSEncoder, pcm []opus_int16, frame_size int,
	data []byte, max_data_bytes opus_int32) int {
	return opus_multistream_encode_native(st, opus_copy_channel_in_short,
		pcm, frame_size, data, max_data_bytes, 16, downmix_int, 0, nil)
}

// opus_multistream_encode24 — C: opus_multistream_encoder.c:1123.
func opus_multistream_encode24(st *OpusMSEncoder, pcm []opus_int32, frame_size int,
	data []byte, max_data_bytes opus_int32) int {
	return opus_multistream_encode_native(st, opus_copy_channel_in_int24,
		pcm, frame_size, data, max_data_bytes, MAX_ENCODING_DEPTH, downmix_int24, 0, nil)
}

// opus_multistream_encode_float — C: opus_multistream_encoder.c:1136.
func opus_multistream_encode_float(st *OpusMSEncoder, pcm []float32, frame_size int,
	data []byte, max_data_bytes opus_int32) int {
	return opus_multistream_encode_native(st, opus_copy_channel_in_float,
		pcm, frame_size, data, max_data_bytes, MAX_ENCODING_DEPTH, downmix_float, 1, nil)
}

// OPUS_MULTISTREAM_GET_ENCODER_STATE_REQUEST — C: opus_multistream.h:55.
const OPUS_MULTISTREAM_GET_ENCODER_STATE_REQUEST = 5120

// opus_multistream_encoder_ctl_va_list — C: opus_multistream_encoder.c:1149.
//
// Go's variadic args substitute for va_list. Accepts the same typed args
// as opus_encoder_ctl.
func opus_multistream_encoder_ctl_va_list(st *OpusMSEncoder, request int, args ...interface{}) int {
	ret := OPUS_OK

	switch request {
	case OPUS_SET_BITRATE_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value != OPUS_AUTO && value != OPUS_BITRATE_MAX {
			if value <= 0 {
				return OPUS_BAD_ARG
			}
			lo := opus_int32(500 * st.layout.nb_channels)
			hi := opus_int32(750000 * st.layout.nb_channels)
			if value < lo {
				value = lo
			}
			if value > hi {
				value = hi
			}
		}
		st.bitrate_bps = value

	case OPUS_GET_BITRATE_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = 0
		for s := 0; s < st.layout.nb_streams; s++ {
			var rate opus_int32
			opus_encoder_ctl(st.encoders[s], request, &rate)
			*value += rate
		}

	case OPUS_GET_LSB_DEPTH_REQUEST, OPUS_GET_VBR_REQUEST, OPUS_GET_APPLICATION_REQUEST,
		OPUS_GET_BANDWIDTH_REQUEST, OPUS_GET_COMPLEXITY_REQUEST,
		OPUS_GET_PACKET_LOSS_PERC_REQUEST, OPUS_GET_DTX_REQUEST,
		OPUS_GET_VOICE_RATIO_REQUEST, OPUS_GET_VBR_CONSTRAINT_REQUEST,
		OPUS_GET_SIGNAL_REQUEST, OPUS_GET_LOOKAHEAD_REQUEST,
		OPUS_GET_SAMPLE_RATE_REQUEST, OPUS_GET_INBAND_FEC_REQUEST,
		OPUS_GET_FORCE_CHANNELS_REQUEST,
		OPUS_GET_PREDICTION_DISABLED_REQUEST, OPUS_GET_PHASE_INVERSION_DISABLED_REQUEST:
		// For int32* GET params, just query the first stream.
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		ret = opus_encoder_ctl(st.encoders[0], request, args[0])

	case OPUS_GET_FINAL_RANGE_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(*opus_uint32)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = 0
		for s := 0; s < st.layout.nb_streams; s++ {
			var tmp opus_uint32
			ret = opus_encoder_ctl(st.encoders[s], request, &tmp)
			if ret != OPUS_OK {
				break
			}
			*value ^= tmp
		}

	case OPUS_SET_LSB_DEPTH_REQUEST, OPUS_SET_COMPLEXITY_REQUEST, OPUS_SET_VBR_REQUEST,
		OPUS_SET_VBR_CONSTRAINT_REQUEST, OPUS_SET_MAX_BANDWIDTH_REQUEST,
		OPUS_SET_BANDWIDTH_REQUEST, OPUS_SET_SIGNAL_REQUEST,
		OPUS_SET_APPLICATION_REQUEST, OPUS_SET_INBAND_FEC_REQUEST,
		OPUS_SET_PACKET_LOSS_PERC_REQUEST, OPUS_SET_DTX_REQUEST,
		OPUS_SET_FORCE_MODE_REQUEST, OPUS_SET_FORCE_CHANNELS_REQUEST,
		OPUS_SET_PREDICTION_DISABLED_REQUEST, OPUS_SET_PHASE_INVERSION_DISABLED_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		for s := 0; s < st.layout.nb_streams; s++ {
			ret = opus_encoder_ctl(st.encoders[s], request, value)
			if ret != OPUS_OK {
				break
			}
		}

	case OPUS_MULTISTREAM_GET_ENCODER_STATE_REQUEST:
		if len(args) < 2 {
			return OPUS_BAD_ARG
		}
		var stream_id int
		switch v := args[0].(type) {
		case int:
			stream_id = v
		case opus_int32:
			stream_id = int(v)
		default:
			return OPUS_BAD_ARG
		}
		if stream_id < 0 || stream_id >= st.layout.nb_streams {
			return OPUS_BAD_ARG
		}
		value, ok := args[1].(**OpusEncoder)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		*value = st.encoders[stream_id]

	case OPUS_SET_EXPERT_FRAME_DURATION_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		st.variable_duration = int(value)

	case OPUS_GET_EXPERT_FRAME_DURATION_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.variable_duration)

	case OPUS_RESET_STATE:
		if st.mapping_type == MAPPING_TYPE_SURROUND {
			OPUS_CLEAR(st.preemph_mem, st.layout.nb_channels)
			OPUS_CLEAR(st.window_mem, st.layout.nb_channels*MAX_OVERLAP_MS)
		}
		for s := 0; s < st.layout.nb_streams; s++ {
			ret = opus_encoder_ctl(st.encoders[s], OPUS_RESET_STATE)
			if ret != OPUS_OK {
				break
			}
		}

	default:
		ret = OPUS_UNIMPLEMENTED
	}

	return ret
}

// opus_multistream_encoder_ctl — C: opus_multistream_encoder.c:1350.
func opus_multistream_encoder_ctl(st *OpusMSEncoder, request int, args ...interface{}) int {
	return opus_multistream_encoder_ctl_va_list(st, request, args...)
}

// opus_multistream_encoder_destroy — C: opus_multistream_encoder.c:1360.
// No-op in Go (GC owns the allocation). Kept for API-surface parity.
func opus_multistream_encoder_destroy(st *OpusMSEncoder) { _ = st }

// imaxI32 / iminI32 — opus_int32 min/max. Mirrors IMAX/IMIN for the
// opus_int32 type used heavily in multistream rate allocation.
func imaxI32(a, b opus_int32) opus_int32 {
	if a > b {
		return a
	}
	return b
}
func iminI32(a, b opus_int32) opus_int32 {
	if a < b {
		return a
	}
	return b
}
