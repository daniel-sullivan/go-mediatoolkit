package mutations

import "time"

// Echo is a single-tap delay-with-feedback effect. Each delayed copy
// is attenuated by Feedback and re-injected into the line, producing
// a geometrically decaying train of echoes. Wet controls the mix
// between the original (dry) signal and the effect output.
//
// Echo operates on interleaved samples; the internal ring length is
// delayFrames * channels, so per-channel delays stay coherent
// automatically.
type Echo struct {
	buf      []float64
	writeIdx int
	channels int
	feedback float64
	wet      float64
}

// NewEcho constructs an Echo with the given delay duration at
// sampleRate. feedback is clamped to [0, 0.99] (1.0 would be a
// self-sustaining loop); wet is clamped to [0, 1]. A non-positive
// duration returns an Echo that passes samples through unchanged.
func NewEcho(delay time.Duration, sampleRate, channels int, feedback, wet float64) *Echo {
	if sampleRate <= 0 || channels <= 0 || delay <= 0 {
		return &Echo{channels: channels}
	}
	delayFrames := DurationToFrames(delay, sampleRate)
	if delayFrames <= 0 {
		return &Echo{channels: channels}
	}
	if feedback < 0 {
		feedback = 0
	} else if feedback > 0.99 {
		feedback = 0.99
	}
	if wet < 0 {
		wet = 0
	} else if wet > 1 {
		wet = 1
	}
	return &Echo{
		buf:      make([]float64, int(delayFrames)*channels),
		channels: channels,
		feedback: feedback,
		wet:      wet,
	}
}

// Process runs the echo effect over samples in place. A buffer whose
// length is not a whole number of frames is processed sample-by-sample
// regardless; partial frames do not corrupt the delay line because
// the ring size is a multiple of channels.
func (e *Echo) Process(samples []float64) {
	if len(e.buf) == 0 || len(samples) == 0 {
		return
	}
	dry := 1 - e.wet
	for i, x := range samples {
		delayed := e.buf[e.writeIdx]
		y := x + e.feedback*delayed
		e.buf[e.writeIdx] = y
		e.writeIdx++
		if e.writeIdx >= len(e.buf) {
			e.writeIdx = 0
		}
		samples[i] = dry*x + e.wet*y
	}
}

// Reset clears the delay buffer.
func (e *Echo) Reset() {
	for i := range e.buf {
		e.buf[i] = 0
	}
	e.writeIdx = 0
}
