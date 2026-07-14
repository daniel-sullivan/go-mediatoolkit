package rnnoise

// State is the pure-Go port of RNNoise's DenoiseState (denoise.c). It
// holds all per-stream analysis/synthesis/pitch/filter/network state for
// the 48 kHz fullband denoiser. One State is bound to one stream and is
// not safe to share across goroutines without external synchronisation.
type State struct {
	analysisMem  [FrameSize]float32
	synthesisMem [FrameSize]float32
	pitchBuf     [PitchBufSize]float32
	lastGain     float32
	lastPeriod   int
	memHpX       [2]float32
	lastg        [NBBands]float32

	rnn rnnState

	delayedX   [FreqSize]fftCpx
	delayedP   [FreqSize]fftCpx
	delayedEx  [NBBands]float32
	delayedEp  [NBBands]float32
	delayedExp [NBBands]float32
}

// NewState returns a zeroed State (matching rnnoise_init's memset).
func NewState() *State { return new(State) }

// Reset clears all state, equivalent to re-running rnnoise_init.
func (s *State) Reset() { *s = State{} }

// ProcessFrame denoises one FrameSize (480-sample) frame of 48 kHz mono
// audio at RNNoise's native ±32768 scale, writing into out and returning
// the frame's voice-activity probability. It is the 1:1 port of
// rnnoise_process_frame (denoise.c): high-pass, feature extraction,
// network gains, pitch filtering of the delayed spectrum, gain
// smoothing, and overlap-add synthesis. in and out must be FrameSize
// long; they may alias.
func (s *State) ProcessFrame(out, in []float32) float32 {
	var x [FrameSize]float32
	biquad(x[:], &s.memHpX, in, hpB, hpA, FrameSize)

	var X, P [FreqSize]fftCpx
	var Ex, Ep, Exp [NBBands]float32
	var features [NBFeatures]float32
	silence := s.computeFrameFeatures(X[:], P[:], Ex[:], Ep[:], Exp[:], features[:], x[:])

	var vadProb float32
	if !silence {
		var g [NBBands]float32
		v := []float32{0}
		computeRnn(theModel(), &s.rnn, g[:], v, features[:])
		vadProb = v[0]
		pitchFilter(s.delayedX[:], s.delayedP[:], s.delayedEx[:], s.delayedEp[:], s.delayedExp[:], g[:])
		for i := 0; i < NBBands; i++ {
			g[i] = maxf(g[i], mul32(0.6, s.lastg[i]))
			s.lastg[i] = g[i]
		}
		gf := make([]float32, FreqSize)
		interpBandGain(gf, g[:])
		for i := 0; i < FreqSize; i++ {
			s.delayedX[i].r = mul32(s.delayedX[i].r, gf[i])
			s.delayedX[i].i = mul32(s.delayedX[i].i, gf[i])
		}
	}
	s.frameSynthesis(out, s.delayedX[:])

	copy(s.delayedX[:], X[:])
	copy(s.delayedP[:], P[:])
	copy(s.delayedEx[:], Ex[:])
	copy(s.delayedEp[:], Ep[:])
	copy(s.delayedExp[:], Exp[:])
	return vadProb
}
