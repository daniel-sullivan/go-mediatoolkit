package nativeopus

import "math"

// Port of libopus/src/analysis.h + libopus/src/analysis.c — float build
// (FIXED_POINT undefined, DISABLE_FLOAT_API undefined).
//
// Literal 1:1 translation of the C source. Every `a ± b*c` expression
// wraps through fma_add / fma_sub so arm64 Go does not fuse a
// FMADDS that the clang oracle (-ffp-contract=off) will not. Bare
// arithmetic near multiplications also routes through mul_f32 /
// add_f32 / sub_f32 so the non-fused rounding rules apply. Unsuffixed
// C literals that originate as doubles (e.g. `.5` without `f`,
// `M_PI`, etc.) are translated as float64 and narrowed at the cast
// boundary exactly as the C compiler does.

// ─── Constants ──────────────────────────────────────────────────────

// C: analysis.h:35-42.
const (
	NB_FRAMES          = 8
	NB_TBANDS          = 18
	ANALYSIS_BUF_SIZE  = 720
	ANALYSIS_COUNT_MAX = 10000
	DETECT_SIZE        = 100

	NB_TONAL_SKIP_BANDS = 9
	TRANSITION_PENALTY  = 10
)

// C: analysis.c:415-416.
const (
	LEAKAGE_OFFSET float32 = 2.5
	LEAKAGE_SLOPE  float32 = 2.0
)

// dct_table — 8x16 DCT matrix, used for BFCC computation.
// C: analysis.c:57-74.
var dct_table = [128]float32{
	0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000,
	0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000,
	0.351851, 0.338330, 0.311806, 0.273300, 0.224292, 0.166664, 0.102631, 0.034654,
	-0.034654, -0.102631, -0.166664, -0.224292, -0.273300, -0.311806, -0.338330, -0.351851,
	0.346760, 0.293969, 0.196424, 0.068975, -0.068975, -0.196424, -0.293969, -0.346760,
	-0.346760, -0.293969, -0.196424, -0.068975, 0.068975, 0.196424, 0.293969, 0.346760,
	0.338330, 0.224292, 0.034654, -0.166664, -0.311806, -0.351851, -0.273300, -0.102631,
	0.102631, 0.273300, 0.351851, 0.311806, 0.166664, -0.034654, -0.224292, -0.338330,
	0.326641, 0.135299, -0.135299, -0.326641, -0.326641, -0.135299, 0.135299, 0.326641,
	0.326641, 0.135299, -0.135299, -0.326641, -0.326641, -0.135299, 0.135299, 0.326641,
	0.311806, 0.034654, -0.273300, -0.338330, -0.102631, 0.224292, 0.351851, 0.166664,
	-0.166664, -0.351851, -0.224292, 0.102631, 0.338330, 0.273300, -0.034654, -0.311806,
	0.293969, -0.068975, -0.346760, -0.196424, 0.196424, 0.346760, 0.068975, -0.293969,
	-0.293969, 0.068975, 0.346760, 0.196424, -0.196424, -0.346760, -0.068975, 0.293969,
	0.273300, -0.166664, -0.338330, 0.034654, 0.351851, 0.102631, -0.311806, -0.224292,
	0.224292, 0.311806, -0.102631, -0.351851, -0.034654, 0.338330, 0.166664, -0.273300,
}

// analysis_window — 240-sample half window (C: analysis.c:76-107).
var analysis_window = [240]float32{
	0.000043, 0.000171, 0.000385, 0.000685, 0.001071, 0.001541, 0.002098, 0.002739,
	0.003466, 0.004278, 0.005174, 0.006156, 0.007222, 0.008373, 0.009607, 0.010926,
	0.012329, 0.013815, 0.015385, 0.017037, 0.018772, 0.020590, 0.022490, 0.024472,
	0.026535, 0.028679, 0.030904, 0.033210, 0.035595, 0.038060, 0.040604, 0.043227,
	0.045928, 0.048707, 0.051564, 0.054497, 0.057506, 0.060591, 0.063752, 0.066987,
	0.070297, 0.073680, 0.077136, 0.080665, 0.084265, 0.087937, 0.091679, 0.095492,
	0.099373, 0.103323, 0.107342, 0.111427, 0.115579, 0.119797, 0.124080, 0.128428,
	0.132839, 0.137313, 0.141849, 0.146447, 0.151105, 0.155823, 0.160600, 0.165435,
	0.170327, 0.175276, 0.180280, 0.185340, 0.190453, 0.195619, 0.200838, 0.206107,
	0.211427, 0.216797, 0.222215, 0.227680, 0.233193, 0.238751, 0.244353, 0.250000,
	0.255689, 0.261421, 0.267193, 0.273005, 0.278856, 0.284744, 0.290670, 0.296632,
	0.302628, 0.308658, 0.314721, 0.320816, 0.326941, 0.333097, 0.339280, 0.345492,
	0.351729, 0.357992, 0.364280, 0.370590, 0.376923, 0.383277, 0.389651, 0.396044,
	0.402455, 0.408882, 0.415325, 0.421783, 0.428254, 0.434737, 0.441231, 0.447736,
	0.454249, 0.460770, 0.467298, 0.473832, 0.480370, 0.486912, 0.493455, 0.500000,
	0.506545, 0.513088, 0.519630, 0.526168, 0.532702, 0.539230, 0.545751, 0.552264,
	0.558769, 0.565263, 0.571746, 0.578217, 0.584675, 0.591118, 0.597545, 0.603956,
	0.610349, 0.616723, 0.623077, 0.629410, 0.635720, 0.642008, 0.648271, 0.654508,
	0.660720, 0.666903, 0.673059, 0.679184, 0.685279, 0.691342, 0.697372, 0.703368,
	0.709330, 0.715256, 0.721144, 0.726995, 0.732807, 0.738579, 0.744311, 0.750000,
	0.755647, 0.761249, 0.766807, 0.772320, 0.777785, 0.783203, 0.788573, 0.793893,
	0.799162, 0.804381, 0.809547, 0.814660, 0.819720, 0.824724, 0.829673, 0.834565,
	0.839400, 0.844177, 0.848895, 0.853553, 0.858151, 0.862687, 0.867161, 0.871572,
	0.875920, 0.880203, 0.884421, 0.888573, 0.892658, 0.896677, 0.900627, 0.904508,
	0.908321, 0.912063, 0.915735, 0.919335, 0.922864, 0.926320, 0.929703, 0.933013,
	0.936248, 0.939409, 0.942494, 0.945503, 0.948436, 0.951293, 0.954072, 0.956773,
	0.959396, 0.961940, 0.964405, 0.966790, 0.969096, 0.971321, 0.973465, 0.975528,
	0.977510, 0.979410, 0.981228, 0.982963, 0.984615, 0.986185, 0.987671, 0.989074,
	0.990393, 0.991627, 0.992778, 0.993844, 0.994826, 0.995722, 0.996534, 0.997261,
	0.997902, 0.998459, 0.998929, 0.999315, 0.999615, 0.999829, 0.999957, 1.000000,
}

// tbands — tonal band boundaries (C: analysis.c:109-111).
var tbands = [NB_TBANDS + 1]int{
	4, 8, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 80, 96, 112, 136, 160, 192, 240,
}

// std_feature_bias — tuned MLP feature offsets. C: analysis.c:410-413.
var std_feature_bias = [9]float32{
	5.684947, 3.475288, 1.770634, 1.599784, 3.773215,
	2.163313, 1.260756, 1.116868, 1.918795,
}

// ─── Types ──────────────────────────────────────────────────────────

// TonalityAnalysisState — C: analysis.h:47-81.
//
// Struct-layout note: the C source uses
// `#define TONALITY_ANALYSIS_RESET_START angle` and zeroes every field
// from `angle` onwards in tonality_analysis_reset. Our Go reset helper
// below zeroes exactly the same range.
type TonalityAnalysisState struct {
	arch        int
	application int
	Fs          opus_int32

	angle              [240]float32
	d_angle            [240]float32
	d2_angle           [240]float32
	inmem              [ANALYSIS_BUF_SIZE]opus_val32
	mem_fill           int
	prev_band_tonality [NB_TBANDS]float32
	prev_tonality      float32
	prev_bandwidth     int
	E                  [NB_FRAMES][NB_TBANDS]float32
	logE               [NB_FRAMES][NB_TBANDS]float32
	lowE               [NB_TBANDS]float32
	highE              [NB_TBANDS]float32
	meanE              [NB_TBANDS + 1]float32
	mem                [32]float32
	cmean              [8]float32
	std                [9]float32
	Etracker           float32
	lowECount          float32
	E_count            int
	count              int
	analysis_offset    int
	write_pos          int
	read_pos           int
	read_subframe      int
	hp_ener_accum      float32
	initialized        int
	rnn_state          [MAX_NEURONS]float32
	downmix_state      [3]opus_val32
	info               [DETECT_SIZE]AnalysisInfo
}

// downmix_func — C: opus_private.h:175.
//
//	typedef void (*downmix_func)(const void *, opus_val32 *, int, int,
//	                             int, int, int);
type downmix_func func(x interface{}, y []opus_val32, subframe, offset, c1, c2, C int)

// ─── opus_select_arch / is_digital_silence helpers ──────────────────

// opus_select_arch — C: celt/cpu_support.h:67-70 (no SSE/ARM fallback).
func opus_select_arch() int { return 0 }

// is_digital_silence — C: opus_encoder.c:1060-1077 (float branch).
// `pcm` is the float buffer already downmixed; `lsb_depth` is the bit
// depth the caller reported.
func is_digital_silence(pcm []opus_res, frame_size, channels, lsb_depth int) int {
	sample_max := celt_maxabs32(pcm, frame_size*channels)
	threshold := opus_val16(1) / opus_val16(opus_int32(1)<<lsb_depth)
	if sample_max <= opus_val32(threshold) {
		return 1
	}
	return 0
}

// is_digital_silence32 — float-build indirection from analysis.c:442.
func is_digital_silence32(pcm []opus_val32, frame_size, channels, lsb_depth int) int {
	return is_digital_silence(pcm, frame_size, channels, lsb_depth)
}

// SCALE_ENER — float build (C: analysis.c:424).
//
//	#define SCALE_ENER(e) ((1.f/32768/32768)*e)
func SCALE_ENER(e float32) float32 {
	return mul_f32(mul_f32(mul_f32(1.0/32768, 1.0/32768), 1), e)
}

// ─── silk_resampler_down2_hp ────────────────────────────────────────

// silk_resampler_down2_hp — downsample-by-2 with high-pass tap output
// used to compute an HF energy tracker for bandwidth detection.
// C: analysis.c:115-163 (float branch).
//
// Every multiply routes through mul_f32 and every add/sub through
// add_f32/sub_f32 so Go's arm64 backend can't fuse the MULT16_32_Q15
// result into the surrounding ADD32 — that would produce an FMADDS
// that the C oracle (-ffp-contract=off) doesn't emit.
func silk_resampler_down2_hp(S []opus_val32, out []opus_val32, in []opus_val32, inLen int) opus_val32 {
	var k int
	len2 := inLen / 2
	var in32, out32, out32_hp, Y, X opus_val32
	var hp_ener opus_val64 = 0
	for k = 0; k < len2; k++ {
		in32 = in[2*k]

		// All-pass section for even input sample.
		Y = sub_f32(in32, S[0])
		X = mul_f32(0.6074371, Y)
		out32 = add_f32(S[0], X)
		S[0] = add_f32(in32, X)
		out32_hp = out32
		in32 = in[2*k+1]

		// All-pass section for odd input sample, and add to output of previous section
		Y = sub_f32(in32, S[1])
		X = mul_f32(0.15063, Y)
		out32 = add_f32(out32, S[1])
		out32 = add_f32(out32, X)
		S[1] = add_f32(in32, X)

		Y = sub_f32(-in32, S[2])
		X = mul_f32(0.15063, Y)
		out32_hp = add_f32(out32_hp, S[2])
		out32_hp = add_f32(out32_hp, X)
		S[2] = add_f32(-in32, X)

		// len2 can be up to 480, so we shift by 8 to make it fit.
		// Float build: opus_val64 is float32; SHR64 is a no-op.
		// hp_ener += out32_hp*(opus_val64)out32_hp
		hp_ener = opus_val64(add_f32(float32(hp_ener), mul_f32(out32_hp, out32_hp)))
		out[k] = mul_f32(0.5, out32)
	}
	return opus_val32(hp_ener)
}

// ─── downmix_and_resample ───────────────────────────────────────────

// downmix_and_resample — C: analysis.c:165-214 (float branch).
func downmix_and_resample(downmix downmix_func, x interface{}, y []opus_val32,
	S []opus_val32, subframe, offset, c1, c2, C, Fs int) opus_val32 {
	var j int
	var ret opus_val32 = 0

	if subframe == 0 {
		return 0
	}
	if Fs == 48000 {
		subframe *= 2
		offset *= 2
	} else if Fs == 16000 {
		subframe = subframe * 2 / 3
		offset = offset * 2 / 3
	} else if Fs != 24000 {
		celt_assert(false)
	}
	tmp := make([]opus_val32, subframe)

	downmix(x, tmp, subframe, offset, c1, c2, C)
	if (c2 == -2 && C == 2) || c2 > -1 {
		for j = 0; j < subframe; j++ {
			tmp[j] = HALF32(tmp[j])
		}
	}
	if Fs == 48000 {
		ret = silk_resampler_down2_hp(S, y, tmp, subframe)
	} else if Fs == 24000 {
		OPUS_COPY(y, tmp, subframe)
	} else if Fs == 16000 {
		tmp3x := make([]opus_val32, 3*subframe)
		// Don't do this at home! This resampler is horrible and it's
		// only (barely) usable for the purpose of the analysis because
		// we don't care about all the aliasing between 8 kHz and 12 kHz.
		for j = 0; j < subframe; j++ {
			tmp3x[3*j] = tmp[j]
			tmp3x[3*j+1] = tmp[j]
			tmp3x[3*j+2] = tmp[j]
		}
		silk_resampler_down2_hp(S, y, tmp3x, 3*subframe)
	}
	// Float build: ret *= 1.f/32768/32768
	ret = mul_f32(ret, mul_f32(1.0/32768, 1.0/32768))
	return ret
}

// ─── tonality_analysis_init / tonality_analysis_reset ───────────────

// tonality_analysis_init — C: analysis.c:216-223.
func tonality_analysis_init(tonal *TonalityAnalysisState, Fs opus_int32) {
	// Initialize reusable fields.
	tonal.arch = opus_select_arch()
	tonal.Fs = Fs
	// Clear remaining fields.
	tonality_analysis_reset(tonal)
}

// tonality_analysis_reset — C: analysis.c:225-230.
//
// In C this uses OPUS_CLEAR starting from TONALITY_ANALYSIS_RESET_START
// (`angle`) through the end of the struct. In Go we explicitly zero
// every field from `angle` onwards.
func tonality_analysis_reset(tonal *TonalityAnalysisState) {
	tonal.angle = [240]float32{}
	tonal.d_angle = [240]float32{}
	tonal.d2_angle = [240]float32{}
	tonal.inmem = [ANALYSIS_BUF_SIZE]opus_val32{}
	tonal.mem_fill = 0
	tonal.prev_band_tonality = [NB_TBANDS]float32{}
	tonal.prev_tonality = 0
	tonal.prev_bandwidth = 0
	tonal.E = [NB_FRAMES][NB_TBANDS]float32{}
	tonal.logE = [NB_FRAMES][NB_TBANDS]float32{}
	tonal.lowE = [NB_TBANDS]float32{}
	tonal.highE = [NB_TBANDS]float32{}
	tonal.meanE = [NB_TBANDS + 1]float32{}
	tonal.mem = [32]float32{}
	tonal.cmean = [8]float32{}
	tonal.std = [9]float32{}
	tonal.Etracker = 0
	tonal.lowECount = 0
	tonal.E_count = 0
	tonal.count = 0
	tonal.analysis_offset = 0
	tonal.write_pos = 0
	tonal.read_pos = 0
	tonal.read_subframe = 0
	tonal.hp_ener_accum = 0
	tonal.initialized = 0
	tonal.rnn_state = [MAX_NEURONS]float32{}
	tonal.downmix_state = [3]opus_val32{}
	tonal.info = [DETECT_SIZE]AnalysisInfo{}
}

// ─── tonality_get_info ──────────────────────────────────────────────

// tonality_get_info — C: analysis.c:232-408.
func tonality_get_info(tonal *TonalityAnalysisState, info_out *AnalysisInfo, length int) {
	var pos int
	var curr_lookahead int
	var tonality_max float32
	var tonality_avg float32
	var tonality_count int
	var i int
	var pos0 int
	var prob_avg float32
	var prob_count float32
	var prob_min, prob_max float32
	var vad_prob float32
	var mpos, vpos int
	var bandwidth_span int

	pos = tonal.read_pos
	curr_lookahead = tonal.write_pos - tonal.read_pos
	if curr_lookahead < 0 {
		curr_lookahead += DETECT_SIZE
	}

	tonal.read_subframe += length / (int(tonal.Fs) / 400)
	for tonal.read_subframe >= 8 {
		tonal.read_subframe -= 8
		tonal.read_pos++
	}
	if tonal.read_pos >= DETECT_SIZE {
		tonal.read_pos -= DETECT_SIZE
	}

	// On long frames, look at the second analysis window rather than the first.
	if length > int(tonal.Fs)/50 && pos != tonal.write_pos {
		pos++
		if pos == DETECT_SIZE {
			pos = 0
		}
	}
	if pos == tonal.write_pos {
		pos--
	}
	if pos < 0 {
		pos = DETECT_SIZE - 1
	}
	pos0 = pos
	*info_out = tonal.info[pos]
	if info_out.valid == 0 {
		return
	}
	tonality_max = info_out.tonality
	tonality_avg = info_out.tonality
	tonality_count = 1
	// Look at the neighbouring frames and pick largest bandwidth found.
	bandwidth_span = 6
	// If possible, look ahead for a tone to compensate for the delay in the tone detector.
	for i = 0; i < 3; i++ {
		pos++
		if pos == DETECT_SIZE {
			pos = 0
		}
		if pos == tonal.write_pos {
			break
		}
		tonality_max = MAX32(tonality_max, tonal.info[pos].tonality)
		tonality_avg = add_f32(tonality_avg, tonal.info[pos].tonality)
		tonality_count++
		info_out.bandwidth = IMAX(info_out.bandwidth, tonal.info[pos].bandwidth)
		bandwidth_span--
	}
	pos = pos0
	// Look back in time to see if any has a wider bandwidth than the current frame.
	for i = 0; i < bandwidth_span; i++ {
		pos--
		if pos < 0 {
			pos = DETECT_SIZE - 1
		}
		if pos == tonal.write_pos {
			break
		}
		info_out.bandwidth = IMAX(info_out.bandwidth, tonal.info[pos].bandwidth)
	}
	// info_out->tonality = MAX32(tonality_avg/tonality_count, tonality_max-.2f);
	info_out.tonality = MAX32(tonality_avg/float32(tonality_count),
		sub_f32(tonality_max, 0.2))

	mpos = pos0
	vpos = pos0
	// Compensate for ~5-frame music-prob delay and ~1-frame VAD delay.
	if curr_lookahead > 15 {
		mpos += 5
		if mpos >= DETECT_SIZE {
			mpos -= DETECT_SIZE
		}
		vpos += 1
		if vpos >= DETECT_SIZE {
			vpos -= DETECT_SIZE
		}
	}

	prob_min = 1.0
	prob_max = 0.0
	vad_prob = tonal.info[vpos].activity_probability
	prob_count = MAX16(0.1, vad_prob)
	// prob_avg = MAX16(.1f, vad_prob)*tonal->info[mpos].music_prob
	prob_avg = mul_f32(MAX16(0.1, vad_prob), tonal.info[mpos].music_prob)
	for {
		var pos_vad float32
		mpos++
		if mpos == DETECT_SIZE {
			mpos = 0
		}
		if mpos == tonal.write_pos {
			break
		}
		vpos++
		if vpos == DETECT_SIZE {
			vpos = 0
		}
		if vpos == tonal.write_pos {
			break
		}
		pos_vad = tonal.info[vpos].activity_probability
		// prob_min = MIN16((prob_avg - TRANSITION_PENALTY*(vad_prob - pos_vad))/prob_count, prob_min)
		prob_min = MIN16(
			sub_f32(prob_avg,
				mul_f32(float32(TRANSITION_PENALTY), sub_f32(vad_prob, pos_vad)))/prob_count,
			prob_min)
		// prob_max = MAX16((prob_avg + TRANSITION_PENALTY*(vad_prob - pos_vad))/prob_count, prob_max)
		prob_max = MAX16(
			add_f32(prob_avg,
				mul_f32(float32(TRANSITION_PENALTY), sub_f32(vad_prob, pos_vad)))/prob_count,
			prob_max)
		// prob_count += MAX16(.1f, pos_vad)
		prob_count = add_f32(prob_count, MAX16(0.1, pos_vad))
		// prob_avg += MAX16(.1f, pos_vad)*tonal->info[mpos].music_prob
		prob_avg = fma_add(prob_avg, MAX16(0.1, pos_vad), tonal.info[mpos].music_prob)
	}
	info_out.music_prob = prob_avg / prob_count
	prob_min = MIN16(prob_avg/prob_count, prob_min)
	prob_max = MAX16(prob_avg/prob_count, prob_max)
	prob_min = MAX16(prob_min, 0.0)
	prob_max = MIN16(prob_max, 1.0)

	// If we don't have enough look-ahead, do our best to make a decent decision.
	if curr_lookahead < 10 {
		var pmin, pmax float32
		pmin = prob_min
		pmax = prob_max
		pos = pos0
		// Look for min/max in the past.
		limit := IMIN(tonal.count-1, 15)
		for i = 0; i < limit; i++ {
			pos--
			if pos < 0 {
				pos = DETECT_SIZE - 1
			}
			pmin = MIN16(pmin, tonal.info[pos].music_prob)
			pmax = MAX16(pmax, tonal.info[pos].music_prob)
		}
		// Bias against switching on active audio.
		// pmin = MAX16(0.f, pmin - .1f*vad_prob)
		pmin = MAX16(0.0, sub_f32(pmin, mul_f32(0.1, vad_prob)))
		// pmax = MIN16(1.f, pmax + .1f*vad_prob)
		pmax = MIN16(1.0, add_f32(pmax, mul_f32(0.1, vad_prob)))
		// prob_min += (1.f - .1f*curr_lookahead) * (pmin - prob_min)
		prob_min = fma_add(prob_min,
			sub_f32(1.0, mul_f32(0.1, float32(curr_lookahead))),
			sub_f32(pmin, prob_min))
		// prob_max += (1.f - .1f*curr_lookahead) * (pmax - prob_max)
		prob_max = fma_add(prob_max,
			sub_f32(1.0, mul_f32(0.1, float32(curr_lookahead))),
			sub_f32(pmax, prob_max))
	}
	info_out.music_prob_min = prob_min
	info_out.music_prob_max = prob_max
}

// ─── tonality_analysis ──────────────────────────────────────────────

// tonality_analysis — main per-frame analysis worker. C: analysis.c:445-952.
func tonality_analysis(tonal *TonalityAnalysisState, celt_mode *CELTMode, x interface{},
	length, offset, c1, c2, C, lsb_depth int, downmix downmix_func) {
	var i, b int
	var kfft *kiss_fft_state
	const N = 480
	const N2 = 240
	A := tonal.angle[:]
	dA := tonal.d_angle[:]
	d2A := tonal.d2_angle[:]
	var band_tonality [NB_TBANDS]float32
	var logE [NB_TBANDS]float32
	var BFCC [8]float32
	var features [25]float32
	var frame_tonality float32
	var max_frame_tonality float32
	var frame_noisiness float32
	// pi4 = (float)(M_PI*M_PI*M_PI*M_PI) — evaluated in double then cast to float.
	const Mpi = 3.141592653
	pi4 := float32(Mpi * Mpi * Mpi * Mpi)
	var slope float32 = 0
	var frame_stationarity float32
	var relativeE float32
	var frame_probs [2]float32
	var alpha, alphaE, alphaE2 float32
	var frame_loudness float32
	var bandwidth_mask float32
	var is_masked [NB_TBANDS + 1]int
	var bandwidth int = 0
	var maxE float32 = 0
	var noise_floor float32
	var remaining int
	var info *AnalysisInfo
	var hp_ener float32
	var tonality2 [240]float32
	var midE [8]float32
	var spec_variability float32 = 0
	var band_log2 [NB_TBANDS + 1]float32
	var leakage_from [NB_TBANDS + 1]float32
	var leakage_to [NB_TBANDS + 1]float32
	var layer_out [MAX_NEURONS]float32
	var below_max_pitch float32
	var above_max_pitch float32
	var is_silence int

	if tonal.initialized == 0 {
		tonal.mem_fill = 240
		tonal.initialized = 1
	}
	alpha = 1.0 / float32(IMIN(10, 1+tonal.count))
	alphaE = 1.0 / float32(IMIN(25, 1+tonal.count))
	// Noise floor related decay for bandwidth detection: -2.2 dB/second
	alphaE2 = 1.0 / float32(IMIN(100, 1+tonal.count))
	if tonal.count <= 1 {
		alphaE2 = 1
	}

	if tonal.Fs == 48000 {
		// len and offset are now at 24 kHz.
		length /= 2
		offset /= 2
	} else if tonal.Fs == 16000 {
		length = 3 * length / 2
		offset = 3 * offset / 2
	}

	kfft = celt_mode.mdct.kfft[0]
	tonal.hp_ener_accum = add_f32(tonal.hp_ener_accum,
		float32(downmix_and_resample(downmix, x,
			tonal.inmem[tonal.mem_fill:], tonal.downmix_state[:],
			IMIN(length, ANALYSIS_BUF_SIZE-tonal.mem_fill), offset, c1, c2, C, int(tonal.Fs))))
	if tonal.mem_fill+length < ANALYSIS_BUF_SIZE {
		tonal.mem_fill += length
		// Don't have enough to update the analysis
		return
	}
	hp_ener = tonal.hp_ener_accum
	info = &tonal.info[tonal.write_pos]
	tonal.write_pos++
	if tonal.write_pos >= DETECT_SIZE {
		tonal.write_pos -= DETECT_SIZE
	}

	is_silence = is_digital_silence32(tonal.inmem[:], ANALYSIS_BUF_SIZE, 1, lsb_depth)

	in := make([]kiss_fft_cpx, 480)
	out := make([]kiss_fft_cpx, 480)
	var tonality [240]float32
	var noisiness [240]float32
	for i = 0; i < N2; i++ {
		w := analysis_window[i]
		in[i].r = kiss_fft_scalar(mul_f32(w, tonal.inmem[i]))
		in[i].i = kiss_fft_scalar(mul_f32(w, tonal.inmem[N2+i]))
		in[N-i-1].r = kiss_fft_scalar(mul_f32(w, tonal.inmem[N-i-1]))
		in[N-i-1].i = kiss_fft_scalar(mul_f32(w, tonal.inmem[N+N2-i-1]))
	}
	OPUS_MOVE(tonal.inmem[:], tonal.inmem[ANALYSIS_BUF_SIZE-240:], 240)
	remaining = length - (ANALYSIS_BUF_SIZE - tonal.mem_fill)
	tonal.hp_ener_accum = float32(downmix_and_resample(downmix, x,
		tonal.inmem[240:], tonal.downmix_state[:], remaining,
		offset+ANALYSIS_BUF_SIZE-tonal.mem_fill, c1, c2, C, int(tonal.Fs)))
	tonal.mem_fill = 240 + remaining
	if is_silence != 0 {
		// On silence, copy the previous analysis.
		prev_pos := tonal.write_pos - 2
		if prev_pos < 0 {
			prev_pos += DETECT_SIZE
		}
		*info = tonal.info[prev_pos]
		return
	}
	opus_fft(kfft, in, out, tonal.arch)
	// If there's any NaN on the input, the entire output will be NaN,
	// so we only need to check one value.
	if celt_isnan(out[0].r) != 0 {
		info.valid = 0
		return
	}

	for i = 1; i < N2; i++ {
		var X1r, X2r, X1i, X2i float32
		var angle, d_angle, d2_angle float32
		var angle2, d_angle2, d2_angle2 float32
		var mod1, mod2, avg_mod float32
		X1r = add_f32(out[i].r, out[N-i].r)
		X1i = sub_f32(out[i].i, out[N-i].i)
		X2r = add_f32(out[i].i, out[N-i].i)
		X2i = sub_f32(out[N-i].r, out[i].r)

		// angle = (float)(.5f/M_PI)*fast_atan2f(X1i, X1r)
		// .5f / M_PI — `.5f` is float; `M_PI` is a double macro; in C the
		// computation is done in double, then cast to float. We mirror by
		// computing in float64 and narrowing.
		const halfOverPi = float32(0.5 / Mpi)
		angle = mul_f32(halfOverPi, fast_atan2f(X1i, X1r))
		d_angle = sub_f32(angle, A[i])
		d2_angle = sub_f32(d_angle, dA[i])

		angle2 = mul_f32(halfOverPi, fast_atan2f(X2i, X2r))
		d_angle2 = sub_f32(angle2, angle)
		d2_angle2 = sub_f32(d_angle2, d_angle)

		// mod1 = d2_angle - (float)float2int(d2_angle)
		mod1 = sub_f32(d2_angle, float32(float2int(d2_angle)))
		noisiness[i] = ABS16(mod1)
		mod1 = mul_f32(mod1, mod1)
		mod1 = mul_f32(mod1, mod1)

		mod2 = sub_f32(d2_angle2, float32(float2int(d2_angle2)))
		noisiness[i] = add_f32(noisiness[i], ABS16(mod2))
		mod2 = mul_f32(mod2, mod2)
		mod2 = mul_f32(mod2, mod2)

		// avg_mod = .25f * (d2A[i] + mod1 + 2*mod2)
		// C: left-to-right associativity: ((d2A[i] + mod1) + 2*mod2)
		tmpA := add_f32(d2A[i], mod1)
		tmpB := fma_add(tmpA, 2, mod2)
		avg_mod = mul_f32(0.25, tmpB)
		// tonality[i] = 1.f/(1.f + 40.f*16.f*pi4*avg_mod) - .015f
		denom := fma_add(1.0, mul_f32(mul_f32(40.0, 16.0), pi4), avg_mod)
		tonality[i] = sub_f32(1.0/denom, 0.015)
		// tonality2[i] = 1.f/(1.f + 40.f*16.f*pi4*mod2) - .015f
		denom2 := fma_add(1.0, mul_f32(mul_f32(40.0, 16.0), pi4), mod2)
		tonality2[i] = sub_f32(1.0/denom2, 0.015)

		A[i] = angle2
		dA[i] = d_angle2
		d2A[i] = mod2
	}
	for i = 2; i < N2-1; i++ {
		tt := MIN32(tonality2[i], MAX32(tonality2[i-1], tonality2[i+1]))
		// tonality[i] = .9f * MAX32(tonality[i], tt-.1f)
		tonality[i] = mul_f32(0.9, MAX32(tonality[i], sub_f32(tt, 0.1)))
	}
	frame_tonality = 0
	max_frame_tonality = 0
	info.activity = 0
	frame_noisiness = 0
	frame_stationarity = 0
	if tonal.count == 0 {
		for b = 0; b < NB_TBANDS; b++ {
			tonal.lowE[b] = 1e10
			tonal.highE[b] = -1e10
		}
	}
	relativeE = 0
	frame_loudness = 0
	// The energy of the very first band is special because of DC.
	{
		var E float32 = 0
		var X1r, X2r float32
		X1r = mul_f32(2, out[0].r)
		X2r = mul_f32(2, out[0].i)
		// E = X1r*X1r + X2r*X2r
		E = add_f32(mul_f32(X1r, X1r), mul_f32(X2r, X2r))
		for i = 1; i < 4; i++ {
			// binE = out[i].r*out[i].r + out[N-i].r*out[N-i].r
			//      + out[i].i*out[i].i + out[N-i].i*out[N-i].i
			t0 := mul_f32(out[i].r, out[i].r)
			t0 = fma_add(t0, out[N-i].r, out[N-i].r)
			t0 = fma_add(t0, out[i].i, out[i].i)
			t0 = fma_add(t0, out[N-i].i, out[N-i].i)
			E = add_f32(E, t0)
		}
		E = SCALE_ENER(E)
		// band_log2[0] = .5f * 1.442695f * (float)log(E + 1e-10f)
		logv := float32(math.Log(float64(add_f32(E, 1e-10))))
		band_log2[0] = mul_f32(mul_f32(0.5, 1.442695), logv)
	}
	for b = 0; b < NB_TBANDS; b++ {
		var E, tE, nE float32 = 0, 0, 0
		var L1, L2 float32
		var stationarity float32
		for i = tbands[b]; i < tbands[b+1]; i++ {
			// binE = out[i].r*out[i].r + out[N-i].r*out[N-i].r
			//      + out[i].i*out[i].i + out[N-i].i*out[N-i].i
			binE := mul_f32(out[i].r, out[i].r)
			binE = fma_add(binE, out[N-i].r, out[N-i].r)
			binE = fma_add(binE, out[i].i, out[i].i)
			binE = fma_add(binE, out[N-i].i, out[N-i].i)
			binE = SCALE_ENER(binE)
			E = add_f32(E, binE)
			// tE += binE * MAX32(0, tonality[i])
			tE = fma_add(tE, binE, MAX32(0, tonality[i]))
			// nE += binE * 2.f * (.5f - noisiness[i])
			nE = fma_add(nE, mul_f32(binE, 2.0), sub_f32(0.5, noisiness[i]))
		}
		// Check for extreme band energies that could cause NaNs later.
		if !(E < 1e9) || celt_isnan(E) != 0 {
			info.valid = 0
			return
		}

		tonal.E[tonal.E_count][b] = E
		// frame_noisiness += nE / (1e-15f + E)
		frame_noisiness = add_f32(frame_noisiness, nE/add_f32(1e-15, E))

		// frame_loudness += (float)sqrt(E + 1e-10f)
		frame_loudness = add_f32(frame_loudness,
			float32(math.Sqrt(float64(add_f32(E, 1e-10)))))
		logE[b] = float32(math.Log(float64(add_f32(E, 1e-10))))
		// band_log2[b+1] = .5f * 1.442695f * (float)log(E + 1e-10f)
		band_log2[b+1] = mul_f32(mul_f32(0.5, 1.442695), logE[b])
		tonal.logE[tonal.E_count][b] = logE[b]
		if tonal.count == 0 {
			tonal.highE[b] = logE[b]
			tonal.lowE[b] = logE[b]
		}
		// if (tonal->highE[b] > tonal->lowE[b] + 7.5)
		// Note: 7.5 is a double literal; compiled as compare in double,
		// result unaffected since float compares promote.
		if float64(tonal.highE[b]) > float64(tonal.lowE[b])+7.5 {
			if sub_f32(tonal.highE[b], logE[b]) > sub_f32(logE[b], tonal.lowE[b]) {
				tonal.highE[b] = sub_f32(tonal.highE[b], 0.01)
			} else {
				tonal.lowE[b] = add_f32(tonal.lowE[b], 0.01)
			}
		}
		if logE[b] > tonal.highE[b] {
			tonal.highE[b] = logE[b]
			// tonal->lowE[b] = MAX32(tonal->highE[b] - 15, tonal->lowE[b])
			tonal.lowE[b] = MAX32(sub_f32(tonal.highE[b], 15), tonal.lowE[b])
		} else if logE[b] < tonal.lowE[b] {
			tonal.lowE[b] = logE[b]
			tonal.highE[b] = MIN32(add_f32(tonal.lowE[b], 15), tonal.highE[b])
		}
		// relativeE += (logE[b] - tonal->lowE[b]) / (1e-5f + (tonal->highE[b] - tonal->lowE[b]))
		relativeE = add_f32(relativeE,
			sub_f32(logE[b], tonal.lowE[b])/
				add_f32(1e-5, sub_f32(tonal.highE[b], tonal.lowE[b])))

		L1 = 0
		L2 = 0
		for i = 0; i < NB_FRAMES; i++ {
			L1 = add_f32(L1, float32(math.Sqrt(float64(tonal.E[i][b]))))
			L2 = add_f32(L2, tonal.E[i][b])
		}

		// stationarity = MIN16(0.99f, L1 / (float)sqrt(1e-15 + NB_FRAMES*L2))
		// 1e-15 is a double literal; NB_FRAMES*L2 is float (L2 is float,
		// NB_FRAMES int) — C promotes to double before sqrt.
		denomD := 1e-15 + float64(NB_FRAMES)*float64(L2)
		stationarity = MIN16(0.99, L1/float32(math.Sqrt(denomD)))
		stationarity = mul_f32(stationarity, stationarity)
		stationarity = mul_f32(stationarity, stationarity)
		frame_stationarity = add_f32(frame_stationarity, stationarity)
		// band_tonality[b] = MAX16(tE/(1e-15f+E), stationarity*tonal->prev_band_tonality[b])
		band_tonality[b] = MAX16(
			tE/add_f32(1e-15, E),
			mul_f32(stationarity, tonal.prev_band_tonality[b]))
		frame_tonality = add_f32(frame_tonality, band_tonality[b])
		if b >= NB_TBANDS-NB_TONAL_SKIP_BANDS {
			frame_tonality = sub_f32(frame_tonality,
				band_tonality[b-NB_TBANDS+NB_TONAL_SKIP_BANDS])
		}
		// max_frame_tonality = MAX16(max_frame_tonality, (1.f + .03f*(b-NB_TBANDS))*frame_tonality)
		scale := fma_add(1.0, 0.03, float32(b-NB_TBANDS))
		max_frame_tonality = MAX16(max_frame_tonality, mul_f32(scale, frame_tonality))
		// slope += band_tonality[b] * (b-8)
		slope = fma_add(slope, band_tonality[b], float32(b-8))
		tonal.prev_band_tonality[b] = band_tonality[b]
	}

	leakage_from[0] = band_log2[0]
	leakage_to[0] = sub_f32(band_log2[0], LEAKAGE_OFFSET)
	for b = 1; b < NB_TBANDS+1; b++ {
		// leak_slope = LEAKAGE_SLOPE * (tbands[b] - tbands[b-1]) / 4
		leak_slope := mul_f32(LEAKAGE_SLOPE, float32(tbands[b]-tbands[b-1])) / 4
		leakage_from[b] = MIN16(add_f32(leakage_from[b-1], leak_slope), band_log2[b])
		leakage_to[b] = MAX16(sub_f32(leakage_to[b-1], leak_slope),
			sub_f32(band_log2[b], LEAKAGE_OFFSET))
	}
	for b = NB_TBANDS - 2; b >= 0; b-- {
		leak_slope := mul_f32(LEAKAGE_SLOPE, float32(tbands[b+1]-tbands[b])) / 4
		leakage_from[b] = MIN16(add_f32(leakage_from[b+1], leak_slope), leakage_from[b])
		leakage_to[b] = MAX16(sub_f32(leakage_to[b+1], leak_slope), leakage_to[b])
	}
	celt_assert(NB_TBANDS+1 <= LEAK_BANDS)
	for b = 0; b < NB_TBANDS+1; b++ {
		// boost = MAX16(0, leakage_to[b] - band_log2[b])
		//       + MAX16(0, band_log2[b] - (leakage_from[b] + LEAKAGE_OFFSET))
		boost := add_f32(
			MAX16(0, sub_f32(leakage_to[b], band_log2[b])),
			MAX16(0, sub_f32(band_log2[b], add_f32(leakage_from[b], LEAKAGE_OFFSET))))
		// info->leak_boost[b] = IMIN(255, (int)floor(.5 + 64.f*boost))
		// .5 is a double literal; 64.f*boost is float; sum done in double.
		v := int(math.Floor(0.5 + 64.0*float64(boost)))
		info.leak_boost[b] = byte(IMIN(255, v))
	}
	// The original C loops continues with `b` (value after the above loop).
	for ; b < LEAK_BANDS; b++ {
		info.leak_boost[b] = 0
	}

	for i = 0; i < NB_FRAMES; i++ {
		var mindist float32 = 1e15
		for j := 0; j < NB_FRAMES; j++ {
			var dist float32 = 0
			for k := 0; k < NB_TBANDS; k++ {
				tmp := sub_f32(tonal.logE[i][k], tonal.logE[j][k])
				// dist += tmp*tmp
				dist = fma_add(dist, tmp, tmp)
			}
			if j != i {
				mindist = MIN32(mindist, dist)
			}
		}
		spec_variability = add_f32(spec_variability, mindist)
	}
	// spec_variability = (float)sqrt(spec_variability / NB_FRAMES / NB_TBANDS)
	// C: division is float (NB_FRAMES/NB_TBANDS are int macros, promoted
	// to float via a float / int). Then narrowed+promoted at sqrt call.
	specDivFloat := spec_variability / float32(NB_FRAMES) / float32(NB_TBANDS)
	spec_variability = float32(math.Sqrt(float64(specDivFloat)))
	bandwidth_mask = 0
	bandwidth = 0
	maxE = 0
	// noise_floor = 5.7e-4f / (1 << IMAX(0, lsb_depth-8))
	noise_floor = 5.7e-4 / float32(int(1)<<IMAX(0, lsb_depth-8))
	noise_floor = mul_f32(noise_floor, noise_floor)
	below_max_pitch = 0
	above_max_pitch = 0
	for b = 0; b < NB_TBANDS; b++ {
		var E float32 = 0
		var Em float32
		var band_start, band_end int
		// Keep a margin of 300 Hz for aliasing
		band_start = tbands[b]
		band_end = tbands[b+1]
		for i = band_start; i < band_end; i++ {
			binE := mul_f32(out[i].r, out[i].r)
			binE = fma_add(binE, out[N-i].r, out[N-i].r)
			binE = fma_add(binE, out[i].i, out[i].i)
			binE = fma_add(binE, out[N-i].i, out[N-i].i)
			E = add_f32(E, binE)
		}
		E = SCALE_ENER(E)
		maxE = MAX32(maxE, E)
		if band_start < 64 {
			below_max_pitch = add_f32(below_max_pitch, E)
		} else {
			above_max_pitch = add_f32(above_max_pitch, E)
		}
		// tonal->meanE[b] = MAX32((1-alphaE2)*tonal->meanE[b], E)
		tonal.meanE[b] = MAX32(mul_f32(sub_f32(1, alphaE2), tonal.meanE[b]), E)
		Em = MAX32(E, tonal.meanE[b])
		// 1) E*1e9f > maxE
		// 2) Em > 3*noise_floor*(band_end-band_start) || E > noise_floor*(band_end-band_start)
		if mul_f32(E, 1e9) > maxE &&
			(Em > mul_f32(mul_f32(3, noise_floor), float32(band_end-band_start)) ||
				E > mul_f32(noise_floor, float32(band_end-band_start))) {
			bandwidth = b + 1
		}
		// Check if the band is masked.
		var maskCoef float32
		if tonal.prev_bandwidth >= b+1 {
			maskCoef = 0.01
		} else {
			maskCoef = 0.05
		}
		if E < mul_f32(maskCoef, bandwidth_mask) {
			is_masked[b] = 1
		} else {
			is_masked[b] = 0
		}
		// Use a simple follower with 13 dB/Bark slope for spreading function.
		bandwidth_mask = MAX32(mul_f32(0.05, bandwidth_mask), E)
	}
	// Special case for the last two bands.
	if tonal.Fs == 48000 {
		var noise_ratio float32
		var Em float32
		// E = hp_ener * (1.f / (60*60))
		E := mul_f32(hp_ener, 1.0/float32(60*60))
		if tonal.prev_bandwidth == 20 {
			noise_ratio = 10.0
		} else {
			noise_ratio = 30.0
		}

		above_max_pitch = add_f32(above_max_pitch, E)
		tonal.meanE[b] = MAX32(mul_f32(sub_f32(1, alphaE2), tonal.meanE[b]), E)
		Em = MAX32(E, tonal.meanE[b])
		if Em > mul_f32(mul_f32(mul_f32(3, noise_ratio), noise_floor), 160) ||
			E > mul_f32(mul_f32(noise_ratio, noise_floor), 160) {
			bandwidth = 20
		}
		// Check if the band is masked.
		var maskCoef float32
		if tonal.prev_bandwidth == 20 {
			maskCoef = 0.01
		} else {
			maskCoef = 0.05
		}
		if E < mul_f32(maskCoef, bandwidth_mask) {
			is_masked[b] = 1
		} else {
			is_masked[b] = 0
		}
	}
	if above_max_pitch > below_max_pitch {
		info.max_pitch_ratio = below_max_pitch / above_max_pitch
	} else {
		info.max_pitch_ratio = 1
	}
	// In some cases, resampling aliasing can create a small amount of
	// energy in the first band being cut. So if the last band is masked,
	// we don't include it.
	if bandwidth == 20 && is_masked[NB_TBANDS] != 0 {
		bandwidth -= 2
	} else if bandwidth > 0 && bandwidth <= NB_TBANDS && is_masked[bandwidth-1] != 0 {
		bandwidth--
	}
	if tonal.count <= 2 {
		bandwidth = 20
	}
	// frame_loudness = 20 * (float)log10(frame_loudness)
	frame_loudness = mul_f32(20, float32(math.Log10(float64(frame_loudness))))
	tonal.Etracker = MAX32(sub_f32(tonal.Etracker, 0.003), frame_loudness)
	// tonal->lowECount *= (1 - alphaE)
	tonal.lowECount = mul_f32(tonal.lowECount, sub_f32(1, alphaE))
	if frame_loudness < sub_f32(tonal.Etracker, 30) {
		tonal.lowECount = add_f32(tonal.lowECount, alphaE)
	}

	for i = 0; i < 8; i++ {
		var sum float32 = 0
		for b = 0; b < 16; b++ {
			// sum += dct_table[i*16+b] * logE[b]
			sum = fma_add(sum, dct_table[i*16+b], logE[b])
		}
		BFCC[i] = sum
	}
	for i = 0; i < 8; i++ {
		var sum float32 = 0
		for b = 0; b < 16; b++ {
			// sum += dct_table[i*16+b] * .5f * (tonal->highE[b]+tonal->lowE[b])
			sum = fma_add(sum, mul_f32(dct_table[i*16+b], 0.5),
				add_f32(tonal.highE[b], tonal.lowE[b]))
		}
		midE[i] = sum
	}

	frame_stationarity = frame_stationarity / float32(NB_TBANDS)
	relativeE = relativeE / float32(NB_TBANDS)
	if tonal.count < 10 {
		relativeE = 0.5
	}
	frame_noisiness = frame_noisiness / float32(NB_TBANDS)
	// info->activity = frame_noisiness + (1-frame_noisiness)*relativeE
	info.activity = fma_add(frame_noisiness, sub_f32(1, frame_noisiness), relativeE)
	// frame_tonality = max_frame_tonality / (NB_TBANDS - NB_TONAL_SKIP_BANDS)
	frame_tonality = max_frame_tonality / float32(NB_TBANDS-NB_TONAL_SKIP_BANDS)
	// frame_tonality = MAX16(frame_tonality, tonal->prev_tonality*.8f)
	frame_tonality = MAX16(frame_tonality, mul_f32(tonal.prev_tonality, 0.8))
	tonal.prev_tonality = frame_tonality

	slope = slope / float32(8*8)
	info.tonality_slope = slope

	tonal.E_count = (tonal.E_count + 1) % NB_FRAMES
	tonal.count = IMIN(tonal.count+1, ANALYSIS_COUNT_MAX)
	info.tonality = frame_tonality

	for i = 0; i < 4; i++ {
		// features[i] = -0.12299f*(BFCC[i]+tonal->mem[i+24])
		//             + 0.49195f*(tonal->mem[i]+tonal->mem[i+16])
		//             + 0.69693f*tonal->mem[i+8]
		//             - 1.4349f*tonal->cmean[i]
		t0 := mul_f32(-0.12299, add_f32(BFCC[i], tonal.mem[i+24]))
		t1 := fma_add(t0, 0.49195, add_f32(tonal.mem[i], tonal.mem[i+16]))
		t2 := fma_add(t1, 0.69693, tonal.mem[i+8])
		features[i] = fma_sub(t2, 1.4349, tonal.cmean[i])
	}

	for i = 0; i < 4; i++ {
		// tonal->cmean[i] = (1-alpha)*tonal->cmean[i] + alpha*BFCC[i]
		tonal.cmean[i] = fma_add(
			mul_f32(sub_f32(1, alpha), tonal.cmean[i]),
			alpha, BFCC[i])
	}

	for i = 0; i < 4; i++ {
		// features[4+i] = 0.63246f*(BFCC[i]-tonal->mem[i+24])
		//               + 0.31623f*(tonal->mem[i]-tonal->mem[i+16])
		t0 := mul_f32(0.63246, sub_f32(BFCC[i], tonal.mem[i+24]))
		features[4+i] = fma_add(t0, 0.31623, sub_f32(tonal.mem[i], tonal.mem[i+16]))
	}
	for i = 0; i < 3; i++ {
		// features[8+i] = 0.53452f*(BFCC[i]+tonal->mem[i+24])
		//               - 0.26726f*(tonal->mem[i]+tonal->mem[i+16])
		//               - 0.53452f*tonal->mem[i+8]
		t0 := mul_f32(0.53452, add_f32(BFCC[i], tonal.mem[i+24]))
		t1 := fma_sub(t0, 0.26726, add_f32(tonal.mem[i], tonal.mem[i+16]))
		features[8+i] = fma_sub(t1, 0.53452, tonal.mem[i+8])
	}

	if tonal.count > 5 {
		for i = 0; i < 9; i++ {
			// tonal->std[i] = (1-alpha)*tonal->std[i] + alpha*features[i]*features[i]
			t0 := mul_f32(sub_f32(1, alpha), tonal.std[i])
			// alpha*features[i] evaluated first, then *features[i] — left-to-right.
			// C: ((1-alpha)*tonal->std[i]) + (alpha*features[i])*features[i]
			// Go: alpha*features[i] computed, then multiplied by features[i],
			// then added to t0.
			inner := mul_f32(mul_f32(alpha, features[i]), features[i])
			tonal.std[i] = add_f32(t0, inner)
		}
	}
	for i = 0; i < 4; i++ {
		features[i] = sub_f32(BFCC[i], midE[i])
	}

	for i = 0; i < 8; i++ {
		tonal.mem[i+24] = tonal.mem[i+16]
		tonal.mem[i+16] = tonal.mem[i+8]
		tonal.mem[i+8] = tonal.mem[i]
		tonal.mem[i] = BFCC[i]
	}
	for i = 0; i < 9; i++ {
		// features[11+i] = (float)sqrt(tonal->std[i]) - std_feature_bias[i]
		features[11+i] = sub_f32(
			float32(math.Sqrt(float64(tonal.std[i]))),
			std_feature_bias[i])
	}
	features[18] = sub_f32(spec_variability, 0.78)
	features[20] = sub_f32(info.tonality, 0.154723)
	features[21] = sub_f32(info.activity, 0.724643)
	features[22] = sub_f32(frame_stationarity, 0.743717)
	features[23] = add_f32(info.tonality_slope, 0.069216)
	features[24] = sub_f32(tonal.lowECount, 0.067930)

	analysis_compute_dense(&layer0, layer_out[:], features[:])
	analysis_compute_gru(&layer1, tonal.rnn_state[:], layer_out[:])
	analysis_compute_dense(&layer2, frame_probs[:], tonal.rnn_state[:])

	// Probability of speech or music vs noise
	info.activity_probability = frame_probs[1]
	info.music_prob = frame_probs[0]

	info.bandwidth = bandwidth
	tonal.prev_bandwidth = bandwidth
	info.noisiness = frame_noisiness
	info.valid = 1
}

// ─── run_analysis ───────────────────────────────────────────────────

// run_analysis — C: analysis.c:954-980.
func run_analysis(analysis *TonalityAnalysisState, celt_mode *CELTMode, analysis_pcm interface{},
	analysis_frame_size, frame_size, c1, c2, C int, Fs opus_int32,
	lsb_depth int, downmix downmix_func, analysis_info *AnalysisInfo) {
	var offset int
	var pcm_len int

	analysis_frame_size -= analysis_frame_size & 1
	if analysis_pcm != nil {
		// Avoid overflow/wrap-around of the analysis buffer.
		analysis_frame_size = IMIN((DETECT_SIZE-5)*int(Fs)/50, analysis_frame_size)

		pcm_len = analysis_frame_size - analysis.analysis_offset
		offset = analysis.analysis_offset
		for pcm_len > 0 {
			tonality_analysis(analysis, celt_mode, analysis_pcm,
				IMIN(int(Fs)/50, pcm_len), offset, c1, c2, C, lsb_depth, downmix)
			offset += int(Fs) / 50
			pcm_len -= int(Fs) / 50
		}
		analysis.analysis_offset = analysis_frame_size

		analysis.analysis_offset -= frame_size
	}

	tonality_get_info(analysis, analysis_info, frame_size)
}

// ─── Downmix callbacks ──────────────────────────────────────────────

// downmix_float — C: opus_encoder.c:748-778 (float branch).
func downmix_float(x interface{}, y []opus_val32, subframe, offset, c1, c2, C int) {
	data := x.([]float32)
	var j int
	for j = 0; j < subframe; j++ {
		y[j] = FLOAT2SIG(data[(j+offset)*C+c1])
	}
	if c2 > -1 {
		for j = 0; j < subframe; j++ {
			y[j] = add_f32(y[j], FLOAT2SIG(data[(j+offset)*C+c2]))
		}
	} else if c2 == -2 {
		for c := 1; c < C; c++ {
			for j = 0; j < subframe; j++ {
				y[j] = add_f32(y[j], FLOAT2SIG(data[(j+offset)*C+c]))
			}
		}
	}
	// Cap signal to +6 dBFS to avoid problems in the analysis.
	for j = 0; j < subframe; j++ {
		if y[j] < -65536.0 {
			y[j] = -65536.0
		}
		if y[j] > 65536.0 {
			y[j] = 65536.0
		}
		if celt_isnan(y[j]) != 0 {
			y[j] = 0
		}
	}
}

// downmix_int — C: opus_encoder.c:781-802.
func downmix_int(x interface{}, y []opus_val32, subframe, offset, c1, c2, C int) {
	data := x.([]opus_int16)
	var j int
	for j = 0; j < subframe; j++ {
		y[j] = INT16TOSIG(data[(j+offset)*C+c1])
	}
	if c2 > -1 {
		for j = 0; j < subframe; j++ {
			y[j] = add_f32(y[j], INT16TOSIG(data[(j+offset)*C+c2]))
		}
	} else if c2 == -2 {
		for c := 1; c < C; c++ {
			for j = 0; j < subframe; j++ {
				y[j] = add_f32(y[j], INT16TOSIG(data[(j+offset)*C+c]))
			}
		}
	}
}
