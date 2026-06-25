package timeline

import (
	"errors"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// NewFadeIn returns a Transform whose gain rises linearly from 0 to 1
// over [0, duration], then holds at 1. Attach it to a Cue whose Start
// is the moment the fade should begin. A zero duration returns unity
// gain.
//
// Linear fades are slightly concave perceptually — most of the
// audible change crowds the end. Use NewFadeInLog when an even-sounding
// fade is important (e.g. long music crossfades).
func NewFadeIn(duration time.Duration) Transform {
	return Transform{Gain: mutations.FadeInEnvelope(duration)}
}

// NewFadeOut returns a Transform that holds unity gain until at, then
// ramps linearly from 1 to 0 over [at, at+duration]. Useful for the
// tail of a long cue where only the last few seconds should fade.
func NewFadeOut(at, duration time.Duration) Transform {
	return Transform{Gain: mutations.FadeOutEnvelope(at, duration)}
}

// NewFadeInLog returns a Transform whose gain rises from roughly -60
// dB to 0 dB over [0, duration] and holds at unity afterwards, with
// interpolation in the log domain (GainCurveExponential). Produces a
// perceptually uniform fade-in that is usually preferred over the
// linear variant for anything longer than ~200 ms.
func NewFadeInLog(duration time.Duration) Transform {
	return Transform{
		Gain:      mutations.FadeInEnvelopeExp(duration),
		GainCurve: mutations.GainCurveExponential,
	}
}

// NewFadeOutLog returns a Transform that holds unity until at, then
// fades from 0 dB to roughly -60 dB over [at, at+duration] with
// log-domain interpolation.
func NewFadeOutLog(at, duration time.Duration) Transform {
	return Transform{
		Gain:      mutations.FadeOutEnvelopeExp(at, duration),
		GainCurve: mutations.GainCurveExponential,
	}
}

// ErrCrossfadeIndefinite is returned by Crossfade when fromCue's Source
// has an indefinite Duration — the crossfade boundary cannot be
// computed without knowing when the outgoing source ends.
var ErrCrossfadeIndefinite = errors.New("timeline: crossfade requires source with known duration")

// Crossfade schedules fromCue and toCue on tl such that fromCue fades
// out while toCue fades in over the last `fade` duration of fromCue's
// playback window. Any Transform set on the caller's cues is
// overwritten.
//
// The caller sets fromCue.Source, fromCue.Start, and toCue.Source;
// Crossfade computes toCue.Start = fromCue.Start + fromDur - fade and
// installs the appropriate fade envelopes. fromCue.Source.Duration()
// must be a finite value.
//
// Returns the two handles in the same order as the inputs.
func Crossfade(tl *Timeline, fromCue, toCue Cue, fade time.Duration) (Handle, Handle, error) {
	if fromCue.Source == nil || toCue.Source == nil {
		return nil, nil, ErrNilSource
	}
	fromDur := fromCue.Source.Duration()
	if fromDur < 0 {
		return nil, nil, ErrCrossfadeIndefinite
	}
	if fade <= 0 || fade > fromDur {
		fade = fromDur
	}

	fromCue.Transform = NewFadeOut(fromDur-fade, fade)
	toCue.Start = fromCue.Start + fromDur - fade
	toCue.Transform = NewFadeIn(fade)

	fromH, err := tl.Schedule(fromCue)
	if err != nil {
		return nil, nil, err
	}
	toH, err := tl.Schedule(toCue)
	if err != nil {
		fromH.Cancel()
		return nil, nil, err
	}
	return fromH, toH, nil
}
