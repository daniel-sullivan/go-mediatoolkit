package mutations

// Reverb is a Schroeder-style reverberator: four parallel comb
// filters with damped feedback, summed and fed through two series
// allpass filters. Delay lengths are taken from Freeverb's prime
// tunings and scaled to the configured sample rate; feedback and
// damping are controlled by the RoomSize and Damping parameters.
//
// This is a small, classical reverb rather than a modern algorithmic
// one — suitable for demonstrations, chorus-ish thickening, and the
// karaoke-style "add some space" use case. For production mastering
// reverbs, consider integrating a convolution engine instead.
type Reverb struct {
	channels int
	wet      float64
	combs    []combState    // len == 4 * channels
	allpass  []allpassState // len == 2 * channels
}

type combState struct {
	buf      []float64
	idx      int
	feedback float64
	damp     float64
	lpState  float64
}

type allpassState struct {
	buf []float64
	idx int
	g   float64
}

// Freeverb-derived comb/allpass delays in milliseconds. These are
// prime-ish numbers chosen to break up resonant modes; at 44.1 kHz
// they match the classic tunings exactly.
var reverbCombDelaysMs = []float64{35.31, 36.67, 33.81, 32.25}
var reverbAllpassDelaysMs = []float64{5.10, 12.61}

// NewReverb constructs a Reverb for the given sample rate and
// channel count.
//
// roomSize (0-1) controls comb feedback, which maps to perceived
// decay length: 0 is a small ambience, 1 is a cathedral. Clamped to
// [0, 1] and then mapped to feedback in [0.7, 0.98].
//
// damping (0-1) attenuates high frequencies fed back through the
// combs — higher values dull the tail. Clamped to [0, 0.99].
//
// wet (0-1) mixes the reverberated signal against the dry input;
// 0 bypasses, 1 returns pure wet. Clamped to [0, 1].
func NewReverb(sampleRate, channels int, roomSize, damping, wet float64) *Reverb {
	if sampleRate <= 0 || channels <= 0 {
		return &Reverb{channels: channels}
	}
	roomSize = clampUnit(roomSize)
	damping = clampUnit(damping)
	if damping > 0.99 {
		damping = 0.99
	}
	wet = clampUnit(wet)
	feedback := 0.7 + 0.28*roomSize

	r := &Reverb{
		channels: channels,
		wet:      wet,
		combs:    make([]combState, 4*channels),
		allpass:  make([]allpassState, 2*channels),
	}
	for ch := 0; ch < channels; ch++ {
		for i, ms := range reverbCombDelaysMs {
			n := int(ms * float64(sampleRate) / 1000)
			if n < 1 {
				n = 1
			}
			r.combs[ch*4+i] = combState{
				buf:      make([]float64, n),
				feedback: feedback,
				damp:     damping,
			}
		}
		for i, ms := range reverbAllpassDelaysMs {
			n := int(ms * float64(sampleRate) / 1000)
			if n < 1 {
				n = 1
			}
			r.allpass[ch*2+i] = allpassState{
				buf: make([]float64, n),
				g:   0.5,
			}
		}
	}
	return r
}

// Process runs the reverb over samples in place.
func (r *Reverb) Process(samples []float64) {
	if len(r.combs) == 0 || len(samples) == 0 {
		return
	}
	dry := 1 - r.wet
	frames := len(samples) / r.channels
	for f := 0; f < frames; f++ {
		for ch := 0; ch < r.channels; ch++ {
			i := f*r.channels + ch
			x := samples[i]

			// Parallel combs.
			var combSum float64
			for k := 0; k < 4; k++ {
				c := &r.combs[ch*4+k]
				y := c.buf[c.idx]
				c.lpState = y*(1-c.damp) + c.lpState*c.damp
				c.buf[c.idx] = x + c.feedback*c.lpState
				c.idx++
				if c.idx >= len(c.buf) {
					c.idx = 0
				}
				combSum += y
			}
			// Normalise across comb count so increasing comb density
			// alone does not inflate output level.
			y := combSum * 0.25

			// Series Schroeder allpasses.
			for k := 0; k < 2; k++ {
				a := &r.allpass[ch*2+k]
				bufOut := a.buf[a.idx]
				newY := -a.g*y + bufOut
				a.buf[a.idx] = y + a.g*bufOut
				a.idx++
				if a.idx >= len(a.buf) {
					a.idx = 0
				}
				y = newY
			}

			samples[i] = dry*x + r.wet*y
		}
	}
}

// Reset clears every filter state so the reverb tail restarts from
// silence on the next Process call.
func (r *Reverb) Reset() {
	for i := range r.combs {
		for j := range r.combs[i].buf {
			r.combs[i].buf[j] = 0
		}
		r.combs[i].idx = 0
		r.combs[i].lpState = 0
	}
	for i := range r.allpass {
		for j := range r.allpass[i].buf {
			r.allpass[i].buf[j] = 0
		}
		r.allpass[i].idx = 0
	}
}

func clampUnit(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
