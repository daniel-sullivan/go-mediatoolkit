// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Exported views of the shared fixed-point kernels the HE-AAC v2 parametric
// stereo decoder (internal/nativeaac/sbr ps_*.go) builds on. Like the QMF/SBR
// export surfaces, these add no logic — they keep ONE coherent definition of
// each kernel/ROM in this package and let the sbr package reuse them without
// duplicating a static symbol. All values are int32 FIXP_DBL (Q-format) / int16
// FIXP_SGL (Q1.15); every kernel is a pure integer kernel, so EXACT-integer
// parity holds in any build.

// CplxMultDiv2SGL is the 32x16X2 complex multiply (a_Re,a_Im FIXP_DBL times
// b_Re,b_Im FIXP_SGL), each product scaled down by 2. On the build platform
// (__ARM_ARCH_8__) the C 32x16X2 form widens b to FIXP_DBL (<<16) and forwards to
// the 32x32X2 arm path that accumulates the two int64 products and shifts >>32
// ONCE (cplx_mul_arm.h:174-197 -> :116-148) — exactly cplxMultDiv2SGL ->
// cplxMultDiv2DBL. The hybrid filterbank's cplxMultDiv2(...,FIXP_SPK) (packed SGL
// twiddle) and fft_8's w_PiFOURTH multiplies land here.
func CplxMultDiv2SGL(aRe, aIm int32, bRe, bIm int16) (cRe, cIm int32) {
	return cplxMultDiv2SGL(aRe, aIm, bRe, bIm)
}

// Fft8 is the hardcoded length-8 complex FFT fft_8 (fft.h:171-260), interleaved
// re/im in x[0..15] in place. Used by the PS hybrid filterbank's
// eightChannelFiltering. Reuses the package's cplxMultDiv2SGL for the two
// w_PiFOURTH (0x5A82 packed) twiddles, so it matches the C bit-for-bit on the
// __ARM_ARCH_8__ target.
func Fft8(x []int32) {
	const w = int16(0x5A82) // w_PiFOURTH.v.re == .v.im

	var a00, a10, a20, a30 int32

	a00 = (x[0] + x[8]) >> 1
	a10 = x[4] + x[12]
	a20 = (x[1] + x[9]) >> 1
	a30 = x[5] + x[13]

	var y [16]int32
	y[0] = a00 + (a10 >> 1)
	y[4] = a00 - (a10 >> 1)
	y[1] = a20 + (a30 >> 1)
	y[5] = a20 - (a30 >> 1)

	a00 = a00 - x[8]
	a10 = (a10 >> 1) - x[12]
	a20 = a20 - x[9]
	a30 = (a30 >> 1) - x[13]

	y[2] = a00 + a30
	y[6] = a00 - a30
	y[3] = a20 - a10
	y[7] = a20 + a10

	a00 = (x[2] + x[10]) >> 1
	a10 = x[6] + x[14]
	a20 = (x[3] + x[11]) >> 1
	a30 = x[7] + x[15]

	y[8] = a00 + (a10 >> 1)
	y[12] = a00 - (a10 >> 1)
	y[9] = a20 + (a30 >> 1)
	y[13] = a20 - (a30 >> 1)

	a00 = a00 - x[10]
	a10 = (a10 >> 1) - x[14]
	a20 = a20 - x[11]
	a30 = (a30 >> 1) - x[15]

	y[10] = a00 + a30
	y[14] = a00 - a30
	y[11] = a20 - a10
	y[15] = a20 + a10

	var vr, vi, ur, ui int32

	ur = y[0] >> 1
	ui = y[1] >> 1
	vr = y[8]
	vi = y[9]
	x[0] = ur + (vr >> 1)
	x[1] = ui + (vi >> 1)
	x[8] = ur - (vr >> 1)
	x[9] = ui - (vi >> 1)

	ur = y[4] >> 1
	ui = y[5] >> 1
	vi = y[12]
	vr = y[13]
	x[4] = ur + (vr >> 1)
	x[5] = ui - (vi >> 1)
	x[12] = ur - (vr >> 1)
	x[13] = ui + (vi >> 1)

	ur = y[10]
	ui = y[11]
	vi, vr = cplxMultDiv2SGL(ui, ur, w, w)

	ur = y[2]
	ui = y[3]
	x[2] = (ur >> 1) + vr
	x[3] = (ui >> 1) + vi
	x[10] = (ur >> 1) - vr
	x[11] = (ui >> 1) - vi

	ur = y[14]
	ui = y[15]
	vr, vi = cplxMultDiv2SGL(ui, ur, w, w)

	ur = y[6]
	ui = y[7]
	x[6] = (ur >> 1) + vr
	x[7] = (ui >> 1) - vi
	x[14] = (ur >> 1) - vr
	x[15] = (ui >> 1) + vi
}
