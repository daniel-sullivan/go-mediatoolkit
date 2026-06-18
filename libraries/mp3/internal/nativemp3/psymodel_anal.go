// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// 1:1 Go translation of LAME 3.100's L3psycho_anal_vbr (psymodel.c:1407): the
// per-granule psychoacoustic-analysis driver. It runs attack detection, the
// long- and short-block FFT/masking passes, M/S threshold derivation, the
// short-block pre-echo control, the final block-type decision, and returns the
// perceptual entropy (the bit-allocation pre-step) plus per-band maskings for
// the *previous* granule (one-granule delay). FLOAT is float32; FMA-sensitive
// terms route through psFma / psMul / psAdd.

// L3psychoAnalVbr is LAME's L3psycho_anal_vbr (psymodel.c:1407).
//
// buffer is the two-channel split PCM window (1024 samples/ch centred over the
// 576-sample granule). The masking/PE/energy/blocktype outputs match the C's
// out-parameters: maskingRatio / maskingMSRatio carry the L,R / M,S en+thm,
// percepEntropy / percepMSEntropy the L,R / M,S perceptual entropy, energy[4]
// the L,R,M,S channel energies, and blocktypeD[2] the previous-granule block
// types. Returns 0 on success (as the C does).
func (pm *LameInternalFlags) L3psychoAnalVbr(buffer [2][]float32, grOut int,
	maskingRatio *[2][2]III_psy_ratio, maskingMSRatio *[2][2]III_psy_ratio,
	percepEntropy, percepMSEntropy, energy []float32, blocktypeD []int) int {

	cfg := &pm.Cfg
	psv := &pm.SvPsy
	gdl := &pm.CdPsy.L
	gds := &pm.CdPsy.S

	var lastThm [4]III_psy_xmin

	// fft and energy calculation
	var fftenergy [HBLKSIZE]float32
	var fftenergyS [3][HBLKSIZEs]float32
	var wsampL [2][BLKSIZE]float32
	var wsampS [2][3][BLKSIZEs]float32
	var eb, thr [4][PsyCBANDS]float32

	var subShortFactor [4][3]float32
	var thmm float32
	const pcfact = float32(0.6)
	// C: NS_INTERP(..., NS_PREECHO_ATT1 * pcfact). NS_PREECHO_ATTn are DOUBLE
	// macros (0.6 / 0.3) and pcfact is FLOAT (0.6f), so the product is a double
	// (double * float -> double) narrowed to FLOAT when bound to NS_INTERP's FLOAT
	// r parameter. A float32 product float32(0.6)*0.6f is off by a ULP and shifts
	// the short-block pre-echo nsInterp factor (psymodel.c:1529/1533/1548).
	preechoR1 := float32(NsPreechoAtt1 * float64(pcfact))
	preechoR2 := float32(NsPreechoAtt2 * float64(pcfact))
	athFactor := float32(1.0)
	if cfg.Msfix > 0.0 {
		athFactor = psMul(cfg.ATHOffsetFactor, pm.ATH.AdjustFactor)
	}

	// block type
	var nsAttacks [4][4]int
	var uselongblock [2]int

	// chn=2 and 3 = Mid and Side channels
	nChnPsy := cfg.ChannelsOut
	if cfg.Mode == modeJointStereo {
		nChnPsy = 4
	}

	lastThm = psv.Thm // memcpy(&last_thm[0], &psv->thm[0], sizeof(last_thm))

	pm.vbrpsyAttackDetection(buffer, grOut, maskingRatio, maskingMSRatio, energy,
		&subShortFactor, &nsAttacks, uselongblock[:])

	cfg.vbrpsyComputeBlockType(uselongblock[:])

	// LONG BLOCK CASE
	{
		for chn := 0; chn < nChnPsy; chn++ {
			ch01 := chn & 0x01
			// wsamp_l = wsamp_L + ch01: the channel-local view is row ch01.
			pm.vbrpsyComputeFftL(buffer, chn, grOut, fftenergy[:], &wsampL, ch01)
			pm.vbrpsyComputeLoudnessApproximationL(grOut, chn, fftenergy[:])
			pm.vbrpsyComputeMaskingL(fftenergy[:], eb[chn][:], thr[chn][:], chn)
		}
		if cfg.Mode == modeJointStereo {
			if uselongblock[0]+uselongblock[1] == 2 {
				vbrpsyComputeMSThresholds(&eb, &thr, gdl.MldCb[:], pm.ATH.CbL[:],
					athFactor, cfg.Msfix, gdl.Npart)
			}
		}
		for chn := 0; chn < nChnPsy; chn++ {
			pm.convertPartition2scalefacL(eb[chn][:], thr[chn][:], chn)
			pm.convertPartition2scalefacLToS(eb[chn][:], thr[chn][:], chn)
		}
	}
	// SHORT BLOCKS CASE
	{
		forceShortBlockCalc := pm.CdPsy.ForceShortBlockCalc
		for sblock := 0; sblock < 3; sblock++ {
			for chn := 0; chn < nChnPsy; chn++ {
				ch01 := chn & 0x01
				if uselongblock[ch01] != 0 && forceShortBlockCalc == 0 {
					pm.vbrpsySkipMaskingS(chn, sblock)
				} else {
					// compute masking thresholds for short blocks
					pm.vbrpsyComputeFftS(buffer, chn, sblock, &fftenergyS, &wsampS, ch01)
					pm.vbrpsyComputeMaskingS(&fftenergyS, eb[chn][:], thr[chn][:], chn, sblock)
				}
			}
			if cfg.Mode == modeJointStereo {
				if uselongblock[0]+uselongblock[1] == 0 {
					vbrpsyComputeMSThresholds(&eb, &thr, gds.MldCb[:], pm.ATH.CbS[:],
						athFactor, cfg.Msfix, gds.Npart)
				}
			}
			for chn := 0; chn < nChnPsy; chn++ {
				ch01 := chn & 0x01
				if uselongblock[ch01] == 0 || forceShortBlockCalc != 0 {
					pm.convertPartition2scalefacS(eb[chn][:], thr[chn][:], chn, sblock)
				}
			}
		}

		// short block pre-echo control
		for chn := 0; chn < nChnPsy; chn++ {
			for sb := 0; sb < SBMAXs; sb++ {
				var newThmm [3]float32
				var prevThm, t1, t2 float32
				for sblock := 0; sblock < 3; sblock++ {
					thmm = psv.Thm[chn].S[sb][sblock]
					// C: thmm *= NS_PREECHO_ATT0 where NS_PREECHO_ATT0 is the DOUBLE
					// macro 0.8 (psymodel.h:56), so thmm promotes to double, multiplies,
					// and narrows to FLOAT. A float32 *= float32(0.8) is off by a ULP
					// because 0.8 is inexact, shifting the short-block pre-echo threshold
					// (psymodel.c:1518).
					thmm = float32(float64(thmm) * NsPreechoAtt0)

					t1, t2 = thmm, thmm

					if sblock > 0 {
						prevThm = newThmm[sblock-1]
					} else {
						prevThm = lastThm[chn].S[sb][2]
					}
					if nsAttacks[chn][sblock] >= 2 || nsAttacks[chn][sblock+1] == 1 {
						t1 = nsInterp(prevThm, thmm, preechoR1)
					}
					thmm = minF32(t1, thmm)
					if nsAttacks[chn][sblock] == 1 {
						t2 = nsInterp(prevThm, thmm, preechoR2)
					} else if (sblock == 0 && psv.LastAttacks[chn] == 3) ||
						(sblock > 0 && nsAttacks[chn][sblock-1] == 3) { // 2nd preceding block
						switch sblock {
						case 0:
							prevThm = lastThm[chn].S[sb][1]
						case 1:
							prevThm = lastThm[chn].S[sb][2]
						case 2:
							prevThm = newThmm[0]
						}
						t2 = nsInterp(prevThm, thmm, preechoR2)
					}

					thmm = minF32(t1, thmm)
					thmm = minF32(t2, thmm)

					// pulse like signal detection for fatboy.wav and so on
					thmm = psMul(thmm, subShortFactor[chn][sblock])

					newThmm[sblock] = thmm
				}
				for sblock := 0; sblock < 3; sblock++ {
					psv.Thm[chn].S[sb][sblock] = newThmm[sblock]
				}
			}
		}
	}
	for chn := 0; chn < nChnPsy; chn++ {
		psv.LastAttacks[chn] = nsAttacks[chn][2]
	}

	// determine final block type
	psv.vbrpsyApplyBlockType(cfg.ChannelsOut, uselongblock[:], blocktypeD)

	// compute the value of PE to return ... no delay and advance
	for chn := 0; chn < nChnPsy; chn++ {
		// ppe selects percep_entropy (chn<2) or percep_MS_entropy-2 (chn>1);
		// the C indexes ppe[chn], so for the MS case ppe[chn] == percep_MS_entropy[chn-2].
		var ppe []float32
		var ppeIdx int
		var typ int
		var mr *III_psy_ratio

		if chn > 1 {
			ppe = percepMSEntropy
			ppeIdx = chn - 2
			typ = NormType
			if blocktypeD[0] == ShortType || blocktypeD[1] == ShortType {
				typ = ShortType
			}
			mr = &maskingMSRatio[grOut][chn-2]
		} else {
			ppe = percepEntropy
			ppeIdx = chn
			typ = blocktypeD[chn]
			mr = &maskingRatio[grOut][chn]
		}
		if typ == ShortType {
			ppe[ppeIdx] = pecalcS(mr, pm.SvQnt.MaskingLower)
		} else {
			ppe[ppeIdx] = pecalcL(mr, pm.SvQnt.MaskingLower)
		}
	}
	return 0
}
