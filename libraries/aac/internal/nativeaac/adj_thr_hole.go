// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Avoid-hole / threshold-reduction stage of the AAC encoder threshold-adjustment
// loop, ported 1:1 from the vendored FDK-AAC reference libAACenc/src/adj_thr.cpp.
// FDKaacEnc_initAvoidHoleFlag seeds the per-sfb avoid-hole flags (and adapts the
// per-sfb minSnr for peaks/valleys and the M/S-coupled stereo requirements);
// FDKaacEnc_reduceThresholdsCBR applies the CBR threshold-reduction formula at a
// given reduction value while avoiding spectral holes; FDKaacEnc_calcChaosMeasure
// estimates the relative active-line count (a chaos / tonality figure).
//
// CBR/AAC-LC path only — the VBR reduceThresholdsVBR sibling is excluded.
//
// Pure fixed-point: every value is an int32 FIXP_DBL Q-format / INT with carried
// block exponents — bit-identical to the C, no float, no transcendental. fMult ==
// fixmul_DD == fixmulDDarm8 on the aarch64 target (KEEPS bit 31). CountLeadingBits
// == fNorm; fAbs == fAbsDBL; scaleValue is the non-saturating shift.

// SNR ld64-domain breakpoint constants (adj_thr.cpp:119-135). These are the raw
// FIXP_DBL hex the C compiler materialises for
// FL2FXCONST_DBL(log(x)/log(2)/LD_DATA_SCALING); carried verbatim.
const (
	snrLdMin1 int32 = -55767585 // 0xfcad0ddf: log(0.316)/log(2)/64
	snrLdMin2 int32 = 55697826  // 0x0351e1a2: log(3.16) /log(2)/64
	snrLdFac  int32 = -10802114 // 0xff5b2c3e: log(0.8)  /log(2)/64
	snrLdMin3 int32 = -33554432 // 0xfe000000: log(0.5)  /log(2)/64
	snrLdMin4 int32 = 33554432  // 0x02000000: log(2.0)  /log(2)/64
	snrLdMin5 int32 = -67108864 // 0xfc000000: log(0.25) /log(2)/64
)

// limitThrReducedLdData mirrors the file-scope static
// limitThrReducedLdData (adj_thr.cpp:986) == (FIXP_DBL)0x00008000. Unused by the
// CBR formula body itself (it is referenced by reduceThresholdsVBR), carried for
// 1:1 fidelity of the constants in this area.
const limitThrReducedLdData = int32(0x00008000)

// initAvoidHoleFlag is the 1:1 port of FDKaacEnc_initAvoidHoleFlag
// (adj_thr.cpp:539-699): decrease the spread energy (3 dB long / 2 dB short),
// optionally adapt the per-sfb minSnr for spectral peaks/valleys, adapt the M/S
// stereo minSnr/spread requirements, and finally seed ahFlag per sfb (NO_AH if a
// hole is impossible, AH_INACTIVE otherwise).
//
// ahFlag is the [2][MAX_GROUPED_SFB] flag matrix (one row per channel).
func initAvoidHoleFlag(qcOutChannel []*QcOutChannel, psyOutChannel []*PsyOutChannel,
	ahFlag [][]uint8, toolsInfo *PsyOutToolsInfo, nChannels int, ahParam *ahParam) {

	// decrease spread energy by 3dB for long blocks, 2dB for shorts
	for ch := 0; ch < nChannels; ch++ {
		qcOutChan := qcOutChannel[ch]
		if psyOutChannel[ch].LastWindowSequence != encShortWindow {
			for sfbGrp := 0; sfbGrp < psyOutChannel[ch].SfbCnt; sfbGrp += psyOutChannel[ch].SfbPerGroup {
				for sfb := 0; sfb < psyOutChannel[ch].MaxSfbPerGroup; sfb++ {
					qcOutChan.SfbSpreadEnergy[sfbGrp+sfb] >>= 1
				}
			}
		} else {
			for sfbGrp := 0; sfbGrp < psyOutChannel[ch].SfbCnt; sfbGrp += psyOutChannel[ch].SfbPerGroup {
				for sfb := 0; sfb < psyOutChannel[ch].MaxSfbPerGroup; sfb++ {
					qcOutChan.SfbSpreadEnergy[sfbGrp+sfb] = fixmulDDarm8(
						fl2fxconstDBL(float64(float32(0.63))), qcOutChan.SfbSpreadEnergy[sfbGrp+sfb])
				}
			}
		}
	}

	// increase minSnr for local peaks, decrease it for valleys
	if ahParam.modifyMinSnr != 0 {
		for ch := 0; ch < nChannels; ch++ {
			qcOutChan := qcOutChannel[ch]
			for sfbGrp := 0; sfbGrp < psyOutChannel[ch].SfbCnt; sfbGrp += psyOutChannel[ch].SfbPerGroup {
				for sfb := 0; sfb < psyOutChannel[ch].MaxSfbPerGroup; sfb++ {
					var sfbEnm1, sfbEnp1 int32
					if sfb > 0 {
						sfbEnm1 = qcOutChan.SfbEnergy[sfbGrp+sfb-1]
					} else {
						sfbEnm1 = qcOutChan.SfbEnergy[sfbGrp+sfb]
					}
					if sfb < psyOutChannel[ch].MaxSfbPerGroup-1 {
						sfbEnp1 = qcOutChan.SfbEnergy[sfbGrp+sfb+1]
					} else {
						sfbEnp1 = qcOutChan.SfbEnergy[sfbGrp+sfb]
					}

					avgEn := (sfbEnm1 >> 1) + (sfbEnp1 >> 1)
					avgEnLdData := calcLdData(avgEn)
					sfbEn := qcOutChan.SfbEnergy[sfbGrp+sfb]
					sfbEnLdData := qcOutChan.SfbEnergyLdData[sfbGrp+sfb]
					// peak ?
					if sfbEn > avgEn {
						var tmpMinSnrLdData int32
						if psyOutChannel[ch].LastWindowSequence == encLongWindow {
							tmpMinSnrLdData = snrLdFac + fMax(avgEnLdData-sfbEnLdData, snrLdMin1-snrLdFac)
						} else {
							tmpMinSnrLdData = snrLdFac + fMax(avgEnLdData-sfbEnLdData, snrLdMin3-snrLdFac)
						}
						qcOutChan.SfbMinSnrLdData[sfbGrp+sfb] = fMin(
							qcOutChan.SfbMinSnrLdData[sfbGrp+sfb], tmpMinSnrLdData)
					}
					// valley ?
					if (sfbEnLdData+snrLdMin4) < avgEnLdData && sfbEn > 0 {
						tmpMinSnrLdData := avgEnLdData - sfbEnLdData - snrLdMin4 +
							qcOutChan.SfbMinSnrLdData[sfbGrp+sfb]
						tmpMinSnrLdData = fMin(snrLdFac, tmpMinSnrLdData)
						qcOutChan.SfbMinSnrLdData[sfbGrp+sfb] = fMin(tmpMinSnrLdData,
							qcOutChan.SfbMinSnrLdData[sfbGrp+sfb]+snrLdMin2)
					}
				}
			}
		}
	}

	// stereo: adapt minimum requirements sfbMinSnr of mid and side channels
	if nChannels == 2 {
		qcOutChanM := qcOutChannel[0]
		qcOutChanS := qcOutChannel[1]
		psyOutChanM := psyOutChannel[0]
		for sfbGrp := 0; sfbGrp < psyOutChanM.SfbCnt; sfbGrp += psyOutChanM.SfbPerGroup {
			for sfb := 0; sfb < psyOutChanM.MaxSfbPerGroup; sfb++ {
				if toolsInfo.MsMask[sfbGrp+sfb] != 0 {
					maxSfbEnLd := fMax(qcOutChanM.SfbEnergyLdData[sfbGrp+sfb],
						qcOutChanS.SfbEnergyLdData[sfbGrp+sfb])
					var maxThrLd, sfbMinSnrTmpLd int32

					if ((snrLdMin5 >> 1) + (maxSfbEnLd >> 1) +
						(qcOutChanM.SfbMinSnrLdData[sfbGrp+sfb] >> 1)) <= fl2fxconstDBL(-0.5) {
						maxThrLd = fl2fxconstDBL(-1.0)
					} else {
						maxThrLd = snrLdMin5 + maxSfbEnLd + qcOutChanM.SfbMinSnrLdData[sfbGrp+sfb]
					}

					if qcOutChanM.SfbEnergy[sfbGrp+sfb] > 0 {
						sfbMinSnrTmpLd = maxThrLd - qcOutChanM.SfbEnergyLdData[sfbGrp+sfb]
					} else {
						sfbMinSnrTmpLd = 0
					}
					qcOutChanM.SfbMinSnrLdData[sfbGrp+sfb] = fMax(
						qcOutChanM.SfbMinSnrLdData[sfbGrp+sfb], sfbMinSnrTmpLd)
					if qcOutChanM.SfbMinSnrLdData[sfbGrp+sfb] <= 0 {
						qcOutChanM.SfbMinSnrLdData[sfbGrp+sfb] = fMin(
							qcOutChanM.SfbMinSnrLdData[sfbGrp+sfb], snrLdFac)
					}

					if qcOutChanS.SfbEnergy[sfbGrp+sfb] > 0 {
						sfbMinSnrTmpLd = maxThrLd - qcOutChanS.SfbEnergyLdData[sfbGrp+sfb]
					} else {
						sfbMinSnrTmpLd = 0
					}
					qcOutChanS.SfbMinSnrLdData[sfbGrp+sfb] = fMax(
						qcOutChanS.SfbMinSnrLdData[sfbGrp+sfb], sfbMinSnrTmpLd)
					if qcOutChanS.SfbMinSnrLdData[sfbGrp+sfb] <= 0 {
						qcOutChanS.SfbMinSnrLdData[sfbGrp+sfb] = fMin(
							qcOutChanS.SfbMinSnrLdData[sfbGrp+sfb], snrLdFac)
					}

					if qcOutChanM.SfbEnergy[sfbGrp+sfb] > qcOutChanM.SfbSpreadEnergy[sfbGrp+sfb] {
						qcOutChanS.SfbSpreadEnergy[sfbGrp+sfb] = fixmulDDarm8(
							qcOutChanS.SfbEnergy[sfbGrp+sfb], fl2fxconstDBL(float64(float32(0.9))))
					}
					if qcOutChanS.SfbEnergy[sfbGrp+sfb] > qcOutChanS.SfbSpreadEnergy[sfbGrp+sfb] {
						qcOutChanM.SfbSpreadEnergy[sfbGrp+sfb] = fixmulDDarm8(
							qcOutChanM.SfbEnergy[sfbGrp+sfb], fl2fxconstDBL(float64(float32(0.9))))
					}
				} // if msMask
			}
		}
	}

	// init ahFlag (0: no ah necessary, 1: ah possible, 2: ah active)
	for ch := 0; ch < nChannels; ch++ {
		qcOutChan := qcOutChannel[ch]
		psyOutChan := psyOutChannel[ch]
		for sfbGrp := 0; sfbGrp < psyOutChan.SfbCnt; sfbGrp += psyOutChan.SfbPerGroup {
			for sfb := 0; sfb < psyOutChan.MaxSfbPerGroup; sfb++ {
				if qcOutChan.SfbSpreadEnergy[sfbGrp+sfb] > qcOutChan.SfbEnergy[sfbGrp+sfb] ||
					qcOutChan.SfbMinSnrLdData[sfbGrp+sfb] > 0 {
					ahFlag[ch][sfbGrp+sfb] = noAH
				} else {
					ahFlag[ch][sfbGrp+sfb] = ahInactive
				}
			}
		}
	}
}

// reduceThresholdsCBR is the 1:1 port of FDKaacEnc_reduceThresholdsCBR
// (adj_thr.cpp:988-1051): for each audible non-AH-active sfb, reduce the
// threshold by 4*log(thrExp + redVal) (block-floating-point in the ld64 domain),
// then guard against creating a hole (clamp to minSnr+energy, mark AH_ACTIVE) and
// against exceeding a 29 dB threshold ratio.
//
// redVal is carried in mantissa/exponent form (redVal_m, redVal_e). thrExp is the
// [2][MAX_GROUPED_SFB] per-sfb thrExp matrix.
func reduceThresholdsCBR(qcOutChannel []*QcOutChannel, psyOutChannel []*PsyOutChannel,
	ahFlag [][]uint8, thrExp [][]int32, nChannels int, redValM int32, redValE int32) {

	for ch := 0; ch < nChannels; ch++ {
		qcOutChan := qcOutChannel[ch]
		for sfbGrp := 0; sfbGrp < psyOutChannel[ch].SfbCnt; sfbGrp += psyOutChannel[ch].SfbPerGroup {
			for sfb := 0; sfb < psyOutChannel[ch].MaxSfbPerGroup; sfb++ {
				sfbEnLdData := qcOutChan.SfbWeightedEnergyLdData[sfbGrp+sfb]
				sfbThrLdData := qcOutChan.SfbThresholdLdData[sfbGrp+sfb]
				sfbThrExp := thrExp[ch][sfbGrp+sfb]
				if sfbEnLdData > sfbThrLdData && ahFlag[ch][sfbGrp+sfb] != ahActive {
					// threshold reduction formula:
					//   float tmp = thrExp[ch][sfb]+redVal; tmp *= tmp;
					//   sfbThrReduced = tmp*tmp;
					minScale := fixMin(int(fNorm(sfbThrExp)),
						int(fNorm(redValM))-int(redValE)) - 1

					// 4*log( sfbThrExp + redVal )
					sfbThrReducedLdData := calcLdData(fAbsDBL(
						scaleValue(sfbThrExp, int32(minScale))+
							scaleValue(redValM, redValE+int32(minScale)))) -
						int32(minScale<<(dfractBits-1-ldDataShift))
					sfbThrReducedLdData <<= 2

					// avoid holes
					if sfbThrReducedLdData > (qcOutChan.SfbMinSnrLdData[sfbGrp+sfb]+sfbEnLdData) &&
						ahFlag[ch][sfbGrp+sfb] != noAH {
						if qcOutChan.SfbMinSnrLdData[sfbGrp+sfb] > (fl2fxconstDBL(-1.0) - sfbEnLdData) {
							sfbThrReducedLdData = fMax(
								qcOutChan.SfbMinSnrLdData[sfbGrp+sfb]+sfbEnLdData, sfbThrLdData)
						} else {
							sfbThrReducedLdData = sfbThrLdData
						}
						ahFlag[ch][sfbGrp+sfb] = ahActive
					}

					// minimum of 29 dB Ratio for Thresholds.
					//
					// The C condition (sfbEnLdData + (FIXP_DBL)MAXVAL_DBL) > C
					// signed-overflows for ordinary ld64 sfbEnLdData; at -O2 the
					// genuine FDK compiler exploits signed-overflow UB and folds it
					// to the no-wrap form sfbEnLdData > C - MAXVAL_DBL. Reproduce
					// that observed semantics in int64 (== the optimizer's no-overflow
					// assumption for in-range int32 operands) to stay bit-exact.
					if int64(sfbEnLdData)+int64(maxvalDBL) > int64(fl2fxconstDBL(9.6336206/64.0)) {
						sfbThrReducedLdData = fMax(sfbThrReducedLdData,
							sfbEnLdData-fl2fxconstDBL(9.6336206/64.0))
					}

					qcOutChan.SfbThresholdLdData[sfbGrp+sfb] = sfbThrReducedLdData
				}
			}
		}
	}
}

// calcChaosMeasure is the 1:1 port of FDKaacEnc_calcChaosMeasure
// (adj_thr.cpp:1054-1106, "similar to prepareSfbPe1()"): a per-channel relative
// active-line count (frameNActiveLines / frameNLines) over the audible bands, in
// block-floating-point ld64 arithmetic. Returns 1.0 if no sfb is above threshold
// (total chaos). The shift constants are the SCALE_* #defines.
//
// psyOutChannel->sfbEnergyLdData / sfbThresholdLdData / sfbEnergy alias the
// QC_OUT_CHANNEL memory (interface.h:140); they are held on QcOutChannel in the
// Go model, so qcOutChannel is passed alongside the psy layout.
func calcChaosMeasure(psyOutChannel *PsyOutChannel, qcOutChannel *QcOutChannel,
	sfbFormFactorLdData []int32) int32 {
	const (
		scaleFormFac   = 4  // SCALE_FORM_FAC
		scaleNrgs      = 8  // SCALE_NRGS
		scaleNLines    = 16 // SCALE_NLINES
		scaleNrgsSqrt4 = 2  // SCALE_NRGS_SQRT4 (0.25*SCALE_NRGS)
		scaleNLinesP34 = 12 // SCALE_NLINES_P34 (0.75*SCALE_NLINES)
	)

	var chaosMeasure int32
	frameNLines := 0
	var frameFormFactor int32
	var frameEnergy int32

	for sfbGrp := 0; sfbGrp < psyOutChannel.SfbCnt; sfbGrp += psyOutChannel.SfbPerGroup {
		for sfb := 0; sfb < psyOutChannel.MaxSfbPerGroup; sfb++ {
			if qcOutChannel.SfbEnergyLdData[sfbGrp+sfb] > qcOutChannel.SfbThresholdLdData[sfbGrp+sfb] {
				frameFormFactor += calcInvLdData(sfbFormFactorLdData[sfbGrp+sfb]) >> scaleFormFac
				frameNLines += psyOutChannel.SfbOffsets[sfbGrp+sfb+1] - psyOutChannel.SfbOffsets[sfbGrp+sfb]
				frameEnergy += qcOutChannel.SfbEnergy[sfbGrp+sfb] >> scaleNrgs
			}
		}
	}

	if frameNLines > 0 {
		// frameNActiveLines = frameFormFactor*2^FORM_FAC_SHIFT *
		//   ((frameEnergy*2^SCALE_NRGS)/frameNLines)^-0.25
		// chaosMeasure = frameNActiveLines / frameNLines
		chaosMeasure = calcInvLdData(
			(((calcLdData(frameFormFactor) >> 1) -
				(calcLdData(frameEnergy) >> (2 + 1))) -
				(fMultDiv2(fl2fxconstDBL(0.75),
					calcLdData(int32(frameNLines)<<(dfractBits-1-scaleNLines))) -
					(int32(-((-scaleFormFac + scaleNrgsSqrt4 - formFacShift + scaleNLinesP34) << (dfractBits - 1 - ldDataShift))) >> 1))) << 1)
	} else {
		// assuming total chaos, if no sfb is above thresholds
		chaosMeasure = fl2fxconstDBL(1.0)
	}

	return chaosMeasure
}
