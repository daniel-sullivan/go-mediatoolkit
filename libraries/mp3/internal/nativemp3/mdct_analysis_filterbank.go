// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

import "unsafe"

// Polyphase analysis filterbank + MDCT for the LAME encoder — a 1:1
// translation of newmdct.c's window_subband / mdct_short / mdct_long /
// mdct_sub48. All math is float32, routed through the mdct* FP helpers so the
// mp3_strict build separately rounds every product like the C oracle.

// The gr_info members mdct_sub48 reads/writes (Xr / BlockType /
// MixedBlockFlag, l3side.h:46) and the sv_enc members it touches (sb_sample /
// amp_filter, util.h:249-250) are now part of the unified context (context.go):
// the per-granule struct is the shared GrInfo, and the encoder-state members
// live on the shared EncStateVar. mdct_sub48 fills GrInfo.Xr, which the
// quantizer / huffman stages then read.

// windowSubband is a 1:1 translation of newmdct.c:430-814 (inline static void
// window_subband). It applies the overlapping analysis window to 32 fresh PCM
// samples concatenated onto the history at x1 and runs Takehiro's fast IDCT,
// writing 32 subband samples into a. The C reads x1[-256..224] and a sibling
// pointer x2 == x1[-62]; both are modeled here as offsets into the x1 slice
// (x1[base+off]). The returned values are sum_j a[j]*cos(PI*j*(k+1/2)/32).
//
// x1 is the PCM slice and base is the index of the C `x1[0]`.
func windowSubband(x1 []float32, base int, a []float32) {
	wp := 10 // index into enwindow, C `FLOAT const *wp = enwindow + 10`

	// const sample_t *x2 = &x1[238 - 14 - 286];  => x2[0] == x1[base-62]
	x2 := base + (238 - 14 - 286)

	x1b := base // moving base for x1 (C `x1--` each iteration)
	for i := -15; i < 0; i++ {
		var w, s, t float32

		w = enwindow[wp-10]
		s = mdctMul(x1[x2-224], w)
		t = mdctMul(x1[x1b+224], w)
		w = enwindow[wp-9]
		s = mdctAdd(s, mdctMul(x1[x2-160], w))
		t = mdctAdd(t, mdctMul(x1[x1b+160], w))
		w = enwindow[wp-8]
		s = mdctAdd(s, mdctMul(x1[x2-96], w))
		t = mdctAdd(t, mdctMul(x1[x1b+96], w))
		w = enwindow[wp-7]
		s = mdctAdd(s, mdctMul(x1[x2-32], w))
		t = mdctAdd(t, mdctMul(x1[x1b+32], w))
		w = enwindow[wp-6]
		s = mdctAdd(s, mdctMul(x1[x2+32], w))
		t = mdctAdd(t, mdctMul(x1[x1b-32], w))
		w = enwindow[wp-5]
		s = mdctAdd(s, mdctMul(x1[x2+96], w))
		t = mdctAdd(t, mdctMul(x1[x1b-96], w))
		w = enwindow[wp-4]
		s = mdctAdd(s, mdctMul(x1[x2+160], w))
		t = mdctAdd(t, mdctMul(x1[x1b-160], w))
		w = enwindow[wp-3]
		s = mdctAdd(s, mdctMul(x1[x2+224], w))
		t = mdctAdd(t, mdctMul(x1[x1b-224], w))

		w = enwindow[wp-2]
		s = mdctAdd(s, mdctMul(x1[x1b-256], w))
		t = mdctSub(t, mdctMul(x1[x2+256], w))
		w = enwindow[wp-1]
		s = mdctAdd(s, mdctMul(x1[x1b-192], w))
		t = mdctSub(t, mdctMul(x1[x2+192], w))
		w = enwindow[wp+0]
		s = mdctAdd(s, mdctMul(x1[x1b-128], w))
		t = mdctSub(t, mdctMul(x1[x2+128], w))
		w = enwindow[wp+1]
		s = mdctAdd(s, mdctMul(x1[x1b-64], w))
		t = mdctSub(t, mdctMul(x1[x2+64], w))
		w = enwindow[wp+2]
		s = mdctAdd(s, mdctMul(x1[x1b+0], w))
		t = mdctSub(t, mdctMul(x1[x2+0], w))
		w = enwindow[wp+3]
		s = mdctAdd(s, mdctMul(x1[x1b+64], w))
		t = mdctSub(t, mdctMul(x1[x2-64], w))
		w = enwindow[wp+4]
		s = mdctAdd(s, mdctMul(x1[x1b+128], w))
		t = mdctSub(t, mdctMul(x1[x2-128], w))
		w = enwindow[wp+5]
		s = mdctAdd(s, mdctMul(x1[x1b+192], w))
		t = mdctSub(t, mdctMul(x1[x2-192], w))

		/*
		 * this multiplyer could be removed, but it needs more 256 FLOAT data.
		 * thinking about the data cache performance, I think we should not
		 * use such a huge table. tt 2000/Oct/25
		 */
		s = mdctMul(s, enwindow[wp+6])
		w = mdctSub(t, s)
		a[30+i*2] = mdctAdd(t, s)
		a[31+i*2] = mdctMul(enwindow[wp+7], w)
		wp += 18
		x1b--
		x2++
	}
	{
		var s, t, u, v float32
		t = mdctMul(x1[x1b-16], enwindow[wp-10])
		s = mdctMul(x1[x1b-32], enwindow[wp-2])
		t = mdctAdd(t, mdctMul(mdctSub(x1[x1b-48], x1[x1b+16]), enwindow[wp-9]))
		s = mdctAdd(s, mdctMul(x1[x1b-96], enwindow[wp-1]))
		t = mdctAdd(t, mdctMul(mdctAdd(x1[x1b-80], x1[x1b+48]), enwindow[wp-8]))
		s = mdctAdd(s, mdctMul(x1[x1b-160], enwindow[wp+0]))
		t = mdctAdd(t, mdctMul(mdctSub(x1[x1b-112], x1[x1b+80]), enwindow[wp-7]))
		s = mdctAdd(s, mdctMul(x1[x1b-224], enwindow[wp+1]))
		t = mdctAdd(t, mdctMul(mdctAdd(x1[x1b-144], x1[x1b+112]), enwindow[wp-6]))
		s = mdctSub(s, mdctMul(x1[x1b+32], enwindow[wp+2]))
		t = mdctAdd(t, mdctMul(mdctSub(x1[x1b-176], x1[x1b+144]), enwindow[wp-5]))
		s = mdctSub(s, mdctMul(x1[x1b+96], enwindow[wp+3]))
		t = mdctAdd(t, mdctMul(mdctAdd(x1[x1b-208], x1[x1b+176]), enwindow[wp-4]))
		s = mdctSub(s, mdctMul(x1[x1b+160], enwindow[wp+4]))
		t = mdctAdd(t, mdctMul(mdctSub(x1[x1b-240], x1[x1b+208]), enwindow[wp-3]))
		s = mdctSub(s, x1[x1b+224])

		u = mdctSub(s, t)
		v = mdctAdd(s, t)

		t = a[14]
		s = mdctSub(a[15], t)

		a[31] = mdctAdd(v, t) /* A0 */
		a[30] = mdctAdd(u, s) /* A1 */
		a[15] = mdctSub(u, s) /* A2 */
		a[14] = mdctSub(v, t) /* A3 */
	}
	{
		var xr float32
		xr = mdctSub(a[28], a[0])
		a[0] = mdctAdd(a[0], a[28])
		a[28] = mdctMul(xr, enwindow[wp-2*18+7])
		xr = mdctSub(a[29], a[1])
		a[1] = mdctAdd(a[1], a[29])
		a[29] = mdctMul(xr, enwindow[wp-2*18+7])

		xr = mdctSub(a[26], a[2])
		a[2] = mdctAdd(a[2], a[26])
		a[26] = mdctMul(xr, enwindow[wp-4*18+7])
		xr = mdctSub(a[27], a[3])
		a[3] = mdctAdd(a[3], a[27])
		a[27] = mdctMul(xr, enwindow[wp-4*18+7])

		xr = mdctSub(a[24], a[4])
		a[4] = mdctAdd(a[4], a[24])
		a[24] = mdctMul(xr, enwindow[wp-6*18+7])
		xr = mdctSub(a[25], a[5])
		a[5] = mdctAdd(a[5], a[25])
		a[25] = mdctMul(xr, enwindow[wp-6*18+7])

		xr = mdctSub(a[22], a[6])
		a[6] = mdctAdd(a[6], a[22])
		a[22] = mdctMulD(sqrt2Const, xr) // C: xr * SQRT2 (double), newmdct.c:559
		xr = mdctSub(a[23], a[7])
		a[7] = mdctAdd(a[7], a[23])
		a[23] = mdctMulDSub(sqrt2Const, xr, a[7]) // C: xr * SQRT2 - a[7], newmdct.c:562
		a[7] = mdctSub(a[7], a[6])
		a[22] = mdctSub(a[22], a[7])
		a[23] = mdctSub(a[23], a[22])

		xr = a[6]
		a[6] = mdctSub(a[31], xr)
		a[31] = mdctAdd(a[31], xr)
		xr = a[7]
		a[7] = mdctSub(a[30], xr)
		a[30] = mdctAdd(a[30], xr)
		xr = a[22]
		a[22] = mdctSub(a[15], xr)
		a[15] = mdctAdd(a[15], xr)
		xr = a[23]
		a[23] = mdctSub(a[14], xr)
		a[14] = mdctAdd(a[14], xr)

		xr = mdctSub(a[20], a[8])
		a[8] = mdctAdd(a[8], a[20])
		a[20] = mdctMul(xr, enwindow[wp-10*18+7])
		xr = mdctSub(a[21], a[9])
		a[9] = mdctAdd(a[9], a[21])
		a[21] = mdctMul(xr, enwindow[wp-10*18+7])

		xr = mdctSub(a[18], a[10])
		a[10] = mdctAdd(a[10], a[18])
		a[18] = mdctMul(xr, enwindow[wp-12*18+7])
		xr = mdctSub(a[19], a[11])
		a[11] = mdctAdd(a[11], a[19])
		a[19] = mdctMul(xr, enwindow[wp-12*18+7])

		xr = mdctSub(a[16], a[12])
		a[12] = mdctAdd(a[12], a[16])
		a[16] = mdctMul(xr, enwindow[wp-14*18+7])
		xr = mdctSub(a[17], a[13])
		a[13] = mdctAdd(a[13], a[17])
		a[17] = mdctMul(xr, enwindow[wp-14*18+7])

		xr = mdctAdd(mdctSub(0, a[20]), a[24])
		a[20] = mdctAdd(a[20], a[24])
		a[24] = mdctMul(xr, enwindow[wp-12*18+7])
		xr = mdctAdd(mdctSub(0, a[21]), a[25])
		a[21] = mdctAdd(a[21], a[25])
		a[25] = mdctMul(xr, enwindow[wp-12*18+7])

		xr = mdctSub(a[4], a[8])
		a[4] = mdctAdd(a[4], a[8])
		a[8] = mdctMul(xr, enwindow[wp-12*18+7])
		xr = mdctSub(a[5], a[9])
		a[5] = mdctAdd(a[5], a[9])
		a[9] = mdctMul(xr, enwindow[wp-12*18+7])

		xr = mdctSub(a[0], a[12])
		a[0] = mdctAdd(a[0], a[12])
		a[12] = mdctMul(xr, enwindow[wp-4*18+7])
		xr = mdctSub(a[1], a[13])
		a[1] = mdctAdd(a[1], a[13])
		a[13] = mdctMul(xr, enwindow[wp-4*18+7])
		xr = mdctSub(a[16], a[28])
		a[16] = mdctAdd(a[16], a[28])
		a[28] = mdctMul(xr, enwindow[wp-4*18+7])
		xr = mdctAdd(mdctSub(0, a[17]), a[29])
		a[17] = mdctAdd(a[17], a[29])
		a[29] = mdctMul(xr, enwindow[wp-4*18+7])

		// C: xr = SQRT2 * (a[2]-a[10]) — the parenthesized float sub is fully
		// rounded, then the double SQRT2 multiply rounds once (newmdct.c:628-639).
		xr = mdctMulD(sqrt2Const, mdctSub(a[2], a[10]))
		a[2] = mdctAdd(a[2], a[10])
		a[10] = xr
		xr = mdctMulD(sqrt2Const, mdctSub(a[3], a[11]))
		a[3] = mdctAdd(a[3], a[11])
		a[11] = xr
		xr = mdctMulD(sqrt2Const, mdctAdd(mdctSub(0, a[18]), a[26]))
		a[18] = mdctAdd(a[18], a[26])
		a[26] = mdctSub(xr, a[18])
		xr = mdctMulD(sqrt2Const, mdctAdd(mdctSub(0, a[19]), a[27]))
		a[19] = mdctAdd(a[19], a[27])
		a[27] = mdctSub(xr, a[19])

		xr = a[2]
		a[19] = mdctSub(a[19], a[3])
		a[3] = mdctSub(a[3], xr)
		a[2] = mdctSub(a[31], xr)
		a[31] = mdctAdd(a[31], xr)
		xr = a[3]
		a[11] = mdctSub(a[11], a[19])
		a[18] = mdctSub(a[18], xr)
		a[3] = mdctSub(a[30], xr)
		a[30] = mdctAdd(a[30], xr)
		xr = a[18]
		a[27] = mdctSub(a[27], a[11])
		a[19] = mdctSub(a[19], xr)
		a[18] = mdctSub(a[15], xr)
		a[15] = mdctAdd(a[15], xr)

		xr = a[19]
		a[10] = mdctSub(a[10], xr)
		a[19] = mdctSub(a[14], xr)
		a[14] = mdctAdd(a[14], xr)
		xr = a[10]
		a[11] = mdctSub(a[11], xr)
		a[10] = mdctSub(a[23], xr)
		a[23] = mdctAdd(a[23], xr)
		xr = a[11]
		a[26] = mdctSub(a[26], xr)
		a[11] = mdctSub(a[22], xr)
		a[22] = mdctAdd(a[22], xr)
		xr = a[26]
		a[27] = mdctSub(a[27], xr)
		a[26] = mdctSub(a[7], xr)
		a[7] = mdctAdd(a[7], xr)

		xr = a[27]
		a[27] = mdctSub(a[6], xr)
		a[6] = mdctAdd(a[6], xr)

		// C: xr = SQRT2 * (a-b) — double SQRT2 × fully-rounded float sub,
		// single trailing round (newmdct.c:678-689).
		xr = mdctMulD(sqrt2Const, mdctSub(a[0], a[4]))
		a[0] = mdctAdd(a[0], a[4])
		a[4] = xr
		xr = mdctMulD(sqrt2Const, mdctSub(a[1], a[5]))
		a[1] = mdctAdd(a[1], a[5])
		a[5] = xr
		xr = mdctMulD(sqrt2Const, mdctSub(a[16], a[20]))
		a[16] = mdctAdd(a[16], a[20])
		a[20] = xr
		xr = mdctMulD(sqrt2Const, mdctSub(a[17], a[21]))
		a[17] = mdctAdd(a[17], a[21])
		a[21] = xr

		// C: xr = -SQRT2 * (a-b) — the negate is on the double SQRT2, so the
		// whole product is double, rounding once (newmdct.c:691-702).
		xr = mdctMulD(-sqrt2Const, mdctSub(a[8], a[12]))
		a[8] = mdctAdd(a[8], a[12])
		a[12] = mdctSub(xr, a[8])
		xr = mdctMulD(-sqrt2Const, mdctSub(a[9], a[13]))
		a[9] = mdctAdd(a[9], a[13])
		a[13] = mdctSub(xr, a[9])
		xr = mdctMulD(-sqrt2Const, mdctSub(a[25], a[29]))
		a[25] = mdctAdd(a[25], a[29])
		a[29] = mdctSub(xr, a[25])
		xr = mdctMulD(-sqrt2Const, mdctAdd(a[24], a[28]))
		a[24] = mdctSub(a[24], a[28])
		a[28] = mdctSub(xr, a[24])

		xr = mdctSub(a[24], a[16])
		a[24] = xr
		xr = mdctSub(a[20], xr)
		a[20] = xr
		xr = mdctSub(a[28], xr)
		a[28] = xr

		xr = mdctSub(a[25], a[17])
		a[25] = xr
		xr = mdctSub(a[21], xr)
		a[21] = xr
		xr = mdctSub(a[29], xr)
		a[29] = xr

		xr = mdctSub(a[17], a[1])
		a[17] = xr
		xr = mdctSub(a[9], xr)
		a[9] = xr
		xr = mdctSub(a[25], xr)
		a[25] = xr
		xr = mdctSub(a[5], xr)
		a[5] = xr
		xr = mdctSub(a[21], xr)
		a[21] = xr
		xr = mdctSub(a[13], xr)
		a[13] = xr
		xr = mdctSub(a[29], xr)
		a[29] = xr

		xr = mdctSub(a[1], a[0])
		a[1] = xr
		xr = mdctSub(a[16], xr)
		a[16] = xr
		xr = mdctSub(a[17], xr)
		a[17] = xr
		xr = mdctSub(a[8], xr)
		a[8] = xr
		xr = mdctSub(a[9], xr)
		a[9] = xr
		xr = mdctSub(a[24], xr)
		a[24] = xr
		xr = mdctSub(a[25], xr)
		a[25] = xr
		xr = mdctSub(a[4], xr)
		a[4] = xr
		xr = mdctSub(a[5], xr)
		a[5] = xr
		xr = mdctSub(a[20], xr)
		a[20] = xr
		xr = mdctSub(a[21], xr)
		a[21] = xr
		xr = mdctSub(a[12], xr)
		a[12] = xr
		xr = mdctSub(a[13], xr)
		a[13] = xr
		xr = mdctSub(a[28], xr)
		a[28] = xr
		xr = mdctSub(a[29], xr)
		a[29] = xr

		xr = a[0]
		a[0] = mdctAdd(a[0], a[31])
		a[31] = mdctSub(a[31], xr)
		xr = a[1]
		a[1] = mdctAdd(a[1], a[30])
		a[30] = mdctSub(a[30], xr)
		xr = a[16]
		a[16] = mdctAdd(a[16], a[15])
		a[15] = mdctSub(a[15], xr)
		xr = a[17]
		a[17] = mdctAdd(a[17], a[14])
		a[14] = mdctSub(a[14], xr)
		xr = a[8]
		a[8] = mdctAdd(a[8], a[23])
		a[23] = mdctSub(a[23], xr)
		xr = a[9]
		a[9] = mdctAdd(a[9], a[22])
		a[22] = mdctSub(a[22], xr)
		xr = a[24]
		a[24] = mdctAdd(a[24], a[7])
		a[7] = mdctSub(a[7], xr)
		xr = a[25]
		a[25] = mdctAdd(a[25], a[6])
		a[6] = mdctSub(a[6], xr)
		xr = a[4]
		a[4] = mdctAdd(a[4], a[27])
		a[27] = mdctSub(a[27], xr)
		xr = a[5]
		a[5] = mdctAdd(a[5], a[26])
		a[26] = mdctSub(a[26], xr)
		xr = a[20]
		a[20] = mdctAdd(a[20], a[11])
		a[11] = mdctSub(a[11], xr)
		xr = a[21]
		a[21] = mdctAdd(a[21], a[10])
		a[10] = mdctSub(a[10], xr)
		xr = a[12]
		a[12] = mdctAdd(a[12], a[19])
		a[19] = mdctSub(a[19], xr)
		xr = a[13]
		a[13] = mdctAdd(a[13], a[18])
		a[18] = mdctSub(a[18], xr)
		xr = a[28]
		a[28] = mdctAdd(a[28], a[3])
		a[3] = mdctSub(a[3], xr)
		xr = a[29]
		a[29] = mdctAdd(a[29], a[2])
		a[2] = mdctSub(a[2], xr)
	}
}

// mdctShort is a 1:1 translation of newmdct.c:832-867 (inline static void
// mdct_short). It runs the three short-block 6-line MDCTs in place over the
// 18-line inout buffer, storing the results side by side.
func mdctShort(inout []float32) {
	off := 0 // C `inout++` per l, modeled as a base offset
	for l := 0; l < 3; l++ {
		var tc0, tc1, tc2, ts0, ts1, ts2 float32

		ts0 = mdctSub(mdctMul(inout[off+2*3], win[ShortType][0]), inout[off+5*3])
		tc0 = mdctSub(mdctMul(inout[off+0*3], win[ShortType][2]), inout[off+3*3])
		tc1 = mdctAdd(ts0, tc0)
		tc2 = mdctSub(ts0, tc0)

		ts0 = mdctAdd(mdctMul(inout[off+5*3], win[ShortType][0]), inout[off+2*3])
		tc0 = mdctAdd(mdctMul(inout[off+3*3], win[ShortType][2]), inout[off+0*3])
		ts1 = mdctAdd(ts0, tc0)
		ts2 = mdctAdd(mdctSub(0, ts0), tc0)

		// C: tc0 = (inout[1*3]*win[..][1] - inout[4*3]) * 2.069978111953089e-11.
		// The float subexpression is fully rounded, then the double scale rounds
		// once on store (mdctMulFD) — newmdct.c:849-850.
		tc0 = mdctMulFD(mdctSub(mdctMul(inout[off+1*3], win[ShortType][1]), inout[off+4*3]), 2.069978111953089e-11) /* tritab_s[1] */
		ts0 = mdctMulFD(mdctAdd(mdctMul(inout[off+4*3], win[ShortType][1]), inout[off+1*3]), 2.069978111953089e-11) /* tritab_s[1] */

		// C: inout[0] = tc1 * 1.907525191737280e-11 + tc0 — the double product
		// and add stay in double, rounding once on store (newmdct.c:852-853).
		inout[off+3*0] = mdctMulFDAdd(tc1, 1.907525191737280e-11 /* tritab_s[2] */, tc0)
		inout[off+3*5] = mdctMulFDAdd(mdctSub(0, ts1), 1.907525191737280e-11 /* tritab_s[0] */, ts0)

		// C: tc2 = tc2 * 0.866... * 1.907...e-11 — chained double scales, one
		// trailing round (newmdct.c:855); ts1 = ts1 * 0.5 * 1.907...e-11 + ts0
		// (newmdct.c:856).
		tc2 = mdctMulFDD(tc2, 0.86602540378443870761, 1.907525191737281e-11 /* tritab_s[2] */)
		ts1 = mdctMulFDDAdd(ts1, 0.5, 1.907525191737281e-11, ts0)
		inout[off+3*1] = mdctSub(tc2, ts1)
		inout[off+3*2] = mdctAdd(tc2, ts1)

		// C: tc1 = tc1 * 0.5 * 1.907...e-11 - tc0 (newmdct.c:860); ts2 = ts2 *
		// 0.866... * 1.907...e-11 (newmdct.c:861).
		tc1 = mdctMulFDDSub(tc1, 0.5, 1.907525191737281e-11, tc0)
		ts2 = mdctMulFDD(ts2, 0.86602540378443870761, 1.907525191737281e-11 /* tritab_s[0] */)
		inout[off+3*3] = mdctAdd(tc1, ts2)
		inout[off+3*4] = mdctSub(tc1, ts2)

		off++
	}
}

// mdctLong is a 1:1 translation of newmdct.c:869-941 (inline static void
// mdct_long). It runs the long-block 18-line MDCT, reading 18 windowed inputs
// from in and writing 18 MDCT lines to out. cx(i) aliases win[ShortType]+12.
func mdctLong(out, in []float32) {
	var ct, st float32
	{
		var tc1, tc2, tc3, tc4, ts5, ts6, ts7, ts8 float32
		/* 1,2, 5,6, 9,10, 13,14, 17 */
		tc1 = mdctSub(in[17], in[9])
		tc3 = mdctSub(in[15], in[11])
		tc4 = mdctSub(in[14], in[12])
		ts5 = mdctAdd(in[0], in[8])
		ts6 = mdctAdd(in[1], in[7])
		ts7 = mdctAdd(in[2], in[6])
		ts8 = mdctAdd(in[3], in[5])

		out[17] = mdctSub(mdctSub(mdctAdd(ts5, ts7), ts8), mdctSub(ts6, in[4]))
		st = mdctAdd(mdctMul(mdctSub(mdctAdd(ts5, ts7), ts8), cx(7)), mdctSub(ts6, in[4]))
		ct = mdctMul(mdctSub(mdctSub(tc1, tc3), tc4), cx(6))
		out[5] = mdctAdd(ct, st)
		out[6] = mdctSub(ct, st)

		tc2 = mdctMul(mdctSub(in[16], in[10]), cx(6))
		ts6 = mdctAdd(mdctMul(ts6, cx(7)), in[4])
		ct = mdctAdd(mdctAdd(mdctAdd(mdctMul(tc1, cx(0)), tc2), mdctMul(tc3, cx(1))), mdctMul(tc4, cx(2)))
		st = mdctAdd(mdctSub(mdctAdd(mdctMul(mdctSub(0, ts5), cx(4)), ts6), mdctMul(ts7, cx(5))), mdctMul(ts8, cx(3)))
		out[1] = mdctAdd(ct, st)
		out[2] = mdctSub(ct, st)

		ct = mdctAdd(mdctSub(mdctSub(mdctMul(tc1, cx(1)), tc2), mdctMul(tc3, cx(2))), mdctMul(tc4, cx(0)))
		st = mdctAdd(mdctSub(mdctAdd(mdctMul(mdctSub(0, ts5), cx(5)), ts6), mdctMul(ts7, cx(3))), mdctMul(ts8, cx(4)))
		out[9] = mdctAdd(ct, st)
		out[10] = mdctSub(ct, st)

		ct = mdctSub(mdctAdd(mdctSub(mdctMul(tc1, cx(2)), tc2), mdctMul(tc3, cx(0))), mdctMul(tc4, cx(1)))
		st = mdctSub(mdctAdd(mdctSub(mdctMul(ts5, cx(3)), ts6), mdctMul(ts7, cx(4))), mdctMul(ts8, cx(5)))
		out[13] = mdctAdd(ct, st)
		out[14] = mdctSub(ct, st)
	}
	{
		var ts1, ts2, ts3, ts4, tc5, tc6, tc7, tc8 float32

		ts1 = mdctSub(in[8], in[0])
		ts3 = mdctSub(in[6], in[2])
		ts4 = mdctSub(in[5], in[3])
		tc5 = mdctAdd(in[17], in[9])
		tc6 = mdctAdd(in[16], in[10])
		tc7 = mdctAdd(in[15], in[11])
		tc8 = mdctAdd(in[14], in[12])

		out[0] = mdctAdd(mdctAdd(mdctAdd(tc5, tc7), tc8), mdctAdd(tc6, in[13]))
		ct = mdctSub(mdctMul(mdctAdd(mdctAdd(tc5, tc7), tc8), cx(7)), mdctAdd(tc6, in[13]))
		st = mdctMul(mdctAdd(mdctSub(ts1, ts3), ts4), cx(6))
		out[11] = mdctAdd(ct, st)
		out[12] = mdctSub(ct, st)

		ts2 = mdctMul(mdctSub(in[7], in[1]), cx(6))
		tc6 = mdctSub(in[13], mdctMul(tc6, cx(7)))
		ct = mdctAdd(mdctAdd(mdctSub(mdctMul(tc5, cx(3)), tc6), mdctMul(tc7, cx(4))), mdctMul(tc8, cx(5)))
		st = mdctAdd(mdctAdd(mdctAdd(mdctMul(ts1, cx(2)), ts2), mdctMul(ts3, cx(0))), mdctMul(ts4, cx(1)))
		out[3] = mdctAdd(ct, st)
		out[4] = mdctSub(ct, st)

		ct = mdctSub(mdctSub(mdctAdd(mdctMul(mdctSub(0, tc5), cx(5)), tc6), mdctMul(tc7, cx(3))), mdctMul(tc8, cx(4)))
		st = mdctSub(mdctSub(mdctAdd(mdctMul(ts1, cx(1)), ts2), mdctMul(ts3, cx(2))), mdctMul(ts4, cx(0)))
		out[7] = mdctAdd(ct, st)
		out[8] = mdctSub(ct, st)

		ct = mdctSub(mdctSub(mdctAdd(mdctMul(mdctSub(0, tc5), cx(4)), tc6), mdctMul(tc7, cx(5))), mdctMul(tc8, cx(3)))
		st = mdctSub(mdctAdd(mdctSub(mdctMul(ts1, cx(0)), ts2), mdctMul(ts3, cx(1))), mdctMul(ts4, cx(2)))
		out[15] = mdctAdd(ct, st)
		out[16] = mdctSub(ct, st)
	}
}

// mdctSub48 is a 1:1 translation of newmdct.c:944-1039 (void mdct_sub48). It
// drives the whole analysis front end: for each output channel and granule it
// runs the polyphase filterbank over 18 frames of PCM (windowSubband), applies
// the per-band amplitude filter, runs the long- or short-block MDCT per band,
// and applies the long-block aliasing-reduction butterfly. w0 / w1 are the
// two channels' PCM runs; the C `wk = w0 + 286` look-ahead offset is preserved.
//
// cfg / esv / l3SideTT stand in for the gfc->cfg / gfc->sv_enc /
// gfc->l3_side.tt[gr][ch] state the C reaches through lame_internal_flags. cfg
// reuses the package-shared SessionConfig (psymodel.go); only its ChannelsOut
// and ModeGr fields are read here.
func mdctSub48(cfg *SessionConfig, esv *EncStateVar, l3SideTT *[2][2]GrInfo, w0, w1 []float32) {
	wkBuf := w0
	wk := 286 // index into wkBuf, C `wk = w0 + 286`

	/* thinking cache performance, ch->gr loop is better than gr->ch loop */
	for ch := 0; ch < cfg.ChannelsOut; ch++ {
		for gr := 0; gr < cfg.ModeGr; gr++ {
			var band int
			gi := &l3SideTT[gr][ch]
			mdctEnc := 0 // index into gi.Xr, C `FLOAT *mdct_enc = gi->xr`

			// FLOAT *samp = esv->sb_sample[ch][1 - gr][0];
			// samp is a moving cursor through the flat [18][SBLIMIT] view.
			samp := 0
			sampBuf := &esv.SbSample[ch][1-gr]

			for k := 0; k < 18/2; k++ {
				// window_subband(wk, samp); window_subband(wk + 32, samp + 32);
				windowSubband(wkBuf, wk, sbSlice(sampBuf, samp))
				windowSubband(wkBuf, wk+32, sbSlice(sampBuf, samp+32))
				samp += 64
				wk += 64
				/*
				 * Compensate for inversion in the analysis filter
				 */
				for band = 1; band < 32; band += 2 {
					sbSet(sampBuf, samp+band-32, mdctMul(sbGet(sampBuf, samp+band-32), -1))
				}
			}

			/*
			 * Perform imdct of 18 previous subband samples
			 * + 18 current subband samples
			 */
			for band = 0; band < 32; band, mdctEnc = band+1, mdctEnc+18 {
				typ := gi.BlockType
				// FLOAT const *band0 = esv->sb_sample[ch][gr][0] + order[band];
				band0Buf := &esv.SbSample[ch][gr]
				band0 := order[band]
				// FLOAT *band1 = esv->sb_sample[ch][1 - gr][0] + order[band];
				band1Buf := &esv.SbSample[ch][1-gr]
				band1 := order[band]
				if gi.MixedBlockFlag != 0 && band < 2 {
					typ = 0
				}
				if esv.AmpFilter[band] < 1e-12 {
					for i := 0; i < 18; i++ {
						gi.Xr[mdctEnc+i] = 0
					}
				} else {
					if esv.AmpFilter[band] < 1.0 {
						for k := 0; k < 18; k++ {
							sbSet(band1Buf, band1+k*32, mdctMul(sbGet(band1Buf, band1+k*32), esv.AmpFilter[band]))
						}
					}
					if typ == ShortType {
						for k := -NS / 4; k < 0; k++ {
							w := win[ShortType][k+3]
							gi.Xr[mdctEnc+k*3+9] = mdctSub(mdctMul(sbGet(band0Buf, band0+(9+k)*32), w), sbGet(band0Buf, band0+(8-k)*32))
							gi.Xr[mdctEnc+k*3+18] = mdctAdd(mdctMul(sbGet(band0Buf, band0+(14-k)*32), w), sbGet(band0Buf, band0+(15+k)*32))
							gi.Xr[mdctEnc+k*3+10] = mdctSub(mdctMul(sbGet(band0Buf, band0+(15+k)*32), w), sbGet(band0Buf, band0+(14-k)*32))
							gi.Xr[mdctEnc+k*3+19] = mdctAdd(mdctMul(sbGet(band1Buf, band1+(2-k)*32), w), sbGet(band1Buf, band1+(3+k)*32))
							gi.Xr[mdctEnc+k*3+11] = mdctSub(mdctMul(sbGet(band1Buf, band1+(3+k)*32), w), sbGet(band1Buf, band1+(2-k)*32))
							gi.Xr[mdctEnc+k*3+20] = mdctAdd(mdctMul(sbGet(band1Buf, band1+(8-k)*32), w), sbGet(band1Buf, band1+(9+k)*32))
						}
						mdctShort(gi.Xr[mdctEnc:])
					} else {
						var work [18]float32
						for k := -NL / 4; k < 0; k++ {
							var a, b float32
							a = mdctAdd(mdctMul(win[typ][k+27], sbGet(band1Buf, band1+(k+9)*32)),
								mdctMul(win[typ][k+36], sbGet(band1Buf, band1+(8-k)*32)))
							b = mdctSub(mdctMul(win[typ][k+9], sbGet(band0Buf, band0+(k+9)*32)),
								mdctMul(win[typ][k+18], sbGet(band0Buf, band0+(8-k)*32)))
							work[k+9] = mdctSub(a, mdctMul(b, tantabL(k+9)))
							work[k+18] = mdctAdd(mdctMul(a, tantabL(k+9)), b)
						}

						mdctLong(gi.Xr[mdctEnc:], work[:])
					}
				}
				/*
				 * Perform aliasing reduction butterfly
				 */
				if typ != ShortType && band != 0 {
					for k := 7; k >= 0; k-- {
						var bu, bd float32
						bu = mdctAdd(mdctMul(gi.Xr[mdctEnc+k], ca(k)), mdctMul(gi.Xr[mdctEnc-1-k], cs(k)))
						bd = mdctSub(mdctMul(gi.Xr[mdctEnc+k], cs(k)), mdctMul(gi.Xr[mdctEnc-1-k], ca(k)))

						gi.Xr[mdctEnc-1-k] = bu
						gi.Xr[mdctEnc+k] = bd
					}
				}
			}
		}
		wkBuf = w1
		wk = 286 // wk = w1 + 286
		if cfg.ModeGr == 1 {
			// memcpy(esv->sb_sample[ch][0], esv->sb_sample[ch][1], 576*sizeof(FLOAT));
			copySubband(&esv.SbSample[ch][0], &esv.SbSample[ch][1])
		}
	}
}

// sbSlice returns the C `FLOAT *` view `&buf[0][0] + off` as a slice into the
// flat [18][SBLIMIT] subband-sample buffer, so window_subband (which indexes
// a[0..31]) can write through it.
func sbSlice(buf *[18][SBLIMIT]float32, off int) []float32 {
	flat := unsafe.Slice(&buf[0][0], 18*SBLIMIT)
	return flat[off:]
}

// sbGet reads the flat [18][SBLIMIT] subband buffer at linear index off
// (C `sb_sample[...][off/32][off%32]`).
func sbGet(buf *[18][SBLIMIT]float32, off int) float32 {
	return buf[off/SBLIMIT][off%SBLIMIT]
}

// sbSet writes the flat [18][SBLIMIT] subband buffer at linear index off.
func sbSet(buf *[18][SBLIMIT]float32, off int, v float32) {
	buf[off/SBLIMIT][off%SBLIMIT] = v
}

// copySubband is the mode_gr==1 carry: memcpy of 576 FLOATs from gr-1's
// subband history into gr-0's (newmdct.c:1036).
func copySubband(dst, src *[18][SBLIMIT]float32) {
	*dst = *src
}
