package mutations

import "math"

// Biquad is a second-order IIR filter implementing the RBJ cookbook
// lowpass, highpass, and bandpass responses. Coefficients are fixed
// at construction time (from cutoff, Q, and sample rate); per-channel
// filter state is kept internally so stereo streams filter coherently
// without cross-talk.
//
// Biquad implements Processor and can be chained in a
// timeline.EffectSource like any other effect. For higher-order
// filters, cascade multiple biquads in the same chain (two
// Butterworth-Q biquads in series ≈ 4th-order Butterworth).
type Biquad struct {
	// Feed-forward coefficients (normalised by a0).
	b0, b1, b2 float64
	// Feed-back coefficients (normalised, a0 omitted since = 1).
	a1, a2 float64

	channels int
	// Per-channel delay line: x1/x2 hold the previous two inputs,
	// y1/y2 the previous two outputs.
	x1, x2 []float64
	y1, y2 []float64
}

// NewLowpass returns a biquad lowpass filter with the given
// -3 dB cutoff frequency (Hz) and Q factor. Q = 0.707 is the
// Butterworth response (maximally flat passband). Cutoff must be in
// (0, sampleRate/2); values outside the open interval are clamped.
func NewLowpass(cutoff, q float64, sampleRate, channels int) *Biquad {
	return newBiquad(biquadLowpass, cutoff, q, sampleRate, channels)
}

// NewHighpass returns a biquad highpass filter. Semantics mirror
// NewLowpass; use Q = 0.707 for a flat Butterworth response.
func NewHighpass(cutoff, q float64, sampleRate, channels int) *Biquad {
	return newBiquad(biquadHighpass, cutoff, q, sampleRate, channels)
}

// NewBandpass returns a biquad bandpass filter with 0 dB peak gain at
// the centre frequency. Q controls the bandwidth: larger Q = narrower
// band. For a given Q, the -3 dB bandwidth is roughly cutoff / Q.
func NewBandpass(cutoff, q float64, sampleRate, channels int) *Biquad {
	return newBiquad(biquadBandpass, cutoff, q, sampleRate, channels)
}

type biquadKind int

const (
	biquadLowpass biquadKind = iota
	biquadHighpass
	biquadBandpass
)

func newBiquad(kind biquadKind, cutoff, q float64, sampleRate, channels int) *Biquad {
	if channels < 1 {
		channels = 1
	}
	if sampleRate <= 0 {
		return &Biquad{channels: channels, b0: 1} // pass-through
	}
	// Clamp cutoff to the valid open interval (0, nyquist).
	nyquist := float64(sampleRate) / 2
	if cutoff <= 0 {
		cutoff = 1
	}
	if cutoff >= nyquist {
		cutoff = nyquist * 0.999
	}
	if q <= 0 {
		q = 0.707
	}

	w0 := 2 * math.Pi * cutoff / float64(sampleRate)
	cosW0 := math.Cos(w0)
	sinW0 := math.Sin(w0)
	alpha := sinW0 / (2 * q)

	var b0, b1, b2, a0, a1, a2 float64
	switch kind {
	case biquadLowpass:
		b0 = (1 - cosW0) / 2
		b1 = 1 - cosW0
		b2 = (1 - cosW0) / 2
		a0 = 1 + alpha
		a1 = -2 * cosW0
		a2 = 1 - alpha
	case biquadHighpass:
		b0 = (1 + cosW0) / 2
		b1 = -(1 + cosW0)
		b2 = (1 + cosW0) / 2
		a0 = 1 + alpha
		a1 = -2 * cosW0
		a2 = 1 - alpha
	case biquadBandpass:
		// Constant 0 dB peak gain ("skirt gain" form).
		b0 = alpha
		b1 = 0
		b2 = -alpha
		a0 = 1 + alpha
		a1 = -2 * cosW0
		a2 = 1 - alpha
	}

	return &Biquad{
		b0:       b0 / a0,
		b1:       b1 / a0,
		b2:       b2 / a0,
		a1:       a1 / a0,
		a2:       a2 / a0,
		channels: channels,
		x1:       make([]float64, channels),
		x2:       make([]float64, channels),
		y1:       make([]float64, channels),
		y2:       make([]float64, channels),
	}
}

// Process runs the biquad over samples in place using Direct Form I.
// Per-channel state is maintained across calls, so a long input
// processed in one call or many chunks produces identical output.
func (b *Biquad) Process(samples []float64) {
	if b.channels <= 0 || len(samples) == 0 || len(b.x1) == 0 {
		return
	}
	frames := len(samples) / b.channels
	for f := 0; f < frames; f++ {
		for ch := 0; ch < b.channels; ch++ {
			i := f*b.channels + ch
			x := samples[i]
			y := b.b0*x + b.b1*b.x1[ch] + b.b2*b.x2[ch] - b.a1*b.y1[ch] - b.a2*b.y2[ch]
			b.x2[ch] = b.x1[ch]
			b.x1[ch] = x
			b.y2[ch] = b.y1[ch]
			b.y1[ch] = y
			samples[i] = y
		}
	}
}

// Reset clears per-channel delay lines so the next Process starts
// from a silent state.
func (b *Biquad) Reset() {
	for i := range b.x1 {
		b.x1[i] = 0
		b.x2[i] = 0
		b.y1[i] = 0
		b.y2[i] = 0
	}
}
