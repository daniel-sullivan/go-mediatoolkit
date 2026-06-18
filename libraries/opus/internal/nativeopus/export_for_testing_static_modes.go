package nativeopus

// Test-visible accessors for the ported mode48000_960_120 static mode
// descriptor. These let parity tests compare every field of the Go
// static mode against its C counterpart byte-exactly.

// StaticModeHandle wraps the ported singleton CELT mode for test use.
type StaticModeHandle struct{ p *OpusCustomMode }

// ExportStaticMode48000 returns the singleton mode48000_960_120.
func ExportStaticMode48000() StaticModeHandle {
	return StaticModeHandle{p: StaticMode48000_960_120()}
}

func (h StaticModeHandle) Fs() int32           { return int32(h.p.Fs) }
func (h StaticModeHandle) Overlap() int        { return h.p.overlap }
func (h StaticModeHandle) NbEBands() int       { return h.p.nbEBands }
func (h StaticModeHandle) EffEBands() int      { return h.p.effEBands }
func (h StaticModeHandle) MaxLM() int          { return h.p.maxLM }
func (h StaticModeHandle) NbShortMdcts() int   { return h.p.nbShortMdcts }
func (h StaticModeHandle) ShortMdctSize() int  { return h.p.shortMdctSize }
func (h StaticModeHandle) NbAllocVectors() int { return h.p.nbAllocVectors }
func (h StaticModeHandle) Preemph() [4]float32 {
	var p [4]float32
	for i := 0; i < 4; i++ {
		p[i] = float32(h.p.preemph[i])
	}
	return p
}
func (h StaticModeHandle) EBands() []int16 {
	out := make([]int16, len(h.p.eBands))
	for i, v := range h.p.eBands {
		out[i] = int16(v)
	}
	return out
}
func (h StaticModeHandle) LogN() []int16 {
	out := make([]int16, len(h.p.logN))
	for i, v := range h.p.logN {
		out[i] = int16(v)
	}
	return out
}
func (h StaticModeHandle) AllocVectors() []byte {
	return append([]byte(nil), h.p.allocVectors...)
}
func (h StaticModeHandle) Window() []float32 {
	out := make([]float32, len(h.p.window))
	for i, v := range h.p.window {
		out[i] = float32(v)
	}
	return out
}

// MDCT lookup accessors.
func (h StaticModeHandle) MdctN() int        { return h.p.mdct.n }
func (h StaticModeHandle) MdctMaxshift() int { return h.p.mdct.maxshift }
func (h StaticModeHandle) MdctTrig() []float32 {
	out := make([]float32, len(h.p.mdct.trig))
	for i, v := range h.p.mdct.trig {
		out[i] = float32(v)
	}
	return out
}

// Per-shift FFT state accessors.
func (h StaticModeHandle) FftNfft(s int) int      { return h.p.mdct.kfft[s].nfft }
func (h StaticModeHandle) FftScale(s int) float32 { return float32(h.p.mdct.kfft[s].scale) }
func (h StaticModeHandle) FftShift(s int) int     { return h.p.mdct.kfft[s].shift }
func (h StaticModeHandle) FftFactors(s int) [16]int16 {
	var out [16]int16
	for i := 0; i < 16; i++ {
		out[i] = int16(h.p.mdct.kfft[s].factors[i])
	}
	return out
}
func (h StaticModeHandle) FftBitrev(s int) []int16 {
	st := h.p.mdct.kfft[s]
	out := make([]int16, len(st.bitrev))
	for i, v := range st.bitrev {
		out[i] = int16(v)
	}
	return out
}
func (h StaticModeHandle) FftTwiddlesLen(s int) int {
	return len(h.p.mdct.kfft[s].twiddles)
}
func (h StaticModeHandle) FftTwiddle(s, i int) (float32, float32) {
	t := h.p.mdct.kfft[s].twiddles[i]
	return float32(t.r), float32(t.i)
}

// Pulse cache accessors.
func (h StaticModeHandle) CacheSize() int { return h.p.cache.size }
func (h StaticModeHandle) CacheIndex() []int16 {
	out := make([]int16, len(h.p.cache.index))
	for i, v := range h.p.cache.index {
		out[i] = int16(v)
	}
	return out
}
func (h StaticModeHandle) CacheBits() []byte {
	return append([]byte(nil), h.p.cache.bits...)
}
func (h StaticModeHandle) CacheCaps() []byte {
	return append([]byte(nil), h.p.cache.caps...)
}
