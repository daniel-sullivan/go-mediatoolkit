package nativemp3

// Layer III stereo decoding — a 1:1 translation of minimp3's mid/side (MS)
// and intensity (I/S) stereo reconstruction (minimp3.h:879..993).
//
// # Data layout
//
// minimp3 lays the two channels of a granule out contiguously in a single
// float buffer: the left channel occupies grbuf[0..575] and the right channel
// grbuf[576..1151]. The C functions take `float *left` pointing at the start
// of the left channel and reach the matching right sample via `left + 576`;
// the intensity walk advances `left` band by band with `left += sfb[i]`. The
// Go port mirrors this exactly: `left` is the granule buffer slice and `base`
// is the index of the current band's first left sample, so the right sample
// is `left[base+i+576]` and a band advance is `base += int(sfb[i])`.
//
// # Strict mode
//
// L3_midside_stereo, L3_intensity_stereo_band, L3_ldexp_q2 and the kl*s / kr*s
// scaling in L3_stereo_process are floating point (kind=fp/mixed). Every
// float32 multiply and add/subtract is routed through the package's
// //go:noinline f32mul / f32add / f32sub helpers so the mp3_strict build
// cannot fuse a*b+c into a single-rounded FMA, matching the cgo oracle built
// with -ffp-contract=off. L3_stereo_top_band and L3_intensity_stereo are
// integer/control-flow only and are bit-identical regardless of build tag.
//
// The SIMD fast path inside the C L3_midside_stereo (#if HAVE_SIMD) computes
// exactly the same a+b / a-b as its scalar tail; only the scalar form is
// ported, which is the bit-exact reference target.

// gPan is minimp3's MPEG-1 intensity-stereo panning table, the static const
// float g_pan[7*2] inside L3_stereo_process (minimp3.h:943). Each (kl, kr)
// pair selects the left/right weighting for an intensity position 0..6.
//
//	static const float g_pan[7*2] = { 0,1,0.21132487f,0.78867513f,0.36602540f,0.63397460f,0.5f,0.5f,0.63397460f,0.36602540f,0.78867513f,0.21132487f,1,0 };
var gPan = [7 * 2]float32{
	0, 1,
	0.21132487, 0.78867513,
	0.36602540, 0.63397460,
	0.5, 0.5,
	0.63397460, 0.36602540,
	0.78867513, 0.21132487,
	1, 0,
}

// (L3_ldexp_q2, minimp3.h:642, the quarter-step power-of-two gain used by the
// MPEG-2 intensity weights, is ported as L3Ldexp in dequantize.go alongside
// the scalefactor decode that first consumes it; l3StereoProcess reuses it.)

// l3MidsideStereo reconstructs the left/right channels of a granule from its
// mid/side representation in place: left = mid + side, right = mid - side,
// for the first n samples (l3MidsideStereo is a 1:1 translation of
// L3_midside_stereo, minimp3.h:879). left is the granule buffer with the left
// channel at left[base..] and the right channel at left[base+576..]; only the
// scalar reference loop is ported (the C SIMD fast path computes the same
// sums).
//
//	static void L3_midside_stereo(float *left, int n)
//	{
//	    int i = 0;
//	    float *right = left + 576;
//	    ...
//	    for (; i < n; i++)
//	    {
//	        float a = left[i];
//	        float b = right[i];
//	        left[i] = a + b;
//	        right[i] = a - b;
//	    }
//	}
func l3MidsideStereo(left []float32, base, n int) {
	right := base + 576
	for i := 0; i < n; i++ {
		a := left[base+i]
		b := left[right+i]
		left[base+i] = f32add(a, b)
		left[right+i] = f32sub(a, b)
	}
}

// l3IntensityStereoBand applies an intensity-stereo (I/S) band: the left
// channel carries the band energy, and both channels are derived from it by
// the left/right intensity weights kl and kr (l3IntensityStereoBand is a 1:1
// translation of L3_intensity_stereo_band, minimp3.h:911). The right sample
// (left[base+i+576]) is written from the original left value before the left
// sample is overwritten, matching the C ordering.
//
//	static void L3_intensity_stereo_band(float *left, int n, float kl, float kr)
//	{
//	    int i;
//	    for (i = 0; i < n; i++)
//	    {
//	        left[i + 576] = left[i]*kr;
//	        left[i] = left[i]*kl;
//	    }
//	}
func l3IntensityStereoBand(left []float32, base, n int, kl, kr float32) {
	for i := 0; i < n; i++ {
		left[base+i+576] = f32mul(left[base+i], kr)
		left[base+i] = f32mul(left[base+i], kl)
	}
}

// l3StereoTopBand finds, per short-block window (i % 3), the highest
// scalefactor band index whose right-channel content is non-zero, recording
// it in maxBand (l3StereoTopBand is a 1:1 translation of L3_stereo_top_band,
// minimp3.h:921). right is the granule buffer positioned at the right
// channel; rightBase is the index of its first sample, advanced band by band.
//
//	static void L3_stereo_top_band(const float *right, const uint8_t *sfb, int nbands, int max_band[3])
//	{
//	    int i, k;
//	    max_band[0] = max_band[1] = max_band[2] = -1;
//	    for (i = 0; i < nbands; i++)
//	    {
//	        for (k = 0; k < sfb[i]; k += 2)
//	        {
//	            if (right[k] != 0 || right[k + 1] != 0)
//	            {
//	                max_band[i % 3] = i;
//	                break;
//	            }
//	        }
//	        right += sfb[i];
//	    }
//	}
func l3StereoTopBand(right []float32, rightBase int, sfb []byte, nbands int, maxBand *[3]int) {
	maxBand[0], maxBand[1], maxBand[2] = -1, -1, -1

	for i := 0; i < nbands; i++ {
		for k := 0; k < int(sfb[i]); k += 2 {
			if right[rightBase+k] != 0 || right[rightBase+k+1] != 0 {
				maxBand[i%3] = i
				break
			}
		}
		rightBase += int(sfb[i])
	}
}

// l3StereoProcess applies stereo reconstruction band by band across a
// granule: intensity-stereo weighting for bands above each window's top
// non-zero band (when the intensity position is in range), otherwise
// mid/side reconstruction when MS stereo is active (l3StereoProcess is a 1:1
// translation of L3_stereo_process, minimp3.h:941). left is the granule
// buffer; mpeg2Sh is gr[1].scalefac_compress & 1, the MPEG-2 intensity shift.
//
//	static void L3_stereo_process(float *left, const uint8_t *ist_pos, const uint8_t *sfb, const uint8_t *hdr, int max_band[3], int mpeg2_sh)
//	{
//	    static const float g_pan[7*2] = { ... };
//	    unsigned i, max_pos = HDR_TEST_MPEG1(hdr) ? 7 : 64;
//	    for (i = 0; sfb[i]; i++)
//	    {
//	        unsigned ipos = ist_pos[i];
//	        if ((int)i > max_band[i % 3] && ipos < max_pos)
//	        {
//	            float kl, kr, s = HDR_TEST_MS_STEREO(hdr) ? 1.41421356f : 1;
//	            if (HDR_TEST_MPEG1(hdr))
//	            {
//	                kl = g_pan[2*ipos];
//	                kr = g_pan[2*ipos + 1];
//	            } else
//	            {
//	                kl = 1;
//	                kr = L3_ldexp_q2(1, (ipos + 1) >> 1 << mpeg2_sh);
//	                if (ipos & 1)
//	                {
//	                    kl = kr;
//	                    kr = 1;
//	                }
//	            }
//	            L3_intensity_stereo_band(left, sfb[i], kl*s, kr*s);
//	        } else if (HDR_TEST_MS_STEREO(hdr))
//	        {
//	            L3_midside_stereo(left, sfb[i]);
//	        }
//	        left += sfb[i];
//	    }
//	}
func l3StereoProcess(left []float32, istPos []byte, sfb []byte, hdr []byte, maxBand *[3]int, mpeg2Sh int) {
	maxPos := uint(64)
	if hdrTestMPEG1(hdr) != 0 {
		maxPos = 7
	}

	base := 0
	for i := uint(0); sfb[i] != 0; i++ {
		ipos := uint(istPos[i])
		if int(i) > maxBand[i%3] && ipos < maxPos {
			var kl, kr float32
			s := float32(1)
			if hdrTestMSStereo(hdr) != 0 {
				s = 1.41421356
			}
			if hdrTestMPEG1(hdr) != 0 {
				kl = gPan[2*ipos]
				kr = gPan[2*ipos+1]
			} else {
				kl = 1
				kr = L3Ldexp(1, int((ipos+1)>>1)<<mpeg2Sh)
				if ipos&1 != 0 {
					kl = kr
					kr = 1
				}
			}
			l3IntensityStereoBand(left, base, int(sfb[i]), f32mul(kl, s), f32mul(kr, s))
		} else if hdrTestMSStereo(hdr) != 0 {
			l3MidsideStereo(left, base, int(sfb[i]))
		}
		base += int(sfb[i])
	}
}

// l3IntensityStereo drives the full Layer III stereo reconstruction for a
// granule: it locates each window's top non-zero right-channel band, fixes up
// the trailing intensity positions, then applies l3StereoProcess
// (l3IntensityStereo is a 1:1 translation of L3_intensity_stereo,
// minimp3.h:975). left is the granule buffer (left channel at left[0..575],
// right at left[576..1151]); istPos is the per-band intensity-position array;
// gr[0] is this granule's side info and gr[1] supplies the MPEG-2 intensity
// shift via scalefac_compress.
//
//	static void L3_intensity_stereo(float *left, uint8_t *ist_pos, const L3_gr_info_t *gr, const uint8_t *hdr)
//	{
//	    int max_band[3], n_sfb = gr->n_long_sfb + gr->n_short_sfb;
//	    int i, max_blocks = gr->n_short_sfb ? 3 : 1;
//	    L3_stereo_top_band(left + 576, gr->sfbtab, n_sfb, max_band);
//	    if (gr->n_long_sfb)
//	    {
//	        max_band[0] = max_band[1] = max_band[2] = MINIMP3_MAX(MINIMP3_MAX(max_band[0], max_band[1]), max_band[2]);
//	    }
//	    for (i = 0; i < max_blocks; i++)
//	    {
//	        int default_pos = HDR_TEST_MPEG1(hdr) ? 3 : 0;
//	        int itop = n_sfb - max_blocks + i;
//	        int prev = itop - max_blocks;
//	        ist_pos[itop] = max_band[i] >= prev ? default_pos : ist_pos[prev];
//	    }
//	    L3_stereo_process(left, ist_pos, gr->sfbtab, hdr, max_band, gr[1].scalefac_compress & 1);
//	}
func l3IntensityStereo(left []float32, istPos []byte, gr []L3GrInfo, hdr []byte) {
	var maxBand [3]int
	nSfb := int(gr[0].NLongSfb) + int(gr[0].NShortSfb)
	maxBlocks := 1
	if gr[0].NShortSfb != 0 {
		maxBlocks = 3
	}

	l3StereoTopBand(left, 576, gr[0].Sfbtab, nSfb, &maxBand)
	if gr[0].NLongSfb != 0 {
		// MINIMP3_MAX(a,b) is ((a) < (b) ? (b) : (a)).
		m := maxBand[0]
		if m < maxBand[1] {
			m = maxBand[1]
		}
		if m < maxBand[2] {
			m = maxBand[2]
		}
		maxBand[0], maxBand[1], maxBand[2] = m, m, m
	}
	for i := 0; i < maxBlocks; i++ {
		defaultPos := 0
		if hdrTestMPEG1(hdr) != 0 {
			defaultPos = 3
		}
		itop := nSfb - maxBlocks + i
		prev := itop - maxBlocks
		if maxBand[i] >= prev {
			istPos[itop] = byte(defaultPos)
		} else {
			istPos[itop] = istPos[prev]
		}
	}
	l3StereoProcess(left, istPos, gr[0].Sfbtab, hdr, &maxBand, int(gr[1].ScalefacCompress&1))
}
