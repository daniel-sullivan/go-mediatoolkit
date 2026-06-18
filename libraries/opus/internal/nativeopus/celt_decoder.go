package nativeopus

import "math"

// Port of libopus/celt/celt_decoder.c. Float-path only.
//
// Skipped / deferred:
//   - ENABLE_DEEP_PLC paths: sinc_filter, update_plc_state,
//     LPCNet-backed pitch PLC. Our config.h has ENABLE_DEEP_PLC off.
//   - ENABLE_QEXT: all qext_* handling, qext_oldBandE, decode_qext_stereo_params,
//     the qext payload parsing in celt_decode_with_ec_dred.
//   - CUSTOM_MODES / ENABLE_OPUS_CUSTOM_API: signalling-header parsing
//     (fromOpus/toOpus) and opus_custom_decoder_create/destroy.
//     We construct the decoder with a caller-supplied static mode.
//   - opus_custom_decoder_ctl: replaced with direct field setters
//     in the Go API surface (Reset, SetStartBand, etc.).
//
// All multiply-accumulate patterns route through fma_add/fma_sub so
// that Go's arm64 code generator emits the same separate FMUL+FADD
// sequence as the C oracle compiled with -ffp-contract=off.

// Error codes. C: opus_defines.h.
const (
	OPUS_OK               = 0
	OPUS_BAD_ARG          = -1
	OPUS_BUFFER_TOO_SMALL = -2
	OPUS_INTERNAL_ERROR   = -3
	OPUS_INVALID_PACKET   = -4
	OPUS_UNIMPLEMENTED    = -5
	OPUS_INVALID_STATE    = -6
	OPUS_ALLOC_FAIL       = -7
)

// PLC / decode-buffer constants. C: celt_decoder.c:62-82.
const (
	PLC_PITCH_LAG_MAX  = 720
	PLC_PITCH_LAG_MIN  = 100
	FRAME_NONE         = 0
	FRAME_NORMAL       = 1
	FRAME_PLC_NOISE    = 2
	FRAME_PLC_PERIODIC = 3
	FRAME_PLC_NEURAL   = 4
	FRAME_DRED         = 5

	DECODE_BUFFER_SIZE = DEC_PITCH_BUF_SIZE
)

// OpusCustomDecoder — Go port of the C struct. C: celt_decoder.c:87-139.
// The C struct uses a single trailing `_decode_mem[1]` array holding
// decode_mem + oldBandE + oldLogE + oldLogE2 + backgroundLogE + lpc
// layed out contiguously. We use discrete slices so each field is
// bounds-checked by the runtime.
type OpusCustomDecoder struct {
	mode            *OpusCustomMode
	overlap         int
	channels        int
	stream_channels int

	downsample  int
	start, end  int
	signalling  int
	disable_inv int
	complexity  int
	arch        int

	// Fields beyond this point are cleared on OPUS_RESET_STATE.
	rng                   opus_uint32
	error                 int
	last_pitch_index      int
	loss_duration         int
	plc_duration          int
	last_frame_type       int
	skip_plc              int
	postfilter_period     int
	postfilter_period_old int
	postfilter_gain       opus_val16
	postfilter_gain_old   opus_val16
	postfilter_tapset     int
	postfilter_tapset_old int
	prefilter_and_fold    int

	preemph_memD [2]celt_sig

	// decode_mem[c] spans DECODE_BUFFER_SIZE+overlap per channel.
	_decode_mem []celt_sig

	// Trailing arrays, each 2*nbEBands long.
	oldBandE       []celt_glog
	oldLogE        []celt_glog
	oldLogE2       []celt_glog
	backgroundLogE []celt_glog

	// LPC state, channels*CELT_LPC_ORDER.
	lpc []opus_val16

	// Per-frame scratch reused across calls. scratchFreq feeds
	// celt_synthesis' IMDCT accumulator; scratchDeemph is the
	// deemphasis filter's per-channel running buffer. Both are sized to
	// N (mode.shortMdctSize << LM) and overwritten each call.
	scratchFreq   []celt_sig
	scratchDeemph []celt_sig

	// Bit-allocation and band-quant scratch for celt_decode_with_ec.
	// All nbEBands-sized; contents are written each frame, no zeroing
	// required across calls. scratchX and scratchCollapseMasks cover
	// the per-frame normalised-band buffer and per-band collapse mask.
	scratchTfRes         []int
	scratchCap           []int
	scratchOffsets       []int
	scratchFineQuant     []int
	scratchPulses        []int
	scratchFinePriority  []int
	scratchX             []celt_norm
	scratchCollapseMasks []byte
	// Threaded into band_ctx during quant_all_bands.
	scratchNorm        []celt_norm
	scratchHadamardTmp []celt_norm
	scratchIy          []int
}

// celt_decoder_get_size — C: celt_decoder.c:185-193. The returned
// value is the serialized on-heap size of the C struct; Go allocates
// slices individually, so this mirrors the C calculation only for
// reference/compatibility.
func celt_decoder_get_size(channels int) int {
	// Assume standard 48 kHz mode (overlap=120, nbEBands=21).
	const overlap = 120
	const nbEBands = 21
	return channels*(DECODE_BUFFER_SIZE+overlap)*4 +
		4*2*nbEBands*4 +
		channels*CELT_LPC_ORDER*4
}

// celt_decoder_init — C: celt_decoder.c:228-244. The caller must
// supply the mode pointer (we don't ship static_modes_float.h — see
// Phase 11 note in modes.go).
func celt_decoder_init(st *OpusCustomDecoder, mode *OpusCustomMode,
	sampling_rate opus_int32, channels int) int {
	if ret := opus_custom_decoder_init(st, mode, channels); ret != OPUS_OK {
		return ret
	}
	st.downsample = resampling_factor(sampling_rate)
	if st.downsample == 0 {
		return OPUS_BAD_ARG
	}
	return OPUS_OK
}

// opus_custom_decoder_init — C: celt_decoder.c:246-279.
func opus_custom_decoder_init(st *OpusCustomDecoder, mode *OpusCustomMode, channels int) int {
	if channels < 0 || channels > 2 {
		return OPUS_BAD_ARG
	}
	if st == nil {
		return OPUS_ALLOC_FAIL
	}
	nb := mode.nbEBands
	*st = OpusCustomDecoder{
		mode:            mode,
		overlap:         mode.overlap,
		channels:        channels,
		stream_channels: channels,
		downsample:      1,
		start:           0,
		end:             mode.effEBands,
		signalling:      1,
		arch:            0,
		// ENABLE_UPDATE_DRAFT is on in our build.
		disable_inv: boolToInt(channels == 1),
	}
	st._decode_mem = make([]celt_sig, channels*(DECODE_BUFFER_SIZE+mode.overlap))
	st.oldBandE = make([]celt_glog, 2*nb)
	st.oldLogE = make([]celt_glog, 2*nb)
	st.oldLogE2 = make([]celt_glog, 2*nb)
	st.backgroundLogE = make([]celt_glog, 2*nb)
	st.lpc = make([]opus_val16, channels*CELT_LPC_ORDER)
	// Scratch buffers reused each frame — size to the mode's max N
	// (shortMdctSize << maxLM).
	maxN := mode.shortMdctSize << mode.maxLM
	st.scratchFreq = make([]celt_sig, maxN)
	st.scratchDeemph = make([]celt_sig, maxN)
	st.scratchTfRes = make([]int, nb)
	st.scratchCap = make([]int, nb)
	st.scratchOffsets = make([]int, nb)
	st.scratchFineQuant = make([]int, nb)
	st.scratchPulses = make([]int, nb)
	st.scratchFinePriority = make([]int, nb)
	// The per-frame band buffers below are indexed by C = stream_channels
	// (the channel count of the current packet), not by the decoder's
	// fixed `channels` (CC). A mono decoder may legitimately be handed a
	// stereo packet, in which case celt_decode_with_ec decodes C=2 stream
	// channels and celt_synthesis downmixes them to CC output channels;
	// libopus sizes these as runtime VLAs of C*N. stream_channels is at
	// most 2, so size for 2 channels regardless of `channels` to cover the
	// stereo-packet-into-mono-decoder case.
	maxC := 2
	st.scratchX = make([]celt_norm, maxC*maxN)
	st.scratchCollapseMasks = make([]byte, maxC*nb)
	// _norm in quant_all_bands is sized to C*(M*lastEBand - norm_offset);
	// hadamardTmp is sized to the per-band max (<= C*maxN); iy is max
	// N per band worst case. Use maxC*maxN as a safe upper bound.
	st.scratchNorm = make([]celt_norm, maxC*maxN)
	st.scratchHadamardTmp = make([]celt_norm, maxC*maxN)
	st.scratchIy = make([]int, maxC*maxN)
	celt_decoder_reset(st)
	return OPUS_OK
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// celt_decoder_reset — implements OPUS_RESET_STATE. C: celt_decoder.c:1794-1813.
func celt_decoder_reset(st *OpusCustomDecoder) {
	st.rng = 0
	st.error = 0
	st.last_pitch_index = 0
	st.loss_duration = 0
	st.plc_duration = 0
	st.last_frame_type = FRAME_NONE
	st.skip_plc = 1
	st.postfilter_period = 0
	st.postfilter_period_old = 0
	st.postfilter_gain = 0
	st.postfilter_gain_old = 0
	st.postfilter_tapset = 0
	st.postfilter_tapset_old = 0
	st.prefilter_and_fold = 0
	st.preemph_memD = [2]celt_sig{}
	for i := range st._decode_mem {
		st._decode_mem[i] = 0
	}
	for i := range st.oldBandE {
		st.oldBandE[i] = 0
	}
	for i := range st.oldLogE {
		st.oldLogE[i] = -GCONST(28.0)
	}
	for i := range st.oldLogE2 {
		st.oldLogE2[i] = -GCONST(28.0)
	}
	for i := range st.backgroundLogE {
		st.backgroundLogE[i] = 0
	}
	for i := range st.lpc {
		st.lpc[i] = 0
	}
}

// decodeMemChan returns the decode_mem slice for channel c.
func (st *OpusCustomDecoder) decodeMemChan(c int) []celt_sig {
	stride := DECODE_BUFFER_SIZE + st.overlap
	return st._decode_mem[c*stride : (c+1)*stride]
}

// deemphasis_stereo_simple — C: celt_decoder.c:292-316.
func deemphasis_stereo_simple(x0, x1 []celt_sig, pcm []opus_res, N int,
	coef0 opus_val16, mem *[2]celt_sig) {
	m0 := mem[0]
	m1 := mem[1]
	for j := 0; j < N; j++ {
		// Add VERY_SMALL to x[] first to reduce dependency chain.
		tmp0 := add_f32(add_f32(x0[j], VERY_SMALL), m0)
		tmp1 := add_f32(add_f32(x1[j], VERY_SMALL), m1)
		m0 = mul_f32(coef0, tmp0)
		m1 = mul_f32(coef0, tmp1)
		pcm[2*j] = SIG2RES(tmp0)
		pcm[2*j+1] = SIG2RES(tmp1)
	}
	mem[0] = m0
	mem[1] = m1
}

// deemphasis — C: celt_decoder.c:322-412 (float path, non-CUSTOM_MODES).
func deemphasis(in [][]celt_sig, pcm []opus_res, N, C, downsample int,
	coef []opus_val16, mem *[2]celt_sig, accum int, scratchBuf []celt_sig) {
	// Common case shortcut: stereo, no downsampling, no accumulation.
	if downsample == 1 && C == 2 && accum == 0 {
		deemphasis_stereo_simple(in[0], in[1], pcm, N, coef[0], mem)
		return
	}
	scratch := scratchBuf[:N]
	coef0 := coef[0]
	Nd := N / downsample
	for c := 0; c < C; c++ {
		apply_downsampling := false
		x := in[c]
		m := mem[c]
		if downsample > 1 {
			for j := 0; j < N; j++ {
				tmp := add_f32(add_f32(x[j], VERY_SMALL), m)
				m = mul_f32(coef0, tmp)
				scratch[j] = tmp
			}
			apply_downsampling = true
		} else {
			if accum != 0 {
				for j := 0; j < N; j++ {
					tmp := add_f32(add_f32(x[j], m), VERY_SMALL)
					m = mul_f32(coef0, tmp)
					pcm[j*C+c] = ADD_RES(pcm[j*C+c], SIG2RES(tmp))
				}
			} else {
				for j := 0; j < N; j++ {
					tmp := add_f32(add_f32(x[j], VERY_SMALL), m)
					m = mul_f32(coef0, tmp)
					pcm[j*C+c] = SIG2RES(tmp)
				}
			}
		}
		mem[c] = m

		if apply_downsampling {
			if accum != 0 {
				for j := 0; j < Nd; j++ {
					pcm[j*C+c] = ADD_RES(pcm[j*C+c], SIG2RES(scratch[j*downsample]))
				}
			} else {
				for j := 0; j < Nd; j++ {
					pcm[j*C+c] = SIG2RES(scratch[j*downsample])
				}
			}
		}
	}
}

// celt_synthesis — C: celt_decoder.c:417-515 (float, non-QEXT).
func celt_synthesis(mode *OpusCustomMode, X []celt_norm, out_syn [][]celt_sig,
	oldBandE []celt_glog, start, effEnd, C, CC, isTransient, LM, downsample,
	silence, arch int, scratchFreq []celt_sig) {
	overlap := mode.overlap
	nbEBands := mode.nbEBands
	N := mode.shortMdctSize << LM
	// Caller provides a length-N scratch buffer; zero the used prefix
	// because the original `make([]celt_sig, N)` implicitly did.
	freq := scratchFreq[:N]
	for i := range freq {
		freq[i] = 0
	}
	M := 1 << LM

	var B, NB, shift int
	if isTransient != 0 {
		B = M
		NB = mode.shortMdctSize
		shift = mode.maxLM
	} else {
		B = 1
		NB = mode.shortMdctSize << LM
		shift = mode.maxLM - LM
	}

	if CC == 2 && C == 1 {
		// Copy a mono stream to two channels.
		denormalise_bands(mode, X, freq, oldBandE, start, effEnd, M, downsample, silence)
		// Store a temporary copy because IMDCT destroys its input.
		freq2 := out_syn[1][overlap/2:]
		copy(freq2[:N], freq)
		for b := 0; b < B; b++ {
			clt_mdct_backward(&mode.mdct, freq2[b:], out_syn[0][NB*b:],
				mode.window, overlap, shift, B, arch)
		}
		for b := 0; b < B; b++ {
			clt_mdct_backward(&mode.mdct, freq[b:], out_syn[1][NB*b:],
				mode.window, overlap, shift, B, arch)
		}
	} else if CC == 1 && C == 2 {
		// Downmix a stereo stream to mono.
		freq2 := out_syn[0][overlap/2:]
		denormalise_bands(mode, X, freq, oldBandE, start, effEnd, M, downsample, silence)
		denormalise_bands(mode, X[N:], freq2, oldBandE[nbEBands:], start, effEnd, M, downsample, silence)
		for i := 0; i < N; i++ {
			freq[i] = add_f32(HALF32(freq[i]), HALF32(freq2[i]))
		}
		for b := 0; b < B; b++ {
			clt_mdct_backward(&mode.mdct, freq[b:], out_syn[0][NB*b:],
				mode.window, overlap, shift, B, arch)
		}
	} else {
		// Normal mono or stereo.
		for c := 0; c < CC; c++ {
			denormalise_bands(mode, X[c*N:], freq, oldBandE[c*nbEBands:],
				start, effEnd, M, downsample, silence)
			for b := 0; b < B; b++ {
				clt_mdct_backward(&mode.mdct, freq[b:], out_syn[c][NB*b:],
					mode.window, overlap, shift, B, arch)
			}
		}
	}
	// Saturate is identity in float mode; loop preserved for structural parity.
	for c := 0; c < CC; c++ {
		for i := 0; i < N; i++ {
			out_syn[c][i] = SATURATE(out_syn[c][i], SIG_SAT)
		}
	}
}

// tf_decode — C: celt_decoder.c:517-554.
func tf_decode(start, end, isTransient int, tf_res []int, LM int, dec *ec_dec) {
	budget := opus_uint32(dec.storage) * 8
	tell := opus_uint32(ec_tell(dec))
	logp := 4
	if isTransient != 0 {
		logp = 2
	}
	tf_select_rsv := 0
	if LM > 0 && tell+opus_uint32(logp)+1 <= budget {
		tf_select_rsv = 1
	}
	budget -= opus_uint32(tf_select_rsv)
	tf_changed := 0
	curr := 0
	for i := start; i < end; i++ {
		if tell+opus_uint32(logp) <= budget {
			curr ^= ec_dec_bit_logp(dec, logp)
			tell = opus_uint32(ec_tell(dec))
			tf_changed |= curr
		}
		tf_res[i] = curr
		logp = 5
		if isTransient != 0 {
			logp = 4
		}
	}
	tf_select := 0
	if tf_select_rsv != 0 &&
		tf_select_table[LM][4*isTransient+0+tf_changed] !=
			tf_select_table[LM][4*isTransient+2+tf_changed] {
		tf_select = ec_dec_bit_logp(dec, 1)
	}
	for i := start; i < end; i++ {
		tf_res[i] = int(tf_select_table[LM][4*isTransient+2*tf_select+tf_res[i]])
	}
}

// celt_plc_pitch_search — C: celt_decoder.c:556-578.
func celt_plc_pitch_search(st *OpusCustomDecoder, decode_mem [][]celt_sig, C, arch int) int {
	lp_pitch_buf := make([]opus_val16, DECODE_BUFFER_SIZE>>1)
	pitch_downsample(decode_mem, lp_pitch_buf, DECODE_BUFFER_SIZE>>1, C, 2, arch)
	var pitch_index int
	pitch_search(lp_pitch_buf[PLC_PITCH_LAG_MAX>>1:], lp_pitch_buf,
		DECODE_BUFFER_SIZE-PLC_PITCH_LAG_MAX,
		PLC_PITCH_LAG_MAX-PLC_PITCH_LAG_MIN, &pitch_index, arch)
	pitch_index = PLC_PITCH_LAG_MAX - pitch_index
	return pitch_index
}

// prefilter_and_fold — C: celt_decoder.c:580-625.
func prefilter_and_fold(st *OpusCustomDecoder, N int) {
	decode_buffer_size := DECODE_BUFFER_SIZE
	mode := st.mode
	overlap := st.overlap
	CC := st.channels
	etmp := make([]opus_val32, overlap)
	for c := 0; c < CC; c++ {
		dmem := st.decodeMemChan(c)
		// Pre-filter the MDCT overlap for the next frame.
		// xOff in the dmem-view = decode_buffer_size-N. But comb_filter
		// reads x[-T-2..], so callers must pass a pre-roll. Since dmem
		// already has history, we pass xOff = decode_buffer_size-N.
		comb_filter(etmp, 0, dmem, decode_buffer_size-N,
			st.postfilter_period_old, st.postfilter_period, overlap,
			-st.postfilter_gain_old, -st.postfilter_gain,
			st.postfilter_tapset_old, st.postfilter_tapset, nil, 0, st.arch)

		// Simulate TDAC on the concealed audio so that it blends with
		// the MDCT of the next frame.
		for i := 0; i < overlap/2; i++ {
			dmem[decode_buffer_size-N+i] =
				add_f32(
					mul_f32(mode.window[i], etmp[overlap-1-i]),
					mul_f32(mode.window[overlap-i-1], etmp[i]))
		}
	}
}

// celt_decode_lost — PLC. C: celt_decoder.c:679-1094 (non-QEXT,
// non-DEEP_PLC float path).
func celt_decode_lost(st *OpusCustomDecoder, N, LM int) {
	C := st.channels
	decode_buffer_size := DECODE_BUFFER_SIZE
	max_period := MAX_PERIOD
	mode := st.mode
	nbEBands := mode.nbEBands
	overlap := mode.overlap
	eBands := mode.eBands

	decode_mem := make([][]celt_sig, C)
	out_syn := make([][]celt_sig, C)
	for c := 0; c < C; c++ {
		decode_mem[c] = st.decodeMemChan(c)
		out_syn[c] = decode_mem[c][decode_buffer_size-N:]
	}
	oldBandE := st.oldBandE
	backgroundLogE := st.backgroundLogE
	lpc := st.lpc

	loss_duration := st.loss_duration
	start := st.start
	curr_frame_type := FRAME_PLC_PERIODIC
	if st.plc_duration >= 40 || start != 0 || st.skip_plc != 0 {
		curr_frame_type = FRAME_PLC_NOISE
	}

	if curr_frame_type == FRAME_PLC_NOISE {
		end := st.end
		effEnd := IMAX(start, IMIN(end, mode.effEBands))
		X := st.scratchX[:C*N]
		for c := 0; c < C; c++ {
			copy(decode_mem[c][:decode_buffer_size-N+overlap],
				decode_mem[c][N:N+decode_buffer_size-N+overlap])
		}
		if st.prefilter_and_fold != 0 {
			prefilter_and_fold(st, N)
		}
		// Energy decay.
		var decay celt_glog = GCONST(0.5)
		if loss_duration == 0 {
			decay = GCONST(1.5)
		}
		for c := 0; c < C; c++ {
			for i := start; i < end; i++ {
				oldBandE[c*nbEBands+i] = MAXG(backgroundLogE[c*nbEBands+i],
					oldBandE[c*nbEBands+i]-decay)
			}
		}
		seed := st.rng
		for c := 0; c < C; c++ {
			for i := start; i < effEnd; i++ {
				boffs := N*c + int(eBands[i])<<LM
				blen := int(eBands[i+1]-eBands[i]) << LM
				for j := 0; j < blen; j++ {
					seed = celt_lcg_rand(seed)
					X[boffs+j] = celt_norm(opus_int32(seed) >> 20)
				}
				renormalise_vector(X[boffs:], blen, opus_val32(Q31ONE), st.arch)
			}
		}
		st.rng = seed
		celt_synthesis(mode, X, out_syn, oldBandE, start, effEnd,
			C, C, 0, LM, st.downsample, 0, st.arch, st.scratchFreq)
		// Run the postfilter with the last parameters. comb_filter reads
		// x[xOff-T1-2..N-1], so we must pass the full decode_mem slice
		// with xOff set to the out_syn origin (decode_buffer_size-N) —
		// mirrors the C pointer `out_syn[c]` (which is decode_mem[c] +
		// decode_buffer_size - N) and the negative-index history access.
		for c := 0; c < C; c++ {
			st.postfilter_period = IMAX(st.postfilter_period, COMBFILTER_MINPERIOD)
			st.postfilter_period_old = IMAX(st.postfilter_period_old, COMBFILTER_MINPERIOD)
			comb_filter(decode_mem[c], decode_buffer_size-N, decode_mem[c], decode_buffer_size-N,
				st.postfilter_period_old, st.postfilter_period, mode.shortMdctSize,
				st.postfilter_gain_old, st.postfilter_gain,
				st.postfilter_tapset_old, st.postfilter_tapset,
				mode.window, overlap, st.arch)
			if LM != 0 {
				comb_filter(decode_mem[c], decode_buffer_size-N+mode.shortMdctSize,
					decode_mem[c], decode_buffer_size-N+mode.shortMdctSize,
					st.postfilter_period, st.postfilter_period, N-mode.shortMdctSize,
					st.postfilter_gain, st.postfilter_gain,
					st.postfilter_tapset, st.postfilter_tapset,
					mode.window, overlap, st.arch)
			}
		}
		st.postfilter_period_old = st.postfilter_period
		st.postfilter_gain_old = st.postfilter_gain
		st.postfilter_tapset_old = st.postfilter_tapset
		st.prefilter_and_fold = 0
		st.skip_plc = 1
	} else {
		// Pitch-based PLC.
		last_neural := false
		curr_neural := false
		var fade opus_val16 = Q15ONE
		var pitch_index int
		if st.last_frame_type != FRAME_PLC_PERIODIC && !(last_neural && curr_neural) {
			pitch_index = celt_plc_pitch_search(st, decode_mem, C, st.arch)
			st.last_pitch_index = pitch_index
		} else {
			pitch_index = st.last_pitch_index
			fade = 0.8
		}

		// We want excitation for 2 pitch periods but capped by MAX_PERIOD.
		exc_length := IMIN(2*pitch_index, max_period)
		_exc := make([]opus_val16, max_period+CELT_LPC_ORDER)
		fir_tmp := make([]opus_val16, exc_length)
		excBase := CELT_LPC_ORDER // exc = _exc+CELT_LPC_ORDER
		window := mode.window
		for c := 0; c < C; c++ {
			var S1 opus_val32 = 0
			buf := decode_mem[c]
			// exc[i - CELT_LPC_ORDER] = buf[decode_buffer_size - max_period - CELT_LPC_ORDER + i]
			for i := 0; i < max_period+CELT_LPC_ORDER; i++ {
				_exc[i] = opus_val16(buf[decode_buffer_size-max_period-CELT_LPC_ORDER+i])
			}

			if st.last_frame_type != FRAME_PLC_PERIODIC && !(last_neural && curr_neural) {
				ac := make([]opus_val32, CELT_LPC_ORDER+1)
				_celt_autocorr(_exc[excBase:], ac, window, overlap,
					CELT_LPC_ORDER, max_period, st.arch)
				// Noise floor of -40 dB. C: ac[0] *= 1.0001f.
				ac[0] = mul_f32(ac[0], 1.0001)
				// Lag windowing to stabilize Levinson-Durbin.
				// C: ac[i] -= ac[i]*(0.008f*0.008f)*i*i.
				// Two things to preserve exactly:
				//   (a) 0.008f*0.008f folds with *double* float32 rounding
				//       (round 0.008 to f32 then f32*f32 IEEE). NOT the
				//       untyped 0.008*0.008 in Go — those differ by 1 ULP
				//       (0x388637be vs 0x388637bd).
				//   (b) The expression runs THREE runtime multiplies:
				//       ((ac[i]*c008sq)*i)*i — not two like (ac[i]*c008sq)*(i*i).
				c008 := float32(0.008)
				c008sq := mul_f32(c008, c008)
				for i := 1; i <= CELT_LPC_ORDER; i++ {
					ifl := float32(i)
					t1 := mul_f32(ac[i], c008sq)
					t2 := mul_f32(t1, ifl)
					t3 := mul_f32(t2, ifl)
					ac[i] = sub_f32(ac[i], t3)
				}
				_celt_lpc(lpc[c*CELT_LPC_ORDER:], ac, CELT_LPC_ORDER)
			}
			// Compute the excitation for exc_length samples before the
			// loss. celt_fir() can't filter in-place so we need a copy.
			// C: celt_fir(exc+max_period-exc_length, lpc+c*LPC_ORDER,
			//             fir_tmp, exc_length, CELT_LPC_ORDER, arch);
			// Our Go celt_fir signature requires a buffer with ord-sample
			// prefix; supply _exc (which has CELT_LPC_ORDER prefix) at
			// the correct offset.
			celt_fir(_exc[excBase+max_period-exc_length-CELT_LPC_ORDER:],
				lpc[c*CELT_LPC_ORDER:], fir_tmp, exc_length, CELT_LPC_ORDER, st.arch)
			copy(_exc[excBase+max_period-exc_length:excBase+max_period], fir_tmp)

			// Check if the waveform is decaying.
			var decay opus_val16
			{
				var E1, E2 opus_val32 = 1, 1
				decay_length := exc_length >> 1
				for i := 0; i < decay_length; i++ {
					e := _exc[excBase+max_period-decay_length+i]
					E1 = fma_add(E1, e, e)
					e = _exc[excBase+max_period-2*decay_length+i]
					E2 = fma_add(E2, e, e)
				}
				E1 = MIN32(E1, E2)
				decay = celt_sqrt(frac_div32(E1, E2))
			}

			// Move the decoder memory one frame to the left.
			copy(buf[:decode_buffer_size-N], buf[N:N+decode_buffer_size-N])

			// Extrapolate.
			extrapolation_offset := max_period - pitch_index
			extrapolation_len := N + overlap
			attenuation := mul_f32(fade, decay)
			j := 0
			for i := 0; i < extrapolation_len; i++ {
				if j >= pitch_index {
					j -= pitch_index
					attenuation = mul_f32(attenuation, decay)
				}
				buf[decode_buffer_size-N+i] = celt_sig(
					mul_f32(attenuation, _exc[excBase+extrapolation_offset+j]))
				tmp := buf[decode_buffer_size-max_period-N+extrapolation_offset+j]
				S1 = fma_add(S1, tmp, tmp)
				j++
			}
			{
				lpc_mem := make([]opus_val16, CELT_LPC_ORDER)
				for i := 0; i < CELT_LPC_ORDER; i++ {
					lpc_mem[i] = opus_val16(buf[decode_buffer_size-N-1-i])
				}
				celt_iir(buf[decode_buffer_size-N:], lpc[c*CELT_LPC_ORDER:],
					buf[decode_buffer_size-N:], extrapolation_len, CELT_LPC_ORDER,
					lpc_mem, st.arch)
			}

			// Check if synthesis energy is higher than expected.
			{
				var S2 opus_val32 = 0
				for i := 0; i < extrapolation_len; i++ {
					tmp := buf[decode_buffer_size-N+i]
					S2 = fma_add(S2, tmp, tmp)
				}
				if !(S1 > 0.2*S2) {
					for i := 0; i < extrapolation_len; i++ {
						buf[decode_buffer_size-N+i] = 0
					}
				} else if S1 < S2 {
					ratio := celt_sqrt(frac_div32(S1+1, S2+1))
					for i := 0; i < overlap; i++ {
						tmp_g := sub_f32(Q15ONE, mul_f32(window[i], sub_f32(Q15ONE, ratio)))
						buf[decode_buffer_size-N+i] = mul_f32(tmp_g, buf[decode_buffer_size-N+i])
					}
					for i := overlap; i < extrapolation_len; i++ {
						buf[decode_buffer_size-N+i] = mul_f32(ratio, buf[decode_buffer_size-N+i])
					}
				}
			}
		}
		st.prefilter_and_fold = 1
	}

	// Saturate duration counters.
	st.loss_duration = IMIN(10000, loss_duration+(1<<LM))
	st.plc_duration = IMIN(10000, st.plc_duration+(1<<LM))
	st.last_frame_type = curr_frame_type
}

// celt_decode_with_ec — main entry. C: celt_decoder.c:1104-1617
// (non-QEXT, non-DEEP_PLC, non-CUSTOM_MODES-signalling).
func celt_decode_with_ec(st *OpusCustomDecoder, data []byte, length int,
	pcm []opus_res, frame_size int, dec *ec_dec, accum int) int {
	_ = math.Abs // keep import while we trim

	CC := st.channels
	mode := st.mode
	nbEBands := mode.nbEBands
	overlap := mode.overlap
	eBands := mode.eBands
	start := st.start
	end := st.end
	frame_size *= st.downsample

	oldBandE := st.oldBandE
	oldLogE := st.oldLogE
	oldLogE2 := st.oldLogE2
	backgroundLogE := st.backgroundLogE

	// Choose LM from frame_size.
	var LM int
	for LM = 0; LM <= mode.maxLM; LM++ {
		if mode.shortMdctSize<<LM == frame_size {
			break
		}
	}
	if LM > mode.maxLM {
		return OPUS_BAD_ARG
	}
	M := 1 << LM

	if length < 0 || length > 1275 || pcm == nil {
		return OPUS_BAD_ARG
	}

	N := M * mode.shortMdctSize
	C := st.stream_channels
	decode_buffer_size := DECODE_BUFFER_SIZE
	decode_mem := make([][]celt_sig, CC)
	out_syn := make([][]celt_sig, CC)
	for c := 0; c < CC; c++ {
		decode_mem[c] = st.decodeMemChan(c)
		out_syn[c] = decode_mem[c][decode_buffer_size-N:]
	}

	effEnd := end
	if effEnd > mode.effEBands {
		effEnd = mode.effEBands
	}

	if data == nil || length <= 1 {
		celt_decode_lost(st, N, LM)
		deemphasis(out_syn, pcm, N, CC, st.downsample, mode.preemph[:], &st.preemph_memD, accum, st.scratchDeemph)
		return frame_size / st.downsample
	}
	if st.loss_duration == 0 {
		st.skip_plc = 0
	}
	var _dec ec_dec
	if dec == nil {
		ec_dec_init(&_dec, data, opus_uint32(length))
		dec = &_dec
	}

	if C == 1 {
		for i := 0; i < nbEBands; i++ {
			oldBandE[i] = MAXG(oldBandE[i], oldBandE[nbEBands+i])
		}
	}

	total_bits := opus_int32(length * 8)
	tell := opus_int32(ec_tell(dec))

	var silence int
	if tell >= total_bits {
		silence = 1
	} else if tell == 1 {
		silence = ec_dec_bit_logp(dec, 15)
	} else {
		silence = 0
	}
	if silence != 0 {
		// Pretend we've read all the remaining bits.
		tell = opus_int32(length * 8)
		dec.nbits_total += int(tell) - ec_tell(dec)
	}

	var postfilter_gain opus_val16
	var postfilter_pitch int
	var postfilter_tapset int
	if start == 0 && tell+16 <= total_bits {
		if ec_dec_bit_logp(dec, 1) != 0 {
			octave := int(ec_dec_uint(dec, 6))
			postfilter_pitch = (16 << uint(octave)) + int(ec_dec_bits(dec, 4+octave)) - 1
			qg := int(ec_dec_bits(dec, 3))
			if opus_int32(ec_tell(dec))+2 <= total_bits {
				postfilter_tapset = ec_dec_icdf(dec, tapset_icdf[:], 2)
			}
			postfilter_gain = opus_val16(0.09375 * float32(qg+1))
		}
		tell = opus_int32(ec_tell(dec))
	}

	var isTransient, shortBlocks int
	if LM > 0 && tell+3 <= total_bits {
		isTransient = ec_dec_bit_logp(dec, 3)
		tell = opus_int32(ec_tell(dec))
	}
	if isTransient != 0 {
		shortBlocks = M
	}

	intra_ener := 0
	if tell+3 <= total_bits {
		intra_ener = ec_dec_bit_logp(dec, 3)
	}
	if intra_ener == 0 && st.loss_duration != 0 {
		for c := 0; c < 2; c++ {
			var safety celt_glog
			missing := IMIN(10, st.loss_duration>>LM)
			if LM == 0 {
				safety = GCONST(1.5)
			} else if LM == 1 {
				safety = GCONST(0.5)
			}
			for i := start; i < end; i++ {
				if oldBandE[c*nbEBands+i] < MAXG(oldLogE[c*nbEBands+i], oldLogE2[c*nbEBands+i]) {
					E0 := oldBandE[c*nbEBands+i]
					E1 := oldLogE[c*nbEBands+i]
					E2 := oldLogE2[c*nbEBands+i]
					slope := MAX32(E1-E0, HALF32(E2-E0))
					slope = MING(slope, GCONST(2.0))
					E0 -= MAX32(0, opus_val32(1+missing)*slope)
					oldBandE[c*nbEBands+i] = MAX32(-GCONST(20.0), E0)
				} else {
					oldBandE[c*nbEBands+i] = MING(MING(oldBandE[c*nbEBands+i], oldLogE[c*nbEBands+i]), oldLogE2[c*nbEBands+i])
				}
				oldBandE[c*nbEBands+i] -= safety
			}
		}
	}

	unquant_coarse_energy(mode, start, end, oldBandE, intra_ener, dec, C, LM)

	tf_res := st.scratchTfRes[:nbEBands]
	tf_decode(start, end, isTransient, tf_res, LM, dec)

	tell = opus_int32(ec_tell(dec))
	spread_decision := SPREAD_NORMAL
	if tell+4 <= total_bits {
		spread_decision = ec_dec_icdf(dec, spread_icdf[:], 5)
	}

	cap_ := st.scratchCap[:nbEBands]
	init_caps(mode, cap_, LM, C)
	offsets := st.scratchOffsets[:nbEBands]
	for i := range offsets {
		offsets[i] = 0
	}
	dynalloc_logp := 6
	total_bits <<= BITRES
	tellF := opus_int32(ec_tell_frac(dec))
	for i := start; i < end; i++ {
		width := C * int(eBands[i+1]-eBands[i]) << LM
		quanta := IMIN(width<<BITRES, IMAX(6<<BITRES, width))
		dynalloc_loop_logp := dynalloc_logp
		boost := 0
		for tellF+opus_int32(dynalloc_loop_logp<<BITRES) < total_bits && boost < cap_[i] {
			flag := ec_dec_bit_logp(dec, dynalloc_loop_logp)
			tellF = opus_int32(ec_tell_frac(dec))
			if flag == 0 {
				break
			}
			boost += quanta
			total_bits -= opus_int32(quanta)
			dynalloc_loop_logp = 1
		}
		offsets[i] = boost
		if boost > 0 {
			dynalloc_logp = IMAX(2, dynalloc_logp-1)
		}
	}

	fine_quant := st.scratchFineQuant[:nbEBands]
	alloc_trim := 5
	if tellF+(6<<BITRES) <= total_bits {
		alloc_trim = ec_dec_icdf(dec, trim_icdf[:], 7)
	}

	bits := opus_int32(length*8)<<BITRES - opus_int32(ec_tell_frac(dec)) - 1
	anti_collapse_rsv := 0
	if isTransient != 0 && LM >= 2 && bits >= opus_int32((LM+2)<<BITRES) {
		anti_collapse_rsv = 1 << BITRES
	}
	bits -= opus_int32(anti_collapse_rsv)

	pulses := st.scratchPulses[:nbEBands]
	fine_priority := st.scratchFinePriority[:nbEBands]
	var intensity, dual_stereo int
	var balance opus_int32
	codedBands := clt_compute_allocation(mode, start, end, offsets, cap_,
		alloc_trim, &intensity, &dual_stereo, bits, &balance, pulses,
		fine_quant, fine_priority, C, LM, dec, 0, 0, 0)

	unquant_fine_energy(mode, start, end, oldBandE, nil, fine_quant, dec, C)

	X := st.scratchX[:C*N]

	for c := 0; c < CC; c++ {
		copy(decode_mem[c][:decode_buffer_size-N+overlap],
			decode_mem[c][N:N+decode_buffer_size-N+overlap])
	}

	collapse_masks := st.scratchCollapseMasks[:C*nbEBands]
	for i := range collapse_masks {
		collapse_masks[i] = 0
	}
	var Y []celt_norm
	if C == 2 {
		Y = X[N:]
	}
	quant_all_bands(0, mode, start, end, X, Y, collapse_masks,
		nil, pulses, shortBlocks, spread_decision, dual_stereo, intensity, tf_res,
		opus_int32(length*(8<<BITRES))-opus_int32(anti_collapse_rsv), balance, dec,
		LM, codedBands, &st.rng, 0, st.arch, st.disable_inv,
		st.scratchNorm, st.scratchHadamardTmp, st.scratchIy)

	var anti_collapse_on int
	if anti_collapse_rsv > 0 {
		anti_collapse_on = int(ec_dec_bits(dec, 1))
	}
	unquant_energy_finalise(mode, start, end, oldBandE,
		fine_quant, fine_priority, length*8-ec_tell(dec), dec, C)
	if anti_collapse_on != 0 {
		anti_collapse(mode, X, collapse_masks, LM, C, N,
			start, end, oldBandE, oldLogE, oldLogE2, pulses, st.rng, 0, st.arch)
	}

	if silence != 0 {
		for i := 0; i < C*nbEBands; i++ {
			oldBandE[i] = -GCONST(28.0)
		}
	}
	if st.prefilter_and_fold != 0 {
		prefilter_and_fold(st, N)
	}
	celt_synthesis(mode, X, out_syn, oldBandE, start, effEnd,
		C, CC, isTransient, LM, st.downsample, silence, st.arch, st.scratchFreq)

	// See the matching call in celt_decode_lost for the xOff rationale:
	// comb_filter needs access to the pre-out_syn history, so we pass
	// decode_mem[c] with xOff = decode_buffer_size-N (plus shortMdctSize
	// on the second call). In C this is simply `out_syn[c]` (a raw
	// pointer into the middle of decode_mem); Go bounds-checks negative
	// indices on a sliced view so we reshape to (slice, offset).
	for c := 0; c < CC; c++ {
		st.postfilter_period = IMAX(st.postfilter_period, COMBFILTER_MINPERIOD)
		st.postfilter_period_old = IMAX(st.postfilter_period_old, COMBFILTER_MINPERIOD)
		comb_filter(decode_mem[c], decode_buffer_size-N, decode_mem[c], decode_buffer_size-N,
			st.postfilter_period_old, st.postfilter_period, mode.shortMdctSize,
			st.postfilter_gain_old, st.postfilter_gain,
			st.postfilter_tapset_old, st.postfilter_tapset,
			mode.window, overlap, st.arch)
		if LM != 0 {
			comb_filter(decode_mem[c], decode_buffer_size-N+mode.shortMdctSize,
				decode_mem[c], decode_buffer_size-N+mode.shortMdctSize,
				st.postfilter_period, postfilter_pitch, N-mode.shortMdctSize,
				st.postfilter_gain, postfilter_gain,
				st.postfilter_tapset, postfilter_tapset,
				mode.window, overlap, st.arch)
		}
	}
	st.postfilter_period_old = st.postfilter_period
	st.postfilter_gain_old = st.postfilter_gain
	st.postfilter_tapset_old = st.postfilter_tapset
	st.postfilter_period = postfilter_pitch
	st.postfilter_gain = postfilter_gain
	st.postfilter_tapset = postfilter_tapset
	if LM != 0 {
		st.postfilter_period_old = st.postfilter_period
		st.postfilter_gain_old = st.postfilter_gain
		st.postfilter_tapset_old = st.postfilter_tapset
	}

	if C == 1 {
		copy(oldBandE[nbEBands:2*nbEBands], oldBandE[:nbEBands])
	}

	if isTransient == 0 {
		copy(oldLogE2, oldLogE[:2*nbEBands])
		copy(oldLogE, oldBandE[:2*nbEBands])
	} else {
		for i := 0; i < 2*nbEBands; i++ {
			oldLogE[i] = MING(oldLogE[i], oldBandE[i])
		}
	}
	max_background_increase := celt_glog(IMIN(160, st.loss_duration+M)) * GCONST(0.001)
	for i := 0; i < 2*nbEBands; i++ {
		backgroundLogE[i] = MING(backgroundLogE[i]+max_background_increase, oldBandE[i])
	}
	for c := 0; c < 2; c++ {
		for i := 0; i < start; i++ {
			oldBandE[c*nbEBands+i] = 0
			oldLogE[c*nbEBands+i] = -GCONST(28.0)
			oldLogE2[c*nbEBands+i] = -GCONST(28.0)
		}
		for i := end; i < nbEBands; i++ {
			oldBandE[c*nbEBands+i] = 0
			oldLogE[c*nbEBands+i] = -GCONST(28.0)
			oldLogE2[c*nbEBands+i] = -GCONST(28.0)
		}
	}
	st.rng = dec.rng

	deemphasis(out_syn, pcm, N, CC, st.downsample, mode.preemph[:], &st.preemph_memD, accum, st.scratchDeemph)
	st.loss_duration = 0
	st.plc_duration = 0
	st.last_frame_type = FRAME_NORMAL
	st.prefilter_and_fold = 0
	if ec_tell(dec) > 8*length {
		return OPUS_INTERNAL_ERROR
	}
	if ec_get_error(dec) != 0 {
		st.error = 1
	}
	return frame_size / st.downsample
}
