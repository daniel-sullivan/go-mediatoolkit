package gtcrn

// Streamer runs the GTCRN model as a causal streaming STFT overlap-add
// denoiser on a 16 kHz mono signal. Process accepts arbitrary-length
// chunks and returns the same number of denoised samples, delayed by
// Latency samples: output[i] is the denoised input[i-Latency], with the
// initial Latency samples zero while the pipeline fills. Feed trailing
// silence at end-of-stream to flush the tail. Not safe for concurrent use.
//
// Framing is causal (no center padding): analysis frame f covers input
// [f*Hop, f*Hop+NFFT). This is the standard real-time convention; it
// differs from the offline center=True reference only by a half-window
// startup transient, and drives the identical per-frame Forward whose
// spectra are parity-green against onnxruntime.
type Streamer struct {
	m *Model

	hist      []float32 // input from the next frame start onward
	histBase  int       // absolute index of hist[0] (== next frame start)
	ola       []float32 // overlap-add accumulator, ola[0] at olaBase
	wsum      []float32 // window^2 overlap, aligned with ola
	olaBase   int
	finalized int // absolute count of finalized output samples

	denoised  []float32 // finalized samples awaiting emission
	zerosLeft int       // remaining startup-latency zeros to emit
}

// streamLatency is the streaming algorithmic latency in samples: one
// analysis window, which guarantees a finalized output sample is always
// ready by the time it must be emitted (no underrun after priming).
const streamLatency = NFFT

// NewStreamer wraps a model in a streaming denoiser.
func NewStreamer(m *Model) *Streamer {
	return &Streamer{m: m, zerosLeft: streamLatency}
}

// Reset clears the streaming state and the model caches.
func (s *Streamer) Reset() {
	s.m.Reset()
	s.hist = s.hist[:0]
	s.histBase = 0
	s.ola = s.ola[:0]
	s.wsum = s.wsum[:0]
	s.olaBase = 0
	s.finalized = 0
	s.denoised = s.denoised[:0]
	s.zerosLeft = streamLatency
}

// LatencySamples is the streaming delay in samples.
func (s *Streamer) LatencySamples() int { return streamLatency }

// Process consumes in and returns len(in) denoised samples (delayed by
// streamLatency). The returned slice is freshly allocated.
func (s *Streamer) Process(in []float32) []float32 {
	s.hist = append(s.hist, in...)

	var fr, fi [NFFT]float64
	specRe := make([]float32, Bins)
	specIm := make([]float32, Bins)
	for len(s.hist) >= NFFT {
		// Analysis: window + FFT of the current frame.
		for n := 0; n < NFFT; n++ {
			fr[n] = float64(s.hist[n] * sqrtHann[n])
			fi[n] = 0
		}
		fft(fr[:], fi[:], false)
		for k := 0; k < Bins; k++ {
			specRe[k] = float32(fr[k])
			specIm[k] = float32(fi[k])
		}

		enh := s.m.Forward(specRe, specIm)

		// Synthesis: Hermitian-complete, inverse FFT, window.
		for k := 0; k < Bins; k++ {
			fr[k] = float64(enh[k*2])
			fi[k] = float64(enh[k*2+1])
		}
		for k := Bins; k < NFFT; k++ {
			fr[k] = float64(enh[(NFFT-k)*2])
			fi[k] = -float64(enh[(NFFT-k)*2+1])
		}
		fft(fr[:], fi[:], true)

		// Overlap-add at the absolute frame start.
		need := (s.histBase - s.olaBase) + NFFT
		for len(s.ola) < need {
			s.ola = append(s.ola, 0)
			s.wsum = append(s.wsum, 0)
		}
		off := s.histBase - s.olaBase
		for n := 0; n < NFFT; n++ {
			w := sqrtHann[n]
			s.ola[off+n] += float32(fr[n]) * w
			s.wsum[off+n] += w * w
		}

		s.histBase += Hop
		s.hist = s.hist[Hop:]

		// Finalize samples now that no future frame overlaps them.
		for a := s.finalized; a < s.histBase; a++ {
			i := a - s.olaBase
			v := s.ola[i]
			if s.wsum[i] > 1e-8 {
				v /= s.wsum[i]
			}
			s.denoised = append(s.denoised, v)
		}
		s.finalized = s.histBase
		if drop := s.histBase - s.olaBase; drop > 0 {
			s.ola = s.ola[drop:]
			s.wsum = s.wsum[drop:]
			s.olaBase = s.histBase
		}
	}

	// Emit len(in) samples from the delay line.
	out := make([]float32, len(in))
	dPos := 0
	for i := range out {
		switch {
		case s.zerosLeft > 0:
			s.zerosLeft--
		case dPos < len(s.denoised):
			out[i] = s.denoised[dPos]
			dPos++
		}
	}
	if dPos > 0 {
		s.denoised = s.denoised[dPos:]
	}
	return out
}
