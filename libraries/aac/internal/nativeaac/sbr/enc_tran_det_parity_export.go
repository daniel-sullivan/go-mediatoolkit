// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// Thin exported drivers for the sbr-enc-analysis cgo parity oracle's transient
// detector slice (internal/parity_tests/sbr-enc-analysis). They add no logic:
// each rebuilds the [][]int32 slot views the ports take from a flat input the
// oracle shares with the genuine C, runs the port, and returns the decision +
// the mutated detector state so the oracle can assert EXACT int equality vs the
// vendored FDKsbrEnc_* symbol.

// RunInitFastTransientDetector inits a FastTranDetector and returns its dBf_m /
// dBf_e weighting ROM (the load-bearing fixed-point output of the init).
func RunInitFastTransientDetector(timeSlotsPerFrame, bandwidthQmfSlot, noQmfChannels, sbrQmf1stBand int) (dBfM []int32, dBfE []int, startBand, stopBand int) {
	var h FastTranDetector
	InitSbrFastTransientDetector(&h, timeSlotsPerFrame, bandwidthQmfSlot, noQmfChannels, sbrQmf1stBand)
	dBfM = append([]int32(nil), h.DBfM[:]...)
	dBfE = append([]int(nil), h.DBfE[:]...)
	return dBfM, dBfE, h.StartBand, h.StopBand
}

// RunFastTransientDetect inits the fast detector then runs it over noSlots+lookahead
// rows of the flat energy matrix (row stride noQmfChannels), returning tran_vector.
func RunFastTransientDetect(energyFlat []int32, rows, noQmfChannels int, scaleEnergies []int, yBufferWriteOffset, timeSlotsPerFrame, bandwidthQmfSlot, sbrQmf1stBand int) []uint8 {
	var h FastTranDetector
	InitSbrFastTransientDetector(&h, timeSlotsPerFrame, bandwidthQmfSlot, noQmfChannels, sbrQmf1stBand)

	energies := make([][]int32, rows)
	for i := 0; i < rows; i++ {
		energies[i] = energyFlat[i*noQmfChannels : i*noQmfChannels+noQmfChannels]
	}
	tranVector := make([]uint8, 3)
	FastTransientDetect(&h, energies, scaleEnergies, yBufferWriteOffset, tranVector)
	return tranVector
}

// RunInitTransientDetector inits a standard detector and returns the seeded
// tran_thr, split_thr_m, split_thr_e the init computes.
func RunInitTransientDetector(lowDelay bool, frameSize, sampleFreq, standardBitrate, nChannels, codecBitrate, tranThr, tranDetMode, tranFc, noCols, noRows, frameShift, tranOff int) (tranThrOut, splitThrM int32, splitThrE int) {
	var h SbrTransientDetector
	InitSbrTransientDetector(&h, lowDelay, frameSize, sampleFreq, standardBitrate, nChannels, codecBitrate, tranThr, tranDetMode, tranFc, noCols, noRows, frameShift, tranOff)
	return h.TranThr, h.SplitThrM, h.SplitThrE
}

// RunTransientDetect inits a standard detector with the given scalars, seeds its
// thresholds/transients from the supplied state, runs it over the flat energy
// matrix (row stride noRowsStride), and returns transient_info plus the mutated
// thresholds + transients rings for full-state comparison.
func RunTransientDetect(energyFlat []int32, rows, rowStride int, scaleEnergies []int, lowDelay bool, frameSize, sampleFreq, standardBitrate, nChannels, codecBitrate, tranThr, tranDetMode, tranFc, noCols, noRows, frameShift, tranOff, yBufferWriteOffset, yBufferSzShift, timeStep, frameMiddleBorder int) (transientInfo []uint8, thresholds, transients []int32) {
	var h SbrTransientDetector
	InitSbrTransientDetector(&h, lowDelay, frameSize, sampleFreq, standardBitrate, nChannels, codecBitrate, tranThr, tranDetMode, tranFc, noCols, noRows, frameShift, tranOff)

	energies := make([][]int32, rows)
	for i := 0; i < rows; i++ {
		energies[i] = energyFlat[i*rowStride : i*rowStride+rowStride]
	}
	transientInfo = make([]uint8, 3)
	TransientDetect(&h, energies, scaleEnergies, transientInfo, yBufferWriteOffset, yBufferSzShift, timeStep, frameMiddleBorder)
	thresholds = append([]int32(nil), h.Thresholds[:]...)
	transients = append([]int32(nil), h.Transients[:]...)
	return transientInfo, thresholds, transients
}

// RunFrameSplitter inits a standard detector, seeds prevLowBandEnergy, runs the
// FIXFIX splitter over the flat energy matrix, and returns tran_vector[0], the
// updated prev energies and tonality.
func RunFrameSplitter(energyFlat []int32, rows, rowStride int, scaleEnergies []int, lowDelay bool, frameSize, sampleFreq, standardBitrate, nChannels, codecBitrate, tranThr, tranDetMode, tranFc, noCols, noRows, frameShift, tranOff int, prevLowBandEnergy int32, freqBandTable []uint8, tranVectorIn []uint8, yBufferWriteOffset, yBufferSzShift, nSfb, timeStep int, tonalityIn int32) (tranVector []uint8, prevLow, prevHigh, tonality int32) {
	var h SbrTransientDetector
	InitSbrTransientDetector(&h, lowDelay, frameSize, sampleFreq, standardBitrate, nChannels, codecBitrate, tranThr, tranDetMode, tranFc, noCols, noRows, frameShift, tranOff)
	h.PrevLowBandEnergy = prevLowBandEnergy

	energies := make([][]int32, rows)
	for i := 0; i < rows; i++ {
		energies[i] = energyFlat[i*rowStride : i*rowStride+rowStride]
	}
	tranVector = append([]uint8(nil), tranVectorIn...)
	tonality = tonalityIn
	FrameSplitter(energies, scaleEnergies, &h, freqBandTable, tranVector, yBufferWriteOffset, yBufferSzShift, nSfb, timeStep, noCols, &tonality)
	return tranVector, h.PrevLowBandEnergy, h.PrevHighBandEnergy, tonality
}
