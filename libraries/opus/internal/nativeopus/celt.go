package nativeopus

// Port of libopus/celt/celt.h + celt.c (shared CELT helpers).
// Float-path, non-QEXT, scalar C fallback (no ARM/SSE intrinsics).
//
// Deferred / skipped:
//   - comb_filter_qext (ENABLE_QEXT).
//   - OPUS_ARM_ASM unrolled comb_filter_const variant (we ship the
//     portable C reference fallback which produces identical results
//     under -ffp-contract=off).
//   - Custom-mode-only toOpus/fromOpus tables.

// CELT constants. C: celt.h:61-237.
const (
	QEXT_EXTENSION_ID    = 124
	LEAK_BANDS           = 19
	COMBFILTER_MAXPERIOD = 1024
	COMBFILTER_MINPERIOD = 15
)

// AnalysisInfo — C: celt.h:65-79.
type AnalysisInfo struct {
	valid                int
	tonality             float32
	tonality_slope       float32
	noisiness            float32
	activity             float32
	music_prob           float32
	music_prob_min       float32
	music_prob_max       float32
	bandwidth            int
	activity_probability float32
	max_pitch_ratio      float32
	leak_boost           [LEAK_BANDS]byte
}

// SILKInfo — C: celt.h:81-84.
type SILKInfo struct {
	signalType int
	offset     int
}

// trim_icdf — allocation trim PDF. C: celt.h:194.
var trim_icdf = [11]byte{126, 124, 119, 109, 87, 41, 19, 9, 4, 2, 0}

// spread_icdf — spreading decision PDF. Probs NONE=21.875%,
// LIGHT=6.25%, NORMAL=65.625%, AGGRESSIVE=6.25%. C: celt.h:196.
var spread_icdf = [4]byte{25, 23, 2, 0}

// tapset_icdf — C: celt.h:198.
var tapset_icdf = [3]byte{2, 1, 0}

// tf_select_table — TF-change lookup. C: celt.c:320-326.
// Indexed as [LM][4*isTransient + 2*tf_select + per_band_flag].
var tf_select_table = [4][8]int8{
	{0, -1, 0, -1, 0, -1, 0, -1}, // 2.5 ms
	{0, -1, 0, -2, 1, 0, 1, -1},  // 5 ms
	{0, -2, 0, -3, 2, 0, 1, -1},  // 10 ms
	{0, -2, 0, -3, 3, 0, 1, -1},  // 20 ms
}

// resampling_factor — C: celt.c:62-93.
func resampling_factor(rate opus_int32) int {
	switch rate {
	case 48000:
		return 1
	case 24000:
		return 2
	case 16000:
		return 3
	case 12000:
		return 4
	case 8000:
		return 6
	default:
		celt_assert(false)
		return 0
	}
}

// comb_filter_const_c — constant-tap comb filter. The C fallback path
// (no OPUS_ARM_ASM). C: celt.c:166-193.
//
// `x` is the input slice; the filter reads x[i-T-2 .. i+N-1], so
// callers must pass a view that starts T+2 samples before the actual
// data being filtered.
func comb_filter_const_c(y []opus_val32, x []opus_val32, xOff, T, N int,
	g10, g11, g12 celt_coef) {
	// Running tap values; the original C uses negative indexing.
	x4 := x[xOff-T-2]
	x3 := x[xOff-T-1]
	x2 := x[xOff-T]
	x1 := x[xOff-T+1]
	for i := 0; i < N; i++ {
		x0 := x[xOff+i-T+2]
		// y[i] = x[i] + g10*x2 + g11*(x1+x3) + g12*(x0+x4)
		// Pin to non-fused via add_f32 / mul_f32 so we match the C
		// oracle under -ffp-contract=off.
		t := add_f32(x[xOff+i], mul_f32(g10, x2))
		t = add_f32(t, mul_f32(g11, add_f32(x1, x3)))
		t = add_f32(t, mul_f32(g12, add_f32(x0, x4)))
		y[i] = t
		x4 = x3
		x3 = x2
		x2 = x1
		x1 = x0
	}
}

// comb_filter — pitch-based comb filter with a windowed transition
// between (T0,g0,tapset0) and (T1,g1,tapset1). C: celt.c:238-314
// (float, non-QEXT path).
//
// `x` and `y` are slices that hold [-T-2 .. N-1]-indexed data; `xOff`
// / `yOff` point to the "logical zero" sample so negative indices map
// to x[xOff-k].
func comb_filter(y []opus_val32, yOff int, x []opus_val32, xOff int,
	T0, T1, N int, g0, g1 opus_val16, tapset0, tapset1 int,
	window []celt_coef, overlap, arch int) {
	_ = arch
	// Tapset gain tables. C: celt.c:246-249.
	gains := [3][3]opus_val16{
		{0.3066406250, 0.2170410156, 0.1296386719},
		{0.4638671875, 0.2680664062, 0.0},
		{0.7998046875, 0.1000976562, 0.0},
	}
	if g0 == 0 && g1 == 0 {
		// Pass-through. C has `if (x!=y) OPUS_MOVE(...)`; Go's copy is
		// safe as a no-op when the slices alias at the same offset.
		copy(y[yOff:yOff+N], x[xOff:xOff+N])
		return
	}
	// When the gain is zero, T0 and/or T1 is set to zero. We need to
	// have them be at least 2 to avoid processing garbage data.
	T0 = IMAX(T0, COMBFILTER_MINPERIOD)
	T1 = IMAX(T1, COMBFILTER_MINPERIOD)
	g00 := mul_f32(g0, gains[tapset0][0])
	g01 := mul_f32(g0, gains[tapset0][1])
	g02 := mul_f32(g0, gains[tapset0][2])
	g10 := mul_f32(g1, gains[tapset1][0])
	g11 := mul_f32(g1, gains[tapset1][1])
	g12 := mul_f32(g1, gains[tapset1][2])
	x1 := x[xOff-T1+1]
	x2 := x[xOff-T1]
	x3 := x[xOff-T1-1]
	x4 := x[xOff-T1-2]
	// If the filter didn't change, we don't need the overlap.
	if g0 == g1 && T0 == T1 && tapset0 == tapset1 {
		overlap = 0
	}
	var i int
	for i = 0; i < overlap; i++ {
		x0 := x[xOff+i-T1+2]
		f := mul_f32(window[i], window[i])
		oneMinusF := sub_f32(COEF_ONE, f)
		// y[i] = x[i]
		//      + (1-f)*g00 * x[i-T0]
		//      + (1-f)*g01 * (x[i-T0+1]+x[i-T0-1])
		//      + (1-f)*g02 * (x[i-T0+2]+x[i-T0-2])
		//      + f*g10 * x2
		//      + f*g11 * (x1+x3)
		//      + f*g12 * (x0+x4)
		t := add_f32(x[xOff+i], mul_f32(mul_f32(oneMinusF, g00), x[xOff+i-T0]))
		t = add_f32(t, mul_f32(mul_f32(oneMinusF, g01),
			add_f32(x[xOff+i-T0+1], x[xOff+i-T0-1])))
		t = add_f32(t, mul_f32(mul_f32(oneMinusF, g02),
			add_f32(x[xOff+i-T0+2], x[xOff+i-T0-2])))
		t = add_f32(t, mul_f32(mul_f32(f, g10), x2))
		t = add_f32(t, mul_f32(mul_f32(f, g11), add_f32(x1, x3)))
		t = add_f32(t, mul_f32(mul_f32(f, g12), add_f32(x0, x4)))
		y[yOff+i] = t
		x4 = x3
		x3 = x2
		x2 = x1
		x1 = x0
	}
	if g1 == 0 {
		copy(y[yOff+overlap:yOff+N], x[xOff+overlap:xOff+N])
		return
	}
	// Compute the constant-filter tail.
	comb_filter_const_c(y[yOff+i:], x, xOff+i, T1, N-i, g10, g11, g12)
}

// init_caps — C: celt.c:329-338.
func init_caps(m *OpusCustomMode, cap []int, LM, C int) {
	for i := 0; i < m.nbEBands; i++ {
		N := int(m.eBands[i+1]-m.eBands[i]) << LM
		cap[i] = (int(m.cache.caps[m.nbEBands*(2*LM+C-1)+i]) + 64) * C * N >> 2
	}
}

// bits_to_bitrate — C: celt.h:147-149.
func bits_to_bitrate(bits, Fs, frame_size opus_int32) opus_int32 {
	return bits * (6 * Fs / frame_size) / 6
}

// bitrate_to_bits — C: celt.h:151-153.
func bitrate_to_bits(bitrate, Fs, frame_size opus_int32) opus_int32 {
	return bitrate * 6 / (6 * Fs / frame_size)
}
