// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Parity-only exports for the scale-factor estimation (sf_estim.cpp) port. These
// thin wrappers let the cgo parity slice under internal/parity_tests/enc-sf-estim/
// drive the unexported leaf kernels and the full estimator against the genuine
// vendored FDKaacEnc_* symbols and compare bit-for-bit. Not part of the
// production API.

// ParitySqrtFixp wraps sqrtFixp (the form-factor sqrt kernel).
func ParitySqrtFixp(op int32) int32 { return sqrtFixp(op) }

// ParityInvSqrtNorm2 wraps invSqrtNorm2, returning (mantissa, shift).
func ParityInvSqrtNorm2(op int32) (int32, int32) { return invSqrtNorm2(op) }

// ParityBitCountScalefactorDelta wraps bitCountScalefactorDelta.
func ParityBitCountScalefactorDelta(delta int) int { return bitCountScalefactorDelta(delta) }

// ParityCalcFormFactorChannel runs calcFormFactorChannel over a flat MDCT
// spectrum + band layout and returns the per-sfb form factor (length
// maxGroupedSFB), matching the C FDKaacEnc_FDKaacEnc_CalcFormFactorChannel output.
func ParityCalcFormFactorChannel(mdctSpectrum []int32, sfbOffsets []int,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int) []int32 {
	out := make([]int32, maxGroupedSFB)
	calcFormFactorChannel(out, mdctSpectrum, sfbOffsets, sfbCnt, sfbPerGroup, maxSfbPerGroup)
	return out
}

// ParityCalcSfbRelevantLines runs calcSfbRelevantLines and returns
// sfbNRelevantLines (length maxGroupedSFB).
func ParityCalcSfbRelevantLines(
	sfbFormFactorLdData, sfbEnergyLdData, sfbThresholdLdData []int32,
	sfbOffsets []int, sfbCnt, sfbPerGroup, maxSfbPerGroup int) []int32 {
	out := make([]int32, maxGroupedSFB)
	calcSfbRelevantLines(sfbFormFactorLdData, sfbEnergyLdData, sfbThresholdLdData,
		sfbOffsets, sfbCnt, sfbPerGroup, maxSfbPerGroup, out)
	return out
}

// ParityCountSingleScfBits wraps countSingleScfBits.
func ParityCountSingleScfBits(scf, scfLeft, scfRight int) int32 {
	return countSingleScfBits(scf, scfLeft, scfRight)
}

// ParityCalcSingleSpecPe wraps calcSingleSpecPe.
func ParityCalcSingleSpecPe(scf int, sfbConstPePart, nLines int32) int32 {
	return calcSingleSpecPe(scf, sfbConstPePart, nLines)
}

// ParityCountScfBitsDiff wraps countScfBitsDiff.
func ParityCountScfBitsDiff(scfOld, scfNew []int, sfbCnt, startSfb, stopSfb int) int32 {
	return countScfBitsDiff(scfOld, scfNew, sfbCnt, startSfb, stopSfb)
}

// ParityCalcSpecPeDiff runs calcSpecPeDiff over a copy of sfbConstPePart and
// returns (specPeDiff, the updated sfbConstPePart) so the lazy-fill of
// sfbConstPePart can be compared against the C oracle too.
func ParityCalcSpecPeDiff(
	sfbEnergyLdData []int32, scfOld, scfNew []int,
	sfbConstPePart, sfbFormFactorLdData, sfbNRelevantLines []int32,
	startSfb, stopSfb int) (int32, []int32) {
	cpe := append([]int32(nil), sfbConstPePart...)
	d := calcSpecPeDiff(sfbEnergyLdData, scfOld, scfNew, cpe,
		sfbFormFactorLdData, sfbNRelevantLines, startSfb, stopSfb)
	return d, cpe
}

// ParityImproveScf runs improveScf over band-local copies of the quant buffers
// and returns (scfBest, distLdData, minScfCalculated, quantSpec) so the chosen
// scalefactor, distortion and the band quantization can all be compared.
func ParityImproveScf(spec []int32, quantSpec, quantSpecTmp []int16, sfbWidth int,
	threshLdData int32, scf, minScf int, dZoneQuantEnable bool) (int, int32, int, []int16) {
	qs := append([]int16(nil), quantSpec...)
	qt := append([]int16(nil), quantSpecTmp...)
	best, dist, msc := improveScf(spec, qs, qt, sfbWidth, threshLdData, scf, minScf, dZoneQuantEnable)
	return best, dist, msc, qs
}

// ParityEstimateScaleFactorsChannel runs the full
// FDKaacEnc_EstimateScaleFactorsChannel over flat per-channel inputs. mdctSpectrum
// (length 1024) is mutated in place (empty bands zeroed). Returns (scf,
// globalGain, quantSpec) — the complete estimator output. The caller passes the
// adjusted sfbEnergyLdData / sfbThresholdLdData / sfbFormFactorLdData and the
// band layout; this mirrors the C call from FDKaacEnc_EstimateScaleFactors.
func ParityEstimateScaleFactorsChannel(
	mdctSpectrum []int32, sfbEnergyLdData, sfbThresholdLdData, sfbFormFactorLdData []int32,
	sfbOffsets []int, sfbCnt, sfbPerGroup, maxSfbPerGroup int,
	invQuant int, dZoneQuantEnable bool) (scf []int, globalGain int, quantSpec []int16) {
	psy := &sfEstimPsyChannel{
		sfbCnt:         sfbCnt,
		sfbPerGroup:    sfbPerGroup,
		maxSfbPerGroup: maxSfbPerGroup,
		sfbOffsets:     sfbOffsets,
	}
	qc := &sfEstimQcChannel{
		mdctSpectrum:        mdctSpectrum,
		sfbEnergyLdData:     sfbEnergyLdData,
		sfbThresholdLdData:  sfbThresholdLdData,
		sfbFormFactorLdData: sfbFormFactorLdData,
	}
	// The C scf array is INT scf[MAX_GROUPED_SFB]; assimilateMultipleScf2 reads
	// scf[startSfb] with startSfb possibly == sfbCnt (== MAX_GROUPED_SFB), an
	// out-of-array read whose value is immediately gated out by the following
	// `startSfb < sfbCnt` test (sf_estim.cpp:797,824 — the value is dead). The C
	// happens to read the adjacent struct member; in Go we give scf one guard
	// cell so the read is defined. The cell never affects any output.
	scfPadded := make([]int, maxGroupedSFB+1)
	quantSpec = make([]int16, 1024)
	globalGain = estimateScaleFactorsChannel(qc, psy, scfPadded, sfbFormFactorLdData,
		invQuant, quantSpec, dZoneQuantEnable)
	scf = scfPadded[:maxGroupedSFB]
	return scf, globalGain, quantSpec
}

// ParityCalcFormFactorDriver runs the top-level CalcFormFactor wrapper over
// nChannels (1 or 2). Inputs are flat per-channel arrays (channel ch's mdct
// spectrum at mdct[ch*1024 .. ], shared band layout). Returns the per-channel
// sfbFormFactorLdData (length nChannels*maxGroupedSFB). Exercises the channel
// loop of FDKaacEnc_CalcFormFactor directly.
func ParityCalcFormFactorDriver(nChannels int, mdct []int32, sfbOffsets []int,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int) []int32 {
	qcs := make([]*QcOutChannel, nChannels)
	psys := make([]*PsyOutChannel, nChannels)
	mdcts := make([][]int32, nChannels)
	for ch := 0; ch < nChannels; ch++ {
		qcs[ch] = new(QcOutChannel)
		psy := new(PsyOutChannel)
		psy.SfbCnt, psy.SfbPerGroup, psy.MaxSfbPerGroup = sfbCnt, sfbPerGroup, maxSfbPerGroup
		copy(psy.SfbOffsets[:], sfbOffsets)
		psys[ch] = psy
		mdcts[ch] = mdct[ch*1024 : ch*1024+1024]
	}
	CalcFormFactor(qcs, psys, mdcts, nChannels)
	out := make([]int32, nChannels*maxGroupedSFB)
	for ch := 0; ch < nChannels; ch++ {
		copy(out[ch*maxGroupedSFB:], qcs[ch].SfbFormFactorLdData[:maxGroupedSFB])
	}
	return out
}

// ParityEstimateScaleFactorsDriver runs the top-level EstimateScaleFactors
// wrapper over nChannels. Inputs are flat per-channel arrays. Returns the
// per-channel scf, globalGain and quantSpec plus the (possibly zeroed) mdct.
// Exercises the channel loop of FDKaacEnc_EstimateScaleFactors directly.
func ParityEstimateScaleFactorsDriver(nChannels int, mdct []int32,
	sfbEnergyLdData, sfbThresholdLdData, sfbFormFactorLdData []int32, sfbOffsets []int,
	sfbCnt, sfbPerGroup, maxSfbPerGroup, invQuant int, dZoneQuantEnable bool) (
	scf []int32, globalGain []int32, quantSpec []int16, mdctOut []int32) {
	qcs := make([]*QcOutChannel, nChannels)
	psys := make([]*PsyOutChannel, nChannels)
	mdcts := make([][]int32, nChannels)
	for ch := 0; ch < nChannels; ch++ {
		qc := new(QcOutChannel)
		base := ch * maxGroupedSFB
		copy(qc.SfbEnergyLdData[:], sfbEnergyLdData[base:base+maxGroupedSFB])
		copy(qc.SfbThresholdLdData[:], sfbThresholdLdData[base:base+maxGroupedSFB])
		copy(qc.SfbFormFactorLdData[:], sfbFormFactorLdData[base:base+maxGroupedSFB])
		qcs[ch] = qc
		psy := new(PsyOutChannel)
		psy.SfbCnt, psy.SfbPerGroup, psy.MaxSfbPerGroup = sfbCnt, sfbPerGroup, maxSfbPerGroup
		copy(psy.SfbOffsets[:], sfbOffsets)
		psys[ch] = psy
		mdcts[ch] = mdct[ch*1024 : ch*1024+1024]
	}
	EstimateScaleFactors(psys, qcs, mdcts, invQuant, dZoneQuantEnable, nChannels)
	scf = make([]int32, nChannels*maxGroupedSFB)
	globalGain = make([]int32, nChannels)
	quantSpec = make([]int16, nChannels*1024)
	for ch := 0; ch < nChannels; ch++ {
		for i := 0; i < maxGroupedSFB; i++ {
			scf[ch*maxGroupedSFB+i] = int32(qcs[ch].Scf[i])
		}
		globalGain[ch] = int32(qcs[ch].GlobalGain)
		copy(quantSpec[ch*1024:], qcs[ch].QuantSpec[:])
	}
	return scf, globalGain, quantSpec, mdct
}
