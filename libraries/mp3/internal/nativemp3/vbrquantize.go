// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

import "math"

// Layer III VBR quantizer leaf kernels — a 1:1 translation of the
// floating-point leaf functions of the vendored LAME 3.100 encoder's
// libmp3lame/vbrquantize.c that the vbr_mtrh (-V) scalefactor search drives:
// vec_max_c, find_lowest_scalefac, k_34_4, calc_sfb_noise_x34,
// tri_calc_sfb_noise_x34, calc_scalefac, guess_scalefac_x34 and
// find_scalefac_x34. Given a granule's coloured magnitudes xr^(3/4) (xr34) and
// the per-band allowed distortion l3_xmin, these decide the per-scalefactor-band
// quantization step that introduces as much noise as is allowed.
//
// # Scope of this slice ("vbrquantize-leaf")
//
// vec_max_c (vbrquantize.c:116), find_lowest_scalefac (:148), k_34_4 (:169),
// calc_sfb_noise_x34 (:218), tri_calc_sfb_noise_x34 (:278), calc_scalefac
// (:317), guess_scalefac_x34 (:324), find_scalefac_x34 (:347). The block_sf /
// calc_short_block_vbr_sf / calc_long_block_vbr_sf drivers and the
// VBR_encode_frame / VBR_quantize entry points that call them are a later
// slice. Every ported function names its vbrquantize.c counterpart as file:line.
//
// # The TAKEHIRO_IEEE754_HACK
//
// The vendored config.h defines TAKEHIRO_IEEE754_HACK (config.h:89), so
// DOUBLEX == double and k_34_4 quantizes via the magic-number float->int bit
// trick (vbrquantize.c:76-110, 169-207) using the adj43asm table — NOT the
// adj43 table the non-hack takehiro.c quantizer (takehiro_quantize.go) uses.
// adj43asm is filled by InitVbrQuantizeTables below. The hack is reproduced
// bit-for-bit in vqHackQuantize (vbrquantize_fp_strict.go).
//
// # Floating-point parity
//
// LAME's FLOAT is float32 (machine.h); DOUBLEX is double under the hack. Every
// float multiply/add on a bit-exact path routes through the //go:noinline
// vq* helpers (vqMulF / vqSubF / vqMulD / vqAddD /
// vqHackQuantize / vqLog10f) so the mp3_strict build separately-rounds, matching
// the cgo oracle built with -ffp-contract=off. The one residual is
// calc_scalefac's log10f: per the project FP-parity convention (strict.go),
// log10 is not bit-pinned to the platform libm — its result feeds an integer
// truncation so it agrees on the int output except possibly at a .5 boundary.

// adj43asm is the TAKEHIRO_IEEE754_HACK rounding-adjust table
// (quantize_pvt.c:176-178/355-358, declared adj43asm[PRECALC_SIZE]). It is the
// hack-branch analogue of adj43 (which the non-hack quantizer uses); k_34_4
// indexes it by the recovered integer fi.i-MAGIC_INT. Filled by
// InitVbrQuantizeTables.
var adj43asm [PrecalcSize]float32

// InitVbrQuantizeTables fills adj43asm, the TAKEHIRO_IEEE754_HACK branch of
// iteration_init's table fill (quantize_pvt.c:355-358):
//
//	adj43asm[0] = 0.0;
//	for (i = 1; i < PRECALC_SIZE; i++)
//	    adj43asm[i] = i - 0.5 - pow(0.5 * (pow43[i-1] + pow43[i]), 0.75);
//
// It depends on pow43 already being filled (InitQuantizePvtTables, which the VBR
// path also runs); pow43 is identical in both the hack and non-hack branches.
// The double pow is narrowed to float32 on store exactly as the C. Idempotent —
// safe to call after InitQuantizePvtTables.
//
// FP residual (the project pow/log10 libm gap, see strict.go). adj43asm's
// `i - 0.5 - pow(0.5*(pow43[i-1]+pow43[i]), 0.75)` suffers catastrophic
// cancellation (pow(...) ≈ i-0.5), so the ~1-ULP difference between Go's
// math.Pow and the platform libm pow the cgo oracle links is amplified into a
// ~1-ULP-in-float32 difference for roughly 1k of the 8208 entries. This is the
// SAME accepted environmental gap the ATH pow/log10 helpers carry and that
// opus's silk_FLP / the flac port follow. It does NOT propagate to a kernel
// output: adj43asm only nudges a value already added to MAGIC_FLOAT near an
// integer+0.5 boundary, and a 4-million-input K344 stress over the full domain
// plus the full leaf-kernel parity suite show ZERO output divergence — the
// residual would have to land the pre-rounding value within that sub-ULP of a
// round-to-even boundary, which realistic and random inputs never hit.
func InitVbrQuantizeTables() {
	adj43asm[0] = 0.0
	for i := 1; i < PrecalcSize; i++ {
		// C: 0.5 * (pow43[i-1] + pow43[i]). pow43 are FLOAT; under FLT_EVAL_METHOD
		// 0 the float+float add is rounded to float32 BEFORE the *0.5 promotes it
		// to double. The expression suffers catastrophic cancellation in
		// `i - 0.5 - pow(...)`, so this float-vs-double rounding of the add is
		// load-bearing — match the C by adding in float32 then promoting.
		sum := pow43[i-1] + pow43[i] // float32 add, rounds to float32
		adj43asm[i] = float32(float64(i) - 0.5 - math.Pow(0.5*float64(sum), 0.75))
	}
}

// vecMaxC returns the largest of the first bw entries of xr34 (vec_max_c,
// vbrquantize.c:116). The C unrolls a 4-wide running-max with a switch tail; the
// comparisons are exact (>), so the port keeps the 4-wide body and the 3/2/1
// fall-through tail to match the visitation order (irrelevant for max, but kept
// 1:1). bw may be 0, in which case the result is the initial 0.
func vecMaxC(xr34 []float32, bw uint) float32 {
	var xfsf float32 = 0
	i := bw >> 2
	remaining := bw & 0x03

	p := 0
	for i > 0 {
		i--
		if xfsf < xr34[p+0] {
			xfsf = xr34[p+0]
		}
		if xfsf < xr34[p+1] {
			xfsf = xr34[p+1]
		}
		if xfsf < xr34[p+2] {
			xfsf = xr34[p+2]
		}
		if xfsf < xr34[p+3] {
			xfsf = xr34[p+3]
		}
		p += 4
	}
	// switch(remaining){ case 3: ...; case 2: ...; case 1: ...; } — fall-through.
	switch remaining {
	case 3:
		if xfsf < xr34[p+2] {
			xfsf = xr34[p+2]
		}
		fallthrough
	case 2:
		if xfsf < xr34[p+1] {
			xfsf = xr34[p+1]
		}
		fallthrough
	case 1:
		if xfsf < xr34[p+0] {
			xfsf = xr34[p+0]
		}
	}
	return xfsf
}

// findLowestScalefac returns the smallest scalefactor sf such that
// ipow20[sf]*xr34 <= IXMAX_VAL, by an 8-step binary search over sf in [0,255]
// (find_lowest_scalefac, vbrquantize.c:148). It returns 255 if no sf in range
// satisfies the bound (the initial sf_ok). xr34 is the band's maximum xr^(3/4)
// (the vecMaxC result). The multiply ipow20[sf]*xr34 is float32.
func findLowestScalefac(xr34 float32) uint8 {
	var sfOk uint8 = 255
	var sf uint8 = 128
	var delsf uint8 = 64
	var ixmaxVal float32 = float32(IXMAXVAL)
	for i := 0; i < 8; i++ {
		xfsf := vqMulF(ipow20[sf], xr34)
		if xfsf <= ixmaxVal {
			sfOk = sf
			sf -= delsf
		} else {
			sf += delsf
		}
		delsf >>= 1
	}
	return sfOk
}

// k344 quantizes four DOUBLEX coefficients x[0..3] to their integer l3[0..3] via
// the TAKEHIRO_IEEE754_HACK magic-number floor + adj43asm rounding adjust
// (k_34_4, vbrquantize.c:169-207, the TAKEHIRO_IEEE754_HACK branch the vendored
// config selects). Each lane is handled by vqHackQuantize (the per-lane magic
// add / union reinterpret / adjust / re-extract); the four lanes are
// independent so the port does them sequentially rather than interleaved, with
// the identical result. The caller guarantees x[k] <= IXMAX_VAL (the C assert).
func k344(x *[4]float64, l3 *[4]int) {
	l3[0] = vqHackQuantize(x[0])
	l3[1] = vqHackQuantize(x[1])
	l3[2] = vqHackQuantize(x[2])
	l3[3] = vqHackQuantize(x[3])
}

// calcSfbNoiseX34 returns the quantization-noise energy a scalefactor sf would
// introduce over the bw coefficients xr / xr34 of one scalefactor band
// (calc_sfb_noise_x34, vbrquantize.c:218). sfpow = pow20[sf+Q_MAX2] is the step
// size and sfpow34 = ipow20[sf] its -3/4 power; each coefficient is quantized
// (k344) and the squared residual |xr| - sfpow*pow43[l3] is summed. The C unrolls
// the loop 4-wide with a switch tail; the port keeps the 4-wide body and the
// remaining-lanes tail so the accumulation grouping
// `(x0*x0+x1*x1)+(x2*x2+x3*x3)` matches exactly.
//
// FP types (DOUBLEX == double under TAKEHIRO_IEEE754_HACK): x[] is the double
// array, so sfpow34*xr34 is double. The residual `x[k] = fabsf(xr[k]) - sfpow *
// pow43[l3[k]]` is computed in FLOAT (float32 product, float32 subtract, both
// operands float32) and stored into the DOUBLE x[k] (promoted on store). The
// squares x[k]*x[k] and the (...)+(...) sums are therefore DOUBLE, and the
// accumulation `xfsf += <double>` is float32(xfsf) promoted to double, added,
// narrowed back to float32. The caller only calls with sf such that
// sfpow34*xr34 <= IXMAX_VAL.
func calcSfbNoiseX34(xr, xr34 []float32, bw uint, sf uint8) float32 {
	var x [4]float64
	var l3 [4]int
	sfpow := pow20[int(sf)+QMax2] // pow20[sf + Q_MAX2]
	sfpow34 := ipow20[sf]         // ipow20[sf]

	var xfsf float32 = 0
	i := bw >> 2
	remaining := bw & 0x03

	xp := 0 // cursor into xr (the C *xr)
	rp := 0 // cursor into xr34

	for i > 0 {
		i--
		// x[k] = sfpow34 * xr34[k]: FLOAT*FLOAT product (float32), promoted to
		// double on the store into the DOUBLEX x[].
		x[0] = float64(vqMulF(sfpow34, xr34[rp+0]))
		x[1] = float64(vqMulF(sfpow34, xr34[rp+1]))
		x[2] = float64(vqMulF(sfpow34, xr34[rp+2]))
		x[3] = float64(vqMulF(sfpow34, xr34[rp+3]))

		k344(&x, &l3)

		// x[k] = fabsf(xr[k]) - sfpow * pow43[l3[k]] — float32, stored into double.
		x[0] = float64(vqSubF(absF32(xr[xp+0]), vqMulF(sfpow, pow43[l3[0]])))
		x[1] = float64(vqSubF(absF32(xr[xp+1]), vqMulF(sfpow, pow43[l3[1]])))
		x[2] = float64(vqSubF(absF32(xr[xp+2]), vqMulF(sfpow, pow43[l3[2]])))
		x[3] = float64(vqSubF(absF32(xr[xp+3]), vqMulF(sfpow, pow43[l3[3]])))
		// xfsf += (x0*x0 + x1*x1) + (x2*x2 + x3*x3) — double squares/sums.
		s01 := vqAddD(vqMulD(x[0], x[0]), vqMulD(x[1], x[1]))
		s23 := vqAddD(vqMulD(x[2], x[2]), vqMulD(x[3], x[3]))
		xfsf = float32(vqAddD(float64(xfsf), vqAddD(s01, s23)))

		xp += 4
		rp += 4
	}
	if remaining != 0 {
		x[0], x[1], x[2], x[3] = 0, 0, 0, 0
		switch remaining {
		case 3:
			x[2] = float64(vqMulF(sfpow34, xr34[rp+2]))
			fallthrough
		case 2:
			x[1] = float64(vqMulF(sfpow34, xr34[rp+1]))
			fallthrough
		case 1:
			x[0] = float64(vqMulF(sfpow34, xr34[rp+0]))
		}

		k344(&x, &l3)
		x[0], x[1], x[2], x[3] = 0, 0, 0, 0

		switch remaining {
		case 3:
			x[2] = float64(vqSubF(absF32(xr[xp+2]), vqMulF(sfpow, pow43[l3[2]])))
			fallthrough
		case 2:
			x[1] = float64(vqSubF(absF32(xr[xp+1]), vqMulF(sfpow, pow43[l3[1]])))
			fallthrough
		case 1:
			x[0] = float64(vqSubF(absF32(xr[xp+0]), vqMulF(sfpow, pow43[l3[0]])))
		}
		s01 := vqAddD(vqMulD(x[0], x[0]), vqMulD(x[1], x[1]))
		s23 := vqAddD(vqMulD(x[2], x[2]), vqMulD(x[3], x[3]))
		xfsf = float32(vqAddD(float64(xfsf), vqAddD(s01, s23)))
	}
	return xfsf
}

// absF32 is the C fabsf(x): the float32 absolute value. Go's math.Abs is
// float64-only; clearing the sign bit on the float32 representation matches the
// hardware fabsf exactly (it is a bit operation, not an FP rounding).
func absF32(x float32) float32 {
	return math.Float32frombits(math.Float32bits(x) &^ (1 << 31))
}

// calcNoiseCache mirrors the C struct calc_noise_cache (vbrquantize.c:269): a
// per-scalefactor memoization slot for tri_calc_sfb_noise_x34, so a given sf's
// calcSfbNoiseX34 is computed at most once across the find_scalefac search.
type calcNoiseCache struct {
	valid int
	value float32
}

// triCalcSfbNoiseX34 returns 1 ("bad" — distortion) if any of sf, sf+1 or sf-1
// produce quantization noise exceeding l3_xmin, else 0 (tri_calc_sfb_noise_x34,
// vbrquantize.c:278). It memoizes each calcSfbNoiseX34 result in didIt. The
// neighbour probes (sf±1) hedge against the binary search landing one step off
// the true boundary. didIt must have length 256 (indices 0..255).
func triCalcSfbNoiseX34(xr, xr34 []float32, l3Xmin float32, bw uint, sf uint8, didIt []calcNoiseCache) uint8 {
	if didIt[sf].valid == 0 {
		didIt[sf].valid = 1
		didIt[sf].value = calcSfbNoiseX34(xr, xr34, bw, sf)
	}
	if l3Xmin < didIt[sf].value {
		return 1
	}
	if sf < 255 {
		sfX := sf + 1
		if didIt[sfX].valid == 0 {
			didIt[sfX].valid = 1
			didIt[sfX].value = calcSfbNoiseX34(xr, xr34, bw, sfX)
		}
		if l3Xmin < didIt[sfX].value {
			return 1
		}
	}
	if sf > 0 {
		sfX := sf - 1
		if didIt[sfX].valid == 0 {
			didIt[sfX].valid = 1
			didIt[sfX].value = calcSfbNoiseX34(xr, xr34, bw, sfX)
		}
		if l3Xmin < didIt[sfX].value {
			return 1
		}
	}
	return 0
}

// calcScalefac estimates the quantization step (scalefactor) determined by the
// allowed masking l3_xmin over a band of bw lines (calc_scalefac,
// vbrquantize.c:317):
//
//	c = 5.799142446;   // 10 * 10^(2/3) * log10(4/3)
//	return 210 + (int)(c * log10f(l3_xmin / bw) - .5f);
//
// c is a double; log10f is the single-precision log10 of the float ratio
// l3_xmin/bw; the product c*log10f is double, less the float .5f promoted to
// double, then truncated to int. The cast (int) truncates toward zero. log10f
// is the one term outside the bit-exact FP set (see strict.go); it feeds an
// integer truncation, so the int result agrees except possibly at a .5 boundary.
func calcScalefac(l3Xmin float32, bw int) int {
	const c = 5.799142446 // 10 * 10^(2/3) * log10(4/3)
	// l3_xmin / bw is float32 (FLOAT / int promoted to FLOAT); log10f narrows.
	ratio := l3Xmin / float32(bw)
	lg := vqLog10f(ratio)
	// c * (double)log10f(...) - (double).5f, truncated to int.
	v := c*float64(lg) - float64(float32(0.5))
	return 210 + int(v)
}

// guessScalefacX34 returns the calc_scalefac estimate clamped to [sf_min, 255]
// (guess_scalefac_x34, vbrquantize.c:324). It is the cheap (no noise-search)
// scalefactor used when cfg->full_outer_loop < 0; xr / xr34 are unused (the C
// casts them to void). This is the alternate of findScalefacX34.
func guessScalefacX34(xr, xr34 []float32, l3Xmin float32, bw uint, sfMin uint8) uint8 {
	guess := calcScalefac(l3Xmin, int(bw))
	if guess < int(sfMin) {
		return sfMin
	}
	if guess >= 255 {
		return 255
	}
	_ = xr
	_ = xr34
	return uint8(guess)
}

// findScalefacX34 returns the largest scalefactor sf in [sf_min, 255] that keeps
// the quantization noise within l3_xmin, by an 8-step binary search with a
// neighbour-probing noise test (find_scalefac_x34, vbrquantize.c:347). It seeds
// a fresh 256-entry memo cache, halves the step each iteration, and clamps the
// result to sf_min. When no distortion-free sf is found it returns the last sf
// (then clamped). This is the full-search alternate of guessScalefacX34.
func findScalefacX34(xr, xr34 []float32, l3Xmin float32, bw uint, sfMin uint8) uint8 {
	var didIt [256]calcNoiseCache
	var sf uint8 = 128
	var sfOk uint8 = 255
	var delsf uint8 = 128
	var seenGoodOne uint8 = 0
	for i := 0; i < 8; i++ {
		delsf >>= 1
		if sf <= sfMin {
			sf += delsf
		} else {
			bad := triCalcSfbNoiseX34(xr, xr34, l3Xmin, bw, sf, didIt[:])
			if bad != 0 { // distortion. try a smaller scalefactor
				sf -= delsf
			} else {
				sfOk = sf
				sf += delsf
				seenGoodOne = 1
			}
		}
	}
	// returning a scalefac without distortion, if possible.
	if seenGoodOne > 0 {
		sf = sfOk
	}
	if sf <= sfMin {
		sf = sfMin
	}
	return sf
}
