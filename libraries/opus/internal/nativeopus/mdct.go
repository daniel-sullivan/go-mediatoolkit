package nativeopus

import (
	"math"
	"unsafe"
)

// Port of libopus/celt/mdct.h + mdct.c. Float-path only.
//
// The MDCT is computed via an N/4-point complex FFT with pre/post
// rotations. All S_MUL patterns route through the non-fused mul_f32
// helper and add/sub operations use explicit parenthesisation to
// match the C oracle's evaluation order. See memory/
// feedback_opus_parity_fp.md for why non-FMA is the parity strategy.

// mdct_lookup — MDCT configuration. C: mdct.h:49-54.
//
// `kfft[0..maxshift]` are N/4, N/8, N/16, N/32 point FFT configs.
// `trig` is a packed table of cos values, contiguously stored for
// each shift level.
type mdct_lookup struct {
	n        int
	maxshift int
	kfft     [4]*kiss_fft_state
	trig     []kiss_twiddle_scalar
	// Per-call scratch for clt_mdct_forward_c / clt_mdct_backward_c.
	// Sized to the shift=0 maxima (N/2 and N/4); smaller shifts reslice.
	scratchF  []kiss_fft_scalar
	scratchF2 []kiss_fft_cpx
}

// clt_mdct_init — build an mdct_lookup. C: mdct.c:66-108 (inside
// CUSTOM_MODES). Ported here because we need runtime mdct_lookup
// construction for parity tests (production uses static_modes_float.h
// precomputed tables).
func clt_mdct_init(l *mdct_lookup, N, maxshift, arch int) int {
	N2 := N >> 1
	l.n = N
	l.maxshift = maxshift
	for i := 0; i <= maxshift; i++ {
		if i == 0 {
			l.kfft[i] = opus_fft_alloc(N>>2>>i, arch)
		} else {
			l.kfft[i] = opus_fft_alloc_twiddles(N>>2>>i, l.kfft[0], arch)
		}
		if l.kfft[i] == nil {
			return 0
		}
	}
	// Float build: cos-only table. Total length = N - (N2 >> maxshift).
	totalLen := N - (N2 >> maxshift)
	l.trig = make([]kiss_twiddle_scalar, totalLen)
	// shift=0: N2 = N/2 is the largest scratch size used below.
	l.scratchF = make([]kiss_fft_scalar, N2)
	l.scratchF2 = make([]kiss_fft_cpx, N>>2)
	off := 0
	curN := N
	curN2 := N2
	for shift := 0; shift <= maxshift; shift++ {
		for i := 0; i < curN2; i++ {
			phase := 2 * PI * (float64(i) + 0.125) / float64(curN)
			l.trig[off+i] = kiss_twiddle_scalar(math.Cos(phase))
		}
		off += curN2
		curN2 >>= 1
		curN >>= 1
	}
	return 1
}

// clt_mdct_clear — release the lookup. Go GC handles this; kept as a
// no-op for API parity. C: mdct.c:110-116.
func clt_mdct_clear(l *mdct_lookup, arch int) {
	for i := 0; i <= l.maxshift; i++ {
		opus_fft_free(l.kfft[i], arch)
	}
	_ = arch
}

// clt_mdct_forward_c — forward MDCT. Trashes the input array.
// C: mdct.c:122-264.
func clt_mdct_forward_c(l *mdct_lookup, in, out []kiss_fft_scalar,
	window []celt_coef, overlap, shift, stride, arch int) {
	_ = arch
	st := l.kfft[shift]
	scale := st.scale

	N := l.n
	trigOff := 0
	for i := 0; i < shift; i++ {
		N >>= 1
		trigOff += N
	}
	trig := l.trig[trigOff:]
	N2 := N >> 1
	N4 := N >> 2

	f := l.scratchF[:N2]
	f2 := l.scratchF2[:N4]

	// Window / shuffle / fold. Treat input as four blocks [a, b, c, d].
	{
		xp1 := overlap >> 1
		xp2 := N2 - 1 + (overlap >> 1)
		yp := 0
		wp1 := overlap >> 1
		wp2 := (overlap >> 1) - 1
		var i int
		for i = 0; i < (overlap+3)>>2; i++ {
			// Real: -d-cR = S_MUL(xp1[N2], *wp2) + S_MUL(*xp2, *wp1)
			f[yp] = add_f32(mul_f32(in[xp1+N2], window[wp2]), mul_f32(in[xp2], window[wp1]))
			yp++
			// Imag: -b+aR = S_MUL(*xp1, *wp1) - S_MUL(xp2[-N2], *wp2)
			f[yp] = sub_f32(mul_f32(in[xp1], window[wp1]), mul_f32(in[xp2-N2], window[wp2]))
			yp++
			xp1 += 2
			xp2 -= 2
			wp1 += 2
			wp2 -= 2
		}
		wp1 = 0
		wp2 = overlap - 1
		for ; i < N4-((overlap+3)>>2); i++ {
			f[yp] = in[xp2]
			yp++
			f[yp] = in[xp1]
			yp++
			xp1 += 2
			xp2 -= 2
		}
		for ; i < N4; i++ {
			// Real: a-bR = -S_MUL(xp1[-N2], *wp1) + S_MUL(*xp2, *wp2)
			f[yp] = add_f32(-mul_f32(in[xp1-N2], window[wp1]), mul_f32(in[xp2], window[wp2]))
			yp++
			// Imag: -c-dR = S_MUL(*xp1, *wp2) + S_MUL(xp2[N2], *wp1)
			f[yp] = add_f32(mul_f32(in[xp1], window[wp2]), mul_f32(in[xp2+N2], window[wp1]))
			yp++
			xp1 += 2
			xp2 -= 2
			wp1 += 2
			wp2 -= 2
		}
	}
	// Pre-rotation + bit-reverse scatter.
	{
		yp := 0
		for i := 0; i < N4; i++ {
			t0 := trig[i]
			t1 := trig[N4+i]
			re := f[yp]
			yp++
			im := f[yp]
			yp++
			yr := sub_f32(mul_f32(re, t0), mul_f32(im, t1))
			yi := add_f32(mul_f32(im, t0), mul_f32(re, t1))
			// Float build: scale here (ENABLE_QEXT path would defer).
			var yc kiss_fft_cpx
			yc.r = mul_f32(yr, scale)
			yc.i = mul_f32(yi, scale)
			f2[st.bitrev[i]] = yc
		}
	}

	// N/4-point complex FFT in-place on f2.
	opus_fft_impl(st, f2)

	// Post-rotate into `out` with stride.
	{
		fp := 0
		yp1 := 0
		yp2 := stride * (N2 - 1)
		for i := 0; i < N4; i++ {
			t0 := trig[i]
			t1 := trig[N4+i]
			yr := sub_f32(mul_f32(f2[fp].i, t1), mul_f32(f2[fp].r, t0))
			yi := add_f32(mul_f32(f2[fp].r, t1), mul_f32(f2[fp].i, t0))
			out[yp1] = yr
			out[yp2] = yi
			fp++
			yp1 += 2 * stride
			yp2 -= 2 * stride
		}
	}
}

// clt_mdct_backward_c — backward MDCT + TDAC window overlap-add.
// C: mdct.c:268-390.
func clt_mdct_backward_c(l *mdct_lookup, in, out []kiss_fft_scalar,
	window []celt_coef, overlap, shift, stride, arch int) {
	_ = arch

	N := l.n
	trigOff := 0
	for i := 0; i < shift; i++ {
		N >>= 1
		trigOff += N
	}
	trig := l.trig[trigOff:]
	N2 := N >> 1
	N4 := N >> 2

	// Pre-rotate and scatter into output at bit-reverse offsets.
	{
		xp1 := 0
		xp2 := stride * (N2 - 1)
		yp := overlap >> 1
		bitrev := l.kfft[shift].bitrev
		for i := 0; i < N4; i++ {
			rev := int(bitrev[i])
			x1 := in[xp1]
			x2 := in[xp2]
			yr := add_f32(mul_f32(x2, trig[i]), mul_f32(x1, trig[N4+i]))
			yi := sub_f32(mul_f32(x1, trig[i]), mul_f32(x2, trig[N4+i]))
			// We swap real/imag because we use an FFT instead of IFFT.
			out[yp+2*rev+1] = yr
			out[yp+2*rev] = yi
			xp1 += 2 * stride
			xp2 -= 2 * stride
		}
	}

	// FFT operates in-place on the overlap/2 offset view of `out`,
	// reinterpreted as []kiss_fft_cpx. Layout is identical: each cpx
	// is two consecutive float32s.
	{
		base := out[overlap>>1:]
		cpx := unsafe.Slice((*kiss_fft_cpx)(unsafe.Pointer(&base[0])), N4)
		opus_fft_impl(l.kfft[shift], cpx)
	}

	// Post-rotate and de-shuffle. Loop to (N4+1)>>1 to handle odd N4.
	{
		yp0 := overlap >> 1
		yp1 := (overlap >> 1) + N2 - 2
		for i := 0; i < (N4+1)>>1; i++ {
			// yp0 side.
			re := out[yp0+1]
			im := out[yp0]
			t0 := trig[i]
			t1 := trig[N4+i]
			yr := add_f32(mul_f32(re, t0), mul_f32(im, t1))
			yi := sub_f32(mul_f32(re, t1), mul_f32(im, t0))
			// yp1 side read BEFORE overwriting.
			re2 := out[yp1+1]
			im2 := out[yp1]
			out[yp0] = yr
			out[yp1+1] = yi

			t0 = trig[N4-i-1]
			t1 = trig[N2-i-1]
			yr = add_f32(mul_f32(re2, t0), mul_f32(im2, t1))
			yi = sub_f32(mul_f32(re2, t1), mul_f32(im2, t0))
			out[yp1] = yr
			out[yp0+1] = yi

			yp0 += 2
			yp1 -= 2
		}
	}

	// Mirror both sides for TDAC.
	{
		xp1 := overlap - 1
		yp1 := 0
		wp1 := 0
		wp2 := overlap - 1
		for i := 0; i < overlap/2; i++ {
			x1 := out[xp1]
			x2 := out[yp1]
			out[yp1] = sub_f32(mul_f32(x2, window[wp2]), mul_f32(x1, window[wp1]))
			out[xp1] = add_f32(mul_f32(x2, window[wp1]), mul_f32(x1, window[wp2]))
			yp1++
			xp1--
			wp1++
			wp2--
		}
	}
}

// clt_mdct_forward / clt_mdct_backward — C macro wrappers (no RTCD).
func clt_mdct_forward(l *mdct_lookup, in, out []kiss_fft_scalar,
	window []celt_coef, overlap, shift, stride, arch int) {
	clt_mdct_forward_c(l, in, out, window, overlap, shift, stride, arch)
}

func clt_mdct_backward(l *mdct_lookup, in, out []kiss_fft_scalar,
	window []celt_coef, overlap, shift, stride, arch int) {
	clt_mdct_backward_c(l, in, out, window, overlap, shift, stride, arch)
}
