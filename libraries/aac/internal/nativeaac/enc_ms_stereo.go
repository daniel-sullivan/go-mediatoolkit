// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// M/S stereo processing for the ENCODER — a 1:1 port of
// FDKaacEnc_MsStereoProcessing (libAACenc/src/ms_stereo.cpp:109-295). This is
// the joint-stereo decision on the encode side: for every non-intensity
// scalefactor band it compares the perceptual-entropy ("pe") cost of plain L/R
// coding against mid/side coding in the ld (log2) domain, and where M/S wins it
// rewrites the L/R spectrum in place to mid/side (specL = L>>1, specR = R>>1;
// out = M = specL+specR, S = specL-specR), copies the mid/side energies and the
// min threshold into the L/R slots, and sets msMask for that band. After the
// per-band loop it folds the result into a frame-level msDigest
// (SI_MS_MASK_NONE/SOME/ALL), promoting to MS_MASK_ALL (and M/S-coding the
// remaining bands) when the false-count predicate holds.
//
// Every value here is fixed-point integer: FIXP_DBL (int32) Q-format energies,
// thresholds and MDCT lines, plus their ld-domain (CalcLdData) FIXP_DBL forms.
// The arithmetic is arithmetic shifts and fixMin/fixMax only — no float, no
// transcendental — so it is bit-identical regardless of vectorization and is
// NOT gated by aac_strict. The ">> 1" before the fixMax-difference in the pe
// metric is the C's headroom guard against int32 overflow when summing two
// ld-domain differences; it is reproduced exactly.
//
// Layout (matching the C, which threads per-channel PSY_DATA / PSY_OUT_CHANNEL
// arrays indexed by the grouped sfb): every per-band slice is indexed directly
// by sfb+sfboffs over [0,sfbCnt). The mdct spectra are flat per-window arrays
// indexed by the absolute line; sfbOffset[band] is the first line of band and
// has length sfbCnt+1. isBook may be nil (treated as all-zero -> every band is
// a candidate), mirroring the `(isBook == NULL) ? 1 : ...` guard.

// MsStereoData bundles the per-band fixed-point arrays the encode-side M/S
// decision reads and writes in place, a faithful flattening of the PSY_DATA /
// PSY_OUT_CHANNEL fields FDKaacEnc_MsStereoProcessing touches
// (ms_stereo.cpp:117-145). All slices are indexed by the grouped sfb in
// [0,SfbCnt); the *LdData slices are the CalcLdData (log2-domain) forms.
//
// Inputs read: SfbEnergyMid/Side (+ their LdData), and the L/R energies,
// thresholds and spread energies (+ LdData). Outputs: where M/S wins for a
// band, the L/R energy/threshold/spread/LdData slots are overwritten with the
// mid/side / min values, MsMask[band] is set, and the MDCT lines of the band
// are rewritten L/R -> M/S in place.
type MsStereoData struct {
	SfbEnergyLeft       []int32 // psyData[0]->sfbEnergy.Long   (modified)
	SfbEnergyRight      []int32 // psyData[1]->sfbEnergy.Long   (modified)
	SfbEnergyMid        []int32 // psyData[0]->sfbEnergyMS.Long (read)
	SfbEnergySide       []int32 // psyData[1]->sfbEnergyMS.Long (read)
	SfbThresholdLeft    []int32 // psyData[0]->sfbThreshold.Long (modified)
	SfbThresholdRight   []int32 // psyData[1]->sfbThreshold.Long (modified)
	SfbSpreadEnLeft     []int32 // psyData[0]->sfbSpreadEnergy.Long (modified)
	SfbSpreadEnRight    []int32 // psyData[1]->sfbSpreadEnergy.Long (modified)
	SfbEnergyLeftLd     []int32 // psyOutChannel[0]->sfbEnergyLdData (modified)
	SfbEnergyRightLd    []int32 // psyOutChannel[1]->sfbEnergyLdData (modified)
	SfbEnergyMidLd      []int32 // psyData[0]->sfbEnergyMSLdData (read)
	SfbEnergySideLd     []int32 // psyData[1]->sfbEnergyMSLdData (read)
	SfbThresholdLeftLd  []int32 // psyOutChannel[0]->sfbThresholdLdData (modified)
	SfbThresholdRightLd []int32 // psyOutChannel[1]->sfbThresholdLdData (modified)
	MdctSpectrumLeft    []int32 // psyData[0]->mdctSpectrum (modified)
	MdctSpectrumRight   []int32 // psyData[1]->mdctSpectrum (modified)
}

// fMinDBL ports the FIXP_DBL fMin (common_fix.h:400 -> fixmin_D): the signed
// 32-bit minimum. ms_stereo uses fixMin == fMin (common_fix.h:306).
func fMinDBL(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// fMaxDBL ports the FIXP_DBL fMax (common_fix.h:401 -> fixmax_D): the signed
// 32-bit maximum. ms_stereo uses fixMax == fMax (common_fix.h:307).
func fMaxDBL(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

// MsStereoProcessing ports FDKaacEnc_MsStereoProcessing
// (ms_stereo.cpp:109-295). It runs the per-band M/S decision over d (modifying
// it in place), writes the per-band msMask, and returns the frame-level
// msDigest (one of SI_MS_MASK_NONE/SOME/ALL).
//
// isBook may be nil (all bands are M/S candidates). msMask must have length
// >= sfbCnt and is written per band. allowMS gates whether any band may use
// M/S. sfbCnt/sfbPerGroup/maxSfbPerGroup/sfbOffset are the grouped-sfb
// geometry; the outer loop strides whole groups (sfb += sfbPerGroup) and the
// inner loop covers maxSfbPerGroup bands, exactly as the C.
func MsStereoProcessing(d *MsStereoData, isBook []int32, msMask []int32,
	allowMS, sfbCnt, sfbPerGroup, maxSfbPerGroup int, sfbOffset []int32) int {

	msMaskTrueSomewhere := 0 // to determine msDigest
	numMsMaskFalse := 0      // non-intensity bands where L/R coding is used

	for sfb := 0; sfb < sfbCnt; sfb += sfbPerGroup {
		for sfboffs := 0; sfboffs < maxSfbPerGroup; sfboffs++ {
			isBookZero := isBook == nil || isBook[sfb+sfboffs] == 0
			if isBookZero {
				var tmp int32

				// we assume that scaleMinThres == 1.0f and we can drop it
				minThresholdLdData := fMinDBL(d.SfbThresholdLeftLd[sfb+sfboffs],
					d.SfbThresholdRightLd[sfb+sfboffs])

				tmp = fMaxDBL(d.SfbEnergyLeftLd[sfb+sfboffs],
					d.SfbThresholdLeftLd[sfb+sfboffs])
				pnlrLdData := (d.SfbThresholdLeftLd[sfb+sfboffs] >> 1) - (tmp >> 1)
				pnlrLdData = pnlrLdData + (d.SfbThresholdRightLd[sfb+sfboffs] >> 1)
				tmp = fMaxDBL(d.SfbEnergyRightLd[sfb+sfboffs],
					d.SfbThresholdRightLd[sfb+sfboffs])
				pnlrLdData = pnlrLdData - (tmp >> 1)

				tmp = fMaxDBL(d.SfbEnergyMidLd[sfb+sfboffs], minThresholdLdData)
				pnmsLdData := minThresholdLdData - (tmp >> 1)
				tmp = fMaxDBL(d.SfbEnergySideLd[sfb+sfboffs], minThresholdLdData)
				pnmsLdData = pnmsLdData - (tmp >> 1)
				useMS := (allowMS != 0) && (pnmsLdData > pnlrLdData)

				if useMS {
					msMask[sfb+sfboffs] = 1
					msMaskTrueSomewhere = 1
					for j := sfbOffset[sfb+sfboffs]; j < sfbOffset[sfb+sfboffs+1]; j++ {
						specL := d.MdctSpectrumLeft[j] >> 1
						specR := d.MdctSpectrumRight[j] >> 1
						d.MdctSpectrumLeft[j] = specL + specR
						d.MdctSpectrumRight[j] = specL - specR
					}
					minThreshold := fMinDBL(d.SfbThresholdLeft[sfb+sfboffs],
						d.SfbThresholdRight[sfb+sfboffs])
					d.SfbThresholdLeft[sfb+sfboffs] = minThreshold
					d.SfbThresholdRight[sfb+sfboffs] = minThreshold
					d.SfbThresholdLeftLd[sfb+sfboffs] = minThresholdLdData
					d.SfbThresholdRightLd[sfb+sfboffs] = minThresholdLdData
					d.SfbEnergyLeft[sfb+sfboffs] = d.SfbEnergyMid[sfb+sfboffs]
					d.SfbEnergyRight[sfb+sfboffs] = d.SfbEnergySide[sfb+sfboffs]
					d.SfbEnergyLeftLd[sfb+sfboffs] = d.SfbEnergyMidLd[sfb+sfboffs]
					d.SfbEnergyRightLd[sfb+sfboffs] = d.SfbEnergySideLd[sfb+sfboffs]

					sp := fMinDBL(d.SfbSpreadEnLeft[sfb+sfboffs],
						d.SfbSpreadEnRight[sfb+sfboffs]) >> 1
					d.SfbSpreadEnLeft[sfb+sfboffs] = sp
					d.SfbSpreadEnRight[sfb+sfboffs] = sp
				} else {
					msMask[sfb+sfboffs] = 0
					numMsMaskFalse++
				} // useMS
			} else { // isBook
				// keep mDigest from IS module
				if msMask[sfb+sfboffs] != 0 {
					msMaskTrueSomewhere = 1
				}
				// prohibit MS_MASK_ALL in combination with IS
				numMsMaskFalse = 9
			} // isBook
		} // sfboffs
	} // sfb

	var msDigest int
	if msMaskTrueSomewhere == 1 {
		if (numMsMaskFalse == 0) ||
			((numMsMaskFalse < maxSfbPerGroup) && (numMsMaskFalse < 9)) {
			msDigest = SiMsMaskAll
			// loop through M/S bands; if msMask==0, set it to 1 and apply M/S
			for sfb := 0; sfb < sfbCnt; sfb += sfbPerGroup {
				for sfboffs := 0; sfboffs < maxSfbPerGroup; sfboffs++ {
					isBookZero := isBook == nil || isBook[sfb+sfboffs] == 0
					if isBookZero && msMask[sfb+sfboffs] == 0 {
						msMask[sfb+sfboffs] = 1
						// apply M/S coding
						for j := sfbOffset[sfb+sfboffs]; j < sfbOffset[sfb+sfboffs+1]; j++ {
							specL := d.MdctSpectrumLeft[j] >> 1
							specR := d.MdctSpectrumRight[j] >> 1
							d.MdctSpectrumLeft[j] = specL + specR
							d.MdctSpectrumRight[j] = specL - specR
						}
						minThreshold := fMinDBL(d.SfbThresholdLeft[sfb+sfboffs],
							d.SfbThresholdRight[sfb+sfboffs])
						d.SfbThresholdLeft[sfb+sfboffs] = minThreshold
						d.SfbThresholdRight[sfb+sfboffs] = minThreshold
						minThresholdLdData := fMinDBL(d.SfbThresholdLeftLd[sfb+sfboffs],
							d.SfbThresholdRightLd[sfb+sfboffs])
						d.SfbThresholdLeftLd[sfb+sfboffs] = minThresholdLdData
						d.SfbThresholdRightLd[sfb+sfboffs] = minThresholdLdData
						d.SfbEnergyLeft[sfb+sfboffs] = d.SfbEnergyMid[sfb+sfboffs]
						d.SfbEnergyRight[sfb+sfboffs] = d.SfbEnergySide[sfb+sfboffs]
						d.SfbEnergyLeftLd[sfb+sfboffs] = d.SfbEnergyMidLd[sfb+sfboffs]
						d.SfbEnergyRightLd[sfb+sfboffs] = d.SfbEnergySideLd[sfb+sfboffs]

						sp := fMinDBL(d.SfbSpreadEnLeft[sfb+sfboffs],
							d.SfbSpreadEnRight[sfb+sfboffs]) >> 1
						d.SfbSpreadEnLeft[sfb+sfboffs] = sp
						d.SfbSpreadEnRight[sfb+sfboffs] = sp
					}
				}
			}
		} else {
			msDigest = SiMsMaskSome
		}
	} else {
		msDigest = SiMsMaskNone
	}

	return msDigest
}
