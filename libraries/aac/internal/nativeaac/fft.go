// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Fixed-point radix FFT primitives for the AAC-LC filterbank, a 1:1 port of the
// vendored libFDK DIT (decimation-in-time) Cooley-Tukey FFT. Everything here is
// integer FIXP_DBL (Q1.31) arithmetic — fft.cpp / fft_rad2.cpp contain zero
// floats — so the kernels are bit-identical regardless of build/vectorization
// and carry no aac_strict FP gate. Data layout: x holds interleaved complex
// samples, real at even indices and imaginary at odd indices, in place; the
// caller tracks the block exponent via the *scalefactor accumulation in fft().
//
// Scope: this is the "fft" decode stage — the DIT FFT (dit_fft) the DCT/MDCT
// filterbank builds on for the power-of-two lengths AAC-LC uses (64/128/256/512,
// dispatched from fft(); fft.cpp:1865-1908). The mixed-radix two-stage helpers
// (fft6/12/.../480) and the hard-coded fft_16/fft_32 are separate kernels not
// part of this slice.

// scramble performs the in-place bit-reversal permutation dit_fft applies at
// entry. 1:1 port of the generic scramble (scramble.h:128-159). length is the
// number of complex samples (n); x is interleaved (re,im) so element k lives at
// x[2k], x[2k+1].
//
//	for (m = 1, j = 0; m < length - 1; m++) {
//	  for (k = length >> 1; (!((j ^= k) & k)); k >>= 1) ;
//	  if (j > m) { swap x[2m],x[2j]; swap x[2m+1],x[2j+1]; }
//	}
func scramble(x []int32, length int) {
	j := 0
	for m := 1; m < length-1; m++ {
		for k := length >> 1; ; k >>= 1 {
			j ^= k
			if (j & k) != 0 {
				break
			}
		}
		if j > m {
			x[2*m], x[2*j] = x[2*j], x[2*m]
			x[2*m+1], x[2*j+1] = x[2*j+1], x[2*m+1]
		}
	}
}

// wPiFourth is W_PiFOURTH == STC(0x5a82799a), the cos(pi/4) == sin(pi/4) ~=
// 0.70710678 twiddle dit_fft's block 2 uses (fft_rad2.cpp:293/308 pass it as the
// b_Re/b_Im pair of cplxMultDiv2). Under SINETABLE_16BIT (the active config) the
// STC() macro narrows the raw 0x5a82799a Q1.31 hex to a FIXP_SGL (Q1.15) via
// FX_DBL2FXCONST_SGL == stcNarrow, so block 2 resolves to the 32x16X2 SGL
// overload (cplxMultDiv2SGL), exactly like the ROM-driven trig multiply — NOT
// the 32x32X2 DBL form. stcNarrow(0x5a82799a) == 23170 (0x5a82).
var wPiFourth = stcNarrow(0x5a82799a)

// ditFFT performs the in-place decimation-in-time Cooley-Tukey FFT of length
// n == 1<<ldn over the interleaved complex buffer x, using the trig ROM
// trigdata of trigDataSize quarter-table entries. 1:1 port of dit_fft
// (fft_rad2.cpp:131-322). Output scaled per the SCALEFACTOR<n> the caller adds
// to its exponent (see fft()). trigdata holds Q1.15 (FIXP_SGL) packed pairs
// (SINETABLE_16BIT active), so the trig multiply uses the 32x16X2 overload.
func ditFFT(x []int32, ldn int, trigdata []fixSTP, trigDataSize int) {
	n := 1 << ldn

	scramble(x, n)

	// 1+2 stage radix 4 (fft_rad2.cpp:143-164).
	for i := 0; i < n*2; i += 8 {
		a00 := (x[i+0] + x[i+2]) >> 1 // Re A + Re B
		a10 := (x[i+4] + x[i+6]) >> 1 // Re C + Re D
		a20 := (x[i+1] + x[i+3]) >> 1 // Im A + Im B
		a30 := (x[i+5] + x[i+7]) >> 1 // Im C + Im D

		x[i+0] = a00 + a10 // Re A'
		x[i+4] = a00 - a10 // Re C'
		x[i+1] = a20 + a30 // Im A'
		x[i+5] = a20 - a30 // Im C'

		a00 = a00 - x[i+2] // Re A - Re B
		a10 = a10 - x[i+6] // Re C - Re D
		a20 = a20 - x[i+3] // Im A - Im B
		a30 = a30 - x[i+7] // Im C - Im D

		x[i+2] = a00 + a30 // Re B'
		x[i+6] = a00 - a30 // Re D'
		x[i+3] = a20 - a10 // Im B'
		x[i+7] = a20 + a10 // Im D'
	}

	for ldm := 3; ldm <= ldn; ldm++ {
		m := 1 << ldm
		mh := m >> 1

		trigstep := (trigDataSize << 2) >> ldm

		// block 1: j == 0, c=1.0 s=0.0 done separately (fft_rad2.cpp:178-217).
		{
			j := 0
			for r := 0; r < n; r += m {
				t1 := (r + j) << 1
				t2 := t1 + (mh << 1)

				vi := x[t2+1] >> 1
				vr := x[t2] >> 1

				ur := x[t1] >> 1
				ui := x[t1+1] >> 1

				x[t1] = ur + vr
				x[t1+1] = ui + vi

				x[t2] = ur - vr
				x[t2+1] = ui - vi

				t1 += mh
				t2 = t1 + (mh << 1)

				vr = x[t2+1] >> 1
				vi = x[t2] >> 1

				ur = x[t1] >> 1
				ui = x[t1+1] >> 1

				x[t1] = ur + vr
				x[t1+1] = ui - vi

				x[t2] = ur - vr
				x[t2+1] = ui + vi
			}
		}

		for j := 1; j < mh/4; j++ {
			cs := trigdata[j*trigstep]

			for r := 0; r < n; r += m {
				t1 := (r + j) << 1
				t2 := t1 + (mh << 1)

				vi, vr := cplxMultDiv2SGL(x[t2+1], x[t2], cs.re, cs.im)

				ur := x[t1] >> 1
				ui := x[t1+1] >> 1

				x[t1] = ur + vr
				x[t1+1] = ui + vi

				x[t2] = ur - vr
				x[t2+1] = ui - vi

				t1 += mh
				t2 = t1 + (mh << 1)

				vr, vi = cplxMultDiv2SGL(x[t2+1], x[t2], cs.re, cs.im)

				ur = x[t1] >> 1
				ui = x[t1+1] >> 1

				x[t1] = ur + vr
				x[t1+1] = ui - vi

				x[t2] = ur - vr
				x[t2+1] = ui + vi

				// Same as above but for t1,t2 with j>mh/4 (cs swapped).
				t1 = (r + mh/2 - j) << 1
				t2 = t1 + (mh << 1)

				vi, vr = cplxMultDiv2SGL(x[t2], x[t2+1], cs.re, cs.im)

				ur = x[t1] >> 1
				ui = x[t1+1] >> 1

				x[t1] = ur + vr
				x[t1+1] = ui - vi

				x[t2] = ur - vr
				x[t2+1] = ui + vi

				t1 += mh
				t2 = t1 + (mh << 1)

				vr, vi = cplxMultDiv2SGL(x[t2], x[t2+1], cs.re, cs.im)

				ur = x[t1] >> 1
				ui = x[t1+1] >> 1

				x[t1] = ur - vr
				x[t1+1] = ui - vi

				x[t2] = ur + vr
				x[t2+1] = ui + vi
			}
		}

		// block 2: j == mh/4, twiddle == W_PiFOURTH narrowed to FIXP_SGL via
		// the 32x16X2 SGL overload (fft_rad2.cpp:285-320).
		{
			j := mh / 4
			for r := 0; r < n; r += m {
				t1 := (r + j) << 1
				t2 := t1 + (mh << 1)

				vi, vr := cplxMultDiv2SGL(x[t2+1], x[t2], wPiFourth, wPiFourth)

				ur := x[t1] >> 1
				ui := x[t1+1] >> 1

				x[t1] = ur + vr
				x[t1+1] = ui + vi

				x[t2] = ur - vr
				x[t2+1] = ui - vi

				t1 += mh
				t2 = t1 + (mh << 1)

				vr, vi = cplxMultDiv2SGL(x[t2+1], x[t2], wPiFourth, wPiFourth)

				ur = x[t1] >> 1
				ui = x[t1+1] >> 1

				x[t1] = ur + vr
				x[t1+1] = ui - vi

				x[t2] = ur - vr
				x[t2+1] = ui + vi
			}
		}
	}
}

// SCALEFACTOR<n> — the headroom (in bits) each fft length scales its output by;
// fft() adds this to *pScalefactor (the block exponent). 1:1 from fft.cpp:127-
// 134 (the DIT-FFT lengths). dit_fft scales by ldn-3 radix-2 stages plus the
// leading radix-4 stage, i.e. log2(n)-1, matching these.
const (
	scalefactor16  = 3 // fft.cpp:134
	scalefactor32  = 4 // fft.cpp:133
	scalefactor64  = 5 // fft.cpp:132
	scalefactor128 = 6 // fft.cpp:131
	scalefactor256 = 7 // fft.cpp:130
	scalefactor512 = 8 // fft.cpp:129
)

// fft is the libFDK fft() dispatcher (fft.cpp:1800-1914). The power-of-two
// AAC-LC filterbank lengths 64/128/256/512 route to the generic radix-2 dit_fft
// with the 512-point sine ROM; lengths 16/32 — needed by the SBR 64-band QMF,
// whose dct_IV/dst_IV at L==64 call fft(M==32) (dct.cpp) — route to the
// hard-coded fft_16/fft_32 kernels (fft_hardcoded.go), exactly as the C
// dispatches them (fft.cpp:1804-1812). pInput is interleaved complex (re,im) in
// place; the block exponent in *scalefactor is incremented by SCALEFACTOR<length>.
// Other lengths (the mixed-radix fftN2 kernels) are not part of this port and
// panic here (FDK_ASSERT(0) in the C default).
func fft(length int, pInput []int32, scalefactor *int) {
	switch length {
	case 16:
		fft16(pInput)
		*scalefactor += scalefactor16
	case 32:
		fft32(pInput)
		*scalefactor += scalefactor32
	case 64:
		ditFFT(pInput, 6, sineTable512Q15[:], 512)
		*scalefactor += scalefactor64
	case 128:
		ditFFT(pInput, 7, sineTable512Q15[:], 512)
		*scalefactor += scalefactor128
	case 256:
		ditFFT(pInput, 8, sineTable512Q15[:], 512)
		*scalefactor += scalefactor256
	case 512:
		ditFFT(pInput, 9, sineTable512Q15[:], 512)
		*scalefactor += scalefactor512
	default:
		panic("nativeaac: fft length not supported by the dit_fft slice")
	}
}
