// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// 1:1 Go translation of LAME 3.100's vbrpsy_attack_detection
// (psymodel.c:769): applies an fs/4 high-pass FIR to the input, measures
// per-sub-short-block energies, and from their ratios decides the long/short
// block flag and pulse-like sub_short_factor attenuations. FLOAT is float32;
// the FIR's `coef*(a+b)` terms route through psFma so the strict build matches
// the -ffp-contract=off oracle.

// firCoef is vbrpsy_attack_detection's static fircoef[] (psymodel.c:788): the
// (NSFIRLEN-1)/2 = 10 half-band high-pass coefficients (each already ×2).
var firCoef = [...]float32{
	-8.65163e-18 * 2, -0.00851586 * 2, -6.74764e-18 * 2, 0.0209036 * 2,
	-3.36639e-17 * 2, -0.0438162 * 2, -1.54175e-17 * 2, 0.0931738 * 2,
	-5.52212e-17 * 2, -0.313819 * 2,
}

// vbrpsyAttackDetection is LAME's vbrpsy_attack_detection (psymodel.c:769).
// buffer is the two-channel split PCM; the outputs match the C: masking_ratio
// / masking_MS_ratio carry the one-granule-delayed en/thm, energy[] the
// channel totals, subShortFactor[][] the pulse attenuations, nsAttacks[][] the
// attack positions, and uselongblock[] the per-channel block flag.
func (pm *LameInternalFlags) vbrpsyAttackDetection(buffer [2][]float32, grOut int,
	maskingRatio *[2][2]III_psy_ratio, maskingMSRatio *[2][2]III_psy_ratio,
	energy []float32, subShortFactor *[4][3]float32, nsAttacks *[4][4]int, uselongblock []int) {

	var nsHpfsmpl [2][576]float32
	cfg := &pm.Cfg
	psv := &pm.SvPsy
	nChnOut := cfg.ChannelsOut
	// chn=2 and 3 = Mid and Side channels
	nChnPsy := nChnOut
	if cfg.Mode == modeJointStereo {
		nChnPsy = 4
	}

	// Don't copy the input buffer into a temporary buffer; unroll the loop 2x.
	for chn := 0; chn < nChnOut; chn++ {
		// apply high pass filter of fs/4
		// firbuf = &buffer[chn][576 - 350 - NSFIRLEN + 192]
		firOff := 576 - 350 - nsfirLen + 192
		firbuf := buffer[chn]
		for i := 0; i < 576; i++ {
			var sum1, sum2 float32
			sum1 = firbuf[firOff+i+10]
			sum2 = 0.0
			for j := 0; j < ((nsfirLen-1)/2)-1; j += 2 {
				sum1 = psFma(sum1, firCoef[j], psAdd(firbuf[firOff+i+j], firbuf[firOff+i+nsfirLen-j]))
				sum2 = psFma(sum2, firCoef[j+1], psAdd(firbuf[firOff+i+j+1], firbuf[firOff+i+nsfirLen-j-1]))
			}
			nsHpfsmpl[chn][i] = psAdd(sum1, sum2)
		}
		maskingRatio[grOut][chn].En = psv.En[chn]
		maskingRatio[grOut][chn].Thm = psv.Thm[chn]
		if nChnPsy > 2 {
			// MS maskings
			maskingMSRatio[grOut][chn].En = psv.En[chn+2]
			maskingMSRatio[grOut][chn].Thm = psv.Thm[chn+2]
		}
	}
	for chn := 0; chn < nChnPsy; chn++ {
		var attackIntensity [12]float32
		var enSubshort [12]float32
		var enShort [4]float32
		pfChan := chn & 1
		pf := 0 // running index into nsHpfsmpl[pfChan]
		nsUselongblock := 1

		if chn == 2 {
			for i, j := 0, 576; j > 0; i, j = i+1, j-1 {
				l := nsHpfsmpl[0][i]
				r := nsHpfsmpl[1][i]
				nsHpfsmpl[0][i] = psAdd(l, r)
				nsHpfsmpl[1][i] = psSub(l, r)
			}
		}
		// determine the block type (window type) — energies of sub-shortblocks
		for i := 0; i < 3; i++ {
			enSubshort[i] = psv.LastEnSubshort[chn][i+6]
			attackIntensity[i] = psDiv(enSubshort[i], psv.LastEnSubshort[chn][i+4])
			enShort[0] = psAdd(enShort[0], enSubshort[i])
		}

		for i := 0; i < 9; i++ {
			// pfe = pf + 576/9
			pfe := pf + 576/9
			p := float32(1.0)
			for ; pf < pfe; pf++ {
				if a := fabsF32(nsHpfsmpl[pfChan][pf]); p < a {
					p = a
				}
			}
			psv.LastEnSubshort[chn][i] = p
			enSubshort[i+3] = p
			enShort[1+i/3] = psAdd(enShort[1+i/3], p)
			if p > enSubshort[i+3-2] {
				p = psDiv(p, enSubshort[i+3-2])
			} else if enSubshort[i+3-2] > psMul(p, 10.0) {
				p = psDiv(enSubshort[i+3-2], psMul(p, 10.0))
			} else {
				p = 0.0
			}
			attackIntensity[i+3] = p
		}

		// pulse like signal detection for fatboy.wav and so on
		for i := 0; i < 3; i++ {
			enn := psAdd(psAdd(enSubshort[i*3+3], enSubshort[i*3+4]), enSubshort[i*3+5])
			factor := float32(1.0)
			if psMul(enSubshort[i*3+5], 6) < enn {
				factor = psMul(factor, 0.5)
				if psMul(enSubshort[i*3+4], 6) < enn {
					factor = psMul(factor, 0.5)
				}
			}
			subShortFactor[chn][i] = factor
		}

		// compare energies between sub-shortblocks
		{
			x := pm.CdPsy.AttackThreshold[chn]
			for i := 0; i < 12; i++ {
				if nsAttacks[chn][i/3] == 0 {
					if attackIntensity[i] > x {
						nsAttacks[chn][i/3] = (i % 3) + 1
					}
				}
			}
		}
		// should have energy change between short blocks (avoid periodic signals)
		for i := 1; i < 4; i++ {
			u := enShort[i-1]
			v := enShort[i]
			m := maxF32(u, v)
			if m < 40000 { // (2)
				if u < psMul(1.7, v) && v < psMul(1.7, u) { // (1)
					if i == 1 && nsAttacks[chn][0] <= nsAttacks[chn][i] {
						nsAttacks[chn][0] = 0
					}
					nsAttacks[chn][i] = 0
				}
			}
		}

		if nsAttacks[chn][0] <= psv.LastAttacks[chn] {
			nsAttacks[chn][0] = 0
		}

		if psv.LastAttacks[chn] == 3 ||
			nsAttacks[chn][0]+nsAttacks[chn][1]+nsAttacks[chn][2]+nsAttacks[chn][3] != 0 {
			nsUselongblock = 0

			if nsAttacks[chn][1] != 0 && nsAttacks[chn][0] != 0 {
				nsAttacks[chn][1] = 0
			}
			if nsAttacks[chn][2] != 0 && nsAttacks[chn][1] != 0 {
				nsAttacks[chn][2] = 0
			}
			if nsAttacks[chn][3] != 0 && nsAttacks[chn][2] != 0 {
				nsAttacks[chn][3] = 0
			}
		}

		if chn < 2 {
			uselongblock[chn] = nsUselongblock
		} else {
			if nsUselongblock == 0 {
				uselongblock[0], uselongblock[1] = 0, 0
			}
		}

		// one granule delay: copy maskings into masking_ratio (done above);
		// here return the channel energy.
		energy[chn] = psv.TotEner[chn]
	}
}
