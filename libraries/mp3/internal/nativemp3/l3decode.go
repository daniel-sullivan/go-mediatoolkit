package nativemp3

// Layer III per-granule decode — the orchestrator that turns one granule's
// side info + main data into 576 time-domain samples per channel. This file
// is a 1:1 translation of minimp3's L3_decode (minimp3.h:1238) and the two
// helpers it owns that no other slice provided: L3_reorder (minimp3.h:995),
// which de-interleaves a short block's reordered scalefactor bands, and
// L3_antialias (minimp3.h:1012), the butterfly that cancels the aliasing the
// hybrid filterbank introduces between adjacent subbands.
//
// L3_decode wires together the already-ported stages: scalefactor decode
// (L3_decode_scalefactors), Huffman/requantization (L3_huffman), the
// joint-stereo reconstruction (L3_intensity_stereo / L3_midside_stereo), the
// inverse MDCT (L3_imdct_gr), and the per-band sign change (L3_change_sign).
// The frame-decode-dispatch slice reaches it through the l3Decode seam, which
// this file assigns in init so the dispatch's control flow stays the verbatim
// C with only the cross-slice callee routed through the seam variable.
//
// L3_reorder's float copies and L3_antialias's butterfly multiplies route
// through the f32mul / f32add / f32sub helpers so the mp3_strict build keeps
// each rounding step separate (matching the cgo oracle under
// -ffp-contract=off); the default build may fuse them and is not a bit-exact
// target.

func init() {
	l3Decode = l3DecodeImpl
}

// gAA is L3_antialias's static const g_aa[2][8] (minimp3.h:1014): the
// alias-reduction butterfly coefficients (cs in row 0, ca in row 1).
var gAA = [2][8]float32{
	{0.85749293, 0.88174200, 0.94962865, 0.98331459, 0.99551782, 0.99916056, 0.99989920, 0.99999316},
	{0.51449576, 0.47173197, 0.31337745, 0.18191320, 0.09457419, 0.04096558, 0.01419856, 0.00369997},
}

// l3Reorder is a 1:1 translation of L3_reorder (minimp3.h:995): for a short
// block it de-interleaves the three windows of each scalefactor band so the
// IMDCT sees contiguous per-window coefficients. grbuf is positioned at the
// first short-block subband; scratch is a flat work buffer (minimp3 reuses
// s->syn[0]); sfb is the short-block scalefactor-band width table positioned
// at the first short band (gr->sfbtab + gr->n_long_sfb).
//
//	static void L3_reorder(float *grbuf, float *scratch, const uint8_t *sfb)
//	{
//	    int i, len;
//	    float *src = grbuf, *dst = scratch;
//	    for (;0 != (len = *sfb); sfb += 3, src += 2*len)
//	    {
//	        for (i = 0; i < len; i++, src++)
//	        {
//	            *dst++ = src[0*len];
//	            *dst++ = src[1*len];
//	            *dst++ = src[2*len];
//	        }
//	    }
//	    memcpy(grbuf, scratch, (dst - scratch)*sizeof(float));
//	}
func l3Reorder(grbuf, scratch []float32, sfb []byte) {
	src := 0 // index into grbuf (the C `src` pointer)
	dst := 0 // index into scratch (the C `dst` pointer)
	sp := 0  // index into sfb (the C `sfb` pointer)

	for {
		l := int(sfb[sp])
		if l == 0 {
			break
		}
		for i := 0; i < l; i++ {
			scratch[dst] = grbuf[src+0*l]
			dst++
			scratch[dst] = grbuf[src+1*l]
			dst++
			scratch[dst] = grbuf[src+2*l]
			dst++
			src++
		}
		sp += 3
		src += 2 * l
	}
	// memcpy(grbuf, scratch, (dst - scratch)*sizeof(float)).
	copy(grbuf[:dst], scratch[:dst])
}

// l3Antialias is a 1:1 translation of the scalar (non-SIMD) reference branch
// of L3_antialias (minimp3.h:1012): for each of nbands subband boundaries it
// applies the eight-tap alias-cancellation butterfly across the 18-sample
// subband seam, mixing the last samples of one subband with the first of the
// next. The cgo oracle builds minimp3 with MINIMP3_NO_SIMD, so this scalar
// `for (i = 0; i < 8; i++)` body is the bit-exact target.
//
//	for (; nbands > 0; nbands--, grbuf += 18)
//	    for (i = 0; i < 8; i++)
//	    {
//	        float u = grbuf[18 + i];
//	        float d = grbuf[17 - i];
//	        grbuf[18 + i] = u*g_aa[0][i] - d*g_aa[1][i];
//	        grbuf[17 - i] = u*g_aa[1][i] + d*g_aa[0][i];
//	    }
func l3Antialias(grbuf []float32, nbands int) {
	base := 0
	for ; nbands > 0; nbands-- {
		for i := 0; i < 8; i++ {
			u := grbuf[base+18+i]
			d := grbuf[base+17-i]
			grbuf[base+18+i] = f32sub(f32mul(u, gAA[0][i]), f32mul(d, gAA[1][i]))
			grbuf[base+17-i] = f32add(f32mul(u, gAA[1][i]), f32mul(d, gAA[0][i]))
		}
		base += 18
	}
}

// l3DecodeImpl is a 1:1 translation of L3_decode (minimp3.h:1238): it decodes
// nch channels of one granule in-place into the scratch — scalefactors,
// Huffman/requantization, joint-stereo reconstruction, short-block reorder,
// anti-alias, IMDCT, and the final per-band sign change. gr indexes the
// granule's first L3GrInfo within s.GrInfo (the dispatch passes igr*nch). It is
// assigned to the l3Decode seam in this file's init.
//
//	static void L3_decode(mp3dec_t *h, mp3dec_scratch_t *s, L3_gr_info_t *gr_info, int nch)
func l3DecodeImpl(h *Decoder, s *Scratch, gr int, nch int) {
	grbuf := s.GrBufFlat()
	syn := s.SynFlat()

	for ch := 0; ch < nch; ch++ {
		layer3grLimit := s.Bs.Pos + int(s.GrInfo[gr+ch].Part23Length)
		L3DecodeScalefactors(h.Header[:], s.IstPos[ch][:], &s.Bs, &s.GrInfo[gr+ch], s.Scf[:], ch)
		L3Huffman(grbuf[576*ch:], &s.Bs, &s.GrInfo[gr+ch], s.Scf[:], layer3grLimit)
	}

	if hdrTestIStereo(h.Header[:]) != 0 {
		l3IntensityStereo(grbuf, s.IstPos[1][:], s.GrInfo[gr:], h.Header[:])
	} else if hdrIsMSStereo(h.Header[:]) {
		l3MidsideStereo(grbuf, 0, 576)
	}

	for ch := 0; ch < nch; ch++ {
		gi := &s.GrInfo[gr+ch]
		aaBands := 31
		// n_long_bands = (mixed_block_flag ? 2 : 0) << (HDR_GET_MY_SAMPLE_RATE == 2).
		nLongBands := 0
		if gi.MixedBlockFlag != 0 {
			nLongBands = 2
		}
		if hdrGetMySampleRate(h.Header[:]) == 2 {
			nLongBands <<= 1
		}

		if gi.NShortSfb != 0 {
			aaBands = nLongBands - 1
			l3Reorder(grbuf[576*ch+nLongBands*18:], syn, gi.Sfbtab[gi.NLongSfb:])
		}

		l3Antialias(grbuf[576*ch:], aaBands)
		l3IMDCTGr(grbuf[576*ch:], h.MdctOverlap[ch][:], gi.BlockType, uint(nLongBands))
		l3ChangeSign(grbuf[576*ch:])
	}
}
