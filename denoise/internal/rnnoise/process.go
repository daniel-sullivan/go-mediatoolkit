package rnnoise

import "math"

// Pitch filtering and overlap-add synthesis, ported 1:1 from
// librnnoise/src/denoise.c (rnn_pitch_filter, frame_synthesis). The
// several sqrt/divide steps mix float32 and float64 exactly as the C
// does (the .001 / 1e-8 constants are doubles; SQUARE products are
// float32).

// pitchFilter is denoise.c rnn_pitch_filter: mixes the pitch-predicted
// spectrum P back into X per band, then re-normalises X to preserve the
// original band energies. X is modified in place.
func pitchFilter(X, P []fftCpx, Ex, Ep, Exp, g []float32) {
	var r [NBBands]float32
	var newE [NBBands]float32
	var norm [NBBands]float32
	rf := make([]float32, FreqSize)
	normf := make([]float32, FreqSize)

	for i := 0; i < NBBands; i++ {
		if Exp[i] > g[i] {
			r[i] = 1
		} else {
			se := mul32(Exp[i], Exp[i])
			sg := mul32(g[i], g[i])
			num := mul32(se, sub32(1, sg))
			den := 0.001 + float64(mul32(sg, sub32(1, se)))
			r[i] = float32(float64(num) / den)
		}
		r[i] = float32(math.Sqrt(float64(minf(1, maxf(0, r[i])))))
		r[i] = float32(float64(r[i]) * math.Sqrt(float64(Ex[i])/(1e-8+float64(Ep[i]))))
	}
	interpBandGain(rf, r[:])
	for i := 0; i < FreqSize; i++ {
		X[i].r = mla(X[i].r, rf[i], P[i].r)
		X[i].i = mla(X[i].i, rf[i], P[i].i)
	}
	computeBandEnergy(newE[:], X)
	for i := 0; i < NBBands; i++ {
		norm[i] = float32(math.Sqrt(float64(Ex[i]) / (1e-8 + float64(newE[i]))))
	}
	interpBandGain(normf, norm[:])
	for i := 0; i < FreqSize; i++ {
		X[i].r = mul32(X[i].r, normf[i])
		X[i].i = mul32(X[i].i, normf[i])
	}
}

// frameSynthesis is denoise.c frame_synthesis: inverse-transform the
// (gained) spectrum, window it, and overlap-add with the retained
// synthesis memory.
func (s *State) frameSynthesis(out []float32, y []fftCpx) {
	var x [WindowSize]float32
	inverseTransform(x[:], y)
	applyWindow(x[:])
	for i := 0; i < FrameSize; i++ {
		out[i] = add32(x[i], s.synthesisMem[i])
	}
	copy(s.synthesisMem[:], x[FrameSize:])
}
