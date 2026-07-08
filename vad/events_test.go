package vad

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// TestEventsAlternationAndOrdering drives a multi-utterance signal and
// pins the full event contract in one pass:
//
//   - strict Start/End alternation beginning with Start,
//   - strictly monotonic (and non-overlapping) frame positions,
//   - Pos consistent with Frame,
//   - state atomics updated BEFORE Publish: inside the callback the
//     detector already reports the delivered event as current state.
func TestEventsAlternationAndOrdering(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)

	var got []SpeechEvent
	det.Events().Subscribe(func(ev SpeechEvent) {
		// Atomics-before-publish: the delivered event must already be
		// the detector's visible state.
		assert.Equal(t, ev.Kind == SpeechStart, det.Active(),
			"Active must be stored before Publish")
		assert.Equal(t, ev.Probability, det.Probability(),
			"Probability must be stored before Publish")
		last, ok := det.LastTransition()
		require.True(t, ok, "LastTransition must be stored before Publish")
		assert.Equal(t, ev, last, "LastTransition must be the event being delivered")
		got = append(got, ev)
	})

	amp := ampForDBFS(-23)
	var sig []float64
	for i := 0; i < 3; i++ {
		sig = appendSilence(sig, rate/2, 1)               // 0.5 s gaps split utterances
		sig = appendTone(sig, 1000, amp, rate/2, rate, 1) // 0.5 s bursts
	}
	sig = appendSilence(sig, rate/2, 1)
	processChunks(det, sig, 160, 1)

	require.Len(t, got, 6, "three utterances → three Start/End pairs")
	for i, ev := range got {
		if i%2 == 0 {
			assert.Equal(t, SpeechStart, ev.Kind, "event %d", i)
			assert.Equal(t, 1.0, ev.Probability)
		} else {
			assert.Equal(t, SpeechEnd, ev.Kind, "event %d", i)
			assert.Equal(t, 0.0, ev.Probability)
		}
		if i > 0 {
			assert.Greater(t, ev.Frame, got[i-1].Frame,
				"positions must be strictly monotonic (event %d)", i)
		}
		assert.GreaterOrEqual(t, ev.Frame, int64(0))
		// Pos must be the exact duration rendering of Frame.
		assert.Equal(t, mutations.FramesToDuration(ev.Frame, rate), ev.Pos, "event %d", i)
	}

	// Each utterance's Start lands exactly on its aligned burst start
	// (bursts begin at 0.5, 1.5, 2.5 s).
	assert.Equal(t, int64(rate/2), got[0].Frame)
	assert.Equal(t, int64(3*rate/2), got[2].Frame)
	assert.Equal(t, int64(5*rate/2), got[4].Frame)
}
