// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Scale-factor estimation — the assimilate passes and the top-level driver,
// ported 1:1 from libAACenc/src/sf_estim.cpp. See sf_estim.go for the file-level
// FP/fixed-point convention and the fMult==fixmulDDarm8 note.
//
// The C operates on PSY_OUT_CHANNEL* / QC_OUT_CHANNEL* pointers. To keep the port
// 1:1 and directly testable against the genuine kernel, the per-channel fields
// these functions touch are gathered into the minimal sfEstimPsyChannel /
// sfEstimQcChannel views below (one Go field per C struct member used). No field
// is renamed or repurposed; the indexing (absolute sfb == sfbGrp+sfb) is exactly
// the C's.

// sfEstimPsyChannel mirrors the PSY_OUT_CHANNEL members sf_estim.cpp reads
// (interface.h PSY_OUT_CHANNEL): the band layout.
type sfEstimPsyChannel struct {
	sfbCnt         int   // sfbCnt
	sfbPerGroup    int   // sfbPerGroup
	maxSfbPerGroup int   // maxSfbPerGroup
	sfbOffsets     []int // sfbOffsets[MAX_GROUPED_SFB+1]
}

// sfEstimQcChannel mirrors the QC_OUT_CHANNEL members sf_estim.cpp reads/writes
// (qc_data.h QC_OUT_CHANNEL): the MDCT spectrum (mutated to zero for empty
// bands), the ld-domain band energies/thresholds and the form factor.
type sfEstimQcChannel struct {
	mdctSpectrum        []int32 // mdctSpectrum[1024]
	sfbEnergyLdData     []int32 // sfbEnergyLdData[MAX_GROUPED_SFB]
	sfbThresholdLdData  []int32 // sfbThresholdLdData[MAX_GROUPED_SFB]
	sfbFormFactorLdData []int32 // sfbFormFactorLdData[MAX_GROUPED_SFB]
}

// assimilateSingleScf is the 1:1 port of FDKaacEnc_assimilateSingleScf
// (sf_estim.cpp:458-623): for every relevant band with a scf bigger than its
// neighbours, try the smaller scf values down to scfMin and keep the one with no
// PE increase and smaller distortion. quantSpec/quantSpecTmp are the full-frame
// (1024) scratch buffers; scf/minScf/sfbDist/sfbConstPePart/.../minScfCalculated
// are MAX_GROUPED_SFB-indexed by absolute sfb.
func assimilateSingleScf(psy *sfEstimPsyChannel, qc *sfEstimQcChannel,
	quantSpec, quantSpecTmp []int16, dZoneQuantEnable bool,
	scf []int, minScf []int, sfbDist, sfbConstPePart, sfbFormFactorLdData, sfbNRelevantLines []int32,
	minScfCalculated []int, restartOnSuccess bool) {
	var sfbLast, sfbAct, sfbNext int
	var scfAct int
	var scfLast, scfNext *int // pointers into scf (or &scfAct)
	var scfMin, scfMax int
	success := false
	var deltaPe int32 // FL2FXCONST_DBL(0.0f)
	var prevScfLast [maxGroupedSFB]int
	var prevScfNext [maxGroupedSFB]int
	var deltaPeLast [maxGroupedSFB]int32

	for i := 0; i < psy.sfbCnt; i++ {
		prevScfLast[i] = fdkIntMax
		prevScfNext[i] = fdkIntMax
		deltaPeLast[i] = int32(fdkIntMax)
	}

	sfbLast = -1
	sfbAct = -1
	sfbNext = -1
	scfLast = nil
	scfNext = nil
	scfMin = fdkIntMax
	scfMax = fdkIntMax

	for {
		// search for new relevant sfb
		sfbNext++
		for (sfbNext < psy.sfbCnt) && (scf[sfbNext] == fdkIntMin) {
			sfbNext++
		}
		if (sfbLast >= 0) && (sfbAct >= 0) && (sfbNext < psy.sfbCnt) {
			// relevant scfs to the left and to the right
			scfAct = scf[sfbAct]
			scfLast = &scf[sfbLast]
			scfNext = &scf[sfbNext]
			scfMin = fixMin(*scfLast, *scfNext)
			scfMax = fixMax(*scfLast, *scfNext)
		} else if (sfbLast == -1) && (sfbAct >= 0) && (sfbNext < psy.sfbCnt) {
			// first relevant scf
			scfAct = scf[sfbAct]
			scfLast = &scfAct
			scfNext = &scf[sfbNext]
			scfMin = *scfNext
			scfMax = *scfNext
		} else if (sfbLast >= 0) && (sfbAct >= 0) && (sfbNext == psy.sfbCnt) {
			// last relevant scf
			scfAct = scf[sfbAct]
			scfLast = &scf[sfbLast]
			scfNext = &scfAct
			scfMin = *scfLast
			scfMax = *scfLast
		}
		if sfbAct >= 0 {
			scfMin = fixMax(scfMin, minScf[sfbAct])
		}

		if (sfbAct >= 0) && (sfbLast >= 0 || sfbNext < psy.sfbCnt) &&
			(scfAct > scfMin) && (scfAct <= scfMin+maxScfDelta) &&
			(scfAct >= scfMax-maxScfDelta) &&
			(scfAct <= fixMin(scfMin, fixMin(*scfLast, *scfNext))+maxScfDelta) &&
			(*scfLast != prevScfLast[sfbAct] || *scfNext != prevScfNext[sfbAct] ||
				deltaPe < deltaPeLast[sfbAct]) {
			// bigger than neighbouring scf found, try to use smaller scf
			success = false

			sfbWidth := psy.sfbOffsets[sfbAct+1] - psy.sfbOffsets[sfbAct]
			sfbOffs := psy.sfbOffsets[sfbAct]

			// estimate required bits for actual scf
			enLdData := qc.sfbEnergyLdData[sfbAct]

			if sfbConstPePart[sfbAct] == int32(fdkIntMin) {
				sfbConstPePart[sfbAct] = ((enLdData - sfbFormFactorLdData[sfbAct] -
					fl2fxconstDBL(0.09375)) >> 1) +
					fl2fxconstDBL(0.02152255861)
			}

			sfbPeOld := calcSingleSpecPe(scfAct, sfbConstPePart[sfbAct], sfbNRelevantLines[sfbAct]) +
				countSingleScfBits(scfAct, *scfLast, *scfNext)

			deltaPeNew := deltaPe
			updateMinScfCalculated := true

			for {
				// estimate required bits for smaller scf
				scfAct--
				// check only if the same check was not done before
				if scfAct < minScfCalculated[sfbAct] && scfAct >= scfMax-maxScfDelta {
					// estimate required bits for new scf
					sfbPeNew := calcSingleSpecPe(scfAct, sfbConstPePart[sfbAct], sfbNRelevantLines[sfbAct]) +
						countSingleScfBits(scfAct, *scfLast, *scfNext)

					// use new scf if no increase in pe and quantization error is smaller
					deltaPeTmp := deltaPe + sfbPeNew - sfbPeOld
					if deltaPeTmp < fl2fxconstDBL(0.0006103515625) {
						// distortion of new scf
						sfbDistNew := fdkaacEncCalcSfbDist(
							qc.mdctSpectrum[sfbOffs:], quantSpecTmp[sfbOffs:],
							sfbWidth, scfAct, dZoneQuantEnable)

						if sfbDistNew < sfbDist[sfbAct] {
							// success, replace scf by new one
							scf[sfbAct] = scfAct
							sfbDist[sfbAct] = sfbDistNew

							for k := 0; k < sfbWidth; k++ {
								quantSpec[sfbOffs+k] = quantSpecTmp[sfbOffs+k]
							}

							deltaPeNew = deltaPeTmp
							success = true
						}
						// mark as already checked
						if updateMinScfCalculated {
							minScfCalculated[sfbAct] = scfAct
						}
					} else {
						// from this scf value on not all new values have been checked
						updateMinScfCalculated = false
					}
				}
				if scfAct <= scfMin {
					break
				}
			}

			deltaPe = deltaPeNew

			// save parameters to avoid multiple computations of the same sfb
			prevScfLast[sfbAct] = *scfLast
			prevScfNext[sfbAct] = *scfNext
			deltaPeLast[sfbAct] = deltaPe
		}

		if success && restartOnSuccess {
			// start again at first sfb
			sfbLast = -1
			sfbAct = -1
			sfbNext = -1
			scfLast = nil
			scfNext = nil
			scfMin = fdkIntMax
			scfMax = fdkIntMax
			success = false
		} else {
			// shift sfbs for next band
			sfbLast = sfbAct
			sfbAct = sfbNext
		}
		if sfbNext >= psy.sfbCnt {
			break
		}
	}
}

// assimilateMultipleScf is the 1:1 port of FDKaacEnc_assimilateMultipleScf
// (sf_estim.cpp:629-752): coarsen whole regions of equal-or-bigger scf to a
// common smaller scfAct when the PE budget and summed distortion both allow it.
func assimilateMultipleScf(psy *sfEstimPsyChannel, qc *sfEstimQcChannel,
	quantSpec, quantSpecTmp []int16, dZoneQuantEnable bool,
	scf []int, minScf []int, sfbDist, sfbConstPePart, sfbFormFactorLdData, sfbNRelevantLines []int32) {
	var scfTmp [maxGroupedSFB]int
	var sfbDistNew [maxGroupedSFB]int32
	var deltaPe int32 // FL2FXCONST_DBL(0.0f)
	sfbCnt := psy.sfbCnt

	// calc min and max scalfactors
	scfMin := fdkIntMax
	scfMax := fdkIntMin
	for sfb := 0; sfb < sfbCnt; sfb++ {
		if scf[sfb] != fdkIntMin {
			scfMin = fixMin(scfMin, scf[sfb])
			scfMax = fixMax(scfMax, scf[sfb])
		}
	}

	if scfMax != fdkIntMin && scfMax <= scfMin+maxScfDelta {
		scfAct := scfMax

		for {
			// try smaller scf
			scfAct--
			for i := 0; i < maxGroupedSFB; i++ {
				scfTmp[i] = scf[i]
			}
			stopSfb := 0
			for {
				// search for region where all scfs are bigger than scfAct
				sfb := stopSfb
				for sfb < sfbCnt && (scf[sfb] == fdkIntMin || scf[sfb] <= scfAct) {
					sfb++
				}
				startSfb := sfb
				sfb++
				for sfb < sfbCnt && (scf[sfb] == fdkIntMin || scf[sfb] > scfAct) {
					sfb++
				}
				stopSfb = sfb

				// check if in all sfb of a valid region scfAct >= minScf[sfb]
				possibleRegionFound := false
				if startSfb < sfbCnt {
					possibleRegionFound = true
					for sfb = startSfb; sfb < stopSfb; sfb++ {
						if scf[sfb] != fdkIntMin {
							if scfAct < minScf[sfb] {
								possibleRegionFound = false
								break
							}
						}
					}
				}

				if possibleRegionFound { // region found
					// replace scfs in region by scfAct
					for sfb = startSfb; sfb < stopSfb; sfb++ {
						if scfTmp[sfb] != fdkIntMin {
							scfTmp[sfb] = scfAct
						}
					}

					// estimate change in bit demand for new scfs
					deltaScfBits := countScfBitsDiff(scf, scfTmp[:], sfbCnt, startSfb, stopSfb)

					deltaSpecPe := calcSpecPeDiff(qc.sfbEnergyLdData, scf, scfTmp[:],
						sfbConstPePart, sfbFormFactorLdData, sfbNRelevantLines, startSfb, stopSfb)

					deltaPeNew := deltaPe + deltaScfBits + deltaSpecPe

					// new bit demand small enough ?
					if deltaPeNew < fl2fxconstDBL(0.0006103515625) {
						// quantize and calc sum of new distortion
						var distOldSum, distNewSum int32 // FL2FXCONST_DBL(0.0f)
						for sfb = startSfb; sfb < stopSfb; sfb++ {
							if scfTmp[sfb] != fdkIntMin {
								distOldSum += calcInvLdData(sfbDist[sfb]) >> distFacShift

								sfbWidth := psy.sfbOffsets[sfb+1] - psy.sfbOffsets[sfb]
								sfbOffs := psy.sfbOffsets[sfb]

								sfbDistNew[sfb] = fdkaacEncCalcSfbDist(
									qc.mdctSpectrum[sfbOffs:], quantSpecTmp[sfbOffs:],
									sfbWidth, scfAct, dZoneQuantEnable)

								if sfbDistNew[sfb] > qc.sfbThresholdLdData[sfb] {
									// no improvement, skip further dist. calculations
									distNewSum = distOldSum << 1
									break
								}
								distNewSum += calcInvLdData(sfbDistNew[sfb]) >> distFacShift
							}
						}
						// distortion smaller ? -> use new scalefactors
						if distNewSum < distOldSum {
							deltaPe = deltaPeNew
							for sfb = startSfb; sfb < stopSfb; sfb++ {
								if scf[sfb] != fdkIntMin {
									sfbWidth := psy.sfbOffsets[sfb+1] - psy.sfbOffsets[sfb]
									sfbOffs := psy.sfbOffsets[sfb]
									scf[sfb] = scfAct
									sfbDist[sfb] = sfbDistNew[sfb]

									for k := 0; k < sfbWidth; k++ {
										quantSpec[sfbOffs+k] = quantSpecTmp[sfbOffs+k]
									}
								}
							}
						}
					}
				}

				if stopSfb > sfbCnt {
					break
				}
			}

			if scfAct <= scfMin {
				break
			}
		}
	}
}

// assimilateMultipleScf2 is the 1:1 port of
// FDKaacEnc_FDKaacEnc_assimilateMultipleScf2 (sf_estim.cpp:758-1044): the
// three-stage region optimizer — (1) coarser quantization within an allowed-
// distortion bound, (2) finer quantization reducing scf-coding bits, (3) reduce
// scf without requantization — over regions of equal scf.
func assimilateMultipleScf2(psy *sfEstimPsyChannel, qc *sfEstimQcChannel,
	quantSpec, quantSpecTmp []int16, dZoneQuantEnable bool,
	scf []int, minScf []int, sfbDist, sfbConstPePart, sfbFormFactorLdData, sfbNRelevantLines []int32) {
	var scfTmp [maxGroupedSFB]int
	var sfbDistNew [maxGroupedSFB]int32
	var sfbDistMax [maxGroupedSFB]int32
	sfbOffs := psy.sfbOffsets
	var deltaPe int32    // FL2FXCONST_DBL(0.0f)
	var deltaPeNew int32 // FL2FXCONST_DBL(0.0f)
	sfbCnt := psy.sfbCnt

	// calc min and max scalfactors
	scfMin := fdkIntMax
	scfMax := fdkIntMin
	for sfb := 0; sfb < sfbCnt; sfb++ {
		if scf[sfb] != fdkIntMin {
			scfMin = fixMin(scfMin, scf[sfb])
			scfMax = fixMax(scfMax, scf[sfb])
		}
	}

	stopSfb := 0
	scfAct := fdkIntMin
	for {
		// search for region with same scf values scfAct
		scfPrev := scfAct

		sfb := stopSfb
		for sfb < sfbCnt && (scf[sfb] == fdkIntMin) {
			sfb++
		}
		startSfb := sfb
		scfAct = scf[startSfb]
		sfb++
		for sfb < sfbCnt && ((scf[sfb] == fdkIntMin) || (scf[sfb] == scf[startSfb])) {
			sfb++
		}
		stopSfb = sfb

		var scfNext int
		if stopSfb < sfbCnt {
			scfNext = scf[stopSfb]
		} else {
			scfNext = scfAct
		}

		if scfPrev == fdkIntMin {
			scfPrev = scfAct
		}

		scfPrevNextMax := fixMax(scfPrev, scfNext)
		scfPrevNextMin := fixMin(scfPrev, scfNext)

		// try to reduce bits by checking scf values in the range scf[startSfb]...scfHi
		scfHi := fixMax(scfPrevNextMax, scfAct)
		// try to find a better solution by reducing the scf difference to the nearest possible lower scf
		var scfLo int
		if scfPrevNextMax >= scfAct {
			scfLo = fixMin(scfAct, scfPrevNextMin)
		} else {
			scfLo = scfPrevNextMax
		}

		if startSfb < sfbCnt && scfHi-scfLo <= maxScfDelta { // region found
			// 1. try to save bits by coarser quantization
			if scfHi > scf[startSfb] {
				// calculate the allowed distortion
				for sfb = startSfb; sfb < stopSfb; sfb++ {
					if scf[sfb] != fdkIntMin {
						sfbDistMax[sfb] = fixmulDDarm8(fl2fxconstDBL(1.0/3.0), qc.sfbThresholdLdData[sfb]) +
							fixmulDDarm8(fl2fxconstDBL(1.0/3.0), sfbDist[sfb]) +
							fixmulDDarm8(fl2fxconstDBL(1.0/3.0), sfbDist[sfb])
						sfbDistMax[sfb] = fMax(sfbDistMax[sfb],
							qc.sfbEnergyLdData[sfb]-fl2fxconstDBL(0.15571537944))
						sfbDistMax[sfb] = fMin(sfbDistMax[sfb], qc.sfbThresholdLdData[sfb])
					}
				}

				// loop over all possible scf values for this region
				bCheckScf := true
				for scfNew := scf[startSfb] + 1; scfNew <= scfHi; scfNew++ {
					for k := 0; k < maxGroupedSFB; k++ {
						scfTmp[k] = scf[k]
					}

					// replace scfs in region by scfNew
					for sfb = startSfb; sfb < stopSfb; sfb++ {
						if scfTmp[sfb] != fdkIntMin {
							scfTmp[sfb] = scfNew
						}
					}

					// estimate change in bit demand for new scfs
					deltaScfBits := countScfBitsDiff(scf, scfTmp[:], sfbCnt, startSfb, stopSfb)

					deltaSpecPe := calcSpecPeDiff(qc.sfbEnergyLdData, scf, scfTmp[:],
						sfbConstPePart, sfbFormFactorLdData, sfbNRelevantLines, startSfb, stopSfb)

					deltaPeNew = deltaPe + deltaScfBits + deltaSpecPe

					// new bit demand small enough ?
					if deltaPeNew < fl2fxconstDBL(0.0) {
						bSuccess := true

						// quantize and calc sum of new distortion
						for sfb = startSfb; sfb < stopSfb; sfb++ {
							if scfTmp[sfb] != fdkIntMin {
								sfbDistNew[sfb] = fdkaacEncCalcSfbDist(
									qc.mdctSpectrum[sfbOffs[sfb]:], quantSpecTmp[sfbOffs[sfb]:],
									sfbOffs[sfb+1]-sfbOffs[sfb], scfNew, dZoneQuantEnable)

								if sfbDistNew[sfb] > sfbDistMax[sfb] {
									// no improvement, skip further dist. calculations
									bSuccess = false
									if sfbDistNew[sfb] == qc.sfbEnergyLdData[sfb] {
										// whole sfb already quantized to 0; coarser useless
										bCheckScf = false
									}
									break
								}
							}
						}
						if !bCheckScf { // further calculations useless ?
							break
						}
						// distortion small enough ? -> use new scalefactors
						if bSuccess {
							deltaPe = deltaPeNew
							for sfb = startSfb; sfb < stopSfb; sfb++ {
								if scf[sfb] != fdkIntMin {
									scf[sfb] = scfNew
									sfbDist[sfb] = sfbDistNew[sfb]

									for k := 0; k < sfbOffs[sfb+1]-sfbOffs[sfb]; k++ {
										quantSpec[sfbOffs[sfb]+k] = quantSpecTmp[sfbOffs[sfb]+k]
									}
								}
							}
						}
					}
				}
			}

			// 2. only if coarser quantization was not successful, try finer quant + reduce scf coding bits
			if scfAct == scf[startSfb] && scfLo < scfAct && scfMax-scfMin <= maxScfDelta {
				bminScfViolation := false

				for k := 0; k < maxGroupedSFB; k++ {
					scfTmp[k] = scf[k]
				}

				scfNew := scfLo

				// replace scfs in region by scfNew and check scfNew >= minScf[sfb]
				for sfb = startSfb; sfb < stopSfb; sfb++ {
					if scfTmp[sfb] != fdkIntMin {
						scfTmp[sfb] = scfNew
						if scfNew < minScf[sfb] {
							bminScfViolation = true
						}
					}
				}

				if !bminScfViolation {
					// estimate change in bit demand for new scfs
					deltaScfBits := countScfBitsDiff(scf, scfTmp[:], sfbCnt, startSfb, stopSfb)

					deltaSpecPe := calcSpecPeDiff(qc.sfbEnergyLdData, scf, scfTmp[:],
						sfbConstPePart, sfbFormFactorLdData, sfbNRelevantLines, startSfb, stopSfb)

					deltaPeNew = deltaPe + deltaScfBits + deltaSpecPe
				}

				// new bit demand small enough ?
				if !bminScfViolation && deltaPeNew < fl2fxconstDBL(0.0) {
					// quantize and calc sum of new distortion
					var distOldSum, distNewSum int32 // FL2FXCONST_DBL(0.0f)
					for sfb = startSfb; sfb < stopSfb; sfb++ {
						if scfTmp[sfb] != fdkIntMin {
							distOldSum += calcInvLdData(sfbDist[sfb]) >> distFacShift

							sfbDistNew[sfb] = fdkaacEncCalcSfbDist(
								qc.mdctSpectrum[sfbOffs[sfb]:], quantSpecTmp[sfbOffs[sfb]:],
								sfbOffs[sfb+1]-sfbOffs[sfb], scfNew, dZoneQuantEnable)

							if sfbDistNew[sfb] > qc.sfbThresholdLdData[sfb] {
								// no improvement, skip further dist. calculations
								distNewSum = distOldSum << 1
								break
							}
							distNewSum += calcInvLdData(sfbDistNew[sfb]) >> distFacShift
						}
					}
					// distortion smaller ? -> use new scalefactors
					if distNewSum < fixmulDDarm8(fl2fxconstDBL(0.8), distOldSum) {
						deltaPe = deltaPeNew
						for sfb = startSfb; sfb < stopSfb; sfb++ {
							if scf[sfb] != fdkIntMin {
								scf[sfb] = scfNew
								sfbDist[sfb] = sfbDistNew[sfb]

								for k := 0; k < sfbOffs[sfb+1]-sfbOffs[sfb]; k++ {
									quantSpec[sfbOffs[sfb]+k] = quantSpecTmp[sfbOffs[sfb]+k]
								}
							}
						}
					}
				}
			}

			// 3. try to save bits by only reducing the scalefactor without new quantization
			if scfMax-scfMin <= maxScfDelta-3 { // 3 bec. scf is reduced 3 times below
				for k := 0; k < sfbCnt; k++ {
					scfTmp[k] = scf[k]
				}

				for i := 0; i < 3; i++ {
					scfNew := scfTmp[startSfb] - 1
					// replace scfs in region by scfNew
					for sfb = startSfb; sfb < stopSfb; sfb++ {
						if scfTmp[sfb] != fdkIntMin {
							scfTmp[sfb] = scfNew
						}
					}
					// estimate change in bit demand for new scfs
					deltaScfBits := countScfBitsDiff(scf, scfTmp[:], sfbCnt, startSfb, stopSfb)
					deltaPeNew = deltaPe + deltaScfBits
					// new bit demand small enough ?
					if deltaPeNew <= fl2fxconstDBL(0.0) {
						bSuccess := true
						var distOldSum, distNewSum int32 // FL2FXCONST_DBL(0.0f)
						for sfb = startSfb; sfb < stopSfb; sfb++ {
							if scfTmp[sfb] != fdkIntMin {
								// calc energy and distortion of the quantized spectrum for a smaller scf
								sfbEnQ, dNew := fdkaacEncCalcSfbQuantEnergyAndDist(
									qc.mdctSpectrum[sfbOffs[sfb]:], quantSpec[sfbOffs[sfb]:],
									sfbOffs[sfb+1]-sfbOffs[sfb], scfNew)
								sfbDistNew[sfb] = dNew

								distOldSum += calcInvLdData(sfbDist[sfb]) >> distFacShift
								distNewSum += calcInvLdData(sfbDistNew[sfb]) >> distFacShift

								if (sfbDistNew[sfb] > (sfbDist[sfb] + fl2fxconstDBL(0.00259488556167))) ||
									(sfbEnQ < (qc.sfbEnergyLdData[sfb] - fl2fxconstDBL(0.00778722686652))) {
									bSuccess = false
									break
								}
							}
						}
						// distortion smaller ? -> use new scalefactors
						if distNewSum < distOldSum && bSuccess {
							deltaPe = deltaPeNew
							for sfb = startSfb; sfb < stopSfb; sfb++ {
								if scf[sfb] != fdkIntMin {
									scf[sfb] = scfNew
									sfbDist[sfb] = sfbDistNew[sfb]
								}
							}
						}
					}
				}
			}
		}

		if stopSfb > sfbCnt {
			break
		}
	}
}

// estimateScaleFactorsChannel is the 1:1 port of
// FDKaacEnc_EstimateScaleFactorsChannel (sf_estim.cpp:1046-1277): derive the
// initial integer scf per band from the adjusted thresholds/energies, refine via
// the assimilate/improve passes when invQuant > 0, limit the scf delta, and emit
// the loop scalefactors + globalGain. scf (MAX_GROUPED_SFB) and quantSpec (1024)
// and globalGain are the outputs; sfbFormFactorLdData is read.
func estimateScaleFactorsChannel(qc *sfEstimQcChannel, psy *sfEstimPsyChannel,
	scf []int, sfbFormFactorLdData []int32, invQuant int,
	quantSpec []int16, dZoneQuantEnable bool) (globalGain int) {
	var minScfCalculated [maxGroupedSFB]int
	var sfbDistLdData [maxGroupedSFB]int32
	var quantSpecTmp [1024]int16
	var minSfMaxQuant [maxGroupedSFB]int

	threshConstLdData := fl2fxconstDBL(0.04304511722) // log10(6.75)/log10(2)/64
	convConst := fl2fxconstDBL(0.30102999566)         // log10(2.0)
	c1Const := fl2fxconstDBL(-0.27083183594)          // C1 = -69.33295 => C1/2^8

	if invQuant > 0 {
		for i := range quantSpec[:1024] {
			quantSpec[i] = 0
		}
	}

	// scfs without energy or with thresh>energy are marked with FDK_INT_MIN
	for i := 0; i < psy.sfbCnt; i++ {
		scf[i] = fdkIntMin
	}

	for i := 0; i < maxGroupedSFB; i++ {
		minSfMaxQuant[i] = fdkIntMin
	}

	for sfbOffs := 0; sfbOffs < psy.sfbCnt; sfbOffs += psy.sfbPerGroup {
		for sfb := 0; sfb < psy.maxSfbPerGroup; sfb++ {
			threshLdData := qc.sfbThresholdLdData[sfbOffs+sfb]
			energyLdData := qc.sfbEnergyLdData[sfbOffs+sfb]

			sfbDistLdData[sfbOffs+sfb] = energyLdData

			if energyLdData > threshLdData {
				// energyPart (0.09375f = scale of sfbFormFactorLdData)
				energyPartLdData := sfbFormFactorLdData[sfbOffs+sfb] + fl2fxconstDBL(0.09375)

				// influence of allowed distortion
				thresholdPartLdData := threshConstLdData + threshLdData

				// scf calc
				scfFract := thresholdPartLdData - energyPartLdData
				// conversion from log2 to log10
				scfFract = fixmulDDarm8(convConst, scfFract)
				// (8.8585f * scfFract)/8
				scfFract = scfFract + fixmulDDarm8(fl2fxconstDBL(0.8585), scfFract>>3)

				// integer scalefactor (3 bits => /8.0; 6 bits => ld64)
				scfInt := int(scfFract >> ((dfractBits - 1) - 3 - ldDataShift))

				// maximum of spectrum
				var maxSpec int32 // FL2FXCONST_DBL(0.0f)

				for j := psy.sfbOffsets[sfbOffs+sfb]; j < psy.sfbOffsets[sfbOffs+sfb+1]; j += 4 {
					maxSpec = fMax(maxSpec,
						fMax(fMax(fixabsD(qc.mdctSpectrum[j+0]), fixabsD(qc.mdctSpectrum[j+1])),
							fMax(fixabsD(qc.mdctSpectrum[j+2]), fixabsD(qc.mdctSpectrum[j+3]))))
				}
				// lower scf limit to avoid quantized values bigger than MAX_QUANT
				tmp := calcLdData(maxSpec)
				if c1Const > fl2fxconstDBL(-1.0)-tmp {
					minSfMaxQuant[sfbOffs+sfb] = int((c1Const+tmp)>>((dfractBits-1)-8)) + 1
				} else {
					minSfMaxQuant[sfbOffs+sfb] = int(fl2fxconstDBL(-1.0)>>((dfractBits-1)-8)) + 1
				}

				scfInt = fixMax(scfInt, minSfMaxQuant[sfbOffs+sfb])

				// find better scalefactor with analysis by synthesis
				if invQuant > 0 {
					off := psy.sfbOffsets[sfbOffs+sfb]
					width := psy.sfbOffsets[sfbOffs+sfb+1] - psy.sfbOffsets[sfbOffs+sfb]
					var d int32
					var msc int
					scfInt, d, msc = improveScf(
						qc.mdctSpectrum[off:], quantSpec[off:], quantSpecTmp[off:],
						width, threshLdData, scfInt, minSfMaxQuant[sfbOffs+sfb], dZoneQuantEnable)
					sfbDistLdData[sfbOffs+sfb] = d
					minScfCalculated[sfbOffs+sfb] = msc
				}
				scf[sfbOffs+sfb] = scfInt
			}
		}
	}

	if invQuant > 0 {
		// try to decrease scf differences
		var sfbConstPePart [maxGroupedSFB]int32
		var sfbNRelevantLines [maxGroupedSFB]int32

		for i := 0; i < psy.sfbCnt; i++ {
			sfbConstPePart[i] = int32(fdkIntMin)
		}

		calcSfbRelevantLines(sfbFormFactorLdData, qc.sfbEnergyLdData, qc.sfbThresholdLdData,
			psy.sfbOffsets, psy.sfbCnt, psy.sfbPerGroup, psy.maxSfbPerGroup, sfbNRelevantLines[:])

		assimilateSingleScf(psy, qc, quantSpec, quantSpecTmp[:], dZoneQuantEnable,
			scf, minSfMaxQuant[:], sfbDistLdData[:], sfbConstPePart[:], sfbFormFactorLdData,
			sfbNRelevantLines[:], minScfCalculated[:], true)

		if invQuant > 1 {
			assimilateMultipleScf(psy, qc, quantSpec, quantSpecTmp[:], dZoneQuantEnable,
				scf, minSfMaxQuant[:], sfbDistLdData[:], sfbConstPePart[:], sfbFormFactorLdData,
				sfbNRelevantLines[:])

			assimilateMultipleScf2(psy, qc, quantSpec, quantSpecTmp[:], dZoneQuantEnable,
				scf, minSfMaxQuant[:], sfbDistLdData[:], sfbConstPePart[:], sfbFormFactorLdData,
				sfbNRelevantLines[:])
		}
	}

	// get min scalefac
	minSf := fdkIntMax
	for sfbOffs := 0; sfbOffs < psy.sfbCnt; sfbOffs += psy.sfbPerGroup {
		for sfb := 0; sfb < psy.maxSfbPerGroup; sfb++ {
			if scf[sfbOffs+sfb] != fdkIntMin {
				minSf = fixMin(minSf, scf[sfbOffs+sfb])
			}
		}
	}

	// limit scf delta
	for sfbOffs := 0; sfbOffs < psy.sfbCnt; sfbOffs += psy.sfbPerGroup {
		for sfb := 0; sfb < psy.maxSfbPerGroup; sfb++ {
			if (scf[sfbOffs+sfb] != fdkIntMin) && (minSf+maxScfDelta) < scf[sfbOffs+sfb] {
				scf[sfbOffs+sfb] = minSf + maxScfDelta
				if invQuant > 0 { // changed bands need to be quantized again
					off := psy.sfbOffsets[sfbOffs+sfb]
					width := psy.sfbOffsets[sfbOffs+sfb+1] - psy.sfbOffsets[sfbOffs+sfb]
					sfbDistLdData[sfbOffs+sfb] = fdkaacEncCalcSfbDist(
						qc.mdctSpectrum[off:], quantSpec[off:], width, scf[sfbOffs+sfb], dZoneQuantEnable)
				}
			}
		}
	}

	// get max scalefac for global gain
	maxSf := fdkIntMin
	for sfbOffs := 0; sfbOffs < psy.sfbCnt; sfbOffs += psy.sfbPerGroup {
		for sfb := 0; sfb < psy.maxSfbPerGroup; sfb++ {
			maxSf = fixMax(maxSf, scf[sfbOffs+sfb])
		}
	}

	// calc loop scalefactors, if spec is not all zero (i.e. maxSf == -99)
	if maxSf > fdkIntMin {
		globalGain = maxSf
		for sfbOffs := 0; sfbOffs < psy.sfbCnt; sfbOffs += psy.sfbPerGroup {
			for sfb := 0; sfb < psy.maxSfbPerGroup; sfb++ {
				if scf[sfbOffs+sfb] == fdkIntMin {
					scf[sfbOffs+sfb] = 0
					// set band explicitely to zero
					for j := psy.sfbOffsets[sfbOffs+sfb]; j < psy.sfbOffsets[sfbOffs+sfb+1]; j++ {
						qc.mdctSpectrum[j] = 0
					}
				} else {
					scf[sfbOffs+sfb] = maxSf - scf[sfbOffs+sfb]
				}
			}
		}
	} else {
		globalGain = 0
		// set spectrum explicitely to zero
		for sfbOffs := 0; sfbOffs < psy.sfbCnt; sfbOffs += psy.sfbPerGroup {
			for sfb := 0; sfb < psy.maxSfbPerGroup; sfb++ {
				scf[sfbOffs+sfb] = 0
				for j := psy.sfbOffsets[sfbOffs+sfb]; j < psy.sfbOffsets[sfbOffs+sfb+1]; j++ {
					qc.mdctSpectrum[j] = 0
				}
			}
		}
	}

	return globalGain
}

// CalcFormFactor is the 1:1 port of FDKaacEnc_CalcFormFactor (sf_estim.cpp:166-
// 174): for each channel it runs FDKaacEnc_FDKaacEnc_CalcFormFactorChannel
// (calcFormFactorChannel) over the channel's MDCT spectrum, filling that
// channel's QcOutChannel.SfbFormFactorLdData. mdctSpectrum[ch] is the per-channel
// MDCT line buffer (the C aliases PSY_OUT_CHANNEL.mdctSpectrum onto
// QC_OUT_CHANNEL.mdctSpectrum; here it is passed alongside since the Go struct
// does not embed the 1024-sample buffer).
func CalcFormFactor(qcOutChannel []*QcOutChannel, psyOutChannel []*PsyOutChannel,
	mdctSpectrum [][]int32, nChannels int) {
	for j := 0; j < nChannels; j++ {
		psy := psyOutChannel[j]
		calcFormFactorChannel(qcOutChannel[j].SfbFormFactorLdData[:],
			mdctSpectrum[j], psy.SfbOffsets[:],
			psy.SfbCnt, psy.SfbPerGroup, psy.MaxSfbPerGroup)
	}
}

// EstimateScaleFactors is the 1:1 port of FDKaacEnc_EstimateScaleFactors
// (sf_estim.cpp:1279-1290): for each channel it runs
// FDKaacEnc_EstimateScaleFactorsChannel (estimateScaleFactorsChannel), deriving
// the per-band integer scalefactors (QcOutChannel.Scf), the global gain
// (QcOutChannel.GlobalGain) and the quantized spectrum (QcOutChannel.QuantSpec)
// from the channel's adjusted thresholds/energies, form factor and MDCT
// spectrum. mdctSpectrum[ch] is the per-channel MDCT line buffer (mutated in
// place: empty bands are zeroed).
func EstimateScaleFactors(psyOutChannel []*PsyOutChannel, qcOutChannel []*QcOutChannel,
	mdctSpectrum [][]int32, invQuant int, dZoneQuantEnable bool, nChannels int) {
	for ch := 0; ch < nChannels; ch++ {
		qc := qcOutChannel[ch]
		psy := psyOutChannel[ch]
		qcView := &sfEstimQcChannel{
			mdctSpectrum:        mdctSpectrum[ch],
			sfbEnergyLdData:     qc.SfbEnergyLdData[:],
			sfbThresholdLdData:  qc.SfbThresholdLdData[:],
			sfbFormFactorLdData: qc.SfbFormFactorLdData[:],
		}
		psyView := &sfEstimPsyChannel{
			sfbCnt:         psy.SfbCnt,
			sfbPerGroup:    psy.SfbPerGroup,
			maxSfbPerGroup: psy.MaxSfbPerGroup,
			sfbOffsets:     psy.SfbOffsets[:],
		}
		qc.GlobalGain = estimateScaleFactorsChannel(qcView, psyView,
			qc.Scf[:], qc.SfbFormFactorLdData[:], invQuant,
			qc.QuantSpec[:], dZoneQuantEnable)
	}
}
