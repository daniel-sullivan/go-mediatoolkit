package rnnoise

// Time/frequency transforms, ported from librnnoise/src/denoise.c
// (forward_transform, inverse_transform, apply_window). Both transforms
// use the FORWARD FFT: inverse_transform reconstructs a full
// conjugate-symmetric spectrum and forward-transforms it.

// forwardTransform is denoise.c forward_transform: real input of length
// WindowSize -> the first FreqSize complex bins.
func forwardTransform(out []fftCpx, in []float32) {
	var x [WindowSize]fftCpx
	var y [WindowSize]fftCpx
	for i := 0; i < WindowSize; i++ {
		x[i].r = in[i]
		x[i].i = 0
	}
	fftForward(&rnnKFFT, x[:], y[:])
	for i := 0; i < FreqSize; i++ {
		out[i] = y[i]
	}
}

// inverseTransform is denoise.c inverse_transform: FreqSize complex bins
// -> real output of length WindowSize (in reversed order, scaled by
// WindowSize).
func inverseTransform(out []float32, in []fftCpx) {
	var x [WindowSize]fftCpx
	var y [WindowSize]fftCpx
	i := 0
	for ; i < FreqSize; i++ {
		x[i] = in[i]
	}
	for ; i < WindowSize; i++ {
		x[i].r = x[WindowSize-i].r
		x[i].i = -x[WindowSize-i].i
	}
	fftForward(&rnnKFFT, x[:], y[:])
	out[0] = mul32(WindowSize, y[0].r)
	for i = 1; i < WindowSize; i++ {
		out[i] = mul32(WindowSize, y[WindowSize-i].r)
	}
}

// applyWindow is denoise.c apply_window: the symmetric sqrt-Vorbis window
// (rnn_half_window) applied in place over WindowSize samples.
func applyWindow(x []float32) {
	for i := 0; i < FrameSize; i++ {
		x[i] = mul32(x[i], rnnHalfWindow[i])
		x[WindowSize-1-i] = mul32(x[WindowSize-1-i], rnnHalfWindow[i])
	}
}
