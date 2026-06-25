// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of libSBRenc/src/sbrenc_freq_sca.cpp — the SBR-encoder frequency
// band-table construction (the encode-side counterpart of the decode freq_sca.go
// already in this package, from a distinct vendored TU). It builds the master /
// hi-res / lo-res QMF band tables from the start/stop frequency parameters:
//   - FDKsbrEnc_getSbrStartFreqRAW / ...StopFreqRAW  (sbrenc_freq_sca.cpp:134-170)
//   - getStartFreq / getStopFreq                     (sbrenc_freq_sca.cpp:182-351)
//   - FDKsbrEnc_FindStartAndStopBand                 (sbrenc_freq_sca.cpp:369-411)
//   - FDKsbrEnc_UpdateFreqScale                      (sbrenc_freq_sca.cpp:422-547)
//   - numberOfBands / CalcBands / cumSum / modifyBands (549-603)
//   - FDKsbrEnc_UpdateHiRes / FDKsbrEnc_UpdateLoRes  (615-673)
//   - shellsortInt (== FDKsbrEnc_Shellsort_int, sbr_misc.cpp) — int sort used here.
//
// Pure fixed-point integer kernel — EXACT-integer parity. The "Enc" prefixes
// distinguish these from the decode-side helpers of the same concept.
package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// maxOctave (29), maxSecondRegion (50), maxFreqCoeffsFs44100 (35) and
// maxFreqCoeffsFs48000 (32) are already defined by the decode-side freq_sca.go
// (the same ISO macros) and reused here.
const (
	maxFreqCoeffsEnc = 48 // MAX_FREQ_COEFFS (sbr_def.h:156)
)

// shellsortInt is the 1:1 port of FDKsbrEnc_Shellsort_int (sbr_misc.cpp): an
// ascending Shell sort over n ints.
func shellsortInt(in []int, n int) {
	inc := 1
	for {
		inc = 3*inc + 1
		if inc > n {
			break
		}
	}
	for {
		inc = inc / 3
		for i := inc + 1; i <= n; i++ {
			v := in[i-1]
			j := i
			for in[j-inc-1] > v {
				in[j-1] = in[j-inc-1]
				j -= inc
				if j <= inc {
					break
				}
			}
			in[j-1] = v
		}
		if inc <= 1 {
			break
		}
	}
}

// getStartFreqEnc is the 1:1 port of getStartFreq (sbrenc_freq_sca.cpp:182-254).
func getStartFreqEnc(fsCore, startFreq int) int {
	var k0Min int
	switch fsCore {
	case 8000:
		k0Min = 24
	case 11025:
		k0Min = 17
	case 12000:
		k0Min = 16
	case 16000:
		k0Min = 16
	case 22050:
		k0Min = 12
	case 24000:
		k0Min = 11
	case 32000:
		k0Min = 10
	case 44100:
		k0Min = 7
	case 48000:
		k0Min = 7
	case 96000:
		k0Min = 3
	default:
		k0Min = 11
	}

	switch fsCore {
	case 8000:
		v := []int{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}
		return k0Min + v[startFreq]
	case 11025:
		v := []int{-5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7, 9, 11, 13}
		return k0Min + v[startFreq]
	case 12000:
		v := []int{-5, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7, 9, 11, 13, 16}
		return k0Min + v[startFreq]
	case 16000:
		v := []int{-6, -4, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7, 9, 11, 13, 16}
		return k0Min + v[startFreq]
	case 22050, 24000, 32000:
		v := []int{-4, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7, 9, 11, 13, 16, 20}
		return k0Min + v[startFreq]
	case 44100, 48000, 96000:
		v := []int{-2, -1, 0, 1, 2, 3, 4, 5, 6, 7, 9, 11, 13, 16, 20, 24}
		return k0Min + v[startFreq]
	default:
		v := []int{0, 1, 2, 3, 4, 5, 6, 7, 9, 11, 13, 16, 20, 24, 28, 33}
		return k0Min + v[startFreq]
	}
}

// getStopFreqEnc is the 1:1 port of getStopFreq (sbrenc_freq_sca.cpp:265-351).
func getStopFreqEnc(fsCore, stopFreq int) int {
	vStopFreq16 := []int{48, 49, 50, 51, 52, 54, 55, 56, 57, 59, 60, 61, 63, 64}
	vStopFreq22 := []int{35, 37, 38, 40, 42, 44, 46, 48, 51, 53, 56, 58, 61, 64}
	vStopFreq24 := []int{32, 34, 36, 38, 40, 42, 44, 46, 49, 52, 55, 58, 61, 64}
	vStopFreq32 := []int{32, 34, 36, 38, 40, 42, 44, 46, 49, 52, 55, 58, 61, 64}
	vStopFreq44 := []int{23, 25, 27, 29, 32, 34, 37, 40, 43, 47, 51, 55, 59, 64}
	vStopFreq48 := []int{21, 23, 25, 27, 30, 32, 35, 38, 42, 45, 49, 54, 59, 64}
	vStopFreq64 := []int{20, 22, 24, 26, 29, 31, 34, 37, 41, 45, 49, 54, 59, 64}
	vStopFreq88 := []int{15, 17, 19, 21, 23, 26, 29, 33, 37, 41, 46, 51, 57, 64}
	vStopFreq96 := []int{13, 15, 17, 19, 21, 24, 27, 31, 35, 39, 44, 50, 57, 64}
	vStopFreq192 := []int{7, 8, 10, 12, 14, 16, 19, 23, 27, 32, 38, 46, 54, 64}

	var k1Min int
	var vStopFreq []int
	switch fsCore {
	case 8000:
		k1Min = 48
		vStopFreq = vStopFreq16
	case 11025:
		k1Min = 35
		vStopFreq = vStopFreq22
	case 12000:
		k1Min = 32
		vStopFreq = vStopFreq24
	case 16000:
		k1Min = 32
		vStopFreq = vStopFreq32
	case 22050:
		k1Min = 23
		vStopFreq = vStopFreq44
	case 24000:
		k1Min = 21
		vStopFreq = vStopFreq48
	case 32000:
		k1Min = 20
		vStopFreq = vStopFreq64
	case 44100:
		k1Min = 15
		vStopFreq = vStopFreq88
	case 48000:
		k1Min = 13
		vStopFreq = vStopFreq96
	case 96000:
		k1Min = 7
		vStopFreq = vStopFreq192
	default:
		k1Min = 21
		vStopFreq = vStopFreq48 // unreachable for legal fs; kept defined
	}

	var vDstop [13]int
	for i := 0; i <= 12; i++ {
		vDstop[i] = vStopFreq[i+1] - vStopFreq[i]
	}
	shellsortInt(vDstop[:], 13)

	result := k1Min
	for i := 0; i < stopFreq; i++ {
		result += vDstop[i]
	}
	return result
}

// GetSbrStartFreqRAW is the 1:1 port of FDKsbrEnc_getSbrStartFreqRAW (134-148).
func GetSbrStartFreqRAW(startFreq, fsCore int) int {
	if startFreq < 0 || startFreq > 15 {
		return -1
	}
	result := getStartFreqEnc(fsCore, startFreq)
	return (result*(fsCore>>5) + 1) >> 1
}

// GetSbrStopFreqRAW is the 1:1 port of FDKsbrEnc_getSbrStopFreqRAW (159-170).
func GetSbrStopFreqRAW(stopFreq, fsCore int) int {
	if stopFreq < 0 || stopFreq > 13 {
		return -1
	}
	result := getStopFreqEnc(fsCore, stopFreq)
	return (result*(fsCore>>5) + 1) >> 1
}

// FindStartAndStopBand is the 1:1 port of FDKsbrEnc_FindStartAndStopBand
// (sbrenc_freq_sca.cpp:369-411). Returns (k0, k2, errCode).
func FindStartAndStopBand(srSbr, srCore, noChannels, startFreq, stopFreq int) (k0, k2, errCode int) {
	k0 = getStartFreqEnc(srCore, startFreq)
	if srSbr*noChannels < k0*srCore {
		return k0, 0, 1
	}
	if stopFreq < 14 {
		k2 = getStopFreqEnc(srCore, stopFreq)
	} else if stopFreq == 14 {
		k2 = 2 * k0
	} else {
		k2 = 3 * k0
	}
	if k2 > noChannels {
		k2 = noChannels
	}
	if srCore == 22050 && (k2-k0) > maxFreqCoeffsFs44100 {
		return k0, k2, 1
	}
	if srCore >= 24000 && (k2-k0) > maxFreqCoeffsFs48000 {
		return k0, k2, 1
	}
	if (k2 - k0) > maxFreqCoeffsEnc {
		return k0, k2, 1
	}
	if (k2 - k0) < 0 {
		return k0, k2, 1
	}
	return k0, k2, 0
}

// numberOfBandsEnc is the 1:1 port of numberOfBands (sbrenc_freq_sca.cpp:549-560).
func numberOfBandsEnc(bPO, start, stop int, warpFactor int32) int {
	return int((bPO*int(nativeaac.FMultDD(nativeaac.CalcLdInt(int32(stop))-nativeaac.CalcLdInt(int32(start)), warpFactor))+
		int(fl2f(0.5)>>encLdDataShift))>>(dfractBits-1-encLdDataShift)) << 1
}

// calcBandsEnc is the 1:1 port of CalcBands (sbrenc_freq_sca.cpp:562-580).
func calcBandsEnc(diff []int, start, stop, numBands int) {
	previous := start
	for i := 1; i <= numBands; i++ {
		base, qb := nativeaac.FDivNorm(int32(stop), int32(start))
		exp, qe := nativeaac.FDivNorm(int32(i), int32(numBands))
		tmp, qtmp := nativeaac.FPow(base, qb, exp, qe)
		tmp = nativeaac.FMultDD(tmp, int32(start)<<24)
		current := int(nativeaac.ScaleValue(tmp, qtmp-23))
		current = (current + 1) >> 1 // rounding
		diff[i-1] = current - previous
		previous = current
	}
}

// cumSumEnc is the 1:1 port of cumSum (sbrenc_freq_sca.cpp:582-588).
func cumSumEnc(startValue int, diff []int, length int, startAddress []uint8) {
	startAddress[0] = uint8(startValue)
	for i := 1; i <= length; i++ {
		startAddress[i] = startAddress[i-1] + uint8(diff[i-1])
	}
}

// modifyBandsEnc is the 1:1 port of modifyBands (sbrenc_freq_sca.cpp:590-603).
func modifyBandsEnc(maxBandPrevious int, diff []int, length int) int {
	change := maxBandPrevious - diff[0]
	if change > (diff[length-1]-diff[0])/2 {
		change = (diff[length-1] - diff[0]) / 2
	}
	diff[0] += change
	diff[length-1] -= change
	shellsortInt(diff, length)
	return 0
}

// UpdateFreqScale is the 1:1 port of FDKsbrEnc_UpdateFreqScale
// (sbrenc_freq_sca.cpp:422-547): builds v_k_master and returns (numBands, err).
func UpdateFreqScale(vKMaster []uint8, k0, k2, freqScale, alterScale int) (numBands, errCode int) {
	bPO := 0
	warp := int32(0)
	dk := 0

	var diffTot [maxOctave + maxSecondRegion]int
	diff0 := diffTot[:]
	diff1 := diffTot[maxOctave:]

	if freqScale == 1 {
		bPO = 12
	}
	if freqScale == 2 {
		bPO = 10
	}
	if freqScale == 3 {
		bPO = 8
	}

	if freqScale > 0 { // Bark
		if alterScale == 0 {
			warp = fl2f(0.5)
		} else {
			warp = fl2f(1.0 / 2.6)
		}

		var k1 int
		var numBands0, numBands1 int
		if 4*k2 >= 9*k0 { // two or more regions
			k1 = 2 * k0
			numBands0 = numberOfBandsEnc(bPO, k0, k1, fl2f(0.5))
			numBands1 = numberOfBandsEnc(bPO, k1, k2, warp)

			calcBandsEnc(diff0, k0, k1, numBands0)
			shellsortInt(diff0, numBands0)
			if diff0[0] == 0 {
				return 0, 1
			}
			cumSumEnc(k0, diff0, numBands0, vKMaster)

			calcBandsEnc(diff1, k1, k2, numBands1)
			shellsortInt(diff1, numBands1)
			if diff0[numBands0-1] > diff1[0] {
				if modifyBandsEnc(diff0[numBands0-1], diff1, numBands1) != 0 {
					return 0, 1
				}
			}
			cumSumEnc(k1, diff1, numBands1, vKMaster[numBands0:])
			numBands = numBands0 + numBands1
		} else { // one region
			k1 = k2
			numBands0 = numberOfBandsEnc(bPO, k0, k1, fl2f(0.5))
			calcBandsEnc(diff0, k0, k1, numBands0)
			shellsortInt(diff0, numBands0)
			if diff0[0] == 0 {
				return 0, 1
			}
			cumSumEnc(k0, diff0, numBands0, vKMaster)
			numBands = numBands0
		}
	} else { // Linear mode
		var numBands0 int
		if alterScale == 0 {
			dk = 1
			numBands0 = 2 * ((k2 - k0) / 2)
		} else {
			dk = 2
			numBands0 = 2 * (((k2-k0)/dk + 1) / 2)
		}

		k2Achived := k0 + numBands0*dk
		k2Diff := k2 - k2Achived

		for i := 0; i < numBands0; i++ {
			diffTot[i] = dk
		}

		incr := 0
		i := 0
		if k2Diff < 0 {
			incr = 1
			i = 0
		}
		if k2Diff > 0 {
			incr = -1
			i = numBands0 - 1
		}
		for k2Diff != 0 {
			diffTot[i] = diffTot[i] - incr
			i = i + incr
			k2Diff = k2Diff + incr
		}

		cumSumEnc(k0, diffTot[:], numBands0, vKMaster)
		numBands = numBands0
	}

	if numBands < 1 {
		return numBands, 1
	}
	return numBands, 0
}

// UpdateHiRes is the 1:1 port of FDKsbrEnc_UpdateHiRes (615-642). Returns
// (numHires, possibly-clipped xoverBand, err).
func UpdateHiRes(hHires, vKMaster []uint8, numMaster, xoverBand int) (numHires, newXover, errCode int) {
	if int(vKMaster[xoverBand]) > 32 || xoverBand > numMaster {
		max1 := 0
		max2 := numMaster
		for int(vKMaster[max1+1]) < 32 && (max1+1) < max2 {
			max1++
		}
		xoverBand = max1
	}

	numHires = numMaster - xoverBand
	for i := xoverBand; i <= numMaster; i++ {
		hHires[i-xoverBand] = vKMaster[i]
	}
	return numHires, xoverBand, 0
}

// UpdateLoRes is the 1:1 port of FDKsbrEnc_UpdateLoRes (653-673). Returns
// numLores.
func UpdateLoRes(hLores, hHires []uint8, numHires int) (numLores int) {
	if numHires%2 == 0 {
		numLores = numHires / 2
		for i := 0; i <= numLores; i++ {
			hLores[i] = hHires[i*2]
		}
	} else {
		numLores = (numHires + 1) / 2
		hLores[0] = hHires[0]
		for i := 1; i <= numLores; i++ {
			hLores[i] = hHires[i*2-1]
		}
	}
	return numLores
}
