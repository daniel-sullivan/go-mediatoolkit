// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Hard-coded radix FFT kernels fft_16 / fft_32, a 1:1 port of the vendored
// libFDK fft.cpp:737-994 (fft_16) and fft.cpp:1004-1523 (fft_32). Unlike the
// power-of-two AAC-LC lengths 64/128/256/512 — which the dispatcher routes
// through the generic radix-2 dit_fft (fft.go) — the C fft() dispatches length
// 16 to fft_16 and length 32 to fft_32, fully unrolled split-radix kernels with
// embedded twiddle constants (fft.cpp:1804-1812). The SBR QMF needs these:
// dct_IV/dst_IV/dct_II/dct_III at the 64-channel transform length L==64 call
// fft(M==32) (dct.cpp), so fft_32 is the load-bearing leaf of the QMF modulation;
// fft_16 is fft_32's mirror and is ported for completeness/verification.
//
// Everything is integer FIXP_DBL (Q1.31) data with FIXP_SGL (Q1.15) twiddles —
// fft.cpp has zero floats — so these kernels are bit-identical regardless of
// build/vectorization and carry no aac_strict FP gate (cf. the integer-kernel
// note in nativeaac.go). The complex-multiply twiddle ROMs fft16_w16 / fft32_w32
// hold STCP(cos,sin) packed FIXP_SGL pairs (SINETABLE_16BIT active), so the
// cplxMultDiv2(..., FIXP_STP) calls resolve to the 32x16X2 SGL overload
// (cplxMultDiv2SGL); SUMDIFF_PIFOURTH multiplies by W_PiFOURTH == STC(0x5a82799a)
// == wPiFourth (FIXP_SGL) via fMultDiv2(LONG, SHORT) == fMultDiv2DS.
//
// SHIFT_A / SHIFT_B: the active config (FFT_TWO_STAGE_RADIX_4 path, fft.cpp:723-
// 724 — the branch where SHIFT_A is empty and SHIFT_B is ">> 1") puts the radix-4
// stage's ">> 1" on the (C+D) combine, i.e. SHIFT_A is a no-op and SHIFT_B is a
// one-bit right shift. The two source variants (fft.cpp:720-721 vs :723-724) are
// algebraically equivalent only up to rounding; the genuine build links the
// :723-724 form (verified by the parity oracle), reproduced here verbatim.

// fft16W16 is the fft_16 twiddle ROM STCP pairs (fft.cpp:733-734), narrowed to
// FIXP_SGL packed pairs under SINETABLE_16BIT (STCP -> {stcNarrow, stcNarrow}).
var fft16W16 = [2]fixSTP{
	{stcNarrow(0x7641af3d), stcNarrow(0x30fbc54d)},
	{stcNarrow(0x30fbc54d), stcNarrow(0x7641af3d)},
}

// fft32W32 is the fft_32 twiddle ROM STCP pairs (fft.cpp:997-1000), narrowed to
// FIXP_SGL packed pairs under SINETABLE_16BIT.
var fft32W32 = [6]fixSTP{
	{stcNarrow(0x7641af3d), stcNarrow(0x30fbc54d)},
	{stcNarrow(0x30fbc54d), stcNarrow(0x7641af3d)},
	{stcNarrow(0x7d8a5f40), stcNarrow(0x18f8b83c)},
	{stcNarrow(0x6a6d98a4), stcNarrow(0x471cece7)},
	{stcNarrow(0x471cece7), stcNarrow(0x6a6d98a4)},
	{stcNarrow(0x18f8b83c), stcNarrow(0x7d8a5f40)},
}

// sumdiffPiFourth is the SUMDIFF_PIFOURTH(diff, sum, a, b) macro (fft.cpp:109-
// 116): wa=fMultDiv2(a,W_PiFOURTH); wb=fMultDiv2(b,W_PiFOURTH); diff=wb-wa;
// sum=wb+wa. W_PiFOURTH is FIXP_SGL (STC under SINETABLE_16BIT), so fMultDiv2 is
// the (LONG, SHORT) overload == fMultDiv2DS.
func sumdiffPiFourth(a, b int32) (diff, sum int32) {
	wa := fMultDiv2DS(a, wPiFourth)
	wb := fMultDiv2DS(b, wPiFourth)
	return wb - wa, wb + wa
}

// fft16 performs the in-place 16-point complex FFT over the interleaved buffer x
// (32 int32, re at even / im at odd), a 1:1 port of fft_16 (fft.cpp:737-994).
// Output is scaled by SCALEFACTOR16 == 3 (added to the caller's block exponent in
// fft()). SHIFT_A == no-op, SHIFT_B == ">> 1" (the active fft.cpp:723-724 form).
func fft16(x []int32) {
	var vr, ur, vr2, ur2, vr3, ur3, vr4, ur4 int32
	var vi, ui, vi2, ui2, vi3, ui3 int32

	vr = (x[0] >> 1) + (x[16] >> 1)
	ur = (x[1] >> 1) + (x[17] >> 1)
	vi = x[8] + x[24]
	ui = x[9] + x[25]
	x[0] = vr + (vi >> 1)
	x[1] = ur + (ui >> 1)

	vr2 = (x[4] >> 1) + (x[20] >> 1)
	ur2 = (x[5] >> 1) + (x[21] >> 1)

	x[4] = vr - (vi >> 1)
	x[5] = ur - (ui >> 1)
	vr -= x[16]
	vi = (vi >> 1) - x[24]
	ur -= x[17]
	ui = (ui >> 1) - x[25]

	vr3 = (x[2] >> 1) + (x[18] >> 1)
	ur3 = (x[3] >> 1) + (x[19] >> 1)

	x[2] = ui + vr
	x[3] = ur - vi

	vr4 = (x[6] >> 1) + (x[22] >> 1)
	ur4 = (x[7] >> 1) + (x[23] >> 1)

	x[6] = vr - ui
	x[7] = vi + ur

	vi2 = x[12] + x[28]
	ui2 = x[13] + x[29]
	x[8] = vr2 + (vi2 >> 1)
	x[9] = ur2 + (ui2 >> 1)
	x[12] = vr2 - (vi2 >> 1)
	x[13] = ur2 - (ui2 >> 1)
	vr2 -= x[20]
	ur2 -= x[21]
	vi2 = (vi2 >> 1) - x[28]
	ui2 = (ui2 >> 1) - x[29]

	vi = x[10] + x[26]
	ui = x[11] + x[27]

	x[10] = ui2 + vr2
	x[11] = ur2 - vi2

	vi3 = x[14] + x[30]
	ui3 = x[15] + x[31]

	x[14] = vr2 - ui2
	x[15] = vi2 + ur2

	x[16] = vr3 + (vi >> 1)
	x[17] = ur3 + (ui >> 1)
	x[20] = vr3 - (vi >> 1)
	x[21] = ur3 - (ui >> 1)
	vr3 -= x[18]
	ur3 -= x[19]
	vi = (vi >> 1) - x[26]
	ui = (ui >> 1) - x[27]
	x[18] = ui + vr3
	x[19] = ur3 - vi

	x[24] = vr4 + (vi3 >> 1)
	x[28] = vr4 - (vi3 >> 1)
	x[25] = ur4 + (ui3 >> 1)
	x[29] = ur4 - (ui3 >> 1)
	vr4 -= x[22]
	ur4 -= x[23]

	x[22] = vr3 - ui
	x[23] = vi + ur3

	vi3 = (vi3 >> 1) - x[30]
	ui3 = (ui3 >> 1) - x[31]
	x[26] = ui3 + vr4
	x[30] = vr4 - ui3
	x[27] = ur4 - vi3
	x[31] = vi3 + ur4

	// xt1=0 xt2=8
	vr = x[8]
	vi = x[9]
	ur = x[0] >> 1
	ui = x[1] >> 1
	x[0] = ur + (vr >> 1)
	x[1] = ui + (vi >> 1)
	x[8] = ur - (vr >> 1)
	x[9] = ui - (vi >> 1)

	// xt1=4 xt2=12
	vr = x[13]
	vi = x[12]
	ur = x[4] >> 1
	ui = x[5] >> 1
	x[4] = ur + (vr >> 1)
	x[5] = ui - (vi >> 1)
	x[12] = ur - (vr >> 1)
	x[13] = ui + (vi >> 1)

	// xt1=16 xt2=24
	vr = x[24]
	vi = x[25]
	ur = x[16] >> 1
	ui = x[17] >> 1
	x[16] = ur + (vr >> 1)
	x[17] = ui + (vi >> 1)
	x[24] = ur - (vr >> 1)
	x[25] = ui - (vi >> 1)

	// xt1=20 xt2=28
	vr = x[29]
	vi = x[28]
	ur = x[20] >> 1
	ui = x[21] >> 1
	x[20] = ur + (vr >> 1)
	x[21] = ui - (vi >> 1)
	x[28] = ur - (vr >> 1)
	x[29] = ui + (vi >> 1)

	// xt1=2 xt2=10
	vi, vr = sumdiffPiFourth(x[10], x[11])
	ur = x[2]
	ui = x[3]
	x[2] = (ur >> 1) + vr
	x[3] = (ui >> 1) + vi
	x[10] = (ur >> 1) - vr
	x[11] = (ui >> 1) - vi

	// xt1=6 xt2=14
	vr, vi = sumdiffPiFourth(x[14], x[15])
	ur = x[6]
	ui = x[7]
	x[6] = (ur >> 1) + vr
	x[7] = (ui >> 1) - vi
	x[14] = (ur >> 1) - vr
	x[15] = (ui >> 1) + vi

	// xt1=18 xt2=26
	vi, vr = sumdiffPiFourth(x[26], x[27])
	ur = x[18]
	ui = x[19]
	x[18] = (ur >> 1) + vr
	x[19] = (ui >> 1) + vi
	x[26] = (ur >> 1) - vr
	x[27] = (ui >> 1) - vi

	// xt1=22 xt2=30
	vr, vi = sumdiffPiFourth(x[30], x[31])
	ur = x[22]
	ui = x[23]
	x[22] = (ur >> 1) + vr
	x[23] = (ui >> 1) - vi
	x[30] = (ur >> 1) - vr
	x[31] = (ui >> 1) + vi

	// xt1=0 xt2=16
	vr = x[16]
	vi = x[17]
	ur = x[0] >> 1
	ui = x[1] >> 1
	x[0] = ur + (vr >> 1)
	x[1] = ui + (vi >> 1)
	x[16] = ur - (vr >> 1)
	x[17] = ui - (vi >> 1)

	// xt1=8 xt2=24
	vi = x[24]
	vr = x[25]
	ur = x[8] >> 1
	ui = x[9] >> 1
	x[8] = ur + (vr >> 1)
	x[9] = ui - (vi >> 1)
	x[24] = ur - (vr >> 1)
	x[25] = ui + (vi >> 1)

	// xt1=2 xt2=18
	vi, vr = cplxMultDiv2SGL(x[19], x[18], fft16W16[0].re, fft16W16[0].im)
	ur = x[2]
	ui = x[3]
	x[2] = (ur >> 1) + vr
	x[3] = (ui >> 1) + vi
	x[18] = (ur >> 1) - vr
	x[19] = (ui >> 1) - vi

	// xt1=10 xt2=26
	vr, vi = cplxMultDiv2SGL(x[27], x[26], fft16W16[0].re, fft16W16[0].im)
	ur = x[10]
	ui = x[11]
	x[10] = (ur >> 1) + vr
	x[11] = (ui >> 1) - vi
	x[26] = (ur >> 1) - vr
	x[27] = (ui >> 1) + vi

	// xt1=4 xt2=20
	vi, vr = sumdiffPiFourth(x[20], x[21])
	ur = x[4]
	ui = x[5]
	x[4] = (ur >> 1) + vr
	x[5] = (ui >> 1) + vi
	x[20] = (ur >> 1) - vr
	x[21] = (ui >> 1) - vi

	// xt1=12 xt2=28
	vr, vi = sumdiffPiFourth(x[28], x[29])
	ur = x[12]
	ui = x[13]
	x[12] = (ur >> 1) + vr
	x[13] = (ui >> 1) - vi
	x[28] = (ur >> 1) - vr
	x[29] = (ui >> 1) + vi

	// xt1=6 xt2=22
	vi, vr = cplxMultDiv2SGL(x[23], x[22], fft16W16[1].re, fft16W16[1].im)
	ur = x[6]
	ui = x[7]
	x[6] = (ur >> 1) + vr
	x[7] = (ui >> 1) + vi
	x[22] = (ur >> 1) - vr
	x[23] = (ui >> 1) - vi

	// xt1=14 xt2=30
	vr, vi = cplxMultDiv2SGL(x[31], x[30], fft16W16[1].re, fft16W16[1].im)
	ur = x[14]
	ui = x[15]
	x[14] = (ur >> 1) + vr
	x[15] = (ui >> 1) - vi
	x[30] = (ur >> 1) - vr
	x[31] = (ui >> 1) + vi
}

// fft32 performs the in-place 32-point complex FFT over the interleaved buffer x
// (64 int32, re at even / im at odd), a 1:1 port of fft_32 (fft.cpp:1004-1523).
// Output is scaled by SCALEFACTOR32 == 4 (added to the caller's block exponent in
// fft()). This is the FFT the SBR 64-band QMF modulation (dct_IV/dst_IV at L==64)
// relies on.
func fft32(x []int32) {
	// 1+2 stage radix 4 (fft.cpp:1010-1207).
	{
		var vi, ui, vi2, ui2, vi3, ui3 int32
		var vr, ur, vr2, ur2, vr3, ur3, vr4, ur4 int32

		// i = 0
		vr = (x[0] + x[32]) >> 1
		ur = (x[1] + x[33]) >> 1
		vi = x[16] + x[48]
		ui = x[17] + x[49]

		x[0] = vr + (vi >> 1)
		x[1] = ur + (ui >> 1)

		vr2 = (x[4] + x[36]) >> 1
		ur2 = (x[5] + x[37]) >> 1

		x[4] = vr - (vi >> 1)
		x[5] = ur - (ui >> 1)

		vr -= x[32]
		ur -= x[33]
		vi = (vi >> 1) - x[48]
		ui = (ui >> 1) - x[49]

		vr3 = (x[2] + x[34]) >> 1
		ur3 = (x[3] + x[35]) >> 1

		x[2] = ui + vr
		x[3] = ur - vi

		vr4 = (x[6] + x[38]) >> 1
		ur4 = (x[7] + x[39]) >> 1

		x[6] = vr - ui
		x[7] = vi + ur

		// i=16
		vi = x[20] + x[52]
		ui = x[21] + x[53]

		x[16] = vr2 + (vi >> 1)
		x[17] = ur2 + (ui >> 1)
		x[20] = vr2 - (vi >> 1)
		x[21] = ur2 - (ui >> 1)

		vr2 -= x[36]
		ur2 -= x[37]
		vi = (vi >> 1) - x[52]
		ui = (ui >> 1) - x[53]

		vi2 = x[18] + x[50]
		ui2 = x[19] + x[51]

		x[18] = ui + vr2
		x[19] = ur2 - vi

		vi3 = x[22] + x[54]
		ui3 = x[23] + x[55]

		x[22] = vr2 - ui
		x[23] = vi + ur2

		// i = 32
		x[32] = vr3 + (vi2 >> 1)
		x[33] = ur3 + (ui2 >> 1)
		x[36] = vr3 - (vi2 >> 1)
		x[37] = ur3 - (ui2 >> 1)

		vr3 -= x[34]
		ur3 -= x[35]
		vi2 = (vi2 >> 1) - x[50]
		ui2 = (ui2 >> 1) - x[51]

		x[34] = ui2 + vr3
		x[35] = ur3 - vi2

		// i=48
		x[48] = vr4 + (vi3 >> 1)
		x[52] = vr4 - (vi3 >> 1)
		x[49] = ur4 + (ui3 >> 1)
		x[53] = ur4 - (ui3 >> 1)

		vr4 -= x[38]
		ur4 -= x[39]

		x[38] = vr3 - ui2
		x[39] = vi2 + ur3

		vi3 = (vi3 >> 1) - x[54]
		ui3 = (ui3 >> 1) - x[55]

		x[50] = ui3 + vr4
		x[54] = vr4 - ui3
		x[51] = ur4 - vi3
		x[55] = vi3 + ur4

		// i=8
		vr = (x[8] + x[40]) >> 1
		ur = (x[9] + x[41]) >> 1
		vi = x[24] + x[56]
		ui = x[25] + x[57]

		x[8] = vr + (vi >> 1)
		x[9] = ur + (ui >> 1)

		vr2 = (x[12] + x[44]) >> 1
		ur2 = (x[13] + x[45]) >> 1

		x[12] = vr - (vi >> 1)
		x[13] = ur - (ui >> 1)

		vr -= x[40]
		ur -= x[41]
		vi = (vi >> 1) - x[56]
		ui = (ui >> 1) - x[57]

		vr3 = (x[10] + x[42]) >> 1
		ur3 = (x[11] + x[43]) >> 1

		x[10] = ui + vr
		x[11] = ur - vi

		vr4 = (x[14] + x[46]) >> 1
		ur4 = (x[15] + x[47]) >> 1

		x[14] = vr - ui
		x[15] = vi + ur

		// i=24
		vi = x[28] + x[60]
		ui = x[29] + x[61]

		x[24] = vr2 + (vi >> 1)
		x[28] = vr2 - (vi >> 1)
		x[25] = ur2 + (ui >> 1)
		x[29] = ur2 - (ui >> 1)

		vr2 -= x[44]
		ur2 -= x[45]
		vi = (vi >> 1) - x[60]
		ui = (ui >> 1) - x[61]

		vi2 = x[26] + x[58]
		ui2 = x[27] + x[59]

		x[26] = ui + vr2
		x[27] = ur2 - vi

		vi3 = x[30] + x[62]
		ui3 = x[31] + x[63]

		x[30] = vr2 - ui
		x[31] = vi + ur2

		// i=40
		x[40] = vr3 + (vi2 >> 1)
		x[44] = vr3 - (vi2 >> 1)
		x[41] = ur3 + (ui2 >> 1)
		x[45] = ur3 - (ui2 >> 1)

		vr3 -= x[42]
		ur3 -= x[43]
		vi2 = (vi2 >> 1) - x[58]
		ui2 = (ui2 >> 1) - x[59]

		x[42] = ui2 + vr3
		x[43] = ur3 - vi2

		// i=56
		x[56] = vr4 + (vi3 >> 1)
		x[60] = vr4 - (vi3 >> 1)
		x[57] = ur4 + (ui3 >> 1)
		x[61] = ur4 - (ui3 >> 1)

		vr4 -= x[46]
		ur4 -= x[47]

		x[46] = vr3 - ui2
		x[47] = vi2 + ur3

		vi3 = (vi3 >> 1) - x[62]
		ui3 = (ui3 >> 1) - x[63]

		x[58] = ui3 + vr4
		x[62] = vr4 - ui3
		x[59] = ur4 - vi3
		x[63] = vi3 + ur4
	}

	// Second stage (fft.cpp:1209-1252): four 4-point butterflies over xt+=16.
	{
		for blk := 0; blk < 4; blk++ {
			xt := blk * 16
			var vi, ui, vr, ur int32

			vr = x[xt+8]
			vi = x[xt+9]
			ur = x[xt+0] >> 1
			ui = x[xt+1] >> 1
			x[xt+0] = ur + (vr >> 1)
			x[xt+1] = ui + (vi >> 1)
			x[xt+8] = ur - (vr >> 1)
			x[xt+9] = ui - (vi >> 1)

			vr = x[xt+13]
			vi = x[xt+12]
			ur = x[xt+4] >> 1
			ui = x[xt+5] >> 1
			x[xt+4] = ur + (vr >> 1)
			x[xt+5] = ui - (vi >> 1)
			x[xt+12] = ur - (vr >> 1)
			x[xt+13] = ui + (vi >> 1)

			vi, vr = sumdiffPiFourth(x[xt+10], x[xt+11])
			ur = x[xt+2]
			ui = x[xt+3]
			x[xt+2] = (ur >> 1) + vr
			x[xt+3] = (ui >> 1) + vi
			x[xt+10] = (ur >> 1) - vr
			x[xt+11] = (ui >> 1) - vi

			vr, vi = sumdiffPiFourth(x[xt+14], x[xt+15])
			ur = x[xt+6]
			ui = x[xt+7]
			x[xt+6] = (ur >> 1) + vr
			x[xt+7] = (ui >> 1) - vi
			x[xt+14] = (ur >> 1) - vr
			x[xt+15] = (ui >> 1) + vi
		}
	}

	// Third stage (fft.cpp:1254-1521).
	{
		var vi, ui, vr, ur int32

		vr = x[16]
		vi = x[17]
		ur = x[0] >> 1
		ui = x[1] >> 1
		x[0] = ur + (vr >> 1)
		x[1] = ui + (vi >> 1)
		x[16] = ur - (vr >> 1)
		x[17] = ui - (vi >> 1)

		vi = x[24]
		vr = x[25]
		ur = x[8] >> 1
		ui = x[9] >> 1
		x[8] = ur + (vr >> 1)
		x[9] = ui - (vi >> 1)
		x[24] = ur - (vr >> 1)
		x[25] = ui + (vi >> 1)

		vr = x[48]
		vi = x[49]
		ur = x[32] >> 1
		ui = x[33] >> 1
		x[32] = ur + (vr >> 1)
		x[33] = ui + (vi >> 1)
		x[48] = ur - (vr >> 1)
		x[49] = ui - (vi >> 1)

		vi = x[56]
		vr = x[57]
		ur = x[40] >> 1
		ui = x[41] >> 1
		x[40] = ur + (vr >> 1)
		x[41] = ui - (vi >> 1)
		x[56] = ur - (vr >> 1)
		x[57] = ui + (vi >> 1)

		vi, vr = cplxMultDiv2SGL(x[19], x[18], fft32W32[0].re, fft32W32[0].im)
		ur = x[2]
		ui = x[3]
		x[2] = (ur >> 1) + vr
		x[3] = (ui >> 1) + vi
		x[18] = (ur >> 1) - vr
		x[19] = (ui >> 1) - vi

		vr, vi = cplxMultDiv2SGL(x[27], x[26], fft32W32[0].re, fft32W32[0].im)
		ur = x[10]
		ui = x[11]
		x[10] = (ur >> 1) + vr
		x[11] = (ui >> 1) - vi
		x[26] = (ur >> 1) - vr
		x[27] = (ui >> 1) + vi

		vi, vr = cplxMultDiv2SGL(x[51], x[50], fft32W32[0].re, fft32W32[0].im)
		ur = x[34]
		ui = x[35]
		x[34] = (ur >> 1) + vr
		x[35] = (ui >> 1) + vi
		x[50] = (ur >> 1) - vr
		x[51] = (ui >> 1) - vi

		vr, vi = cplxMultDiv2SGL(x[59], x[58], fft32W32[0].re, fft32W32[0].im)
		ur = x[42]
		ui = x[43]
		x[42] = (ur >> 1) + vr
		x[43] = (ui >> 1) - vi
		x[58] = (ur >> 1) - vr
		x[59] = (ui >> 1) + vi

		vi, vr = sumdiffPiFourth(x[20], x[21])
		ur = x[4]
		ui = x[5]
		x[4] = (ur >> 1) + vr
		x[5] = (ui >> 1) + vi
		x[20] = (ur >> 1) - vr
		x[21] = (ui >> 1) - vi

		vr, vi = sumdiffPiFourth(x[28], x[29])
		ur = x[12]
		ui = x[13]
		x[12] = (ur >> 1) + vr
		x[13] = (ui >> 1) - vi
		x[28] = (ur >> 1) - vr
		x[29] = (ui >> 1) + vi

		vi, vr = sumdiffPiFourth(x[52], x[53])
		ur = x[36]
		ui = x[37]
		x[36] = (ur >> 1) + vr
		x[37] = (ui >> 1) + vi
		x[52] = (ur >> 1) - vr
		x[53] = (ui >> 1) - vi

		vr, vi = sumdiffPiFourth(x[60], x[61])
		ur = x[44]
		ui = x[45]
		x[44] = (ur >> 1) + vr
		x[45] = (ui >> 1) - vi
		x[60] = (ur >> 1) - vr
		x[61] = (ui >> 1) + vi

		vi, vr = cplxMultDiv2SGL(x[23], x[22], fft32W32[1].re, fft32W32[1].im)
		ur = x[6]
		ui = x[7]
		x[6] = (ur >> 1) + vr
		x[7] = (ui >> 1) + vi
		x[22] = (ur >> 1) - vr
		x[23] = (ui >> 1) - vi

		vr, vi = cplxMultDiv2SGL(x[31], x[30], fft32W32[1].re, fft32W32[1].im)
		ur = x[14]
		ui = x[15]
		x[14] = (ur >> 1) + vr
		x[15] = (ui >> 1) - vi
		x[30] = (ur >> 1) - vr
		x[31] = (ui >> 1) + vi

		vi, vr = cplxMultDiv2SGL(x[55], x[54], fft32W32[1].re, fft32W32[1].im)
		ur = x[38]
		ui = x[39]
		x[38] = (ur >> 1) + vr
		x[39] = (ui >> 1) + vi
		x[54] = (ur >> 1) - vr
		x[55] = (ui >> 1) - vi

		vr, vi = cplxMultDiv2SGL(x[63], x[62], fft32W32[1].re, fft32W32[1].im)
		ur = x[46]
		ui = x[47]
		x[46] = (ur >> 1) + vr
		x[47] = (ui >> 1) - vi
		x[62] = (ur >> 1) - vr
		x[63] = (ui >> 1) + vi

		vr = x[32]
		vi = x[33]
		ur = x[0] >> 1
		ui = x[1] >> 1
		x[0] = ur + (vr >> 1)
		x[1] = ui + (vi >> 1)
		x[32] = ur - (vr >> 1)
		x[33] = ui - (vi >> 1)

		vi = x[48]
		vr = x[49]
		ur = x[16] >> 1
		ui = x[17] >> 1
		x[16] = ur + (vr >> 1)
		x[17] = ui - (vi >> 1)
		x[48] = ur - (vr >> 1)
		x[49] = ui + (vi >> 1)

		vi, vr = cplxMultDiv2SGL(x[35], x[34], fft32W32[2].re, fft32W32[2].im)
		ur = x[2]
		ui = x[3]
		x[2] = (ur >> 1) + vr
		x[3] = (ui >> 1) + vi
		x[34] = (ur >> 1) - vr
		x[35] = (ui >> 1) - vi

		vr, vi = cplxMultDiv2SGL(x[51], x[50], fft32W32[2].re, fft32W32[2].im)
		ur = x[18]
		ui = x[19]
		x[18] = (ur >> 1) + vr
		x[19] = (ui >> 1) - vi
		x[50] = (ur >> 1) - vr
		x[51] = (ui >> 1) + vi

		vi, vr = cplxMultDiv2SGL(x[37], x[36], fft32W32[0].re, fft32W32[0].im)
		ur = x[4]
		ui = x[5]
		x[4] = (ur >> 1) + vr
		x[5] = (ui >> 1) + vi
		x[36] = (ur >> 1) - vr
		x[37] = (ui >> 1) - vi

		vr, vi = cplxMultDiv2SGL(x[53], x[52], fft32W32[0].re, fft32W32[0].im)
		ur = x[20]
		ui = x[21]
		x[20] = (ur >> 1) + vr
		x[21] = (ui >> 1) - vi
		x[52] = (ur >> 1) - vr
		x[53] = (ui >> 1) + vi

		vi, vr = cplxMultDiv2SGL(x[39], x[38], fft32W32[3].re, fft32W32[3].im)
		ur = x[6]
		ui = x[7]
		x[6] = (ur >> 1) + vr
		x[7] = (ui >> 1) + vi
		x[38] = (ur >> 1) - vr
		x[39] = (ui >> 1) - vi

		vr, vi = cplxMultDiv2SGL(x[55], x[54], fft32W32[3].re, fft32W32[3].im)
		ur = x[22]
		ui = x[23]
		x[22] = (ur >> 1) + vr
		x[23] = (ui >> 1) - vi
		x[54] = (ur >> 1) - vr
		x[55] = (ui >> 1) + vi

		vi, vr = sumdiffPiFourth(x[40], x[41])
		ur = x[8]
		ui = x[9]
		x[8] = (ur >> 1) + vr
		x[9] = (ui >> 1) + vi
		x[40] = (ur >> 1) - vr
		x[41] = (ui >> 1) - vi

		vr, vi = sumdiffPiFourth(x[56], x[57])
		ur = x[24]
		ui = x[25]
		x[24] = (ur >> 1) + vr
		x[25] = (ui >> 1) - vi
		x[56] = (ur >> 1) - vr
		x[57] = (ui >> 1) + vi

		vi, vr = cplxMultDiv2SGL(x[43], x[42], fft32W32[4].re, fft32W32[4].im)
		ur = x[10]
		ui = x[11]
		x[10] = (ur >> 1) + vr
		x[11] = (ui >> 1) + vi
		x[42] = (ur >> 1) - vr
		x[43] = (ui >> 1) - vi

		vr, vi = cplxMultDiv2SGL(x[59], x[58], fft32W32[4].re, fft32W32[4].im)
		ur = x[26]
		ui = x[27]
		x[26] = (ur >> 1) + vr
		x[27] = (ui >> 1) - vi
		x[58] = (ur >> 1) - vr
		x[59] = (ui >> 1) + vi

		vi, vr = cplxMultDiv2SGL(x[45], x[44], fft32W32[1].re, fft32W32[1].im)
		ur = x[12]
		ui = x[13]
		x[12] = (ur >> 1) + vr
		x[13] = (ui >> 1) + vi
		x[44] = (ur >> 1) - vr
		x[45] = (ui >> 1) - vi

		vr, vi = cplxMultDiv2SGL(x[61], x[60], fft32W32[1].re, fft32W32[1].im)
		ur = x[28]
		ui = x[29]
		x[28] = (ur >> 1) + vr
		x[29] = (ui >> 1) - vi
		x[60] = (ur >> 1) - vr
		x[61] = (ui >> 1) + vi

		vi, vr = cplxMultDiv2SGL(x[47], x[46], fft32W32[5].re, fft32W32[5].im)
		ur = x[14]
		ui = x[15]
		x[14] = (ur >> 1) + vr
		x[15] = (ui >> 1) + vi
		x[46] = (ur >> 1) - vr
		x[47] = (ui >> 1) - vi

		vr, vi = cplxMultDiv2SGL(x[63], x[62], fft32W32[5].re, fft32W32[5].im)
		ur = x[30]
		ui = x[31]
		x[30] = (ur >> 1) + vr
		x[31] = (ui >> 1) - vi
		x[62] = (ur >> 1) - vr
		x[63] = (ui >> 1) + vi
	}
}
