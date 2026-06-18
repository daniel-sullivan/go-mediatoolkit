package nativeopus

import "math"

// Port of libopus/celt/kiss_fft.h (types + prototypes) and
// kiss_fft.c (butterflies + opus_fft_impl + opus_fft_c/opus_ifft_c).
// Float-path only.
//
// Our build does not define CUSTOM_MODES or USE_SIMD, so the
// allocation helpers (opus_fft_alloc*, opus_fft_free, compute_twiddles,
// compute_bitrev_table, kf_factor) are not compiled — kiss_fft_state
// instances come from static_modes_float.h precomputed tables instead.
// Only opus_fft_alloc_arch_c / opus_fft_free_arch_c exist in this port
// and both are no-ops.
//
// All FMA-pattern expressions are routed through fma.go's non-fused
// helpers (mul_f32 / add_f32 / sub_f32 / fma_add / fma_sub) so the
// output is bit-identical to the Cgo oracle compiled with
// -ffp-contract=off. See memory/feedback_opus_parity_fp.md.

const MAXFACTORS = 8

// kiss_fft_scalar / kiss_twiddle_scalar — both plain float32 in the
// float build.
type (
	kiss_fft_scalar     = float32
	kiss_twiddle_scalar = float32
)

// kiss_fft_cpx — complex sample. C: kiss_fft.h:78-81.
type kiss_fft_cpx struct {
	r kiss_fft_scalar
	i kiss_fft_scalar
}

// kiss_twiddle_cpx — complex twiddle factor. C: kiss_fft.h:83-86.
type kiss_twiddle_cpx struct {
	r kiss_twiddle_scalar
	i kiss_twiddle_scalar
}

// arch_fft_state — unused in our build but kept for struct-layout
// parity with C. C: kiss_fft.h:94-97.
type arch_fft_state struct {
	is_supported int
	priv         unsafe_priv
}
type unsafe_priv = struct{} // void* in C; no consumers in our build.

// kiss_fft_state — FFT configuration. C: kiss_fft.h:99-110.
//
// The factors, bitrev, and twiddles slices are populated from the
// static_modes_float.h tables; they are not allocated at runtime.
type kiss_fft_state struct {
	nfft     int
	scale    celt_coef
	shift    int
	factors  [2 * MAXFACTORS]opus_int16
	bitrev   []opus_int16
	twiddles []kiss_twiddle_cpx
	arch_fft *arch_fft_state
}

// ── Complex arithmetic helpers ──────────────────────────────────────
//
// Direct transcriptions of _kiss_fft_guts.h's float-branch macros.
// Each multiply routes through mul_f32 so Go doesn't fuse it into
// any surrounding add/sub.

// c_add: res = a + b. Plain component-wise add; no multiply in
// scope, so direct `+` is safe.
func c_add(a, b kiss_fft_cpx) kiss_fft_cpx {
	return kiss_fft_cpx{r: a.r + b.r, i: a.i + b.i}
}

// c_sub: res = a - b.
func c_sub(a, b kiss_fft_cpx) kiss_fft_cpx {
	return kiss_fft_cpx{r: a.r - b.r, i: a.i - b.i}
}

// c_mul: res = a * b (complex). C: (a.r*b.r - a.i*b.i, a.r*b.i + a.i*b.r).
func c_mul(a kiss_fft_cpx, b kiss_twiddle_cpx) kiss_fft_cpx {
	return kiss_fft_cpx{
		r: sub_f32(mul_f32(a.r, b.r), mul_f32(a.i, b.i)),
		i: add_f32(mul_f32(a.r, b.i), mul_f32(a.i, b.r)),
	}
}

// c_mulc: res = a * conj(b). C: (a.r*b.r + a.i*b.i, a.i*b.r - a.r*b.i).
func c_mulc(a kiss_fft_cpx, b kiss_twiddle_cpx) kiss_fft_cpx {
	return kiss_fft_cpx{
		r: add_f32(mul_f32(a.r, b.r), mul_f32(a.i, b.i)),
		i: sub_f32(mul_f32(a.i, b.r), mul_f32(a.r, b.i)),
	}
}

// c_mulbyscalar: c *= s.
func c_mulbyscalar(c kiss_fft_cpx, s kiss_twiddle_scalar) kiss_fft_cpx {
	return kiss_fft_cpx{r: mul_f32(c.r, s), i: mul_f32(c.i, s)}
}

// S_MUL scalar multiply. Float mode: a*b with no surrounding add.
func S_MUL(a, b kiss_fft_scalar) kiss_fft_scalar  { return mul_f32(a, b) }
func S_MUL2(a, b kiss_fft_scalar) kiss_fft_scalar { return mul_f32(a, b) }

// HALF_OF: C macro `(x)*.5f` in float mode.
func HALF_OF(x kiss_fft_scalar) kiss_fft_scalar { return mul_f32(x, 0.5) }

// ─── Butterflies ────────────────────────────────────────────────────

// kf_bfly2 — radix-2 butterfly. In our default non-CUSTOM_MODES
// build m is always 4 (radix-2 follows a radix-4); with CUSTOM_MODES
// the degenerate m==1 case is also reachable. Both paths are ported
// so tests can exercise arbitrary nfft factorisations.
// C: kiss_fft.c:52-106.
func kf_bfly2(Fout []kiss_fft_cpx, m int, N int) {
	if m == 1 {
		off := 0
		for i := 0; i < N; i++ {
			t := Fout[off+1]
			Fout[off+1] = c_sub(Fout[off], t)
			Fout[off].r += t.r
			Fout[off].i += t.i
			off += 2
		}
		return
	}
	// celt_coef tw = QCONST32(0.7071067812f, COEF_SHIFT-1)
	tw := celt_coef(0.7071067812)
	celt_assert(m == 4)
	off := 0
	for i := 0; i < N; i++ {
		// Fout2 = Fout + 4 in C; in Go that's Fout[off+4+..]
		var t kiss_fft_cpx

		t = Fout[off+4]
		Fout[off+4] = c_sub(Fout[off], t)
		Fout[off].r += t.r
		Fout[off].i += t.i

		// t.r = S_MUL(Fout2[1].r + Fout2[1].i, tw)
		// t.i = S_MUL(Fout2[1].i - Fout2[1].r, tw)
		t.r = S_MUL(Fout[off+5].r+Fout[off+5].i, tw)
		t.i = S_MUL(Fout[off+5].i-Fout[off+5].r, tw)
		Fout[off+5] = c_sub(Fout[off+1], t)
		Fout[off+1].r += t.r
		Fout[off+1].i += t.i

		// t.r = Fout2[2].i; t.i = -Fout2[2].r
		t.r = Fout[off+6].i
		t.i = -Fout[off+6].r
		Fout[off+6] = c_sub(Fout[off+2], t)
		Fout[off+2].r += t.r
		Fout[off+2].i += t.i

		// t.r = S_MUL(Fout2[3].i - Fout2[3].r, tw)
		// t.i = S_MUL(-(Fout2[3].i + Fout2[3].r), tw)
		t.r = S_MUL(Fout[off+7].i-Fout[off+7].r, tw)
		t.i = S_MUL(-(Fout[off+7].i + Fout[off+7].r), tw)
		Fout[off+7] = c_sub(Fout[off+3], t)
		Fout[off+3].r += t.r
		Fout[off+3].i += t.i

		off += 8
	}
}

// kf_bfly4 — radix-4 butterfly. C: kiss_fft.c:108-175.
func kf_bfly4(Fout []kiss_fft_cpx, fstride int, st *kiss_fft_state,
	m, N, mm int) {
	if m == 1 {
		// Degenerate: all twiddles are 1.
		off := 0
		for i := 0; i < N; i++ {
			var scratch0, scratch1 kiss_fft_cpx

			scratch0 = c_sub(Fout[off], Fout[off+2])
			// *Fout += Fout[2]
			Fout[off].r += Fout[off+2].r
			Fout[off].i += Fout[off+2].i
			scratch1 = c_add(Fout[off+1], Fout[off+3])
			Fout[off+2] = c_sub(Fout[off], scratch1)
			Fout[off].r += scratch1.r
			Fout[off].i += scratch1.i
			scratch1 = c_sub(Fout[off+1], Fout[off+3])

			Fout[off+1].r = scratch0.r + scratch1.i
			Fout[off+1].i = scratch0.i - scratch1.r
			Fout[off+3].r = scratch0.r - scratch1.i
			Fout[off+3].i = scratch0.i + scratch1.r
			off += 4
		}
		return
	}
	var scratch [6]kiss_fft_cpx
	m2 := 2 * m
	m3 := 3 * m
	for i := 0; i < N; i++ {
		fbase := i * mm
		tw1, tw2, tw3 := 0, 0, 0
		for j := 0; j < m; j++ {
			pos := fbase + j
			scratch[0] = c_mul(Fout[pos+m], st.twiddles[tw1])
			scratch[1] = c_mul(Fout[pos+m2], st.twiddles[tw2])
			scratch[2] = c_mul(Fout[pos+m3], st.twiddles[tw3])

			scratch[5] = c_sub(Fout[pos], scratch[1])
			Fout[pos].r += scratch[1].r
			Fout[pos].i += scratch[1].i
			scratch[3] = c_add(scratch[0], scratch[2])
			scratch[4] = c_sub(scratch[0], scratch[2])
			Fout[pos+m2] = c_sub(Fout[pos], scratch[3])
			tw1 += fstride
			tw2 += fstride * 2
			tw3 += fstride * 3
			Fout[pos].r += scratch[3].r
			Fout[pos].i += scratch[3].i

			Fout[pos+m].r = scratch[5].r + scratch[4].i
			Fout[pos+m].i = scratch[5].i - scratch[4].r
			Fout[pos+m3].r = scratch[5].r - scratch[4].i
			Fout[pos+m3].i = scratch[5].i + scratch[4].r
		}
	}
}

// kf_bfly3 — radix-3 butterfly. C: kiss_fft.c:180-235.
func kf_bfly3(Fout []kiss_fft_cpx, fstride int, st *kiss_fft_state,
	m, N, mm int) {
	m2 := 2 * m
	var scratch [5]kiss_fft_cpx
	// float build: epi3 = st->twiddles[fstride*m]
	epi3 := st.twiddles[fstride*m]
	for i := 0; i < N; i++ {
		fbase := i * mm
		tw1, tw2 := 0, 0
		// For non-custom modes, m is guaranteed to be a multiple of 4.
		k := m
		for {
			pos := fbase
			scratch[1] = c_mul(Fout[pos+m], st.twiddles[tw1])
			scratch[2] = c_mul(Fout[pos+m2], st.twiddles[tw2])

			scratch[3] = c_add(scratch[1], scratch[2])
			scratch[0] = c_sub(scratch[1], scratch[2])
			tw1 += fstride
			tw2 += fstride * 2

			Fout[pos+m].r = Fout[pos].r - HALF_OF(scratch[3].r)
			Fout[pos+m].i = Fout[pos].i - HALF_OF(scratch[3].i)

			// C_MULBYSCALAR(scratch[0], epi3.i)
			scratch[0] = c_mulbyscalar(scratch[0], epi3.i)

			Fout[pos].r += scratch[3].r
			Fout[pos].i += scratch[3].i

			Fout[pos+m2].r = Fout[pos+m].r + scratch[0].i
			Fout[pos+m2].i = Fout[pos+m].i - scratch[0].r

			Fout[pos+m].r -= scratch[0].i
			Fout[pos+m].i += scratch[0].r

			fbase++
			k--
			if k == 0 {
				break
			}
		}
	}
}

// kf_bfly5 — radix-5 butterfly. C: kiss_fft.c:239-312.
func kf_bfly5(Fout []kiss_fft_cpx, fstride int, st *kiss_fft_state,
	m, N, mm int) {
	var scratch [13]kiss_fft_cpx
	// float build: ya, yb read from st->twiddles.
	ya := st.twiddles[fstride*m]
	yb := st.twiddles[fstride*2*m]
	tw := st.twiddles

	for i := 0; i < N; i++ {
		fbase := i * mm
		Fout0 := fbase
		Fout1 := Fout0 + m
		Fout2 := Fout0 + 2*m
		Fout3 := Fout0 + 3*m
		Fout4 := Fout0 + 4*m

		for u := 0; u < m; u++ {
			scratch[0] = Fout[Fout0]

			scratch[1] = c_mul(Fout[Fout1], tw[u*fstride])
			scratch[2] = c_mul(Fout[Fout2], tw[2*u*fstride])
			scratch[3] = c_mul(Fout[Fout3], tw[3*u*fstride])
			scratch[4] = c_mul(Fout[Fout4], tw[4*u*fstride])

			scratch[7] = c_add(scratch[1], scratch[4])
			scratch[10] = c_sub(scratch[1], scratch[4])
			scratch[8] = c_add(scratch[2], scratch[3])
			scratch[9] = c_sub(scratch[2], scratch[3])

			Fout[Fout0].r += scratch[7].r + scratch[8].r
			Fout[Fout0].i += scratch[7].i + scratch[8].i

			// C parenthesises as `s0 + (a + b)` via nested ADD32_ovflw
			// — preserve that grouping so the two FADDs happen in the
			// same order as the oracle.
			scratch[5].r = scratch[0].r +
				(mul_f32(scratch[7].r, ya.r) + mul_f32(scratch[8].r, yb.r))
			scratch[5].i = scratch[0].i +
				(mul_f32(scratch[7].i, ya.r) + mul_f32(scratch[8].i, yb.r))

			scratch[6].r = mul_f32(scratch[10].i, ya.i) + mul_f32(scratch[9].i, yb.i)
			scratch[6].i = -(mul_f32(scratch[10].r, ya.i) + mul_f32(scratch[9].r, yb.i))

			Fout[Fout1] = c_sub(scratch[5], scratch[6])
			Fout[Fout4] = c_add(scratch[5], scratch[6])

			scratch[11].r = scratch[0].r +
				(mul_f32(scratch[7].r, yb.r) + mul_f32(scratch[8].r, ya.r))
			scratch[11].i = scratch[0].i +
				(mul_f32(scratch[7].i, yb.r) + mul_f32(scratch[8].i, ya.r))
			scratch[12].r = mul_f32(scratch[9].i, ya.i) - mul_f32(scratch[10].i, yb.i)
			scratch[12].i = mul_f32(scratch[10].r, yb.i) - mul_f32(scratch[9].r, ya.i)

			Fout[Fout2] = c_add(scratch[11], scratch[12])
			Fout[Fout3] = c_sub(scratch[11], scratch[12])

			Fout0++
			Fout1++
			Fout2++
			Fout3++
			Fout4++
		}
	}
}

// opus_fft_alloc_arch_c / opus_fft_free_arch_c are no-ops in our
// build (arch-specific FFT allocation is an ARM NE10 feature we
// don't support). C: kiss_fft.c:435-438, 519-521.
func opus_fft_alloc_arch_c(st *kiss_fft_state) int { _ = st; return 0 }
func opus_fft_free_arch_c(st *kiss_fft_state)      { _ = st }

// ─── CUSTOM_MODES allocation helpers ────────────────────────────────
//
// In the production codec these are not compiled — kiss_fft_state
// objects come from static_modes_float.h precomputed data. Ported
// here so parity tests can construct states at runtime without
// needing the full modes table. C: kiss_fft.c:319-433 (inside the
// `#ifdef CUSTOM_MODES` block).

// compute_bitrev_table — recursive bit-reversal index build.
// C: kiss_fft.c:321-352.
func compute_bitrev_table(Fout int, f []opus_int16, fstride, in_stride int,
	factors []opus_int16, st *kiss_fft_state) {
	p := int(factors[0])
	m := int(factors[1])
	factorsRest := factors[2:]
	if m == 1 {
		for j := 0; j < p; j++ {
			f[0] = opus_int16(Fout + j)
			f = f[fstride*in_stride:]
		}
	} else {
		for j := 0; j < p; j++ {
			compute_bitrev_table(Fout, f, fstride*p, in_stride, factorsRest, st)
			f = f[fstride*in_stride:]
			Fout += m
		}
	}
}

// kf_factor — factor nfft into {4, 2, 3, 5} radices. Returns 1 on
// success, 0 if nfft has a prime factor > 5. C: kiss_fft.c:358-411.
func kf_factor(n int, facbuf []opus_int16) int {
	p := 4
	stages := 0
	nbak := n
	for {
		for n%p != 0 {
			switch p {
			case 4:
				p = 2
			case 2:
				p = 3
			default:
				p += 2
			}
			if p > 32000 || p*p > n {
				p = n
			}
		}
		n /= p
		if p > 5 {
			return 0
		}
		facbuf[2*stages] = opus_int16(p)
		if p == 2 && stages > 1 {
			facbuf[2*stages] = 4
			facbuf[2] = 2
		}
		stages++
		if n <= 1 {
			break
		}
	}
	n = nbak
	// Reverse the order so radix-4 is last.
	for i := 0; i < stages/2; i++ {
		facbuf[2*i], facbuf[2*(stages-i-1)] = facbuf[2*(stages-i-1)], facbuf[2*i]
	}
	for i := 0; i < stages; i++ {
		n /= int(facbuf[2*i])
		facbuf[2*i+1] = opus_int16(n)
	}
	return 1
}

// compute_twiddles — fill `twiddles[0..nfft)` with exp(-2πi·k/nfft).
// Float branch only. C: kiss_fft.c:426-432.
func compute_twiddles(twiddles []kiss_twiddle_cpx, nfft int) {
	const pi = 3.14159265358979323846264338327
	for i := 0; i < nfft; i++ {
		phase := (-2 * pi / float64(nfft)) * float64(i)
		twiddles[i].r = kiss_twiddle_scalar(cosF(phase))
		twiddles[i].i = kiss_twiddle_scalar(sinF(phase))
	}
}

// opus_fft_alloc_twiddles — allocate a kiss_fft_state with freshly
// computed twiddles/factors/bitrev. C: kiss_fft.c:446-512. Unlike the
// C version we have no custom mem pool; every allocation is a plain
// make() and lifetime is GC-managed.
func opus_fft_alloc_twiddles(nfft int, base *kiss_fft_state, arch int) *kiss_fft_state {
	st := &kiss_fft_state{nfft: nfft}
	// float build: st->scale = 1.f/nfft
	st.scale = celt_coef(1.0 / float32(nfft))
	if base != nil {
		st.twiddles = base.twiddles
		st.shift = 0
		for st.shift < 32 && nfft<<st.shift != base.nfft {
			st.shift++
		}
		if st.shift >= 32 {
			return nil
		}
	} else {
		st.twiddles = make([]kiss_twiddle_cpx, nfft)
		compute_twiddles(st.twiddles, nfft)
		st.shift = -1
	}
	if kf_factor(nfft, st.factors[:]) == 0 {
		return nil
	}
	st.bitrev = make([]opus_int16, nfft)
	compute_bitrev_table(0, st.bitrev, 1, 1, st.factors[:], st)
	if opus_fft_alloc_arch_c(st) != 0 {
		return nil
	}
	_ = arch
	return st
}

// opus_fft_alloc — convenience wrapper around opus_fft_alloc_twiddles.
// C: kiss_fft.c:514-517.
func opus_fft_alloc(nfft int, arch int) *kiss_fft_state {
	return opus_fft_alloc_twiddles(nfft, nil, arch)
}

// opus_fft_free — release a state. Go has GC so this is a no-op but
// kept so call-sites port 1:1. C: kiss_fft.c:523-533.
func opus_fft_free(cfg *kiss_fft_state, arch int) {
	_ = cfg
	_ = arch
}

// cosF / sinF — math.Cos / math.Sin wrappers dropping down through
// float64 like the C `(kiss_fft_scalar)cos(phase)` cast.
func cosF(x float64) float32 { return float32(math.Cos(x)) }
func sinF(x float64) float32 { return float32(math.Sin(x)) }

// opus_fft_impl — main FFT dispatch across the factor chain.
// C: kiss_fft.c:562-613 (float branch; FIXED_POINT fft_downshift stubs
// collapse to no-ops under `#else #define fft_downshift(...)`).
func opus_fft_impl(st *kiss_fft_state, fout []kiss_fft_cpx) {
	var m2, m int
	var p int
	var L int
	var fstride [MAXFACTORS]int

	// st.shift can be -1; use 0 when negative.
	shift := 0
	if st.shift > 0 {
		shift = st.shift
	}

	fstride[0] = 1
	L = 0
	for {
		p = int(st.factors[2*L])
		m = int(st.factors[2*L+1])
		fstride[L+1] = fstride[L] * p
		L++
		if m == 1 {
			break
		}
	}
	m = int(st.factors[2*L-1])
	for i := L - 1; i >= 0; i-- {
		if i != 0 {
			m2 = int(st.factors[2*i-1])
		} else {
			m2 = 1
		}
		switch st.factors[2*i] {
		case 2:
			kf_bfly2(fout, m, fstride[i])
		case 4:
			kf_bfly4(fout, fstride[i]<<shift, st, m, fstride[i], m2)
		case 3:
			kf_bfly3(fout, fstride[i]<<shift, st, m, fstride[i], m2)
		case 5:
			kf_bfly5(fout, fstride[i]<<shift, st, m, fstride[i], m2)
		}
		m = m2
	}
}

// opus_fft_c — forward FFT. Bit-reverses the input then drives
// opus_fft_impl. C: kiss_fft.c:615-635.
func opus_fft_c(st *kiss_fft_state, fin, fout []kiss_fft_cpx) {
	scale := st.scale
	celt_assert2(sliceSameCpxStart(fin, fout) == false, "In-place FFT not supported")
	for i := 0; i < st.nfft; i++ {
		x := fin[i]
		fout[st.bitrev[i]].r = S_MUL2(x.r, scale)
		fout[st.bitrev[i]].i = S_MUL2(x.i, scale)
	}
	opus_fft_impl(st, fout)
}

// opus_ifft_c — inverse FFT. C: kiss_fft.c:638-650.
func opus_ifft_c(st *kiss_fft_state, fin, fout []kiss_fft_cpx) {
	celt_assert2(sliceSameCpxStart(fin, fout) == false, "In-place FFT not supported")
	for i := 0; i < st.nfft; i++ {
		fout[st.bitrev[i]] = fin[i]
	}
	for i := 0; i < st.nfft; i++ {
		fout[i].i = -fout[i].i
	}
	opus_fft_impl(st, fout)
	for i := 0; i < st.nfft; i++ {
		fout[i].i = -fout[i].i
	}
}

// sliceSameCpxStart — honor the C `assert(fin != fout)` contract.
func sliceSameCpxStart(a, b []kiss_fft_cpx) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	return &a[0] == &b[0]
}

// ─── non-override opus_fft / opus_ifft wrappers ─────────────────────
//
// Our build does not define HAVE_ARM_NE10 or OPUS_HAVE_RTCD, so the
// OPUS_FFT/OPUS_IFFT function-pointer dispatch tables from kiss_fft.h
// collapse to direct calls into opus_fft_c / opus_ifft_c.

func opus_fft(cfg *kiss_fft_state, fin, fout []kiss_fft_cpx, arch int) {
	_ = arch
	opus_fft_c(cfg, fin, fout)
}

func opus_ifft(cfg *kiss_fft_state, fin, fout []kiss_fft_cpx, arch int) {
	_ = arch
	opus_ifft_c(cfg, fin, fout)
}
