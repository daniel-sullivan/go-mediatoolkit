package nativeopus

// quant_bands test shims.

func ExportTestAmp2Log2(h CeltModeHandle, effEnd, end int, bandE []float32, bandLogE []float32, C int) {
	amp2Log2(h.p, effEnd, end, bandE, bandLogE, C)
}

func ExportTestQuantCoarseEnergy(h CeltModeHandle, start, end, effEnd int,
	eBands, oldEBands []float32, budget uint32, error []float32,
	enc EcCtxHandle, C, LM, nbAvailableBytes, forceIntra int,
	delayedIntra *float32, twoPass, lossRate, lfe int) {
	di := opus_val32(*delayedIntra)
	quant_coarse_energy(h.p, start, end, effEnd, eBands, oldEBands,
		opus_uint32(budget), error, enc.p, C, LM, nbAvailableBytes,
		forceIntra, &di, twoPass, lossRate, lfe)
	*delayedIntra = float32(di)
}

func ExportTestQuantFineEnergy(h CeltModeHandle, start, end int,
	oldEBands, error []float32, prevQuant, extraQuant []int,
	enc EcCtxHandle, C int) {
	quant_fine_energy(h.p, start, end, oldEBands, error, prevQuant, extraQuant, enc.p, C)
}

func ExportTestQuantEnergyFinalise(h CeltModeHandle, start, end int,
	oldEBands, error []float32, fineQuant, finePriority []int,
	bitsLeft int, enc EcCtxHandle, C int) {
	quant_energy_finalise(h.p, start, end, oldEBands, error, fineQuant, finePriority, bitsLeft, enc.p, C)
}

func ExportTestUnquantCoarseEnergy(h CeltModeHandle, start, end int,
	oldEBands []float32, intra int, dec EcCtxHandle, C, LM int) {
	unquant_coarse_energy(h.p, start, end, oldEBands, intra, dec.p, C, LM)
}

func ExportTestUnquantFineEnergy(h CeltModeHandle, start, end int,
	oldEBands []float32, prevQuant, extraQuant []int,
	dec EcCtxHandle, C int) {
	unquant_fine_energy(h.p, start, end, oldEBands, prevQuant, extraQuant, dec.p, C)
}

func ExportTestUnquantEnergyFinalise(h CeltModeHandle, start, end int,
	oldEBands []float32, fineQuant, finePriority []int,
	bitsLeft int, dec EcCtxHandle, C int) {
	unquant_energy_finalise(h.p, start, end, oldEBands, fineQuant, finePriority, bitsLeft, dec.p, C)
}

func ExportTestLossDistortion(eBands, oldEBands []float32, start, end, length, C int) float32 {
	return float32(loss_distortion(eBands, oldEBands, start, end, length, C))
}
