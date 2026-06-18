package nativeopus

// Port of libopus/celt/pitch.h (static-inline helpers) and pitch.c.
// Float-path only.
//
// fma_add / fma_sub convention (see fma.go):
//   fma_add(addend, mul1, mul2) → addend + mul1*mul2 (FMADDS)
//   fma_sub(addend, mul1, mul2) → addend - mul1*mul2 (FMSUBS)
// Every `MAC16_16(c, a, b) = c + a*b` in the C source therefore maps
// to `fma_add(c, a, b)` here.

// xcorr_kernel_c — sliding 4-way cross-correlation.
// C: pitch.h:65-129.
//
// On arm64 dispatches to a NEON assembly kernel (see
// xcorr_kernel_arm64.s) that processes all 4 lanes in parallel with
// separately-rounded FMUL.4S + FADD.4S — bit-exact vs the scalar body
// below. On other architectures falls through to the scalar path.
func xcorr_kernel_c(x, y []opus_val16, sum []opus_val32, ln int) {
	celt_assert(ln >= 3)
	if xcorrSIMDAvailable && ln > 0 && len(sum) >= 4 {
		// The asm kernel consumes ln x samples and reads y[0..ln+2] —
		// same range as the scalar body below.
		sumArr := (*[4]opus_val32)(sum[:4])
		xcorrKernelSIMD(&x[0], &y[0], sumArr, ln)
		return
	}
	xcorr_kernel_scalar(x, y, sum, ln)
}

// xcorr_kernel_scalar — the Go scalar body. Kept for non-arm64 builds
// and as a cross-check reference; each `sum[k] + mul_f32(tmp, y_x)`
// is an inline FADD on a //go:noinline mul_f32 return, so SSA cannot
// fuse to FMADDS.
//
//go:noinline
func xcorr_kernel_scalar(x, y []opus_val16, sum []opus_val32, ln int) {
	var y_0, y_1, y_2, y_3 opus_val16
	celt_assert(ln >= 3)
	y_3 = 0
	yi := 0
	xi := 0
	y_0 = y[yi]
	yi++
	y_1 = y[yi]
	yi++
	y_2 = y[yi]
	yi++
	var j int
	for j = 0; j < ln-3; j += 4 {
		var tmp opus_val16
		tmp = x[xi]
		xi++
		y_3 = y[yi]
		yi++
		sum[0] = sum[0] + mul_f32(tmp, y_0)
		sum[1] = sum[1] + mul_f32(tmp, y_1)
		sum[2] = sum[2] + mul_f32(tmp, y_2)
		sum[3] = sum[3] + mul_f32(tmp, y_3)
		tmp = x[xi]
		xi++
		y_0 = y[yi]
		yi++
		sum[0] = sum[0] + mul_f32(tmp, y_1)
		sum[1] = sum[1] + mul_f32(tmp, y_2)
		sum[2] = sum[2] + mul_f32(tmp, y_3)
		sum[3] = sum[3] + mul_f32(tmp, y_0)
		tmp = x[xi]
		xi++
		y_1 = y[yi]
		yi++
		sum[0] = sum[0] + mul_f32(tmp, y_2)
		sum[1] = sum[1] + mul_f32(tmp, y_3)
		sum[2] = sum[2] + mul_f32(tmp, y_0)
		sum[3] = sum[3] + mul_f32(tmp, y_1)
		tmp = x[xi]
		xi++
		y_2 = y[yi]
		yi++
		sum[0] = sum[0] + mul_f32(tmp, y_3)
		sum[1] = sum[1] + mul_f32(tmp, y_0)
		sum[2] = sum[2] + mul_f32(tmp, y_1)
		sum[3] = sum[3] + mul_f32(tmp, y_2)
	}
	if j < ln {
		j++
		tmp := x[xi]
		xi++
		y_3 = y[yi]
		yi++
		sum[0] = sum[0] + mul_f32(tmp, y_0)
		sum[1] = sum[1] + mul_f32(tmp, y_1)
		sum[2] = sum[2] + mul_f32(tmp, y_2)
		sum[3] = sum[3] + mul_f32(tmp, y_3)
	}
	if j < ln {
		j++
		tmp := x[xi]
		xi++
		y_0 = y[yi]
		yi++
		sum[0] = sum[0] + mul_f32(tmp, y_1)
		sum[1] = sum[1] + mul_f32(tmp, y_2)
		sum[2] = sum[2] + mul_f32(tmp, y_3)
		sum[3] = sum[3] + mul_f32(tmp, y_0)
	}
	if j < ln {
		tmp := x[xi]
		y_1 = y[yi]
		sum[0] = sum[0] + mul_f32(tmp, y_2)
		sum[1] = sum[1] + mul_f32(tmp, y_3)
		sum[2] = sum[2] + mul_f32(tmp, y_0)
		sum[3] = sum[3] + mul_f32(tmp, y_1)
	}
}

func xcorr_kernel(x, y []opus_val16, sum []opus_val32, ln int, arch int) {
	_ = arch
	xcorr_kernel_c(x, y, sum, ln)
}

// dual_inner_prod_c — two parallel inner products sharing x.
// C: pitch.h:137-150.
func dual_inner_prod_c(x, y01, y02 []opus_val16, N int, xy1, xy2 *opus_val32) {
	*xy1, *xy2 = dual_inner_prod_batch(x, y01, y02, N)
}

func dual_inner_prod(x, y01, y02 []opus_val16, N int, xy1, xy2 *opus_val32, arch int) {
	_ = arch
	if innerProdSIMDAvailable && N >= 4 {
		// SIMD is a net win around N >= 4; below that the setup cost
		// dominates. Short-N callers fall through to the scalar batch.
		dualInnerProdSIMD(&x[0], &y01[0], &y02[0], N, xy1, xy2)
		return
	}
	dual_inner_prod_c(x, y01, y02, N, xy1, xy2)
}

// celt_inner_prod_c — scalar inner product.
// C: pitch.h:159-167.
func celt_inner_prod_c(x, y []opus_val16, N int) opus_val32 {
	return celt_inner_prod_batch(x, y, N)
}

func celt_inner_prod(x, y []opus_val16, N int, arch int) opus_val32 {
	_ = arch
	if innerProdSIMDAvailable && N >= 4 {
		return celtInnerProdSIMD(&x[0], &y[0], N)
	}
	return celt_inner_prod_c(x, y, N)
}

// ── pitch.c: float path ─────────────────────────────────────────────

// find_best_pitch — select the two strongest normalised peaks.
// C: pitch.c:45-103.
func find_best_pitch(xcorr []opus_val32, y []opus_val16, ln, max_pitch int, best_pitch []int) {
	var Syy opus_val32 = 1
	var best_num [2]opus_val16
	var best_den [2]opus_val32

	best_num[0] = -1
	best_num[1] = -1
	best_den[0] = 0
	best_den[1] = 0
	best_pitch[0] = 0
	best_pitch[1] = 1
	for j := 0; j < ln; j++ {
		Syy = fma_add(Syy, y[j], y[j])
	}
	for i := 0; i < max_pitch; i++ {
		if xcorr[i] > 0 {
			var num opus_val16
			xcorr16 := xcorr[i]
			xcorr16 *= 1e-12
			num = MULT16_16_Q15(xcorr16, xcorr16)
			if MULT16_32_Q15(num, best_den[1]) > MULT16_32_Q15(best_num[1], Syy) {
				if MULT16_32_Q15(num, best_den[0]) > MULT16_32_Q15(best_num[0], Syy) {
					best_num[1] = best_num[0]
					best_den[1] = best_den[0]
					best_pitch[1] = best_pitch[0]
					best_num[0] = num
					best_den[0] = Syy
					best_pitch[0] = i
				} else {
					best_num[1] = num
					best_den[1] = Syy
					best_pitch[1] = i
				}
			}
		}
		// Syy += y[i+ln]^2 - y[i]^2.
		Syy = fma_sub(fma_add(Syy, y[i+ln], y[i+ln]), y[i], y[i])
		Syy = MAX32(1, Syy)
	}
}

// celt_fir5 — 5-tap FIR. C: pitch.c:105-137.
func celt_fir5(x, num []opus_val16, N int) {
	num0 := num[0]
	num1 := num[1]
	num2 := num[2]
	num3 := num[3]
	num4 := num[4]
	var mem0, mem1, mem2, mem3, mem4 opus_val32
	for i := 0; i < N; i++ {
		sum := SHL32(EXTEND32(x[i]), SIG_SHIFT)
		sum = fma_add(sum, num0, mem0)
		sum = fma_add(sum, num1, mem1)
		sum = fma_add(sum, num2, mem2)
		sum = fma_add(sum, num3, mem3)
		sum = fma_add(sum, num4, mem4)
		mem4 = mem3
		mem3 = mem2
		mem2 = mem1
		mem1 = mem0
		mem0 = opus_val32(x[i])
		x[i] = ROUND16(sum, SIG_SHIFT)
	}
}

// pitch_downsample — low-pass and decimate by `factor`.
// C: pitch.c:140-222.
func pitch_downsample(x [][]celt_sig, x_lp []opus_val16, ln, C, factor, arch int) {
	var ac [5]opus_val32
	var tmp opus_val16 = Q15ONE
	var lpc [4]opus_val16
	var lpc2 [5]opus_val16
	c1 := QCONST16(0.8, 15)
	offset := factor / 2

	// x_lp[i] = .25*x[fi-o] + .25*x[fi+o] + .5*x[fi]
	// C evaluates left-to-right as ((.25*a + .25*b) + .5*c). The first
	// mul is pure, the two subsequent adds fuse: each `addend + x*y`.
	for i := 1; i < ln; i++ {
		t1 := 0.25 * x[0][factor*i-offset]
		t2 := fma_add(t1, 0.25, x[0][factor*i+offset])
		x_lp[i] = opus_val16(fma_add(t2, 0.5, x[0][factor*i]))
	}
	x_lp[0] = opus_val16(fma_add(0.25*x[0][offset], 0.5, x[0][0]))
	if C == 2 {
		for i := 1; i < ln; i++ {
			t1 := 0.25 * x[1][factor*i-offset]
			t2 := fma_add(t1, 0.25, x[1][factor*i+offset])
			x_lp[i] += opus_val16(fma_add(t2, 0.5, x[1][factor*i]))
		}
		x_lp[0] += opus_val16(fma_add(0.25*x[1][offset], 0.5, x[1][0]))
	}
	_celt_autocorr(x_lp, ac[:], nil, 0, 4, ln, arch)

	ac[0] *= 1.0001
	// Lag windowing: ac[i] -= ac[i]*(.008*i)^2.
	for i := 1; i <= 4; i++ {
		coef := 0.008 * float32(i)
		ac[i] = fma_sub(ac[i], ac[i]*coef, coef)
	}

	_celt_lpc(lpc[:], ac[:], 4)
	for i := 0; i < 4; i++ {
		tmp = MULT16_16_Q15(QCONST16(0.9, 15), tmp)
		lpc[i] = MULT16_16_Q15(lpc[i], tmp)
	}
	// lpc2[j] = lpc[j] + c1*lpc[j-1]
	lpc2[0] = lpc[0] + QCONST16(0.8, SIG_SHIFT)
	lpc2[1] = opus_val16(fma_add(lpc[1], c1, lpc[0]))
	lpc2[2] = opus_val16(fma_add(lpc[2], c1, lpc[1]))
	lpc2[3] = opus_val16(fma_add(lpc[3], c1, lpc[2]))
	lpc2[4] = MULT16_16_Q15(c1, lpc[3])
	celt_fir5(x_lp, lpc2[:], ln)
}

// celt_pitch_xcorr_c — float build `void` form.
// C: pitch.c:224-305 (unrolled branch).
func celt_pitch_xcorr_c(_x, _y []opus_val16, xcorr []opus_val32, ln, max_pitch, arch int) {
	celt_assert(max_pitch > 0)
	var i int
	for i = 0; i < max_pitch-3; i += 4 {
		sum := [4]opus_val32{0, 0, 0, 0}
		xcorr_kernel(_x, _y[i:], sum[:], ln, arch)
		xcorr[i] = sum[0]
		xcorr[i+1] = sum[1]
		xcorr[i+2] = sum[2]
		xcorr[i+3] = sum[3]
	}
	for ; i < max_pitch; i++ {
		xcorr[i] = celt_inner_prod(_x, _y[i:], ln, arch)
	}
}

func celt_pitch_xcorr(_x, _y []opus_val16, xcorr []opus_val32, ln, max_pitch, arch int) {
	celt_pitch_xcorr_c(_x, _y, xcorr, ln, max_pitch, arch)
}

// pitch_search — coarse + fine pitch search.
// C: pitch.c:307-416.
func pitch_search(x_lp, y []opus_val16, ln, max_pitch int, pitch *int, arch int) {
	var best_pitch = [2]int{0, 0}
	var offset int

	celt_assert(ln > 0)
	celt_assert(max_pitch > 0)
	lag := ln + max_pitch

	x_lp4 := make([]opus_val16, ln>>2)
	y_lp4 := make([]opus_val16, lag>>2)
	xcorr := make([]opus_val32, max_pitch>>1)

	for j := 0; j < ln>>2; j++ {
		x_lp4[j] = x_lp[2*j]
	}
	for j := 0; j < lag>>2; j++ {
		y_lp4[j] = y[2*j]
	}

	celt_pitch_xcorr(x_lp4, y_lp4, xcorr, ln>>2, max_pitch>>2, arch)
	find_best_pitch(xcorr, y_lp4, ln>>2, max_pitch>>2, best_pitch[:])

	for i := 0; i < max_pitch>>1; i++ {
		var sum opus_val32
		xcorr[i] = 0
		ai := i - 2*best_pitch[0]
		if ai < 0 {
			ai = -ai
		}
		bi := i - 2*best_pitch[1]
		if bi < 0 {
			bi = -bi
		}
		if ai > 2 && bi > 2 {
			continue
		}
		sum = celt_inner_prod(x_lp, y[i:], ln>>1, arch)
		xcorr[i] = MAX32(-1, sum)
	}
	find_best_pitch(xcorr, y, ln>>1, max_pitch>>1, best_pitch[:])

	if best_pitch[0] > 0 && best_pitch[0] < (max_pitch>>1)-1 {
		a := xcorr[best_pitch[0]-1]
		b := xcorr[best_pitch[0]]
		c := xcorr[best_pitch[0]+1]
		if c-a > MULT16_32_Q15(QCONST16(0.7, 15), b-a) {
			offset = 1
		} else if a-c > MULT16_32_Q15(QCONST16(0.7, 15), b-c) {
			offset = -1
		} else {
			offset = 0
		}
	} else {
		offset = 0
	}
	*pitch = 2*best_pitch[0] - offset
}

// compute_pitch_gain — float build: xy / sqrt(1 + xx*yy).
// C: pitch.c:446-451.
func compute_pitch_gain(xy, xx, yy opus_val32) opus_val16 {
	return opus_val16(xy / celt_sqrt(fma_add(1.0, xx, yy)))
}

// second_check — lookup for pitch halving detection.
var second_check = [16]int{0, 0, 3, 2, 3, 2, 5, 2, 3, 2, 3, 2, 5, 2, 3, 2}

// remove_doubling — pitch halving detector. C: pitch.c:454-560.
func remove_doubling(x []opus_val16, maxperiod, minperiod, N int, T0_ *int,
	prev_period int, prev_gain opus_val16, arch int) opus_val16 {
	var k int
	var T, T0 int
	var g, g0 opus_val16
	var pg opus_val16
	var xy, xx, yy, xy2 opus_val32
	var xcorr [3]opus_val32
	var best_xy, best_yy opus_val32
	var offset int
	minperiod0 := minperiod
	maxperiod /= 2
	minperiod /= 2
	*T0_ /= 2
	prev_period /= 2
	N /= 2
	xOff := maxperiod
	if *T0_ >= maxperiod {
		*T0_ = maxperiod - 1
	}
	T = *T0_
	T0 = *T0_
	yy_lookup := make([]opus_val32, maxperiod+1)
	dual_inner_prod(x[xOff:], x[xOff:], x[xOff-T0:], N, &xx, &xy, arch)
	yy_lookup[0] = xx
	yy = xx
	for i := 1; i <= maxperiod; i++ {
		yy = fma_sub(fma_add(yy, x[xOff-i], x[xOff-i]),
			x[xOff+N-i], x[xOff+N-i])
		yy_lookup[i] = MAX32(0, yy)
	}
	yy = yy_lookup[T0]
	best_xy = xy
	best_yy = yy
	g = compute_pitch_gain(xy, xx, yy)
	g0 = g
	for k = 2; k <= 15; k++ {
		var T1, T1b int
		var g1 opus_val16
		var cont opus_val16 = 0
		var thresh opus_val16
		T1 = int(celt_udiv(opus_uint32(2*T0+k), opus_uint32(2*k)))
		if T1 < minperiod {
			break
		}
		if k == 2 {
			if T1+T0 > maxperiod {
				T1b = T0
			} else {
				T1b = T0 + T1
			}
		} else {
			T1b = int(celt_udiv(opus_uint32(2*second_check[k]*T0+k), opus_uint32(2*k)))
		}
		dual_inner_prod(x[xOff:], x[xOff-T1:], x[xOff-T1b:], N, &xy, &xy2, arch)
		xy = HALF32(xy + xy2)
		yy = HALF32(yy_lookup[T1] + yy_lookup[T1b])
		g1 = compute_pitch_gain(xy, xx, yy)
		absDT1 := T1 - prev_period
		if absDT1 < 0 {
			absDT1 = -absDT1
		}
		if absDT1 <= 1 {
			cont = prev_gain
		} else if absDT1 <= 2 && 5*k*k < T0 {
			cont = HALF16(prev_gain)
		} else {
			cont = 0
		}
		thresh = MAX16(QCONST16(0.3, 15), MULT16_16_Q15(QCONST16(0.7, 15), g0)-cont)
		if T1 < 3*minperiod {
			thresh = MAX16(QCONST16(0.4, 15), MULT16_16_Q15(QCONST16(0.85, 15), g0)-cont)
		} else if T1 < 2*minperiod {
			thresh = MAX16(QCONST16(0.5, 15), MULT16_16_Q15(QCONST16(0.9, 15), g0)-cont)
		}
		if g1 > thresh {
			best_xy = xy
			best_yy = yy
			T = T1
			g = g1
		}
	}
	best_xy = MAX32(0, best_xy)
	if best_yy <= best_xy {
		pg = Q15ONE
	} else {
		pg = opus_val16(SHR32(frac_div32(best_xy, best_yy+1), 16))
	}
	for k = 0; k < 3; k++ {
		xcorr[k] = celt_inner_prod(x[xOff:], x[xOff-(T+k-1):], N, arch)
	}
	if xcorr[2]-xcorr[0] > MULT16_32_Q15(QCONST16(0.7, 15), xcorr[1]-xcorr[0]) {
		offset = 1
	} else if xcorr[0]-xcorr[2] > MULT16_32_Q15(QCONST16(0.7, 15), xcorr[1]-xcorr[2]) {
		offset = -1
	} else {
		offset = 0
	}
	if pg > g {
		pg = g
	}
	*T0_ = 2*T + offset
	if *T0_ < minperiod0 {
		*T0_ = minperiod0
	}
	return pg
}
