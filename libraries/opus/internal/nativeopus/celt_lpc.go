package nativeopus

// Port of libopus/celt/celt_lpc.h + celt_lpc.c. Float-path only.
//
// fma_* convention: fma_add(addend, mul1, mul2) → addend + mul1*mul2;
// fma_sub(addend, mul1, mul2) → addend - mul1*mul2. Every
// `MAC16_16(c, a, b) = c + a*b` in C maps to `fma_add(c, a, b)`.
//
// celt_fir_c / celt_iir accept their `x` (or `_x`) argument INCLUDING
// an `ord`-sample prefix at the start — so that the C pointer
// arithmetic `x[i-ord]` corresponds to `x[i]` in Go and negative
// slice indices are never needed. Callers must pass a slice of
// length N+ord with the first ord entries as history.

const CELT_LPC_ORDER = 24

// _celt_lpc — Levinson-Durbin recursion. Float build writes directly
// into _lpc (the C `float *lpc = _lpc` alias).
// C: celt_lpc.c:37-143 (float branch).
//
// Inner rr loops use `acc + mul_f32(a, b)` instead of fma_add() to
// drop the add_f32 wrapper; the mul stays behind //go:noinline
// mul_f32 so no FMA fusion is possible.
func _celt_lpc(_lpc []opus_val16, ac []opus_val32, p int) {
	var r opus_val32
	error := ac[0]
	lpc := _lpc

	OPUS_CLEAR(lpc, p)
	if ac[0] > 1e-10 {
		for i := 0; i < p; i++ {
			var rr opus_val32 = 0
			for j := 0; j < i; j++ {
				rr = rr + mul_f32(lpc[j], ac[i-j])
			}
			rr += SHR32(ac[i+1], 6)
			r = -frac_div32(SHL32(rr, 6), error)
			lpc[i] = SHR32(r, 6)
			for j := 0; j < (i+1)>>1; j++ {
				tmp1 := lpc[j]
				tmp2 := lpc[i-1-j]
				lpc[j] = tmp1 + mul_f32(r, tmp2)
				lpc[i-1-j] = tmp2 + mul_f32(r, tmp1)
			}
			error = error - mul_f32(r*r, error)
			if error <= 0.001*ac[0] {
				break
			}
		}
	}
}

// celt_fir_c — FIR filter. `x` has length N+ord with ord-sample
// history in x[0..ord-1]; output y has length N.
// C: celt_lpc.c:146-192. Signature note: the C takes `x` as a pointer
// that must already satisfy x[i-ord] valid — i.e. the caller passed
// `original+ord`. In Go we accept the original (un-offset) buffer so
// indices stay non-negative.
func celt_fir_c(x, num, y []opus_val16, N, ord, arch int) {
	rnum := make([]opus_val16, ord)
	for i := 0; i < ord; i++ {
		rnum[i] = num[ord-i-1]
	}
	var i int
	for i = 0; i < N-3; i += 4 {
		var sum [4]opus_val32
		// C: sum[k] = SHL32(EXTEND32(x[i+k]), SIG_SHIFT), where C's x
		// is offset by ord — Go's x is not, so we read x[ord+i+k].
		sum[0] = SHL32(EXTEND32(x[ord+i]), SIG_SHIFT)
		sum[1] = SHL32(EXTEND32(x[ord+i+1]), SIG_SHIFT)
		sum[2] = SHL32(EXTEND32(x[ord+i+2]), SIG_SHIFT)
		sum[3] = SHL32(EXTEND32(x[ord+i+3]), SIG_SHIFT)
		// xcorr_kernel reads y[0..len-1+3]; C passes x+i-ord meaning
		// "start reading from position (i-ord) relative to offset-x",
		// i.e. absolute position i in the un-offset buffer.
		xcorr_kernel(rnum, x[i:], sum[:], ord, arch)
		y[i] = SROUND16(sum[0], SIG_SHIFT)
		y[i+1] = SROUND16(sum[1], SIG_SHIFT)
		y[i+2] = SROUND16(sum[2], SIG_SHIFT)
		y[i+3] = SROUND16(sum[3], SIG_SHIFT)
	}
	for ; i < N; i++ {
		sum := SHL32(EXTEND32(x[ord+i]), SIG_SHIFT)
		for j := 0; j < ord; j++ {
			// C: rnum[j] * x[i+j-ord] → Go absolute index i+j.
			sum = sum + mul_f32(rnum[j], x[i+j])
		}
		y[i] = SROUND16(sum, SIG_SHIFT)
	}
}

func celt_fir(x, num, y []opus_val16, N, ord, arch int) {
	celt_fir_c(x, num, y, N, ord, arch)
}

// celt_iir — IIR filter (non-SMALL_FOOTPRINT, 4-way unrolled).
// C: celt_lpc.c:194-282 (non-SMALL_FOOTPRINT branch).
func celt_iir(_x []opus_val32, den []opus_val16, _y []opus_val32,
	N, ord int, mem []opus_val16, arch int) {
	celt_assert(ord&3 == 0)
	rden := make([]opus_val16, ord)
	y := make([]opus_val16, N+ord)
	for i := 0; i < ord; i++ {
		rden[i] = den[ord-i-1]
	}
	for i := 0; i < ord; i++ {
		y[i] = -mem[ord-i-1]
	}

	var i int
	for i = 0; i < N-3; i += 4 {
		var sum [4]opus_val32
		sum[0] = _x[i]
		sum[1] = _x[i+1]
		sum[2] = _x[i+2]
		sum[3] = _x[i+3]
		xcorr_kernel(rden, y[i:], sum[:], ord, arch)
		y[i+ord] = -SROUND16(sum[0], SIG_SHIFT)
		_y[i] = sum[0]
		sum[1] = fma_add(sum[1], y[i+ord], den[0])
		y[i+ord+1] = -SROUND16(sum[1], SIG_SHIFT)
		_y[i+1] = sum[1]
		sum[2] = fma_add(sum[2], y[i+ord+1], den[0])
		sum[2] = fma_add(sum[2], y[i+ord], den[1])
		y[i+ord+2] = -SROUND16(sum[2], SIG_SHIFT)
		_y[i+2] = sum[2]

		sum[3] = fma_add(sum[3], y[i+ord+2], den[0])
		sum[3] = fma_add(sum[3], y[i+ord+1], den[1])
		sum[3] = fma_add(sum[3], y[i+ord], den[2])
		y[i+ord+3] = -SROUND16(sum[3], SIG_SHIFT)
		_y[i+3] = sum[3]
	}
	for ; i < N; i++ {
		sum := _x[i]
		for j := 0; j < ord; j++ {
			sum = sum - mul_f32(rden[j], opus_val32(y[i+j]))
		}
		y[i+ord] = SROUND16(sum, SIG_SHIFT)
		_y[i] = sum
	}
	for i := 0; i < ord; i++ {
		mem[i] = opus_val16(_y[N-i-1])
	}
}

// _celt_autocorr — windowed autocorrelation up to lag `lag`.
// C: celt_lpc.c:284-374 (float branch — no shift returned).
func _celt_autocorr(x []opus_val16, ac []opus_val32, window []celt_coef,
	overlap, lag, n, arch int) int {
	var d opus_val32
	fastN := n - lag
	var xptr []opus_val16
	xx := make([]opus_val16, n)
	celt_assert(n > 0)
	celt_assert(overlap >= 0)
	if overlap == 0 {
		xptr = x
	} else {
		for i := 0; i < n; i++ {
			xx[i] = x[i]
		}
		for i := 0; i < overlap; i++ {
			w := COEF2VAL16(window[i])
			xx[i] = MULT16_16_Q15(x[i], w)
			xx[n-i-1] = MULT16_16_Q15(x[n-i-1], w)
		}
		xptr = xx
	}
	celt_pitch_xcorr(xptr, xptr, ac, fastN, lag+1, arch)
	for k := 0; k <= lag; k++ {
		d = celt_autocorr_tail_batch(xptr, k, fastN, n)
		ac[k] += d
	}
	return 0
}
