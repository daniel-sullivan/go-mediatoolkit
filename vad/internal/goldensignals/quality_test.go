package goldensignals

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/silero"
)

// TestSignalsAreDeterministic pins the generator: two calls must be
// bit-identical, or the committed goldens mean nothing.
func TestSignalsAreDeterministic(t *testing.T) {
	a, b := Signals(), Signals()
	require.Len(t, b, len(a))
	for i := range a {
		assert.Equal(t, a[i].Name, b[i].Name)
		assert.Equal(t, a[i].Samples, b[i].Samples, a[i].Name)
	}
}

func TestSignalGeometry(t *testing.T) {
	for _, s := range Signals() {
		assert.Equal(t, signalSeconds*SampleRate, len(s.Samples), s.Name)
		assert.Zero(t, len(s.Samples)%WindowSize, s.Name)
		for i, v := range s.Samples {
			if v > 1 || v < -1 {
				t.Fatalf("%s sample %d outside nominal [-1, 1]: %v", s.Name, i, v)
			}
		}
	}
}

// TestSpeechPulsesDriveTheModel asserts the speech-like signal is fit
// for purpose: the Silero model must score sustained runs well above
// the default threshold inside utterances and near zero in the
// pauses, so behavioural tests can exercise both decision regimes at
// realistic thresholds. If a generator change breaks this, the
// committed goldens and the vad behavioural tests need regenerating
// and retuning together.
func TestSpeechPulsesDriveTheModel(t *testing.T) {
	model, err := silero.New()
	require.NoError(t, err)

	var probs []float64
	for _, s := range Signals() {
		if s.Name != "speech-pulses" {
			continue
		}
		for off := 0; off+silero.WindowSize <= len(s.Samples); off += silero.WindowSize {
			p, err := model.Process(s.Samples[off : off+silero.WindowSize])
			require.NoError(t, err)
			probs = append(probs, float64(p))
		}
	}
	require.NotEmpty(t, probs)

	longest := 0
	cur := 0
	low := 0
	for _, p := range probs {
		if p >= 0.5 {
			cur++
			if cur > longest {
				longest = cur
			}
		} else {
			cur = 0
		}
		if p < 0.1 {
			low++
		}
	}
	t.Logf("speech-pulses: %d windows, longest ≥0.5 run %d (%d ms), %d windows <0.1",
		len(probs), longest, longest*32, low)
	assert.GreaterOrEqual(t, longest, 10,
		"utterances must sustain p ≥ 0.5 for ≥ 10 windows (MinSpeech coverage)")
	assert.GreaterOrEqual(t, low, len(probs)/5,
		"pauses must score < 0.1 (SpeechEnd coverage)")
}
