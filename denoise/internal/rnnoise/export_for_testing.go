package rnnoise

// Test-only accessors that expose the embedded tables and static FFT
// state to the external parity_tests packages (which cannot see
// unexported identifiers). Mirrors the opus port's
// export_for_testing_fft.go convention.

// FFTBitrev returns the embedded 960-entry bit-reversal table.
func FFTBitrev() []int32 { return fftBitrev[:] }

// FFTTwiddleR / FFTTwiddleI return the real / imaginary parts of the
// embedded 960-entry twiddle table.
func FFTTwiddleR() []float32 {
	r := make([]float32, len(fftTwiddles))
	for i := range fftTwiddles {
		r[i] = fftTwiddles[i].r
	}
	return r
}

func FFTTwiddleI() []float32 {
	out := make([]float32, len(fftTwiddles))
	for i := range fftTwiddles {
		out[i] = fftTwiddles[i].i
	}
	return out
}

// HalfWindow returns the embedded rnn_half_window[480].
func HalfWindow() []float32 { return rnnHalfWindow[:] }

// DctTable returns the embedded rnn_dct_table[1024].
func DctTable() []float32 { return rnnDctTable[:] }

// KFFTParams returns the static rnn_kfft scalar parameters for parity
// checks: nfft, scale, shift, and the 16-entry factors array.
func KFFTParams() (nfft int, scale float32, shift int, factors [2 * maxFactors]int16) {
	return rnnKFFT.nfft, rnnKFFT.scale, rnnKFFT.shift, rnnKFFT.factors
}

// BiquadHP runs the fixed input high-pass biquad (rnn_biquad with the
// b_hp/a_hp coefficients) over x into y, threading state through mem.
func BiquadHP(y []float32, mem *[2]float32, x []float32) {
	biquad(y, mem, x, hpB, hpA, len(x))
}

// ForwardTransform runs denoise.c forward_transform: WindowSize real
// input -> FreqSize complex bins, returned as parallel real/imag slices.
func ForwardTransform(in []float32) (outR, outI []float32) {
	var out [FreqSize]fftCpx
	forwardTransform(out[:], in)
	outR = make([]float32, FreqSize)
	outI = make([]float32, FreqSize)
	for i := range out {
		outR[i] = out[i].r
		outI[i] = out[i].i
	}
	return outR, outI
}

// InverseTransform runs denoise.c inverse_transform: FreqSize complex
// bins (parallel real/imag) -> WindowSize real output.
func InverseTransform(inR, inI []float32) []float32 {
	var in [FreqSize]fftCpx
	for i := 0; i < FreqSize; i++ {
		in[i].r = inR[i]
		in[i].i = inI[i]
	}
	out := make([]float32, WindowSize)
	inverseTransform(out, in[:])
	return out
}

// ApplyWindow applies denoise.c apply_window in place over WindowSize.
func ApplyWindow(x []float32) { applyWindow(x) }

// Eband returns the eband20ms band-edge table for parity checks.
func Eband() []int { return eband20ms[:] }

// ComputeBandEnergy runs denoise.c compute_band_energy over a FreqSize
// spectrum (parallel real/imag), returning NBBands band energies.
func ComputeBandEnergy(xr, xi []float32) []float32 {
	X := make([]fftCpx, FreqSize)
	for i := range X {
		X[i] = fftCpx{xr[i], xi[i]}
	}
	bandE := make([]float32, NBBands)
	computeBandEnergy(bandE, X)
	return bandE
}

// ComputeBandCorr runs denoise.c compute_band_corr over two FreqSize
// spectra, returning NBBands band cross-powers.
func ComputeBandCorr(xr, xi, pr, pi []float32) []float32 {
	X := make([]fftCpx, FreqSize)
	P := make([]fftCpx, FreqSize)
	for i := range X {
		X[i] = fftCpx{xr[i], xi[i]}
		P[i] = fftCpx{pr[i], pi[i]}
	}
	bandE := make([]float32, NBBands)
	computeBandCorr(bandE, X, P)
	return bandE
}

// InterpBandGain runs denoise.c interp_band_gain, returning a zeroed
// FreqSize gain curve with bins [0,400) interpolated from bandE.
func InterpBandGain(bandE []float32) []float32 {
	g := make([]float32, FreqSize)
	interpBandGain(g, bandE)
	return g
}

// Dct runs denoise.c dct over NBBands inputs.
func Dct(in []float32) []float32 {
	out := make([]float32, NBBands)
	dct(out, in)
	return out
}

// PitchDownsample runs pitch.c rnn_pitch_downsample (C==1) over a
// length-len input, returning the len/2 downsampled buffer.
func PitchDownsample(x []float32, length int) []float32 {
	xLp := make([]float32, length>>1)
	pitchDownsample(x, xLp, length)
	return xLp
}

// PitchSearch runs pitch.c rnn_pitch_search over xLp/y (which may alias).
func PitchSearch(xLp, y []float32, length, maxPitch int) int {
	return pitchSearch(xLp, y, length, maxPitch)
}

// RemoveDoubling runs pitch.c rnn_remove_doubling, returning the gain and
// the updated pitch lag.
func RemoveDoubling(x []float32, maxperiod, minperiod, n, t0, prevPeriod int, prevGain float32) (gain float32, newT0 int) {
	return removeDoubling(x, maxperiod, minperiod, n, t0, prevPeriod, prevGain)
}

// ComputeFrameFeatures runs denoise.c rnn_compute_frame_features on this
// State for one FrameSize input frame, returning the NBFeatures feature
// vector, the silence flag, and the NBBands Ex/Ep/Exp energies. The State
// carries the analysis/pitch history across calls.
func (s *State) ComputeFrameFeatures(in []float32) (features []float32, silence bool, Ex, Ep, Exp []float32) {
	X := make([]fftCpx, FreqSize)
	P := make([]fftCpx, FreqSize)
	Ex = make([]float32, NBBands)
	Ep = make([]float32, NBBands)
	Exp = make([]float32, NBBands)
	features = make([]float32, NBFeatures)
	silence = s.computeFrameFeatures(X, P, Ex, Ep, Exp, features, in)
	return features, silence, Ex, Ep, Exp
}

// RnnStateHandle exposes an rnnState for the network parity slice to
// drive across frames.
type RnnStateHandle struct{ st rnnState }

// NewRnnState returns a zeroed recurrent-network state.
func NewRnnState() *RnnStateHandle { return new(RnnStateHandle) }

// Step runs rnn.c compute_rnn for one NBFeatures input, returning the
// NBBands band gains and the VAD probability, updating the state.
func (h *RnnStateHandle) Step(input []float32) (gains []float32, vad float32) {
	gains = make([]float32, NBBands)
	v := make([]float32, 1)
	computeRnn(theModel(), &h.st, gains, v, input)
	return gains, v[0]
}
