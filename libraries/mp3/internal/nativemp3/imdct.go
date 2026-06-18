package nativemp3

// IMDCT (inverse MDCT, a type-II DCT-IV) and the polyphase synthesis
// filterbank: minimp3's "imdct-synthesis-filterbank" slice.
//
// This file translates, 1:1 from the vendored minimp3.h, the back end of
// Layer III reconstruction that turns a granule's antialiased / reordered
// frequency-domain subband samples into time-domain PCM:
//
//   - the per-subband inverse MDCT (L3_dct3_9, L3_imdct36, L3_idct3,
//     L3_imdct12, L3_imdct_short, L3_change_sign, L3_imdct_gr) which folds
//     each subband's 18 (long) or 3x6 (short) coefficients against the
//     overlap-add window into 18 time samples; and
//   - the polyphase synthesis filterbank (mp3d_DCT_II, mp3d_scale_pcm,
//     mp3d_synth_pair, mp3d_synth, mp3d_synth_granule) which runs a 32->32
//     DCT-II per row, windows it against the carried qmf_state history, and
//     emits the 32-sample-per-band PCM block.
//
// Data layout (matching minimp3): each channel's granule is a flat
// float32[576] = 32 subbands x 18 samples, subband-major (subband b,
// sample i at index b*18 + i). L3_imdct_gr's overlap is the per-channel
// mdct_overlap[9*32] history. mp3d_synth_granule's lins scratch is the
// syn[18+15][2*32] block; qmfState is the cross-frame [15*64] filterbank
// history. PCM output is interleaved int16 (minimp3's default mp3d_sample_t,
// no MINIMP3_FLOAT_OUTPUT), dstl/dstr being the left/right write cursors.
//
// # FP discipline
//
// This is a kind=fp slice. Every float multiply and add/sub/div is routed
// through the package's //go:noinline f32mul / f32add / f32sub / f32div
// helpers (declared in huffman_fp_strict.go for the mp3_strict build and
// huffman_fp_default.go otherwise) so the strict build cannot fuse a*b+c
// into a single-rounded FMA. All trigonometric constants below are the
// literal float32 values minimp3 hard-codes, so no transcendental shim is
// needed here. Only the scalar reference branches of mp3d_DCT_II / mp3d_synth
// are translated; minimp3's SIMD fast paths are guarded by HAVE_SIMD and
// MINIMP3_NO_SIMD keeps the oracle on the same scalar path.
//
// stopBlockType is STOP_BLOCK_TYPE (minimp3.h:58): the block_type value (3)
// whose IMDCT uses the stop-block window. shortBlockType (= 2) is declared
// alongside the side-info slice.
const stopBlockType = 3

// l3DCT39 is a 1:1 translation of L3_dct3_9 (minimp3.h:1047): an in-place
// 9-point DCT-III on y, the radix step inside the long-block IMDCT.
func l3DCT39(y []float32) {
	var s0, s1, s2, s3, s4, s5, s6, s7, s8, t0, t2, t4 float32

	s0 = y[0]
	s2 = y[2]
	s4 = y[4]
	s6 = y[6]
	s8 = y[8]
	t0 = f32add(s0, f32mul(s6, 0.5))
	s0 = f32sub(s0, s6)
	t4 = f32mul(f32add(s4, s2), 0.93969262)
	t2 = f32mul(f32add(s8, s2), 0.76604444)
	s6 = f32mul(f32sub(s4, s8), 0.17364818)
	s4 = f32add(s4, f32sub(s8, s2))

	s2 = f32sub(s0, f32mul(s4, 0.5))
	y[4] = f32add(s4, s0)
	s8 = f32add(f32sub(t0, t2), s6)
	s0 = f32add(f32sub(t0, t4), t2)
	s4 = f32sub(f32add(t0, t4), s6)

	s1 = y[1]
	s3 = y[3]
	s5 = y[5]
	s7 = y[7]

	s3 = f32mul(s3, 0.86602540)
	t0 = f32mul(f32add(s5, s1), 0.98480775)
	t4 = f32mul(f32sub(s5, s7), 0.34202014)
	t2 = f32mul(f32add(s1, s7), 0.64278761)
	s1 = f32mul(f32sub(f32sub(s1, s5), s7), 0.86602540)

	s5 = f32sub(f32sub(t0, s3), t2)
	s7 = f32sub(f32sub(t4, s3), t0)
	s3 = f32sub(f32add(t4, s3), t2)

	y[0] = f32sub(s4, s7)
	y[1] = f32add(s2, s1)
	y[2] = f32sub(s0, s3)
	y[3] = f32add(s8, s5)
	y[5] = f32sub(s8, s5)
	y[6] = f32add(s0, s3)
	y[7] = f32sub(s2, s1)
	y[8] = f32add(s4, s7)
}

// gTwid9 is L3_imdct36's static const g_twid9[18] (minimp3.h:1090).
var gTwid9 = [18]float32{
	0.73727734, 0.79335334, 0.84339145, 0.88701083, 0.92387953, 0.95371695, 0.97629601, 0.99144486, 0.99904822,
	0.67559021, 0.60876143, 0.53729961, 0.46174861, 0.38268343, 0.30070580, 0.21643961, 0.13052619, 0.04361938,
}

// l3IMDCT36 is a 1:1 translation of L3_imdct36 (minimp3.h:1087): the
// long-block inverse MDCT. For each of nbands subbands it builds the
// cosine/sine half-spectra (co/si), runs l3DCT39 on each, then windows
// against the overlap history and the supplied window, writing 18 time
// samples per subband and updating overlap. grbuf advances 18 floats and
// overlap 9 floats per band, so they are re-sliced each iteration.
func l3IMDCT36(grbuf, overlap []float32, window []float32, nbands int) {
	for j := 0; j < nbands; j, grbuf, overlap = j+1, grbuf[18:], overlap[9:] {
		var co, si [9]float32
		co[0] = -grbuf[0]
		si[0] = grbuf[17]
		for i := 0; i < 4; i++ {
			si[8-2*i] = f32sub(grbuf[4*i+1], grbuf[4*i+2])
			co[1+2*i] = f32add(grbuf[4*i+1], grbuf[4*i+2])
			si[7-2*i] = f32sub(grbuf[4*i+4], grbuf[4*i+3])
			co[2+2*i] = -f32add(grbuf[4*i+3], grbuf[4*i+4])
		}
		l3DCT39(co[:])
		l3DCT39(si[:])

		si[1] = -si[1]
		si[3] = -si[3]
		si[5] = -si[5]
		si[7] = -si[7]

		for i := 0; i < 9; i++ {
			ovl := overlap[i]
			sum := f32add(f32mul(co[i], gTwid9[9+i]), f32mul(si[i], gTwid9[0+i]))
			overlap[i] = f32sub(f32mul(co[i], gTwid9[0+i]), f32mul(si[i], gTwid9[9+i]))
			grbuf[i] = f32sub(f32mul(ovl, window[0+i]), f32mul(sum, window[9+i]))
			grbuf[17-i] = f32add(f32mul(ovl, window[9+i]), f32mul(sum, window[0+i]))
		}
	}
}

// l3IDCT3 is a 1:1 translation of L3_idct3 (minimp3.h:1144): a 3-point
// inverse DCT writing dst[0..2], the radix step inside the short-block IMDCT.
func l3IDCT3(x0, x1, x2 float32, dst []float32) {
	m1 := f32mul(x1, 0.86602540)
	a1 := f32sub(x0, f32mul(x2, 0.5))
	dst[1] = f32add(x0, x2)
	dst[0] = f32add(a1, m1)
	dst[2] = f32sub(a1, m1)
}

// gTwid3 is L3_imdct12's static const g_twid3[6] (minimp3.h:1155).
var gTwid3 = [6]float32{0.79335334, 0.92387953, 0.99144486, 0.60876143, 0.38268343, 0.13052619}

// l3IMDCT12 is a 1:1 translation of L3_imdct12 (minimp3.h:1153): the
// per-window inverse MDCT for one of a short block's three windows. It builds
// co/si via two l3IDCT3 calls, windows against overlap and g_twid3, and writes
// 6 time samples into dst while updating overlap.
func l3IMDCT12(x, dst, overlap []float32) {
	var co, si [3]float32

	l3IDCT3(-x[0], f32add(x[6], x[3]), f32add(x[12], x[9]), co[:])
	l3IDCT3(x[15], f32sub(x[12], x[9]), f32sub(x[6], x[3]), si[:])
	si[1] = -si[1]

	for i := 0; i < 3; i++ {
		ovl := overlap[i]
		sum := f32add(f32mul(co[i], gTwid3[3+i]), f32mul(si[i], gTwid3[0+i]))
		overlap[i] = f32sub(f32mul(co[i], gTwid3[0+i]), f32mul(si[i], gTwid3[3+i]))
		dst[i] = f32sub(f32mul(ovl, gTwid3[2-i]), f32mul(sum, gTwid3[5-i]))
		dst[5-i] = f32add(f32mul(ovl, gTwid3[5-i]), f32mul(sum, gTwid3[2-i]))
	}
}

// l3IMDCTShort is a 1:1 translation of L3_imdct_short (minimp3.h:1173): the
// short-block inverse MDCT. For each of nbands subbands it copies the 18
// input coefficients to a scratch tmp, overlaps the first 6 outputs from
// history, then runs l3IMDCT12 on the three interleaved windows. grbuf
// advances 18 and overlap 9 per band.
func l3IMDCTShort(grbuf, overlap []float32, nbands int) {
	for ; nbands > 0; nbands, overlap, grbuf = nbands-1, overlap[9:], grbuf[18:] {
		var tmp [18]float32
		copy(tmp[:], grbuf[:18])
		copy(grbuf[:6], overlap[:6])
		l3IMDCT12(tmp[:], grbuf[6:], overlap[6:])
		l3IMDCT12(tmp[1:], grbuf[12:], overlap[6:])
		l3IMDCT12(tmp[2:], overlap, overlap[6:])
	}
}

// l3ChangeSign is a 1:1 translation of L3_change_sign (minimp3.h:1186): it
// negates every odd-indexed sample of every other subband, the frequency
// inversion applied after the IMDCT before synthesis. The C advances grbuf by
// 18 before the loop and by 36 each iteration, leaving the pointer one stride
// past the buffer on the final iteration — legal one-past pointer arithmetic in
// C, but reslicing the Go slice that far overruns its capacity (the right
// channel's grbuf row is exactly 576 wide). The port therefore walks a base
// index `o` and only ever indexes o+1..o+17, which never reads past the active
// 32*18 region.
func l3ChangeSign(grbuf []float32) {
	o := 18
	for b := 0; b < 32; b, o = b+2, o+36 {
		for i := 1; i < 18; i += 2 {
			grbuf[o+i] = -grbuf[o+i]
		}
	}
}

// gMDCTWindow is L3_imdct_gr's static const g_mdct_window[2][18]
// (minimp3.h:1196): index 0 is the normal long window, index 1 the
// start/stop window.
var gMDCTWindow = [2][18]float32{
	{0.99904822, 0.99144486, 0.97629601, 0.95371695, 0.92387953, 0.88701083, 0.84339145, 0.79335334, 0.73727734, 0.04361938, 0.13052619, 0.21643961, 0.30070580, 0.38268343, 0.46174861, 0.53729961, 0.60876143, 0.67559021},
	{1, 1, 1, 1, 1, 1, 0.99144486, 0.92387953, 0.79335334, 0, 0, 0, 0, 0, 0, 0.13052619, 0.38268343, 0.60876143},
}

// l3IMDCTGr is a 1:1 translation of L3_imdct_gr (minimp3.h:1194): it drives
// the per-granule IMDCT. The first nLongBands subbands always use the long
// window; the remaining 32-nLongBands subbands use the short IMDCT for a
// short block, otherwise the long IMDCT with the normal window (or the stop
// window when blockType is the stop block).
func l3IMDCTGr(grbuf, overlap []float32, blockType uint8, nLongBands uint) {
	if nLongBands != 0 {
		l3IMDCT36(grbuf, overlap, gMDCTWindow[0][:], int(nLongBands))
		grbuf = grbuf[18*nLongBands:]
		overlap = overlap[9*nLongBands:]
	}
	if blockType == shortBlockType {
		l3IMDCTShort(grbuf, overlap, 32-int(nLongBands))
	} else {
		windowIdx := 0
		if blockType == stopBlockType {
			windowIdx = 1
		}
		l3IMDCT36(grbuf, overlap, gMDCTWindow[windowIdx][:], 32-int(nLongBands))
	}
}

// gSec is mp3d_DCT_II's static const g_sec[24] (minimp3.h:1276).
var gSec = [24]float32{
	10.19000816, 0.50060302, 0.50241929, 3.40760851, 0.50547093, 0.52249861, 2.05778098, 0.51544732, 0.56694406, 1.48416460, 0.53104258, 0.64682180,
	1.16943991, 0.55310392, 0.78815460, 0.97256821, 0.58293498, 1.06067765, 0.83934963, 0.62250412, 1.72244716, 0.74453628, 0.67480832, 5.10114861,
}

// mp3dDCTII is a 1:1 translation of the scalar (non-SIMD) reference branch of
// mp3d_DCT_II (minimp3.h:1274, the `for (; k < n; k++)` body at minimp3.h:1368).
// It runs an in-place 32-point DCT-II across the n subband columns of grbuf
// (column k is grbuf[k], grbuf[k+18], ... stride 18), the per-row transform
// of the polyphase synthesis. minimp3's SIMD path is skipped: the cgo oracle
// builds with MINIMP3_NO_SIMD, so this scalar branch is the bit-exact target.
func mp3dDCTII(grbuf []float32, n int) {
	for k := 0; k < n; k++ {
		var t [4][8]float32
		y := grbuf[k:]

		for i := 0; i < 8; i++ {
			x0 := y[i*18]
			x1 := y[(15-i)*18]
			x2 := y[(16+i)*18]
			x3 := y[(31-i)*18]
			t0 := f32add(x0, x3)
			t1 := f32add(x1, x2)
			t2 := f32mul(f32sub(x1, x2), gSec[3*i+0])
			t3 := f32mul(f32sub(x0, x3), gSec[3*i+1])
			t[0][i] = f32add(t0, t1)
			t[1][i] = f32mul(f32sub(t0, t1), gSec[3*i+2])
			t[2][i] = f32add(t3, t2)
			t[3][i] = f32mul(f32sub(t3, t2), gSec[3*i+2])
		}
		for i := 0; i < 4; i++ {
			x := t[i][:]
			x0, x1, x2, x3, x4, x5, x6, x7 := x[0], x[1], x[2], x[3], x[4], x[5], x[6], x[7]
			var xt float32
			xt = f32sub(x0, x7)
			x0 = f32add(x0, x7)
			x7 = f32sub(x1, x6)
			x1 = f32add(x1, x6)
			x6 = f32sub(x2, x5)
			x2 = f32add(x2, x5)
			x5 = f32sub(x3, x4)
			x3 = f32add(x3, x4)
			x4 = f32sub(x0, x3)
			x0 = f32add(x0, x3)
			x3 = f32sub(x1, x2)
			x1 = f32add(x1, x2)
			x[0] = f32add(x0, x1)
			x[4] = f32mul(f32sub(x0, x1), 0.70710677)
			x5 = f32add(x5, x6)
			x6 = f32mul(f32add(x6, x7), 0.70710677)
			x7 = f32add(x7, xt)
			x3 = f32mul(f32add(x3, x4), 0.70710677)
			x5 = f32sub(x5, f32mul(x7, 0.198912367)) // rotate by PI/8
			x7 = f32add(x7, f32mul(x5, 0.382683432))
			x5 = f32sub(x5, f32mul(x7, 0.198912367))
			x0 = f32sub(xt, x6)
			xt = f32add(xt, x6)
			x[1] = f32mul(f32add(xt, x7), 0.50979561)
			x[2] = f32mul(f32add(x4, x3), 0.54119611)
			x[3] = f32mul(f32sub(x0, x5), 0.60134488)
			x[5] = f32mul(f32add(x0, x5), 0.89997619)
			x[6] = f32mul(f32sub(x4, x3), 1.30656302)
			x[7] = f32mul(f32sub(xt, x7), 2.56291556)
		}
		for i := 0; i < 7; i++ {
			y[0*18] = t[0][i]
			y[1*18] = f32add(f32add(t[2][i], t[3][i]), t[3][i+1])
			y[2*18] = f32add(t[1][i], t[1][i+1])
			y[3*18] = f32add(f32add(t[2][i+1], t[3][i]), t[3][i+1])
			y = y[4*18:]
		}
		y[0*18] = t[0][7]
		y[1*18] = f32add(t[2][7], t[3][7])
		y[2*18] = t[1][7]
		y[3*18] = t[3][7]
	}
}

// mp3dScalePCM is a 1:1 translation of mp3d_scale_pcm (minimp3.h:1430), the
// default int16 (no MINIMP3_FLOAT_OUTPUT, non-ARMv6) variant: it saturates
// the synthesis accumulator to [-32768, 32767] and rounds away from zero to
// be bitstream-compliant. The rounding is integer-exact, but the +0.5f add
// and the comparisons are float32 operations so the saturation thresholds
// match the C; the add is routed through f32add for FMA-free parity.
func mp3dScalePCM(sample float32) int16 {
	if sample >= 32766.5 {
		return 32767
	}
	if sample <= -32767.5 {
		return -32768
	}
	s := int16(f32add(sample, 0.5))
	if s < 0 {
		s -= 1 // away from zero, to be compliant
	}
	return s
}

// mp3dSynthPair is a 1:1 translation of mp3d_synth_pair (minimp3.h:1451):
// the two fixed-coefficient dot products that produce a pair of band-0
// (DC-ish) PCM samples directly from the windowed history z, bypassing the
// general windowing loop. pcm receives the two outputs at offsets 0 and
// 16*nch.
func mp3dSynthPair(pcm []int16, nch int, z []float32) {
	var a float32
	a = f32mul(f32sub(z[14*64], z[0]), 29)
	a = f32add(a, f32mul(f32add(z[1*64], z[13*64]), 213))
	a = f32add(a, f32mul(f32sub(z[12*64], z[2*64]), 459))
	a = f32add(a, f32mul(f32add(z[3*64], z[11*64]), 2037))
	a = f32add(a, f32mul(f32sub(z[10*64], z[4*64]), 5153))
	a = f32add(a, f32mul(f32add(z[5*64], z[9*64]), 6574))
	a = f32add(a, f32mul(f32sub(z[8*64], z[6*64]), 37489))
	a = f32add(a, f32mul(z[7*64], 75038))
	pcm[0] = mp3dScalePCM(a)

	z = z[2:]
	a = f32mul(z[14*64], 104)
	a = f32add(a, f32mul(z[12*64], 1567))
	a = f32add(a, f32mul(z[10*64], 9727))
	a = f32add(a, f32mul(z[8*64], 64019))
	a = f32add(a, f32mul(z[6*64], -9975))
	a = f32add(a, f32mul(z[4*64], -45))
	a = f32add(a, f32mul(z[2*64], 146))
	a = f32add(a, f32mul(z[0*64], -5))
	pcm[16*nch] = mp3dScalePCM(a)
}

// gWin is mp3d_synth's static const g_win[] (minimp3.h:1482): the 15x16
// synthesis window coefficients, walked row by row.
var gWin = [...]float32{
	-1, 26, -31, 208, 218, 401, -519, 2063, 2000, 4788, -5517, 7134, 5959, 35640, -39336, 74992,
	-1, 24, -35, 202, 222, 347, -581, 2080, 1952, 4425, -5879, 7640, 5288, 33791, -41176, 74856,
	-1, 21, -38, 196, 225, 294, -645, 2087, 1893, 4063, -6237, 8092, 4561, 31947, -43006, 74630,
	-1, 19, -41, 190, 227, 244, -711, 2085, 1822, 3705, -6589, 8492, 3776, 30112, -44821, 74313,
	-1, 17, -45, 183, 228, 197, -779, 2075, 1739, 3351, -6935, 8840, 2935, 28289, -46617, 73908,
	-1, 16, -49, 176, 228, 153, -848, 2057, 1644, 3004, -7271, 9139, 2037, 26482, -48390, 73415,
	-2, 14, -53, 169, 227, 111, -919, 2032, 1535, 2663, -7597, 9389, 1082, 24694, -50137, 72835,
	-2, 13, -58, 161, 224, 72, -991, 2001, 1414, 2330, -7910, 9592, 70, 22929, -51853, 72169,
	-2, 11, -63, 154, 221, 36, -1064, 1962, 1280, 2006, -8209, 9750, -998, 21189, -53534, 71420,
	-2, 10, -68, 147, 215, 2, -1137, 1919, 1131, 1692, -8491, 9863, -2122, 19478, -55178, 70590,
	-3, 9, -73, 139, 208, -29, -1210, 1870, 970, 1388, -8755, 9935, -3300, 17799, -56778, 69679,
	-3, 8, -79, 132, 200, -57, -1283, 1817, 794, 1095, -8998, 9966, -4533, 16155, -58333, 68692,
	-4, 7, -85, 125, 189, -83, -1356, 1759, 605, 814, -9219, 9959, -5818, 14548, -59838, 67629,
	-4, 7, -91, 117, 177, -106, -1428, 1698, 402, 545, -9416, 9916, -7154, 12980, -61289, 66494,
	-5, 6, -97, 111, 163, -127, -1498, 1634, 185, 288, -9585, 9838, -8540, 11455, -62684, 65290,
}

// mp3dSynth is a 1:1 translation of the scalar (non-SIMD) reference branch of
// mp3d_synth (minimp3.h:1476, the `for (i = 14; i >= 0; i--)` body at
// minimp3.h:1598). It performs the windowed overlap-add of one synthesis
// stage: it seeds the linear history zlin from the two subband rows xl/xr,
// emits the band-0 pair via mp3dSynthPair, then for each of the 15 window
// offsets accumulates the a/b dot products (S0/S1/S2 with the +/-/swap sign
// pattern) and scatters eight PCM samples per offset into dstl/dstr.
//
// xl is the left granule subband data (xr = xl + 576*(nch-1)); dstl is the
// left PCM cursor (dstr = dstl + (nch-1)); lins is the per-stage slice of the
// syn scratch. The C aliases zlin = lins + 15*64 and addresses it with both
// positive and negative subscripts (e.g. zlin[4*(i-16)+...] and
// zlin[4*i - k*64]) that land back inside the lins block. Reslicing zlin in Go
// and then indexing it at a negative offset panics, so the port keeps a single
// lins view and a constant zbase = 15*64: every C `zlin[idx]` becomes
// `lins[zbase+idx]`, which is always a valid (non-negative) lins index.
func mp3dSynth(xl []float32, dstl []int16, nch int, lins []float32) {
	xr := xl[576*(nch-1):]
	dstr := dstl[(nch - 1):]

	const zbase = 15 * 64
	w := gWin[:]
	wpos := 0

	lins[zbase+4*15] = xl[18*16]
	lins[zbase+4*15+1] = xr[18*16]
	lins[zbase+4*15+2] = xl[0]
	lins[zbase+4*15+3] = xr[0]

	lins[zbase+4*31] = xl[1+18*16]
	lins[zbase+4*31+1] = xr[1+18*16]
	lins[zbase+4*31+2] = xl[1]
	lins[zbase+4*31+3] = xr[1]

	mp3dSynthPair(dstr, nch, lins[4*15+1:])
	mp3dSynthPair(dstr[32*nch:], nch, lins[4*15+64+1:])
	mp3dSynthPair(dstl, nch, lins[4*15:])
	mp3dSynthPair(dstl[32*nch:], nch, lins[4*15+64:])

	for i := 14; i >= 0; i-- {
		var a, b [4]float32

		lins[zbase+4*i] = xl[18*(31-i)]
		lins[zbase+4*i+1] = xr[18*(31-i)]
		lins[zbase+4*i+2] = xl[1+18*(31-i)]
		lins[zbase+4*i+3] = xr[1+18*(31-i)]
		lins[zbase+4*(i+16)] = xl[1+18*(1+i)]
		lins[zbase+4*(i+16)+1] = xr[1+18*(1+i)]
		lins[zbase+4*(i-16)+2] = xl[18*(1+i)]
		lins[zbase+4*(i-16)+3] = xr[18*(1+i)]

		// S0(0) S2(1) S1(2) S2(3) S1(4) S2(5) S1(6) S2(7).
		// LOAD(k): w0,w1 advance through w; vz = zlin[4*i - k*64],
		// vy = zlin[4*i - (15-k)*64], both expressed off the lins base.
		load := func(k int) (w0, w1 float32, vz, vy []float32) {
			w0 = w[wpos]
			w1 = w[wpos+1]
			wpos += 2
			vz = lins[zbase+4*i-k*64:]
			vy = lins[zbase+4*i-(15-k)*64:]
			return
		}
		// S0: b = vz*w1 + vy*w0, a = vz*w0 - vy*w1.
		s0 := func(k int) {
			w0, w1, vz, vy := load(k)
			for j := 0; j < 4; j++ {
				b[j] = f32add(f32mul(vz[j], w1), f32mul(vy[j], w0))
				a[j] = f32sub(f32mul(vz[j], w0), f32mul(vy[j], w1))
			}
		}
		// S1: b += vz*w1 + vy*w0, a += vz*w0 - vy*w1.
		s1 := func(k int) {
			w0, w1, vz, vy := load(k)
			for j := 0; j < 4; j++ {
				b[j] = f32add(b[j], f32add(f32mul(vz[j], w1), f32mul(vy[j], w0)))
				a[j] = f32add(a[j], f32sub(f32mul(vz[j], w0), f32mul(vy[j], w1)))
			}
		}
		// S2: b += vz*w1 + vy*w0, a += vy*w1 - vz*w0.
		s2 := func(k int) {
			w0, w1, vz, vy := load(k)
			for j := 0; j < 4; j++ {
				b[j] = f32add(b[j], f32add(f32mul(vz[j], w1), f32mul(vy[j], w0)))
				a[j] = f32add(a[j], f32sub(f32mul(vy[j], w1), f32mul(vz[j], w0)))
			}
		}
		s0(0)
		s2(1)
		s1(2)
		s2(3)
		s1(4)
		s2(5)
		s1(6)
		s2(7)

		dstr[(15-i)*nch] = mp3dScalePCM(a[1])
		dstr[(17+i)*nch] = mp3dScalePCM(b[1])
		dstl[(15-i)*nch] = mp3dScalePCM(a[0])
		dstl[(17+i)*nch] = mp3dScalePCM(b[0])
		dstr[(47-i)*nch] = mp3dScalePCM(a[3])
		dstr[(49+i)*nch] = mp3dScalePCM(b[3])
		dstl[(47-i)*nch] = mp3dScalePCM(a[2])
		dstl[(49+i)*nch] = mp3dScalePCM(b[2])
	}
}

// mp3dSynthGranule is a 1:1 translation of mp3d_synth_granule (minimp3.h:1629):
// the driver for one granule's synthesis filterbank. It runs the per-channel
// DCT-II, seeds the lins scratch from the carried qmfState history, runs the
// windowed synthesis stage for each subband pair, then saves the tail of lins
// back into qmfState for the next granule. nch==1 takes the standard
// (non-MINIMP3_NONSTANDARD_BUT_LOGICAL) stride-2 partial save.
//
// grbuf holds nch channels of 576 floats (channel i at grbuf[576*i]); pcm is
// the interleaved int16 output cursor; lins is the syn[18+15][64] scratch.
func mp3dSynthGranule(qmfState, grbuf []float32, nbands, nch int, pcm []int16, lins []float32) {
	for i := 0; i < nch; i++ {
		mp3dDCTII(grbuf[576*i:], nbands)
	}

	copy(lins[:15*64], qmfState[:15*64])

	for i := 0; i < nbands; i += 2 {
		mp3dSynth(grbuf[i:], pcm[32*nch*i:], nch, lins[i*64:])
	}

	if nch == 1 {
		for i := 0; i < 15*64; i += 2 {
			qmfState[i] = lins[nbands*64+i]
		}
	} else {
		copy(qmfState[:15*64], lins[nbands*64:nbands*64+15*64])
	}
}
