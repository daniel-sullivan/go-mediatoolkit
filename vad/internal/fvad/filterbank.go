package fvad

// This file ports src/vad/vad_filterbank.c — the feature extraction
// used by core.go: a split-filter cascade dividing 0–4 kHz into six
// bands, plus the log-energy of each band.

// Constants used in logOfEnergy().
const (
	kLogConst         = int16(24660) // 160*log10(2) in Q9.
	kLogEnergyIntPart = int16(14336) // 14 in Q10
)

// Coefficients used by highPassFilter, Q14.
var (
	kHpZeroCoefs = [3]int16{6631, -13262, 6631}
	kHpPoleCoefs = [3]int16{16384, -7756, 5620}
)

// Allpass filter coefficients, upper and lower, in Q15.
// Upper: 0.64, Lower: 0.17
var kAllPassCoefsQ15 = [2]int16{20972, 5571}

// Adjustment for division with two in splitFilter.
var kOffsetVector = [6]int16{368, 368, 272, 176, 176, 176}

// highPassFilter ports HighPassFilter: a 80 Hz cut-off highpass for
// data sampled at 500 Hz. Reads len(dataIn) samples; filterState holds
// 4 states.
func highPassFilter(dataIn []int16, filterState []int16, dataOut []int16) {
	for i := 0; i < len(dataIn); i++ {
		v := dataIn[i]

		// All-zero section (filter coefficients in Q14).
		tmp32 := int32(kHpZeroCoefs[0]) * int32(v)
		tmp32 += int32(kHpZeroCoefs[1]) * int32(filterState[0])
		tmp32 += int32(kHpZeroCoefs[2]) * int32(filterState[1])
		filterState[1] = filterState[0]
		filterState[0] = v

		// All-pole section (filter coefficients in Q14).
		tmp32 -= int32(kHpPoleCoefs[1]) * int32(filterState[2])
		tmp32 -= int32(kHpPoleCoefs[2]) * int32(filterState[3])
		filterState[3] = filterState[2]
		filterState[2] = int16(tmp32 >> 14)
		dataOut[i] = filterState[2]
	}
}

// allPassFilter ports AllPassFilter: allpass-filter every SECOND sample
// of dataIn (stride 2, dataLength outputs) ahead of the band split.
// dataIn is Q0, filterState (one int16) is Q(-1), dataOut is Q(-1).
// dataIn and dataOut must not alias.
func allPassFilter(dataIn []int16, dataLength int, filterCoefficient int16, filterState *int16, dataOut []int16) {
	// The filter can only cause overflow (in the int16 output) if more
	// than 4 consecutive input numbers are of maximum value and have
	// the same sign as the impulse response's first taps.
	state32 := int32(*filterState) * (1 << 16) // Q15
	inIdx := 0
	for i := 0; i < dataLength; i++ {
		tmp32 := state32 + int32(filterCoefficient)*int32(dataIn[inIdx])
		tmp16 := int16(tmp32 >> 16) // Q(-1)
		dataOut[i] = tmp16
		state32 = int32(dataIn[inIdx])*(1<<14) - int32(filterCoefficient)*int32(tmp16) // Q14
		state32 *= 2                                                                   // Q15.
		inIdx += 2
	}
	*filterState = int16(state32 >> 16) // Q(-1)
}

// splitFilter ports SplitFilter: split dataIn (dataLength samples) into
// an upper (highpass) and lower (lowpass) half-band, each
// dataLength/2 samples, downsampled by 2.
func splitFilter(dataIn []int16, dataLength int, upperState, lowerState *int16, hpDataOut, lpDataOut []int16) {
	halfLength := dataLength >> 1 // Downsampling by 2.

	// All-pass filtering upper branch.
	allPassFilter(dataIn, halfLength, kAllPassCoefsQ15[0], upperState, hpDataOut)

	// All-pass filtering lower branch.
	allPassFilter(dataIn[1:], halfLength, kAllPassCoefsQ15[1], lowerState, lpDataOut)

	// Make LP and HP signals.
	for i := 0; i < halfLength; i++ {
		tmpOut := hpDataOut[i]
		hpDataOut[i] -= lpDataOut[i]
		lpDataOut[i] += tmpOut
	}
}

// logOfEnergy ports LogOfEnergy: 10·log10(energy of dataIn) in Q4 into
// logEnergy (plus offset), and updates totalEnergy with the frame
// energy while totalEnergy <= kMinEnergy (it is only an indicator, not
// an exact total).
func logOfEnergy(dataIn []int16, offset int16, totalEnergy *int16, logEnergy *int16) {
	// totRshifts accumulates the number of right shifts performed on
	// energy.
	energyS32, totRshifts := Energy(dataIn)
	// The energy will be normalized to 15 bits. We use unsigned integer
	// because we eventually will mask out the fractional part.
	energy := uint32(energyS32)

	if energy != 0 {
		// By construction, normalizing to 15 bits is equivalent with 17
		// leading zeros of an unsigned 32 bit value.
		normalizingRshifts := 17 - int(NormU32(energy))
		// In a 15 bit representation the leading bit is 2^14. log2(2^14)
		// in Q10 is (14 << 10), which is what we initialize log2Energy
		// with.
		log2Energy := kLogEnergyIntPart

		totRshifts += normalizingRshifts
		// Normalize energy to 15 bits; tot_rshifts is now the total
		// number of right shifts performed on energy after
		// normalization, i.e. energy is in Q(-totRshifts).
		if normalizingRshifts < 0 {
			energy <<= uint(-normalizingRshifts)
		} else {
			energy >>= uint(normalizingRshifts)
		}

		// Calculate the energy of dataIn in dB, in Q4. See the C source
		// for the full derivation; frac_Q15 = (energy & 0x00003FFF).
		log2Energy += int16((energy & 0x00003FFF) >> 4)

		// kLogConst is in Q9, log2Energy in Q10 and totRshifts in Q0.
		*logEnergy = int16(((int32(kLogConst) * int32(log2Energy)) >> 19) +
			((int32(totRshifts) * int32(kLogConst)) >> 9))

		if *logEnergy < 0 {
			*logEnergy = 0
		}
	} else {
		*logEnergy = offset
		return
	}

	*logEnergy += offset

	// Update the approximate totalEnergy with the energy of dataIn, if
	// totalEnergy has not exceeded kMinEnergy.
	if *totalEnergy <= MinEnergy {
		if totRshifts >= 0 {
			// We know by construction that energy > kMinEnergy in Q0, so
			// add an arbitrary value such that totalEnergy exceeds
			// kMinEnergy.
			*totalEnergy += MinEnergy + 1
		} else {
			// By construction energy is represented by 15 bits, hence
			// any number of right shifted energy fits an int16, and
			// adding it is wrap-around safe as long as kMinEnergy < 8192.
			*totalEnergy += int16(energy >> uint(-totRshifts)) // Q0.
		}
	}
}

// CalculateFeatures ports WebRtcVad_CalculateFeatures: split dataIn
// (80/160/240 samples of 8 kHz audio) into the six VAD frequency bands
//
//	80–250, 250–500, 500–1000, 1000–2000, 2000–3000, 3000–4000 Hz
//
// and write 10·log10(band energy) in Q4 to features[0..5]. The return
// value is the approximate total energy — arbitrary above MinEnergy,
// used only as a signal indicator by the GMM.
func CalculateFeatures(self *Inst, dataIn []int16, features []int16) int16 {
	totalEnergy := int16(0)
	// The intermediate downsampled data has at most 120 samples after
	// the first split and at most 60 after the second.
	var hp120, lp120 [120]int16
	var hp60, lp60 [60]int16
	dataLength := len(dataIn)
	halfDataLength := dataLength >> 1
	length := halfDataLength // bandwidth = 2000 Hz after downsampling.

	// Split at 2000 Hz and downsample.
	splitFilter(dataIn, dataLength, &self.UpperState[0], &self.LowerState[0],
		hp120[:], lp120[:])

	// For the upper band (2000–4000 Hz) split at 3000 Hz and downsample.
	splitFilter(hp120[:length], length, &self.UpperState[1], &self.LowerState[1],
		hp60[:], lp60[:])

	// Energy in 3000 Hz - 4000 Hz.
	length >>= 1 // bandwidth = 1000 Hz.
	logOfEnergy(hp60[:length], kOffsetVector[5], &totalEnergy, &features[5])

	// Energy in 2000 Hz - 3000 Hz.
	logOfEnergy(lp60[:length], kOffsetVector[4], &totalEnergy, &features[4])

	// For the lower band (0–2000 Hz) split at 1000 Hz and downsample.
	length = halfDataLength // bandwidth = 2000 Hz.
	splitFilter(lp120[:length], length, &self.UpperState[2], &self.LowerState[2],
		hp60[:], lp60[:])

	// Energy in 1000 Hz - 2000 Hz.
	length >>= 1 // bandwidth = 1000 Hz.
	logOfEnergy(hp60[:length], kOffsetVector[3], &totalEnergy, &features[3])

	// For the lower band (0–1000 Hz) split at 500 Hz and downsample.
	splitFilter(lp60[:length], length, &self.UpperState[3], &self.LowerState[3],
		hp120[:], lp120[:])

	// Energy in 500 Hz - 1000 Hz.
	length >>= 1 // bandwidth = 500 Hz.
	logOfEnergy(hp120[:length], kOffsetVector[2], &totalEnergy, &features[2])

	// For the lower band (0–500 Hz) split at 250 Hz and downsample.
	splitFilter(lp120[:length], length, &self.UpperState[4], &self.LowerState[4],
		hp60[:], lp60[:])

	// Energy in 250 Hz - 500 Hz.
	length >>= 1 // bandwidth = 250 Hz.
	logOfEnergy(hp60[:length], kOffsetVector[1], &totalEnergy, &features[1])

	// Remove 0 Hz - 80 Hz by high pass filtering the lower band.
	highPassFilter(lp60[:length], self.HpFilterState[:], hp120[:length])

	// Energy in 80 Hz - 250 Hz.
	logOfEnergy(hp120[:length], kOffsetVector[0], &totalEnergy, &features[0])

	return totalEnergy
}
