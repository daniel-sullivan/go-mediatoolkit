package vad

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func TestNewDuckerValidation(t *testing.T) {
	det := newStubDetector(48000, 1)
	valid := DuckerConfig{SampleRate: 48000, Channels: 2, Detector: det}

	tests := []struct {
		name    string
		mutate  func(*DuckerConfig)
		wantErr error
	}{
		{"valid", func(c *DuckerConfig) {}, nil},
		{"detector format independent", func(c *DuckerConfig) { c.Detector = newStubDetector(16000, 1) }, nil},
		{"nil detector", func(c *DuckerConfig) { c.Detector = nil }, ErrNilDetector},
		{"zero rate", func(c *DuckerConfig) { c.SampleRate = 0 }, ErrBadSampleRate},
		{"zero channels", func(c *DuckerConfig) { c.Channels = 0 }, ErrBadChannels},
		{"too many channels", func(c *DuckerConfig) { c.Channels = 65 }, ErrBadChannels},
		{"negative attack", func(c *DuckerConfig) { c.Attack = -time.Millisecond }, ErrBadConfig},
		{"negative release", func(c *DuckerConfig) { c.Release = -time.Millisecond }, ErrBadConfig},
		{"positive depth", func(c *DuckerConfig) { c.DepthDB = 3 }, ErrBadConfig},
		{"NaN depth", func(c *DuckerConfig) { c.DepthDB = math.NaN() }, ErrBadConfig},
		{"-Inf depth ok", func(c *DuckerConfig) { c.DepthDB = math.Inf(-1) }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			d, err := NewDucker(cfg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, d)
		})
	}
}

// TestDuckerDepthExactness: at steady state the ducked gain equals
// DepthDB exactly (linear ramp clamps to target), and recovery lands
// back at exactly unity.
func TestDuckerDepthExactness(t *testing.T) {
	const rate = 48000
	det := newStubDetector(rate, 1)
	d, err := NewDucker(DuckerConfig{SampleRate: rate, Channels: 1, Detector: det})
	require.NoError(t, err)

	// Unity before any speech.
	buf := ones(480)
	d.Process(buf)
	assert.Equal(t, 1.0, buf[len(buf)-1])
	assert.Equal(t, 0.0, d.GainDB())

	// Fully ducked: exactly the −12 dB default depth.
	det.active.Store(true)
	buf = ones(rate / 2)
	d.Process(buf)
	want := mutations.Decibels(-12)
	assert.Equal(t, want, buf[len(buf)-1])
	assert.InDelta(t, -12.0, d.GainDB(), 1e-12)

	// Recovered: exactly unity again.
	det.active.Store(false)
	buf = ones(rate)
	d.Process(buf)
	assert.Equal(t, 1.0, buf[len(buf)-1])
	assert.Equal(t, 0.0, d.GainDB())
}

// TestDuckerStalenessInterleave: the documented cross-chain wiring —
// the voice chain feeds the detector, the bed chain reads it, chunk by
// interleaved chunk (exactly what a mixer does track by track). The
// bed must duck within DecisionLatency + one chunk of the voice onset
// and recover to exact unity after end-of-speech + hangover + release.
func TestDuckerStalenessInterleave(t *testing.T) {
	const (
		rate  = 16000
		chunk = 160 // 10 ms
	)
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	d, err := NewDucker(DuckerConfig{
		SampleRate: rate, Channels: 1, Detector: det,
		Attack:  10 * time.Millisecond,
		Release: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	voiceOn := rate / 2
	voiceOff := voiceOn + rate
	var voice []float64
	voice = appendSilence(voice, voiceOn, 1)
	voice = appendTone(voice, 1000, 0.5, rate, rate, 1)
	voice = appendSilence(voice, rate, 1) // 1 s tail: hangover + release run out

	bed := ones(len(voice))
	for off := 0; off < len(voice); off += chunk {
		end := off + chunk
		if end > len(voice) {
			end = len(voice)
		}
		det.Process(voice[off:end]) // voice chain feeds...
		d.Process(bed[off:end])     // ...bed chain reads, one chunk later at worst
	}

	// First bed sample that moved off unity.
	firstDucked := -1
	for i := 0; i < len(bed); i++ {
		if bed[i] < 0.9999 {
			firstDucked = i
			break
		}
	}
	require.GreaterOrEqual(t, firstDucked, voiceOn, "the bed must not duck before the voice starts")
	latencyFrames := int(mutations.DurationToFrames(det.DecisionLatency(), rate))
	assert.LessOrEqual(t, firstDucked, voiceOn+latencyFrames+2*chunk,
		"ducking must engage within DecisionLatency plus chunk staleness")

	// Fully ducked mid-speech.
	mid := voiceOn + rate/2
	assert.Equal(t, mutations.Decibels(-12), bed[mid])

	// Fully recovered by the end: hangover (200 ms) + one decision
	// frame + release (100 ms) fits well inside the 1 s tail.
	assert.Equal(t, 1.0, bed[len(bed)-1])
	assert.Equal(t, 0.0, d.GainDB())

	// The last ducked sample must not be (much) after end-of-speech +
	// hangover + release + chunk staleness.
	lastDucked := -1
	for i := len(bed) - 1; i >= 0; i-- {
		if bed[i] < 0.9999 {
			lastDucked = i
			break
		}
	}
	require.GreaterOrEqual(t, lastDucked, mid)
	hangover := int(mutations.DurationToFrames(200*time.Millisecond, rate))
	release := int(mutations.DurationToFrames(100*time.Millisecond, rate))
	assert.LessOrEqual(t, lastDucked, voiceOff+hangover+release+latencyFrames+2*chunk)
}

// TestDuckerResetLeavesDetectorAlone: the ducker doesn't own its
// sidechain — Reset must not reset the detector.
func TestDuckerResetLeavesDetectorAlone(t *testing.T) {
	det := newStubDetector(48000, 1)
	det.active.Store(true)
	d, err := NewDucker(DuckerConfig{SampleRate: 48000, Channels: 1, Detector: det})
	require.NoError(t, err)

	buf := ones(48000 / 2)
	d.Process(buf)
	assert.Less(t, buf[len(buf)-1], 1.0)

	d.Reset()
	assert.Equal(t, 0.0, d.GainDB(), "Reset returns the ducker to unity")
	assert.Equal(t, int64(0), det.resets.Load(), "Reset must NOT touch the sidechain detector")
	assert.True(t, det.Active())
}

// TestDuckerSetters: live setter validation and effect.
func TestDuckerSetters(t *testing.T) {
	const rate = 48000
	det := newStubDetector(rate, 1)
	d, err := NewDucker(DuckerConfig{SampleRate: rate, Channels: 1, Detector: det})
	require.NoError(t, err)

	require.ErrorIs(t, d.SetDepthDB(1), ErrBadConfig)
	require.ErrorIs(t, d.SetDepthDB(math.NaN()), ErrBadConfig)
	require.NoError(t, d.SetDepthDB(math.Inf(-1)))
	require.ErrorIs(t, d.SetAttack(-time.Second), ErrBadConfig)
	require.ErrorIs(t, d.SetRelease(-time.Second), ErrBadConfig)
	require.NoError(t, d.SetAttack(time.Nanosecond))
	require.NoError(t, d.SetRelease(time.Nanosecond))

	// −Inf depth with an instant attack mutes the bed completely.
	det.active.Store(true)
	buf := ones(480)
	d.Process(buf)
	assert.Equal(t, 0.0, buf[len(buf)-1])

	// SetDepthDB(0) re-selects the −12 dB default.
	require.NoError(t, d.SetDepthDB(0))
	buf = ones(480)
	d.Process(buf)
	assert.Equal(t, mutations.Decibels(-12), buf[len(buf)-1])
}
