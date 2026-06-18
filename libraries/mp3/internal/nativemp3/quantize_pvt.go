// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// quantize_pvt.go is the 1:1 Go port of the floating-point heart of LAME
// 3.100's quantizer-support translation unit libmp3lame/quantize_pvt.c — the
// psychoacoustic distortion budget (calc_xmin) and quantization-noise measure
// (calc_noise / calc_noise_core_c), plus the threshold-of-hearing helpers they
// rest on (athAdjust, ATHmdct, compute_ath). The C reference is the vendored
// tree at libraries/mp3/liblame/libmp3lame/quantize_pvt.c.
//
// # What this slice computes
//
// calc_xmin (quantize_pvt.c:590) walks a granule's scalefactor bands and, for
// each, derives the allowed distortion xmin = ratio*en/bw clamped to the ATH,
// returning the count of bands whose energy exceeds the ATH. calc_noise
// (quantize_pvt.c:816) measures the quantization noise per band against that
// xmin budget, accumulating the over/tot/max-noise dB statistics the iteration
// loop steers on. calc_noise_core_c (quantize_pvt.c:751) is the inner
// per-coefficient noise kernel (three regions: above count1, the 0/1 region,
// and the big-values region). athAdjust (quantize_pvt.c:555) / ATHmdct
// (quantize_pvt.c:211) / compute_ath (quantize_pvt.c:231) shape the ATH into
// the per-band MDCT-domain energy floors calc_xmin reads.
//
// # Floating-point parity
//
// LAME's FLOAT is float32 (machine.h), so every FLOAT field is float32 here.
// The per-frame arithmetic that must round bit-identically to the cgo oracle is
// routed through the //go:noinline ps* helpers (psMul/psAdd/psSub/psDiv,
// psPowf, psFma) in psymodel_fp_strict.go so the mp3_strict build separately
// rounds each product/sum, matching the oracle's -ffp-contract=off build.
//
// C MIXED float/double SEMANTICS are reproduced exactly. Several expressions in
// quantize_pvt.c involve a double-returning libm call (log10, pow) or a double
// literal (DBL_EPSILON, the Max(xmin, DBL_EPSILON) macro): C promotes the FLOAT
// operand to double, evaluates in double, then narrows on assignment to the
// FLOAT lvalue. Those are written as float32(... float64 expression ...) below,
// NOT as float32-domain ps* ops, so the rounding matches the C. The
// single-precision FLOAT*FLOAT products that stay in float (energy sums, the
// ratio*en/e budget) go through the ps* helpers. Each function's doc comment
// flags where the C arithmetic is double vs float.
//
// # Transcendentals
//
// powf is shimmed to the double kernel narrowed (psPowf -> float32(math.Pow))
// because the platform single-precision powf is neither correctly-rounded nor
// portable; the cgo oracle #defines powf(x,y) to ((float)pow((double)x,(double)y))
// so both sides agree on every platform (the SKILL "Transcendentals" rule).
// log10 in compute_ath/athAdjust is the C DOUBLE log10 (FAST_LOG10_X expands to
// (log10(x)*(y)) with USE_FAST_LOG undefined in the vendored config), so it is
// computed as math.Log10(float64(x)) in double and narrowed.

import "math"

// nsAthScale is quantize_pvt.c:41 (#define NSATHSCALE 100): the dynamic-range
// reference subtracted from the ATH when cfg->ATHfixpoint is not set.
const nsAthScale = 100

// dblEpsilon is the C DBL_EPSILON (<float.h>): the smallest x such that 1+x != 1
// in double precision. quantize_pvt.c seeds rh2 with it and floors xmin at it.
const dblEpsilon = float64(2.2204460492503131e-16)

// floatMaxQ is the FLOAT_MAX compute_ath seeds the per-band ATH minimum with
// (quantize_pvt.c:246/257/268/280). In quantize_pvt.c's translation unit
// FLOAT_MAX resolves to the machine.h fallback 1e37, NOT FLT_MAX: machine.h
// (machine.h:128) only #defines FLOAT_MAX to FLT_MAX when FLT_MAX is already
// visible, but quantize_pvt.c includes machine.h (line 32) BEFORE <float.h>
// (line 38), so FLT_MAX is undefined at that point and FLOAT_MAX becomes the
// `1e37 /* approx */` fallback. The cgo oracle therefore seeds with float32(1e37)
// (verified by the compute_ath parity assertion on the empty-range psfb bands),
// so the Go port must match — this is distinct from psymodel.go's floatMax
// (math.MaxFloat32), whose translation unit context differs.
const floatMaxQ = float32(1e37)

// pretab is LAME's Table B.6 layer3 preemphasis table (quantize_pvt.c:86,
// pretab[SBMAX_l]); calc_noise adds pretab[sfb] to the scalefactor when the
// granule's preflag is set.
var pretab = [SBMAXl]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1, 2, 2, 3, 3, 3, 2, 0,
}

// CalcNoiseResult is LAME's calc_noise_result (quantize_pvt.h:61): the
// aggregate quantization-noise statistics calc_noise returns to the iteration
// loop. Field names/types mirror the C struct.
type CalcNoiseResult struct {
	OverNoise float32 // over_noise: sum of quantization noise > masking
	TotNoise  float32 // tot_noise:  sum of all quantization noise
	MaxNoise  float32 // max_noise:  max quantization noise
	OverCount int     // over_count: number of bands with noise > masking
	OverSSD   int     // over_SSD:   SSD-like cost of distorted bands
	Bits      int     // bits
}

// CalcNoiseData is LAME's calc_noise_data (quantize_pvt.h:75): the per-band
// noise cache that lets calc_noise reuse values when the quantizer step is
// unchanged across calls.
type CalcNoiseData struct {
	GlobalGain int         // global_gain
	SfbCount1  int         // sfb_count1
	Step       [39]int     // step[39]
	Noise      [39]float32 // noise[39]
	NoiseLog   [39]float32 // noise_log[39]
}

// ---------------------------------------------------------------------------
// Quantizer precompute tables (quantize_pvt.c:172-180) and their fill from
// iteration_init (quantize_pvt.c:351-367). calc_noise reads pow43[] and the
// POW20 table pow20[]; they are package globals exactly as in C, populated by
// InitQuantizePvtTables (the table-fill portion of iteration_init). A later
// iteration-init slice owns the rest of iteration_init's setup; this slice
// owns and exports the table fill its calc_noise consumes.
// ---------------------------------------------------------------------------

// IXMAXVAL is quantize_pvt.h:25 (#define IXMAX_VAL 8206) and PrecalcSize is
// quantize_pvt.h:30 (#define PRECALC_SIZE (IXMAX_VAL+2)): the size of the pow43
// / adj43 precompute tables.
const (
	IXMAXVAL    = 8206
	PrecalcSize = IXMAXVAL + 2
)

// QMax / QMax2 are quantize_pvt.h:47/48 (#define Q_MAX (256+1), Q_MAX2 116):
// the size and offset of the POW20 / IPOW20 step tables. POW20(x) indexes
// pow20[x+Q_MAX2].
const (
	QMax  = 256 + 1
	QMax2 = 116
)

// Quantizer precompute tables (quantize_pvt.c:172-179). FLOAT == float32.
var (
	pow20  [QMax + QMax2 + 1]float32 // pow20[Q_MAX + Q_MAX2 + 1]
	ipow20 [QMax]float32             // ipow20[Q_MAX]
	pow43  [PrecalcSize]float32      // pow43[PRECALC_SIZE]
	adj43  [PrecalcSize]float32      // adj43[PRECALC_SIZE]
)

// InitQuantizePvtTables fills the pow43 / adj43 / ipow20 / pow20 precompute
// tables, mirroring the table-fill block of iteration_init (quantize_pvt.c:351-
// 367, the non-TAKEHIRO_IEEE754_HACK branch — the vendored config does not
// define that hack). pow43[i] = i^(4/3); adj43 is the rounding-adjust table;
// ipow20/pow20 are the quarter/eighth-step quantizer gain tables. All four use
// DOUBLE pow exactly as the C (pow((FLOAT)i, 4.0/3.0) etc.) and narrow to
// float32 on store. calc_noise reads pow43[] and pow20[]; the full
// iteration_init (huffman_init, the longfact/shortfact fill, the loop-function
// selection) is a separate slice — this exported entry owns only the table fill
// its calc_noise depends on, called once before the parity kernels.
func InitQuantizePvtTables() {
	pow43[0] = 0.0
	for i := 1; i < PrecalcSize; i++ {
		pow43[i] = float32(math.Pow(float64(i), 4.0/3.0))
	}

	for i := 0; i < PrecalcSize-1; i++ {
		adj43[i] = float32(float64(i+1) - math.Pow(0.5*(float64(pow43[i])+float64(pow43[i+1])), 0.75))
	}
	adj43[PrecalcSize-1] = 0.5

	for i := 0; i < QMax; i++ {
		ipow20[i] = float32(math.Pow(2.0, float64(i-210)*-0.1875))
	}
	for i := 0; i <= QMax+QMax2; i++ {
		pow20[i] = float32(math.Pow(2.0, float64(i-210-QMax2)*0.25))
	}
}

// pow20Idx is LAME's POW20(x) macro (machine.h:88): pow20[x+Q_MAX2]. The C
// asserts 0 <= x+Q_MAX2 && x < Q_MAX; the Go port relies on the same caller
// invariants (calc_noise's step exponent is in range by construction).
func pow20Idx(x int) float32 { return pow20[x+QMax2] }

// ---------------------------------------------------------------------------
// athAdjust / ATHmdct / compute_ath — the ATH shaping (quantize_pvt.c:555/211/
// 231).
//
// ACCEPTED FP RESIDUAL (NOT bit-exact, <=2 ULP). These three helpers rest on
// pow()/log10() (psPowf -> math.Pow, FAST_LOG10_X -> math.Log10), and Go's
// math.Pow/math.Log10 are not bit-identical to the platform libm the cgo
// oracle links — unlike math.Cos/Sin/Exp/Log, which Go's stdlib does match to
// the last bit. The ATH energy floors therefore land within <=2 ULP of the C
// oracle rather than byte-for-byte. This is an INTENTIONAL, environmental
// libm-vs-Go-stdlib gap, accepted on the same footing as opus's silk_FLP and
// the flac port (which likewise do not bit-pin pow/log10). The downstream
// pinned set — calc_xmin / calc_noise / calc_noise_core_c and the rest of the
// quantizer — remains bit-exact; only the pow/log10-derived ATH inputs carry
// this residual. Comment only: do not "fix" the math to chase the last ULPs.
// ---------------------------------------------------------------------------

// athAdjust is LAME's athAdjust (quantize_pvt.c:555): adjusts the ATH while
// keeping the original noise floor, affecting higher frequencies more. C
// SEMANTICS: o/p/u/v/w are FLOAT (float32); FAST_LOG10_X(x,y) expands to
// (log10(x)*(y)) which is DOUBLE (log10 is double, the float y promotes to
// double, product double, narrowed on store to the FLOAT lvalue u/w). The
// remaining +/-/* are float32. powf is the double-narrowed shim.
func athAdjust(a, x, athFloor, athFixpoint float32) float32 {
	const o = float32(90.30873362)
	var p float32
	if athFixpoint < 1.0 { // (ATHfixpoint < 1.f) ? 94.82444863f : ATHfixpoint
		p = 94.82444863
	} else {
		p = athFixpoint
	}
	// FAST_LOG10_X(x, 10.0f) = log10(x)*10.0f, evaluated in double, narrowed.
	u := float32(math.Log10(float64(x)) * 10.0)
	v := psMul(a, a) // FLOAT const v = a * a
	var w float32    // FLOAT w = 0.0f
	u = psSub(u, athFloor)
	if v > 1e-20 {
		// 1.f + FAST_LOG10_X(v, 10.0f/o): 10.0f/o is a float div; the
		// FAST_LOG10_X product and the 1.f+ are evaluated in double (log10 is
		// double) then narrowed to the FLOAT w. The 1.0 + product is routed
		// through the //go:noinline double helpers so the strict build does not
		// fuse it into a double FMA (the oracle's util.h FAST_LOG10_X expands in
		// a -ffp-contract=off TU, so the add rounds separately).
		coef := psDiv(10.0, o) // 10.0f / o  (float32 division of float consts)
		w = float32(psAddD64(1.0, psMulD64(math.Log10(float64(v)), float64(coef))))
	}
	if w < 0 {
		w = 0.0
	}
	u = psMul(u, w)
	u = psAdd(u, psSub(psAdd(athFloor, o), p)) // u += athFloor + o - p
	return psPowf(10.0, psMul(0.1, u))         // powf(10.f, 0.1f * u)
}

// athmdct is LAME's static ATHmdct (quantize_pvt.c:211): the per-frequency ATH
// in MDCT-domain energy. ath = ATHformula(cfg,f); subtract the fixpoint (or
// NSATHSCALE), add the dB offset, then powf(10, ath*0.1f) to energy. The
// subtract/add are float32; ATHformula already returns float32; powf is the
// double-narrowed shim.
func athmdct(cfg *SessionConfig, f float32) float32 {
	ath := athFormula(cfg, f)

	if cfg.ATHfixpoint > 0 {
		ath = psSub(ath, cfg.ATHfixpoint)
	} else {
		ath = psSub(ath, nsAthScale)
	}
	ath = psAdd(ath, cfg.ATHOffsetDb)

	// modify the MDCT scaling for the ATH and convert to energy
	ath = psPowf(10.0, psMul(ath, 0.1))
	return ath
}

// computeATH is LAME's static compute_ath (quantize_pvt.c:231): fills the
// per-scalefactor-band ATH energy floors (ATH->l, ATH->psfb21, ATH->s,
// ATH->psfb12) by taking, per band, the minimum athmdct over the band's MDCT
// lines, then scaling the short / psfb12 entries by the band width, applying
// the noATH override, and computing ATH->floor. samp_freq is cfg->samplerate_out
// as a FLOAT. The freq = i*samp_freq/(2*576|192) is a single-precision product
// chain; ATH->floor = 10.*log10(athmdct(cfg,-1.)) is DOUBLE (the literal 10.
// and log10 are double) narrowed to the FLOAT floor.
func computeATH(gfc *LameInternalFlags) {
	cfg := &gfc.Cfg
	ath := gfc.ATH
	sampFreq := float32(cfg.SamplerateOut)

	for sfb := 0; sfb < SBMAXl; sfb++ {
		start := gfc.ScalefacBand.L[sfb]
		end := gfc.ScalefacBand.L[sfb+1]
		ath.L[sfb] = floatMaxQ
		for i := start; i < end; i++ {
			freq := psDiv(psMul(float32(i), sampFreq), 2*576)
			athF := athmdct(cfg, freq) // freq in kHz
			ath.L[sfb] = minF32(ath.L[sfb], athF)
		}
	}

	for sfb := 0; sfb < PSFB21; sfb++ {
		start := gfc.ScalefacBand.Psfb21[sfb]
		end := gfc.ScalefacBand.Psfb21[sfb+1]
		ath.Psfb21[sfb] = floatMaxQ
		for i := start; i < end; i++ {
			freq := psDiv(psMul(float32(i), sampFreq), 2*576)
			athF := athmdct(cfg, freq)
			ath.Psfb21[sfb] = minF32(ath.Psfb21[sfb], athF)
		}
	}

	for sfb := 0; sfb < SBMAXs; sfb++ {
		start := gfc.ScalefacBand.S[sfb]
		end := gfc.ScalefacBand.S[sfb+1]
		ath.S[sfb] = floatMaxQ
		for i := start; i < end; i++ {
			freq := psDiv(psMul(float32(i), sampFreq), 2*192)
			athF := athmdct(cfg, freq)
			ath.S[sfb] = minF32(ath.S[sfb], athF)
		}
		ath.S[sfb] = psMul(ath.S[sfb], float32(gfc.ScalefacBand.S[sfb+1]-gfc.ScalefacBand.S[sfb]))
	}

	for sfb := 0; sfb < PSFB12; sfb++ {
		start := gfc.ScalefacBand.Psfb12[sfb]
		end := gfc.ScalefacBand.Psfb12[sfb+1]
		ath.Psfb12[sfb] = floatMaxQ
		for i := start; i < end; i++ {
			freq := psDiv(psMul(float32(i), sampFreq), 2*192)
			athF := athmdct(cfg, freq)
			ath.Psfb12[sfb] = minF32(ath.Psfb12[sfb], athF)
		}
		// not sure about the following (C comment)
		ath.Psfb12[sfb] = psMul(ath.Psfb12[sfb], float32(gfc.ScalefacBand.S[13]-gfc.ScalefacBand.S[12]))
	}

	// no-ATH mode: reduce ATH to -200 dB
	if cfg.NoATH != 0 {
		for sfb := 0; sfb < SBMAXl; sfb++ {
			ath.L[sfb] = 1e-20
		}
		for sfb := 0; sfb < PSFB21; sfb++ {
			ath.Psfb21[sfb] = 1e-20
		}
		for sfb := 0; sfb < SBMAXs; sfb++ {
			ath.S[sfb] = 1e-20
		}
		for sfb := 0; sfb < PSFB12; sfb++ {
			ath.Psfb12[sfb] = 1e-20
		}
	}

	// work in progress, don't rely on it too much (C comment).
	// gfc->ATH->floor = 10. * log10(ATHmdct(cfg, -1.)) — DOUBLE narrowed.
	ath.Floor = float32(10.0 * math.Log10(float64(athmdct(cfg, -1.0))))
}

// ---------------------------------------------------------------------------
// calc_xmin (quantize_pvt.c:590).
// ---------------------------------------------------------------------------

// calcXmin is LAME's calc_xmin (quantize_pvt.c:590): computes the allowed
// distortion l3_xmin[sfb] per scalefactor band from the psychoacoustic ratio
// and the ATH, and returns the number of bands whose energy exceeds the ATH.
//
// pxmin must have room for cod_info.Psymax entries (the long bands up to
// psy_lmax, then the short bands * 3). C SEMANTICS notes inline: en0/x2/rh1/rh2
// are float32 sums; the Max(xmin, DBL_EPSILON) and rh2=DBL_EPSILON seeds use
// the DOUBLE epsilon (comparison/store narrowed to float32); the 1e-12f /
// 1e-14f thresholds are float32; cd_psy->decay is gfc.CdPsy.Decay.
func calcXmin(gfc *LameInternalFlags, ratio *III_psy_ratio, codInfo *GrInfo, pxmin []float32) int {
	cfg := &gfc.Cfg
	ath := gfc.ATH
	xr := &codInfo.Xr
	athOver := 0
	j := 0
	pi := 0 // write cursor into pxmin (the C *pxmin++ pointer)

	for gsfb := 0; gsfb < codInfo.PsyLmax; gsfb++ {
		xmin := athAdjust(ath.AdjustFactor, ath.L[gsfb], ath.Floor, cfg.ATHfixpoint)
		xmin = psMul(xmin, gfc.SvQnt.Longfact[gsfb])

		width := codInfo.Width[gsfb]
		rh1 := psDiv(xmin, float32(width))
		rh2 := float32(dblEpsilon) // rh2 = DBL_EPSILON (double narrowed to FLOAT)
		en0 := float32(0.0)
		for l := 0; l < width; l++ {
			xa := xr[j]
			j++
			x2 := psMul(xa, xa)
			en0 = psAdd(en0, x2)
			if x2 < rh1 {
				rh2 = psAdd(rh2, x2)
			} else {
				rh2 = psAdd(rh2, rh1)
			}
		}
		if en0 > xmin {
			athOver++
		}

		var rh3 float32
		if en0 < xmin {
			rh3 = en0
		} else if rh2 < xmin {
			rh3 = xmin
		} else {
			rh3 = rh2
		}
		xmin = rh3
		{
			e := ratio.En.L[gsfb]
			if e > 1e-12 {
				x := psMul(en0, ratio.Thm.L[gsfb])
				x = psDiv(x, e)
				x = psMul(x, gfc.SvQnt.Longfact[gsfb])
				if xmin < x {
					xmin = x
				}
			}
		}
		// xmin = Max(xmin, DBL_EPSILON): compare/store in DOUBLE, narrow.
		xmin = float32(math.Max(float64(xmin), dblEpsilon))
		if en0 > psAdd(xmin, 1e-14) {
			codInfo.EnergyAboveCutoff[gsfb] = 1
		} else {
			codInfo.EnergyAboveCutoff[gsfb] = 0
		}
		pxmin[pi] = xmin
		pi++
	} // end of long block loop

	// determine the highest non-zero coeff (quantize_pvt.c:653-684). |xr[k]| uses
	// the C fabs (double) > 1e-12f (float promoted to double): both sides double.
	maxNonzero := 0
	for k := 575; k > 0; k-- {
		if math.Abs(float64(xr[k])) > 1e-12 {
			maxNonzero = k
			break
		}
	}
	if codInfo.BlockType != ShortType { // NORM, START or STOP type, but not SHORT
		maxNonzero |= 1 // only odd numbers
	} else {
		maxNonzero /= 6 // 3 short blocks
		maxNonzero *= 6
		maxNonzero += 5
	}

	if gfc.SvQnt.Sfb21Extra == 0 && cfg.SamplerateOut < 44000 {
		sfbL := 21
		sfbS := 12
		if cfg.SamplerateOut <= 8000 {
			sfbL = 17
			sfbS = 9
		}
		limit := 575
		if codInfo.BlockType != ShortType {
			limit = gfc.ScalefacBand.L[sfbL] - 1
		} else {
			limit = 3*gfc.ScalefacBand.S[sfbS] - 1
		}
		if maxNonzero > limit {
			maxNonzero = limit
		}
	}
	codInfo.MaxNonzeroCoeff = maxNonzero

	gsfb := codInfo.PsyLmax
	for sfb := codInfo.SfbSmin; gsfb < codInfo.Psymax; sfb, gsfb = sfb+1, gsfb+3 {
		tmpATH := athAdjust(ath.AdjustFactor, ath.S[sfb], ath.Floor, cfg.ATHfixpoint)
		tmpATH = psMul(tmpATH, gfc.SvQnt.Shortfact[sfb])

		width := codInfo.Width[gsfb]
		for b := 0; b < 3; b++ {
			en0 := float32(0.0)
			xmin := tmpATH

			rh1 := psDiv(tmpATH, float32(width))
			rh2 := float32(dblEpsilon)
			for l := 0; l < width; l++ {
				xa := xr[j]
				j++
				x2 := psMul(xa, xa)
				en0 = psAdd(en0, x2)
				if x2 < rh1 {
					rh2 = psAdd(rh2, x2)
				} else {
					rh2 = psAdd(rh2, rh1)
				}
			}
			if en0 > tmpATH {
				athOver++
			}

			var rh3 float32
			if en0 < tmpATH {
				rh3 = en0
			} else if rh2 < tmpATH {
				rh3 = tmpATH
			} else {
				rh3 = rh2
			}
			xmin = rh3
			{
				e := ratio.En.S[sfb][b]
				if e > 1e-12 {
					x := psMul(en0, ratio.Thm.S[sfb][b])
					x = psDiv(x, e)
					x = psMul(x, gfc.SvQnt.Shortfact[sfb])
					if xmin < x {
						xmin = x
					}
				}
			}
			xmin = float32(math.Max(float64(xmin), dblEpsilon))
			if en0 > psAdd(xmin, 1e-14) {
				codInfo.EnergyAboveCutoff[gsfb+b] = 1
			} else {
				codInfo.EnergyAboveCutoff[gsfb+b] = 0
			}
			pxmin[pi] = xmin
			pi++
		} // b
		if cfg.UseTemporalMaskingEffect != 0 {
			// pxmin[-3], pxmin[-2], pxmin[-1] relative to the post-increment cursor.
			if pxmin[pi-3] > pxmin[pi-3+1] {
				pxmin[pi-3+1] = psAdd(pxmin[pi-3+1], psMul(psSub(pxmin[pi-3], pxmin[pi-3+1]), gfc.CdPsy.Decay))
			}
			if pxmin[pi-3+1] > pxmin[pi-3+2] {
				pxmin[pi-3+2] = psAdd(pxmin[pi-3+2], psMul(psSub(pxmin[pi-3+1], pxmin[pi-3+2]), gfc.CdPsy.Decay))
			}
		}
	} // end of short block sfb loop

	return athOver
}

// ---------------------------------------------------------------------------
// calc_noise_core_c (quantize_pvt.c:751) + calc_noise (quantize_pvt.c:816).
// ---------------------------------------------------------------------------

// calcNoiseCoreC is LAME's static calc_noise_core_c (quantize_pvt.c:751): the
// inner per-coefficient noise kernel over l pairs of lines starting at
// *startline, in three regions — above count1 (raw squared xr), the 0/1 region
// (|xr|-{0,step}), and the big-values region (|xr|-pow43[ix]*step). It returns
// the accumulated noise and advances *startline (here startJ, returned). All
// products/sums are float32; fabs is the C double fabs narrowed via the
// subtraction operands (C: fabs returns double, ix01[ix[j]] is float promoted,
// difference is double, temp is FLOAT -> narrowed). To match, the |xr|-c
// difference is formed in DOUBLE then narrowed to temp, and temp*temp is the
// float32 product accumulated in the float32 noise sum.
func calcNoiseCoreC(codInfo *GrInfo, startJ int, l int, step float32) (float32, int) {
	noise := float32(0.0)
	j := startJ
	ix := &codInfo.L3Enc

	if j > codInfo.Count1 {
		for l > 0 {
			l--
			temp := codInfo.Xr[j]
			j++
			noise = psAdd(noise, psMul(temp, temp))
			temp = codInfo.Xr[j]
			j++
			noise = psAdd(noise, psMul(temp, temp))
		}
	} else if j > codInfo.BigValues {
		var ix01 [2]float32
		ix01[0] = 0
		ix01[1] = step
		for l > 0 {
			l--
			// temp = fabs(xr[j]) - ix01[ix[j]]: fabs is DOUBLE, the float ix01
			// promotes to double, difference DOUBLE, narrowed to temp (FLOAT).
			temp := float32(math.Abs(float64(codInfo.Xr[j])) - float64(ix01[ix[j]]))
			j++
			noise = psAdd(noise, psMul(temp, temp))
			temp = float32(math.Abs(float64(codInfo.Xr[j])) - float64(ix01[ix[j]]))
			j++
			noise = psAdd(noise, psMul(temp, temp))
		}
	} else {
		for l > 0 {
			l--
			// temp = fabs(xr[j]) - pow43[ix[j]]*step. The pow43[ix]*step product
			// is a float32 multiply; fabs(xr) is double; the difference is in
			// DOUBLE (fabs double minus the float product promoted), narrowed.
			temp := float32(math.Abs(float64(codInfo.Xr[j])) - float64(psMul(pow43[ix[j]], step)))
			j++
			noise = psAdd(noise, psMul(temp, temp))
			temp = float32(math.Abs(float64(codInfo.Xr[j])) - float64(psMul(pow43[ix[j]], step)))
			j++
			noise = psAdd(noise, psMul(temp, temp))
		}
	}

	return noise, j
}

// calcNoise is LAME's calc_noise (quantize_pvt.c:816): measures the
// quantization noise per scalefactor band against l3_xmin, fills distort[] with
// the per-band noise/xmin ratio and res with the over/tot/max-noise statistics,
// and returns the number of bands whose noise exceeds the masking (over). The
// optional prevNoise cache (may be nil) reuses values when the per-band
// quantizer step is unchanged.
//
// C SEMANTICS: s is the integer step exponent; step = POW20(s) (a float32 table
// lookup). r_l3_xmin = 1.f / *l3_xmin is a float32 reciprocal; distort_ =
// r_l3_xmin*noise is a float32 product; noise = FAST_LOG10(Max(distort_,
// 1E-20f)) is the DOUBLE log10 (FAST_LOG10 expands to log10(x), USE_FAST_LOG
// undefined) of a float32 max, narrowed to the FLOAT noise; tot/over_noise_db
// are float32 accumulations; over_SSD uses an int cast of (noise*10+.5).
func calcNoise(codInfo *GrInfo, l3Xmin []float32, distort []float32, res *CalcNoiseResult, prevNoise *CalcNoiseData) int {
	over := 0
	overNoiseDb := float32(0.0)
	totNoiseDb := float32(0.0)
	maxNoise := float32(-20.0) // -200 dB relative to masking
	j := 0
	si := 0 // scalefac read cursor (the C *scalefac++ pointer)
	li := 0 // l3_xmin read cursor (the C *l3_xmin++ pointer)
	di := 0 // distort write cursor (the C *distort++ pointer)

	res.OverSSD = 0

	for sfb := 0; sfb < codInfo.Psymax; sfb++ {
		pre := 0
		if codInfo.Preflag != 0 {
			pre = pretab[sfb]
		}
		s := codInfo.GlobalGain -
			((codInfo.Scalefac[si] + pre) << (codInfo.ScalefacScale + 1)) -
			codInfo.SubblockGain[codInfo.Window[sfb]]*8
		si++
		rL3Xmin := psDiv(1.0, l3Xmin[li]) // 1.f / *l3_xmin++
		li++
		distort_ := float32(0.0)
		noise := float32(0.0)

		if prevNoise != nil && prevNoise.Step[sfb] == s {
			// use previously computed values
			j += codInfo.Width[sfb]
			distort_ = psMul(rL3Xmin, prevNoise.Noise[sfb])
			noise = prevNoise.NoiseLog[sfb]
		} else {
			step := pow20Idx(s)
			l := codInfo.Width[sfb] >> 1

			if (j + codInfo.Width[sfb]) > codInfo.MaxNonzeroCoeff {
				usefullsize := codInfo.MaxNonzeroCoeff - j + 1
				if usefullsize > 0 {
					l = usefullsize >> 1
				} else {
					l = 0
				}
			}

			noise, j = calcNoiseCoreC(codInfo, j, l, step)

			if prevNoise != nil {
				prevNoise.Step[sfb] = s
				prevNoise.Noise[sfb] = noise
			}

			distort_ = psMul(rL3Xmin, noise)

			// noise = FAST_LOG10(Max(distort_, 1E-20f)): Max is float32, log10
			// is DOUBLE, narrowed to the FLOAT noise.
			noise = float32(math.Log10(float64(maxF32(distort_, 1e-20))))

			if prevNoise != nil {
				prevNoise.NoiseLog[sfb] = noise
			}
		}
		distort[di] = distort_
		di++

		if prevNoise != nil {
			prevNoise.GlobalGain = codInfo.GlobalGain
		}

		// tot_noise *= Max(noise, 1E-20) (C comment); the live code adds dB.
		totNoiseDb = psAdd(totNoiseDb, noise)

		if noise > 0.0 {
			// tmp = Max((int)(noise*10 + .5), 1): noise*10+.5 is DOUBLE (noise
			// FLOAT promoted, 10 and .5 double), truncated to int.
			tmp := int(float64(noise)*10 + 0.5)
			if tmp < 1 {
				tmp = 1
			}
			res.OverSSD += tmp * tmp

			over++
			overNoiseDb = psAdd(overNoiseDb, noise)
		}
		maxNoise = maxF32(maxNoise, noise)
	}

	res.OverCount = over
	res.TotNoise = totNoiseDb
	res.OverNoise = overNoiseDb
	res.MaxNoise = maxNoise

	return over
}
