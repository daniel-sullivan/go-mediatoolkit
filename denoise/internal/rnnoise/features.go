package rnnoise

import "math"

// Feature extraction, ported 1:1 from librnnoise/src/denoise.c
// (rnn_frame_analysis, rnn_compute_frame_features). This ties together
// the window/FFT, band, and pitch slices and adds several precision
// traps matched exactly:
//   - log10(1e-2+Ex[i]) is a true-libm double log10 (math.Log10),
//     result narrowed to float32.
//   - the Ly clamp MAX16(logMax-7, MAX16(follow-1.5, Ly)) mixes types:
//     logMax-7 is float32 (7 is int) while follow-1.5 is float64 (1.5 is
//     a double literal), so the inner maxes are computed in float64.
//   - Exp[i]/sqrt(.001+Ex[i]*Ep[i]) forms Ex*Ep in float32, then the
//     .001 add, sqrt, and divide in float64.

// maxd mirrors the C MAX16 ternary in double precision.
func maxd(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// frameAnalysis is denoise.c rnn_frame_analysis: window + FFT the current
// frame (prepended with the retained analysis memory) and take its band
// energies. X must be FreqSize, Ex NBBands.
func (s *State) frameAnalysis(X []fftCpx, Ex []float32, in []float32) {
	var x [WindowSize]float32
	copy(x[:FrameSize], s.analysisMem[:])
	for i := 0; i < FrameSize; i++ {
		x[FrameSize+i] = in[i]
	}
	copy(s.analysisMem[:], in[:FrameSize])
	applyWindow(x[:])
	forwardTransform(X, x[:])
	computeBandEnergy(Ex, X)
}

// computeFrameFeatures is denoise.c rnn_compute_frame_features. It fills X,
// P (FreqSize), Ex, Ep, Exp (NBBands), and features (NBFeatures), updates
// the pitch state, and returns true when the frame is treated as silence
// (E < 0.04), in which case features is cleared.
func (s *State) computeFrameFeatures(X, P []fftCpx, Ex, Ep, Exp, features, in []float32) bool {
	s.frameAnalysis(X, Ex, in)

	// pitch_buf history: shift left by a frame, append the new frame.
	copy(s.pitchBuf[:PitchBufSize-FrameSize], s.pitchBuf[FrameSize:])
	copy(s.pitchBuf[PitchBufSize-FrameSize:], in[:FrameSize])

	pbuf := make([]float32, PitchBufSize>>1)
	pitchDownsample(s.pitchBuf[:], pbuf, PitchBufSize)
	pitchIndex := pitchSearch(pbuf[PitchMaxPeriod>>1:], pbuf, PitchFrameSize, PitchMaxPeriod-3*PitchMinPeriod)
	pitchIndex = PitchMaxPeriod - pitchIndex

	gain, pitchIndex2 := removeDoubling(pbuf, PitchMaxPeriod, PitchMinPeriod, PitchFrameSize, pitchIndex, s.lastPeriod, s.lastGain)
	pitchIndex = pitchIndex2
	s.lastPeriod = pitchIndex
	s.lastGain = gain

	var p [WindowSize]float32
	for i := 0; i < WindowSize; i++ {
		p[i] = s.pitchBuf[PitchBufSize-WindowSize-pitchIndex+i]
	}
	applyWindow(p[:])
	forwardTransform(P, p[:])
	computeBandEnergy(Ep, P)
	computeBandCorr(Exp, X, P)
	for i := 0; i < NBBands; i++ {
		prod := mul32(Ex[i], Ep[i])
		Exp[i] = float32(float64(Exp[i]) / math.Sqrt(0.001+float64(prod)))
	}
	dct(features[NBBands:], Exp)
	features[2*NBBands] = float32(0.01 * float64(pitchIndex-300))

	var Ly [NBBands]float32
	logMax := float32(-2)
	follow := float32(-2)
	var E float32
	for i := 0; i < NBBands; i++ {
		ly := float32(math.Log10(0.01 + float64(Ex[i])))
		// MAX16(logMax-7, MAX16(follow-1.5, ly)) with the mixed-precision
		// semantics of the C: follow-1.5 (double) vs ly, then logMax-7
		// (float32) vs that double result.
		inner := maxd(float64(follow)-1.5, float64(ly))
		ly = float32(maxd(float64(sub32(logMax, 7)), inner))
		Ly[i] = ly
		logMax = maxf(logMax, ly)
		follow = float32(maxd(float64(follow)-1.5, float64(ly)))
		E = add32(E, Ex[i])
	}
	if float64(E) < 0.04 {
		for i := 0; i < NBFeatures; i++ {
			features[i] = 0
		}
		return true
	}
	dct(features, Ly[:])
	features[0] = sub32(features[0], 12)
	features[1] = sub32(features[1], 4)
	return false
}
