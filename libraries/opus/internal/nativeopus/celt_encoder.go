package nativeopus

import "math"

// Port of libopus/celt/celt_encoder.c. Float-path only, non-QEXT.
//
// Skipped / deferred:
//   - ENABLE_QEXT: qext_mode, qext_bands, encode_qext_stereo_params,
//     extra_quant/extra_pulses, padding-len framing in the packet
//     header.
//   - CUSTOM_MODES / ENABLE_OPUS_CUSTOM_API: opus custom signalling
//     header (the "fromOpus/toOpus" round-trip) and the
//     opus_custom_encoder_create/destroy helpers.
//   - RESYNTH: not defined in our config.h. The re-synthesis path that
//     would call celt_synthesis + deemphasis inside the encoder is
//     skipped; we produce bitstream only.
//   - FUZZING: decorators for fuzzers.
//   - FIXED_POINT branches.
//   - opus_custom_encoder_ctl va-arg dispatcher (replaced with direct
//     field setters in the Go API).
//
// Multiply-accumulate patterns route through fma_add/fma_sub so Go's
// arm64 backend emits separate FMUL+FADD and matches the C oracle
// under -ffp-contract=off.

// OPUS_BITRATE_MAX — C: opus_defines.h:212.
const OPUS_BITRATE_MAX = -1

// OpusCustomEncoder — C: celt_encoder.c:63-142.
type OpusCustomEncoder struct {
	mode            *OpusCustomMode
	channels        int
	stream_channels int

	force_intra     int
	clip            int
	disable_pf      int
	complexity      int
	upsample        int
	start, end      int
	bitrate         opus_int32
	vbr             int
	signalling      int
	constrained_vbr int
	loss_rate       int
	lsb_depth       int
	lfe             int
	disable_inv     int
	arch            int

	// Reset-on-OPUS_RESET_STATE.
	rng              opus_uint32
	spread_decision  int
	delayedIntra     opus_val32
	tonal_average    int
	lastCodedBands   int
	hf_average       int
	tapset_decision  int
	prefilter_period int
	prefilter_gain   opus_val16
	prefilter_tapset int
	consec_transient int
	analysis         AnalysisInfo
	silk_info        SILKInfo
	preemph_memE     [2]opus_val32
	preemph_memD     [2]opus_val32
	vbr_reservoir    opus_int32
	vbr_drift        opus_int32
	vbr_offset       opus_int32
	vbr_count        opus_int32
	overlap_max      opus_val32
	stereo_saving    opus_val16
	intensity        int
	energy_mask      []celt_glog
	spec_avg         celt_glog

	// Trailing arrays laid out as discrete slices.
	in_mem        []celt_sig // channels*overlap
	prefilter_mem []celt_sig // channels*COMBFILTER_MAXPERIOD
	oldBandE      []celt_glog
	oldLogE       []celt_glog
	oldLogE2      []celt_glog
	energyError   []celt_glog

	// Per-frame scratch for celt_encode_with_ec. Sized once at init:
	// signal/freq/norm buffers to channels*max_N, bit-allocation
	// buffers to nbEBands. Contents are overwritten each call.
	scratchIn               []celt_sig
	scratchFreq             []celt_sig
	scratchBandE            []celt_ener
	scratchBandLogE         []celt_glog
	scratchBandLogE2        []celt_glog
	scratchSurroundDynalloc []celt_glog
	scratchErrorArr         []celt_glog
	scratchX                []celt_norm
	scratchCollapseMasks    []byte
	scratchOffsets          []int
	scratchImportance       []int
	scratchSpreadWeight     []int
	scratchTfRes            []int
	scratchCap              []int
	scratchFineQuant        []int
	scratchPulses           []int
	scratchFinePriority     []int

	// run_prefilter scratch: pre-buffer (CC*(N+COMBFILTER_MAXPERIOD))
	// and pitch-search downsampled buffer ((N+max_period)/2).
	scratchPre      []celt_sig
	scratchPitchBuf []opus_val16
	// Threaded into band_ctx during quant_all_bands.
	scratchNorm        []celt_norm
	scratchHadamardTmp []celt_norm
	scratchIy          []int

	// transient_analysis / tone_detect tmp buffer, sized to N+overlap.
	scratchTransient []opus_val16
}

// celt_encoder_get_size — C: celt_encoder.c:147-155.
//
// Returns a byte count matching the C arena layout for API
// compatibility. Go allocates a native struct regardless, so the
// value is symbolic (1) and is not consumed by any Go caller.
func celt_encoder_get_size(channels int) int {
	_ = channels
	return 1
}

// celt_encoder_init — C: celt_encoder.c:243-260.
func celt_encoder_init(st *OpusCustomEncoder, mode *OpusCustomMode,
	sampling_rate opus_int32, channels, arch int) int {
	if ret := opus_custom_encoder_init_arch(st, mode, channels, arch); ret != OPUS_OK {
		return ret
	}
	st.upsample = resampling_factor(sampling_rate)
	return OPUS_OK
}

// opus_custom_encoder_init_arch — C: celt_encoder.c:197-234.
func opus_custom_encoder_init_arch(st *OpusCustomEncoder, mode *OpusCustomMode,
	channels, arch int) int {
	if channels < 0 || channels > 2 {
		return OPUS_BAD_ARG
	}
	if st == nil || mode == nil {
		return OPUS_ALLOC_FAIL
	}
	nb := mode.nbEBands
	*st = OpusCustomEncoder{
		mode:            mode,
		channels:        channels,
		stream_channels: channels,
		upsample:        1,
		start:           0,
		end:             mode.effEBands,
		signalling:      1,
		arch:            arch,
		constrained_vbr: 1,
		clip:            1,
		bitrate:         OPUS_BITRATE_MAX,
		complexity:      5,
		lsb_depth:       24,
	}
	st.in_mem = make([]celt_sig, channels*mode.overlap)
	st.prefilter_mem = make([]celt_sig, channels*COMBFILTER_MAXPERIOD)
	st.oldBandE = make([]celt_glog, channels*nb)
	st.oldLogE = make([]celt_glog, channels*nb)
	st.oldLogE2 = make([]celt_glog, channels*nb)
	st.energyError = make([]celt_glog, channels*nb)
	// Per-frame scratch sized to the mode's max N (shortMdctSize<<maxLM).
	maxN := mode.shortMdctSize << mode.maxLM
	st.scratchIn = make([]celt_sig, channels*(maxN+mode.overlap))
	st.scratchFreq = make([]celt_sig, channels*maxN)
	st.scratchBandE = make([]celt_ener, nb*channels)
	st.scratchBandLogE = make([]celt_glog, nb*channels)
	st.scratchBandLogE2 = make([]celt_glog, channels*nb)
	st.scratchSurroundDynalloc = make([]celt_glog, channels*nb)
	st.scratchErrorArr = make([]celt_glog, channels*nb)
	st.scratchX = make([]celt_norm, channels*maxN)
	st.scratchCollapseMasks = make([]byte, channels*nb)
	st.scratchOffsets = make([]int, nb)
	st.scratchImportance = make([]int, nb)
	st.scratchSpreadWeight = make([]int, nb)
	st.scratchTfRes = make([]int, nb)
	st.scratchCap = make([]int, nb)
	st.scratchFineQuant = make([]int, nb)
	st.scratchPulses = make([]int, nb)
	st.scratchFinePriority = make([]int, nb)
	st.scratchPre = make([]celt_sig, channels*(maxN+COMBFILTER_MAXPERIOD))
	st.scratchPitchBuf = make([]opus_val16, (COMBFILTER_MAXPERIOD+maxN)>>1)
	st.scratchNorm = make([]celt_norm, channels*maxN)
	st.scratchHadamardTmp = make([]celt_norm, channels*maxN)
	st.scratchIy = make([]int, channels*maxN)
	st.scratchTransient = make([]opus_val16, maxN+mode.overlap)
	celt_encoder_reset(st)
	return OPUS_OK
}

// celt_encoder_reset — mirror of OPUS_RESET_STATE on the encoder.
func celt_encoder_reset(st *OpusCustomEncoder) {
	st.rng = 0
	st.spread_decision = SPREAD_NORMAL
	st.delayedIntra = 1
	st.tonal_average = 256
	st.lastCodedBands = 0
	st.hf_average = 0
	st.tapset_decision = 0
	st.prefilter_period = 0
	st.prefilter_gain = 0
	st.prefilter_tapset = 0
	st.consec_transient = 0
	st.analysis = AnalysisInfo{}
	st.silk_info = SILKInfo{}
	st.preemph_memE = [2]opus_val32{}
	st.preemph_memD = [2]opus_val32{}
	st.vbr_reservoir = 0
	st.vbr_drift = 0
	st.vbr_offset = 0
	st.vbr_count = 0
	st.overlap_max = 0
	st.stereo_saving = 0
	st.intensity = 0
	st.spec_avg = 0
	for i := range st.in_mem {
		st.in_mem[i] = 0
	}
	for i := range st.prefilter_mem {
		st.prefilter_mem[i] = 0
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
	for i := range st.energyError {
		st.energyError[i] = 0
	}
}

// transient_analysis_inv_table — C: celt_encoder.c:289-298.
var transient_analysis_inv_table = [128]byte{
	255, 255, 156, 110, 86, 70, 59, 51, 45, 40, 37, 33, 31, 28, 26, 25,
	23, 22, 21, 20, 19, 18, 17, 16, 16, 15, 15, 14, 13, 13, 12, 12,
	12, 12, 11, 11, 11, 10, 10, 10, 9, 9, 9, 9, 9, 9, 8, 8,
	8, 8, 8, 7, 7, 7, 7, 7, 7, 6, 6, 6, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6, 6, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 2,
}

// transient_analysis — C: celt_encoder.c:270-472 (float path).
func transient_analysis(in []opus_val32, length, C int,
	tf_estimate *opus_val16, tf_chan *int, allow_weak_transients int,
	weak_transient *int, tone_freq opus_val16, toneishness opus_val32,
	scratchTmp []opus_val16) int {
	var mem0, mem1 opus_val32
	is_transient := 0
	mask_metric := opus_int32(0)
	var tf_max opus_val16
	forward_decay := opus_val16(0.0625)
	tmp := scratchTmp[:length]

	*weak_transient = 0
	if allow_weak_transients != 0 {
		forward_decay = 0.03125
	}
	len2 := length / 2
	for c := 0; c < C; c++ {
		var mean opus_val32
		var unmask opus_int32
		var norm opus_val32
		var maxE opus_val16
		mem0 = 0
		mem1 = 0
		for i := 0; i < length; i++ {
			x := in[i+c*length]
			y := add_f32(mem0, x)
			// Dependency-chain-shortened variant from the C source:
			//   mem00 = mem0
			//   mem0 = mem0 - x + 0.5*mem1
			//   mem1 = x - mem00
			mem00 := mem0
			mem0 = add_f32(sub_f32(mem0, x), mul_f32(0.5, mem1))
			mem1 = sub_f32(x, mem00)
			tmp[i] = opus_val16(y)
		}
		// First few samples are bad because we don't propagate the memory.
		for i := 0; i < 12 && i < length; i++ {
			tmp[i] = 0
		}

		mean = 0
		mem0 = 0
		// Forward pass to compute the post-echo threshold.
		for i := 0; i < len2; i++ {
			x2 := add_f32(mul_f32(tmp[2*i], tmp[2*i]), mul_f32(tmp[2*i+1], tmp[2*i+1]))
			mean = add_f32(mean, x2)
			// mem0 = x2 + (1-fd)*mem0
			mem0 = fma_add(x2, sub_f32(1.0, forward_decay), mem0)
			tmp[i] = mul_f32(forward_decay, mem0)
		}

		mem0 = 0
		maxE = 0
		// Backward pass — 13.9 dB/ms masking.
		for i := len2 - 1; i >= 0; i-- {
			mem0 = fma_add(opus_val32(tmp[i]), 0.875, mem0)
			tmp[i] = mul_f32(0.125, mem0)
			maxE = MAX16(maxE, mul_f32(0.125, mem0))
		}

		// Frame energy = geometric mean of energy and half the max.
		// C: `celt_sqrt(mean * maxE * .5 * len2)`. `.5` is a bare double
		// literal, so the whole product promotes to double before sqrt.
		meanDbl := float64(mean) * float64(maxE) * 0.5 * float64(len2)
		mean = celt_sqrt(opus_val32(meanDbl))
		// Inverse of the mean energy.
		norm = SHL32(EXTEND32(opus_val16(len2)), 6+14) / (EPSILON + SHR32(mean, 1))
		unmask = 0
		celt_assert(!celt_isnanb(opus_val32(tmp[0])))
		celt_assert(!celt_isnanb(norm))
		for i := 12; i < len2-5; i += 4 {
			// C: floor(64*norm*(tmp[i]+EPSILON)) evaluated left-to-right
			// as ((64*norm)*(tmp[i]+EPSILON)).
			p := mul_f32(mul_f32(64.0, norm), add_f32(opus_val32(tmp[i]), EPSILON))
			id := int(math.Floor(float64(p)))
			if id < 0 {
				id = 0
			} else if id > 127 {
				id = 127
			}
			unmask += opus_int32(transient_analysis_inv_table[id])
		}
		// Normalize.
		unmask = 64 * unmask * 4 / opus_int32(6*(len2-17))
		if unmask > mask_metric {
			*tf_chan = c
			mask_metric = unmask
		}
	}
	if mask_metric > 200 {
		is_transient = 1
	}
	// Prevent partial cycles of very low-frequency tones being confused
	// with a transient.
	if toneishness > opus_val32(0.98) && tone_freq < opus_val16(0.026) {
		is_transient = 0
		mask_metric = 0
	}
	if allow_weak_transients != 0 && is_transient != 0 && mask_metric < 600 {
		is_transient = 0
		*weak_transient = 1
	}
	tf_max = MAX16(0, sub_f32(celt_sqrt(opus_val32(27*mask_metric)), 42))
	// C: `*tf_estimate = celt_sqrt(MAX32(0, SHL32(MULT16_16(0.0069, MIN16(163, tf_max)), 14) - QCONST32(0.139, 28)));`
	// QCONST32(0.139, 28) expands to `(0.139)` which is an untyped double
	// literal; the subtraction `float - double` promotes to double.
	innerMul := mul_f32(0.0069, MIN16(163, tf_max))
	diffDbl := float64(innerMul) - 0.139
	if diffDbl < 0 {
		diffDbl = 0
	}
	*tf_estimate = opus_val16(math.Sqrt(diffDbl))
	return is_transient
}

// celt_isnanb — self-inequality predicate (bool).
func celt_isnanb(x opus_val32) bool { return x != x }

// patch_transient_decision — C: celt_encoder.c:476-510.
func patch_transient_decision(newE, oldE []celt_glog, nbEBands, start, end, C int) int {
	var mean_diff opus_val32
	var spread_old [26]celt_glog
	if C == 1 {
		spread_old[start] = oldE[start]
		for i := start + 1; i < end; i++ {
			spread_old[i] = MAXG(spread_old[i-1]-GCONST(1.0), oldE[i])
		}
	} else {
		spread_old[start] = MAXG(oldE[start], oldE[start+nbEBands])
		for i := start + 1; i < end; i++ {
			spread_old[i] = MAXG(spread_old[i-1]-GCONST(1.0),
				MAXG(oldE[i], oldE[i+nbEBands]))
		}
	}
	for i := end - 2; i >= start; i-- {
		spread_old[i] = MAXG(spread_old[i], spread_old[i+1]-GCONST(1.0))
	}
	for c := 0; c < C; c++ {
		for i := IMAX(2, start); i < end-1; i++ {
			x1 := MAXG(0, newE[i+c*nbEBands])
			x2 := MAXG(0, spread_old[i])
			mean_diff = ADD32(mean_diff, MAXG(0, SUB32(x1, x2)))
		}
	}
	mean_diff = mean_diff / opus_val32(C*(end-1-IMAX(2, start)))
	if mean_diff > GCONST(1.0) {
		return 1
	}
	return 0
}

// compute_mdcts — C: celt_encoder.c:514-557.
func compute_mdcts(mode *OpusCustomMode, shortBlocks int, in, out []celt_sig,
	C, CC, LM, upsample, arch int) {
	overlap := mode.overlap
	var N, B, shift int
	if shortBlocks != 0 {
		B = shortBlocks
		N = mode.shortMdctSize
		shift = mode.maxLM
	} else {
		B = 1
		N = mode.shortMdctSize << LM
		shift = mode.maxLM - LM
	}
	for c := 0; c < CC; c++ {
		for b := 0; b < B; b++ {
			clt_mdct_forward(&mode.mdct,
				in[c*(B*N+overlap)+b*N:],
				out[b+c*N*B:],
				mode.window, overlap, shift, B, arch)
		}
	}
	if CC == 2 && C == 1 {
		for i := 0; i < B*N; i++ {
			out[i] = add_f32(HALF32(out[i]), HALF32(out[B*N+i]))
		}
	}
	if upsample != 1 {
		for c := 0; c < C; c++ {
			bound := B * N / upsample
			for i := 0; i < bound; i++ {
				out[c*B*N+i] = mul_f32(out[c*B*N+i], opus_val32(upsample))
			}
			OPUS_CLEAR(out[c*B*N+bound:], B*N-bound)
		}
	}
}

// celt_preemphasis — C: celt_encoder.c:560-649 (float path, non-CUSTOM_MODES).
func celt_preemphasis(pcmp []opus_res, inp []celt_sig, N, CC, upsample int,
	coef []opus_val16, mem *opus_val32, clip int) {
	coef0 := coef[0]
	m := *mem
	// Fast path: coef[1]==0, upsample==1, no clip.
	if coef[1] == 0 && upsample == 1 && clip == 0 {
		for i := 0; i < N; i++ {
			x := opus_val32(RES2SIG(pcmp[CC*i]))
			inp[i] = sub_f32(x, m)
			m = mul_f32(coef0, x)
		}
		*mem = m
		return
	}
	Nu := N / upsample
	if upsample != 1 {
		OPUS_CLEAR(inp, N)
	}
	for i := 0; i < Nu; i++ {
		inp[i*upsample] = opus_val32(RES2SIG(pcmp[CC*i]))
	}
	if clip != 0 {
		for i := 0; i < Nu; i++ {
			v := inp[i*upsample]
			if v > 65536 {
				v = 65536
			}
			if v < -65536 {
				v = -65536
			}
			inp[i*upsample] = v
		}
	}
	for i := 0; i < N; i++ {
		x := inp[i]
		inp[i] = sub_f32(x, m)
		m = mul_f32(coef0, x)
	}
	*mem = m
}

// l1_metric — C: celt_encoder.c:653-664.
func l1_metric(tmp []celt_norm, N, LM int, bias opus_val16) opus_val32 {
	var L1 opus_val32
	for i := 0; i < N; i++ {
		L1 += opus_val32(ABS16(tmp[i]))
	}
	// L1 += LM*bias*L1 (MAC16_32_Q15 in float = fused).
	L1 = fma_add(L1, opus_val16(LM)*bias, L1)
	return L1
}

// tf_analysis — C: celt_encoder.c:666-825.
func tf_analysis(m *OpusCustomMode, length, isTransient int, tf_res []int,
	lambda int, X []celt_norm, N0, LM int, tf_estimate opus_val16,
	tf_chan int, importance []int) int {
	bias := mul_f32(0.04, MAX16(-0.25, sub_f32(0.5, tf_estimate)))

	metric := make([]int, length)
	tmpBand := int(m.eBands[length]-m.eBands[length-1]) << LM
	tmp := make([]celt_norm, tmpBand)
	tmp_1 := make([]celt_norm, tmpBand)
	path0 := make([]int, length)
	path1 := make([]int, length)

	for i := 0; i < length; i++ {
		N := int(m.eBands[i+1]-m.eBands[i]) << LM
		narrow := 0
		if m.eBands[i+1]-m.eBands[i] == 1 {
			narrow = 1
		}
		copy(tmp[:N], X[tf_chan*N0+(int(m.eBands[i])<<LM):tf_chan*N0+(int(m.eBands[i])<<LM)+N])
		lmFirst := 0
		if isTransient != 0 {
			lmFirst = LM
		}
		L1 := l1_metric(tmp, N, lmFirst, bias)
		best_L1 := L1
		best_level := 0
		// Check the -1 case for transients.
		if isTransient != 0 && narrow == 0 {
			copy(tmp_1[:N], tmp[:N])
			haar1(tmp_1, N>>uint(LM), 1<<uint(LM))
			L1 = l1_metric(tmp_1, N, LM+1, bias)
			if L1 < best_L1 {
				best_L1 = L1
				best_level = -1
			}
		}
		kmax := LM
		if isTransient == 0 && narrow == 0 {
			kmax = LM + 1
		}
		for k := 0; k < kmax; k++ {
			var B int
			if isTransient != 0 {
				B = LM - k - 1
			} else {
				B = k + 1
			}
			haar1(tmp, N>>uint(k), 1<<uint(k))
			L1 = l1_metric(tmp, N, B, bias)
			if L1 < best_L1 {
				best_L1 = L1
				best_level = k + 1
			}
		}
		if isTransient != 0 {
			metric[i] = 2 * best_level
		} else {
			metric[i] = -2 * best_level
		}
		if narrow != 0 && (metric[i] == 0 || metric[i] == -2*LM) {
			metric[i] -= 1
		}
	}
	// Viterbi / tf_select decision.
	tf_select := 0
	var selcost [2]int
	var cost0, cost1 int
	for sel := 0; sel < 2; sel++ {
		cost0 = importance[0] * absInt(metric[0]-2*int(tf_select_table[LM][4*isTransient+2*sel+0]))
		cost1 = importance[0] * absInt(metric[0]-2*int(tf_select_table[LM][4*isTransient+2*sel+1]))
		if isTransient == 0 {
			cost1 += lambda
		}
		for i := 1; i < length; i++ {
			curr0 := IMIN(cost0, cost1+lambda)
			curr1 := IMIN(cost0+lambda, cost1)
			cost0 = curr0 + importance[i]*absInt(metric[i]-2*int(tf_select_table[LM][4*isTransient+2*sel+0]))
			cost1 = curr1 + importance[i]*absInt(metric[i]-2*int(tf_select_table[LM][4*isTransient+2*sel+1]))
		}
		cost0 = IMIN(cost0, cost1)
		selcost[sel] = cost0
	}
	if selcost[1] < selcost[0] && isTransient != 0 {
		tf_select = 1
	}
	cost0 = importance[0] * absInt(metric[0]-2*int(tf_select_table[LM][4*isTransient+2*tf_select+0]))
	cost1 = importance[0] * absInt(metric[0]-2*int(tf_select_table[LM][4*isTransient+2*tf_select+1]))
	if isTransient == 0 {
		cost1 += lambda
	}
	for i := 1; i < length; i++ {
		from0 := cost0
		from1 := cost1 + lambda
		var curr0, curr1 int
		if from0 < from1 {
			curr0 = from0
			path0[i] = 0
		} else {
			curr0 = from1
			path0[i] = 1
		}
		from0 = cost0 + lambda
		from1 = cost1
		if from0 < from1 {
			curr1 = from0
			path1[i] = 0
		} else {
			curr1 = from1
			path1[i] = 1
		}
		cost0 = curr0 + importance[i]*absInt(metric[i]-2*int(tf_select_table[LM][4*isTransient+2*tf_select+0]))
		cost1 = curr1 + importance[i]*absInt(metric[i]-2*int(tf_select_table[LM][4*isTransient+2*tf_select+1]))
	}
	if cost0 < cost1 {
		tf_res[length-1] = 0
	} else {
		tf_res[length-1] = 1
	}
	for i := length - 2; i >= 0; i-- {
		if tf_res[i+1] == 1 {
			tf_res[i] = path1[i+1]
		} else {
			tf_res[i] = path0[i+1]
		}
	}
	return tf_select
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// tf_encode — C: celt_encoder.c:827-865.
func tf_encode(start, end, isTransient int, tf_res []int, LM, tf_select int, enc *ec_enc) {
	budget := opus_uint32(enc.storage) * 8
	tell := opus_uint32(ec_tell(enc))
	logp := 4
	if isTransient != 0 {
		logp = 2
	}
	tf_select_rsv := 0
	if LM > 0 && tell+opus_uint32(logp)+1 <= budget {
		tf_select_rsv = 1
	}
	budget -= opus_uint32(tf_select_rsv)
	curr, tf_changed := 0, 0
	for i := start; i < end; i++ {
		if tell+opus_uint32(logp) <= budget {
			ec_enc_bit_logp(enc, tf_res[i]^curr, logp)
			tell = opus_uint32(ec_tell(enc))
			curr = tf_res[i]
			tf_changed |= curr
		} else {
			tf_res[i] = curr
		}
		logp = 5
		if isTransient != 0 {
			logp = 4
		}
	}
	if tf_select_rsv != 0 &&
		tf_select_table[LM][4*isTransient+0+tf_changed] !=
			tf_select_table[LM][4*isTransient+2+tf_changed] {
		ec_enc_bit_logp(enc, tf_select, 1)
	} else {
		tf_select = 0
	}
	for i := start; i < end; i++ {
		tf_res[i] = int(tf_select_table[LM][4*isTransient+2*tf_select+tf_res[i]])
	}
}

// alloc_trim_analysis — C: celt_encoder.c:868-958 (float path).
func alloc_trim_analysis(m *OpusCustomMode, X []celt_norm, bandLogE []celt_glog,
	end, LM, C, N0 int, analysis *AnalysisInfo, stereo_saving *opus_val16,
	tf_estimate opus_val16, intensity int, surround_trim celt_glog,
	equiv_rate opus_int32, arch int) int {
	var diff opus_val32
	var trim opus_val16 = 5.0
	if equiv_rate < 64000 {
		trim = 4.0
	} else if equiv_rate < 80000 {
		// C: `frac = (equiv_rate-64000) >> 10` — integer shift, not
		// float division. Then `(1.f/16.f) * (int)frac`.
		frac := int((equiv_rate - 64000) >> 10)
		trim = 4.0 + mul_f32(1.0/16.0, opus_val16(frac))
	}
	if C == 2 {
		var sum opus_val16 = 0
		for i := 0; i < 8; i++ {
			partial := celt_inner_prod(
				X[int(m.eBands[i])<<LM:],
				X[N0+(int(m.eBands[i])<<LM):],
				int(m.eBands[i+1]-m.eBands[i])<<LM, arch)
			sum = add_f32(sum, partial)
		}
		sum = mul_f32(1.0/8.0, sum)
		sum = MIN16(1.0, ABS16(sum))
		minXC := sum
		for i := 8; i < intensity; i++ {
			partial := celt_inner_prod(
				X[int(m.eBands[i])<<LM:],
				X[N0+(int(m.eBands[i])<<LM):],
				int(m.eBands[i+1]-m.eBands[i])<<LM, arch)
			minXC = MIN16(minXC, ABS16(partial))
		}
		minXC = MIN16(1.0, ABS16(minXC))
		logXC := celt_log2(sub_f32(1.001, mul_f32(sum, sum)))
		logXC2 := MAX16(mul_f32(0.5, logXC), celt_log2(sub_f32(1.001, mul_f32(minXC, minXC))))
		trim += MAX16(-4.0, mul_f32(0.75, logXC))
		*stereo_saving = MIN16(*stereo_saving+0.25, mul_f32(-0.5, logXC2))
	}

	// Spectral tilt estimate. Pin the multiply-accumulate to match the
	// C oracle compiled with -ffp-contract=off.
	for c := 0; c < C; c++ {
		for i := 0; i < end-1; i++ {
			diff = add_f32(diff,
				mul_f32(opus_val16(bandLogE[i+c*m.nbEBands]), opus_val16(2+2*i-end)))
		}
	}
	diff /= opus_val32(C * (end - 1))
	// C: trim -= MAX32(-2, MIN32(2, SHR32(diff + QCONST32(1.f, DB_SHIFT-5), DB_SHIFT-13)/6))
	// In the float build SHR32 is a no-op, so the expression is (diff + 1) / 6.
	adj := (diff + 1.0) / 6.0
	if adj > 2 {
		adj = 2
	}
	if adj < -2 {
		adj = -2
	}
	trim -= opus_val16(adj)
	trim -= opus_val16(surround_trim)
	trim -= mul_f32(2, tf_estimate)
	if analysis.valid != 0 {
		a := 2.0 * (analysis.tonality_slope + 0.05)
		if a > 2 {
			a = 2
		}
		if a < -2 {
			a = -2
		}
		trim -= opus_val16(a)
	}
	trim_index := int(math.Floor(float64(trim + 0.5)))
	trim_index = IMAX(0, IMIN(10, trim_index))
	return trim_index
}

// stereo_analysis — C: celt_encoder.c:960-990 (float path).
func stereo_analysis(m *OpusCustomMode, X []celt_norm, LM, N0 int) int {
	sumLR := opus_val32(EPSILON)
	sumMS := opus_val32(EPSILON)
	for i := 0; i < 13; i++ {
		for j := int(m.eBands[i]) << LM; j < int(m.eBands[i+1])<<LM; j++ {
			L := opus_val32(X[j])
			R := opus_val32(X[N0+j])
			M := add_f32(L, R)
			S := sub_f32(L, R)
			sumLR = add_f32(sumLR, add_f32(ABS32(L), ABS32(R)))
			sumMS = add_f32(sumMS, add_f32(ABS32(M), ABS32(S)))
		}
	}
	sumMS = mul_f32(0.707107, sumMS)
	thetas := 13
	if LM <= 1 {
		thetas -= 8
	}
	lhs := mul_f32(opus_val32(int(m.eBands[13])<<uint(LM+1)+thetas), sumMS)
	rhs := mul_f32(opus_val32(int(m.eBands[13])<<uint(LM+1)), sumLR)
	if lhs > rhs {
		return 1
	}
	return 0
}

func mswap_glog(a, b *celt_glog) { tmp := *a; *a = *b; *b = tmp }

// median_of_5 — C: celt_encoder.c:993-1030.
func median_of_5(x []celt_glog) celt_glog {
	t2 := x[2]
	var t0, t1, t3, t4 celt_glog
	if x[0] > x[1] {
		t0 = x[1]
		t1 = x[0]
	} else {
		t0 = x[0]
		t1 = x[1]
	}
	if x[3] > x[4] {
		t3 = x[4]
		t4 = x[3]
	} else {
		t3 = x[3]
		t4 = x[4]
	}
	if t0 > t3 {
		mswap_glog(&t0, &t3)
		mswap_glog(&t1, &t4)
	}
	if t2 > t1 {
		if t1 < t3 {
			return MING(t2, t3)
		}
		return MING(t4, t1)
	}
	if t2 < t3 {
		return MING(t1, t3)
	}
	return MING(t2, t4)
}

// median_of_3 — C: celt_encoder.c:1032-1050.
func median_of_3(x []celt_glog) celt_glog {
	var t0, t1, t2 celt_glog
	if x[0] > x[1] {
		t0 = x[1]
		t1 = x[0]
	} else {
		t0 = x[0]
		t1 = x[1]
	}
	t2 = x[2]
	if t1 < t2 {
		return t1
	} else if t0 < t2 {
		return t2
	}
	return t0
}

// dynalloc_analysis — C: celt_encoder.c:1052-1276 (float path).
func dynalloc_analysis(bandLogE, bandLogE2, oldBandE []celt_glog,
	nbEBands, start, end, C int, offsets []int, lsb_depth int,
	logN []opus_int16, isTransient, vbr, constrained_vbr int,
	eBands []opus_int16, LM, effectiveBytes int, tot_boost_ *opus_int32,
	lfe int, surround_dynalloc []celt_glog, analysis *AnalysisInfo,
	importance, spread_weight []int,
	tone_freq opus_val16, toneishness opus_val32) celt_glog {
	var tot_boost opus_int32
	var maxDepth celt_glog

	follower := make([]celt_glog, C*nbEBands)
	noise_floor := make([]celt_glog, C*nbEBands)
	bandLogE3 := make([]celt_glog, nbEBands)
	for i := range offsets {
		offsets[i] = 0
	}

	maxDepth = -GCONST(31.9)
	for i := 0; i < end; i++ {
		// noise_floor[i] = 0.0625*logN[i] + 0.5 + (9-lsb_depth) - eMeans[i] + 0.0062*(i+5)^2
		// Pin every add/sub so Go doesn't fuse `a*b+c` into FMADD.
		t := mul_f32(celt_glog(0.0625), celt_glog(logN[i]))
		t = add_f32(t, celt_glog(0.5))
		t = add_f32(t, celt_glog(9-lsb_depth))
		t = sub_f32(t, celt_glog(eMeans[i]))
		q := mul_f32(celt_glog(0.0062), celt_glog(i+5))
		q = mul_f32(q, celt_glog(i+5))
		noise_floor[i] = add_f32(t, q)
	}
	for c := 0; c < C; c++ {
		for i := 0; i < end; i++ {
			maxDepth = MAXG(maxDepth, bandLogE[c*nbEBands+i]-noise_floor[i])
		}
	}
	{
		mask := make([]celt_glog, nbEBands)
		sig := make([]celt_glog, nbEBands)
		for i := 0; i < end; i++ {
			mask[i] = bandLogE[i] - noise_floor[i]
		}
		if C == 2 {
			for i := 0; i < end; i++ {
				mask[i] = MAXG(mask[i], bandLogE[nbEBands+i]-noise_floor[i])
			}
		}
		copy(sig[:end], mask[:end])
		for i := 1; i < end; i++ {
			mask[i] = MAXG(mask[i], mask[i-1]-GCONST(2.0))
		}
		for i := end - 2; i >= 0; i-- {
			mask[i] = MAXG(mask[i], mask[i+1]-GCONST(3.0))
		}
		for i := 0; i < end; i++ {
			smr := sig[i] - MAXG(MAXG(0, maxDepth-GCONST(12.0)), mask[i])
			// C: int shift = IMIN(5, IMAX(0, -(int)floor(.5f + smr)));
			// floor is applied to (.5f+smr), cast to int, then negated — the
			// negation must come AFTER the floor-to-int, because
			// -floor(x) != floor(-x) in general.
			shift := -int(math.Floor(float64(add_f32(celt_glog(0.5), smr))))
			if shift < 0 {
				shift = 0
			}
			if shift > 5 {
				shift = 5
			}
			spread_weight[i] = 32 >> uint(shift)
		}
	}
	if effectiveBytes >= (30+5*LM) && lfe == 0 {
		last := 0
		for c := 0; c < C; c++ {
			copy(bandLogE3[:end], bandLogE2[c*nbEBands:c*nbEBands+end])
			if LM == 0 {
				for i := 0; i < IMIN(8, end); i++ {
					bandLogE3[i] = MAXG(bandLogE2[c*nbEBands+i], oldBandE[c*nbEBands+i])
				}
			}
			f := follower[c*nbEBands : (c+1)*nbEBands]
			f[0] = bandLogE3[0]
			for i := 1; i < end; i++ {
				if bandLogE3[i] > bandLogE3[i-1]+GCONST(0.5) {
					last = i
				}
				f[i] = MING(f[i-1]+GCONST(1.5), bandLogE3[i])
			}
			for i := last - 1; i >= 0; i-- {
				f[i] = MING(f[i], MING(f[i+1]+GCONST(2.0), bandLogE3[i]))
			}
			offset := celt_glog(GCONST(1.0))
			for i := 2; i < end-2; i++ {
				f[i] = MAXG(f[i], median_of_5(bandLogE3[i-2:])-offset)
			}
			tmp := median_of_3(bandLogE3[:]) - offset
			f[0] = MAXG(f[0], tmp)
			f[1] = MAXG(f[1], tmp)
			tmp = median_of_3(bandLogE3[end-3:]) - offset
			f[end-2] = MAXG(f[end-2], tmp)
			f[end-1] = MAXG(f[end-1], tmp)
			for i := 0; i < end; i++ {
				f[i] = MAXG(f[i], noise_floor[i])
			}
		}
		if C == 2 {
			for i := start; i < end; i++ {
				follower[nbEBands+i] = MAXG(follower[nbEBands+i], follower[i]-GCONST(4.0))
				follower[i] = MAXG(follower[i], follower[nbEBands+i]-GCONST(4.0))
				follower[i] = HALF32(MAXG(0, bandLogE[i]-follower[i]) +
					MAXG(0, bandLogE[nbEBands+i]-follower[nbEBands+i]))
			}
		} else {
			for i := start; i < end; i++ {
				follower[i] = MAXG(0, bandLogE[i]-follower[i])
			}
		}
		for i := start; i < end; i++ {
			follower[i] = MAXG(follower[i], surround_dynalloc[i])
		}
		for i := start; i < end; i++ {
			// C: `floor(.5f + 13*celt_exp2_db(...))`. Pin so Go doesn't
			// fuse the mul and add into a single FMADD.
			expVal := celt_exp2_db(MING(follower[i], GCONST(4.0)))
			sum := add_f32(0.5, mul_f32(13, expVal))
			importance[i] = int(math.Floor(float64(sum)))
		}
		if (vbr == 0 || constrained_vbr != 0) && isTransient == 0 {
			for i := start; i < end; i++ {
				follower[i] = HALF32(follower[i])
			}
		}
		for i := start; i < end; i++ {
			if i < 8 {
				follower[i] *= 2
			}
			if i >= 12 {
				follower[i] = HALF32(follower[i])
			}
		}
		if toneishness > opus_val32(0.98) {
			freq_bin := int(math.Floor(0.5 + float64(tone_freq)*120.0/math.Pi))
			for i := start; i < end; i++ {
				if freq_bin >= int(eBands[i]) && freq_bin <= int(eBands[i+1]) {
					follower[i] += GCONST(2.0)
				}
				if freq_bin >= int(eBands[i])-1 && freq_bin <= int(eBands[i+1])+1 {
					follower[i] += GCONST(1.0)
				}
				if freq_bin >= int(eBands[i])-2 && freq_bin <= int(eBands[i+1])+2 {
					follower[i] += GCONST(1.0)
				}
				if freq_bin >= int(eBands[i])-3 && freq_bin <= int(eBands[i+1])+3 {
					follower[i] += GCONST(0.5)
				}
			}
			if freq_bin >= int(eBands[end]) {
				follower[end-1] += GCONST(2.0)
				follower[end-2] += GCONST(1.0)
			}
		}
		if analysis.valid != 0 {
			for i := start; i < IMIN(LEAK_BANDS, end); i++ {
				follower[i] = follower[i] + GCONST(1.0/64.0)*celt_glog(analysis.leak_boost[i])
			}
		}
		for i := start; i < end; i++ {
			var boost, boost_bits int
			follower[i] = MING(follower[i], GCONST(4))
			width := C * int(eBands[i+1]-eBands[i]) << LM
			if width < 6 {
				boost = int(follower[i])
				boost_bits = boost * width << BITRES
			} else if width > 48 {
				boost = int(follower[i] * 8)
				boost_bits = (boost * width << BITRES) / 8
			} else {
				boost = int(follower[i] * celt_glog(width) / 6)
				boost_bits = boost * 6 << BITRES
			}
			if (vbr == 0 || (constrained_vbr != 0 && isTransient == 0)) &&
				(int(tot_boost)+boost_bits)>>BITRES>>3 > 2*effectiveBytes/3 {
				cap := opus_int32((2 * effectiveBytes / 3) << BITRES << 3)
				offsets[i] = int(cap - tot_boost)
				tot_boost = cap
				break
			} else {
				offsets[i] = boost
				tot_boost += opus_int32(boost_bits)
			}
		}
	} else {
		for i := start; i < end; i++ {
			importance[i] = 13
		}
	}
	*tot_boost_ = tot_boost
	return maxDepth
}

// tone_lpc — C: celt_encoder.c:1308-1362 (float path). Every `r += x*y`
// and `a*b - c*d` pattern is pinned with add_f32 / sub_f32 / mul_f32
// so Go emits separate FMUL+FADD instructions, matching the C oracle
// compiled with -ffp-contract=off.
func tone_lpc(x []opus_val16, length, delay int, lpc []opus_val32) int {
	var r00, r01, r11, r02, r12, r22 opus_val32
	celt_assert(length > 2*delay)
	for i := 0; i < length-2*delay; i++ {
		r00 = add_f32(r00, mul_f32(x[i], x[i]))
		r01 = add_f32(r01, mul_f32(x[i], x[i+delay]))
		r02 = add_f32(r02, mul_f32(x[i], x[i+2*delay]))
	}
	var edges opus_val32
	for i := 0; i < delay; i++ {
		a := mul_f32(x[length+i-2*delay], x[length+i-2*delay])
		b := mul_f32(x[i], x[i])
		edges = add_f32(edges, sub_f32(a, b))
	}
	r11 = add_f32(r00, edges)
	edges = 0
	for i := 0; i < delay; i++ {
		a := mul_f32(x[length+i-delay], x[length+i-delay])
		b := mul_f32(x[i+delay], x[i+delay])
		edges = add_f32(edges, sub_f32(a, b))
	}
	r22 = add_f32(r11, edges)
	edges = 0
	for i := 0; i < delay; i++ {
		a := mul_f32(x[length+i-2*delay], x[length+i-delay])
		b := mul_f32(x[i], x[i+delay])
		edges = add_f32(edges, sub_f32(a, b))
	}
	r12 = add_f32(r01, edges)
	{
		R00 := r00 + r22
		R01 := r01 + r12
		R11 := 2 * r11
		R02 := 2 * r02
		R12 := r12 + r01
		R22 := r00 + r22
		r00 = R00
		r01 = R01
		r11 = R11
		r02 = R02
		r12 = R12
		r22 = R22
		_ = r22
	}
	// `a*b - c*d` in C under -ffp-contract=off is two FMULs + FSUB.
	// Pin to avoid Go fusing one mul into an FMSUB.
	den := sub_f32(mul_f32(r00, r11), mul_f32(r01, r01))
	// C: `den < .001f*MULT32_32_Q31(r00,r11)` with macro = `0.001 * (r00*r11)`.
	// The inner product is the already-computed `r00*r11` from the den
	// numerator. In C under -ffp-contract=off the second `r00*r11` is
	// recomputed as a separate FMUL matching the first one bit-for-bit
	// (same inputs, same op) — but the associativity is `.001 * prod`,
	// not `(0.001 * r00) * r11`. Mirror that here.
	if den < mul_f32(0.001, mul_f32(r00, r11)) {
		return 1
	}
	num1 := sub_f32(mul_f32(r02, r11), mul_f32(r01, r12))
	if num1 >= den {
		lpc[1] = 1
	} else if num1 <= -den {
		lpc[1] = -1
	} else {
		lpc[1] = num1 / den
	}
	num0 := sub_f32(mul_f32(r00, r12), mul_f32(r02, r01))
	if HALF32(num0) >= den {
		lpc[0] = 1.999999
	} else if HALF32(num0) <= -den {
		lpc[0] = -1.999999
	} else {
		lpc[0] = num0 / den
	}
	return 0
}

// tone_detect — C: celt_encoder.c:1365-1405 (float path).
func tone_detect(in []celt_sig, CC, N int, toneishness *opus_val32,
	Fs opus_int32) opus_val16 {
	delay := 1
	x := make([]opus_val16, N)
	if CC == 2 {
		for i := 0; i < N; i++ {
			// C: `x[i] = PSHR32(ADD32(SHR32(in[i], 1), SHR32(in[i+N], 1)), SIG_SHIFT+2);`
			// FP build: `SHR32` and `PSHR32` are identity (see arch.h), so
			// the expression collapses to `in[i] + in[i+N]`. Earlier port
			// used `0.5*in[i] + 0.5*in[i+N]`, matching the fixed-point
			// semantics but halving the signal amplitude in the float
			// build — the tone_lpc() analysis that follows then sees
			// quarter-energy inputs and picks different LPC coefficients.
			x[i] = opus_val16(add_f32(in[i], in[i+N]))
		}
	} else {
		for i := 0; i < N; i++ {
			x[i] = opus_val16(in[i])
		}
	}
	var lpc [2]opus_val32
	fail := tone_lpc(x, N, delay, lpc[:])
	for delay <= int(Fs)/3000 && (fail != 0 || (lpc[0] > 1 && lpc[1] < 0)) {
		delay *= 2
		fail = tone_lpc(x, N, delay, lpc[:])
	}
	var freq opus_val16
	// C: `lpc[0]*lpc[0] + 3.999999*lpc[1] < 0`. Pin so Go doesn't fuse
	// the second mul into an FMADD with the first mul.
	lpcDisc := add_f32(mul_f32(lpc[0], lpc[0]), mul_f32(3.999999, lpc[1]))
	if fail == 0 && lpcDisc < 0 {
		*toneishness = -lpc[1]
		// C: `acos(.5f*lpc[0])/delay` — acos returns double, division
		// runs in double, then narrows at assignment.
		freq = opus_val16(math.Acos(float64(0.5*lpc[0])) / float64(delay))
	} else {
		freq = -1
		*toneishness = 0
	}
	return freq
}

// run_prefilter — C: celt_encoder.c:1407-1605 (float, non-QEXT).
func run_prefilter(st *OpusCustomEncoder, in []celt_sig, prefilter_mem []celt_sig,
	CC, N, prefilter_tapset int, pitch *int, gain *opus_val16, qgain *int,
	enabled, complexity int, tf_estimate opus_val16, nbAvailableBytes int,
	analysis *AnalysisInfo, tone_freq opus_val16, toneishness opus_val32) int {
	max_period := COMBFILTER_MAXPERIOD
	min_period := COMBFILTER_MINPERIOD
	mode := st.mode
	overlap := mode.overlap
	_pre := st.scratchPre[:CC*(N+max_period)]
	pre := [2][]celt_sig{
		_pre[:N+max_period],
		_pre[N+max_period:],
	}
	for c := 0; c < CC; c++ {
		copy(pre[c][:max_period], prefilter_mem[c*max_period:(c+1)*max_period])
		copy(pre[c][max_period:max_period+N], in[c*(N+overlap)+overlap:c*(N+overlap)+overlap+N])
	}

	var pitch_index int
	var gain1 opus_val16
	if enabled != 0 && toneishness > opus_val32(0.99) {
		multiple := 1
		if tone_freq >= 3.1416 {
			tone_freq = 3.141593 - tone_freq
		}
		for tone_freq >= opus_val16(multiple)*0.39 {
			multiple++
		}
		if tone_freq > 0.006148 {
			pitch_index = int(math.Floor(0.5 + 2.0*math.Pi*float64(multiple)/float64(tone_freq)))
			if pitch_index > COMBFILTER_MAXPERIOD-2 {
				pitch_index = COMBFILTER_MAXPERIOD - 2
			}
		} else {
			pitch_index = COMBFILTER_MINPERIOD
		}
		gain1 = 0.75
	} else if enabled != 0 && complexity >= 5 {
		pitch_buf := st.scratchPitchBuf[:(max_period+N)>>1]
		pitch_downsample(pre[:CC], pitch_buf, (max_period+N)>>1, CC, 2, st.arch)
		pitch_search(pitch_buf[max_period>>1:], pitch_buf, N,
			max_period-3*min_period, &pitch_index, st.arch)
		pitch_index = max_period - pitch_index
		gain1 = remove_doubling(pitch_buf, max_period, min_period,
			N, &pitch_index, st.prefilter_period, st.prefilter_gain, st.arch)
		if pitch_index > max_period-2 {
			pitch_index = max_period - 2
		}
		gain1 = mul_f32(0.7, gain1)
		if st.loss_rate > 2 {
			gain1 = HALF32(gain1)
		}
		if st.loss_rate > 4 {
			gain1 = HALF32(gain1)
		}
		if st.loss_rate > 8 {
			gain1 = 0
		}
	} else {
		gain1 = 0
		pitch_index = COMBFILTER_MINPERIOD
	}
	if analysis.valid != 0 {
		gain1 = opus_val16(gain1 * analysis.max_pitch_ratio)
	}
	pf_threshold := opus_val16(0.2)
	if absInt(pitch_index-st.prefilter_period)*10 > pitch_index {
		pf_threshold += 0.2
		if tf_estimate > 0.98 {
			gain1 = 0
		}
	}
	if nbAvailableBytes < 25 {
		pf_threshold += 0.1
	}
	if nbAvailableBytes < 35 {
		pf_threshold += 0.1
	}
	if st.prefilter_gain > 0.4 {
		pf_threshold -= 0.1
	}
	if st.prefilter_gain > 0.55 {
		pf_threshold -= 0.1
	}
	pf_threshold = MAX16(pf_threshold, 0.2)
	var pf_on, qg int
	if gain1 < pf_threshold {
		gain1 = 0
		pf_on = 0
		qg = 0
	} else {
		if ABS16(gain1-st.prefilter_gain) < 0.1 {
			gain1 = st.prefilter_gain
		}
		// C: `qg = (int)floor(.5f+gain1*32/3)-1;`. Every op runs in
		// float32: `gain1*32`, `/3`, `+.5f`, then the float32 result
		// promotes to double for `floor`. Earlier port computed everything
		// in float64 which can round differently at the boundary.
		mulq := mul_f32(float32(gain1), 32)
		divq := mulq / 3
		addq := add_f32(0.5, divq)
		qg = int(math.Floor(float64(addq))) - 1
		qg = IMAX(0, IMIN(7, qg))
		gain1 = opus_val16(0.09375 * float32(qg+1))
		pf_on = 1
	}
	before := [2]opus_val32{}
	after := [2]opus_val32{}
	for c := 0; c < CC; c++ {
		offset := mode.shortMdctSize - overlap
		st.prefilter_period = IMAX(st.prefilter_period, COMBFILTER_MINPERIOD)
		copy(in[c*(N+overlap):c*(N+overlap)+overlap], st.in_mem[c*overlap:(c+1)*overlap])
		for i := 0; i < N; i++ {
			before[c] += ABS32(in[c*(N+overlap)+overlap+i])
		}
		if offset != 0 {
			comb_filter(in, c*(N+overlap)+overlap, pre[c], max_period,
				st.prefilter_period, st.prefilter_period, offset,
				-st.prefilter_gain, -st.prefilter_gain,
				st.prefilter_tapset, st.prefilter_tapset, nil, 0, st.arch)
		}
		comb_filter(in, c*(N+overlap)+overlap+offset, pre[c], max_period+offset,
			st.prefilter_period, pitch_index, N-offset,
			-st.prefilter_gain, -gain1,
			st.prefilter_tapset, prefilter_tapset,
			mode.window, overlap, st.arch)
		for i := 0; i < N; i++ {
			after[c] += ABS32(in[c*(N+overlap)+overlap+i])
		}
	}

	cancel_pitch := 0
	if CC == 2 {
		var thresh [2]opus_val16
		thresh[0] = opus_val16(add_f32(mul_f32(mul_f32(0.25, gain1), before[0]), mul_f32(0.01, before[1])))
		thresh[1] = opus_val16(add_f32(mul_f32(mul_f32(0.25, gain1), before[1]), mul_f32(0.01, before[0])))
		if after[0]-before[0] > opus_val32(thresh[0]) || after[1]-before[1] > opus_val32(thresh[1]) {
			cancel_pitch = 1
		}
		if before[0]-after[0] < opus_val32(thresh[0]) && before[1]-after[1] < opus_val32(thresh[1]) {
			cancel_pitch = 1
		}
	} else {
		if after[0] > before[0] {
			cancel_pitch = 1
		}
	}
	if cancel_pitch != 0 {
		for c := 0; c < CC; c++ {
			offset := mode.shortMdctSize - overlap
			copy(in[c*(N+overlap)+overlap:c*(N+overlap)+overlap+N], pre[c][max_period:max_period+N])
			comb_filter(in, c*(N+overlap)+overlap+offset, pre[c], max_period+offset,
				st.prefilter_period, pitch_index, overlap,
				-st.prefilter_gain, 0,
				st.prefilter_tapset, prefilter_tapset,
				mode.window, overlap, st.arch)
		}
		gain1 = 0
		pf_on = 0
		qg = 0
	}

	for c := 0; c < CC; c++ {
		copy(st.in_mem[c*overlap:(c+1)*overlap], in[c*(N+overlap)+N:c*(N+overlap)+N+overlap])
		if N > max_period {
			copy(prefilter_mem[c*max_period:(c+1)*max_period], pre[c][N:N+max_period])
		} else {
			copy(prefilter_mem[c*max_period:c*max_period+max_period-N],
				prefilter_mem[c*max_period+N:(c+1)*max_period])
			copy(prefilter_mem[c*max_period+max_period-N:(c+1)*max_period],
				pre[c][max_period:max_period+N])
		}
	}

	*gain = gain1
	*pitch = pitch_index
	*qgain = qg
	return pf_on
}

// compute_vbr — C: celt_encoder.c:1607-1719 (float, non-QEXT).
func compute_vbr(mode *OpusCustomMode, analysis *AnalysisInfo, base_target opus_int32,
	LM int, bitrate opus_int32, lastCodedBands, C, intensity, constrained_vbr int,
	stereo_saving opus_val16, tot_boost int, tf_estimate opus_val16,
	pitch_change int, maxDepth celt_glog, lfe, has_surround_mask int,
	surround_masking, temporal_vbr celt_glog) opus_int32 {
	nbEBands := mode.nbEBands
	eBands := mode.eBands

	coded_bands := lastCodedBands
	if coded_bands == 0 {
		coded_bands = nbEBands
	}
	coded_bins := int(eBands[coded_bands]) << LM
	if C == 2 {
		coded_bins += int(eBands[IMIN(intensity, coded_bands)]) << LM
	}
	target := base_target

	if analysis.valid != 0 && analysis.activity < 0.4 {
		target -= opus_int32(float32(coded_bins<<BITRES) * (0.4 - analysis.activity))
	}
	if C == 2 {
		coded_stereo_bands := IMIN(intensity, coded_bands)
		coded_stereo_dof := (int(eBands[coded_stereo_bands]) << LM) - coded_stereo_bands
		max_frac := 0.8 * float32(coded_stereo_dof) / float32(coded_bins)
		if stereo_saving > opus_val16(1.0) {
			stereo_saving = 1.0
		}
		target -= opus_int32(MIN32(
			opus_val32(max_frac)*opus_val32(target),
			opus_val32(stereo_saving-opus_val16(0.1))*opus_val32(coded_stereo_dof<<BITRES)))
	}
	target += opus_int32(tot_boost) - opus_int32(19<<LM)
	tf_calibration := opus_val16(0.044)
	// C: `target += SHL32(MULT16_32_Q15(tf_estimate-tf_calibration, target), 1);`
	// FP build: `MULT16_32_Q15(a,b) = a*b`, and crucially `SHL32(x, 1) = x`
	// (identity — see arch.h). The FP macro drops the `<<1` that the
	// FIXED_POINT path applies. An earlier draft of this port treated the
	// shift as a multiply by two (`* 2.0`), which matched fixed-point but
	// doubled the transient-boost contribution in float, inflating VBR
	// targets and blowing up CELT-only packets by ~16 %.
	target += opus_int32(mul_f32(float32(tf_estimate-tf_calibration), float32(target)))
	if analysis.valid != 0 && lfe == 0 {
		tonal := float32(0)
		if analysis.tonality > 0.15 {
			tonal = analysis.tonality - 0.15
		}
		tonal -= 0.12
		tonal_target := target + opus_int32(float32(coded_bins<<BITRES)*1.2*tonal)
		if pitch_change != 0 {
			tonal_target += opus_int32(float32(coded_bins<<BITRES) * 0.8)
		}
		target = tonal_target
	}
	if has_surround_mask != 0 && lfe == 0 {
		surround_target := target + opus_int32(surround_masking*celt_glog(coded_bins<<BITRES))
		if target/4 > surround_target {
			target = target / 4
		} else {
			target = surround_target
		}
	}
	{
		bins := int(eBands[nbEBands-2]) << LM
		floor_depth := opus_int32(celt_glog(C*bins<<BITRES) * maxDepth)
		if floor_depth < target>>2 {
			floor_depth = target >> 2
		}
		if target > floor_depth {
			target = floor_depth
		}
	}
	if (has_surround_mask == 0 || lfe != 0) && constrained_vbr != 0 {
		target = base_target + opus_int32(0.67*float32(target-base_target))
	}
	if has_surround_mask == 0 && tf_estimate < opus_val16(0.2) {
		amount := 0.0000031 * float32(IMAX(0, IMIN(32000, 96000-int(bitrate))))
		tvbr_factor := celt_glog(temporal_vbr) * celt_glog(amount)
		target += opus_int32(float32(tvbr_factor) * float32(target))
	}
	if target > 2*base_target {
		target = 2 * base_target
	}
	return target
}

// celt_encode_with_ec — main entry. C: celt_encoder.c:1728-2835
// (non-QEXT, non-CUSTOM_MODES, non-RESYNTH float path).
func celt_encode_with_ec(st *OpusCustomEncoder, pcm []opus_res, frame_size int,
	compressed []byte, nbCompressedBytes int, enc *ec_enc) int {
	CC := st.channels
	C := st.stream_channels
	mode := st.mode
	nbEBands := mode.nbEBands
	overlap := mode.overlap
	eBands := mode.eBands
	start := st.start
	end := st.end
	hybrid := 0
	if start != 0 {
		hybrid = 1
	}
	tf_estimate := opus_val16(0)
	if nbCompressedBytes < 2 || pcm == nil {
		return OPUS_BAD_ARG
	}
	frame_size *= st.upsample
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
	N := M * mode.shortMdctSize

	oldBandE := st.oldBandE
	oldLogE := st.oldLogE
	oldLogE2 := st.oldLogE2
	energyError := st.energyError

	var _enc ec_enc
	var tell, tell0_frac opus_int32
	var nbFilledBytes int
	if enc == nil {
		tell0_frac = 1
		tell = 1
		nbFilledBytes = 0
	} else {
		tell0_frac = opus_int32(ec_tell_frac(enc))
		tell = opus_int32(ec_tell(enc))
		nbFilledBytes = int(tell+4) >> 3
	}
	if st.signalling != 0 {
		return OPUS_BAD_ARG // CUSTOM_MODES signalling not supported.
	}

	packet_size_cap := 1275
	if nbCompressedBytes > packet_size_cap {
		nbCompressedBytes = packet_size_cap
	}
	var vbr_rate opus_int32
	var effectiveBytes int
	if st.vbr != 0 && st.bitrate != OPUS_BITRATE_MAX {
		vbr_rate = bitrate_to_bits(st.bitrate, mode.Fs, opus_int32(frame_size)) << BITRES
		effectiveBytes = int(vbr_rate >> (3 + BITRES))
	} else {
		vbr_rate = 0
		tmp := st.bitrate * opus_int32(frame_size)
		if tell > 1 {
			tmp += tell * mode.Fs
		}
		if st.bitrate != OPUS_BITRATE_MAX {
			nbc := (tmp + 4*mode.Fs) / (8 * mode.Fs)
			if nbc < 2 {
				nbc = 2
			}
			if int(nbc) < nbCompressedBytes {
				nbCompressedBytes = int(nbc)
			}
			if enc != nil {
				ec_enc_shrink(enc, opus_uint32(nbCompressedBytes))
			}
		}
		effectiveBytes = nbCompressedBytes - nbFilledBytes
	}
	nbAvailableBytes := nbCompressedBytes - nbFilledBytes
	equiv_rate := opus_int32(nbCompressedBytes*8*50<<uint(3-LM)) - opus_int32((40*C+20)*((400>>LM)-50))
	if st.bitrate != OPUS_BITRATE_MAX {
		if lim := st.bitrate - opus_int32((40*C+20)*((400>>LM)-50)); lim < equiv_rate {
			equiv_rate = lim
		}
	}
	if enc == nil {
		ec_enc_init(&_enc, compressed, opus_uint32(nbCompressedBytes))
		enc = &_enc
	}
	if vbr_rate > 0 && st.constrained_vbr != 0 {
		vbr_bound := vbr_rate
		minAllowed := int(2)
		if tell != 1 {
			minAllowed = 0
		}
		max_allowed := int((vbr_rate + vbr_bound - st.vbr_reservoir) >> (BITRES + 3))
		if max_allowed < minAllowed {
			max_allowed = minAllowed
		}
		if max_allowed > nbAvailableBytes {
			max_allowed = nbAvailableBytes
		}
		if max_allowed < nbAvailableBytes {
			nbCompressedBytes = nbFilledBytes + max_allowed
			nbAvailableBytes = max_allowed
			ec_enc_shrink(enc, opus_uint32(nbCompressedBytes))
		}
	}
	total_bits := opus_int32(nbCompressedBytes * 8)
	effEnd := end
	if effEnd > mode.effEBands {
		effEnd = mode.effEBands
	}

	in := st.scratchIn[:CC*(N+overlap)]
	sample_max := MAX32(st.overlap_max, opus_val32(celt_maxabs_res(pcm, CC*(N-overlap)/st.upsample)))
	st.overlap_max = opus_val32(celt_maxabs_res(pcm[CC*(N-overlap)/st.upsample:], CC*overlap/st.upsample))
	sample_max = MAX32(sample_max, st.overlap_max)
	silence := 0
	if sample_max <= opus_val32(1)/float32(int(1)<<uint(st.lsb_depth)) {
		silence = 1
	}
	if tell == 1 {
		ec_enc_bit_logp(enc, silence, 15)
	} else {
		silence = 0
	}
	if silence != 0 {
		if vbr_rate > 0 {
			if nbFilledBytes+2 < nbCompressedBytes {
				nbCompressedBytes = nbFilledBytes + 2
				effectiveBytes = nbCompressedBytes
			}
			total_bits = opus_int32(nbCompressedBytes * 8)
			nbAvailableBytes = 2
			ec_enc_shrink(enc, opus_uint32(nbCompressedBytes))
		}
		// Pretend we've filled all the remaining bits with zeros.
		tell = total_bits
		enc.nbits_total += int(tell) - ec_tell(enc)
	}
	prefilter_mem := st.prefilter_mem
	for c := 0; c < CC; c++ {
		need_clip := 0
		if st.clip != 0 && sample_max > 65536.0 {
			need_clip = 1
		}
		celt_preemphasis(pcm[c:], in[c*(N+overlap)+overlap:], N, CC, st.upsample,
			mode.preemph[:], &st.preemph_memE[c], need_clip)
		// Pre-roll the overlap region from the prefilter memory tail.
		// prefilter_mem[(1+c)*COMBFILTER_MAXPERIOD-overlap .. +overlap]
		// copied into in[c*(N+overlap) .. +overlap]
		base := (1 + c) * COMBFILTER_MAXPERIOD
		copy(in[c*(N+overlap):c*(N+overlap)+overlap],
			prefilter_mem[base-overlap:base])
	}

	var toneishness opus_val32
	tone_freq := tone_detect(in, CC, N+overlap, &toneishness, mode.Fs)
	isTransient := 0
	shortBlocks := 0
	weak_transient := 0
	var tf_chan int
	if st.complexity >= 1 && st.lfe == 0 {
		allow_weak_transients := 0
		if hybrid != 0 && effectiveBytes < 15 && st.silk_info.signalType != 2 {
			allow_weak_transients = 1
		}
		isTransient = transient_analysis(in, N+overlap, CC,
			&tf_estimate, &tf_chan, allow_weak_transients, &weak_transient, tone_freq, toneishness,
			st.scratchTransient)
	}
	// toneishness = min(toneishness, 1 - tf_estimate)
	if t := opus_val32(1) - opus_val32(tf_estimate); t < toneishness {
		toneishness = t
	}

	// Pitch / prefilter.
	pitch_index := COMBFILTER_MINPERIOD
	var gain1 opus_val16
	var prefilter_tapset int
	var pf_on int
	pitch_change := 0
	{
		enabled := 0
		if ((st.lfe != 0 && nbAvailableBytes > 3) || nbAvailableBytes > 12*C) &&
			hybrid == 0 && silence == 0 && tell+16 <= total_bits && st.disable_pf == 0 {
			enabled = 1
		}
		prefilter_tapset = st.tapset_decision
		var qg int
		pf_on = run_prefilter(st, in, prefilter_mem, CC, N, prefilter_tapset,
			&pitch_index, &gain1, &qg, enabled, st.complexity, tf_estimate,
			nbAvailableBytes, &st.analysis, tone_freq, toneishness)
		// C: `pitch_index > 1.26*st->prefilter_period || pitch_index < .79*st->prefilter_period`.
		// `1.26` / `.79` are bare double literals; the compares run in
		// double precision (int promoted to double).
		pp := float64(st.prefilter_period)
		pi := float64(pitch_index)
		if (gain1 > 0.4 || st.prefilter_gain > 0.4) &&
			(st.analysis.valid == 0 || st.analysis.tonality > 0.3) &&
			(pi > 1.26*pp || pi < 0.79*pp) {
			pitch_change = 1
		}
		if pf_on == 0 {
			if hybrid == 0 && tell+16 <= total_bits {
				ec_enc_bit_logp(enc, 0, 1)
			}
		} else {
			ec_enc_bit_logp(enc, 1, 1)
			pitch_index += 1
			octave := ec_ilog(opus_uint32(pitch_index)) - 5
			ec_enc_uint(enc, opus_uint32(octave), 6)
			ec_enc_bits(enc, opus_uint32(pitch_index-(16<<octave)), 4+octave)
			pitch_index -= 1
			ec_enc_bits(enc, opus_uint32(qg), 3)
			ec_enc_icdf(enc, prefilter_tapset, tapset_icdf[:], 2)
		}
	}
	transient_got_disabled := 0
	if LM > 0 && opus_int32(ec_tell(enc))+3 <= total_bits {
		if isTransient != 0 {
			shortBlocks = M
		}
	} else {
		isTransient = 0
		transient_got_disabled = 1
	}

	freq := st.scratchFreq[:CC*N]
	bandE := st.scratchBandE[:nbEBands*CC]
	bandLogE := st.scratchBandLogE[:nbEBands*CC]

	secondMdct := 0
	if shortBlocks != 0 && st.complexity >= 8 {
		secondMdct = 1
	}
	bandLogE2 := st.scratchBandLogE2[:C*nbEBands]
	if secondMdct != 0 {
		compute_mdcts(mode, 0, in, freq, C, CC, LM, st.upsample, st.arch)
		compute_band_energies(mode, freq, bandE, effEnd, C, LM, st.arch)
		amp2Log2(mode, effEnd, end, bandE, bandLogE2, C)
		for c := 0; c < C; c++ {
			for i := 0; i < end; i++ {
				bandLogE2[nbEBands*c+i] += HALF32(celt_glog(LM))
			}
		}
	}
	compute_mdcts(mode, shortBlocks, in, freq, C, CC, LM, st.upsample, st.arch)
	if CC == 2 && C == 1 {
		tf_chan = 0
	}
	compute_band_energies(mode, freq, bandE, effEnd, C, LM, st.arch)
	if st.lfe != 0 {
		for i := 2; i < end; i++ {
			if lim := opus_val32(0.0001) * bandE[0]; bandE[i] > lim {
				bandE[i] = lim
			}
			bandE[i] = MAX32(bandE[i], EPSILON)
		}
	}
	amp2Log2(mode, effEnd, end, bandE, bandLogE, C)

	surround_dynalloc := st.scratchSurroundDynalloc[:C*nbEBands]
	for i := range surround_dynalloc {
		surround_dynalloc[i] = 0
	}
	var surround_masking, temporal_vbr, surround_trim celt_glog

	// Surround masking: energy_mask support omitted (encoder caller
	// typically leaves energy_mask NULL). The non-hybrid branch runs
	// only when st.energy_mask != nil, so skip entirely.

	// Temporal VBR (but not for LFE).
	if st.lfe == 0 {
		follow := -GCONST(10.0)
		var frame_avg opus_val32
		offset := celt_glog(0)
		if shortBlocks != 0 {
			offset = HALF32(celt_glog(LM))
		}
		for i := start; i < end; i++ {
			follow = MAXG(follow-GCONST(1.0), bandLogE[i]-offset)
			if C == 2 {
				follow = MAXG(follow, bandLogE[i+nbEBands]-offset)
			}
			frame_avg += opus_val32(follow)
		}
		frame_avg /= opus_val32(end - start)
		temporal_vbr = celt_glog(frame_avg) - st.spec_avg
		temporal_vbr = MING(GCONST(3.0), MAXG(-GCONST(1.5), temporal_vbr))
		st.spec_avg += GCONST(0.02) * temporal_vbr
	}

	if secondMdct == 0 {
		copy(bandLogE2[:C*nbEBands], bandLogE[:C*nbEBands])
	}

	// Last-chance transient patch.
	if LM > 0 && opus_int32(ec_tell(enc))+3 <= total_bits && isTransient == 0 &&
		st.complexity >= 5 && st.lfe == 0 && hybrid == 0 {
		if patch_transient_decision(bandLogE, oldBandE, nbEBands, start, end, C) != 0 {
			isTransient = 1
			shortBlocks = M
			compute_mdcts(mode, shortBlocks, in, freq, C, CC, LM, st.upsample, st.arch)
			compute_band_energies(mode, freq, bandE, effEnd, C, LM, st.arch)
			amp2Log2(mode, effEnd, end, bandE, bandLogE, C)
			for c := 0; c < C; c++ {
				for i := 0; i < end; i++ {
					bandLogE2[nbEBands*c+i] += HALF32(celt_glog(LM))
				}
			}
			tf_estimate = 0.2
		}
	}

	if LM > 0 && opus_int32(ec_tell(enc))+3 <= total_bits {
		ec_enc_bit_logp(enc, isTransient, 3)
	}

	X := st.scratchX[:C*N]
	normalise_bands(mode, freq, X, bandE, effEnd, C, M)

	enable_tf_analysis := 0
	if effectiveBytes >= 15*C && hybrid == 0 && st.complexity >= 2 &&
		st.lfe == 0 && toneishness < opus_val32(0.98) {
		enable_tf_analysis = 1
	}

	offsets := st.scratchOffsets[:nbEBands]
	for i := range offsets {
		offsets[i] = 0
	}
	importance := st.scratchImportance[:nbEBands]
	spread_weight := st.scratchSpreadWeight[:nbEBands]
	var tot_boost opus_int32
	maxDepth := dynalloc_analysis(bandLogE, bandLogE2, oldBandE, nbEBands,
		start, end, C, offsets, st.lsb_depth, mode.logN, isTransient,
		st.vbr, st.constrained_vbr, eBands, LM, effectiveBytes, &tot_boost,
		st.lfe, surround_dynalloc, &st.analysis, importance, spread_weight,
		tone_freq, toneishness)

	tf_res := st.scratchTfRes[:nbEBands]
	var tf_select int
	if enable_tf_analysis != 0 {
		lambda := IMAX(80, 20480/effectiveBytes+2)
		tf_select = tf_analysis(mode, effEnd, isTransient, tf_res, lambda,
			X, N, LM, tf_estimate, tf_chan, importance)
		for i := effEnd; i < end; i++ {
			tf_res[i] = tf_res[effEnd-1]
		}
	} else if hybrid != 0 && weak_transient != 0 {
		for i := 0; i < end; i++ {
			tf_res[i] = 1
		}
		tf_select = 0
	} else if hybrid != 0 && effectiveBytes < 15 && st.silk_info.signalType != 2 {
		for i := 0; i < end; i++ {
			tf_res[i] = 0
		}
		tf_select = isTransient
	} else {
		for i := 0; i < end; i++ {
			tf_res[i] = isTransient
		}
		tf_select = 0
	}

	errorArr := st.scratchErrorArr[:C*nbEBands]
	for i := range errorArr {
		errorArr[i] = 0
	}
	for c := 0; c < C; c++ {
		for i := start; i < end; i++ {
			if ABS32(bandLogE[i+c*nbEBands]-oldBandE[i+c*nbEBands]) < GCONST(2.0) {
				bandLogE[i+c*nbEBands] -= mul_f32(0.25, energyError[i+c*nbEBands])
			}
		}
	}
	quant_coarse_energy(mode, start, end, effEnd, bandLogE, oldBandE,
		opus_uint32(total_bits), errorArr, enc, C, LM, nbAvailableBytes,
		st.force_intra, &st.delayedIntra, boolToInt(st.complexity >= 4),
		st.loss_rate, st.lfe)

	tf_encode(start, end, isTransient, tf_res, LM, tf_select, enc)

	if opus_int32(ec_tell(enc))+4 <= total_bits {
		if st.lfe != 0 {
			st.tapset_decision = 0
			st.spread_decision = SPREAD_NORMAL
		} else if hybrid != 0 {
			if st.complexity == 0 {
				st.spread_decision = SPREAD_NONE
			} else if isTransient != 0 {
				st.spread_decision = SPREAD_NORMAL
			} else {
				st.spread_decision = SPREAD_AGGRESSIVE
			}
		} else if shortBlocks != 0 || st.complexity < 3 || nbAvailableBytes < 10*C {
			if st.complexity == 0 {
				st.spread_decision = SPREAD_NONE
			} else {
				st.spread_decision = SPREAD_NORMAL
			}
		} else {
			st.spread_decision = spreading_decision(mode, X,
				&st.tonal_average, st.spread_decision, &st.hf_average,
				&st.tapset_decision,
				boolToInt(pf_on != 0 && shortBlocks == 0),
				effEnd, C, M, spread_weight)
		}
		ec_enc_icdf(enc, st.spread_decision, spread_icdf[:], 5)
	} else {
		st.spread_decision = SPREAD_NORMAL
	}

	// For LFE, everything interesting is in the first band.
	if st.lfe != 0 {
		offsets[0] = IMIN(8, effectiveBytes/3)
	}
	cap_ := st.scratchCap[:nbEBands]
	init_caps(mode, cap_, LM, C)
	dynalloc_logp := 6
	total_bits <<= BITRES
	total_boost := opus_int32(0)
	tell = opus_int32(ec_tell_frac(enc))
	for i := start; i < end; i++ {
		width := C * int(eBands[i+1]-eBands[i]) << LM
		quanta := IMIN(width<<BITRES, IMAX(6<<BITRES, width))
		dynalloc_loop_logp := dynalloc_logp
		boost := 0
		j := 0
		for tell+opus_int32(dynalloc_loop_logp<<BITRES) < total_bits-total_boost && boost < cap_[i] {
			flag := 0
			if j < offsets[i] {
				flag = 1
			}
			ec_enc_bit_logp(enc, flag, dynalloc_loop_logp)
			tell = opus_int32(ec_tell_frac(enc))
			if flag == 0 {
				break
			}
			boost += quanta
			total_boost += opus_int32(quanta)
			dynalloc_loop_logp = 1
			j++
		}
		if j != 0 {
			dynalloc_logp = IMAX(2, dynalloc_logp-1)
		}
		offsets[i] = boost
	}

	dual_stereo := 0
	if C == 2 {
		intensity_thresholds := [21]opus_val16{
			1, 2, 3, 4, 5, 6, 7, 8, 16, 24, 36, 44, 50, 56, 62, 67, 72, 79, 88, 106, 134,
		}
		intensity_histeresis := [21]opus_val16{
			1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 3, 3, 4, 5, 6, 8, 8,
		}
		if LM != 0 {
			dual_stereo = stereo_analysis(mode, X, LM, N)
		}
		st.intensity = hysteresis_decision(opus_val16(equiv_rate/1000),
			intensity_thresholds[:], intensity_histeresis[:], 21, st.intensity)
		st.intensity = IMIN(end, IMAX(start, st.intensity))
	}

	alloc_trim := 5
	if tell+(6<<BITRES) <= total_bits-total_boost {
		if start > 0 || st.lfe != 0 {
			st.stereo_saving = 0
			alloc_trim = 5
		} else {
			alloc_trim = alloc_trim_analysis(mode, X, bandLogE, end, LM, C, N,
				&st.analysis, &st.stereo_saving, tf_estimate,
				st.intensity, surround_trim, equiv_rate, st.arch)
		}
		ec_enc_icdf(enc, alloc_trim, trim_icdf[:], 7)
		tell = opus_int32(ec_tell_frac(enc))
	}

	min_allowed := int((tell+total_boost+opus_int32(1<<(BITRES+3)-1))>>(BITRES+3)) + 2
	if hybrid != 0 {
		alt := int((tell0_frac + (37 << BITRES) + total_boost + opus_int32(1<<(BITRES+3)-1)) >> (BITRES + 3))
		if alt > min_allowed {
			min_allowed = alt
		}
	}

	if vbr_rate > 0 {
		lm_diff := mode.maxLM - LM
		if nbCompressedBytes > packet_size_cap>>uint(3-LM) {
			nbCompressedBytes = packet_size_cap >> uint(3-LM)
		}
		var base_target opus_int32
		if hybrid == 0 {
			base_target = vbr_rate - opus_int32((40*C+20)<<BITRES)
		} else {
			base_target = vbr_rate - opus_int32((9*C+4)<<BITRES)
			if base_target < 0 {
				base_target = 0
			}
		}
		if st.constrained_vbr != 0 {
			base_target += st.vbr_offset >> uint(lm_diff)
		}
		var target opus_int32
		if hybrid == 0 {
			target = compute_vbr(mode, &st.analysis, base_target, LM, equiv_rate,
				st.lastCodedBands, C, st.intensity, st.constrained_vbr,
				st.stereo_saving, int(tot_boost), tf_estimate, pitch_change, maxDepth,
				st.lfe, boolToInt(st.energy_mask != nil), surround_masking, temporal_vbr)
		} else {
			target = base_target
			if st.silk_info.offset < 100 {
				target += 12 << BITRES >> uint(3-LM)
			}
			if st.silk_info.offset > 100 {
				target -= 18 << BITRES >> uint(3-LM)
			}
			target += opus_int32(float32(tf_estimate-opus_val16(0.25)) * float32(50<<BITRES))
			if tf_estimate > opus_val16(0.7) && target < 50<<BITRES {
				target = 50 << BITRES
			}
		}
		target += tell
		nbAvailableBytes = int((target + opus_int32(1<<(BITRES+2))) >> (BITRES + 3))
		nbAvailableBytes = IMAX(min_allowed, nbAvailableBytes)
		nbAvailableBytes = IMIN(nbCompressedBytes, nbAvailableBytes)
		delta := target - vbr_rate
		target = opus_int32(nbAvailableBytes) << (BITRES + 3)
		if silence != 0 {
			nbAvailableBytes = 2
			target = 2 * 8 << BITRES
			delta = 0
		}
		var alpha opus_val16
		if st.vbr_count < 970 {
			st.vbr_count++
			alpha = 1.0 / opus_val16(st.vbr_count+20)
		} else {
			alpha = 0.001
		}
		if st.constrained_vbr != 0 {
			st.vbr_reservoir += target - vbr_rate
		}
		if st.constrained_vbr != 0 {
			st.vbr_drift += opus_int32(float32(alpha) *
				float32((delta*opus_int32(1<<uint(lm_diff)))-st.vbr_offset-st.vbr_drift))
			st.vbr_offset = -st.vbr_drift
		}
		if st.constrained_vbr != 0 && st.vbr_reservoir < 0 {
			adjust := int(-st.vbr_reservoir) / (8 << BITRES)
			if silence == 0 {
				nbAvailableBytes += adjust
			}
			st.vbr_reservoir = 0
		}
		if nbCompressedBytes > nbAvailableBytes {
			nbCompressedBytes = nbAvailableBytes
		}
		ec_enc_shrink(enc, opus_uint32(nbCompressedBytes))
	}

	// Bit allocation.
	fine_quant := st.scratchFineQuant[:nbEBands]
	pulses := st.scratchPulses[:nbEBands]
	fine_priority := st.scratchFinePriority[:nbEBands]
	bits := opus_int32(nbCompressedBytes*8)<<BITRES - opus_int32(ec_tell_frac(enc)) - 1
	anti_collapse_rsv := 0
	if isTransient != 0 && LM >= 2 && bits >= opus_int32((LM+2)<<BITRES) {
		anti_collapse_rsv = 1 << BITRES
	}
	bits -= opus_int32(anti_collapse_rsv)
	signalBandwidth := end - 1
	if st.analysis.valid != 0 {
		var min_bandwidth int
		if equiv_rate < 32000*opus_int32(C) {
			min_bandwidth = 13
		} else if equiv_rate < 48000*opus_int32(C) {
			min_bandwidth = 16
		} else if equiv_rate < 60000*opus_int32(C) {
			min_bandwidth = 18
		} else if equiv_rate < 80000*opus_int32(C) {
			min_bandwidth = 19
		} else {
			min_bandwidth = 20
		}
		signalBandwidth = IMAX(st.analysis.bandwidth, min_bandwidth)
	}
	if st.lfe != 0 {
		signalBandwidth = 1
	}
	var balance opus_int32
	codedBands := clt_compute_allocation(mode, start, end, offsets, cap_,
		alloc_trim, &st.intensity, &dual_stereo, bits, &balance, pulses,
		fine_quant, fine_priority, C, LM, enc, 1, st.lastCodedBands, signalBandwidth)
	if st.lastCodedBands != 0 {
		st.lastCodedBands = IMIN(st.lastCodedBands+1,
			IMAX(st.lastCodedBands-1, codedBands))
	} else {
		st.lastCodedBands = codedBands
	}
	quant_fine_energy(mode, start, end, oldBandE, errorArr, nil, fine_quant, enc, C)
	for i := range energyError[:nbEBands*CC] {
		energyError[i] = 0
	}

	collapse_masks := st.scratchCollapseMasks[:C*nbEBands]
	for i := range collapse_masks {
		collapse_masks[i] = 0
	}
	var Y []celt_norm
	if C == 2 {
		Y = X[N:]
	}
	quant_all_bands(1, mode, start, end, X, Y, collapse_masks,
		bandE, pulses, shortBlocks, st.spread_decision, dual_stereo,
		st.intensity, tf_res, opus_int32(nbCompressedBytes*(8<<BITRES))-opus_int32(anti_collapse_rsv),
		balance, enc, LM, codedBands, &st.rng, st.complexity, st.arch, st.disable_inv,
		st.scratchNorm, st.scratchHadamardTmp, st.scratchIy)

	var anti_collapse_on int
	if anti_collapse_rsv > 0 {
		if st.consec_transient < 2 {
			anti_collapse_on = 1
		}
		ec_enc_bits(enc, opus_uint32(anti_collapse_on), 1)
	}
	quant_energy_finalise(mode, start, end, oldBandE, errorArr, fine_quant,
		fine_priority, nbCompressedBytes*8-ec_tell(enc), enc, C)
	for c := 0; c < C; c++ {
		for i := start; i < end; i++ {
			energyError[i+c*nbEBands] = MAXG(-GCONST(0.5), MING(GCONST(0.5), errorArr[i+c*nbEBands]))
		}
	}
	if silence != 0 {
		for i := 0; i < C*nbEBands; i++ {
			oldBandE[i] = -GCONST(28.0)
		}
	}

	st.prefilter_period = pitch_index
	st.prefilter_gain = gain1
	st.prefilter_tapset = prefilter_tapset

	if CC == 2 && C == 1 {
		copy(oldBandE[nbEBands:2*nbEBands], oldBandE[:nbEBands])
	}
	if isTransient == 0 {
		copy(oldLogE2, oldLogE[:CC*nbEBands])
		copy(oldLogE, oldBandE[:CC*nbEBands])
	} else {
		for i := 0; i < CC*nbEBands; i++ {
			oldLogE[i] = MING(oldLogE[i], oldBandE[i])
		}
	}
	for c := 0; c < CC; c++ {
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
	if isTransient != 0 || transient_got_disabled != 0 {
		st.consec_transient++
	} else {
		st.consec_transient = 0
	}
	st.rng = enc.rng
	ec_enc_done(enc)
	if ec_get_error(enc) != 0 {
		return OPUS_INTERNAL_ERROR
	}
	return nbCompressedBytes
}
