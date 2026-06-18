// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

import "math"

// 1:1 Go translation of the per-frame threshold/masking machinery of LAME
// 3.100's psymodel.c — energy and tonality per partition band, the spreading
// convolution, additive masking, partition→scalefactor mapping, M/S threshold
// derivation, attack detection / block-type decision, and the perceptual
// entropy used as the bit-allocation pre-step. The driver L3psycho_anal_vbr is
// in psymodel_anal.go; the one-time constant setup is in psymodel_init.go.
//
// FLOAT is float32. FMA-sensitive `a + b*c` terms are routed through psFma /
// psMul / psAdd; transcendentals (pow/exp/log/log10) match the C's
// double-precision libm and narrow to float32 on store.

// ── mask_add additive-masking constants (psymodel.c:246) ───────────────────

const (
	i1Limit = 8  // psymodel.c:246, I1LIMIT (as in if(i>8))
	i2Limit = 23 // psymodel.c:247, I2LIMIT
	mLimit  = 15 // psymodel.c:248, MLIMIT
)

// maMaxI1 = pow(10,(I1LIMIT+1)/16.0) (psymodel.c:251).
const maMaxI1 = float32(3.6517412725483771)

// maMaxI2 = pow(10,(I2LIMIT+1)/16.0) (psymodel.c:253).
const maMaxI2 = float32(31.622776601683793)

// maMaxM = pow(10,(MLIMIT)/10.0) (psymodel.c:255). Unused on the active path
// (it backs the disabled mask_add variant) but kept for the 1:1 mapping.
const maMaxM = float32(31.622776601683793)

// maskTab is the tonality masking table tab[] (psymodel.c:263): 0 dB (TMN) to
// 9.3 dB (NMT) as linear ratios.
var maskTab = [...]float32{
	1.0,     // pow(10,-0)
	0.79433, // pow(10,-0.1)
	0.63096, // pow(10,-0.2)
	0.63096,
	0.63096,
	0.63096,
	0.63096,
	0.25119, // pow(10,-0.6)
	0.11749, // pow(10,-0.93)
}

// tabMaskAddDelta is psymodel.c:275, the per-tab delta table.
var tabMaskAddDelta = [...]int{2, 2, 2, 1, 1, 1, 0, 0, -1}

// maskAddDelta is LAME's mask_add_delta (psymodel.c:278).
func maskAddDelta(i int) int { return tabMaskAddDelta[i] }

// maskTable2 is vbrpsy_mask_add's static table2[] (psymodel.c:307).
var maskTable2 = [...]float32{
	1.33352 * 1.33352, 1.35879 * 1.35879, 1.38454 * 1.38454, 1.39497 * 1.39497,
	1.40548 * 1.40548, 1.3537 * 1.3537, 1.30382 * 1.30382, 1.22321 * 1.22321,
	1.14758 * 1.14758,
	1,
}

// vbrpsyMaskAdd is LAME's vbrpsy_mask_add (psymodel.c:304): addition of
// simultaneous masking (Naoki Shibata). FAST_LOG10_X(ratio,16.0f) is the
// non-USE_FAST_LOG log10(ratio)*16 form.
func vbrpsyMaskAdd(m1, m2 float32, b, delta int) float32 {
	var ratio float32

	if m1 < 0 {
		m1 = 0
	}
	if m2 < 0 {
		m2 = 0
	}
	if m1 <= 0 {
		return m2
	}
	if m2 <= 0 {
		return m1
	}
	if m2 > m1 {
		ratio = psDiv(m2, m1)
	} else {
		ratio = psDiv(m1, m2)
	}
	if absInt(b) <= delta { // approximately, 1 bark = 3 partitions
		// originally 'if(i > 8)'
		if ratio >= maMaxI1 {
			return psAdd(m1, m2)
		}
		i := int(fastLog10X(ratio, 16.0))
		return psMul(psAdd(m1, m2), maskTable2[i])
	}
	if ratio < maMaxI2 {
		return psAdd(m1, m2)
	}
	if m1 < m2 {
		m1 = m2
	}
	return m1
}

// ── partition → scalefactor conversion (psymodel.c:360) ────────────────────

// convertPartition2scalefac is LAME's convert_partition2scalefac
// (psymodel.c:360): splits partition-band energies (eb) and thresholds (thr)
// across the scalefactor bands of mapping gd, writing ennOut/thmOut.
func convertPartition2scalefac(gd *PsyConstCB2SB, eb, thr []float32, ennOut, thmOut []float32) {
	var enn, thmm float32
	n := gd.NSb
	enn, thmm = 0.0, 0.0
	sb, b := 0, 0
	for ; sb < n; b, sb = b+1, sb+1 {
		boSb := gd.Bo[sb]
		npart := gd.Npart
		bLim := boSb
		if npart < bLim {
			bLim = npart
		}
		for b < bLim {
			enn = psAdd(enn, eb[b])
			thmm = psAdd(thmm, thr[b])
			b++
		}
		if b >= npart {
			ennOut[sb] = enn
			thmOut[sb] = thmm
			sb++
			break
		}
		// at transition sfb -> sfb+1
		wCurr := gd.BoWeight[sb]
		wNext := psSub(1.0, wCurr)
		enn = psAdd(enn, psMul(wCurr, eb[b]))
		thmm = psAdd(thmm, psMul(wCurr, thr[b]))
		ennOut[sb] = enn
		thmOut[sb] = thmm
		enn = psMul(wNext, eb[b])
		thmm = psMul(wNext, thr[b])
	}
	// zero initialize the rest
	for ; sb < n; sb++ {
		ennOut[sb] = 0
		thmOut[sb] = 0
	}
}

// convertPartition2scalefacS is LAME's convert_partition2scalefac_s
// (psymodel.c:405): short-block partition→scalefac for one sub-block.
func (pm *LameInternalFlags) convertPartition2scalefacS(eb, thr []float32, chn, sblock int) {
	psv := &pm.SvPsy
	gds := &pm.CdPsy.S
	var enn, thm [SBMAXs]float32
	convertPartition2scalefac(gds, eb, thr, enn[:], thm[:])
	for sb := 0; sb < SBMAXs; sb++ {
		psv.En[chn].S[sb][sblock] = enn[sb]
		psv.Thm[chn].S[sb][sblock] = thm[sb]
	}
}

// convertPartition2scalefacL is LAME's convert_partition2scalefac_l
// (psymodel.c:421): long-block partition→scalefac.
func (pm *LameInternalFlags) convertPartition2scalefacL(eb, thr []float32, chn int) {
	psv := &pm.SvPsy
	gdl := &pm.CdPsy.L
	convertPartition2scalefac(gdl, eb, thr, psv.En[chn].L[:], psv.Thm[chn].L[:])
}

// convertPartition2scalefacLToS is LAME's convert_partition2scalefac_l_to_s
// (psymodel.c:431): derives short-block en/thm from the long-block mapping
// (used when a forced short-block calc is needed but long was computed).
func (pm *LameInternalFlags) convertPartition2scalefacLToS(eb, thr []float32, chn int) {
	psv := &pm.SvPsy
	gds := &pm.CdPsy.LToS
	var enn, thm [SBMAXs]float32
	convertPartition2scalefac(gds, eb, thr, enn[:], thm[:])
	for sb := 0; sb < SBMAXs; sb++ {
		const scale = float32(1.0 / 64.0)
		tmpEnn := enn[sb]
		tmpThm := psMul(thm[sb], scale)
		for sblock := 0; sblock < 3; sblock++ {
			psv.En[chn].S[sb][sblock] = tmpEnn
			psv.Thm[chn].S[sb][sblock] = tmpThm
		}
	}
}

// nsInterp is LAME's NS_INTERP (psymodel.c:453): pow(x/y,r)*y with the r>=1
// and r<=0 fast exits. powf matches the C single-precision power.
func nsInterp(x, y, r float32) float32 {
	if r >= 1.0 {
		return x // 99.7% of the time
	}
	if r <= 0.0 {
		return y
	}
	if y > 0.0 {
		return psMul(powf(psDiv(x, y), r), y) // rest of the time
	}
	return 0.0 // never happens
}

// ── perceptual entropy (psymodel.c:468) ────────────────────────────────────

// regcoefS is pecalc_s's regcoef_s[] (psymodel.c:472).
var regcoefS = [...]float32{
	11.8, 13.6, 17.2, 32, 46.5, 51.3, 57.5, 67.1, 71.5, 84.6, 97.6, 130,
}

// pecalcS is LAME's pecalc_s (psymodel.c:468): short-block perceptual entropy.
//
// FP NOTE: the accumulation `pe_s += regcoef_s[sb] * FAST_LOG10(en/x)` mixes
// precisions. en/x and x*1e10f are FLOAT, but FAST_LOG10 is the DOUBLE log10
// (non-USE_FAST_LOG) and 10.0f*LOG10 is double, so `regcoef * (…)` is a double
// product; the `pe_s +=` then widens the FLOAT pe_s, adds in double, and narrows
// back to FLOAT. Computing the product/accumulate in float32 diverges pe by a
// ULP (psymodel.c:468-506).
func pecalcS(mr *III_psy_ratio, maskingLower float32) float32 {
	peS := float32(1236.28 / 4)
	for sb := 0; sb < SBMAXs-1; sb++ {
		for sblock := 0; sblock < 3; sblock++ {
			thm := mr.Thm.S[sb][sblock]
			if thm > 0.0 {
				x := psMul(thm, maskingLower)
				en := mr.En.S[sb][sblock]
				if en > x {
					if en > psMul(x, 1e10) {
						// 10.0f * LOG10: 10.0f promotes to double, LOG10 is double.
						peS = float32(float64(peS) + float64(regcoefS[sb])*(10.0*log10Const))
					} else {
						peS = float32(float64(peS) + float64(regcoefS[sb])*math.Log10(float64(psDiv(en, x))))
					}
				}
			}
		}
	}
	return peS
}

// regcoefL is pecalc_l's regcoef_l[] (psymodel.c:517).
var regcoefL = [...]float32{
	6.8, 5.8, 5.8, 6.4, 6.5, 9.9, 12.1, 14.4, 15, 18.9, 21.6, 26.9,
	34.2, 40.2, 46.8, 56.5, 60.7, 73.9, 85.7, 93.4, 126.1,
}

// pecalcL is LAME's pecalc_l (psymodel.c:513): long-block perceptual entropy.
// Same mixed-precision accumulation as pecalcS (see its FP note): the
// regcoef * FAST_LOG10/10.0f*LOG10 product is double and pe_l += widens/narrows
// through double (psymodel.c:513-560).
func pecalcL(mr *III_psy_ratio, maskingLower float32) float32 {
	peL := float32(1124.23 / 4)
	for sb := 0; sb < SBMAXl-1; sb++ {
		thm := mr.Thm.L[sb]
		if thm > 0.0 {
			x := psMul(thm, maskingLower)
			en := mr.En.L[sb]
			if en > x {
				if en > psMul(x, 1e10) {
					peL = float32(float64(peL) + float64(regcoefL[sb])*(10.0*log10Const))
				} else {
					peL = float32(float64(peL) + float64(regcoefL[sb])*math.Log10(float64(psDiv(en, x))))
				}
			}
		}
	}
	return peL
}

// ── energy / mask-index per partition (psymodel.c:566) ─────────────────────

// calcEnergy is LAME's calc_energy (psymodel.c:566): sums fftenergy into each
// partition band's total (eb), max line (max), and average (avg).
func calcEnergy(l *PsyConstCB2SB, fftenergy []float32, eb, mx, avg []float32) {
	j := 0
	for b := 0; b < l.Npart; b++ {
		var ebb, m float32
		for i := 0; i < l.Numlines[b]; i++ {
			el := fftenergy[j]
			ebb = psAdd(ebb, el)
			if m < el {
				m = el
			}
			j++
		}
		eb[b] = ebb
		mx[b] = m
		avg[b] = psMul(ebb, l.Rnumlines[b])
	}
}

// calcMaskIndexL is LAME's calc_mask_index_l (psymodel.c:593): the long-block
// tonality mask index per partition band, written into maskIdx.
func (pm *LameInternalFlags) calcMaskIndexL(mx, avg []float32, maskIdx []byte) {
	gdl := &pm.CdPsy.L
	var m, a float32
	lastTabEntry := len(maskTab) - 1
	b := 0
	a = psAdd(avg[b], avg[b+1])
	if a > 0.0 {
		m = mx[b]
		if m < mx[b+1] {
			m = mx[b+1]
		}
		a = psDiv(psMul(20.0, psSub(psMul(m, 2.0), a)),
			psMul(a, float32(gdl.Numlines[b]+gdl.Numlines[b+1]-1)))
		k := int(a)
		if k > lastTabEntry {
			k = lastTabEntry
		}
		maskIdx[b] = byte(k)
	} else {
		maskIdx[b] = 0
	}

	for b = 1; b < gdl.Npart-1; b++ {
		a = psAdd(psAdd(avg[b-1], avg[b]), avg[b+1])
		if a > 0.0 {
			m = mx[b-1]
			if m < mx[b] {
				m = mx[b]
			}
			if m < mx[b+1] {
				m = mx[b+1]
			}
			a = psDiv(psMul(20.0, psSub(psMul(m, 3.0), a)),
				psMul(a, float32(gdl.Numlines[b-1]+gdl.Numlines[b]+gdl.Numlines[b+1]-1)))
			k := int(a)
			if k > lastTabEntry {
				k = lastTabEntry
			}
			maskIdx[b] = byte(k)
		} else {
			maskIdx[b] = 0
		}
	}

	a = psAdd(avg[b-1], avg[b])
	if a > 0.0 {
		m = mx[b-1]
		if m < mx[b] {
			m = mx[b]
		}
		a = psDiv(psMul(20.0, psSub(psMul(m, 2.0), a)),
			psMul(a, float32(gdl.Numlines[b-1]+gdl.Numlines[b]-1)))
		k := int(a)
		if k > lastTabEntry {
			k = lastTabEntry
		}
		maskIdx[b] = byte(k)
	} else {
		maskIdx[b] = 0
	}
}

// ── FFT energy computation (psymodel.c:665) ────────────────────────────────

// vbrpsyComputeFftL is LAME's vbrpsy_compute_fft_l (psymodel.c:665): runs the
// long FFT (or derives M/S from L/R), then squares into fftenergy and totals
// the channel energy. wsampL is the two long-block FFT buffers wsamp_L[2]; the
// C passes &wsamp_L[ch01], i.e. a pointer to one [BLKSIZE] row, but for chn==2
// (mid/side derivation) it reaches both rows, so this takes the whole pair and
// the channel-local view index.
func (pm *LameInternalFlags) vbrpsyComputeFftL(buffer [2][]float32, chn, grOut int, fftenergy []float32, wsampL *[2][BLKSIZE]float32, view int) {
	psv := &pm.SvPsy

	if chn < 2 {
		pm.fftLong(&wsampL[view], chn, buffer)
	} else if chn == 2 {
		sqrt2Half := psMul(float32(sqrt2Const), 0.5)
		// FFT data for mid and side channel is derived from L & R
		for j := BLKSIZE - 1; j >= 0; j-- {
			l := wsampL[0][j]
			r := wsampL[1][j]
			wsampL[0][j] = psMul(psAdd(l, r), sqrt2Half)
			wsampL[1][j] = psMul(psSub(l, r), sqrt2Half)
		}
	}

	// compute energies
	fftenergy[0] = wsampL[view][0]
	fftenergy[0] = psMul(fftenergy[0], fftenergy[0])

	for j := BLKSIZE/2 - 1; j >= 0; j-- {
		re := wsampL[view][BLKSIZE/2-j]
		im := wsampL[view][BLKSIZE/2+j]
		fftenergy[BLKSIZE/2-j] = psMul(psAdd(psMul(re, re), psMul(im, im)), 0.5)
	}
	// total energy
	{
		var totalenergy float32
		for j := 11; j < HBLKSIZE; j++ {
			totalenergy = psAdd(totalenergy, fftenergy[j])
		}
		psv.TotEner[chn] = totalenergy
	}
}

// vbrpsyComputeFftS is LAME's vbrpsy_compute_fft_s (psymodel.c:717): runs the
// short FFT for sub-block 0 (or derives M/S), then squares into fftenergyS.
// wsampS is the short-block FFT buffer pair wsamp_S[2]; view selects the
// channel-local row.
func (pm *LameInternalFlags) vbrpsyComputeFftS(buffer [2][]float32, chn, sblock int, fftenergyS *[3][HBLKSIZEs]float32, wsampS *[2][3][BLKSIZEs]float32, view int) {
	if sblock == 0 && chn < 2 {
		pm.fftShort(&wsampS[view], chn, buffer)
	}
	if chn == 2 {
		sqrt2Half := psMul(float32(sqrt2Const), 0.5)
		// FFT data for mid and side channel is derived from L & R
		for j := BLKSIZEs - 1; j >= 0; j-- {
			l := wsampS[0][sblock][j]
			r := wsampS[1][sblock][j]
			wsampS[0][sblock][j] = psMul(psAdd(l, r), sqrt2Half)
			wsampS[1][sblock][j] = psMul(psSub(l, r), sqrt2Half)
		}
	}

	// compute energies
	fftenergyS[sblock][0] = wsampS[view][sblock][0]
	fftenergyS[sblock][0] = psMul(fftenergyS[sblock][0], fftenergyS[sblock][0])
	for j := BLKSIZEs/2 - 1; j >= 0; j-- {
		re := wsampS[view][sblock][BLKSIZEs/2-j]
		im := wsampS[view][sblock][BLKSIZEs/2+j]
		fftenergyS[sblock][BLKSIZEs/2-j] = psMul(psAdd(psMul(re, re), psMul(im, im)), 0.5)
	}
}

// psychoLoudnessApprox is LAME's psycho_loudness_approx (psymodel.c:215):
// weighted sum of BLKSIZE/2 energies scaled by VO_SCALE.
func psychoLoudnessApprox(energy, eqlW []float32) float32 {
	var loudnessPower float32
	for i := 0; i < BLKSIZE/2; i++ {
		loudnessPower = psAdd(loudnessPower, psMul(energy[i], eqlW[i]))
	}
	loudnessPower = psMul(loudnessPower, float32(VOScale))
	return loudnessPower
}

// vbrpsyComputeLoudnessApproximationL is LAME's
// vbrpsy_compute_loudness_approximation_l (psymodel.c:753): one-granule-delayed
// loudness^2, skipped for mid/side channels.
func (pm *LameInternalFlags) vbrpsyComputeLoudnessApproximationL(grOut, chn int, fftenergy []float32) {
	psv := &pm.SvPsy
	if chn < 2 { // no loudness for mid/side ch
		pm.OvPsy.LoudnessSq[grOut][chn] = psv.LoudnessSqSave[chn]
		psv.LoudnessSqSave[chn] = psychoLoudnessApprox(fftenergy, pm.ATH.EqlW[:])
	}
}

// ── short-block masking (psymodel.c:953) ───────────────────────────────────

// vbrpsySkipMaskingS is LAME's vbrpsy_skip_masking_s (psymodel.c:953): copies
// nb_s1 into nb_s2 when a long block is used (sblock 0 only).
func (pm *LameInternalFlags) vbrpsySkipMaskingS(chn, sblock int) {
	if sblock == 0 {
		nbs2 := pm.SvPsy.NbS2[chn][:]
		nbs1 := pm.SvPsy.NbS1[chn][:]
		n := pm.CdPsy.S.Npart
		for b := 0; b < n; b++ {
			nbs2[b] = nbs1[b]
		}
	}
}

// vbrpsyCalcMaskIndexS is LAME's vbrpsy_calc_mask_index_s (psymodel.c:968):
// the short-block tonality mask index per partition band.
func (pm *LameInternalFlags) vbrpsyCalcMaskIndexS(mx, avg []float32, maskIdx []byte) {
	gds := &pm.CdPsy.S
	var m, a float32
	lastTabEntry := len(maskTab) - 1
	b := 0
	a = psAdd(avg[b], avg[b+1])
	if a > 0.0 {
		m = mx[b]
		if m < mx[b+1] {
			m = mx[b+1]
		}
		a = psDiv(psMul(20.0, psSub(psMul(m, 2.0), a)),
			psMul(a, float32(gds.Numlines[b]+gds.Numlines[b+1]-1)))
		k := int(a)
		if k > lastTabEntry {
			k = lastTabEntry
		}
		maskIdx[b] = byte(k)
	} else {
		maskIdx[b] = 0
	}

	for b = 1; b < gds.Npart-1; b++ {
		a = psAdd(psAdd(avg[b-1], avg[b]), avg[b+1])
		if a > 0.0 {
			m = mx[b-1]
			if m < mx[b] {
				m = mx[b]
			}
			if m < mx[b+1] {
				m = mx[b+1]
			}
			a = psDiv(psMul(20.0, psSub(psMul(m, 3.0), a)),
				psMul(a, float32(gds.Numlines[b-1]+gds.Numlines[b]+gds.Numlines[b+1]-1)))
			k := int(a)
			if k > lastTabEntry {
				k = lastTabEntry
			}
			maskIdx[b] = byte(k)
		} else {
			maskIdx[b] = 0
		}
	}

	a = psAdd(avg[b-1], avg[b])
	if a > 0.0 {
		m = mx[b-1]
		if m < mx[b] {
			m = mx[b]
		}
		a = psDiv(psMul(20.0, psSub(psMul(m, 2.0), a)),
			psMul(a, float32(gds.Numlines[b-1]+gds.Numlines[b]-1)))
		k := int(a)
		if k > lastTabEntry {
			k = lastTabEntry
		}
		maskIdx[b] = byte(k)
	} else {
		maskIdx[b] = 0
	}
}

// vbrpsyComputeMaskingS is LAME's vbrpsy_compute_masking_s (psymodel.c:1041):
// short-block energy/threshold with the spreading convolution and threshold
// limiting; updates nb_s1/nb_s2.
func (pm *LameInternalFlags) vbrpsyComputeMaskingS(fftenergyS *[3][HBLKSIZEs]float32, eb, thr []float32, chn, sblock int) {
	psv := &pm.SvPsy
	gds := &pm.CdPsy.S
	var mx, avg [PsyCBANDS]float32
	var maskIdxS [PsyCBANDS]byte

	j := 0
	for b := 0; b < gds.Npart; b++ {
		var ebb, m float32
		n := gds.Numlines[b]
		for i := 0; i < n; i++ {
			el := fftenergyS[sblock][j]
			ebb = psAdd(ebb, el)
			if m < el {
				m = el
			}
			j++
		}
		eb[b] = ebb
		mx[b] = m
		avg[b] = psMul(ebb, gds.Rnumlines[b])
	}
	pm.vbrpsyCalcMaskIndexS(mx[:], avg[:], maskIdxS[:])
	j = 0
	for b := 0; b < gds.Npart; b++ {
		kk := gds.S3ind[b][0]
		last := gds.S3ind[b][1]
		delta := maskAddDelta(int(maskIdxS[b]))
		var x, ecb, avgMask float32
		maskingLower := psMul(gds.MaskingLower[b], pm.SvQnt.MaskingLower)

		dd := int(maskIdxS[kk])
		ddN := 1
		ecb = psMul(psMul(gds.S3[j], eb[kk]), maskTab[maskIdxS[kk]])
		j++
		kk++
		for kk <= last {
			dd += int(maskIdxS[kk])
			ddN++
			x = psMul(psMul(gds.S3[j], eb[kk]), maskTab[maskIdxS[kk]])
			ecb = vbrpsyMaskAdd(ecb, x, kk-b, delta)
			j++
			kk++
		}
		dd = (1 + 2*dd) / (2 * ddN)
		avgMask = psMul(maskTab[dd], 0.5)
		ecb = psMul(ecb, avgMask)
		// (the pre-echo control variant here is #if 0 in C; we do it later)
		thr[b] = ecb
		psv.NbS2[chn][b] = psv.NbS1[chn][b]
		psv.NbS1[chn][b] = ecb
		{
			// if THR exceeds EB, limit THR (tonaltest.wav distortion guard)
			x = mx[b]
			x = psMul(x, gds.Minval[b])
			x = psMul(x, avgMask)
			if thr[b] > x {
				thr[b] = x
			}
		}
		if maskingLower > 1 {
			thr[b] = psMul(thr[b], maskingLower)
		}
		if thr[b] > eb[b] {
			thr[b] = eb[b]
		}
		if maskingLower < 1 {
			thr[b] = psMul(thr[b], maskingLower)
		}
	}
	for b := gds.Npart; b < PsyCBANDS; b++ {
		eb[b] = 0
		thr[b] = 0
	}
}

// ── long-block masking (psymodel.c:1144) ───────────────────────────────────

// vbrpsyComputeMaskingL is LAME's vbrpsy_compute_masking_l (psymodel.c:1144):
// long-block energy/threshold with the spreading convolution, long-block
// pre-echo control, and threshold limiting; updates nb_l1/nb_l2.
func (pm *LameInternalFlags) vbrpsyComputeMaskingL(fftenergy, ebL, thr []float32, chn int) {
	psv := &pm.SvPsy
	gdl := &pm.CdPsy.L
	var mx, avg [PsyCBANDS]float32
	var maskIdxL [PsyCBANDS + 2]byte

	calcEnergy(gdl, fftenergy, ebL, mx[:], avg[:])
	pm.calcMaskIndexL(mx[:], avg[:], maskIdxL[:])

	k := 0
	for b := 0; b < gdl.Npart; b++ {
		var x, ecb, avgMask, t float32
		maskingLower := psMul(gdl.MaskingLower[b], pm.SvQnt.MaskingLower)
		kk := gdl.S3ind[b][0]
		last := gdl.S3ind[b][1]
		delta := maskAddDelta(int(maskIdxL[b]))
		dd := 0
		ddN := 0

		dd = int(maskIdxL[kk])
		ddN++
		ecb = psMul(psMul(gdl.S3[k], ebL[kk]), maskTab[maskIdxL[kk]])
		k++
		kk++
		for kk <= last {
			dd += int(maskIdxL[kk])
			ddN++
			x = psMul(psMul(gdl.S3[k], ebL[kk]), maskTab[maskIdxL[kk]])
			t = vbrpsyMaskAdd(ecb, x, kk-b, delta)
			ecb = t
			k++
			kk++
		}
		dd = (1 + 2*dd) / (2 * ddN)
		avgMask = psMul(maskTab[dd], 0.5)
		ecb = psMul(ecb, avgMask)

		// long block pre-echo control
		if psv.BlocktypeOld[chn&0x01] == ShortType {
			ecbLimit := psMul(float32(Rpelev), psv.NbL1[chn][b])
			if ecbLimit > 0 {
				thr[b] = minF32(ecb, ecbLimit)
			} else {
				thr[b] = minF32(ecb, psMul(ebL[b], float32(NsPreechoAtt2)))
			}
		} else {
			ecbLimit2 := psMul(float32(Rpelev2), psv.NbL2[chn][b])
			ecbLimit1 := psMul(float32(Rpelev), psv.NbL1[chn][b])
			var ecbLimit float32
			if ecbLimit2 <= 0 {
				ecbLimit2 = ecb
			}
			if ecbLimit1 <= 0 {
				ecbLimit1 = ecb
			}
			if psv.BlocktypeOld[chn&0x01] == NormType {
				ecbLimit = minF32(ecbLimit1, ecbLimit2)
			} else {
				ecbLimit = ecbLimit1
			}
			thr[b] = minF32(ecb, ecbLimit)
		}
		psv.NbL2[chn][b] = psv.NbL1[chn][b]
		psv.NbL1[chn][b] = ecb
		{
			x = mx[b]
			x = psMul(x, gdl.Minval[b])
			x = psMul(x, avgMask)
			if thr[b] > x {
				thr[b] = x
			}
		}
		if maskingLower > 1 {
			thr[b] = psMul(thr[b], maskingLower)
		}
		if thr[b] > ebL[b] {
			thr[b] = ebL[b]
		}
		if maskingLower < 1 {
			thr[b] = psMul(thr[b], maskingLower)
		}
	}
	for b := gdl.Npart; b < PsyCBANDS; b++ {
		ebL[b] = 0
		thr[b] = 0
	}
}

// ── block type & M/S thresholds (psymodel.c:1275) ──────────────────────────

// vbrpsyComputeBlockType is LAME's vbrpsy_compute_block_type (psymodel.c:1275):
// resolves uselongblock against the short_blocks coupling/dispensed/forced
// policy.
func (cfg *SessionConfig) vbrpsyComputeBlockType(uselongblock []int) {
	if cfg.ShortBlocks == shortBlockCoupled && !(uselongblock[0] != 0 && uselongblock[1] != 0) {
		uselongblock[0], uselongblock[1] = 0, 0
	}
	for chn := 0; chn < cfg.ChannelsOut; chn++ {
		if cfg.ShortBlocks == shortBlockDispensed {
			uselongblock[chn] = 1
		}
		if cfg.ShortBlocks == shortBlockForced {
			uselongblock[chn] = 0
		}
	}
}

// vbrpsyApplyBlockType is LAME's vbrpsy_apply_block_type (psymodel.c:1299):
// finalises the previous granule's block type from this granule's uselongblock
// decision and rotates blocktype_old.
func (psv *PsyStateVar) vbrpsyApplyBlockType(nch int, uselongblock, blocktypeD []int) {
	for chn := 0; chn < nch; chn++ {
		blocktype := NormType
		if uselongblock[chn] != 0 {
			// no attack : use long blocks
			if psv.BlocktypeOld[chn] == ShortType {
				blocktype = StopType
			}
		} else {
			// attack : use short blocks
			blocktype = ShortType
			if psv.BlocktypeOld[chn] == NormType {
				psv.BlocktypeOld[chn] = StartType
			}
			if psv.BlocktypeOld[chn] == StopType {
				psv.BlocktypeOld[chn] = ShortType
			}
		}
		blocktypeD[chn] = psv.BlocktypeOld[chn] // value returned to calling program
		psv.BlocktypeOld[chn] = blocktype       // save for next call
	}
}

// vbrpsyComputeMSThresholds is LAME's vbrpsy_compute_MS_thresholds
// (psymodel.c:1336): Johnston & Ferreira inter-channel masking, optionally
// applying the user msfix.
func vbrpsyComputeMSThresholds(eb *[4][PsyCBANDS]float32, thr *[4][PsyCBANDS]float32, cbMld, athCb []float32, athlower, msfix float32, n int) {
	msfix2 := psMul(msfix, 2.0)
	var rside, rmid float32
	for b := 0; b < n; b++ {
		ebM := eb[2][b]
		ebS := eb[3][b]
		thmL := thr[0][b]
		thmR := thr[1][b]
		thmM := thr[2][b]
		thmS := thr[3][b]

		if thmL <= psMul(1.58, thmR) && thmR <= psMul(1.58, thmL) {
			mldM := psMul(cbMld[b], ebS)
			mldS := psMul(cbMld[b], ebM)
			tmpM := minF32(thmS, mldM)
			tmpS := minF32(thmM, mldS)
			rmid = maxF32(thmM, tmpM)
			rside = maxF32(thmS, tmpS)
		} else {
			rmid = thmM
			rside = thmS
		}
		if msfix > 0.0 {
			// Adjust M/S maskings if user set "msfix" (Naoki Shibata 2000)
			var thmLR, thmMS float32
			ath := psMul(athCb[b], athlower)
			tmpL := maxF32(thmL, ath)
			tmpR := maxF32(thmR, ath)
			thmLR = minF32(tmpL, tmpR)
			thmM = maxF32(rmid, ath)
			thmS = maxF32(rside, ath)
			thmMS = psAdd(thmM, thmS)
			if thmMS > 0.0 && psMul(thmLR, msfix2) < thmMS {
				f := psDiv(psMul(thmLR, msfix2), thmMS)
				thmM = psMul(thmM, f)
				thmS = psMul(thmS, f)
			}
			rmid = minF32(thmM, rmid)
			rside = minF32(thmS, rside)
		}
		if rmid > ebM {
			rmid = ebM
		}
		if rside > ebS {
			rside = ebS
		}
		thr[2][b] = rmid
		thr[3][b] = rside
	}
}

// ── small integer / math shims ─────────────────────────────────────────────

// absInt is C's abs() for int (used by vbrpsy_mask_add's abs(b)).
func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// powf is C's powf (single-precision power); LAME's NS_INTERP and init use it.
// Computed via the double kernel narrowed to float32 to match the oracle's
// powf #define and be platform-portable.
func powf(x, y float32) float32 {
	return float32(math.Pow(float64(x), float64(y)))
}

// fabsF32 is C's fabs() applied to a FLOAT operand (attack detection).
func fabsF32(x float32) float32 {
	return float32(math.Abs(float64(x)))
}
