// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

import "math"

// 1:1 Go translation of LAME 3.100's psychoacoustic-model constant setup
// (psymodel.c:1615-2167): the spreading function s3_func, stereo_demask,
// init_numline / compute_bark_values / init_s3_values, and the psymodel_init
// driver that computes every session-constant the per-frame analysis reads
// (partition layout, spreading convolution, ATH, MINVAL, masking_lower,
// windows, equal-loudness weights, attack thresholds). FLOAT is float32; this
// one-time path uses LAME's double-precision pow/exp/sqrt/log/atan exactly as
// the C, narrowing to float32 on store — it is not on the FMA-sensitive
// per-frame path.

// s3Func is LAME's s3_func (psymodel.c:1615): the spreading function, returned
// in units of energy and normalised so s3 o 1 = 1.
func s3Func(bark float32) float32 {
	var tempx, x, tempy, temp float32
	tempx = bark
	if tempx >= 0 {
		tempx *= 3 // 3 is an int literal -> FLOAT product (exact), float32 matches
	} else {
		// C: tempx *= 1.5 where 1.5 is a DOUBLE literal, so tempx promotes to
		// double, multiplies, and narrows to FLOAT. A float32 *= 1.5 diverges by a
		// ULP for some bark values (psymodel.c:1623).
		tempx = float32(float64(tempx) * 1.5)
	}

	if tempx >= 0.5 && tempx <= 2.5 {
		// C: temp = tempx - 0.5 (0.5 is a double literal -> tempx promotes to
		// double, narrowed to FLOAT temp); x = 8.0 * (temp*temp - 2.0*temp)
		// where temp*temp is a FLOAT product (single-rounded) but 2.0/8.0 are
		// double literals, so the subtraction and outer scale evaluate in DOUBLE
		// before narrowing to FLOAT x. Doing the whole thing in float32 diverges
		// by a ULP, which surfaces as a 1-2 ULP drift in the spread masking
		// threshold wherever real energy lands (psymodel.c:1626-1627).
		temp = float32(float64(tempx) - 0.5)
		x = float32(8.0 * (float64(temp*temp) - 2.0*float64(temp)))
	} else {
		x = 0.0
	}
	// C: tempx += 0.474 where 0.474 is a DOUBLE literal -> tempx promotes to
	// double, adds, narrows to FLOAT. A float32 += 0.474 diverges by a ULP
	// (psymodel.c:1631).
	tempx = float32(float64(tempx) + 0.474)
	// C: 15.811389 + 7.5*tempx - 17.5*sqrt(1.0 + tempx*tempx). The double
	// literals promote tempx to double for the +/-/* and the sqrt is double, BUT
	// `tempx * tempx` is a FLOAT product (both operands FLOAT) — single-rounded to
	// float32 — before the `1.0 +` promotes it to double. Computing tempx*tempx in
	// double (float64(tempx)*float64(tempx)) drops that rounding and shifts tempy
	// by a ULP, feeding the s3 spreading drift (psymodel.c:1632).
	tempy = float32(15.811389 + 7.5*float64(tempx) - 17.5*math.Sqrt(1.0+float64(psMul(tempx, tempx))))

	if tempy <= -60.0 {
		return 0.0
	}

	// C: exp((x + tempy) * LN_TO_LOG10). x and tempy are both FLOAT, so `x +
	// tempy` is a FLOAT (single-rounded float32) sum FIRST, then promoted to
	// double for the * LN_TO_LOG10. Adding in double (float64(x)+float64(tempy))
	// skips that float32 rounding and diverges the spreading function s3 by up to
	// hundreds of ULP, which is the dominant source of the multi-granule masking
	// threshold drift (psymodel.c:1637).
	tempx = float32(math.Exp(float64(psAdd(x, tempy)) * lnToLog10))

	// Normalization: s3 should integrate to 1 over bark. C: tempx /= .6609193,
	// where .6609193 is a DOUBLE literal, so the FLOAT tempx promotes to double,
	// divides, and narrows back to FLOAT. A float32 divide by a float32 0.6609193
	// is off by a ULP (psymodel.c:1646).
	tempx = float32(float64(tempx) / 0.6609193)
	return tempx
}

// stereoDemask is LAME's stereo_demask (psymodel.c:1700): the stereo-demasking
// threshold setup, reverse-engineered from the Johnston/Ferreira plot.
func stereoDemask(f float64) float32 {
	arg := float64(freq2bark(float32(f)))
	arg = math.Min(arg, 15.5) / 15.5
	return float32(math.Pow(10.0, 1.25*(1-math.Cos(piConst*arg))-2.5))
}

// initNumline is LAME's init_numline (psymodel.c:1711): computes numlines, bo,
// bm, bo_weight, mld, mld_cb for one partition→scalefactor mapping gd.
func initNumline(gd *PsyConstCB2SB, sfreq float32, fftSize, mdctSize, sbmax int, scalepos []int) {
	var bFrq [PsyCBANDS + 1]float32
	mdctFreqFrac := sfreq / (2.0 * float32(mdctSize))
	deltafreq := float32(fftSize) / (2.0 * float32(mdctSize))
	var partition [HBLKSIZE]int
	sfreq /= float32(fftSize)
	j := 0
	ni := 0
	// compute numlines, the number of spectral lines in each partition band.
	// each partition band should be about DELBARK wide.
	var i int
	for i = 0; i < PsyCBANDS; i++ {
		var bark1 float32
		var j2, nl int
		bark1 = freq2bark(sfreq * float32(j))

		bFrq[i] = sfreq * float32(j)

		for j2 = j; freq2bark(sfreq*float32(j2))-bark1 < DELBARK && j2 <= fftSize/2; j2++ {
		}

		nl = j2 - j
		gd.Numlines[i] = nl
		if nl > 0 {
			gd.Rnumlines[i] = 1.0 / float32(nl)
		} else {
			gd.Rnumlines[i] = 0
		}

		ni = i + 1

		for j < j2 {
			partition[j] = i
			j++
		}
		if j > fftSize/2 {
			j = fftSize / 2
			i++
			break
		}
	}
	bFrq[i] = sfreq * float32(j)

	gd.NSb = sbmax
	gd.Npart = ni

	{
		j = 0
		for i = 0; i < gd.Npart; i++ {
			nl := gd.Numlines[i]
			freq := sfreq * float32(j+nl/2)
			gd.MldCb[i] = stereoDemask(float64(freq))
			j += nl
		}
		for ; i < PsyCBANDS; i++ {
			gd.MldCb[i] = 1
		}
	}
	for sfb := 0; sfb < sbmax; sfb++ {
		var i1, i2, bo int
		start := scalepos[sfb]
		end := scalepos[sfb+1]

		i1 = int(math.Floor(0.5 + float64(deltafreq)*(float64(start)-0.5)))
		if i1 < 0 {
			i1 = 0
		}
		i2 = int(math.Floor(0.5 + float64(deltafreq)*(float64(end)-0.5)))

		if i2 > fftSize/2 {
			i2 = fftSize / 2
		}

		bo = partition[i2]
		gd.Bm[sfb] = (partition[i1] + partition[i2]) / 2
		gd.Bo[sfb] = bo

		// how much of this band belongs to the current scalefactor band.
		// C (psymodel.c:1788-1789): f_tmp = mdct_freq_frac * end; bo_w = (f_tmp -
		// b_frq[bo]) / (b_frq[bo+1] - b_frq[bo]). The oracle is -ffp-contract=off,
		// so f_tmp is rounded to FLOAT before the subtraction. Go's default backend
		// can fuse `mdct_freq_frac*end - b_frq[bo]` into a single-rounded FMA,
		// skipping that intermediate rounding and shifting bo_w by several ULP
		// (visible at 48k/32k where the band geometry differs from 44.1k). Route the
		// products/sums through the strict ps* helpers, which are the FMA barriers.
		{
			fTmp := psMul(mdctFreqFrac, float32(end))
			boW := psDiv(psSub(fTmp, bFrq[bo]), psSub(bFrq[bo+1], bFrq[bo]))
			if boW < 0 {
				boW = 0
			} else if boW > 1 {
				boW = 1
			}
			gd.BoWeight[sfb] = boW
		}
		gd.Mld[sfb] = stereoDemask(float64(psMul(mdctFreqFrac, float32(start))))
	}
}

// computeBarkValues is LAME's compute_bark_values (psymodel.c:1804): bark value
// and width of each critical band.
func computeBarkValues(gd *PsyConstCB2SB, sfreq float32, fftSize int, bval, bvalWidth []float32) {
	j := 0
	ni := gd.Npart
	sfreq /= float32(fftSize)
	for k := 0; k < ni; k++ {
		w := gd.Numlines[k]
		var bark1, bark2 float32

		bark1 = freq2bark(sfreq * float32(j))
		bark2 = freq2bark(sfreq * float32(j+w-1))
		bval[k] = 0.5 * (bark1 + bark2)

		bark1 = freq2bark(sfreq * (float32(j) - 0.5))
		bark2 = freq2bark(sfreq * (float32(j+w) - 0.5))
		bvalWidth[k] = bark2 - bark1
		j += w
	}
}

// initS3Values is LAME's init_s3_values (psymodel.c:1826): builds the
// non-linear-in-bark spreading matrix s3[i][j], records the non-zero column
// range per row in s3ind, and packs the non-zero entries into gd.S3.
func initS3Values(s3Out *[]float32, s3ind *[PsyCBANDS][2]int, npart int, bval, bvalWidth, norm []float32) int {
	var s3 [PsyCBANDS][PsyCBANDS]float32

	// s[i][j]: spreading function centered at band j (masker) for band i (maskee).
	for i := 0; i < npart; i++ {
		for j := 0; j < npart; j++ {
			v := s3Func(bval[i]-bval[j]) * bvalWidth[j]
			s3[i][j] = v * norm[i]
		}
	}
	numberOfNoneZero := 0
	for i := 0; i < npart; i++ {
		var j int
		for j = 0; j < npart; j++ {
			if s3[i][j] > 0.0 {
				break
			}
		}
		s3ind[i][0] = j

		for j = npart - 1; j > 0; j-- {
			if s3[i][j] > 0.0 {
				break
			}
		}
		s3ind[i][1] = j
		numberOfNoneZero += s3ind[i][1] - s3ind[i][0] + 1
	}
	p := make([]float32, numberOfNoneZero)

	k := 0
	for i := 0; i < npart; i++ {
		for j := s3ind[i][0]; j <= s3ind[i][1]; j++ {
			p[k] = s3[i][j]
			k++
		}
	}
	*s3Out = p
	return 0
}

// vbrQSk is psymodel_init's static sk[] VBR-quality masking-lower seed table
// (psymodel.c:2139).
var vbrQSk = [...]float32{
	-7.4, -7.4, -7.4, -9.5, -7.4, -6.1, -5.5, -4.7, -4.7, -4.7, -4.7,
}

// InitPsyModel is LAME's psymodel_init (psymodel.c:1877). It allocates and
// fills pm.CdPsy (PsyConst_t) and seeds pm.SvPsy / pm.ATH, from the SessionConfig
// (pm.Cfg), scalefactor-band tables (pm.ScalefacBand) and the global-flags
// subset gfp. pm.ATH must be non-nil. Returns 0 on success (as the C does).
func (pm *LameInternalFlags) InitPsyModel(gfp *PsyInitParams) int {
	cfg := &pm.Cfg
	psv := &pm.SvPsy

	bvlA, bvlB := float32(13), float32(24)
	snrLA, snrLB := float32(0), float32(0)
	snrSA, snrSB := float32(-8.25), float32(-4.5)

	var bval, bvalWidth, norm [PsyCBANDS]float32
	sfreq := float32(cfg.SamplerateOut)

	xav, xbv := float32(10), float32(12)
	minvalLow := float32(0) - cfg.Minval

	if pm.CdPsy != nil {
		return 0
	}

	gd := new(PsyConst)
	pm.CdPsy = gd

	gd.ForceShortBlockCalc = gfp.ExperimentalZ

	psv.BlocktypeOld[0], psv.BlocktypeOld[1] = NormType, NormType // vbr header is long blocks

	for i := 0; i < 4; i++ {
		for j := 0; j < PsyCBANDS; j++ {
			psv.NbL1[i][j] = 1e20
			psv.NbL2[i][j] = 1e20
			psv.NbS1[i][j] = 1.0
			psv.NbS2[i][j] = 1.0
		}
		for sb := 0; sb < SBMAXl; sb++ {
			psv.En[i].L[sb] = 1e20
			psv.Thm[i].L[sb] = 1e20
		}
		for j := 0; j < 3; j++ {
			for sb := 0; sb < SBMAXs; sb++ {
				psv.En[i].S[sb][j] = 1e20
				psv.Thm[i].S[sb][j] = 1e20
			}
			psv.LastAttacks[i] = 0
		}
		for j := 0; j < 9; j++ {
			psv.LastEnSubshort[i][j] = 10.0
		}
	}

	// init. for loudness approx.
	psv.LoudnessSqSave[0], psv.LoudnessSqSave[1] = 0.0, 0.0

	// compute numlines, bo, bm, bval, bval_width, mld (long)
	initNumline(&gd.L, sfreq, BLKSIZE, 576, SBMAXl, pm.ScalefacBand.L[:])
	computeBarkValues(&gd.L, sfreq, BLKSIZE, bval[:], bvalWidth[:])

	// compute the spreading function (long)
	for i := 0; i < gd.L.Npart; i++ {
		snr := float64(snrLA)
		if bval[i] >= bvlA {
			// C: snr = snr_l_b*(bval[i]-bvl_a)/(bvl_b-bvl_a) +
			// snr_l_a*(bvl_b-bval[i])/(bvl_b-bvl_a) — all operands are FLOAT, so the
			// whole expression evaluates in float32 and only widens when stored into
			// the double snr (psymodel.c:1948). For long blocks snr_l_a/b are 0 so it
			// is moot, but keep the float32 form for fidelity / to mirror the short path.
			snr = float64(psAdd(
				psDiv(psMul(snrLB, psSub(bval[i], bvlA)), psSub(bvlB, bvlA)),
				psDiv(psMul(snrLA, psSub(bvlB, bval[i])), psSub(bvlB, bvlA))))
		}
		norm[i] = float32(math.Pow(10.0, snr/10.0))
	}
	if rc := initS3Values(&gd.L.S3, &gd.L.S3ind, gd.L.Npart, bval[:], bvalWidth[:], norm[:]); rc != 0 {
		return rc
	}

	// long block specific values, ATH and MINVAL
	j := 0
	for i := 0; i < gd.L.Npart; i++ {
		var x float64
		x = floatMax
		for k := 0; k < gd.L.Numlines[i]; k++ {
			// C (psymodel.c:1965-1972): freq = sfreq*j/(1000.0*BLKSIZE) with the
			// divisor a DOUBLE (1000.0 is double), narrowed to FLOAT freq; level =
			// ATHformula(freq*1000)-20 (FLOAT); level = pow(10.,0.1*level) — the
			// double pow is NARROWED back to FLOAT level here; level *= numlines is a
			// FLOAT multiply. The earlier port kept the pow/multiply in double, which
			// diverges cb_l (esp. at 32k) — match the FLOAT narrowing exactly.
			freq := float32(float64(psMul(sfreq, float32(j))) / (1000.0 * BLKSIZE))
			level := psSub(athFormula(cfg, psMul(freq, 1000)), 20) // scale to FFT units (dB)
			levelE := float32(math.Pow(10.0, 0.1*float64(level)))  // dB -> energy (narrowed)
			levelE = psMul(levelE, float32(gd.L.Numlines[i]))
			if x > float64(levelE) {
				x = float64(levelE)
			}
			j++
		}
		pm.ATH.CbL[i] = float32(x)

		// MINVAL. C: x = 20.0 * (bval[i] / xav - 1.0). xav is FLOAT (=10), so
		// `bval[i] / xav` is a FLOAT division (single-rounded float32) before the
		// `- 1.0` double literal promotes it. Doing the division in double diverges
		// minval by a few ULP (psymodel.c:1983).
		x = 20.0 * (float64(psDiv(bval[i], xav)) - 1.0)
		if x > 6 {
			x = 30
		}
		if x < float64(minvalLow) {
			x = float64(minvalLow)
		}
		if cfg.SamplerateOut < 44000 {
			x = 30
		}
		x -= 8.0
		gd.L.Minval[i] = float32(math.Pow(10.0, x/10.0) * float64(gd.L.Numlines[i]))
	}

	// do the same things for short blocks
	initNumline(&gd.S, sfreq, BLKSIZEs, 192, SBMAXs, pm.ScalefacBand.S[:])
	computeBarkValues(&gd.S, sfreq, BLKSIZEs, bval[:], bvalWidth[:])

	// SNR formula (short)
	j = 0
	for i := 0; i < gd.S.Npart; i++ {
		var x float64
		snr := float64(snrSA)
		if bval[i] >= bvlA {
			// C: snr = snr_s_b*(bval[i]-bvl_a)/(bvl_b-bvl_a) +
			// snr_s_a*(bvl_b-bval[i])/(bvl_b-bvl_a) — all FLOAT operands, so the whole
			// expression is float32 and widens only on the store into double snr.
			// snr_s_a/b are nonzero (-8.25/-4.5), so doing this in double diverges the
			// short-block norm (and thus the s3 spreading) by a couple ULP
			// (psymodel.c:2010).
			snr = float64(psAdd(
				psDiv(psMul(snrSB, psSub(bval[i], bvlA)), psSub(bvlB, bvlA)),
				psDiv(psMul(snrSA, psSub(bvlB, bval[i])), psSub(bvlB, bvlA))))
		}
		norm[i] = float32(math.Pow(10.0, snr/10.0))

		// ATH (same FLOAT-narrowing as the long block above; psymodel.c:2017-2025)
		x = floatMax
		for k := 0; k < gd.S.Numlines[i]; k++ {
			freq := float32(float64(psMul(sfreq, float32(j))) / (1000.0 * BLKSIZEs))
			level := psSub(athFormula(cfg, psMul(freq, 1000)), 20)
			levelE := float32(math.Pow(10.0, 0.1*float64(level)))
			levelE = psMul(levelE, float32(gd.S.Numlines[i]))
			if x > float64(levelE) {
				x = float64(levelE)
			}
			j++
		}
		pm.ATH.CbS[i] = float32(x)

		// MINVAL. C: x = 7.0 * (bval[i] / xbv - 1.0). xbv is FLOAT (=12), so the
		// division is single-rounded float32 before the double `- 1.0` (psymodel.c:2032).
		x = 7.0 * (float64(psDiv(bval[i], xbv)) - 1.0)
		if bval[i] > xbv {
			x *= 1 + math.Log(1+x)*3.1
		}
		if bval[i] < xbv {
			x *= 1 + math.Log(1-x)*2.3
		}
		if x > 6 {
			x = 30
		}
		if x < float64(minvalLow) {
			x = float64(minvalLow)
		}
		if cfg.SamplerateOut < 44000 {
			x = 30
		}
		x -= 8
		gd.S.Minval[i] = float32(math.Pow(10.0, x/10) * float64(gd.S.Numlines[i]))
	}

	if rc := initS3Values(&gd.S.S3, &gd.S.S3ind, gd.S.Npart, bval[:], bvalWidth[:], norm[:]); rc != 0 {
		return rc
	}

	initFFT(gd)

	// setup temporal masking
	gd.Decay = float32(math.Exp(-1.0 * log10Const / (TemporalmaskSustainSec * float64(sfreq) / 192.0)))

	{
		msfix := float32(NsMsfix)
		if cfg.UseSafeJointStereo != 0 {
			msfix = 1.0
		}
		if math.Abs(float64(cfg.Msfix)) > 0.0 {
			msfix = cfg.Msfix
		}
		cfg.Msfix = msfix

		// spread only from npart_l bands
		for b := 0; b < gd.L.Npart; b++ {
			if gd.L.S3ind[b][1] > gd.L.Npart-1 {
				gd.L.S3ind[b][1] = gd.L.Npart - 1
			}
		}
	}

	// ATH auto adjustment: decrease ATH by 12 dB per second
	frameDuration := 576.0 * float64(cfg.ModeGr) / float64(sfreq)
	pm.ATH.Decay = float32(math.Pow(10.0, -12.0/10.0*frameDuration))
	pm.ATH.AdjustFactor = 0.01 // minimum, for leading low loudness
	pm.ATH.AdjustLimit = 1.0   // on lead, allow adjust up to maximum

	if cfg.ATHtype != -1 {
		// compute equal loudness weights (eql_w)
		freqInc := float32(cfg.SamplerateOut) / float32(BLKSIZE)
		var eqlBalance float32
		freq := float32(0.0)
		for i := 0; i < BLKSIZE/2; i++ {
			freq += freqInc
			pm.ATH.EqlW[i] = float32(1.0 / math.Pow(10, float64(athFormula(cfg, freq))/10))
			eqlBalance += pm.ATH.EqlW[i]
		}
		eqlBalance = 1.0 / eqlBalance
		for i := BLKSIZE / 2; ; {
			i--
			if i < 0 {
				break
			}
			pm.ATH.EqlW[i] *= eqlBalance
		}
	}

	// short block attack threshold
	{
		x := gfp.Attackthre
		y := gfp.AttackthreS
		if x < 0 {
			x = NsAttackThre
		}
		if y < 0 {
			y = NsAttackThreS
		}
		gd.AttackThreshold[0], gd.AttackThreshold[1], gd.AttackThreshold[2] = x, x, x
		gd.AttackThreshold[3] = y
	}
	{
		var skS, skL float32 = -10.0, -4.7
		if gfp.VBRq < 4 {
			skL, skS = vbrQSk[0], vbrQSk[0]
		} else {
			v := vbrQSk[gfp.VBRq] + gfp.VBRqFrac*(vbrQSk[gfp.VBRq]-vbrQSk[gfp.VBRq+1])
			skL, skS = v, v
		}
		b := 0
		for ; b < gd.S.Npart; b++ {
			m := float32(gd.S.Npart-b) / float32(gd.S.Npart)
			gd.S.MaskingLower[b] = powf(10.0, skS*m*0.1)
		}
		for ; b < PsyCBANDS; b++ {
			gd.S.MaskingLower[b] = 1.0
		}
		b = 0
		for ; b < gd.L.Npart; b++ {
			m := float32(gd.L.Npart-b) / float32(gd.L.Npart)
			gd.L.MaskingLower[b] = powf(10.0, skL*m*0.1)
		}
		for ; b < PsyCBANDS; b++ {
			gd.L.MaskingLower[b] = 1.0
		}
	}
	gd.LToS = gd.L // memcpy(&gd->l_to_s, &gd->l, sizeof(gd->l_to_s))
	// NOTE: gd.L.S3 is a slice; the C memcpy copies the s3 pointer. l_to_s is
	// re-initialised by init_numline below (which does not touch S3), so LToS
	// continues to share L's S3 backing array exactly as the C shares the
	// pointer. l_to_s.S3 is never indexed by the per-frame path
	// (convert_partition2scalefac uses only bo/bo_weight/n_sb/npart).
	initNumline(&gd.LToS, sfreq, BLKSIZE, 192, SBMAXs, pm.ScalefacBand.S[:])
	return 0
}
