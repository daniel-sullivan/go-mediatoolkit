package nativeopus

// Bands test shims.

func ExportTestBitexactCos(x int16) int16          { return int16(bitexact_cos(opus_int16(x))) }
func ExportTestBitexactLog2Tan(isin, icos int) int { return bitexact_log2tan(isin, icos) }
func ExportTestCeltLcgRand(seed uint32) uint32     { return uint32(celt_lcg_rand(opus_uint32(seed))) }

func ExportTestHysteresisDecision(val float32, thresholds, hysteresis []float32, N, prev int) int {
	return hysteresis_decision(opus_val16(val), thresholds, hysteresis, N, prev)
}

func ExportTestComputeBandEnergies(h CeltModeHandle, X []float32, bandE []float32, end, C, LM int) {
	compute_band_energies(h.p, X, bandE, end, C, LM, 0)
}

func ExportTestNormaliseBands(h CeltModeHandle, freq, X, bandE []float32, end, C, M int) {
	normalise_bands(h.p, freq, X, bandE, end, C, M)
}

func ExportTestDenormaliseBands(h CeltModeHandle, X []float32, freq []float32,
	bandLogE []float32, start, end, M, downsample, silence int) {
	denormalise_bands(h.p, X, freq, bandLogE, start, end, M, downsample, silence)
}

func ExportTestAntiCollapse(h CeltModeHandle, X []float32, collapse_masks []byte,
	LM, C, size, start, end int, logE, prev1logE, prev2logE []float32,
	pulses []int, seed uint32, encode int) {
	anti_collapse(h.p, X, collapse_masks, LM, C, size, start, end,
		logE, prev1logE, prev2logE, pulses, opus_uint32(seed), encode, 0)
}

func ExportTestIntensityStereo(h CeltModeHandle, X, Y []float32, bandE []float32, bandID, N int) {
	intensity_stereo(h.p, X, Y, bandE, bandID, N)
}

func ExportTestStereoSplit(X, Y []float32, N int) { stereo_split(X, Y, N) }

func ExportTestStereoMerge(X, Y []float32, mid float32, N int) {
	stereo_merge(X, Y, opus_val32(mid), N, 0)
}

func ExportTestHaar1(X []float32, N0, stride int) { haar1(X, N0, stride) }

func ExportTestDeinterleaveHadamard(X []float32, N0, stride, hadamard int) {
	deinterleave_hadamard(X, N0, stride, hadamard, make([]celt_norm, N0*stride))
}

func ExportTestInterleaveHadamard(X []float32, N0, stride, hadamard int) {
	interleave_hadamard(X, N0, stride, hadamard, make([]celt_norm, N0*stride))
}

func ExportTestComputeQn(N, b, offset, pulse_cap, stereo int) int {
	return compute_qn(N, b, offset, pulse_cap, stereo)
}

func ExportTestSpreadingDecision(h CeltModeHandle, X []float32, average *int,
	lastDecision int, hfAverage, tapsetDecision *int, updateHf, end, C, M int,
	spreadWeight []int) int {
	return spreading_decision(h.p, X, average, lastDecision, hfAverage,
		tapsetDecision, updateHf, end, C, M, spreadWeight)
}

func ExportTestQuantAllBands(encode int, h CeltModeHandle, start, end int,
	X, Y []float32, collapseMasks []byte, bandE []float32, pulses []int,
	shortBlocks, spread, dualStereo, intensity int, tfRes []int,
	totalBits, balance int32, ec EcCtxHandle,
	LM, codedBands int, seed *uint32, complexity, disableInv int) {
	s := opus_uint32(*seed)
	// Test harness provides fresh scratch per call.
	C := 1
	if Y != nil {
		C = 2
	}
	maxN := h.p.shortMdctSize << h.p.maxLM
	scratchNorm := make([]celt_norm, C*(1<<LM)*int(h.p.eBands[h.p.nbEBands-1]))
	scratchHad := make([]celt_norm, C*maxN)
	scratchIy := make([]int, C*maxN)
	quant_all_bands(encode, h.p, start, end, X, Y, collapseMasks, bandE,
		pulses, shortBlocks, spread, dualStereo, intensity, tfRes,
		opus_int32(totalBits), opus_int32(balance), ec.p,
		LM, codedBands, &s, complexity, 0, disableInv,
		scratchNorm, scratchHad, scratchIy)
	*seed = uint32(s)
}
