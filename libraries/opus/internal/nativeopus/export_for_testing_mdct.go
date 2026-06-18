package nativeopus

// MDCT test shims. Like the FFT shims, we accept caller-supplied trig
// tables + per-shift FFT states so the Go mdct_lookup uses the same
// bit-exact data the C oracle does (independent of Go's math.Cos vs
// Apple libm cos differences).

type MdctLookupHandle struct{ p *mdct_lookup }

// NewMdctLookupFromData builds an mdct_lookup with caller-provided
// FFT states (one per shift) and trig table. Each entry of `ffts` is
// an FftStateHandle obtained via NewFftStateFromData.
func NewMdctLookupFromData(N, maxshift int, ffts []FftStateHandle, trig []float32) MdctLookupHandle {
	l := &mdct_lookup{n: N, maxshift: maxshift}
	for i := 0; i < len(ffts) && i < len(l.kfft); i++ {
		l.kfft[i] = ffts[i].p
	}
	l.trig = make([]kiss_twiddle_scalar, len(trig))
	for i, v := range trig {
		l.trig[i] = kiss_twiddle_scalar(v)
	}
	// Per-call scratch for clt_mdct_forward_c; sized to the shift=0
	// maxima (N/2 and N/4).
	l.scratchF = make([]kiss_fft_scalar, N>>1)
	l.scratchF2 = make([]kiss_fft_cpx, N>>2)
	return MdctLookupHandle{p: l}
}

// ExportTestCltMdctForward — forward MDCT. Input will be modified
// in place (matches C docstring "trashes the input array").
func ExportTestCltMdctForward(h MdctLookupHandle, in, out, window []float32,
	overlap, shift, stride int) {
	clt_mdct_forward_c(h.p, in, out, window, overlap, shift, stride, 0)
}

// ExportTestCltMdctBackward — inverse MDCT + TDAC.
func ExportTestCltMdctBackward(h MdctLookupHandle, in, out, window []float32,
	overlap, shift, stride int) {
	clt_mdct_backward_c(h.p, in, out, window, overlap, shift, stride, 0)
}
