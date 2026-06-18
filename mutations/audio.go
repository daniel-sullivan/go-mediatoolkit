package mutations

import "time"

// Audio is a PCM buffer paired with its format metadata. It is the
// toolkit's preferred type for handing audio between packages when
// the buffer has a lifetime beyond a single streaming Pull —
// generators, clip loaders, offline renderers, and user code. Hot
// paths (timeline.Source.Pull, Processor.Process) still work on bare
// []float64 for allocation-free streaming.
//
// Data holds interleaved float64 samples normalised to [-1, 1].
// SampleRate is in Hz; Channels is the interleave count (1 = mono,
// 2 = stereo, ...). Audio is a small value struct: copying it copies
// the header but the underlying Data slice is shared — use Clone to
// duplicate the samples.
//
// Methods that modify samples without changing buffer length operate
// in place and return the receiver, allowing chained writes:
//
//	audio.ApplyGain(0.5).ApplyFadeIn(50 * time.Millisecond)
//
// Methods that change length (e.g. CrossfadeLoop, RenderWithEffects)
// allocate a new buffer and return a fresh Audio; the receiver is
// untouched.
type Audio struct {
	Data       []float64
	SampleRate int
	Channels   int
}

// Duration reports the playback time of the buffer.
func (a Audio) Duration() time.Duration {
	if a.SampleRate <= 0 || a.Channels <= 0 {
		return 0
	}
	return FramesToDuration(int64(a.Frames()), a.SampleRate)
}

// Frames reports the number of sample frames in the buffer.
func (a Audio) Frames() int {
	if a.Channels <= 0 {
		return 0
	}
	return len(a.Data) / a.Channels
}

// Clone returns an independent Audio with a fresh copy of Data.
func (a Audio) Clone() Audio {
	b := Audio{
		Data:       make([]float64, len(a.Data)),
		SampleRate: a.SampleRate,
		Channels:   a.Channels,
	}
	copy(b.Data, a.Data)
	return b
}

// ApplyGain multiplies every sample in place by gain and returns the
// receiver for chaining. Delegates to ApplyGain.
func (a Audio) ApplyGain(gain float64) Audio {
	ApplyGain(a.Data, gain)
	return a
}

// ApplyGainEnvelope runs a time-varying linear gain envelope over the
// buffer in place. elapsed is the envelope's time origin — use 0
// when the envelope should start at the buffer's first sample.
func (a Audio) ApplyGainEnvelope(env []GainPoint, elapsed time.Duration) Audio {
	ApplyGainEnvelope(a.Data, env, elapsed, a.Channels, a.SampleRate)
	return a
}

// ApplyGainEnvelopeCurve is ApplyGainEnvelope with an explicit
// interpolation curve (linear or exponential).
func (a Audio) ApplyGainEnvelopeCurve(env []GainPoint, elapsed time.Duration, curve GainCurve) Audio {
	ApplyGainEnvelopeCurve(a.Data, env, elapsed, a.Channels, a.SampleRate, curve)
	return a
}

// ApplyCustomGain multiplies every sample by a user-provided gain
// function evaluated per frame.
func (a Audio) ApplyCustomGain(fn func(elapsed time.Duration) float64, elapsed time.Duration) Audio {
	ApplyCustomGain(a.Data, fn, elapsed, a.Channels, a.SampleRate)
	return a
}

// ApplyFadeIn attaches a linear fade from silence to unity over
// [0, duration] at the start of the buffer.
func (a Audio) ApplyFadeIn(duration time.Duration) Audio {
	return a.ApplyGainEnvelope(FadeInEnvelope(duration), 0)
}

// ApplyFadeOut holds unity until at, then fades to silence over
// [at, at+duration].
func (a Audio) ApplyFadeOut(at, duration time.Duration) Audio {
	return a.ApplyGainEnvelope(FadeOutEnvelope(at, duration), 0)
}

// ApplySaturator runs every sample through the given saturator.
func (a Audio) ApplySaturator(s Saturator) Audio {
	ApplySaturator(a.Data, s)
	return a
}

// ApplyEffect runs a single Processor over the buffer in place.
func (a Audio) ApplyEffect(p Processor) Audio {
	if p != nil {
		p.Process(a.Data)
	}
	return a
}

// ApplyEffects runs a chain of Processors over the buffer in place,
// in declaration order.
func (a Audio) ApplyEffects(chain ...Processor) Audio {
	for _, p := range chain {
		if p != nil {
			p.Process(a.Data)
		}
	}
	return a
}

// CrossfadeLoop returns a seamlessly-loopable shorter copy of the
// buffer by equal-power crossfading its tail into its head. The
// receiver is untouched. See the free function for full details.
func (a Audio) CrossfadeLoop(fade time.Duration) Audio {
	fadeFrames := int(DurationToFrames(fade, a.SampleRate))
	return Audio{
		Data:       CrossfadeLoop(a.Data, fadeFrames, a.Channels),
		SampleRate: a.SampleRate,
		Channels:   a.Channels,
	}
}

// RenderWithEffects returns a new Audio containing this buffer run
// through the given processor chain, extended by tail duration of
// silence so tail-carrying effects decay fully. The receiver is
// untouched.
func (a Audio) RenderWithEffects(chain []Processor, tail time.Duration) Audio {
	tailFrames := int(DurationToFrames(tail, a.SampleRate))
	return Audio{
		Data:       RenderBuffer(a.Data, chain, tailFrames, a.Channels),
		SampleRate: a.SampleRate,
		Channels:   a.Channels,
	}
}
