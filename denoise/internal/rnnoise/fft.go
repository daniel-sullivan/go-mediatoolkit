package rnnoise

// 960-pt FFT, ported 1:1 from librnnoise/src/kiss_fft.c (float build,
// CUSTOM_MODES). The float-mode _kiss_fft_guts.h / arch.h macros expand
// to plain arithmetic, routed here through the strict primitives so the
// output is bit-identical to the -ffp-contract=off generic-branch oracle
// (project_fp_cross_statement_fma): every complex multiply is two scalar
// multiplies feeding an add/sub, which must not fuse.
//
// Only the forward transform (rnn_fft_c) is ported: denoise.c's
// inverse_transform builds a conjugate-symmetric spectrum and calls the
// FORWARD FFT, so rnn_ifft_c is never used on the denoise path.

// cMul is C_MUL: (m.r, m.i) = (a.r*b.r - a.i*b.i, a.r*b.i + a.i*b.r).
func cMul(a fftCpx, b twiddleCpx) fftCpx {
	return fftCpx{
		r: mulsub(a.r, b.r, a.i, b.i),
		i: muladd(a.r, b.i, a.i, b.r),
	}
}

// sMul is S_MUL: a*b (single scalar multiply).
func sMul(a, b float32) float32 { return mul32(a, b) }

// halfOf is HALF_OF: x*0.5f.
func halfOf(x float32) float32 { return mul32(x, 0.5) }

// kfBfly2 — radix-2 butterfly (kiss_fft.c kf_bfly2). In the non-custom
// path m is always 4 (radix-2 follows radix-4); the m==1 degenerate case
// is also ported for arbitrary factorisations.
func kfBfly2(fout []fftCpx, m, n int) {
	if m == 1 {
		off := 0
		for i := 0; i < n; i++ {
			t := fout[off+1]
			fout[off+1] = fftCpx{sub32(fout[off].r, t.r), sub32(fout[off].i, t.i)}
			fout[off] = fftCpx{add32(fout[off].r, t.r), add32(fout[off].i, t.i)}
			off += 2
		}
		return
	}
	const tw float32 = 0.7071067812
	off := 0
	for i := 0; i < n; i++ {
		var t fftCpx
		t = fout[off+4]
		fout[off+4] = fftCpx{sub32(fout[off].r, t.r), sub32(fout[off].i, t.i)}
		fout[off] = fftCpx{add32(fout[off].r, t.r), add32(fout[off].i, t.i)}

		t.r = sMul(add32(fout[off+5].r, fout[off+5].i), tw)
		t.i = sMul(sub32(fout[off+5].i, fout[off+5].r), tw)
		fout[off+5] = fftCpx{sub32(fout[off+1].r, t.r), sub32(fout[off+1].i, t.i)}
		fout[off+1] = fftCpx{add32(fout[off+1].r, t.r), add32(fout[off+1].i, t.i)}

		t.r = fout[off+6].i
		t.i = -fout[off+6].r
		fout[off+6] = fftCpx{sub32(fout[off+2].r, t.r), sub32(fout[off+2].i, t.i)}
		fout[off+2] = fftCpx{add32(fout[off+2].r, t.r), add32(fout[off+2].i, t.i)}

		t.r = sMul(sub32(fout[off+7].i, fout[off+7].r), tw)
		t.i = sMul(-(add32(fout[off+7].i, fout[off+7].r)), tw)
		fout[off+7] = fftCpx{sub32(fout[off+3].r, t.r), sub32(fout[off+3].i, t.i)}
		fout[off+3] = fftCpx{add32(fout[off+3].r, t.r), add32(fout[off+3].i, t.i)}
		off += 8
	}
}

// kfBfly4 — radix-4 butterfly (kiss_fft.c kf_bfly4).
func kfBfly4(fout []fftCpx, fstride int, st *fftState, m, n, mm int) {
	if m == 1 {
		off := 0
		for i := 0; i < n; i++ {
			var scratch0, scratch1 fftCpx
			scratch0 = fftCpx{sub32(fout[off].r, fout[off+2].r), sub32(fout[off].i, fout[off+2].i)}
			fout[off] = fftCpx{add32(fout[off].r, fout[off+2].r), add32(fout[off].i, fout[off+2].i)}
			scratch1 = fftCpx{add32(fout[off+1].r, fout[off+3].r), add32(fout[off+1].i, fout[off+3].i)}
			fout[off+2] = fftCpx{sub32(fout[off].r, scratch1.r), sub32(fout[off].i, scratch1.i)}
			fout[off] = fftCpx{add32(fout[off].r, scratch1.r), add32(fout[off].i, scratch1.i)}
			scratch1 = fftCpx{sub32(fout[off+1].r, fout[off+3].r), sub32(fout[off+1].i, fout[off+3].i)}

			fout[off+1] = fftCpx{add32(scratch0.r, scratch1.i), sub32(scratch0.i, scratch1.r)}
			fout[off+3] = fftCpx{sub32(scratch0.r, scratch1.i), add32(scratch0.i, scratch1.r)}
			off += 4
		}
		return
	}
	var scratch [6]fftCpx
	m2 := 2 * m
	m3 := 3 * m
	for i := 0; i < n; i++ {
		base := i * mm
		tw1, tw2, tw3 := 0, 0, 0
		for j := 0; j < m; j++ {
			pos := base + j
			scratch[0] = cMul(fout[pos+m], st.twiddles[tw1])
			scratch[1] = cMul(fout[pos+m2], st.twiddles[tw2])
			scratch[2] = cMul(fout[pos+m3], st.twiddles[tw3])

			scratch[5] = fftCpx{sub32(fout[pos].r, scratch[1].r), sub32(fout[pos].i, scratch[1].i)}
			fout[pos] = fftCpx{add32(fout[pos].r, scratch[1].r), add32(fout[pos].i, scratch[1].i)}
			scratch[3] = fftCpx{add32(scratch[0].r, scratch[2].r), add32(scratch[0].i, scratch[2].i)}
			scratch[4] = fftCpx{sub32(scratch[0].r, scratch[2].r), sub32(scratch[0].i, scratch[2].i)}
			fout[pos+m2] = fftCpx{sub32(fout[pos].r, scratch[3].r), sub32(fout[pos].i, scratch[3].i)}
			tw1 += fstride
			tw2 += fstride * 2
			tw3 += fstride * 3
			fout[pos] = fftCpx{add32(fout[pos].r, scratch[3].r), add32(fout[pos].i, scratch[3].i)}

			fout[pos+m] = fftCpx{add32(scratch[5].r, scratch[4].i), sub32(scratch[5].i, scratch[4].r)}
			fout[pos+m3] = fftCpx{sub32(scratch[5].r, scratch[4].i), add32(scratch[5].i, scratch[4].r)}
		}
	}
}

// kfBfly3 — radix-3 butterfly (kiss_fft.c kf_bfly3).
func kfBfly3(fout []fftCpx, fstride int, st *fftState, m, n, mm int) {
	m2 := 2 * m
	var scratch [5]fftCpx
	epi3 := st.twiddles[fstride*m]
	for i := 0; i < n; i++ {
		base := i * mm
		tw1, tw2 := 0, 0
		k := m
		pos := base
		for {
			scratch[1] = cMul(fout[pos+m], st.twiddles[tw1])
			scratch[2] = cMul(fout[pos+m2], st.twiddles[tw2])

			scratch[3] = fftCpx{add32(scratch[1].r, scratch[2].r), add32(scratch[1].i, scratch[2].i)}
			scratch[0] = fftCpx{sub32(scratch[1].r, scratch[2].r), sub32(scratch[1].i, scratch[2].i)}
			tw1 += fstride
			tw2 += fstride * 2

			fout[pos+m] = fftCpx{sub32(fout[pos].r, halfOf(scratch[3].r)), sub32(fout[pos].i, halfOf(scratch[3].i))}

			scratch[0] = fftCpx{mul32(scratch[0].r, epi3.i), mul32(scratch[0].i, epi3.i)}

			fout[pos] = fftCpx{add32(fout[pos].r, scratch[3].r), add32(fout[pos].i, scratch[3].i)}

			fout[pos+m2] = fftCpx{add32(fout[pos+m].r, scratch[0].i), sub32(fout[pos+m].i, scratch[0].r)}

			fout[pos+m] = fftCpx{sub32(fout[pos+m].r, scratch[0].i), add32(fout[pos+m].i, scratch[0].r)}
			pos++
			k--
			if k == 0 {
				break
			}
		}
	}
}

// kfBfly5 — radix-5 butterfly (kiss_fft.c kf_bfly5).
func kfBfly5(fout []fftCpx, fstride int, st *fftState, m, n, mm int) {
	var scratch [13]fftCpx
	ya := st.twiddles[fstride*m]
	yb := st.twiddles[fstride*2*m]
	tw := st.twiddles
	for i := 0; i < n; i++ {
		base := i * mm
		f0 := base
		f1 := f0 + m
		f2 := f0 + 2*m
		f3 := f0 + 3*m
		f4 := f0 + 4*m
		for u := 0; u < m; u++ {
			scratch[0] = fout[f0]

			scratch[1] = cMul(fout[f1], tw[u*fstride])
			scratch[2] = cMul(fout[f2], tw[2*u*fstride])
			scratch[3] = cMul(fout[f3], tw[3*u*fstride])
			scratch[4] = cMul(fout[f4], tw[4*u*fstride])

			scratch[7] = fftCpx{add32(scratch[1].r, scratch[4].r), add32(scratch[1].i, scratch[4].i)}
			scratch[10] = fftCpx{sub32(scratch[1].r, scratch[4].r), sub32(scratch[1].i, scratch[4].i)}
			scratch[8] = fftCpx{add32(scratch[2].r, scratch[3].r), add32(scratch[2].i, scratch[3].i)}
			scratch[9] = fftCpx{sub32(scratch[2].r, scratch[3].r), sub32(scratch[2].i, scratch[3].i)}

			fout[f0].r = add32(fout[f0].r, add32(scratch[7].r, scratch[8].r))
			fout[f0].i = add32(fout[f0].i, add32(scratch[7].i, scratch[8].i))

			scratch[5].r = add32(scratch[0].r, add32(sMul(scratch[7].r, ya.r), sMul(scratch[8].r, yb.r)))
			scratch[5].i = add32(scratch[0].i, add32(sMul(scratch[7].i, ya.r), sMul(scratch[8].i, yb.r)))

			scratch[6].r = add32(sMul(scratch[10].i, ya.i), sMul(scratch[9].i, yb.i))
			scratch[6].i = -(add32(sMul(scratch[10].r, ya.i), sMul(scratch[9].r, yb.i)))

			fout[f1] = fftCpx{sub32(scratch[5].r, scratch[6].r), sub32(scratch[5].i, scratch[6].i)}
			fout[f4] = fftCpx{add32(scratch[5].r, scratch[6].r), add32(scratch[5].i, scratch[6].i)}

			scratch[11].r = add32(scratch[0].r, add32(sMul(scratch[7].r, yb.r), sMul(scratch[8].r, ya.r)))
			scratch[11].i = add32(scratch[0].i, add32(sMul(scratch[7].i, yb.r), sMul(scratch[8].i, ya.r)))
			scratch[12].r = sub32(sMul(scratch[9].i, ya.i), sMul(scratch[10].i, yb.i))
			scratch[12].i = sub32(sMul(scratch[10].r, yb.i), sMul(scratch[9].r, ya.i))

			fout[f2] = fftCpx{add32(scratch[11].r, scratch[12].r), add32(scratch[11].i, scratch[12].i)}
			fout[f3] = fftCpx{sub32(scratch[11].r, scratch[12].r), sub32(scratch[11].i, scratch[12].i)}

			f0++
			f1++
			f2++
			f3++
			f4++
		}
	}
}

// fftImpl drives the butterfly chain (kiss_fft.c rnn_fft_impl).
func fftImpl(st *fftState, fout []fftCpx) {
	var fstride [maxFactors]int
	shift := 0
	if st.shift > 0 {
		shift = st.shift
	}
	fstride[0] = 1
	l := 0
	var m int
	for {
		p := int(st.factors[2*l])
		m = int(st.factors[2*l+1])
		fstride[l+1] = fstride[l] * p
		l++
		if m == 1 {
			break
		}
	}
	m = int(st.factors[2*l-1])
	for i := l - 1; i >= 0; i-- {
		var m2 int
		if i != 0 {
			m2 = int(st.factors[2*i-1])
		} else {
			m2 = 1
		}
		switch st.factors[2*i] {
		case 2:
			kfBfly2(fout, m, fstride[i])
		case 4:
			kfBfly4(fout, fstride[i]<<shift, st, m, fstride[i], m2)
		case 3:
			kfBfly3(fout, fstride[i]<<shift, st, m, fstride[i], m2)
		case 5:
			kfBfly5(fout, fstride[i]<<shift, st, m, fstride[i], m2)
		}
		m = m2
	}
}

// fftForward is rnn_fft_c: bit-reverse + scale the input, then run the
// butterfly chain. fin and fout must not alias.
func fftForward(st *fftState, fin, fout []fftCpx) {
	scale := st.scale
	for i := 0; i < st.nfft; i++ {
		x := fin[i]
		fout[st.bitrev[i]].r = mul32(scale, x.r)
		fout[st.bitrev[i]].i = mul32(scale, x.i)
	}
	fftImpl(st, fout)
}
