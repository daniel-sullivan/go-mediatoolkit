// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

import "math"

// 1:1 Go translation of LAME 3.100's FFT/FHT
// (libraries/mp3/liblame/libmp3lame/fft.c). fht() is Ron Mayer's fast Hartley
// transform; fftLong / fftShort window the PCM and run the FHT to produce the
// real spectra the psychoacoustic model squares into energies. initFFT builds
// the Blackman (long) and Hann (short) analysis windows.
//
// FLOAT is float32 (machine.h). The transforms are FMA-sensitive: every
// `c*x + s*y` butterfly term is routed through the //go:noinline psFma /
// psMul / psAdd helpers so the mp3_strict build separately rounds the product
// before the add, matching the cgo oracle under -ffp-contract=off. The FHT is
// a serial in-place butterfly and is left scalar in both builds (it cannot be
// vectorized line-for-line without changing rounding).
//
// One subtlety is the `SQRT2 * gi[k]` term (fft.c:95-96): SQRT2 is a DOUBLE
// literal (util.h:79, = M_SQRT2), so C's usual arithmetic conversions promote
// gi[k] (float) to double, multiply in double precision, then narrow the result
// to float32 on assignment to the FLOAT lvalue. That is a single double-rounded
// product, NOT a float32 multiply, and is independent of -ffp-contract. It is
// routed through psMulD (double mul → float32) rather than psMul; casting SQRT2
// to float32 and multiplying in float32 diverges by a ULP and was the FHT
// parity failure (out[2]/out[8] under long_BLKSIZE_1024).

// triSize is fft.c:51, #define TRI_SIZE (5-1) — log2(BLKSIZE/256) levels of
// the trig recurrence table.
const triSize = 5 - 1

// costab is fft.c:55, the cos/sin twiddle seed table for the FHT trig
// generator (Buneman recurrence). Stored as doubles in the C; FLOAT is
// float32 so each entry narrows to float32 here exactly as the C does on load.
var costab = [triSize * 2]float32{
	9.238795325112867e-01, 3.826834323650898e-01,
	9.951847266721969e-01, 9.801714032956060e-02,
	9.996988186962042e-01, 2.454122852291229e-02,
	9.999811752826011e-01, 6.135884649154475e-03,
}

// fht is LAME's fht (fft.c:62): an in-place fast Hartley transform of n points
// in fz. fz is the slice starting at the transform's base; n is the point
// count (the caller passes BLKSIZE/2 etc., and the routine doubles it
// internally as the C does "to get BLKSIZE, because of 3DNow! ASM routine").
//
// The C indexes fz with raw pointers fi/gi advanced by k4 and compared against
// fn = fz+n. This port mirrors that with base-relative integer indices: fiBase
// / giBase advance by k4, and the inner do-while runs while fiBase < n.
func fht(fz []float32, n int) {
	tri := 0 // index into costab (the C's `const FLOAT *tri = costab`)
	var k4 int

	n <<= 1      // to get BLKSIZE, because of 3DNow! ASM routine
	fnLimit := n // fn = fz + n; the loop guard is fiBase < fnLimit
	k4 = 4
	for {
		var s1, c1 float32
		var i, k1, k2, k3, kx int
		kx = k4 >> 1
		k1 = k4
		k2 = k4 << 1
		k3 = k2 + k1
		k4 = k2 << 1
		fiBase := 0
		giBase := fiBase + kx
		for {
			var f0, f1, f2, f3 float32
			f1 = psSub(fz[fiBase+0], fz[fiBase+k1])
			f0 = psAdd(fz[fiBase+0], fz[fiBase+k1])
			f3 = psSub(fz[fiBase+k2], fz[fiBase+k3])
			f2 = psAdd(fz[fiBase+k2], fz[fiBase+k3])
			fz[fiBase+k2] = psSub(f0, f2)
			fz[fiBase+0] = psAdd(f0, f2)
			fz[fiBase+k3] = psSub(f1, f3)
			fz[fiBase+k1] = psAdd(f1, f3)
			f1 = psSub(fz[giBase+0], fz[giBase+k1])
			f0 = psAdd(fz[giBase+0], fz[giBase+k1])
			f3 = psMulD(sqrt2Const, fz[giBase+k3])
			f2 = psMulD(sqrt2Const, fz[giBase+k2])
			fz[giBase+k2] = psSub(f0, f2)
			fz[giBase+0] = psAdd(f0, f2)
			fz[giBase+k3] = psSub(f1, f3)
			fz[giBase+k1] = psAdd(f1, f3)
			giBase += k4
			fiBase += k4
			if fiBase >= fnLimit {
				break
			}
		}
		c1 = costab[tri+0]
		s1 = costab[tri+1]
		for i = 1; i < kx; i++ {
			var c2, s2 float32
			c2 = psSub(1, psMul(psMul(2, s1), s1))
			s2 = psMul(psMul(2, s1), c1)
			fiBase = i
			giBase = k1 - i
			for {
				var a, b, g0, f0, f1, g1, f2, g2, f3, g3 float32
				b = psFmaSub(psMul(s2, fz[fiBase+k1]), c2, fz[giBase+k1])
				a = psFma(psMul(c2, fz[fiBase+k1]), s2, fz[giBase+k1])
				f1 = psSub(fz[fiBase+0], a)
				f0 = psAdd(fz[fiBase+0], a)
				g1 = psSub(fz[giBase+0], b)
				g0 = psAdd(fz[giBase+0], b)
				b = psFmaSub(psMul(s2, fz[fiBase+k3]), c2, fz[giBase+k3])
				a = psFma(psMul(c2, fz[fiBase+k3]), s2, fz[giBase+k3])
				f3 = psSub(fz[fiBase+k2], a)
				f2 = psAdd(fz[fiBase+k2], a)
				g3 = psSub(fz[giBase+k2], b)
				g2 = psAdd(fz[giBase+k2], b)
				b = psFmaSub(psMul(s1, f2), c1, g3)
				a = psFma(psMul(c1, f2), s1, g3)
				fz[fiBase+k2] = psSub(f0, a)
				fz[fiBase+0] = psAdd(f0, a)
				fz[giBase+k3] = psSub(g1, b)
				fz[giBase+k1] = psAdd(g1, b)
				b = psFmaSub(psMul(c1, g2), s1, f3)
				a = psFma(psMul(s1, g2), c1, f3)
				fz[giBase+k2] = psSub(g0, a)
				fz[giBase+0] = psAdd(g0, a)
				fz[fiBase+k3] = psSub(f1, b)
				fz[fiBase+k1] = psAdd(f1, b)
				giBase += k4
				fiBase += k4
				if fiBase >= fnLimit {
					break
				}
			}
			c2 = c1
			c1 = psFmaSub(psMul(c2, costab[tri+0]), s1, costab[tri+1])
			s1 = psFma(psMul(c2, costab[tri+1]), s1, costab[tri+0])
		}
		tri += 2
		if k4 >= n {
			break
		}
	}
}

// rvTbl is LAME's rv_tbl (fft.c:150): the bit-reversal permutation table used
// to scatter windowed samples into transform order.
var rvTbl = [...]byte{
	0x00, 0x80, 0x40, 0xc0, 0x20, 0xa0, 0x60, 0xe0,
	0x10, 0x90, 0x50, 0xd0, 0x30, 0xb0, 0x70, 0xf0,
	0x08, 0x88, 0x48, 0xc8, 0x28, 0xa8, 0x68, 0xe8,
	0x18, 0x98, 0x58, 0xd8, 0x38, 0xb8, 0x78, 0xf8,
	0x04, 0x84, 0x44, 0xc4, 0x24, 0xa4, 0x64, 0xe4,
	0x14, 0x94, 0x54, 0xd4, 0x34, 0xb4, 0x74, 0xf4,
	0x0c, 0x8c, 0x4c, 0xcc, 0x2c, 0xac, 0x6c, 0xec,
	0x1c, 0x9c, 0x5c, 0xdc, 0x3c, 0xbc, 0x7c, 0xfc,
	0x02, 0x82, 0x42, 0xc2, 0x22, 0xa2, 0x62, 0xe2,
	0x12, 0x92, 0x52, 0xd2, 0x32, 0xb2, 0x72, 0xf2,
	0x0a, 0x8a, 0x4a, 0xca, 0x2a, 0xaa, 0x6a, 0xea,
	0x1a, 0x9a, 0x5a, 0xda, 0x3a, 0xba, 0x7a, 0xfa,
	0x06, 0x86, 0x46, 0xc6, 0x26, 0xa6, 0x66, 0xe6,
	0x16, 0x96, 0x56, 0xd6, 0x36, 0xb6, 0x76, 0xf6,
	0x0e, 0x8e, 0x4e, 0xce, 0x2e, 0xae, 0x6e, 0xee,
	0x1e, 0x9e, 0x5e, 0xde, 0x3e, 0xbe, 0x7e, 0xfe,
}

// fftShort is LAME's fft_short (fft.c:191). For each of the 3 short sub-blocks
// it windows 256 samples of buffer[chn] (offset by k = 192*(b+1)) with the
// Hann window window_s, scatters them via rv_tbl into x_real[b], and runs the
// FHT. xReal is [3][BLKSIZEs]; buffer is the two-channel split PCM windows.
//
// The C macros ms00..ms31 expand to window_s[idx]*ch01(i+k+off); ch01(index)
// is buffer[chn][index]. Reproduced inline.
func (pm *LameInternalFlags) fftShort(xReal *[3][BLKSIZEs]float32, chn int, buffer [2][]float32) {
	windowS := pm.CdPsy.WindowS[:]
	buf := buffer[chn]

	for b := 0; b < 3; b++ {
		// x = &x_real[b][BLKSIZE_s/2]; we index x_real[b] with xOff (= the C's
		// running x pointer, which starts at BLKSIZE_s/2 and is decremented).
		x := &xReal[b]
		k := (576 / 3) * (b + 1)
		xOff := BLKSIZEs / 2
		j := BLKSIZEs/8 - 1
		for {
			var f0, f1, f2, f3, w float32
			i := int(rvTbl[j<<2])

			f0 = psMul(windowS[i], buf[i+k])          // ms00
			w = psMul(windowS[0x7f-i], buf[i+k+0x80]) // ms10
			f1 = psSub(f0, w)
			f0 = psAdd(f0, w)
			f2 = psMul(windowS[i+0x40], buf[i+k+0x40]) // ms20
			w = psMul(windowS[0x3f-i], buf[i+k+0xc0])  // ms30
			f3 = psSub(f2, w)
			f2 = psAdd(f2, w)

			xOff -= 4
			x[xOff+0] = psAdd(f0, f2)
			x[xOff+2] = psSub(f0, f2)
			x[xOff+1] = psAdd(f1, f3)
			x[xOff+3] = psSub(f1, f3)

			f0 = psMul(windowS[i+0x01], buf[i+k+0x01]) // ms01
			w = psMul(windowS[0x7e-i], buf[i+k+0x81])  // ms11
			f1 = psSub(f0, w)
			f0 = psAdd(f0, w)
			f2 = psMul(windowS[i+0x41], buf[i+k+0x41]) // ms21
			w = psMul(windowS[0x3e-i], buf[i+k+0xc1])  // ms31
			f3 = psSub(f2, w)
			f2 = psAdd(f2, w)

			x[xOff+BLKSIZEs/2+0] = psAdd(f0, f2)
			x[xOff+BLKSIZEs/2+2] = psSub(f0, f2)
			x[xOff+BLKSIZEs/2+1] = psAdd(f1, f3)
			x[xOff+BLKSIZEs/2+3] = psSub(f1, f3)

			j--
			if j < 0 {
				break
			}
		}
		// gfc->fft_fht(x, BLKSIZE_s/2). x points at x_real[b][0] (the C passed
		// the original x_real[b], not the offset cursor: after the loop the
		// cursor reached 0; the FHT operates on the whole x_real[b]).
		fht(x[:], BLKSIZEs/2)
	}
}

// fftLong is LAME's fft_long (fft.c:249): windows BLKSIZE samples of
// buffer[chn] with the Blackman window, scatters them via rv_tbl into x, and
// runs the FHT. The C macros ml00..ml31 expand to window[idx]*ch01(idx).
func (pm *LameInternalFlags) fftLong(x *[BLKSIZE]float32, chn int, buffer [2][]float32) {
	window := pm.CdPsy.Window[:]
	buf := buffer[chn]

	jj := BLKSIZE/8 - 1
	xOff := BLKSIZE / 2
	for {
		var f0, f1, f2, f3, w float32
		i := int(rvTbl[jj])

		f0 = psMul(window[i], buf[i])            // ml00
		w = psMul(window[i+0x200], buf[i+0x200]) // ml10
		f1 = psSub(f0, w)
		f0 = psAdd(f0, w)
		f2 = psMul(window[i+0x100], buf[i+0x100]) // ml20
		w = psMul(window[i+0x300], buf[i+0x300])  // ml30
		f3 = psSub(f2, w)
		f2 = psAdd(f2, w)

		xOff -= 4
		x[xOff+0] = psAdd(f0, f2)
		x[xOff+2] = psSub(f0, f2)
		x[xOff+1] = psAdd(f1, f3)
		x[xOff+3] = psSub(f1, f3)

		f0 = psMul(window[i+0x001], buf[i+0x001]) // ml01
		w = psMul(window[i+0x201], buf[i+0x201])  // ml11
		f1 = psSub(f0, w)
		f0 = psAdd(f0, w)
		f2 = psMul(window[i+0x101], buf[i+0x101]) // ml21
		w = psMul(window[i+0x301], buf[i+0x301])  // ml31
		f3 = psSub(f2, w)
		f2 = psAdd(f2, w)

		x[xOff+BLKSIZE/2+0] = psAdd(f0, f2)
		x[xOff+BLKSIZE/2+2] = psSub(f0, f2)
		x[xOff+BLKSIZE/2+1] = psAdd(f1, f3)
		x[xOff+BLKSIZE/2+3] = psSub(f1, f3)

		jj--
		if jj < 0 {
			break
		}
	}
	fht(x[:], BLKSIZE/2)
}

// initFFT is LAME's init_fft (fft.c:307): builds the Blackman long-block
// window and the Hann short-block window into the PsyConst. The vendored build
// has no NASM/SSE FHT, so fft_fht is always fht (we call fht directly). The
// double-precision cos here matches the C; the result narrows to float32.
func initFFT(cd *PsyConst) {
	// blackman window
	for i := 0; i < BLKSIZE; i++ {
		cd.Window[i] = float32(0.42 -
			0.5*math.Cos(2*piConst*(float64(i)+0.5)/BLKSIZE) +
			0.08*math.Cos(4*piConst*(float64(i)+0.5)/BLKSIZE))
	}
	for i := 0; i < BLKSIZEs/2; i++ {
		cd.WindowS[i] = float32(0.5 * (1.0 - math.Cos(2.0*piConst*(float64(i)+0.5)/BLKSIZEs)))
	}
}
