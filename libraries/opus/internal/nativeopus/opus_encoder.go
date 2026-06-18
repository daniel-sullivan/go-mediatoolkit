package nativeopus

import "math"

// Port of libopus/src/opus_encoder.c — top-level OpusEncoder helpers.
//
// This sub-wave (9f-C) covers the leaf helper functions used by the
// encode path:
//   - silk_biquad_res (float path)
//   - hp_cutoff        (high-pass pre-filter)
//   - dc_reject        (DC-reject filter)
//   - stereo_fade      (stereo↔mono crossfade)
//   - gain_fade        (gain ramp with overlap window)
//   - downmix_int24 (downmix_float / downmix_int already in analysis.go)
//   - compute_stereo_width + StereoWidthState
//
// The OpusEncoder struct, lifecycle (_create / _init / _destroy), CTL
// handler, and opus_encode_native are ported in sibling sub-waves
// (9f-A / later). Sections are marked with banner comments so merges
// remain trivial.
//
// Every `a ± b*c` expression in the C source is written using the
// fma_add / fma_sub / fma_rsub helpers from fma.go so the Go code
// matches a clang `-O2 -ffp-contract=off` build bit-for-bit. Bare
// multiplies routed through mul_f32, bare additions through add_f32 /
// sub_f32 where a later expression would re-form an FMA pattern under
// Go's SSA.

// ─────────────────────────────────────────────────────────────────────
// StereoWidthState — C: opus_encoder.c:70-74
// ─────────────────────────────────────────────────────────────────────

// StereoWidthState holds the compute_stereo_width smoothing state.
//
// C fields (float build):
//
//	typedef struct {
//	   opus_val32 XX, XY, YY;
//	   opus_val16 smoothed_width;
//	   opus_val16 max_follower;
//	} StereoWidthState;
type StereoWidthState struct {
	XX, XY, YY     opus_val32
	smoothed_width opus_val16
	max_follower   opus_val16
}

// ─────────────────────────────────────────────────────────────────────
// 9f-A: Struct + lifecycle + TOC + frame_size_select + user_bitrate
// C: opus_encoder.c:63-328, 622-664, 733-745, 827-852, 3362-3365
// ─────────────────────────────────────────────────────────────────────

// MAX_ENCODER_BUFFER — C: opus_encoder.c:65. QEXT disabled → 480.
const MAX_ENCODER_BUFFER = 480

// PSEUDO_SNR_THRESHOLD — C: opus_encoder.c:68. 10^(25/10).
const PSEUDO_SNR_THRESHOLD = 316.23

// OPUS_APPLICATION_* — C: opus_defines.h:216-226.
const (
	OPUS_APPLICATION_VOIP                = 2048
	OPUS_APPLICATION_AUDIO               = 2049
	OPUS_APPLICATION_RESTRICTED_LOWDELAY = 2051
	OPUS_APPLICATION_RESTRICTED_SILK     = 2052
	OPUS_APPLICATION_RESTRICTED_CELT     = 2053
)

// OPUS_AUTO — C: opus_defines.h:211.
const OPUS_AUTO = -1000

// OPUS_FRAMESIZE_* — C: opus_defines.h:236-245.
const (
	OPUS_FRAMESIZE_ARG    = 5000
	OPUS_FRAMESIZE_2_5_MS = 5001
	OPUS_FRAMESIZE_5_MS   = 5002
	OPUS_FRAMESIZE_10_MS  = 5003
	OPUS_FRAMESIZE_20_MS  = 5004
	OPUS_FRAMESIZE_40_MS  = 5005
	OPUS_FRAMESIZE_60_MS  = 5006
	OPUS_FRAMESIZE_80_MS  = 5007
	OPUS_FRAMESIZE_100_MS = 5008
	OPUS_FRAMESIZE_120_MS = 5009
)

// OpusEncoder — Go port of the C struct. C: opus_encoder.c:76-146.
//
// The C struct interleaves sub-encoders inline via offsets; Go uses
// separate pointer fields since allocation is GC-managed and the
// offsets are irrelevant to bit-exact parity. The *_offset fields are
// retained for API-surface parity.
type OpusEncoder struct {
	celt_enc_offset    int
	silk_enc_offset    int
	silk_mode          silk_EncControlStruct
	application        int
	channels           int
	delay_compensation int
	force_channels     int
	signal_type        int
	user_bandwidth     int
	max_bandwidth      int
	user_forced_mode   int
	voice_ratio        int
	Fs                 opus_int32
	use_vbr            int
	vbr_constraint     int
	variable_duration  int
	bitrate_bps        opus_int32
	user_bitrate_bps   opus_int32
	lsb_depth          int
	encoder_buffer     int
	lfe                int
	arch               int
	use_dtx            int
	fec_config         int
	// DISABLE_FLOAT_API is off in our config.
	analysis TonalityAnalysisState

	// Everything below this line is cleared on OPUS_RESET_STATE.
	// C: opus_encoder.c:111 (OPUS_ENCODER_RESET_START = stream_channels).
	stream_channels         int
	hybrid_stereo_width_Q14 opus_int16
	variable_HP_smth2_Q15   opus_int32
	prev_HB_gain            opus_val16
	hp_mem                  [4]opus_val32
	mode                    int
	prev_mode               int
	prev_channels           int
	prev_framesize          int
	bandwidth               int
	auto_bandwidth          int
	silk_bw_switch          int
	first                   int
	energy_masking          []celt_glog
	width_mem               StereoWidthState
	detected_bandwidth      int
	nb_no_activity_ms_Q1    int
	peak_signal_energy      opus_val32
	nonfinal_frame          int
	rangeFinal              opus_uint32

	// Sub-encoders — in C these live in the flat arena after the
	// struct via *_offset; here we use plain pointers.
	silk_enc *silk_encoder
	celt_enc *OpusCustomEncoder

	// delay_buffer[MAX_ENCODER_BUFFER*2] — C: opus_encoder.c:145. The
	// C arena may truncate this tail for RESTRICTED_* / mono; for the
	// Go struct we always allocate the maximum and let the *_offset
	// dance account for the byte-size difference.
	delay_buffer [MAX_ENCODER_BUFFER * 2]opus_res

	// Per-frame scratch for opus_encode_frame_native. pcmBuf holds the
	// resampled+concatenated input including history (total_buffer+
	// frame_size samples × channels). tmpPrefill is 2.5 ms of zeros for
	// the LBRR prefill path. Both sized lazily on first use since their
	// upper bound depends on the selected application.
	scratchPcmBuf     []opus_res
	scratchTmpPrefill []opus_res
}

// opusEncoderSizeOfStruct — placeholder for `sizeof(OpusEncoder)` in
// C. Go does not share the C arena layout, so this is a symbolic
// constant used only by opus_encoder_get_size / _init for API-surface
// parity. The numeric value is not consumed by any Go caller.
func opusEncoderSizeOfStruct() int { return 1 }

// opusResSizeof — sizeof(opus_res) in bytes. Float build → 4.
func opusResSizeof() int { return 4 }

// opus_encoder_get_size — C: opus_encoder.c:194-202.
func opus_encoder_get_size(channels int) int {
	ret := opus_encoder_init(nil, 48000, channels, OPUS_APPLICATION_AUDIO)
	if ret < 0 {
		return 0
	}
	return ret
}

// opus_encoder_init — C: opus_encoder.c:204-328.
//
// When `st == nil` returns the byte size the C arena would occupy.
// Otherwise zeroes *st and initializes it, returning OPUS_OK on
// success.
func opus_encoder_init(st *OpusEncoder, Fs opus_int32, channels, application int) int {
	if (Fs != 48000 && Fs != 24000 && Fs != 16000 && Fs != 12000 && Fs != 8000) ||
		(channels != 1 && channels != 2) ||
		(application != OPUS_APPLICATION_VOIP && application != OPUS_APPLICATION_AUDIO &&
			application != OPUS_APPLICATION_RESTRICTED_LOWDELAY &&
			application != OPUS_APPLICATION_RESTRICTED_SILK &&
			application != OPUS_APPLICATION_RESTRICTED_CELT) {
		return OPUS_BAD_ARG
	}

	// SILK encoder size.
	var silkSize opus_int
	if ret := silk_Get_Encoder_Size(&silkSize, opus_int(channels)); ret != 0 {
		return OPUS_BAD_ARG
	}
	silkEncSizeBytes := alignInt(int(silkSize))
	if application == OPUS_APPLICATION_RESTRICTED_CELT {
		silkEncSizeBytes = 0
	}
	celtEncSizeBytes := 0
	if application != OPUS_APPLICATION_RESTRICTED_SILK {
		celtEncSizeBytes = celt_encoder_get_size(channels)
	}
	// The C build trims the trailing delay_buffer tail when the
	// encoder is mono or restricted (a negative delta is subtracted
	// from the full sizeof). Our Go struct is symbolic (sizeof=1) so
	// applying the trim would drive the reported size below zero.
	// Since Go does not actually use the byte value for arena
	// addressing, we skip the trim and just return a positive
	// symbolic size. offsets still follow the same pattern so later
	// sub-waves can compute them.
	base_size := alignInt(opusEncoderSizeOfStruct())
	tot_size := base_size + silkEncSizeBytes + celtEncSizeBytes
	if st == nil {
		return tot_size
	}
	// OPUS_CLEAR((char*)st, tot_size).
	*st = OpusEncoder{}
	st.silk_enc_offset = base_size
	st.celt_enc_offset = st.silk_enc_offset + silkEncSizeBytes

	st.stream_channels = channels
	st.channels = channels

	st.Fs = Fs

	st.arch = opus_select_arch()

	if application != OPUS_APPLICATION_RESTRICTED_CELT {
		st.silk_enc = &silk_encoder{}
		if r := silk_InitEncoder(st.silk_enc, opus_int(st.channels), st.arch, &st.silk_mode); r != 0 {
			return OPUS_INTERNAL_ERROR
		}
	}

	// Default SILK parameters — C: opus_encoder.c:260-274.
	st.silk_mode.nChannelsAPI = opus_int32(channels)
	st.silk_mode.nChannelsInternal = opus_int32(channels)
	st.silk_mode.API_sampleRate = st.Fs
	st.silk_mode.maxInternalSampleRate = 16000
	st.silk_mode.minInternalSampleRate = 8000
	st.silk_mode.desiredInternalSampleRate = 16000
	st.silk_mode.payloadSize_ms = 20
	st.silk_mode.bitRate = 25000
	st.silk_mode.packetLossPercentage = 0
	st.silk_mode.complexity = 9
	st.silk_mode.useInBandFEC = 0
	st.silk_mode.useDRED = 0
	st.silk_mode.useDTX = 0
	st.silk_mode.useCBR = 0
	st.silk_mode.reducedDependency = 0

	// CELT encoder init. C passes the static mode pointer from
	// opus_custom_mode_create (or NULL, which celt_encoder_init resolves
	// via the same static_mode_list). We install the ported static mode
	// descriptor (static_modes_float.go) directly.
	if application != OPUS_APPLICATION_RESTRICTED_SILK {
		st.celt_enc = &OpusCustomEncoder{}
		if st.celt_enc.mode == nil {
			st.celt_enc.mode = StaticMode48000_960_120()
		}
		if err := celt_encoder_init(st.celt_enc, st.celt_enc.mode,
			Fs, channels, st.arch); err != OPUS_OK {
			return OPUS_INTERNAL_ERROR
		}
		// celt_encoder_ctl(celt_enc, CELT_SET_SIGNALLING(0));
		st.celt_enc.signalling = 0
		// celt_encoder_ctl(celt_enc, OPUS_SET_COMPLEXITY(st->silk_mode.complexity));
		st.celt_enc.complexity = int(st.silk_mode.complexity)
	}

	st.use_vbr = 1
	st.vbr_constraint = 1
	st.user_bitrate_bps = OPUS_AUTO
	st.bitrate_bps = 3000 + Fs*opus_int32(channels)
	st.application = application
	st.signal_type = OPUS_AUTO
	st.user_bandwidth = OPUS_AUTO
	st.max_bandwidth = OPUS_BANDWIDTH_FULLBAND
	st.force_channels = OPUS_AUTO
	st.user_forced_mode = OPUS_AUTO
	st.voice_ratio = -1
	if application != OPUS_APPLICATION_RESTRICTED_CELT &&
		application != OPUS_APPLICATION_RESTRICTED_SILK {
		st.encoder_buffer = int(st.Fs) / 100
	} else {
		st.encoder_buffer = 0
	}
	st.lsb_depth = 24
	st.variable_duration = OPUS_FRAMESIZE_ARG

	// 4 ms delay compensation (2.5 ms SILK look-ahead + 1.5 ms SILK
	// resamplers + stereo prediction).
	st.delay_compensation = int(st.Fs) / 250

	st.hybrid_stereo_width_Q14 = opus_int16(1 << 14)
	st.prev_HB_gain = Q15ONE
	st.variable_HP_smth2_Q15 = silk_LSHIFT(silk_lin2log(VARIABLE_HP_MIN_CUTOFF_HZ), 8)
	st.first = 1
	st.mode = MODE_HYBRID
	st.bandwidth = OPUS_BANDWIDTH_FULLBAND

	tonality_analysis_init(&st.analysis, st.Fs)
	st.analysis.application = st.application

	return OPUS_OK
}

// opus_encoder_create — C: opus_encoder.c:622-664.
func opus_encoder_create(Fs opus_int32, channels, application int, error_ *int) *OpusEncoder {
	if (Fs != 48000 && Fs != 24000 && Fs != 16000 && Fs != 12000 && Fs != 8000) ||
		(channels != 1 && channels != 2) ||
		(application != OPUS_APPLICATION_VOIP && application != OPUS_APPLICATION_AUDIO &&
			application != OPUS_APPLICATION_RESTRICTED_LOWDELAY &&
			application != OPUS_APPLICATION_RESTRICTED_SILK &&
			application != OPUS_APPLICATION_RESTRICTED_CELT) {
		if error_ != nil {
			*error_ = OPUS_BAD_ARG
		}
		return nil
	}
	size := opus_encoder_init(nil, Fs, channels, application)
	if size <= 0 {
		if error_ != nil {
			*error_ = OPUS_INTERNAL_ERROR
		}
		return nil
	}
	st := &OpusEncoder{}
	ret := opus_encoder_init(st, Fs, channels, application)
	if error_ != nil {
		*error_ = ret
	}
	if ret != OPUS_OK {
		return nil
	}
	return st
}

// opus_encoder_destroy — C: opus_encoder.c:3362-3365. Go's GC owns the
// allocation; explicit destruction is a no-op. Kept for API-surface
// parity.
func opus_encoder_destroy(st *OpusEncoder) {
	_ = st
}

// gen_toc — C: opus_encoder.c:330-360. Packs (mode, framerate,
// bandwidth, channels) into the one-byte TOC prefix.
func gen_toc(mode, framerate, bandwidth, channels int) byte {
	var period int
	var toc byte
	period = 0
	for framerate < 400 {
		framerate <<= 1
		period++
	}
	if mode == MODE_SILK_ONLY {
		toc = byte((bandwidth - OPUS_BANDWIDTH_NARROWBAND) << 5)
		toc |= byte((period - 2) << 3)
	} else if mode == MODE_CELT_ONLY {
		tmp := bandwidth - OPUS_BANDWIDTH_MEDIUMBAND
		if tmp < 0 {
			tmp = 0
		}
		toc = 0x80
		toc |= byte(tmp << 5)
		toc |= byte(period << 3)
	} else { // Hybrid
		toc = 0x60
		toc |= byte((bandwidth - OPUS_BANDWIDTH_SUPERWIDEBAND) << 4)
		toc |= byte((period - 2) << 3)
	}
	if channels == 2 {
		toc |= 1 << 2
	}
	return toc
}

// user_bitrate_to_bitrate — C: opus_encoder.c:733-745.
func user_bitrate_to_bitrate(st *OpusEncoder, frame_size, max_data_bytes int) opus_int32 {
	var max_bitrate, user_bitrate opus_int32
	if frame_size == 0 {
		frame_size = int(st.Fs) / 400
	}
	max_bitrate = bits_to_bitrate(opus_int32(max_data_bytes*8), st.Fs, opus_int32(frame_size))
	if st.user_bitrate_bps == OPUS_AUTO {
		user_bitrate = 60*st.Fs/opus_int32(frame_size) + st.Fs*opus_int32(st.channels)
	} else if st.user_bitrate_bps == OPUS_BITRATE_MAX {
		user_bitrate = 1500000
	} else {
		user_bitrate = st.user_bitrate_bps
	}
	if user_bitrate < max_bitrate {
		return user_bitrate
	}
	return max_bitrate
}

// frame_size_select — C: opus_encoder.c:827-852.
//
// Given a requested frame size (samples per channel) and a variable-
// duration hint, returns the actual frame size in samples, or -1 if
// rejected.
func frame_size_select(application int, frame_size opus_int32, variable_duration int, Fs opus_int32) opus_int32 {
	var new_size opus_int32
	if frame_size < Fs/400 {
		return -1
	}
	if variable_duration == OPUS_FRAMESIZE_ARG {
		new_size = frame_size
	} else if variable_duration >= OPUS_FRAMESIZE_2_5_MS && variable_duration <= OPUS_FRAMESIZE_120_MS {
		if variable_duration <= OPUS_FRAMESIZE_40_MS {
			new_size = (Fs / 400) << (variable_duration - OPUS_FRAMESIZE_2_5_MS)
		} else {
			new_size = opus_int32(variable_duration-OPUS_FRAMESIZE_2_5_MS-2) * Fs / 50
		}
	} else {
		return -1
	}
	if new_size > frame_size {
		return -1
	}
	if 400*new_size != Fs && 200*new_size != Fs && 100*new_size != Fs &&
		50*new_size != Fs && 25*new_size != Fs && 50*new_size != 3*Fs &&
		50*new_size != 4*Fs && 50*new_size != 5*Fs && 50*new_size != 6*Fs {
		return -1
	}
	if application == OPUS_APPLICATION_RESTRICTED_SILK && new_size < Fs/100 {
		return -1
	}
	return new_size
}

// ─────────────────────────────────────────────────────────────────────
// silk_biquad_res — C: opus_encoder.c:402-438 (float path)
// ─────────────────────────────────────────────────────────────────────
//
// Second-order ARMA filter, Direct Form II Transposed, float
// coefficients derived on the fly from Q28 integer coefficients. Called
// from hp_cutoff.

func silk_biquad_res(
	in_ []opus_res, B_Q28, A_Q28 []opus_int32,
	S []opus_val32, out []opus_res,
	length opus_int32, stride int,
) {
	var A [2]opus_val32
	var B [3]opus_val32

	// A[i] / B[i] — C: B_Q28[i] * (1.f / ((opus_int32)1<<28)).
	// A single float32 multiply; no add to fuse with.
	invScale := opus_val32(1.0) / opus_val32(opus_int32(1)<<28)
	A[0] = mul_f32(opus_val32(A_Q28[0]), invScale)
	A[1] = mul_f32(opus_val32(A_Q28[1]), invScale)
	B[0] = mul_f32(opus_val32(B_Q28[0]), invScale)
	B[1] = mul_f32(opus_val32(B_Q28[1]), invScale)
	B[2] = mul_f32(opus_val32(B_Q28[2]), invScale)

	for k := opus_int32(0); k < length; k++ {
		inval := in_[int(k)*stride]
		// vout = S[0] + B[0]*inval
		vout := fma_add(S[0], B[0], inval)
		// S[0] = S[1] - vout*A[0] + B[1]*inval
		s0 := fma_sub(S[1], vout, A[0])
		S[0] = fma_add(s0, B[1], inval)
		// S[1] = -vout*A[1] + B[2]*inval + VERY_SMALL
		s1 := fma_add(fneg_mul(vout, A[1]), B[2], inval)
		S[1] = add_f32(s1, VERY_SMALL)

		out[int(k)*stride] = vout
	}
}

// ─────────────────────────────────────────────────────────────────────
// hp_cutoff — C: opus_encoder.c:441-476
// ─────────────────────────────────────────────────────────────────────
//
// Derives second-order high-pass biquad coefficients from a cutoff
// frequency and runs the filter across all channels. The float build
// path uses silk_biquad_res for each channel (channel 1 with an
// offset into the interleaved buffer).

func hp_cutoff(
	in_ []opus_res, cutoff_Hz opus_int32, out []opus_res,
	hp_mem []opus_val32, length int, channels int, Fs opus_int32, arch int,
) {
	_ = arch

	silk_assert(cutoff_Hz <= silk_int32_MAX/SILK_FIX_CONST(1.5*3.14159/1000, 19))
	Fc_Q19 := silk_DIV32_16(
		silk_SMULBB(SILK_FIX_CONST(1.5*3.14159/1000, 19), cutoff_Hz),
		Fs/1000)
	silk_assert(Fc_Q19 > 0 && Fc_Q19 < 32768)

	r_Q28 := SILK_FIX_CONST(1.0, 28) - silk_MUL(SILK_FIX_CONST(0.92, 9), Fc_Q19)

	// b = r * [ 1; -2; 1 ];
	var B_Q28 [3]opus_int32
	B_Q28[0] = r_Q28
	B_Q28[1] = silk_LSHIFT(-r_Q28, 1)
	B_Q28[2] = r_Q28

	// a = [ 1; -2*r*(1 - 0.5*Fc^2); r^2 ];
	r_Q22 := silk_RSHIFT(r_Q28, 6)
	var A_Q28 [2]opus_int32
	A_Q28[0] = silk_SMULWW(r_Q22,
		silk_SMULWW(Fc_Q19, Fc_Q19)-SILK_FIX_CONST(2.0, 22))
	A_Q28[1] = silk_SMULWW(r_Q22, r_Q22)

	// Float path: silk_biquad_res per channel.
	silk_biquad_res(in_, B_Q28[:], A_Q28[:], hp_mem[0:2], out, opus_int32(length), channels)
	if channels == 2 {
		silk_biquad_res(in_[1:], B_Q28[:], A_Q28[:], hp_mem[2:4], out[1:], opus_int32(length), channels)
	}
}

// ─────────────────────────────────────────────────────────────────────
// dc_reject — C: opus_encoder.c:507-545 (float path)
// ─────────────────────────────────────────────────────────────────────
//
// First-order leaky integrator implementing an HP-style DC reject with
// per-sample state evolution. Two channels share state interleaved in
// hp_mem[0] / hp_mem[2] (the unused [1] / [3] slots mirror the C
// layout where hp_mem is a float[4]).

func dc_reject(
	in_ []opus_val16, cutoff_Hz opus_int32, out []opus_val16,
	hp_mem []opus_val32, length int, channels int, Fs opus_int32,
) {
	// coef  = 6.3f * cutoff_Hz / Fs
	// coef2 = 1 - coef
	// C evaluates left-to-right: (6.3f * cutoff_Hz) / Fs.
	coef := mul_f32(6.3, opus_val32(cutoff_Hz)) / opus_val32(Fs)
	coef2 := sub_f32(1, coef)

	if channels == 2 {
		m0 := hp_mem[0]
		m2 := hp_mem[2]
		for i := 0; i < length; i++ {
			x0 := opus_val32(in_[2*i+0])
			x1 := opus_val32(in_[2*i+1])
			out0 := sub_f32(x0, m0)
			out1 := sub_f32(x1, m2)
			// m0 = coef*x0 + VERY_SMALL + coef2*m0
			// Left-to-right: ((coef*x0) + VERY_SMALL) + (coef2*m0).
			m0 = fma_add(fma_add(VERY_SMALL, coef, x0), coef2, m0)
			m2 = fma_add(fma_add(VERY_SMALL, coef, x1), coef2, m2)
			out[2*i+0] = out0
			out[2*i+1] = out1
		}
		hp_mem[0] = m0
		hp_mem[2] = m2
	} else {
		m0 := hp_mem[0]
		for i := 0; i < length; i++ {
			x := opus_val32(in_[i])
			y := sub_f32(x, m0)
			m0 = fma_add(fma_add(VERY_SMALL, coef, x), coef2, m0)
			out[i] = y
		}
		hp_mem[0] = m0
	}
}

// ─────────────────────────────────────────────────────────────────────
// stereo_fade — C: opus_encoder.c:548-579
// ─────────────────────────────────────────────────────────────────────
//
// Cross-fades between stereo and the sum/diff "near-mono" mix over the
// MDCT overlap window. g1 and g2 are Q15 stereo weights; the internal
// transform `g = Q15ONE - g` makes them mono-ness coefficients.

func stereo_fade(
	in_ []opus_res, out []opus_res, g1, g2 opus_val16,
	overlap48, frame_size, channels int,
	window []celt_coef, Fs opus_int32,
) {
	inc := IMAX(1, int(48000/Fs))
	overlap := overlap48 / inc
	g1 = Q15ONE - g1
	g2 = Q15ONE - g2
	var i int
	for i = 0; i < overlap; i++ {
		w := COEF2VAL16(window[i*inc])
		w = MULT16_16_Q15(w, w)
		// g = (w*g2) + ((Q15ONE-w)*g1)
		oneMinusW := sub_f32(Q15ONE, w)
		g := add_f32(mul_f32(w, g2), mul_f32(oneMinusW, g1))
		// diff = 0.5 * (in[2i] - in[2i+1]); diff *= g
		diff := mul_f32(0.5, sub_f32(opus_val32(in_[i*channels]), opus_val32(in_[i*channels+1])))
		diff = mul_f32(g, diff)
		out[i*channels] = sub_f32(out[i*channels], diff)
		out[i*channels+1] = add_f32(out[i*channels+1], diff)
	}
	for ; i < frame_size; i++ {
		diff := mul_f32(0.5, sub_f32(opus_val32(in_[i*channels]), opus_val32(in_[i*channels+1])))
		diff = mul_f32(g2, diff)
		out[i*channels] = sub_f32(out[i*channels], diff)
		out[i*channels+1] = add_f32(out[i*channels+1], diff)
	}
}

// ─────────────────────────────────────────────────────────────────────
// gain_fade — C: opus_encoder.c:581-620
// ─────────────────────────────────────────────────────────────────────
//
// Applies a gain ramp from g1 to g2 over the overlap region (shaped by
// the window), then a flat g2 over the remainder of the frame.

func gain_fade(
	in_ []opus_res, out []opus_res, g1, g2 opus_val16,
	overlap48, frame_size, channels int,
	window []celt_coef, Fs opus_int32,
) {
	inc := IMAX(1, int(48000/Fs))
	overlap := overlap48 / inc
	if channels == 1 {
		for i := 0; i < overlap; i++ {
			w := COEF2VAL16(window[i*inc])
			w = MULT16_16_Q15(w, w)
			oneMinusW := sub_f32(Q15ONE, w)
			g := add_f32(mul_f32(w, g2), mul_f32(oneMinusW, g1))
			out[i] = mul_f32(g, in_[i])
		}
	} else {
		for i := 0; i < overlap; i++ {
			w := COEF2VAL16(window[i*inc])
			w = MULT16_16_Q15(w, w)
			oneMinusW := sub_f32(Q15ONE, w)
			g := add_f32(mul_f32(w, g2), mul_f32(oneMinusW, g1))
			out[i*2] = mul_f32(g, in_[i*2])
			out[i*2+1] = mul_f32(g, in_[i*2+1])
		}
	}
	c := 0
	for {
		for i := overlap; i < frame_size; i++ {
			out[i*channels+c] = mul_f32(g2, in_[i*channels+c])
		}
		c++
		if c >= channels {
			break
		}
	}
}

// ─────────────────────────────────────────────────────────────────────
// downmix_int24 — C: opus_encoder.c:804-825
// ─────────────────────────────────────────────────────────────────────
//
// downmix_float and downmix_int are already ported in analysis.go (they
// were needed earlier by the tonality-analysis path). downmix_int24 is
// new here. Select channel c1 and optionally accumulate into channel c2
// (or all remaining channels when c2==-2), scaled by 1/256 to move
// int24 into sig-domain.

func downmix_int24(x interface{}, y []opus_val32, subframe, offset, c1, c2, C int) {
	xs := x.([]opus_int32)
	const oneOver256 = float32(1.0 / 256.0)
	for j := 0; j < subframe; j++ {
		// INT24TOSIG(a) = (float)a * (1.f/256.f) — single multiply.
		y[j] = mul_f32(float32(xs[(j+offset)*C+c1]), oneOver256)
	}
	if c2 > -1 {
		for j := 0; j < subframe; j++ {
			// y[j] += INT24TOSIG(x[...]) — fma_add pattern.
			y[j] = fma_add(y[j], float32(xs[(j+offset)*C+c2]), oneOver256)
		}
	} else if c2 == -2 {
		for c := 1; c < C; c++ {
			for j := 0; j < subframe; j++ {
				y[j] = fma_add(y[j], float32(xs[(j+offset)*C+c]), oneOver256)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────
// compute_stereo_width — C: opus_encoder.c:854-938
// ─────────────────────────────────────────────────────────────────────
//
// Estimates effective stereo width via short-term cross-correlation
// smoothing. Drift-sensitive: every smoothing update is a first-order
// IIR of the form `a = (1-α)*a + α*new`.

func compute_stereo_width(
	pcm []opus_res, frame_size int, Fs opus_int32, mem *StereoWidthState,
) opus_val16 {
	var xx, xy, yy opus_val32
	var sqrt_xx, sqrt_yy opus_val16
	var qrrt_xx, qrrt_yy opus_val16
	frame_rate := int(Fs) / frame_size
	// short_alpha = MULT16_16(25, Q15ONE) / IMAX(50, frame_rate)
	short_alpha := opus_val16(float32(MULT16_16(opus_val16(25), Q15ONE)) / float32(IMAX(50, frame_rate)))
	xx, xy, yy = 0, 0, 0
	// Unroll by 4: discard the trailing <4 samples.
	var i int
	for i = 0; i <= frame_size-4; i += 4 {
		var pxx, pxy, pyy opus_val32
		var x, y opus_val16
		x = RES2VAL16(pcm[2*i])
		y = RES2VAL16(pcm[2*i+1])
		pxx = SHR32(MULT16_16(x, x), 2)
		pxy = SHR32(MULT16_16(x, y), 2)
		pyy = SHR32(MULT16_16(y, y), 2)
		x = RES2VAL16(pcm[2*i+2])
		y = RES2VAL16(pcm[2*i+3])
		pxx = add_f32(pxx, SHR32(MULT16_16(x, x), 2))
		pxy = add_f32(pxy, SHR32(MULT16_16(x, y), 2))
		pyy = add_f32(pyy, SHR32(MULT16_16(y, y), 2))
		x = RES2VAL16(pcm[2*i+4])
		y = RES2VAL16(pcm[2*i+5])
		pxx = add_f32(pxx, SHR32(MULT16_16(x, x), 2))
		pxy = add_f32(pxy, SHR32(MULT16_16(x, y), 2))
		pyy = add_f32(pyy, SHR32(MULT16_16(y, y), 2))
		x = RES2VAL16(pcm[2*i+6])
		y = RES2VAL16(pcm[2*i+7])
		pxx = add_f32(pxx, SHR32(MULT16_16(x, x), 2))
		pxy = add_f32(pxy, SHR32(MULT16_16(x, y), 2))
		pyy = add_f32(pyy, SHR32(MULT16_16(y, y), 2))

		xx = add_f32(xx, pxx)
		xy = add_f32(xy, pxy)
		yy = add_f32(yy, pyy)
	}
	// Float build: reject pathological (huge / NaN) energies.
	if !(xx < 1e9) || celt_isnan(xx) != 0 || !(yy < 1e9) || celt_isnan(yy) != 0 {
		xy, xx, yy = 0, 0, 0
	}
	// mem->XX += short_alpha*(xx - mem->XX)
	mem.XX = fma_add(mem.XX, short_alpha, sub_f32(xx, mem.XX))
	// mem->XY = (Q15ONE - short_alpha)*mem->XY + short_alpha*xy
	oneMinusAlpha := sub_f32(Q15ONE, short_alpha)
	mem.XY = add_f32(mul_f32(oneMinusAlpha, mem.XY), mul_f32(short_alpha, xy))
	// mem->YY += short_alpha*(yy - mem->YY)
	mem.YY = fma_add(mem.YY, short_alpha, sub_f32(yy, mem.YY))
	mem.XX = MAX32(0, mem.XX)
	mem.XY = MAX32(0, mem.XY)
	mem.YY = MAX32(0, mem.YY)
	if MAX32(mem.XX, mem.YY) > QCONST16(8e-4, 18) {
		var corr, ldiff, width opus_val16
		sqrt_xx = celt_sqrt(mem.XX)
		sqrt_yy = celt_sqrt(mem.YY)
		qrrt_xx = celt_sqrt(sqrt_xx)
		qrrt_yy = celt_sqrt(sqrt_yy)
		// mem->XY = MIN32(mem->XY, sqrt_xx*sqrt_yy)
		mem.XY = MIN32(mem.XY, mul_f32(sqrt_xx, sqrt_yy))
		// corr = frac_div32(mem->XY, EPSILON + sqrt_xx*sqrt_yy)
		denom1 := add_f32(EPSILON, mul_f32(sqrt_xx, sqrt_yy))
		corr = SHR32(frac_div32(mem.XY, denom1), 16)
		// ldiff = Q15ONE * |qrrt_xx - qrrt_yy| / (EPSILON + qrrt_xx + qrrt_yy)
		diffAbs := ABS16(sub_f32(qrrt_xx, qrrt_yy))
		num := mul_f32(Q15ONE, diffAbs)
		denom2 := add_f32(add_f32(EPSILON, qrrt_xx), qrrt_yy)
		ldiff = opus_val16(num / denom2)
		// width = MIN16(Q15ONE, sqrt(1 - corr*corr)) * ldiff
		oneMinusCorr2 := fma_sub(1.0, corr, corr)
		width = MULT16_16_Q15(MIN16(Q15ONE, celt_sqrt(oneMinusCorr2)), ldiff)
		// mem->smoothed_width += (width - mem->smoothed_width)/frame_rate
		delta := sub_f32(width, mem.smoothed_width) / opus_val16(frame_rate)
		mem.smoothed_width = add_f32(mem.smoothed_width, delta)
		// mem->max_follower = MAX16(mem->max_follower - .02/frame_rate, smoothed_width)
		decayed := sub_f32(mem.max_follower, opus_val16(0.02)/opus_val16(frame_rate))
		mem.max_follower = MAX16(decayed, mem.smoothed_width)
	}
	return EXTRACT16(MIN32(Q15ONE, MULT16_16(opus_val16(20), mem.max_follower)))
}

// ─────────────────────────────────────────────────────────────────────
// 9f-D: Threshold tables and leaf helpers
// C: opus_encoder.c:148-192, 940-1168
// ─────────────────────────────────────────────────────────────────────
//
// Threshold tables drive bandwidth / mode / FEC switching decisions.
// Leaf helpers below them are pure functions called from the encode
// path — each is a literal 1:1 port. The C globals are `static const
// opus_int32`; the "int16" description in the task was a typo — the
// C source declares them all as opus_int32.

// Transition tables for voice and music. First column is the middle
// (memoryless) threshold; second column is the hysteresis (difference
// from the middle). C: opus_encoder.c:151-174.
var mono_voice_bandwidth_thresholds = [8]opus_int32{
	9000, 700, // NB<->MB
	9000, 700, // MB<->WB
	13500, 1000, // WB<->SWB
	14000, 2000, // SWB<->FB
}
var mono_music_bandwidth_thresholds = [8]opus_int32{
	9000, 700, // NB<->MB
	9000, 700, // MB<->WB
	11000, 1000, // WB<->SWB
	12000, 2000, // SWB<->FB
}
var stereo_voice_bandwidth_thresholds = [8]opus_int32{
	9000, 700, // NB<->MB
	9000, 700, // MB<->WB
	13500, 1000, // WB<->SWB
	14000, 2000, // SWB<->FB
}
var stereo_music_bandwidth_thresholds = [8]opus_int32{
	9000, 700, // NB<->MB
	9000, 700, // MB<->WB
	11000, 1000, // WB<->SWB
	12000, 2000, // SWB<->FB
}

// Threshold bit-rates for switching between mono and stereo.
// C: opus_encoder.c:176-177.
const (
	stereo_voice_threshold opus_int32 = 19000
	stereo_music_threshold opus_int32 = 17000
)

// mode_thresholds — bit-rate thresholds for switching between
// SILK/hybrid and CELT-only. Indexed [stereo][music].
// C: opus_encoder.c:180-184.
var mode_thresholds = [2][2]opus_int32{
	// voice   music
	{64000, 10000}, // mono
	{44000, 10000}, // stereo
}

// fec_thresholds — FEC-enable thresholds and hysteresis per bandwidth.
// C: opus_encoder.c:186-192.
var fec_thresholds = [10]opus_int32{
	12000, 1000, // NB
	14000, 1000, // MB
	16000, 1000, // WB
	20000, 1000, // SWB
	22000, 1000, // FB
}

// decide_fec — C: opus_encoder.c:940-971.
//
// Decides whether in-band FEC should be enabled. May adjust `bandwidth`
// downward when loss > 5% and the current bandwidth's rate-threshold
// cannot be met at the given rate.
func decide_fec(useInBandFEC, PacketLoss_perc, last_fec, mode int, bandwidth *int, rate opus_int32) int {
	if useInBandFEC == 0 || PacketLoss_perc == 0 || mode == MODE_CELT_ONLY {
		return 0
	}
	orig_bandwidth := *bandwidth
	for {
		var hysteresis, LBRR_rate_thres_bps opus_int32
		// Compute threshold for using FEC at the current bandwidth setting.
		LBRR_rate_thres_bps = fec_thresholds[2*(*bandwidth-OPUS_BANDWIDTH_NARROWBAND)]
		hysteresis = fec_thresholds[2*(*bandwidth-OPUS_BANDWIDTH_NARROWBAND)+1]
		if last_fec == 1 {
			LBRR_rate_thres_bps -= hysteresis
		}
		if last_fec == 0 {
			LBRR_rate_thres_bps += hysteresis
		}
		LBRR_rate_thres_bps = silk_SMULWB(
			silk_MUL(LBRR_rate_thres_bps,
				opus_int32(125)-silk_min(opus_int32(PacketLoss_perc), opus_int32(25))),
			SILK_FIX_CONST(0.01, 16))
		// If loss <= 5%, we look at whether we have enough rate to enable FEC.
		// If loss > 5%, we decrease the bandwidth until we can enable FEC.
		if rate > LBRR_rate_thres_bps {
			return 1
		} else if PacketLoss_perc <= 5 {
			return 0
		} else if *bandwidth > OPUS_BANDWIDTH_NARROWBAND {
			*bandwidth--
		} else {
			break
		}
	}
	// Couldn't find any bandwidth to enable FEC, keep original bandwidth.
	*bandwidth = orig_bandwidth
	return 0
}

// compute_silk_rate_for_hybrid — C: opus_encoder.c:973-1023.
//
// Allocates a per-channel SILK rate in a SILK+CELT hybrid configuration
// by linearly interpolating between points in a bitrate-lookup table.
func compute_silk_rate_for_hybrid(rate, bandwidth, frame20ms, vbr, fec, channels int) int {
	// rate_table — C: opus_encoder.c:978-989. Columns:
	//   |total| |-------- SILK------------|
	//           |-- No FEC -| |--- FEC ---|
	//            10ms   20ms   10ms   20ms
	rate_table := [7][5]int{
		{0, 0, 0, 0, 0},
		{12000, 10000, 10000, 11000, 11000},
		{16000, 13500, 13500, 15000, 15000},
		{20000, 16000, 16000, 18000, 18000},
		{24000, 18000, 18000, 21000, 21000},
		{32000, 22000, 22000, 28000, 28000},
		{64000, 38000, 38000, 50000, 50000},
	}
	// Per-channel allocation.
	rate /= channels
	entry := 1 + frame20ms + 2*fec
	N := len(rate_table)
	var i int
	var silk_rate int
	for i = 1; i < N; i++ {
		if rate_table[i][0] > rate {
			break
		}
	}
	if i == N {
		silk_rate = rate_table[i-1][entry]
		// For now, just give 50% of the extra bits to SILK.
		silk_rate += (rate - rate_table[i-1][0]) / 2
	} else {
		var lo, hi, x0, x1 opus_int32
		lo = opus_int32(rate_table[i-1][entry])
		hi = opus_int32(rate_table[i][entry])
		x0 = opus_int32(rate_table[i-1][0])
		x1 = opus_int32(rate_table[i][0])
		silk_rate = int((lo*(x1-opus_int32(rate)) + hi*(opus_int32(rate)-x0)) / (x1 - x0))
	}
	if vbr == 0 {
		// Tiny boost to SILK for CBR. We should probably tune this better.
		silk_rate += 100
	}
	if bandwidth == OPUS_BANDWIDTH_SUPERWIDEBAND {
		silk_rate += 300
	}
	silk_rate *= channels
	// Small adjustment for stereo (calibrated for 32 kb/s, haven't tried other bitrates).
	if channels == 2 && rate >= 12000 {
		silk_rate -= 1000
	}
	return silk_rate
}

// compute_equiv_rate — C: opus_encoder.c:1027-1058.
//
// Returns the equivalent bitrate corresponding to 20 ms frames,
// complexity 10 VBR operation.
func compute_equiv_rate(bitrate opus_int32, channels, frame_rate, vbr, mode, complexity, loss int) opus_int32 {
	var equiv opus_int32
	equiv = bitrate
	// Take into account overhead from smaller frames.
	if frame_rate > 50 {
		equiv -= opus_int32(40*channels+20) * opus_int32(frame_rate-50)
	}
	// CBR is about an 8% penalty for both SILK and CELT.
	if vbr == 0 {
		equiv -= equiv / 12
	}
	// Complexity makes about 10% difference (from 0 to 10) in general.
	equiv = equiv * opus_int32(90+complexity) / 100
	if mode == MODE_SILK_ONLY || mode == MODE_HYBRID {
		// SILK complexity 0-1 uses the non-delayed-decision NSQ, which
		// costs about 20%.
		if complexity < 2 {
			equiv = equiv * 4 / 5
		}
		equiv -= equiv * opus_int32(loss) / opus_int32(6*loss+10)
	} else if mode == MODE_CELT_ONLY {
		// CELT complexity 0-4 doesn't have the pitch filter, which
		// costs about 10%.
		if complexity < 5 {
			equiv = equiv * 9 / 10
		}
	} else {
		// Mode not known yet — half the SILK loss.
		equiv -= equiv * opus_int32(loss) / opus_int32(12*loss+20)
	}
	return equiv
}

// compute_frame_energy — C: opus_encoder.c:1107-1111 (float branch).
//
// Float build is a thin wrapper over celt_inner_prod. The C signature
// takes `const opus_val16 *pcm`; in our port opus_res == opus_val16 ==
// float32, so callers pass the same slice type.
func compute_frame_energy(pcm []opus_val16, frame_size, channels, arch int) opus_val32 {
	len_ := frame_size * channels
	return celt_inner_prod(pcm, pcm, len_, arch) / opus_val32(len_)
}

// decide_dtx_mode — C: opus_encoder.c:1115-1140.
//
// Decides if DTX should be turned on (1) or off (0). Updates the
// no-activity counter (in Q1 milliseconds).
func decide_dtx_mode(activity opus_int, nb_no_activity_ms_Q1 *int, frame_size_ms_Q1 int) int {
	if activity == 0 {
		// The number of consecutive DTX frames should be within the
		// allowed bounds. The bound is defined in the SILK headers and
		// assumes 20 ms frames; convert this frame's length to ms
		// before comparing.
		*nb_no_activity_ms_Q1 += frame_size_ms_Q1
		if *nb_no_activity_ms_Q1 > NB_SPEECH_FRAMES_BEFORE_DTX*20*2 {
			if *nb_no_activity_ms_Q1 <= (NB_SPEECH_FRAMES_BEFORE_DTX+MAX_CONSECUTIVE_DTX)*20*2 {
				// Valid frame for DTX!
				return 1
			}
			*nb_no_activity_ms_Q1 = NB_SPEECH_FRAMES_BEFORE_DTX * 20 * 2
		}
	} else {
		*nb_no_activity_ms_Q1 = 0
	}
	return 0
}

// compute_redundancy_bytes — C: opus_encoder.c:1142-1168.
func compute_redundancy_bytes(max_data_bytes, bitrate_bps opus_int32, frame_rate, channels int) int {
	var redundancy_bytes_cap, redundancy_bytes int
	var redundancy_rate, available_bits opus_int32
	var base_bits int
	base_bits = 40*channels + 20

	// Equivalent rate for 5 ms frames.
	redundancy_rate = bitrate_bps + opus_int32(base_bits)*opus_int32(200-frame_rate)
	// For VBR, further increase the bitrate if we can afford it. It's
	// pretty short and we'll avoid artefacts.
	redundancy_rate = 3 * redundancy_rate / 2
	redundancy_bytes = int(redundancy_rate / 1600)

	// Compute the max rate we can use given CBR or VBR with cap.
	available_bits = max_data_bytes*8 - 2*opus_int32(base_bits)
	redundancy_bytes_cap = int((available_bits*240/(240+48000/opus_int32(frame_rate)) + opus_int32(base_bits)) / 8)
	redundancy_bytes = IMIN(redundancy_bytes, redundancy_bytes_cap)
	// It we can't get enough bits for redundancy to be worth it, rely
	// on the decoder PLC.
	if redundancy_bytes > 4+8*channels {
		redundancy_bytes = IMIN(257, redundancy_bytes)
	} else {
		redundancy_bytes = 0
	}
	return redundancy_bytes
}

// ─────────────────────────────────────────────────────────────────────
// opus_encoder_ctl (sub-wave 9f-B) — C: opus_encoder.c:2772-3360
// ─────────────────────────────────────────────────────────────────────
//
// Literal 1:1 port of the CTL dispatcher. Skipped cases are the ones
// behind config-flag gates that are OFF in our build:
//   - ENABLE_DRED  (OPUS_SET/GET_DRED_DURATION_REQUEST, OPUS_SET_DNN_BLOB_REQUEST)
//   - ENABLE_QEXT  (OPUS_SET/GET_QEXT_REQUEST)
//   - ENABLE_OSCE  (none referenced by opus_encoder.c CTL)
//   - DEEP_PLC     (none referenced by opus_encoder.c CTL)
//   - USE_WEIGHTS_FILE (OPUS_SET_DNN_BLOB_REQUEST)
//
// The forwarded sub-CTLs (OPUS_SET_COMPLEXITY, OPUS_SET_PACKET_LOSS_PERC,
// OPUS_SET_PHASE_INVERSION_DISABLED, OPUS_GET_PHASE_INVERSION_DISABLED,
// OPUS_SET_LFE, OPUS_SET_ENERGY_MASK, OPUS_RESET_STATE, CELT_GET_MODE)
// mirror celt_encoder_ctl's state updates by writing the corresponding
// OpusCustomEncoder fields directly — bit-exact with the C side since
// celt_encoder_ctl is itself just a field setter for these requests.

// OPUS_*_REQUEST constants for encoder CTL. C: opus_defines.h:130-180,
// opus_private.h:152-173, celt.h:122-141.
const (
	OPUS_SET_APPLICATION_REQUEST           = 4000
	OPUS_GET_APPLICATION_REQUEST           = 4001
	OPUS_SET_BITRATE_REQUEST               = 4002
	OPUS_GET_BITRATE_REQUEST               = 4003
	OPUS_SET_MAX_BANDWIDTH_REQUEST         = 4004
	OPUS_GET_MAX_BANDWIDTH_REQUEST         = 4005
	OPUS_SET_VBR_REQUEST                   = 4006
	OPUS_GET_VBR_REQUEST                   = 4007
	OPUS_SET_BANDWIDTH_REQUEST             = 4008
	OPUS_SET_INBAND_FEC_REQUEST            = 4012
	OPUS_GET_INBAND_FEC_REQUEST            = 4013
	OPUS_SET_PACKET_LOSS_PERC_REQUEST      = 4014
	OPUS_GET_PACKET_LOSS_PERC_REQUEST      = 4015
	OPUS_SET_DTX_REQUEST                   = 4016
	OPUS_GET_DTX_REQUEST                   = 4017
	OPUS_SET_VBR_CONSTRAINT_REQUEST        = 4020
	OPUS_GET_VBR_CONSTRAINT_REQUEST        = 4021
	OPUS_SET_FORCE_CHANNELS_REQUEST        = 4022
	OPUS_GET_FORCE_CHANNELS_REQUEST        = 4023
	OPUS_SET_SIGNAL_REQUEST                = 4024
	OPUS_GET_SIGNAL_REQUEST                = 4025
	OPUS_GET_LOOKAHEAD_REQUEST             = 4027
	OPUS_SET_LSB_DEPTH_REQUEST             = 4036
	OPUS_GET_LSB_DEPTH_REQUEST             = 4037
	OPUS_SET_EXPERT_FRAME_DURATION_REQUEST = 4040
	OPUS_GET_EXPERT_FRAME_DURATION_REQUEST = 4041
	OPUS_SET_PREDICTION_DISABLED_REQUEST   = 4042
	OPUS_GET_PREDICTION_DISABLED_REQUEST   = 4043
	OPUS_GET_IN_DTX_REQUEST                = 4049

	OPUS_SET_VOICE_RATIO_REQUEST = 11018
	OPUS_GET_VOICE_RATIO_REQUEST = 11019
	OPUS_SET_FORCE_MODE_REQUEST  = 11002

	CELT_GET_MODE_REQUEST        = 10015
	OPUS_SET_LFE_REQUEST         = 10024
	OPUS_SET_ENERGY_MASK_REQUEST = 10026
)

// OPUS_SIGNAL_* — C: opus_defines.h:228-229.
const (
	OPUS_SIGNAL_VOICE = 3001
	OPUS_SIGNAL_MUSIC = 3002
)

// opus_encoder_ctl — C: opus_encoder.c:2772-3360.
//
// Go port of the va_list dispatcher. The `args...` slice carries the
// typed arguments in-order; SET CTLs accept an opus_int32 (or a
// []celt_glog for OPUS_SET_ENERGY_MASK), GET CTLs accept a pointer
// (*opus_int32 / *opus_uint32 / **OpusCustomMode). A nil pointer
// argument maps to C's `!value` bad_arg path.
func opus_encoder_ctl(st *OpusEncoder, request int, args ...interface{}) int {
	ret := OPUS_OK
	var celt_enc *OpusCustomEncoder
	if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
		celt_enc = st.celt_enc
	}

	switch request {
	case OPUS_SET_APPLICATION_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if st.application == OPUS_APPLICATION_RESTRICTED_SILK || st.application == OPUS_APPLICATION_RESTRICTED_CELT {
			ret = OPUS_BAD_ARG
			break
		}
		if (int(value) != OPUS_APPLICATION_VOIP && int(value) != OPUS_APPLICATION_AUDIO &&
			int(value) != OPUS_APPLICATION_RESTRICTED_LOWDELAY) ||
			(st.first == 0 && st.application != int(value)) {
			ret = OPUS_BAD_ARG
			break
		}
		st.application = int(value)
		st.analysis.application = int(value)

	case OPUS_GET_APPLICATION_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.application)

	case OPUS_SET_BITRATE_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value != OPUS_AUTO && value != OPUS_BITRATE_MAX {
			if value <= 0 {
				return OPUS_BAD_ARG
			} else if value <= 500 {
				value = 500
			} else if value > opus_int32(750000)*opus_int32(st.channels) {
				value = opus_int32(750000) * opus_int32(st.channels)
			}
		}
		st.user_bitrate_bps = value

	case OPUS_GET_BITRATE_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = user_bitrate_to_bitrate(st, st.prev_framesize, 1276)

	case OPUS_SET_FORCE_CHANNELS_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if (value < 1 || int(value) > st.channels) && value != OPUS_AUTO {
			return OPUS_BAD_ARG
		}
		st.force_channels = int(value)

	case OPUS_GET_FORCE_CHANNELS_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.force_channels)

	case OPUS_SET_MAX_BANDWIDTH_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if int(value) < OPUS_BANDWIDTH_NARROWBAND || int(value) > OPUS_BANDWIDTH_FULLBAND {
			return OPUS_BAD_ARG
		}
		st.max_bandwidth = int(value)
		if st.max_bandwidth == OPUS_BANDWIDTH_NARROWBAND {
			st.silk_mode.maxInternalSampleRate = 8000
		} else if st.max_bandwidth == OPUS_BANDWIDTH_MEDIUMBAND {
			st.silk_mode.maxInternalSampleRate = 12000
		} else {
			st.silk_mode.maxInternalSampleRate = 16000
		}

	case OPUS_GET_MAX_BANDWIDTH_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.max_bandwidth)

	case OPUS_SET_BANDWIDTH_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if (int(value) < OPUS_BANDWIDTH_NARROWBAND || int(value) > OPUS_BANDWIDTH_FULLBAND) && value != OPUS_AUTO {
			return OPUS_BAD_ARG
		}
		st.user_bandwidth = int(value)
		if st.user_bandwidth == OPUS_BANDWIDTH_NARROWBAND {
			st.silk_mode.maxInternalSampleRate = 8000
		} else if st.user_bandwidth == OPUS_BANDWIDTH_MEDIUMBAND {
			st.silk_mode.maxInternalSampleRate = 12000
		} else {
			st.silk_mode.maxInternalSampleRate = 16000
		}

	case OPUS_GET_BANDWIDTH_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.bandwidth)

	case OPUS_SET_DTX_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value < 0 || value > 1 {
			return OPUS_BAD_ARG
		}
		st.use_dtx = int(value)

	case OPUS_GET_DTX_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.use_dtx)

	case OPUS_SET_COMPLEXITY_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value < 0 || value > 10 {
			return OPUS_BAD_ARG
		}
		st.silk_mode.complexity = opus_int(value)
		if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
			// celt_encoder_ctl(celt_enc, OPUS_SET_COMPLEXITY(value))
			celt_enc.complexity = int(value)
		}

	case OPUS_GET_COMPLEXITY_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.silk_mode.complexity)

	case OPUS_SET_INBAND_FEC_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value < 0 || value > 2 {
			return OPUS_BAD_ARG
		}
		st.fec_config = int(value)
		if value != 0 {
			st.silk_mode.useInBandFEC = 1
		} else {
			st.silk_mode.useInBandFEC = 0
		}

	case OPUS_GET_INBAND_FEC_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.fec_config)

	case OPUS_SET_PACKET_LOSS_PERC_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value < 0 || value > 100 {
			return OPUS_BAD_ARG
		}
		st.silk_mode.packetLossPercentage = opus_int(value)
		if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
			// celt_encoder_ctl(celt_enc, OPUS_SET_PACKET_LOSS_PERC(value))
			celt_enc.loss_rate = int(value)
		}

	case OPUS_GET_PACKET_LOSS_PERC_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.silk_mode.packetLossPercentage)

	case OPUS_SET_VBR_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value < 0 || value > 1 {
			return OPUS_BAD_ARG
		}
		st.use_vbr = int(value)
		st.silk_mode.useCBR = opus_int(1 - value)

	case OPUS_GET_VBR_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.use_vbr)

	case OPUS_SET_VOICE_RATIO_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value < -1 || value > 100 {
			return OPUS_BAD_ARG
		}
		st.voice_ratio = int(value)

	case OPUS_GET_VOICE_RATIO_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.voice_ratio)

	case OPUS_SET_VBR_CONSTRAINT_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value < 0 || value > 1 {
			return OPUS_BAD_ARG
		}
		st.vbr_constraint = int(value)

	case OPUS_GET_VBR_CONSTRAINT_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.vbr_constraint)

	case OPUS_SET_SIGNAL_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value != OPUS_AUTO && int(value) != OPUS_SIGNAL_VOICE && int(value) != OPUS_SIGNAL_MUSIC {
			return OPUS_BAD_ARG
		}
		st.signal_type = int(value)

	case OPUS_GET_SIGNAL_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.signal_type)

	case OPUS_GET_LOOKAHEAD_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = st.Fs / 400
		if st.application != OPUS_APPLICATION_RESTRICTED_LOWDELAY && st.application != OPUS_APPLICATION_RESTRICTED_CELT {
			*value += opus_int32(st.delay_compensation)
		}

	case OPUS_GET_SAMPLE_RATE_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = st.Fs

	case OPUS_GET_FINAL_RANGE_REQUEST:
		value, ok := ctlGetU32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = st.rangeFinal

	case OPUS_SET_LSB_DEPTH_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value < 8 || value > 24 {
			return OPUS_BAD_ARG
		}
		st.lsb_depth = int(value)

	case OPUS_GET_LSB_DEPTH_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.lsb_depth)

	case OPUS_SET_EXPERT_FRAME_DURATION_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if int(value) != OPUS_FRAMESIZE_ARG && int(value) != OPUS_FRAMESIZE_2_5_MS &&
			int(value) != OPUS_FRAMESIZE_5_MS && int(value) != OPUS_FRAMESIZE_10_MS &&
			int(value) != OPUS_FRAMESIZE_20_MS && int(value) != OPUS_FRAMESIZE_40_MS &&
			int(value) != OPUS_FRAMESIZE_60_MS && int(value) != OPUS_FRAMESIZE_80_MS &&
			int(value) != OPUS_FRAMESIZE_100_MS && int(value) != OPUS_FRAMESIZE_120_MS {
			return OPUS_BAD_ARG
		}
		st.variable_duration = int(value)

	case OPUS_GET_EXPERT_FRAME_DURATION_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.variable_duration)

	case OPUS_SET_PREDICTION_DISABLED_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value > 1 || value < 0 {
			return OPUS_BAD_ARG
		}
		st.silk_mode.reducedDependency = opus_int(value)

	case OPUS_GET_PREDICTION_DISABLED_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		*value = opus_int32(st.silk_mode.reducedDependency)

	case OPUS_SET_PHASE_INVERSION_DISABLED_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if value < 0 || value > 1 {
			return OPUS_BAD_ARG
		}
		if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
			// celt_encoder_ctl(celt_enc, OPUS_SET_PHASE_INVERSION_DISABLED(value))
			celt_enc.disable_inv = int(value)
		}

	case OPUS_GET_PHASE_INVERSION_DISABLED_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
			// celt_encoder_ctl(celt_enc, OPUS_GET_PHASE_INVERSION_DISABLED(value))
			*value = opus_int32(celt_enc.disable_inv)
		} else {
			*value = 0
		}

	case OPUS_RESET_STATE:
		var dummy silk_EncControlStruct
		silk_enc := st.silk_enc
		tonality_analysis_reset(&st.analysis)

		// OPUS_CLEAR of the reset-start suffix — mirror C exactly.
		st.stream_channels = 0
		st.hybrid_stereo_width_Q14 = 0
		st.variable_HP_smth2_Q15 = 0
		st.prev_HB_gain = 0
		st.hp_mem = [4]opus_val32{}
		st.mode = 0
		st.prev_mode = 0
		st.prev_channels = 0
		st.prev_framesize = 0
		st.bandwidth = 0
		st.auto_bandwidth = 0
		st.silk_bw_switch = 0
		st.first = 0
		st.energy_masking = nil
		st.width_mem = StereoWidthState{}
		st.detected_bandwidth = 0
		st.nb_no_activity_ms_Q1 = 0
		st.peak_signal_energy = 0
		st.nonfinal_frame = 0
		st.rangeFinal = 0
		st.delay_buffer = [MAX_ENCODER_BUFFER * 2]opus_res{}

		if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
			// celt_encoder_ctl(celt_enc, OPUS_RESET_STATE)
			celt_encoder_reset(celt_enc)
		}
		if st.application != OPUS_APPLICATION_RESTRICTED_CELT {
			silk_InitEncoder(silk_enc, opus_int(st.channels), st.arch, &dummy)
		}
		st.stream_channels = st.channels
		st.hybrid_stereo_width_Q14 = opus_int16(1 << 14)
		st.prev_HB_gain = Q15ONE
		st.first = 1
		st.mode = MODE_HYBRID
		st.bandwidth = OPUS_BANDWIDTH_FULLBAND
		st.variable_HP_smth2_Q15 = silk_LSHIFT(silk_lin2log(VARIABLE_HP_MIN_CUTOFF_HZ), 8)

	case OPUS_SET_FORCE_MODE_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if (int(value) < MODE_SILK_ONLY || int(value) > MODE_CELT_ONLY) && value != OPUS_AUTO {
			return OPUS_BAD_ARG
		}
		st.user_forced_mode = int(value)

	case OPUS_SET_LFE_REQUEST:
		value, ok := ctlGetI32(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		st.lfe = int(value)
		if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
			// ret = celt_encoder_ctl(celt_enc, OPUS_SET_LFE(value))
			celt_enc.lfe = int(value)
			ret = OPUS_OK
		}

	case OPUS_SET_ENERGY_MASK_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].([]celt_glog)
		if !ok {
			return OPUS_BAD_ARG
		}
		st.energy_masking = value
		if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
			// ret = celt_encoder_ctl(celt_enc, OPUS_SET_ENERGY_MASK(value))
			celt_enc.energy_mask = value
			ret = OPUS_OK
		}

	case OPUS_GET_IN_DTX_REQUEST:
		value, ok := ctlGetI32Ptr(args)
		if !ok {
			return OPUS_BAD_ARG
		}
		if st.silk_mode.useDTX != 0 && (st.prev_mode == MODE_SILK_ONLY || st.prev_mode == MODE_HYBRID) {
			// DTX determined by Silk.
			silk_enc := st.silk_enc
			if silk_enc.state_Fxx[0].sCmn.noSpeechCounter >= NB_SPEECH_FRAMES_BEFORE_DTX {
				*value = 1
			} else {
				*value = 0
			}
			// Stereo: check second channel unless only the middle channel was encoded.
			if *value == 1 && st.silk_mode.nChannelsInternal == 2 && silk_enc.prev_decode_only_middle == 0 {
				if silk_enc.state_Fxx[1].sCmn.noSpeechCounter >= NB_SPEECH_FRAMES_BEFORE_DTX {
					*value = 1
				} else {
					*value = 0
				}
			}
		} else if st.use_dtx != 0 {
			// DTX determined by Opus.
			if st.nb_no_activity_ms_Q1 >= NB_SPEECH_FRAMES_BEFORE_DTX*20*2 {
				*value = 1
			} else {
				*value = 0
			}
		} else {
			*value = 0
		}

	case CELT_GET_MODE_REQUEST:
		if len(args) < 1 {
			return OPUS_BAD_ARG
		}
		value, ok := args[0].(**OpusCustomMode)
		if !ok || value == nil {
			return OPUS_BAD_ARG
		}
		celt_assert(celt_enc != nil)
		// ret = celt_encoder_ctl(celt_enc, CELT_GET_MODE(value))
		*value = celt_enc.mode
		ret = OPUS_OK

	default:
		ret = OPUS_UNIMPLEMENTED
	}

	return ret
}

// ctlGetI32 extracts the first va_arg as an opus_int32 SET argument.
// Accepts Go int32/opus_int32/int literals commonly used at call sites.
func ctlGetI32(args []interface{}) (opus_int32, bool) {
	if len(args) < 1 {
		return 0, false
	}
	switch v := args[0].(type) {
	case opus_int32:
		return v, true
	case int:
		return opus_int32(v), true
	}
	return 0, false
}

// ctlGetI32Ptr extracts the first va_arg as a pointer-out SET argument.
func ctlGetI32Ptr(args []interface{}) (*opus_int32, bool) {
	if len(args) < 1 {
		return nil, false
	}
	v, ok := args[0].(*opus_int32)
	if !ok || v == nil {
		return nil, false
	}
	return v, true
}

// ctlGetU32Ptr extracts the first va_arg as an opus_uint32 pointer.
func ctlGetU32Ptr(args []interface{}) (*opus_uint32, bool) {
	if len(args) < 1 {
		return nil, false
	}
	v, ok := args[0].(*opus_uint32)
	if !ok || v == nil {
		return nil, false
	}
	return v, true
}

// ─────────────────────────────────────────────────────────────────────
// ---- opus_encode_frame_native ----
// 9f-E: core codec driver. C: opus_encoder.c:1855-2657.
//
// This sub-wave ports the static `opus_encode_frame_native` which is
// the internal workhorse invoked by opus_encode_native (9f-F) once
// mode / bandwidth / frame_size have been selected. It calls SILK and
// CELT, interleaves the range coder, handles redundancy frames and
// padding.
//
// DRED / ENABLE_OSCE / ENABLE_BWE / DEEP_PLC / QEXT / RES24 are all
// gated out by config.h and omitted. Fixed-point branches are omitted
// (float build only).
//
// Every `a ± b*c` expression routes through fma_add / fma_sub to keep
// parity with the Cgo oracle under `-ffp-contract=off`.
// ─────────────────────────────────────────────────────────────────────

// Local CELT encoder CTL helpers. These mirror the CELT_SET_* cases in
// libopus/celt/celt_encoder.c:2932+ but bypass the celt_encoder_ctl
// dispatcher (not yet ported) by mutating struct fields directly.

func celtEncSetStartBand(st *OpusCustomEncoder, v int) { st.start = v }
func celtEncSetEndBand(st *OpusCustomEncoder, v int)   { st.end = v }
func celtEncSetChannels(st *OpusCustomEncoder, v int)  { st.stream_channels = v }
func celtEncSetBitrate(st *OpusCustomEncoder, v opus_int32) {
	if v <= 500 && v != OPUS_BITRATE_MAX {
		return
	}
	if v > opus_int32(750000*st.channels) {
		v = opus_int32(750000 * st.channels)
	}
	st.bitrate = v
}
func celtEncSetVBR(st *OpusCustomEncoder, v int)           { st.vbr = v }
func celtEncSetVBRConstraint(st *OpusCustomEncoder, v int) { st.constrained_vbr = v }
func celtEncSetPrediction(st *OpusCustomEncoder, v int) {
	if v <= 1 {
		st.disable_pf = 1
	} else {
		st.disable_pf = 0
	}
	if v == 0 {
		st.force_intra = 1
	} else {
		st.force_intra = 0
	}
}
func celtEncSetAnalysis(st *OpusCustomEncoder, info *AnalysisInfo) {
	if info != nil {
		st.analysis = *info
	}
}
func celtEncSetSilkInfo(st *OpusCustomEncoder, info *SILKInfo) {
	if info != nil {
		st.silk_info = *info
	}
}

// opus_encode_frame_native — C: opus_encoder.c:1855-2657.
func opus_encode_frame_native(
	st *OpusEncoder,
	pcm []opus_res,
	frame_size int,
	data []byte,
	orig_max_data_bytes opus_int32,
	float_api int,
	first_frame int,
	analysis_info *AnalysisInfo,
	is_silence int,
	redundancy int,
	celt_to_silk int,
	prefill int,
	equiv_rate opus_int32,
	to_celt int,
) opus_int32 {
	_ = first_frame // Used only with ENABLE_DRED.

	var silk_enc *silk_encoder
	var celt_enc *OpusCustomEncoder
	var celt_mode *OpusCustomMode
	var i int
	var ret int
	var max_data_bytes opus_int32
	var nBytes opus_int32
	var enc ec_enc
	var bits_target int
	start_band := 0
	redundancy_bytes := 0
	var nb_compr_bytes int
	var redundant_rng opus_uint32 = 0
	var cutoff_Hz int
	var hp_freq_smth1 int
	var HB_gain opus_val16
	var apply_padding int
	var frame_rate int
	var curr_bandwidth int
	var delay_compensation int
	var total_buffer int
	var activity opus_int = VAD_NO_DECISION

	if orig_max_data_bytes < 1276 {
		max_data_bytes = orig_max_data_bytes
	} else {
		max_data_bytes = 1276
	}
	st.rangeFinal = 0
	if st.application != OPUS_APPLICATION_RESTRICTED_CELT {
		silk_enc = st.silk_enc
	}
	if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
		celt_enc = st.celt_enc
		celt_mode = celt_enc.mode
	}
	curr_bandwidth = st.bandwidth
	if st.application == OPUS_APPLICATION_RESTRICTED_LOWDELAY ||
		st.application == OPUS_APPLICATION_RESTRICTED_CELT ||
		st.application == OPUS_APPLICATION_RESTRICTED_SILK {
		delay_compensation = 0
	} else {
		delay_compensation = st.delay_compensation
	}
	total_buffer = delay_compensation

	frame_rate = int(st.Fs) / frame_size

	if is_silence != 0 {
		// C: activity = !is_silence; which is always 0 when is_silence is truthy.
		activity = 0
	} else if analysis_info != nil && analysis_info.valid != 0 {
		if analysis_info.activity_probability >= DTX_ACTIVITY_THRESHOLD {
			activity = 1
		} else {
			activity = 0
		}
		if activity == 0 {
			// Mark as active if this noise frame is sufficiently loud.
			noise_energy := compute_frame_energy(pcm, frame_size, st.channels, st.arch)
			// peak_signal_energy < (PSEUDO_SNR_THRESHOLD * noise_energy)
			if st.peak_signal_energy < mul_f32(float32(PSEUDO_SNR_THRESHOLD), float32(noise_energy)) {
				activity = 1
			} else {
				activity = 0
			}
		}
	} else if st.mode == MODE_CELT_ONLY {
		noise_energy := compute_frame_energy(pcm, frame_size, st.channels, st.arch)
		// Boost peak energy: PSEUDO_SNR_THRESHOLD * (opus_val64)HALF32(noise_energy)
		if st.peak_signal_energy < mul_f32(float32(QCONST16(PSEUDO_SNR_THRESHOLD, 0)), float32(HALF32(noise_energy))) {
			activity = 1
		} else {
			activity = 0
		}
	}

	// For the first frame at a new SILK bandwidth.
	if st.silk_bw_switch != 0 {
		redundancy = 1
		celt_to_silk = 1
		st.silk_bw_switch = 0
		prefill = 2
	}

	// If we decided to go with CELT, disable redundancy.
	if st.mode == MODE_CELT_ONLY {
		redundancy = 0
	}

	if redundancy != 0 {
		redundancy_bytes = compute_redundancy_bytes(max_data_bytes, st.bitrate_bps, frame_rate, st.stream_channels)
		if redundancy_bytes == 0 {
			redundancy = 0
		}
	}
	if st.application == OPUS_APPLICATION_RESTRICTED_SILK {
		redundancy = 0
		redundancy_bytes = 0
	}

	// bits_target = IMIN(8*(max_data_bytes-redundancy_bytes),
	//                    bitrate_to_bits(st->bitrate_bps, st->Fs, frame_size)) - 8
	b1 := int(8 * (int(max_data_bytes) - redundancy_bytes))
	b2 := int(bitrate_to_bits(st.bitrate_bps, st.Fs, opus_int32(frame_size)))
	if b1 < b2 {
		bits_target = b1 - 8
	} else {
		bits_target = b2 - 8
	}

	// data += 1 (in C); in Go we slice.
	dataOff := 1
	data1 := data[dataOff:]

	ec_enc_init(&enc, data1, opus_uint32(orig_max_data_bytes-1))

	pcmBufLen := (total_buffer + frame_size) * st.channels
	if cap(st.scratchPcmBuf) < pcmBufLen {
		st.scratchPcmBuf = make([]opus_res, pcmBufLen)
	}
	pcm_buf := st.scratchPcmBuf[:pcmBufLen]
	OPUS_COPY(pcm_buf, st.delay_buffer[(st.encoder_buffer-total_buffer)*st.channels:], total_buffer*st.channels)

	if st.mode == MODE_CELT_ONLY {
		hp_freq_smth1 = int(silk_LSHIFT(silk_lin2log(opus_int32(VARIABLE_HP_MIN_CUTOFF_HZ)), 8))
	} else {
		hp_freq_smth1 = int(silk_enc.state_Fxx[0].sCmn.variable_HP_smth1_Q15)
	}

	st.variable_HP_smth2_Q15 = silk_SMLAWB(
		st.variable_HP_smth2_Q15,
		opus_int32(hp_freq_smth1)-st.variable_HP_smth2_Q15,
		SILK_FIX_CONST(VARIABLE_HP_SMTH_COEF2, 16),
	)

	// convert from log scale to Hertz.
	cutoff_Hz = int(silk_log2lin(silk_RSHIFT(st.variable_HP_smth2_Q15, 8)))

	if st.application == OPUS_APPLICATION_VOIP {
		hp_cutoff(pcm, opus_int32(cutoff_Hz), pcm_buf[total_buffer*st.channels:], st.hp_mem[:], frame_size, st.channels, st.Fs, st.arch)
	} else {
		dc_reject(pcm, 3, pcm_buf[total_buffer*st.channels:], st.hp_mem[:], frame_size, st.channels, st.Fs)
	}

	// Float-API NaN/ridiculous-signal filter.
	if float_api != 0 {
		sum := celt_inner_prod(
			pcm_buf[total_buffer*st.channels:],
			pcm_buf[total_buffer*st.channels:],
			frame_size*st.channels, st.arch)
		if !(sum < 1e9) || celt_isnan(sum) != 0 {
			OPUS_CLEAR(pcm_buf[total_buffer*st.channels:], frame_size*st.channels)
			st.hp_mem[0] = 0
			st.hp_mem[1] = 0
			st.hp_mem[2] = 0
			st.hp_mem[3] = 0
		}
	}

	// SILK processing.
	HB_gain = Q15ONE
	if st.mode != MODE_CELT_ONLY {
		var total_bitRate, celt_rate opus_int32
		var pcm_silk []opus_res

		total_bitRate = bits_to_bitrate(opus_int32(bits_target), st.Fs, opus_int32(frame_size))
		if st.mode == MODE_HYBRID {
			var frame20ms int
			if int(st.Fs) == 50*frame_size {
				frame20ms = 1
			}
			st.silk_mode.bitRate = opus_int32(compute_silk_rate_for_hybrid(
				int(total_bitRate), curr_bandwidth, frame20ms,
				int(st.use_vbr), int(st.silk_mode.LBRR_coded), st.stream_channels))
			if st.energy_masking == nil {
				// Increasingly attenuate high band when it gets allocated fewer bits.
				celt_rate = total_bitRate - st.silk_mode.bitRate
				// HB_gain = Q15ONE - SHR32(celt_exp2(-celt_rate * QCONST16(1.f/1024, 10)), 1)
				expArg := mul_f32(-float32(celt_rate), float32(QCONST16(opus_val16(1.0/1024), 10)))
				HB_gain = opus_val16(sub_f32(float32(Q15ONE), float32(SHR32(opus_val32(celt_exp2(expArg)), 1))))
			}
		} else {
			st.silk_mode.bitRate = total_bitRate
		}

		// Surround masking for SILK.
		if st.energy_masking != nil && st.use_vbr != 0 && st.lfe == 0 {
			var mask_sum opus_val32 = 0
			var masking_depth celt_glog
			var rate_offset opus_int32
			var c int
			end := 17
			var srate opus_int16 = 16000
			if st.bandwidth == OPUS_BANDWIDTH_NARROWBAND {
				end = 13
				srate = 8000
			} else if st.bandwidth == OPUS_BANDWIDTH_MEDIUMBAND {
				end = 15
				srate = 12000
			}
			for c = 0; c < st.channels; c++ {
				for i = 0; i < end; i++ {
					var mask celt_glog
					mask = MAXG(MING(st.energy_masking[21*c+i], GCONST(0.5)), -GCONST(2.0))
					if mask > 0 {
						mask = HALF32(mask)
					}
					// mask_sum += mask — leave as bare float add; no FMA pattern.
					mask_sum = add_f32(mask_sum, mask)
				}
			}
			// masking_depth = mask_sum / end * st->channels
			// C operator precedence: (mask_sum / end) * st->channels
			md1 := mul_f32(float32(mask_sum)/float32(end), float32(st.channels))
			// masking_depth += GCONST(.2f)
			masking_depth = celt_glog(add_f32(md1, float32(GCONST(0.2))))
			rate_offset = opus_int32(PSHR32(MULT16_16(opus_val16(srate), SHR32(opus_val32(masking_depth), DB_SHIFT-10)), 10))
			if rate_offset < -2*st.silk_mode.bitRate/3 {
				rate_offset = -2 * st.silk_mode.bitRate / 3
			}
			if st.bandwidth == OPUS_BANDWIDTH_SUPERWIDEBAND || st.bandwidth == OPUS_BANDWIDTH_FULLBAND {
				st.silk_mode.bitRate += 3 * rate_offset / 5
			} else {
				st.silk_mode.bitRate += rate_offset
			}
		}

		st.silk_mode.payloadSize_ms = opus_int(1000 * frame_size / int(st.Fs))
		st.silk_mode.nChannelsAPI = opus_int32(st.channels)
		st.silk_mode.nChannelsInternal = opus_int32(st.stream_channels)
		if curr_bandwidth == OPUS_BANDWIDTH_NARROWBAND {
			st.silk_mode.desiredInternalSampleRate = 8000
		} else if curr_bandwidth == OPUS_BANDWIDTH_MEDIUMBAND {
			st.silk_mode.desiredInternalSampleRate = 12000
		} else {
			st.silk_mode.desiredInternalSampleRate = 16000
		}
		if st.mode == MODE_HYBRID {
			st.silk_mode.minInternalSampleRate = 16000
		} else {
			st.silk_mode.minInternalSampleRate = 8000
		}

		st.silk_mode.maxInternalSampleRate = 16000
		if st.mode == MODE_SILK_ONLY {
			effective_max_rate := bits_to_bitrate(opus_int32(max_data_bytes*8), st.Fs, opus_int32(frame_size))
			if frame_rate > 50 {
				effective_max_rate = effective_max_rate * 2 / 3
			}
			if effective_max_rate < 8000 {
				st.silk_mode.maxInternalSampleRate = 12000
				if st.silk_mode.desiredInternalSampleRate > 12000 {
					st.silk_mode.desiredInternalSampleRate = 12000
				}
			}
			if effective_max_rate < 7000 {
				st.silk_mode.maxInternalSampleRate = 8000
				if st.silk_mode.desiredInternalSampleRate > 8000 {
					st.silk_mode.desiredInternalSampleRate = 8000
				}
			}
		}

		if st.use_vbr == 0 {
			st.silk_mode.useCBR = 1
		} else {
			st.silk_mode.useCBR = 0
		}

		// Max bits for SILK (ToC + redundancy bytes).
		st.silk_mode.maxBits = opus_int(max_data_bytes-1) * 8
		if redundancy != 0 && redundancy_bytes >= 2 {
			st.silk_mode.maxBits -= opus_int(redundancy_bytes*8 + 1)
			if st.mode == MODE_HYBRID {
				st.silk_mode.maxBits -= 20
			}
		}
		if st.silk_mode.useCBR != 0 {
			if st.mode == MODE_HYBRID {
				// Allow SILK to steal up to 25% of the remaining bits.
				// C: opus_int16 other_bits = IMAX(0, maxBits - bitRate*frame_size/Fs);
				tmp := opus_int32(st.silk_mode.maxBits) - st.silk_mode.bitRate*opus_int32(frame_size)/st.Fs
				if tmp < 0 {
					tmp = 0
				}
				other_bits := opus_int16(tmp)
				// st->silk_mode.maxBits = IMAX(0, st->silk_mode.maxBits - other_bits*3/4);
				newMax := opus_int(st.silk_mode.maxBits) - opus_int(other_bits)*3/4
				if newMax < 0 {
					newMax = 0
				}
				st.silk_mode.maxBits = newMax
				st.silk_mode.useCBR = 0
			}
		} else {
			if st.mode == MODE_HYBRID {
				var frame20ms int
				if int(st.Fs) == 50*frame_size {
					frame20ms = 1
				}
				maxBitRate := compute_silk_rate_for_hybrid(
					int(opus_int32(st.silk_mode.maxBits)*st.Fs/opus_int32(frame_size)),
					curr_bandwidth, frame20ms,
					int(st.use_vbr), int(st.silk_mode.LBRR_coded), st.stream_channels)
				st.silk_mode.maxBits = opus_int(bitrate_to_bits(opus_int32(maxBitRate), st.Fs, opus_int32(frame_size)))
			}
		}

		if prefill != 0 && st.application != OPUS_APPLICATION_RESTRICTED_SILK {
			var zero opus_int32 = 0
			var prefill_offset int
			prefill_offset = st.channels * (st.encoder_buffer - st.delay_compensation - int(st.Fs)/400)
			gain_fade(
				st.delay_buffer[prefill_offset:], st.delay_buffer[prefill_offset:],
				0, Q15ONE, celt_mode.overlap, int(st.Fs)/400, st.channels, celt_mode.window, st.Fs)
			OPUS_CLEAR(st.delay_buffer[:], prefill_offset)
			pcm_silk = st.delay_buffer[:]
			silk_Encode(silk_enc, &st.silk_mode, pcm_silk, opus_int(st.encoder_buffer), nil, &zero, opus_int(prefill), activity)
			// Prevent a second switch in the real encode call.
			st.silk_mode.opusCanSwitch = 0
		}

		pcm_silk = pcm_buf[total_buffer*st.channels:]
		ret = int(silk_Encode(silk_enc, &st.silk_mode, pcm_silk, opus_int(frame_size), &enc, &nBytes, 0, activity))
		if ret != 0 {
			return OPUS_INTERNAL_ERROR
		}

		// Extract SILK internal bandwidth.
		if st.mode == MODE_SILK_ONLY {
			if st.silk_mode.internalSampleRate == 8000 {
				curr_bandwidth = OPUS_BANDWIDTH_NARROWBAND
			} else if st.silk_mode.internalSampleRate == 12000 {
				curr_bandwidth = OPUS_BANDWIDTH_MEDIUMBAND
			} else if st.silk_mode.internalSampleRate == 16000 {
				curr_bandwidth = OPUS_BANDWIDTH_WIDEBAND
			}
		}

		if st.silk_mode.switchReady != 0 && st.nonfinal_frame == 0 {
			st.silk_mode.opusCanSwitch = 1
		} else {
			st.silk_mode.opusCanSwitch = 0
		}

		if activity == VAD_NO_DECISION {
			if st.silk_mode.signalType != TYPE_NO_VOICE_ACTIVITY {
				activity = 1
			} else {
				activity = 0
			}
		}
		if nBytes == 0 {
			st.rangeFinal = 0
			data[0] = gen_toc(st.mode, int(st.Fs)/frame_size, curr_bandwidth, st.stream_channels)
			return 1
		}

		// FIXME: How do we allocate the redundancy for CBR?
		if st.silk_mode.opusCanSwitch != 0 {
			if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
				redundancy_bytes = compute_redundancy_bytes(max_data_bytes, st.bitrate_bps, frame_rate, st.stream_channels)
				if redundancy_bytes != 0 {
					redundancy = 1
				} else {
					redundancy = 0
				}
			}
			celt_to_silk = 0
			st.silk_bw_switch = 1
		}
	}

	// CELT processing.
	if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
		endband := 21
		switch curr_bandwidth {
		case OPUS_BANDWIDTH_NARROWBAND:
			endband = 13
		case OPUS_BANDWIDTH_MEDIUMBAND, OPUS_BANDWIDTH_WIDEBAND:
			endband = 17
		case OPUS_BANDWIDTH_SUPERWIDEBAND:
			endband = 19
		case OPUS_BANDWIDTH_FULLBAND:
			endband = 21
		}
		celtEncSetEndBand(celt_enc, endband)
		celtEncSetChannels(celt_enc, st.stream_channels)
		celtEncSetBitrate(celt_enc, OPUS_BITRATE_MAX)
	}
	if st.mode != MODE_SILK_ONLY {
		celt_pred := 2
		if st.silk_mode.reducedDependency != 0 {
			celt_pred = 0
		}
		celtEncSetPrediction(celt_enc, celt_pred)
	}

	tmpPrefillLen := st.channels * int(st.Fs) / 400
	if cap(st.scratchTmpPrefill) < tmpPrefillLen {
		st.scratchTmpPrefill = make([]opus_res, tmpPrefillLen)
	}
	tmp_prefill := st.scratchTmpPrefill[:tmpPrefillLen]
	for i := range tmp_prefill {
		tmp_prefill[i] = 0
	}
	if st.mode != MODE_SILK_ONLY && st.mode != st.prev_mode && st.prev_mode > 0 &&
		st.application != OPUS_APPLICATION_RESTRICTED_CELT {
		OPUS_COPY(tmp_prefill,
			st.delay_buffer[(st.encoder_buffer-total_buffer-int(st.Fs)/400)*st.channels:],
			st.channels*int(st.Fs)/400)
	}

	if st.channels*(st.encoder_buffer-(frame_size+total_buffer)) > 0 {
		OPUS_MOVE(st.delay_buffer[:],
			st.delay_buffer[st.channels*frame_size:],
			st.channels*(st.encoder_buffer-frame_size-total_buffer))
		OPUS_COPY(
			st.delay_buffer[st.channels*(st.encoder_buffer-frame_size-total_buffer):],
			pcm_buf,
			(frame_size+total_buffer)*st.channels)
	} else {
		OPUS_COPY(st.delay_buffer[:],
			pcm_buf[(frame_size+total_buffer-st.encoder_buffer)*st.channels:],
			st.encoder_buffer*st.channels)
	}

	// gain_fade and stereo_fade happen AFTER delay_buffer copy so
	// the SILK part is unaffected.
	if (st.prev_HB_gain < Q15ONE || HB_gain < Q15ONE) && celt_mode != nil {
		gain_fade(pcm_buf, pcm_buf,
			st.prev_HB_gain, HB_gain, celt_mode.overlap, frame_size, st.channels, celt_mode.window, st.Fs)
	}
	st.prev_HB_gain = HB_gain
	if st.mode != MODE_HYBRID || st.stream_channels == 1 {
		if equiv_rate > 32000 {
			st.silk_mode.stereoWidth_Q14 = 16384
		} else if equiv_rate < 16000 {
			st.silk_mode.stereoWidth_Q14 = 0
		} else {
			// 16384 - 2048*(opus_int32)(32000-equiv_rate)/(equiv_rate-14000)
			st.silk_mode.stereoWidth_Q14 = opus_int(16384 - 2048*opus_int32(32000-equiv_rate)/(equiv_rate-14000))
		}
	}
	if st.energy_masking == nil && st.channels == 2 {
		// Apply stereo width reduction (at low bitrates).
		if st.hybrid_stereo_width_Q14 < (1<<14) || st.silk_mode.stereoWidth_Q14 < (1<<14) {
			var g1, g2 opus_val16
			g1 = opus_val16(st.hybrid_stereo_width_Q14)
			g2 = opus_val16(st.silk_mode.stereoWidth_Q14)
			// Float: g1 *= 1.f/16384
			g1 = opus_val16(mul_f32(float32(g1), 1.0/16384))
			g2 = opus_val16(mul_f32(float32(g2), 1.0/16384))
			if celt_mode != nil {
				stereo_fade(pcm_buf, pcm_buf, g1, g2, celt_mode.overlap,
					frame_size, st.channels, celt_mode.window, st.Fs)
			}
			st.hybrid_stereo_width_Q14 = opus_int16(st.silk_mode.stereoWidth_Q14)
		}
	}

	hybridExtra := 0
	if st.mode == MODE_HYBRID {
		hybridExtra = 20
	}
	if st.mode != MODE_CELT_ONLY && ec_tell(&enc)+17+hybridExtra <= 8*(int(max_data_bytes)-1) {
		// For SILK mode, the redundancy is inferred from the length.
		if st.mode == MODE_HYBRID {
			ec_enc_bit_logp(&enc, redundancy, 12)
		}
		if redundancy != 0 {
			var max_redundancy int
			ec_enc_bit_logp(&enc, celt_to_silk, 1)
			if st.mode == MODE_HYBRID {
				max_redundancy = (int(max_data_bytes) - 1) - ((ec_tell(&enc) + 8 + 3 + 7) >> 3)
			} else {
				max_redundancy = (int(max_data_bytes) - 1) - ((ec_tell(&enc) + 7) >> 3)
			}
			if max_redundancy < redundancy_bytes {
				redundancy_bytes = max_redundancy
			}
			// redundancy_bytes = IMIN(257, IMAX(2, redundancy_bytes))
			if redundancy_bytes < 2 {
				redundancy_bytes = 2
			}
			if redundancy_bytes > 257 {
				redundancy_bytes = 257
			}
			if st.mode == MODE_HYBRID {
				ec_enc_uint(&enc, opus_uint32(redundancy_bytes-2), 256)
			}
		}
	} else {
		redundancy = 0
	}

	if redundancy == 0 {
		st.silk_bw_switch = 0
		redundancy_bytes = 0
	}
	if st.mode != MODE_CELT_ONLY {
		start_band = 17
	}

	if st.mode == MODE_SILK_ONLY {
		ret = (ec_tell(&enc) + 7) >> 3
		ec_enc_done(&enc)
		nb_compr_bytes = ret
	} else {
		nb_compr_bytes = (int(max_data_bytes) - 1) - redundancy_bytes
		ec_enc_shrink(&enc, opus_uint32(nb_compr_bytes))
	}

	if redundancy != 0 || st.mode != MODE_SILK_ONLY {
		celtEncSetAnalysis(celt_enc, analysis_info)
	}
	if st.mode == MODE_HYBRID {
		var info SILKInfo
		info.signalType = int(st.silk_mode.signalType)
		info.offset = int(st.silk_mode.offset)
		celtEncSetSilkInfo(celt_enc, &info)
	}

	// 5 ms redundant frame for CELT->SILK.
	if redundancy != 0 && celt_to_silk != 0 {
		var err int
		celtEncSetStartBand(celt_enc, 0)
		celtEncSetVBR(celt_enc, 0)
		celtEncSetBitrate(celt_enc, OPUS_BITRATE_MAX)
		err = celt_encode_with_ec(celt_enc, pcm_buf, int(st.Fs)/200,
			data1[nb_compr_bytes:], redundancy_bytes, nil)
		if err < 0 {
			return OPUS_INTERNAL_ERROR
		}
		redundant_rng = celt_enc.rng
		celt_encoder_reset(celt_enc)
	}

	if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
		celtEncSetStartBand(celt_enc, start_band)
	}

	data[0] = 0
	if st.mode != MODE_SILK_ONLY {
		celtEncSetVBR(celt_enc, st.use_vbr)
		if st.mode == MODE_HYBRID {
			if st.use_vbr != 0 {
				celtEncSetBitrate(celt_enc, st.bitrate_bps-st.silk_mode.bitRate)
				celtEncSetVBRConstraint(celt_enc, 0)
			}
		} else {
			if st.use_vbr != 0 {
				celtEncSetVBR(celt_enc, 1)
				celtEncSetVBRConstraint(celt_enc, st.vbr_constraint)
				celtEncSetBitrate(celt_enc, st.bitrate_bps)
			}
		}
		if st.mode != st.prev_mode && st.prev_mode > 0 &&
			st.application != OPUS_APPLICATION_RESTRICTED_CELT {
			var dummy [2]byte
			celt_encoder_reset(celt_enc)
			// Prefilling.
			celt_encode_with_ec(celt_enc, tmp_prefill, int(st.Fs)/400, dummy[:], 2, nil)
			celtEncSetPrediction(celt_enc, 0)
		}
		// If false, we already busted the budget and we'll end up with a "PLC frame".
		if ec_tell(&enc) <= 8*nb_compr_bytes {
			ret = celt_encode_with_ec(celt_enc, pcm_buf, frame_size, nil, nb_compr_bytes, &enc)
			if ret < 0 {
				return OPUS_INTERNAL_ERROR
			}
			// Put CELT->SILK redundancy data in the right place.
			if redundancy != 0 && celt_to_silk != 0 && st.mode == MODE_HYBRID && nb_compr_bytes != ret {
				OPUS_MOVE(data1[ret:], data1[nb_compr_bytes:], redundancy_bytes)
				nb_compr_bytes = ret + redundancy_bytes
			}
		}
		st.rangeFinal = celt_enc.rng
	} else {
		st.rangeFinal = enc.rng
	}

	// 5 ms redundant frame for SILK->CELT.
	if redundancy != 0 && celt_to_silk == 0 {
		var err int
		var dummy [2]byte
		var N2, N4 int
		N2 = int(st.Fs) / 200
		N4 = int(st.Fs) / 400

		celt_encoder_reset(celt_enc)
		celtEncSetStartBand(celt_enc, 0)
		celtEncSetPrediction(celt_enc, 0)
		celtEncSetVBR(celt_enc, 0)
		celtEncSetBitrate(celt_enc, OPUS_BITRATE_MAX)

		if st.mode == MODE_HYBRID {
			// Shrink packet to what the encoder actually used.
			nb_compr_bytes = ret
			ec_enc_shrink(&enc, opus_uint32(nb_compr_bytes))
		}
		celt_encode_with_ec(celt_enc, pcm_buf[st.channels*(frame_size-N2-N4):], N4, dummy[:], 2, nil)

		err = celt_encode_with_ec(celt_enc, pcm_buf[st.channels*(frame_size-N2):], N2,
			data1[nb_compr_bytes:], redundancy_bytes, nil)
		if err < 0 {
			return OPUS_INTERNAL_ERROR
		}
		redundant_rng = celt_enc.rng
	}

	// Signalling the mode in the first byte (data-- in C; here restore dataOff).
	data[0] |= gen_toc(st.mode, int(st.Fs)/frame_size, curr_bandwidth, st.stream_channels)

	st.rangeFinal ^= redundant_rng

	if to_celt != 0 {
		st.prev_mode = MODE_CELT_ONLY
	} else {
		st.prev_mode = st.mode
	}
	st.prev_channels = st.stream_channels
	st.prev_framesize = frame_size

	st.first = 0

	// DTX decision.
	if st.use_dtx != 0 && st.silk_mode.useDTX == 0 {
		if decide_dtx_mode(activity, &st.nb_no_activity_ms_Q1, 2*1000*frame_size/int(st.Fs)) != 0 {
			st.rangeFinal = 0
			data[0] = gen_toc(st.mode, int(st.Fs)/frame_size, curr_bandwidth, st.stream_channels)
			return 1
		}
	} else {
		st.nb_no_activity_ms_Q1 = 0
	}

	// In the unlikely case that the SILK encoder busted its target, tell
	// the decoder to call the PLC.
	if ec_tell(&enc) > (int(max_data_bytes)-1)*8 {
		if max_data_bytes < 2 {
			return OPUS_BUFFER_TOO_SMALL
		}
		data[1] = 0
		ret = 1
		st.rangeFinal = 0
	} else if st.mode == MODE_SILK_ONLY && redundancy == 0 {
		// LPC-only mode may strip trailing zero bytes — the decoder
		// fills them in. Can't do this for MDCT modes (actual length
		// matters for allocation).
		for ret > 2 && data1[ret-1] == 0 {
			ret--
		}
	}
	// Count ToC and redundancy.
	ret += 1 + redundancy_bytes
	if st.use_vbr != 0 {
		apply_padding = 0
	} else {
		apply_padding = 1
	}
	_ = first_frame
	if apply_padding != 0 {
		if opus_packet_pad(data, opus_int32(ret), orig_max_data_bytes) != OPUS_OK {
			return OPUS_INTERNAL_ERROR
		}
		ret = int(orig_max_data_bytes)
	}
	return opus_int32(ret)
}

// ─────────────────────────────────────────────────────────────────────
// 9f-F: opus_encode_native + opus_encode + opus_encode_float
// C: opus_encoder.c:1182-1852, 2671-2766
// ─────────────────────────────────────────────────────────────────────
//
// opus_encode_native is the top-level dispatcher. It:
//   - Runs digital-silence detection + optional analysis
//   - Tracks peak signal energy + stereo width
//   - Computes equivalent-rate; picks mono/stereo and SILK/CELT mode
//   - Chooses bandwidth via interpolated thresholds (with hysteresis)
//   - Forwards frame-size to the inner frame encoder
//
// DRED / RES24 / FUZZING / FIXED_POINT branches are omitted (float build).
//
// Every `a ± b*c` expression routes through fma_add/fma_sub. The Q15
// mode-threshold interpolation at L1496-L1499 is expressed as an integer
// operation on opus_int32 in C (the table values are opus_int32), so
// there is no FP fma to wrap — it's a plain int32 computation.

// opus_encode_native — C: opus_encoder.c:1182-1852.
//
// pcm is the caller's converted opus_res buffer (for int16/float API the
// wrappers above convert; for the float-native branch the caller passes
// the raw float32 buffer). analysis_pcm is the *original* input buffer
// used by the tonality analyser (may be int16 or float32); analysis_size
// is the original analysis_frame_size (unmodified by frame_size_select).
func opus_encode_native(
	st *OpusEncoder,
	pcm []opus_res,
	frame_size int,
	data []byte,
	out_data_bytes opus_int32,
	lsb_depth int,
	analysis_pcm interface{},
	analysis_size opus_int32,
	c1, c2, analysis_channels int,
	downmix downmix_func,
	float_api int,
) opus_int32 {
	var silk_enc *silk_encoder
	var celt_enc *OpusCustomEncoder
	var i int
	var ret int = 0
	var prefill int = 0
	var redundancy int = 0
	var celt_to_silk int = 0
	var to_celt int = 0
	var voice_est int
	var equiv_rate opus_int32
	var frame_rate int
	var max_rate opus_int32
	var curr_bandwidth int
	var max_data_bytes opus_int32
	var cbr_bytes opus_int32 = -1
	var stereo_width opus_val16
	var celt_mode *OpusCustomMode = nil
	var packet_size_cap int = 1276
	var analysis_info AnalysisInfo
	analysis_info.valid = 0
	var analysis_read_pos_bak int = -1
	var analysis_read_subframe_bak int = -1
	var is_silence int = 0

	// Avoid insane packet sizes here; real bounds applied later.
	maxCap := opus_int32(packet_size_cap * 6)
	if maxCap < out_data_bytes {
		max_data_bytes = maxCap
	} else {
		max_data_bytes = out_data_bytes
	}

	st.rangeFinal = 0
	if frame_size <= 0 || max_data_bytes <= 0 {
		return OPUS_BAD_ARG
	}

	// Cannot encode 100 ms in 1 byte.
	if max_data_bytes == 1 && st.Fs == opus_int32(frame_size*10) {
		return OPUS_BUFFER_TOO_SMALL
	}

	if st.application != OPUS_APPLICATION_RESTRICTED_CELT {
		silk_enc = st.silk_enc
	}
	if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
		celt_enc = st.celt_enc
	}
	_ = silk_enc

	if lsb_depth > st.lsb_depth {
		lsb_depth = st.lsb_depth
	}

	if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
		// C: celt_encoder_ctl(celt_enc, CELT_GET_MODE(&celt_mode))
		celt_mode = celt_enc.mode
	}
	is_silence = is_digital_silence(pcm, frame_size, st.channels, lsb_depth)

	// float-build analysis gate: complexity >= 7 && 16k <= Fs <= 48k && !RESTRICTED_SILK.
	if st.silk_mode.complexity >= 7 && st.Fs >= 16000 && st.Fs <= 48000 &&
		st.application != OPUS_APPLICATION_RESTRICTED_SILK {
		analysis_read_pos_bak = st.analysis.read_pos
		analysis_read_subframe_bak = st.analysis.read_subframe
		run_analysis(&st.analysis, celt_mode, analysis_pcm, int(analysis_size), frame_size,
			c1, c2, analysis_channels, st.Fs,
			lsb_depth, downmix, &analysis_info)
	} else if st.analysis.initialized != 0 {
		tonality_analysis_reset(&st.analysis)
	}

	// Reset voice_ratio on non-silent frames.
	if is_silence == 0 {
		st.voice_ratio = -1
	}
	st.detected_bandwidth = 0
	if analysis_info.valid != 0 {
		var analysis_bandwidth int
		if st.signal_type == OPUS_AUTO {
			var prob float32
			if st.prev_mode == 0 {
				prob = analysis_info.music_prob
			} else if st.prev_mode == MODE_CELT_ONLY {
				prob = analysis_info.music_prob_max
			} else {
				prob = analysis_info.music_prob_min
			}
			// (int)floor(.5 + 100*(1-prob))
			st.voice_ratio = int(math.Floor(float64(fma_add(0.5, 100.0, sub_f32(1.0, prob)))))
		}
		analysis_bandwidth = analysis_info.bandwidth
		if analysis_bandwidth <= 12 {
			st.detected_bandwidth = OPUS_BANDWIDTH_NARROWBAND
		} else if analysis_bandwidth <= 14 {
			st.detected_bandwidth = OPUS_BANDWIDTH_MEDIUMBAND
		} else if analysis_bandwidth <= 16 {
			st.detected_bandwidth = OPUS_BANDWIDTH_WIDEBAND
		} else if analysis_bandwidth <= 18 {
			st.detected_bandwidth = OPUS_BANDWIDTH_SUPERWIDEBAND
		} else {
			st.detected_bandwidth = OPUS_BANDWIDTH_FULLBAND
		}
	}

	// Track the peak signal energy.
	if analysis_info.valid == 0 || analysis_info.activity_probability > DTX_ACTIVITY_THRESHOLD {
		if is_silence == 0 {
			e := compute_frame_energy(pcm, frame_size, st.channels, st.arch)
			// FP build: MULT16_32_Q15(0.999f, peak) = 0.999f * peak (no shift).
			decay := mul_f32(0.999, float32(st.peak_signal_energy))
			if decay > float32(e) {
				st.peak_signal_energy = opus_val32(decay)
			} else {
				st.peak_signal_energy = e
			}
		}
	}
	if st.channels == 2 && st.force_channels != 1 {
		stereo_width = compute_stereo_width(pcm, frame_size, st.Fs, &st.width_mem)
	} else {
		stereo_width = 0
	}
	st.bitrate_bps = user_bitrate_to_bitrate(st, frame_size, int(max_data_bytes))

	frame_rate = int(st.Fs) / frame_size
	if st.use_vbr == 0 {
		bits := bitrate_to_bits(st.bitrate_bps, st.Fs, opus_int32(frame_size))
		cbr_bytes = (bits + 4) / 8
		if max_data_bytes < cbr_bytes {
			cbr_bytes = max_data_bytes
		}
		st.bitrate_bps = bits_to_bitrate(cbr_bytes*8, st.Fs, opus_int32(frame_size))
		// At least one byte.
		if cbr_bytes < 1 {
			max_data_bytes = 1
		} else {
			max_data_bytes = cbr_bytes
		}
	}
	if max_data_bytes < 3 || st.bitrate_bps < opus_int32(3*frame_rate*8) ||
		(frame_rate < 50 && (int(max_data_bytes)*frame_rate < 300 || st.bitrate_bps < 2400)) {
		// Emit 'PLC' frames in too-low-bandwidth cases.
		tocmode := st.mode
		var bw int
		if st.bandwidth == 0 {
			bw = OPUS_BANDWIDTH_NARROWBAND
		} else {
			bw = st.bandwidth
		}
		packet_code := 0
		num_multiframes := 0

		if tocmode == 0 {
			tocmode = MODE_SILK_ONLY
		}
		if frame_rate > 100 {
			tocmode = MODE_CELT_ONLY
		}
		if frame_rate == 25 && tocmode != MODE_SILK_ONLY {
			frame_rate = 50
			packet_code = 1
		}
		if frame_rate <= 16 {
			if out_data_bytes == 1 || (tocmode == MODE_SILK_ONLY && frame_rate != 10) {
				tocmode = MODE_SILK_ONLY
				if frame_rate <= 12 {
					packet_code = 1
				} else {
					packet_code = 0
				}
				if frame_rate == 12 {
					frame_rate = 25
				} else {
					frame_rate = 16
				}
			} else {
				num_multiframes = 50 / frame_rate
				frame_rate = 50
				packet_code = 3
			}
		}
		if tocmode == MODE_SILK_ONLY && bw > OPUS_BANDWIDTH_WIDEBAND {
			bw = OPUS_BANDWIDTH_WIDEBAND
		} else if tocmode == MODE_CELT_ONLY && bw == OPUS_BANDWIDTH_MEDIUMBAND {
			bw = OPUS_BANDWIDTH_NARROWBAND
		} else if tocmode == MODE_HYBRID && bw <= OPUS_BANDWIDTH_SUPERWIDEBAND {
			bw = OPUS_BANDWIDTH_SUPERWIDEBAND
		}

		data[0] = gen_toc(tocmode, frame_rate, bw, st.stream_channels)
		data[0] |= byte(packet_code)

		if packet_code <= 1 {
			ret = 1
		} else {
			ret = 2
		}
		if int(max_data_bytes) < ret {
			max_data_bytes = opus_int32(ret)
		}

		if packet_code == 3 {
			data[1] = byte(num_multiframes)
		}

		if st.use_vbr == 0 {
			padRet := opus_packet_pad(data, opus_int32(ret), max_data_bytes)
			if padRet == OPUS_OK {
				ret = int(max_data_bytes)
			} else {
				ret = OPUS_INTERNAL_ERROR
			}
		}
		return opus_int32(ret)
	}
	max_rate = bits_to_bitrate(max_data_bytes*8, st.Fs, opus_int32(frame_size))

	// Equivalent 20-ms rate for mode/channel/bandwidth decisions.
	equiv_rate = compute_equiv_rate(st.bitrate_bps, st.channels, int(st.Fs)/frame_size,
		st.use_vbr, 0, st.silk_mode.complexity, st.silk_mode.packetLossPercentage)

	if st.signal_type == OPUS_SIGNAL_VOICE {
		voice_est = 127
	} else if st.signal_type == OPUS_SIGNAL_MUSIC {
		voice_est = 0
	} else if st.voice_ratio >= 0 {
		voice_est = st.voice_ratio * 327 >> 8
		if st.application == OPUS_APPLICATION_AUDIO {
			if voice_est > 115 {
				voice_est = 115
			}
		}
	} else if st.application == OPUS_APPLICATION_VOIP {
		voice_est = 115
	} else {
		voice_est = 48
	}

	if st.force_channels != OPUS_AUTO && st.channels == 2 {
		st.stream_channels = st.force_channels
	} else {
		// Rate-dependent mono-stereo decision.
		if st.channels == 2 {
			var stereo_threshold opus_int32
			stereo_threshold = stereo_music_threshold +
				opus_int32((voice_est*voice_est*int(stereo_voice_threshold-stereo_music_threshold))>>14)
			if st.stream_channels == 2 {
				stereo_threshold -= 1000
			} else {
				stereo_threshold += 1000
			}
			if equiv_rate > stereo_threshold {
				st.stream_channels = 2
			} else {
				st.stream_channels = 1
			}
		} else {
			st.stream_channels = st.channels
		}
	}
	// Update equivalent rate for channels decision.
	equiv_rate = compute_equiv_rate(st.bitrate_bps, st.stream_channels, int(st.Fs)/frame_size,
		st.use_vbr, 0, st.silk_mode.complexity, st.silk_mode.packetLossPercentage)

	// SILK DTX pass-through: allow when use_dtx but the generalized DTX can't fire.
	if st.use_dtx != 0 && !(analysis_info.valid != 0 || is_silence != 0) {
		st.silk_mode.useDTX = 1
	} else {
		st.silk_mode.useDTX = 0
	}

	// Mode selection.
	if st.application == OPUS_APPLICATION_RESTRICTED_SILK {
		st.mode = MODE_SILK_ONLY
	} else if st.application == OPUS_APPLICATION_RESTRICTED_LOWDELAY ||
		st.application == OPUS_APPLICATION_RESTRICTED_CELT {
		st.mode = MODE_CELT_ONLY
	} else if st.user_forced_mode == OPUS_AUTO {
		var mode_voice, mode_music opus_int32
		var threshold opus_int32

		// Interpolate based on stereo width.
		// C (FP build): MULT16_32_Q15(a,b) = a*b (no shift). Q15ONE=1.0f.
		//
		//   mode_voice = (opus_int32)((1-sw)*mode_thresholds[0][0] + sw*mode_thresholds[1][0])
		//   mode_music = (opus_int32)((1-sw)*mode_thresholds[1][1] + sw*mode_thresholds[1][1])   (C quirk, L1498-1499)
		//
		// The outer cast truncates toward zero. Each `a + b*c` routes
		// through fma_add for -ffp-contract=off parity.
		oneMinus := sub_f32(float32(Q15ONE), float32(stereo_width))
		mv0 := mul_f32(oneMinus, float32(mode_thresholds[0][0]))
		mode_voice = opus_int32(fma_add(mv0, float32(stereo_width), float32(mode_thresholds[1][0])))
		mm0 := mul_f32(oneMinus, float32(mode_thresholds[1][1]))
		mode_music = opus_int32(fma_add(mm0, float32(stereo_width), float32(mode_thresholds[1][1])))

		threshold = mode_music + opus_int32((voice_est*voice_est*int(mode_voice-mode_music))>>14)
		if st.application == OPUS_APPLICATION_VOIP {
			threshold += 8000
		}
		// Hysteresis.
		if st.prev_mode == MODE_CELT_ONLY {
			threshold -= 4000
		} else if st.prev_mode > 0 {
			threshold += 4000
		}
		if equiv_rate >= threshold {
			st.mode = MODE_CELT_ONLY
		} else {
			st.mode = MODE_SILK_ONLY
		}
		// FEC->SILK; DTX->SILK for voice.
		if st.silk_mode.useInBandFEC != 0 &&
			int(st.silk_mode.packetLossPercentage) > (128-voice_est)>>4 &&
			(st.fec_config != 2 || voice_est > 25) {
			st.mode = MODE_SILK_ONLY
		}
		if st.silk_mode.useDTX != 0 && voice_est > 100 {
			st.mode = MODE_SILK_ONLY
		}
		// Very low bitrate -> CELT-only.
		var switchRate int = 6000
		if frame_rate > 50 {
			switchRate = 9000
		}
		if int(max_data_bytes) < int(bitrate_to_bits(opus_int32(switchRate), st.Fs, opus_int32(frame_size)))/8 {
			st.mode = MODE_CELT_ONLY
		}
	} else {
		st.mode = st.user_forced_mode
	}

	// Override for small frame sizes.
	if st.mode != MODE_CELT_ONLY && frame_size < int(st.Fs)/100 {
		celt_assert(st.application != OPUS_APPLICATION_RESTRICTED_SILK)
		st.mode = MODE_CELT_ONLY
	}
	if st.lfe != 0 && st.application != OPUS_APPLICATION_RESTRICTED_SILK {
		st.mode = MODE_CELT_ONLY
	}

	if st.prev_mode > 0 &&
		((st.mode != MODE_CELT_ONLY && st.prev_mode == MODE_CELT_ONLY) ||
			(st.mode == MODE_CELT_ONLY && st.prev_mode != MODE_CELT_ONLY)) {
		redundancy = 1
		if st.mode != MODE_CELT_ONLY {
			celt_to_silk = 1
		} else {
			celt_to_silk = 0
		}
		if celt_to_silk == 0 {
			// Switch to SILK/hybrid if frame size is >= 10 ms.
			if frame_size >= int(st.Fs)/100 {
				st.mode = st.prev_mode
				to_celt = 1
			} else {
				redundancy = 0
			}
		}
	}

	// Delay stereo->mono transition.
	if st.stream_channels == 1 && st.prev_channels == 2 && st.silk_mode.toMono == 0 &&
		st.mode != MODE_CELT_ONLY && st.prev_mode != MODE_CELT_ONLY {
		st.silk_mode.toMono = 1
		st.stream_channels = 2
	} else {
		st.silk_mode.toMono = 0
	}

	// Update equivalent rate with mode decision.
	equiv_rate = compute_equiv_rate(st.bitrate_bps, st.stream_channels, int(st.Fs)/frame_size,
		st.use_vbr, st.mode, st.silk_mode.complexity, st.silk_mode.packetLossPercentage)

	if st.mode != MODE_CELT_ONLY && st.prev_mode == MODE_CELT_ONLY {
		var dummy silk_EncControlStruct
		silk_InitEncoder(silk_enc, opus_int(st.channels), st.arch, &dummy)
		prefill = 1
	}

	// Automatic bandwidth selection.
	if st.mode == MODE_CELT_ONLY || st.first != 0 || st.silk_mode.allowBandwidthSwitch != 0 {
		var voice_bandwidth_thresholds, music_bandwidth_thresholds []opus_int32
		var bandwidth_thresholds [8]opus_int32
		var bandwidth int = OPUS_BANDWIDTH_FULLBAND

		if st.channels == 2 && st.force_channels != 1 {
			voice_bandwidth_thresholds = stereo_voice_bandwidth_thresholds[:]
			music_bandwidth_thresholds = stereo_music_bandwidth_thresholds[:]
		} else {
			voice_bandwidth_thresholds = mono_voice_bandwidth_thresholds[:]
			music_bandwidth_thresholds = mono_music_bandwidth_thresholds[:]
		}
		for i = 0; i < 8; i++ {
			bandwidth_thresholds[i] = music_bandwidth_thresholds[i] +
				opus_int32((voice_est*voice_est*int(voice_bandwidth_thresholds[i]-music_bandwidth_thresholds[i]))>>14)
		}
		for {
			var threshold, hysteresis opus_int32
			threshold = bandwidth_thresholds[2*(bandwidth-OPUS_BANDWIDTH_MEDIUMBAND)]
			hysteresis = bandwidth_thresholds[2*(bandwidth-OPUS_BANDWIDTH_MEDIUMBAND)+1]
			if st.first == 0 {
				if st.auto_bandwidth >= bandwidth {
					threshold -= hysteresis
				} else {
					threshold += hysteresis
				}
			}
			if equiv_rate >= threshold {
				break
			}
			bandwidth--
			if bandwidth <= OPUS_BANDWIDTH_NARROWBAND {
				break
			}
		}
		if bandwidth == OPUS_BANDWIDTH_MEDIUMBAND {
			bandwidth = OPUS_BANDWIDTH_WIDEBAND
		}
		st.bandwidth = bandwidth
		st.auto_bandwidth = bandwidth
		// Prevent SWB/FB until SILK is fully in WB mode.
		if st.first == 0 && st.mode != MODE_CELT_ONLY &&
			st.silk_mode.inWBmodeWithoutVariableLP == 0 && st.bandwidth > OPUS_BANDWIDTH_WIDEBAND {
			st.bandwidth = OPUS_BANDWIDTH_WIDEBAND
		}
	}

	if st.bandwidth > st.max_bandwidth {
		st.bandwidth = st.max_bandwidth
	}
	if st.user_bandwidth != OPUS_AUTO {
		st.bandwidth = st.user_bandwidth
	}

	// Prevent hybrid at unsafe CBR/max rates.
	if st.mode != MODE_CELT_ONLY && max_rate < 15000 {
		if st.bandwidth > OPUS_BANDWIDTH_WIDEBAND {
			st.bandwidth = OPUS_BANDWIDTH_WIDEBAND
		}
	}

	// Clamp bandwidth by Fs.
	if st.Fs <= 24000 && st.bandwidth > OPUS_BANDWIDTH_SUPERWIDEBAND {
		st.bandwidth = OPUS_BANDWIDTH_SUPERWIDEBAND
	}
	if st.Fs <= 16000 && st.bandwidth > OPUS_BANDWIDTH_WIDEBAND {
		st.bandwidth = OPUS_BANDWIDTH_WIDEBAND
	}
	if st.Fs <= 12000 && st.bandwidth > OPUS_BANDWIDTH_MEDIUMBAND {
		st.bandwidth = OPUS_BANDWIDTH_MEDIUMBAND
	}
	if st.Fs <= 8000 && st.bandwidth > OPUS_BANDWIDTH_NARROWBAND {
		st.bandwidth = OPUS_BANDWIDTH_NARROWBAND
	}

	// Use detected bandwidth to reduce encoded bandwidth.
	if st.detected_bandwidth != 0 && st.user_bandwidth == OPUS_AUTO {
		var min_detected_bandwidth int
		if equiv_rate <= opus_int32(18000*st.stream_channels) && st.mode == MODE_CELT_ONLY {
			min_detected_bandwidth = OPUS_BANDWIDTH_NARROWBAND
		} else if equiv_rate <= opus_int32(24000*st.stream_channels) && st.mode == MODE_CELT_ONLY {
			min_detected_bandwidth = OPUS_BANDWIDTH_MEDIUMBAND
		} else if equiv_rate <= opus_int32(30000*st.stream_channels) {
			min_detected_bandwidth = OPUS_BANDWIDTH_WIDEBAND
		} else if equiv_rate <= opus_int32(44000*st.stream_channels) {
			min_detected_bandwidth = OPUS_BANDWIDTH_SUPERWIDEBAND
		} else {
			min_detected_bandwidth = OPUS_BANDWIDTH_FULLBAND
		}
		if st.detected_bandwidth < min_detected_bandwidth {
			st.detected_bandwidth = min_detected_bandwidth
		}
		if st.bandwidth > st.detected_bandwidth {
			st.bandwidth = st.detected_bandwidth
		}
	}

	st.silk_mode.LBRR_coded = opus_int(decide_fec(int(st.silk_mode.useInBandFEC), int(st.silk_mode.packetLossPercentage),
		int(st.silk_mode.LBRR_coded), st.mode, &st.bandwidth, equiv_rate))
	if st.application != OPUS_APPLICATION_RESTRICTED_SILK {
		// celt_encoder_ctl(celt_enc, OPUS_SET_LSB_DEPTH(lsb_depth))
		celt_enc.lsb_depth = lsb_depth
	}

	// CELT has no mediumband.
	if st.mode == MODE_CELT_ONLY && st.bandwidth == OPUS_BANDWIDTH_MEDIUMBAND {
		st.bandwidth = OPUS_BANDWIDTH_WIDEBAND
	}
	if st.lfe != 0 {
		st.bandwidth = OPUS_BANDWIDTH_NARROWBAND
	}

	curr_bandwidth = st.bandwidth

	if st.application == OPUS_APPLICATION_RESTRICTED_SILK && curr_bandwidth > OPUS_BANDWIDTH_WIDEBAND {
		st.bandwidth = OPUS_BANDWIDTH_WIDEBAND
		curr_bandwidth = OPUS_BANDWIDTH_WIDEBAND
	}
	// Never swap to/from CELT here.
	if st.mode == MODE_SILK_ONLY && curr_bandwidth > OPUS_BANDWIDTH_WIDEBAND {
		st.mode = MODE_HYBRID
	}
	if st.mode == MODE_HYBRID && curr_bandwidth <= OPUS_BANDWIDTH_WIDEBAND {
		st.mode = MODE_SILK_ONLY
	}

	// Multi-frame path: frame >60 ms (or >20 ms in non-SILK).
	if (frame_size > int(st.Fs)/50 && st.mode != MODE_SILK_ONLY) || frame_size > 3*int(st.Fs)/50 {
		var enc_frame_size, nb_frames int
		var max_header_bytes int
		var repacketize_len opus_int32
		var max_len_sum opus_int32
		var tot_size opus_int32 = 0
		var tmp_len int
		var dtx_count int = 0
		var bak_to_mono opus_int

		if st.mode == MODE_SILK_ONLY {
			if frame_size == 2*int(st.Fs)/25 {
				enc_frame_size = int(st.Fs) / 25
			} else if frame_size == 3*int(st.Fs)/25 {
				enc_frame_size = 3 * int(st.Fs) / 50
			} else {
				enc_frame_size = int(st.Fs) / 50
			}
		} else {
			enc_frame_size = int(st.Fs) / 50
		}
		nb_frames = frame_size / enc_frame_size

		if analysis_read_pos_bak != -1 {
			st.analysis.read_pos = analysis_read_pos_bak
			st.analysis.read_subframe = analysis_read_subframe_bak
		}

		if nb_frames == 2 {
			max_header_bytes = 3
		} else {
			max_header_bytes = 2 + (nb_frames-1)*2
		}

		if st.use_vbr != 0 || st.user_bitrate_bps == OPUS_BITRATE_MAX {
			repacketize_len = out_data_bytes
		} else {
			celt_assert(cbr_bytes >= 0)
			if cbr_bytes < out_data_bytes {
				repacketize_len = cbr_bytes
			} else {
				repacketize_len = out_data_bytes
			}
		}
		max_len_sum = opus_int32(nb_frames) + repacketize_len - opus_int32(max_header_bytes)

		tmp_data := make([]byte, max_len_sum)
		curr_off := 0
		var rp OpusRepacketizer
		opus_repacketizer_init(&rp)

		bak_to_mono = st.silk_mode.toMono
		if bak_to_mono != 0 {
			st.force_channels = 1
		} else {
			st.prev_channels = st.stream_channels
		}

		for i = 0; i < nb_frames; i++ {
			var first_frame int
			var frame_to_celt int
			var frame_redundancy int
			var curr_max opus_int32

			if i == 0 || i == dtx_count {
				first_frame = 1
			} else {
				first_frame = 0
			}
			st.silk_mode.toMono = 0
			if i < (nb_frames - 1) {
				st.nonfinal_frame = 1
			} else {
				st.nonfinal_frame = 0
			}

			if to_celt != 0 && i == nb_frames-1 {
				frame_to_celt = 1
			} else {
				frame_to_celt = 0
			}
			if redundancy != 0 && (frame_to_celt != 0 || (to_celt == 0 && i == 0)) {
				frame_redundancy = 1
			} else {
				frame_redundancy = 0
			}

			// curr_max = IMIN(bitrate_to_bits(bitrate, Fs, enc_frame_size)/8, max_len_sum/nb_frames)
			b1 := bitrate_to_bits(st.bitrate_bps, st.Fs, opus_int32(enc_frame_size)) / 8
			b2 := max_len_sum / opus_int32(nb_frames)
			if b1 < b2 {
				curr_max = b1
			} else {
				curr_max = b2
			}
			// curr_max = IMIN(max_len_sum-tot_size, curr_max)
			rem := max_len_sum - tot_size
			if rem < curr_max {
				curr_max = rem
			}

			if analysis_read_pos_bak != -1 {
				tonality_get_info(&st.analysis, &analysis_info, enc_frame_size)
			}
			pcmOff := i * st.channels * enc_frame_size
			is_silence = is_digital_silence(pcm[pcmOff:], enc_frame_size, st.channels, lsb_depth)

			var ai *AnalysisInfo
			if analysis_info.valid != 0 || analysis_read_pos_bak != -1 {
				ai = &analysis_info
			} else {
				ai = &analysis_info
			}

			tmp_len = int(opus_encode_frame_native(st, pcm[pcmOff:], enc_frame_size,
				tmp_data[curr_off:], curr_max, float_api, first_frame,
				ai, is_silence, frame_redundancy, celt_to_silk, prefill,
				equiv_rate, frame_to_celt))
			if tmp_len < 0 {
				return OPUS_INTERNAL_ERROR
			} else if tmp_len == 1 {
				dtx_count++
			}
			catRet := opus_repacketizer_cat(&rp, tmp_data[curr_off:curr_off+tmp_len], opus_int32(tmp_len))
			if catRet < 0 {
				return OPUS_INTERNAL_ERROR
			}
			tot_size += opus_int32(tmp_len)
			curr_off += tmp_len
		}
		var padVal int
		if st.use_vbr == 0 && dtx_count != nb_frames {
			padVal = 1
		} else {
			padVal = 0
		}
		ret = int(opus_repacketizer_out_range_impl(&rp, 0, nb_frames, data, repacketize_len, 0, padVal, nil, 0))
		if ret < 0 {
			ret = OPUS_INTERNAL_ERROR
		}
		st.silk_mode.toMono = bak_to_mono
		return opus_int32(ret)
	}

	// Single-frame path.
	var ai *AnalysisInfo = &analysis_info
	r := opus_encode_frame_native(st, pcm, frame_size, data, max_data_bytes, float_api, 1,
		ai, is_silence, redundancy, celt_to_silk, prefill,
		equiv_rate, to_celt)
	return r
}

// opus_encode — int16 PCM wrapper. C: opus_encoder.c:2671-2693.
func opus_encode(st *OpusEncoder, pcm []opus_int16, analysis_frame_size int,
	data []byte, max_data_bytes opus_int32) opus_int32 {
	frame_size := frame_size_select(st.application, opus_int32(analysis_frame_size), st.variable_duration, st.Fs)
	if frame_size <= 0 {
		return OPUS_BAD_ARG
	}
	in := make([]opus_res, int(frame_size)*st.channels)
	for i := 0; i < int(frame_size)*st.channels; i++ {
		in[i] = INT16TORES(pcm[i])
	}
	return opus_encode_native(st, in, int(frame_size), data, max_data_bytes, 16,
		pcm, opus_int32(analysis_frame_size), 0, -2, st.channels, downmix_int, 1)
}

// opus_encode_float — float32 PCM wrapper. C: opus_encoder.c:2735-2742.
func opus_encode_float(st *OpusEncoder, pcm []float32, analysis_frame_size int,
	data []byte, out_data_bytes opus_int32) opus_int32 {
	frame_size := frame_size_select(st.application, opus_int32(analysis_frame_size), st.variable_duration, st.Fs)
	// float-native branch does not copy; it passes pcm directly as opus_res
	// because opus_res == float32 in the float build.
	return opus_encode_native(st, pcm, int(frame_size), data, out_data_bytes, MAX_ENCODING_DEPTH,
		pcm, opus_int32(analysis_frame_size), 0, -2, st.channels, downmix_float, 1)
}
