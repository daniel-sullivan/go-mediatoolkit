package nativeopus

// FFT test shims. The test harness builds a kiss_fft_state from
// externally supplied twiddles/bitrev/factors so we can share C-
// computed trig values and isolate butterfly parity from the cos/sin
// implementation-difference question (Go's math.Cos vs Apple libm
// are not guaranteed bit-exact).

type FftStateHandle struct{ p *kiss_fft_state }

// SetFftTwiddles lets tests share a twiddle slice across multiple
// states. The C opus_fft_alloc_twiddles(..., base, ...) path points
// sub-states' twiddles pointer at the base's table — necessary
// because sub-state butterflies index past their own nfft into the
// larger base twiddles. Go must do the same or kf_bfly{3,5} panic
// with an out-of-range slice access.
func (h FftStateHandle) SetFftTwiddles(tw []kiss_twiddle_cpx) {
	h.p.twiddles = tw
}

// FftTwiddles returns the backing twiddle slice of a Go state, so
// test helpers can share it across sub-states.
func (h FftStateHandle) FftTwiddles() []kiss_twiddle_cpx {
	return h.p.twiddles
}

// NewFftStateFromData populates a kiss_fft_state with caller-provided
// tables — useful in tests where we want identical twiddles on both
// sides. C: equivalent to manually initialising the fields opus_fft_
// alloc_twiddles would have set.
func NewFftStateFromData(
	nfft int,
	scale float32,
	shift int,
	factors []int16,
	bitrev []int16,
	twiddleR, twiddleI []float32,
) FftStateHandle {
	st := &kiss_fft_state{
		nfft:  nfft,
		scale: celt_coef(scale),
		shift: shift,
	}
	for i, f := range factors {
		if i >= 2*MAXFACTORS {
			break
		}
		st.factors[i] = opus_int16(f)
	}
	st.bitrev = make([]opus_int16, len(bitrev))
	for i, b := range bitrev {
		st.bitrev[i] = opus_int16(b)
	}
	st.twiddles = make([]kiss_twiddle_cpx, len(twiddleR))
	for i := range twiddleR {
		st.twiddles[i] = kiss_twiddle_cpx{
			r: kiss_twiddle_scalar(twiddleR[i]),
			i: kiss_twiddle_scalar(twiddleI[i]),
		}
	}
	return FftStateHandle{p: st}
}

// ExportTestOpusFFTC — run Go opus_fft_c. Input / output are flat
// (real, imag, real, imag, ...) arrays of length 2*nfft.
func ExportTestOpusFFTC(h FftStateHandle, in, out []float32) {
	n := h.p.nfft
	fin := make([]kiss_fft_cpx, n)
	fout := make([]kiss_fft_cpx, n)
	for i := 0; i < n; i++ {
		fin[i].r = in[2*i]
		fin[i].i = in[2*i+1]
	}
	opus_fft_c(h.p, fin, fout)
	for i := 0; i < n; i++ {
		out[2*i] = fout[i].r
		out[2*i+1] = fout[i].i
	}
}

// ExportTestOpusIFFTC — inverse form.
func ExportTestOpusIFFTC(h FftStateHandle, in, out []float32) {
	n := h.p.nfft
	fin := make([]kiss_fft_cpx, n)
	fout := make([]kiss_fft_cpx, n)
	for i := 0; i < n; i++ {
		fin[i].r = in[2*i]
		fin[i].i = in[2*i+1]
	}
	opus_ifft_c(h.p, fin, fout)
	for i := 0; i < n; i++ {
		out[2*i] = fout[i].r
		out[2*i+1] = fout[i].i
	}
}
