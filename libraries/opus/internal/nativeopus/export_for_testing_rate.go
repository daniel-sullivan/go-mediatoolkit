package nativeopus

// Rate / allocation test shims. Construct a Go OpusCustomMode from
// caller-supplied field mirrors (eBands / logN / allocVectors /
// cache.bits / cache.index), matching the C static-mode fields so
// parity tests share identical inputs.

type CeltModeHandle struct{ p *OpusCustomMode }

func NewCeltModeFromData(
	Fs int32, overlap int,
	nbEBands, effEBands int,
	eBands []int16,
	maxLM, nbShortMdcts, shortMdctSize int,
	nbAllocVectors int,
	allocVectors []byte,
	logN []int16,
	cacheSize int,
	cacheIndex []int16,
	cacheBits []byte,
	cacheCaps []byte,
) CeltModeHandle {
	m := &OpusCustomMode{
		Fs:             opus_int32(Fs),
		overlap:        overlap,
		nbEBands:       nbEBands,
		effEBands:      effEBands,
		maxLM:          maxLM,
		nbShortMdcts:   nbShortMdcts,
		shortMdctSize:  shortMdctSize,
		nbAllocVectors: nbAllocVectors,
	}
	m.eBands = make([]opus_int16, len(eBands))
	for i, v := range eBands {
		m.eBands[i] = opus_int16(v)
	}
	m.allocVectors = append([]byte(nil), allocVectors...)
	m.logN = make([]opus_int16, len(logN))
	for i, v := range logN {
		m.logN[i] = opus_int16(v)
	}
	m.cache.size = cacheSize
	m.cache.index = make([]opus_int16, len(cacheIndex))
	for i, v := range cacheIndex {
		m.cache.index[i] = opus_int16(v)
	}
	m.cache.bits = append([]byte(nil), cacheBits...)
	m.cache.caps = append([]byte(nil), cacheCaps...)
	return CeltModeHandle{p: m}
}

// SetModePreemph sets the 4-element preemphasis coefficients on the
// mode mirror.
func (h CeltModeHandle) SetModePreemph(p [4]float32) {
	for i, v := range p {
		h.p.preemph[i] = opus_val16(v)
	}
}

// SetModeWindow sets the overlap window coefficient slice.
func (h CeltModeHandle) SetModeWindow(w []float32) {
	h.p.window = make([]celt_coef, len(w))
	for i, v := range w {
		h.p.window[i] = celt_coef(v)
	}
}

// SetModeMdct installs a Go mdct_lookup (built via NewMdctLookupFromData)
// on the mode mirror. Parity tests share the same trig/FFT tables so
// the Go decoder uses the exact same data as the C oracle.
func (h CeltModeHandle) SetModeMdct(mh MdctLookupHandle) {
	h.p.mdct = *mh.p
}

// ExportTestCltComputeAllocation — wrapper so parity tests can call
// clt_compute_allocation with the sanitized Go ec_ctx.
func ExportTestCltComputeAllocation(
	h CeltModeHandle, start, end int,
	offsets, cap_ []int,
	allocTrim int,
	intensity, dualStereo *int,
	total int32, balance *int32,
	pulses, ebits, finePriority []int,
	C, LM int,
	ec EcCtxHandle,
	encode, prev, signalBandwidth int,
) int {
	bal := opus_int32(*balance)
	cb := clt_compute_allocation(h.p, start, end, offsets, cap_, allocTrim,
		intensity, dualStereo, opus_int32(total), &bal,
		pulses, ebits, finePriority, C, LM, ec.p, encode, prev, signalBandwidth)
	*balance = int32(bal)
	return cb
}

// ExportTestGetPulses exposes the get_pulses inline.
func ExportTestGetPulses(i int) int { return get_pulses(i) }

// ExportTestBits2Pulses / ExportTestPulses2Bits expose the cache
// lookups for direct parity testing.
func ExportTestBits2Pulses(h CeltModeHandle, band, LM, bits int) int {
	return bits2pulses(h.p, band, LM, bits)
}
func ExportTestPulses2Bits(h CeltModeHandle, band, LM, pulses int) int {
	return pulses2bits(h.p, band, LM, pulses)
}
