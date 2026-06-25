// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file is the pure-Go 1:1 port of the QMF-energy + noise-floor + envelope
// leaf kernels of the Fraunhofer FDK-AAC SBR-encoder envelope estimator,
// libSBRenc/src/env_est.cpp:
//   - FDKsbrEnc_getEnergyFromCplxQmfData / ...Full (the QMF subband-energy
//     extraction that also rescales the QMF data in place),
//   - FDKsbrEnc_GetTonality (mean tonality of the 5 highest-energy bands),
//   - mapPanorama / sbrNoiseFloorLevelsQuantisation / coupleNoiseFloor (noise
//     floor quantisation + stereo coupling),
//   - getEnvSfbEnergy (per-SFB energy summation over a slot range),
//   - mhLoweringEnergy / nmhLoweringEnergy (missing-harmonic energy compensation).
//
// These are the load-bearing leaves that turn the input-signal QMF into the SBR
// envelope/noise energies the grid (fram_gen) and MH detector (mh_det) consume.
// The full multi-stereo-mode envelope assembler calculateSbrEnvelope and the
// bitstream-coupled extractSbrEnvelope1/2 orchestrators are OUT of this batch's
// scope (they pull in ton_corr / code_env / bit_sbr) and are noted as excluded.
//
// fdk-aac SBR is FIXED-POINT: EXACT integer parity. Shared libFDK kernels
// (fPow2Div2/fPow2AddDiv2, getScalefactor, scaleValues, CountLeadingBits,
// CalcLdInt, CalcLdData/CalcInvLdData, fMult, fDivNorm, GetInvInt, fAddSaturate)
// are reused bit-for-bit from internal/nativeaac.
package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// env_est.cpp constants.
const (
	envYNrgScale     = 5  // Y_NRG_SCALE (env_est.cpp:116)
	envMaxNrgSlotsLD = 16 // MAX_NRG_SLOTS_LD (env_est.cpp:117)
	envFractBits     = 32 // FRACT_BITS
	fdkIntMax        = int(^uint(0) >> 1)
)

// panTable / maxIndex, 1:1 from env_est.cpp:119-121.
var (
	panTable  = [2][9]uint8{{0, 2, 4, 6, 8, 12, 16, 20, 24}, {0, 2, 4, 8, 12, 0, 0, 0, 0}}
	maxIndexT = [2]int{9, 5}
)

// noiseFloorOffset64 == NOISE_FLOOR_OFFSET_64 (sbr_def.h:166).
func noiseFloorOffset64() int32 { return fl2f(0.09375) }

// GetEnergyFromCplxQmfData is the 1:1 port of FDKsbrEnc_getEnergyFromCplxQmfData
// (env_est.cpp:242-337): per timeslot-pair QMF energy, rescaling the QMF in
// place. realValues/imagValues are numberCols rows of numberBands; energyValues
// holds numberCols/2 rows of numberBands. qmfScale/energyScale are in/out.
func GetEnergyFromCplxQmfData(energyValues, realValues, imagValues [][]int32, numberBands, numberCols int, qmfScale, energyScale *int) {
	maxVal := int32(0)
	tmpNrg := make([]int32, 32*64/2)

	scale := dfractBits
	for k := 0; k < numberCols; k++ {
		scale = nativeaac.FMinI(scale, nativeaac.FMinI(nativeaac.GetScalefactor(realValues[k], numberBands), nativeaac.GetScalefactor(imagValues[k], numberBands)))
	}
	if scale >= dfractBits-1 {
		scale = (envFractBits - 1 - *qmfScale)
	}
	scale = nativeaac.FMaxI(0, scale-1)
	*qmfScale += scale

	nrgIdx := 0
	for k := 0; k < numberCols; k += 2 {
		r0 := realValues[k]
		i0 := imagValues[k]
		r1 := realValues[k+1]
		i1 := imagValues[k+1]
		for j := 0; j < numberBands; j++ {
			tr0 := r0[j]
			tr1 := r1[j]
			ti0 := i0[j]
			ti1 := i1[j]

			tr0 <<= uint(scale)
			ti0 <<= uint(scale)
			energy := nativeaac.FPow2AddDiv2(nativeaac.FPow2Div2(tr0), ti0) >> 1

			tr1 <<= uint(scale)
			ti1 <<= uint(scale)
			energy += nativeaac.FPow2AddDiv2(nativeaac.FPow2Div2(tr1), ti1) >> 1

			tmpNrg[nrgIdx] = energy
			nrgIdx++
			maxVal = nativeaac.FMaxDBL(maxVal, energy)

			r0[j] = tr0
			r1[j] = tr1
			i0[j] = ti0
			i1[j] = ti1
		}
	}
	*energyScale = 2*(*qmfScale) - 1

	scale = int(nativeaac.CountLeadingBits(maxVal))
	// scaleValues(dst, src, n, scale) (scale.cpp): copy from tmpNrg into the
	// output rows with a logical-by-sign shift.
	nrgOff := 0
	for k := 0; k < numberCols>>1; k++ {
		for j := 0; j < numberBands; j++ {
			energyValues[k][j] = nativeaac.ScaleValue(tmpNrg[nrgOff+j], int32(scale))
		}
		nrgOff += numberBands
	}
	*energyScale += scale
}

// GetEnergyFromCplxQmfDataFull is the 1:1 port of
// FDKsbrEnc_getEnergyFromCplxQmfDataFull (env_est.cpp:340-427): per-timeslot QMF
// energy (no pairing). energyValues holds numberCols rows.
func GetEnergyFromCplxQmfDataFull(energyValues, realValues, imagValues [][]int32, numberBands, numberCols int, qmfScale, energyScale *int) {
	maxVal := int32(0)
	tmpNrg := make([]int32, envMaxNrgSlotsLD*64)

	scale := dfractBits
	for k := 0; k < numberCols; k++ {
		scale = nativeaac.FMinI(scale, nativeaac.FMinI(nativeaac.GetScalefactor(realValues[k], numberBands), nativeaac.GetScalefactor(imagValues[k], numberBands)))
	}
	if scale >= dfractBits-1 {
		scale = (envFractBits - 1 - *qmfScale)
	}
	scale = nativeaac.FMaxI(0, scale-1)
	*qmfScale += scale

	nrgIdx := 0
	for k := 0; k < numberCols; k++ {
		r0 := realValues[k]
		i0 := imagValues[k]
		for j := 0; j < numberBands; j++ {
			tr0 := r0[j]
			ti0 := i0[j]
			tr0 <<= uint(scale)
			ti0 <<= uint(scale)
			energy := nativeaac.FPow2AddDiv2(nativeaac.FPow2Div2(tr0), ti0)
			tmpNrg[nrgIdx] = energy
			nrgIdx++
			maxVal = nativeaac.FMaxDBL(maxVal, energy)
			r0[j] = tr0
			i0[j] = ti0
		}
	}
	*energyScale = 2*(*qmfScale) - 1

	scale = int(nativeaac.CountLeadingBits(maxVal))
	nrgOff := 0
	for k := 0; k < numberCols; k++ {
		for j := 0; j < numberBands; j++ {
			energyValues[k][j] = nativeaac.ScaleValue(tmpNrg[nrgOff+j], int32(scale))
		}
		nrgOff += numberBands
	}
	*energyScale += scale
}

// GetTonality is the 1:1 port of FDKsbrEnc_GetTonality (env_est.cpp:144-230):
// mean tonality of the 5 highest-energy bands.
func GetTonality(quotaMatrix [][]int32, noEstPerFrame, startIndex int, energies [][]int32, startBand, stopBand, numberCols int) int32 {
	const sbrMaxEnergyValues = 5
	noEnMaxBand := [sbrMaxEnergyValues]int{-1, -1, -1, -1, -1}
	var energyMax [sbrMaxEnergyValues]int32
	var tonalityBand [sbrMaxEnergyValues]int32
	globalTonality := int32(0)
	var energyBand [64]int32

	if numberCols == 15 {
		for b := startBand; b < stopBand; b++ {
			energyBand[b] = 0
		}
	} else {
		for b := startBand; b < stopBand; b++ {
			energyBand[b] = energies[15][b] >> 4
		}
	}
	for k := 0; k < 15; k++ {
		for b := startBand; b < stopBand; b++ {
			energyBand[b] += energies[k][b] >> 4
		}
	}

	maxNEnergyValues := nativeaac.FMinI(sbrMaxEnergyValues, stopBand-startBand)

	energyMaxMin := energyBand[startBand]
	energyMax[0] = energyMaxMin
	noEnMaxBand[0] = startBand
	posEnergyMaxMin := 0
	for k := 1; k < maxNEnergyValues; k++ {
		energyMax[k] = energyBand[startBand+k]
		noEnMaxBand[k] = startBand + k
		if energyMaxMin > energyMax[k] {
			energyMaxMin = energyMax[k]
			posEnergyMaxMin = k
		}
	}

	for b := startBand + maxNEnergyValues; b < stopBand; b++ {
		if energyBand[b] > energyMaxMin {
			energyMax[posEnergyMaxMin] = energyBand[b]
			noEnMaxBand[posEnergyMaxMin] = b

			energyMaxMin = energyMax[0]
			posEnergyMaxMin = 0
			for k := 1; k < maxNEnergyValues; k++ {
				if energyMaxMin > energyMax[k] {
					energyMaxMin = energyMax[k]
					posEnergyMaxMin = k
				}
			}
		}
	}

	for e := 0; e < maxNEnergyValues; e++ {
		tonalityBand[e] = 0
		for k := 0; k < noEstPerFrame; k++ {
			tonalityBand[e] += quotaMatrix[startIndex+k][noEnMaxBand[e]] >> 1
		}
		globalTonality += tonalityBand[e] >> 2
	}

	return globalTonality
}

// mapPanorama is the 1:1 port of mapPanorama (env_est.cpp:437-465): quantize a
// panorama (balance) value. Returns the quantized pan and the quantization error
// (second value).
func mapPanorama(nrgVal, ampRes int) (pan, quantError int) {
	sign := 1
	if nrgVal <= 0 {
		sign = -1
	}
	nrgVal *= sign

	minVal := fdkIntMax
	panIndex := 0
	for i := 0; i < maxIndexT[ampRes]; i++ {
		val := int(nativeaac.FixpAbs(int32(nrgVal - int(panTable[ampRes][i]))))
		if val < minVal {
			minVal = val
			panIndex = i
		}
	}
	quantError = minVal
	pan = int(panTable[ampRes][maxIndexT[ampRes]-1]) + sign*int(panTable[ampRes][panIndex])
	return pan, quantError
}

// SbrNoiseFloorLevelsQuantisation is the 1:1 port of
// sbrNoiseFloorLevelsQuantisation (env_est.cpp:475-509).
func SbrNoiseFloorLevelsQuantisation(iNoiseLevels []int8, noiseLevels []int32, coupling int) {
	for i := 0; i < encMaxNumNoiseValues; i++ {
		var tmp int
		if noiseLevels[i] > fl2f(0.46875) {
			tmp = 30
		} else {
			tmp = int(noiseLevels[i] >> (dfractBits - 1 - encLdDataShift))
			if tmp != 0 {
				tmp += 1
			}
		}
		if coupling != 0 {
			if tmp < -30 {
				tmp = -30
			}
			tmp, _ = mapPanorama(tmp, 1)
		}
		iNoiseLevels[i] = int8(tmp)
	}
}

// CoupleNoiseFloor is the 1:1 port of coupleNoiseFloor (env_est.cpp:519-591):
// stereo coupling of the noise floor levels (modified in place).
func CoupleNoiseFloor(noiseLevelLeft, noiseLevelRight []int32) {
	nfo := noiseFloorOffset64()
	for i := 0; i < encMaxNumNoiseValues; i++ {
		cmpValLeft := nfo - noiseLevelLeft[i]
		cmpValRight := nfo - noiseLevelRight[i]

		var temp1, temp2 int32
		if cmpValRight < 0 {
			temp1 = nativeaac.CalcInvLdData(nfo - noiseLevelRight[i])
		} else {
			temp1 = nativeaac.CalcInvLdData(nfo - noiseLevelRight[i])
			temp1 = temp1 << (dfractBits - 1 - encLdDataShift - 1)
		}
		if cmpValLeft < 0 {
			temp2 = nativeaac.CalcInvLdData(nfo - noiseLevelLeft[i])
		} else {
			temp2 = nativeaac.CalcInvLdData(nfo - noiseLevelLeft[i])
			temp2 = temp2 << (dfractBits - 1 - encLdDataShift - 1)
		}

		if cmpValLeft < 0 && cmpValRight < 0 {
			noiseLevelLeft[i] = nfo - nativeaac.CalcLdData((temp1>>1)+(temp2>>1))
			noiseLevelRight[i] = nativeaac.CalcLdData(temp2) - nativeaac.CalcLdData(temp1)
		}
		if cmpValLeft >= 0 && cmpValRight >= 0 {
			noiseLevelLeft[i] = nfo - (nativeaac.CalcLdData((temp1>>1)+(temp2>>1)) + fl2f(0.109375))
			noiseLevelRight[i] = nativeaac.CalcLdData(temp2) - nativeaac.CalcLdData(temp1)
		}
		if cmpValLeft >= 0 && cmpValRight < 0 {
			noiseLevelLeft[i] = nfo - (nativeaac.CalcLdData((temp1>>(7+1))+(temp2>>1)) + fl2f(0.109375))
			noiseLevelRight[i] = (nativeaac.CalcLdData(temp2) + fl2f(0.109375)) - nativeaac.CalcLdData(temp1)
		}
		if cmpValLeft < 0 && cmpValRight >= 0 {
			noiseLevelLeft[i] = nfo - (nativeaac.CalcLdData((temp1>>1)+(temp2>>(7+1))) + fl2f(0.109375))
			noiseLevelRight[i] = nativeaac.CalcLdData(temp2) - (nativeaac.CalcLdData(temp1) + fl2f(0.109375))
		}
	}
}

// GetEnvSfbEnergy is the 1:1 port of getEnvSfbEnergy (env_est.cpp:603-649): the
// per-SFB energy summation over a slot range with dynamic scaling.
func GetEnvSfbEnergy(li, ui, startPos, stopPos, borderPos int, yBuffer [][]int32, yBufferSzShift, scaleNrg0, scaleNrg1 int) int32 {
	var dynScale int
	if ui-li == 0 {
		dynScale = dfractBits - 1
	} else {
		dynScale = int(nativeaac.CalcLdInt(int32(ui-li)) >> (dfractBits - 1 - encLdDataShift))
	}

	sc0 := nativeaac.FMinI(scaleNrg0, envYNrgScale)
	sc1 := nativeaac.FMinI(scaleNrg1, envYNrgScale)
	dynScale1 := nativeaac.FMinI(scaleNrg0-sc0, dynScale)
	dynScale2 := nativeaac.FMinI(scaleNrg1-sc1, dynScale)
	nrgSum := int32(0)
	accu1 := int32(0)
	accu2 := int32(0)

	for k := li; k < ui; k++ {
		nrg1 := int32(0)
		nrg2 := int32(0)
		var l int
		for l = startPos; l < borderPos; l++ {
			nrg1 += yBuffer[l>>yBufferSzShift][k] >> uint(sc0)
		}
		for ; l < stopPos; l++ {
			nrg2 += yBuffer[l>>yBufferSzShift][k] >> uint(sc1)
		}
		accu1 = nativeaac.FAddSaturate(accu1, nrg1>>uint(dynScale1))
		accu2 = nativeaac.FAddSaturate(accu2, nrg2>>uint(dynScale2))
	}
	nrgSum += (accu1 >> uint(nativeaac.FMinI(scaleNrg0-sc0-dynScale1, dfractBits-1))) +
		(accu2 >> uint(nativeaac.FMinI(scaleNrg1-sc1-dynScale2, dfractBits-1)))

	return nrgSum
}

// MhLoweringEnergy is the 1:1 port of mhLoweringEnergy (env_est.cpp:659-688).
func MhLoweringEnergy(nrg int32, M int) int32 {
	if M > 2 {
		tmpScale := int(nativeaac.CountLeadingBits(nrg))
		nrg <<= uint(tmpScale)
		nrg = nativeaac.FMultDD(nrg, fl2f(0.398107267))
		nrg >>= uint(tmpScale)
	} else {
		if M > 1 {
			nrg >>= 1
		}
	}
	return nrg
}

// NmhLoweringEnergy is the 1:1 port of nmhLoweringEnergy (env_est.cpp:698-712).
func NmhLoweringEnergy(nrg, nrgSum int32, nrgSumScale, M int) int32 {
	if nrg > 0 {
		divM, sc := nativeaac.FDivNorm(nrgSum, nrg)
		gain := nativeaac.FMultDD(divM, nativeaac.GetInvInt(M+1))
		scI := int(sc) + nrgSumScale

		if !((scI >= 0) && (gain > (encMaxvalDBL >> uint(scI)))) {
			nrg = nativeaac.FMultDD(nativeaac.ScaleValue(gain, int32(scI)), nrg)
		}
	}
	return nrg
}
