// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Perceptual-entropy (PE) module ported 1:1 from the vendored FDK-AAC reference
// libAACenc/src/line_pe.cpp + line_pe.h. This is the load-bearing core of the
// AAC encoder threshold-adjustment driver (adj_thr.cpp): FDKaacEnc_AdjustThresholds
// repeatedly maps PsyOut sfb energies/thresholds (in the ld64 log domain) to a
// per-sfb perceptual-entropy estimate against the granted bit budget, and the PE
// figures computed here drive that whole loop. Pure fixed-point: every value is
// an int32 FIXP_DBL Q-format / INT, with carried block exponents — bit-identical
// to the C, no float.

// PE Q-format constants (line_pe.h:110, sf_estim.h:112, fixpoint_math.h:114).
//
//	#define PE_CONSTPART_SHIFT FRACT_BITS   // FRACT_BITS == 16 (common_fix.h:112)
//	#define FORM_FAC_SHIFT 6
//	#define LD_DATA_SHIFT 6
const (
	peConstPartShift = 16 // PE_CONSTPART_SHIFT == FRACT_BITS
	formFacShift     = 6  // FORM_FAC_SHIFT
)

// maxGroupedSFB is MAX_GROUPED_SFB (psy_const.h:151) == 60: the maximum number
// of (grouped) scalefactor bands per channel.
const maxGroupedSFB = 60

// peCxConstants are the ld-domain breakpoint constants of FDKaacEnc_calcSfbPe
// (line_pe.cpp:109-113). They are fl2fxconstDBL of the documented reals scaled by
// 1/LD_DATA_SCALING, materialised exactly as the C compiler folds them.
//
//	C1 = 3.0       = log(8.0)/log(2)   -> FL2FXCONST_DBL(3.0/LD_DATA_SCALING)
//	C2 = 1.3219281 = log(2.5)/log(2)   -> FL2FXCONST_DBL(1.3219281/LD_DATA_SCALING)
//	C3 = 0.5593573 = 1-C2/C1           -> FL2FXCONST_DBL(0.5593573)
var (
	c1LdData = fl2fxconstDBL(3.0 / 64.0)
	c2LdData = fl2fxconstDBL(1.3219281 / 64.0)
	c3LdData = fl2fxconstDBL(0.5593573)
)

// peChannelData is the 1:1 port of PE_CHANNEL_DATA (line_pe.h:112-122): the
// per-channel PE working/output state. Arrays are sized MAX_GROUPED_SFB and
// indexed by the absolute sfb (sfbGrp+sfb), exactly as the C struct.
type peChannelData struct {
	sfbNLines       [maxGroupedSFB]int32 // number of relevant lines (prepareSfbPe)
	sfbPe           [maxGroupedSFB]int32 // pe for each sfb
	sfbConstPart    [maxGroupedSFB]int32 // constant part for each sfb
	sfbNActiveLines [maxGroupedSFB]int32 // number of active lines in sfb
	pe              int32                // sum of sfbPe
	constPart       int32                // sum of sfbConstPart
	nActiveLines    int32                // sum of sfbNActiveLines
}

// peData is the 1:1 port of PE_DATA (line_pe.h:124-130).
type peData struct {
	peChannelData [2]peChannelData
	pe            int32
	constPart     int32
	nActiveLines  int32
	offset        int32
}

// prepareSfbPe is the 1:1 port of FDKaacEnc_prepareSfbPe (line_pe.cpp:116-150):
// it estimates, per sfb, the number of active spectral lines (sfbNLines) for the
// PE calculation — constants that do not change across successive pe calculations
// in the threshold-adjustment loop. The estimate is
// CalcInvLdData(sfbFormFactorLdData + FORM_FAC_SHIFT/LD_DATA_SCALING +
// avgFormFactorLdData), clamped to the sfb width.
//
//	formFacScaling = FL2FXCONST_DBL((float)FORM_FAC_SHIFT / LD_DATA_SCALING)
func prepareSfbPe(peChan *peChannelData,
	sfbEnergyLdData, sfbThresholdLdData, sfbFormFactorLdData []int32,
	sfbOffset []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup int) {
	formFacScaling := fl2fxconstDBL(float64(formFacShift) / 64.0)

	for sfbGrp := 0; sfbGrp < sfbCnt; sfbGrp += sfbPerGroup {
		for sfb := 0; sfb < maxSfbPerGroup; sfb++ {
			idx := sfbGrp + sfb
			if sfbEnergyLdData[idx] > sfbThresholdLdData[idx] {
				sfbWidth := sfbOffset[idx+1] - sfbOffset[idx]
				// estimate number of active lines
				avgFormFactorLdData := ((-sfbEnergyLdData[idx] >> 1) +
					(calcLdInt(sfbWidth) >> 1)) >> 1
				peChan.sfbNLines[idx] = calcInvLdData(
					(sfbFormFactorLdData[idx] + formFacScaling) +
						avgFormFactorLdData)
				// Make sure sfbNLines is never greater than sfbWidth due to
				// unaccuracies (e.g. sfbEnergyLdData[idx] = 0x80000000)
				peChan.sfbNLines[idx] = fMin(sfbWidth, peChan.sfbNLines[idx])
			} else {
				peChan.sfbNLines[idx] = 0
			}
		}
	}
}

// calcSfbPe is the 1:1 port of FDKaacEnc_calcSfbPe (line_pe.cpp:161-234): given
// the ld-domain sfb energies and thresholds (and the precomputed sfbNLines), it
// computes the per-sfb perceptual entropy (sfbPe), its threshold-independent
// constant part (sfbConstPart) and active-line count (sfbNActiveLines), plus the
// channel sums (pe, constPart, nActiveLines). The PE formula per sfb is
//
//	pe = n * ld(en/thr),             if ld(en/thr) >= C1
//	pe = n * (C2 + C3 * ld(en/thr)), if ld(en/thr) <  C1
//
// scaled by PE_CONSTPART_SHIFT and corrected back at the end. Intensity-coded
// bands (isBook) instead account for the scalefactor-delta bit cost.
//
// fMultDiv2(LONG,LONG) == fMultDiv2DD; fMult(LONG,LONG) == fMultDD;
// fMultI(C3LdData,nLines) == fMultI. scaleLd is a fixed 0 here (the C carries it
// as a 0 placeholder for an unused intensity-scale path).
func calcSfbPe(peChan *peChannelData,
	sfbEnergyLdData, sfbThresholdLdData []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int,
	isBook, isScale []int32) {
	var scaleLd int32 // (FIXP_DBL)0
	lastValIs := int32(0)

	pe := int32(0)
	constPart := int32(0)
	nActiveLines := int32(0)

	for sfbGrp := 0; sfbGrp < sfbCnt; sfbGrp += sfbPerGroup {
		for sfb := 0; sfb < maxSfbPerGroup; sfb++ {
			tmpPe := int32(0)
			tmpConstPart := int32(0)
			tmpNActiveLines := int32(0)

			thisSfb := sfbGrp + sfb

			if sfbEnergyLdData[thisSfb] > sfbThresholdLdData[thisSfb] {
				logDataRatio := sfbEnergyLdData[thisSfb] - sfbThresholdLdData[thisSfb]
				nLines := peChan.sfbNLines[thisSfb]

				factor := nLines << (ldDataShift + peConstPartShift + 1)
				if logDataRatio >= c1LdData {
					// scale sfbPe and sfbConstPart with PE_CONSTPART_SHIFT
					tmpPe = fMultDiv2DD(logDataRatio, factor)
					tmpConstPart = fMultDiv2DD(sfbEnergyLdData[thisSfb]+scaleLd, factor)
				} else {
					// scale sfbPe and sfbConstPart with PE_CONSTPART_SHIFT.
					// fMult(LONG,LONG) on __ARM_ARCH_8__ == fixmul_DD ==
					// (a*b)>>31 (fixmulDDarm8), KEEPING bit 31 — not the generic
					// (fixmuldiv2_DD<<1) which drops it.
					tmpPe = fMultDiv2DD(
						c2LdData+fixmulDDarm8(c3LdData, logDataRatio), factor)
					tmpConstPart = fMultDiv2DD(
						c2LdData+fixmulDDarm8(c3LdData, sfbEnergyLdData[thisSfb]+scaleLd),
						factor)

					nLines = fMultI(c3LdData, nLines)
				}
				tmpNActiveLines = nLines
			} else if isBook[thisSfb] != 0 {
				// provide for cost of scale factor for Intensity
				delta := isScale[thisSfb] - lastValIs
				lastValIs = isScale[thisSfb]
				peChan.sfbPe[thisSfb] = int32(huffltabscf[delta+codeBookScfLav]) << peConstPartShift
				peChan.sfbConstPart[thisSfb] = 0
				peChan.sfbNActiveLines[thisSfb] = 0
			}
			peChan.sfbPe[thisSfb] = tmpPe
			peChan.sfbConstPart[thisSfb] = tmpConstPart
			peChan.sfbNActiveLines[thisSfb] = tmpNActiveLines

			// sum up peChanData values
			pe += tmpPe
			constPart += tmpConstPart
			nActiveLines += tmpNActiveLines
		}
	}

	// correct scaled pe and constPart values
	peChan.pe = pe >> peConstPartShift
	peChan.constPart = constPart >> peConstPartShift
	peChan.nActiveLines = nActiveLines
}
