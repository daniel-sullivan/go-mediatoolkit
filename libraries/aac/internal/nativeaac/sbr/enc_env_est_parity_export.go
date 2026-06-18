// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// Thin exported drivers for the sbr-enc-analysis envelope-estimator parity slice.
// Each rebuilds the [][]int32 slot views from a flat shared input, runs the leaf
// port, and returns the result (plus the mutated QMF/scales where the C mutates
// in place) so the oracle can assert EXACT int equality vs the vendored static.

// RunGetEnergyFromCplxQmfData runs the timeslot-pair energy extraction over
// numberCols rows (stride numberBands) of real/imag (mutated in place), returning
// the energy matrix (numberCols/2 rows flat), the mutated real/imag, and the
// updated qmfScale/energyScale.
func RunGetEnergyFromCplxQmfData(realFlat, imagFlat []int32, numberBands, numberCols, qmfScaleIn int) (energyFlat, realOut, imagOut []int32, qmfScale, energyScale int) {
	real := splitRows(realFlat, numberCols, numberBands)
	imag := splitRows(imagFlat, numberCols, numberBands)
	energy := make([][]int32, numberCols/2)
	energyFlat = make([]int32, (numberCols/2)*numberBands)
	for i := range energy {
		energy[i] = energyFlat[i*numberBands : i*numberBands+numberBands]
	}
	qmfScale = qmfScaleIn
	GetEnergyFromCplxQmfData(energy, real, imag, numberBands, numberCols, &qmfScale, &energyScale)
	return energyFlat, realFlat, imagFlat, qmfScale, energyScale
}

// RunGetEnergyFromCplxQmfDataFull runs the per-timeslot energy extraction.
func RunGetEnergyFromCplxQmfDataFull(realFlat, imagFlat []int32, numberBands, numberCols, qmfScaleIn int) (energyFlat, realOut, imagOut []int32, qmfScale, energyScale int) {
	real := splitRows(realFlat, numberCols, numberBands)
	imag := splitRows(imagFlat, numberCols, numberBands)
	energy := make([][]int32, numberCols)
	energyFlat = make([]int32, numberCols*numberBands)
	for i := range energy {
		energy[i] = energyFlat[i*numberBands : i*numberBands+numberBands]
	}
	qmfScale = qmfScaleIn
	GetEnergyFromCplxQmfDataFull(energy, real, imag, numberBands, numberCols, &qmfScale, &energyScale)
	return energyFlat, realFlat, imagFlat, qmfScale, energyScale
}

// RunGetTonality runs FDKsbrEnc_GetTonality over the flat quota (totEst rows) +
// energy (numberCols rows) matrices.
func RunGetTonality(quotaFlat []int32, totEst, qmfChannels int, energyFlat []int32, numberCols, noEstPerFrame, startIndex, startBand, stopBand int) int32 {
	quota := splitRows(quotaFlat, totEst, qmfChannels)
	energy := splitRows(energyFlat, numberCols, qmfChannels)
	return GetTonality(quota, noEstPerFrame, startIndex, energy, startBand, stopBand, numberCols)
}

// RunMapPanorama runs mapPanorama.
func RunMapPanorama(nrgVal, ampRes int) (pan, quantError int) { return mapPanorama(nrgVal, ampRes) }

// RunSbrNoiseFloorLevelsQuantisation runs the noise-floor quantiser.
func RunSbrNoiseFloorLevelsQuantisation(noiseLevels []int32, coupling int) []int8 {
	out := make([]int8, encMaxNumNoiseValues)
	SbrNoiseFloorLevelsQuantisation(out, noiseLevels, coupling)
	return out
}

// RunCoupleNoiseFloor runs the stereo noise coupling (returns mutated left/right).
func RunCoupleNoiseFloor(left, right []int32) (l, r []int32) {
	CoupleNoiseFloor(left, right)
	return left, right
}

// RunGetEnvSfbEnergy runs the per-SFB energy summation over a flat YBuffer
// (rows = numYRows, stride qmfChannels).
func RunGetEnvSfbEnergy(li, ui, startPos, stopPos, borderPos int, yFlat []int32, numYRows, qmfChannels, yBufferSzShift, scaleNrg0, scaleNrg1 int) int32 {
	y := splitRows(yFlat, numYRows, qmfChannels)
	return GetEnvSfbEnergy(li, ui, startPos, stopPos, borderPos, y, yBufferSzShift, scaleNrg0, scaleNrg1)
}

// RunMhLoweringEnergy / RunNmhLoweringEnergy expose the compensation leaves.
func RunMhLoweringEnergy(nrg int32, M int) int32 { return MhLoweringEnergy(nrg, M) }
func RunNmhLoweringEnergy(nrg, nrgSum int32, nrgSumScale, M int) int32 {
	return NmhLoweringEnergy(nrg, nrgSum, nrgSumScale, M)
}

// splitRows views a flat int32 buffer as rows rows of stride columns.
func splitRows(flat []int32, rows, stride int) [][]int32 {
	out := make([][]int32, rows)
	for i := 0; i < rows; i++ {
		out[i] = flat[i*stride : i*stride+stride]
	}
	return out
}
