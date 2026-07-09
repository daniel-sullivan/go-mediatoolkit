package vad

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func TestNewEnergyDetectorValidation(t *testing.T) {
	valid := EnergyConfig{SampleRate: 16000, Channels: 1}

	tests := []struct {
		name    string
		mutate  func(*EnergyConfig)
		wantErr error
	}{
		{"minimal valid", func(c *EnergyConfig) {}, nil},
		{"full valid", func(c *EnergyConfig) {
			c.SampleRate = 48000
			c.Channels = 2
			c.HighpassHz = 100
			c.LowpassHz = 8000
			c.OnThresholdDB = 9
			c.OffThresholdDB = 4
			c.AbsoluteFloorDB = -60
			c.FloorFall = 100 * time.Millisecond
			c.FloorRise = 2 * time.Second
			c.Onset = 20 * time.Millisecond
			c.Hangover = 300 * time.Millisecond
		}, nil},
		{"rate at floor", func(c *EnergyConfig) { c.SampleRate = 100 }, nil},
		{"rate too low", func(c *EnergyConfig) { c.SampleRate = 99 }, ErrBadSampleRate},
		{"zero rate", func(c *EnergyConfig) { c.SampleRate = 0 }, ErrBadSampleRate},
		{"zero channels", func(c *EnergyConfig) { c.Channels = 0 }, ErrBadChannels},
		{"too many channels", func(c *EnergyConfig) { c.Channels = 65 }, ErrBadChannels},
		{"negative highpass", func(c *EnergyConfig) { c.HighpassHz = -1 }, ErrBadConfig},
		{"NaN lowpass", func(c *EnergyConfig) { c.LowpassHz = math.NaN() }, ErrBadConfig},
		{"off >= on", func(c *EnergyConfig) { c.OnThresholdDB = 3; c.OffThresholdDB = 5 }, ErrBadThreshold},
		{"off == on", func(c *EnergyConfig) { c.OnThresholdDB = 4; c.OffThresholdDB = 4 }, ErrBadThreshold},
		{"off >= default on", func(c *EnergyConfig) { c.OffThresholdDB = 7 }, ErrBadThreshold},
		{"NaN on threshold", func(c *EnergyConfig) { c.OnThresholdDB = math.NaN() }, ErrBadThreshold},
		{"infinite on threshold", func(c *EnergyConfig) { c.OnThresholdDB = math.Inf(1) }, ErrBadThreshold},
		{"NaN absolute floor", func(c *EnergyConfig) { c.AbsoluteFloorDB = math.NaN() }, ErrBadThreshold},
		{"+Inf absolute floor", func(c *EnergyConfig) { c.AbsoluteFloorDB = math.Inf(1) }, ErrBadThreshold},
		{"-Inf absolute floor ok", func(c *EnergyConfig) { c.AbsoluteFloorDB = math.Inf(-1) }, nil},
		{"negative floor fall", func(c *EnergyConfig) { c.FloorFall = -time.Millisecond }, ErrBadConfig},
		{"negative floor rise", func(c *EnergyConfig) { c.FloorRise = -time.Millisecond }, ErrBadConfig},
		{"negative onset", func(c *EnergyConfig) { c.Onset = -time.Millisecond }, ErrBadConfig},
		{"negative hangover", func(c *EnergyConfig) { c.Hangover = -time.Millisecond }, ErrBadConfig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			det, err := NewEnergyDetector(cfg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, det)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, det)
		})
	}
}

func TestEnergyDetectorDefaultsAndReaders(t *testing.T) {
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: 48000, Channels: 2})
	require.NoError(t, err)

	assert.Equal(t, 48000, det.SampleRate())
	assert.Equal(t, 2, det.Channels())
	assert.False(t, det.Active())
	assert.Equal(t, 0.0, det.Probability())
	_, ok := det.LastTransition()
	assert.False(t, ok)
	require.NotNil(t, det.Events())

	// Default decision latency: one 10 ms decision frame + 30 ms onset.
	assert.Equal(t, 40*time.Millisecond, det.DecisionLatency())

	// DecisionLatency is live: it follows SetOnset.
	require.NoError(t, det.SetOnset(50*time.Millisecond))
	assert.Equal(t, 60*time.Millisecond, det.DecisionLatency())
	require.NoError(t, det.SetOnset(0)) // zero re-selects the 30 ms default
	assert.Equal(t, 40*time.Millisecond, det.DecisionLatency())
}

// TestEnergyBurst pins the flagship case: silence → tone burst →
// silence yields exactly one Start/End pair with back-timestamped
// positions. The burst is frame-aligned, so SpeechStart is EXACT; the
// filters' ring-down after the burst can hold one extra decision frame
// raw-voiced, so SpeechEnd is pinned to within one decision frame.
func TestEnergyBurst(t *testing.T) {
	const (
		rate      = 48000
		frameSize = 480 // 10 ms decision frame
	)
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	evs := collectEvents(det)

	var sig []float64
	sig = appendSilence(sig, rate, 1)                             // 1 s
	sig = appendTone(sig, 1000, ampForDBFS(-23), rate/2, rate, 1) // 0.5 s at −23 dBFS
	sig = appendSilence(sig, rate, 1)                             // 1 s
	processChunks(det, sig, frameSize, 1)

	require.Len(t, *evs, 2)
	start, end := (*evs)[0], (*evs)[1]

	assert.Equal(t, SpeechStart, start.Kind)
	assert.Equal(t, int64(rate), start.Frame, "SpeechStart must be back-timestamped exactly to the aligned burst start")
	assert.Equal(t, mutations.FramesToDuration(rate, rate), start.Pos)
	assert.Equal(t, 1.0, start.Probability)

	assert.Equal(t, SpeechEnd, end.Kind)
	burstEnd := int64(rate + rate/2)
	assert.GreaterOrEqual(t, end.Frame, burstEnd)
	assert.LessOrEqual(t, end.Frame, burstEnd+frameSize, "SpeechEnd back-timestamp within one decision frame of the actual stop")
	assert.Equal(t, 0.0, end.Probability)

	assert.False(t, det.Active())
	assert.Equal(t, 0.0, det.Probability())
	last, ok := det.LastTransition()
	require.True(t, ok)
	assert.Equal(t, end, last)
}

// TestEnergyActiveDuringBurst checks the polled state mid-stream.
func TestEnergyActiveDuringBurst(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)

	var sig []float64
	sig = appendSilence(sig, rate/2, 1)
	sig = appendTone(sig, 1000, ampForDBFS(-23), rate/2, rate, 1)
	processChunks(det, sig, 160, 1)

	assert.True(t, det.Active())
	assert.Equal(t, 1.0, det.Probability())
	last, ok := det.LastTransition()
	require.True(t, ok)
	assert.Equal(t, SpeechStart, last.Kind)
	assert.Equal(t, int64(rate/2), last.Frame)
}

// TestEnergyHumRejection: 50 Hz mains hum at −40 dBFS sits 24 dB under
// the 200 Hz highpass, landing well below the absolute floor — no
// events. The identical level in-band (1 kHz) must fire, proving the
// rejection is the filter's doing.
func TestEnergyHumRejection(t *testing.T) {
	const rate = 16000
	amp := ampForDBFS(-43) // in-band energy −43 dBFS

	for _, tc := range []struct {
		name       string
		freq       float64
		wantEvents bool
	}{
		{"50Hz hum rejected", 50, false},
		{"1kHz control detected", 1000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
			require.NoError(t, err)
			evs := collectEvents(det)

			var sig []float64
			sig = appendSilence(sig, rate/2, 1)
			sig = appendTone(sig, tc.freq, amp, rate, rate, 1)
			processChunks(det, sig, 160, 1)

			if tc.wantEvents {
				require.NotEmpty(t, *evs)
				assert.Equal(t, SpeechStart, (*evs)[0].Kind)
			} else {
				assert.Empty(t, *evs)
				assert.False(t, det.Active())
			}
		})
	}
}

// TestEnergy10kRejection: a 10 kHz tone at −48 dBFS in-band energy is
// pushed 16 dB down by the 4 kHz lowpass, below the −55 dBFS absolute
// floor; the same level at 1 kHz fires.
func TestEnergy10kRejection(t *testing.T) {
	const rate = 48000
	amp := ampForDBFS(-48)

	for _, tc := range []struct {
		name       string
		freq       float64
		wantEvents bool
	}{
		{"10kHz rejected", 10000, false},
		{"1kHz control detected", 1000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
			require.NoError(t, err)
			evs := collectEvents(det)

			var sig []float64
			sig = appendSilence(sig, rate/2, 1)
			sig = appendTone(sig, tc.freq, amp, rate, rate, 1)
			processChunks(det, sig, 480, 1)

			if tc.wantEvents {
				require.NotEmpty(t, *evs)
			} else {
				assert.Empty(t, *evs)
			}
		})
	}
}

// TestEnergyFloorStep: a steady bed tone below the absolute floor must
// (a) fire nothing itself and (b) drag the adaptive floor up so that a
// probe the FRESH floor accepted is rejected against the ADAPTED one.
func TestEnergyFloorStep(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{
		SampleRate: rate,
		Channels:   1,
		FloorRise:  200 * time.Millisecond, // fast adaptation to keep the test short
	})
	require.NoError(t, err)
	evs := collectEvents(det)

	probeAmp := ampForDBFS(-53) // above the −55 abs floor, above fresh floor+6
	bedAmp := ampForDBFS(-57)   // below the −55 abs floor

	var sig []float64
	sig = appendSilence(sig, rate, 1)                      // floor settles at −90
	sig = appendTone(sig, 1000, probeAmp, rate/5, rate, 1) // probe #1: fires
	sig = appendSilence(sig, rate/2, 1)                    // hangover ends the utterance
	probe2Start := len(sig) + rate*5/2
	sig = appendTone(sig, 1000, bedAmp, rate*5/2, rate, 1) // 2.5 s bed: floor adapts to ≈−57
	sig = appendTone(sig, 1000, probeAmp, rate/5, rate, 1) // probe #2: rejected (−53 < adapted floor+6)
	sig = appendSilence(sig, rate/2, 1)
	processChunks(det, sig, 160, 1)

	require.Len(t, *evs, 2, "only probe #1 may fire: bed is under the absolute floor, probe #2 under the adapted relative threshold")
	assert.Equal(t, SpeechStart, (*evs)[0].Kind)
	assert.Equal(t, int64(rate), (*evs)[0].Frame)
	assert.Equal(t, SpeechEnd, (*evs)[1].Kind)
	assert.Less(t, (*evs)[1].Frame, int64(probe2Start), "no event may originate at or after probe #2")
}

// TestEnergyHangoverChatter: a sub-hangover gap between two bursts is
// bridged into one utterance; a super-hangover gap splits them.
func TestEnergyHangoverChatter(t *testing.T) {
	const (
		rate      = 16000
		frameSize = 160
	)
	amp := ampForDBFS(-23)

	build := func(gapFrames int) []float64 {
		var sig []float64
		sig = appendSilence(sig, rate/2, 1)                  // 0.5 s
		sig = appendTone(sig, 1000, amp, rate*3/10, rate, 1) // 0.3 s burst A
		sig = appendSilence(sig, gapFrames, 1)
		sig = appendTone(sig, 1000, amp, rate*3/10, rate, 1) // 0.3 s burst B
		sig = appendSilence(sig, rate, 1)
		return sig
	}

	t.Run("100ms gap bridged", func(t *testing.T) {
		det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
		require.NoError(t, err)
		evs := collectEvents(det)
		processChunks(det, build(rate/10), 160, 1)

		require.Len(t, *evs, 2, "a 100 ms stop closure must be bridged by the 200 ms hangover")
		assert.Equal(t, SpeechStart, (*evs)[0].Kind)
		assert.Equal(t, int64(rate/2), (*evs)[0].Frame)
		assert.Equal(t, SpeechEnd, (*evs)[1].Kind)
		// End of burst B: 0.5 + 0.3 + 0.1 + 0.3 = 1.2 s.
		bEnd := int64(rate * 12 / 10)
		assert.GreaterOrEqual(t, (*evs)[1].Frame, bEnd)
		assert.LessOrEqual(t, (*evs)[1].Frame, bEnd+frameSize)
	})

	t.Run("400ms gap split", func(t *testing.T) {
		det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
		require.NoError(t, err)
		evs := collectEvents(det)
		processChunks(det, build(rate*4/10), 160, 1)

		require.Len(t, *evs, 4, "a 400 ms pause must split two utterances")
		assert.Equal(t, SpeechStart, (*evs)[0].Kind)
		assert.Equal(t, int64(rate/2), (*evs)[0].Frame)
		assert.Equal(t, SpeechEnd, (*evs)[1].Kind)
		aEnd := int64(rate * 8 / 10) // 0.5 + 0.3
		assert.GreaterOrEqual(t, (*evs)[1].Frame, aEnd)
		assert.LessOrEqual(t, (*evs)[1].Frame, aEnd+frameSize)
		assert.Equal(t, SpeechStart, (*evs)[2].Kind)
		assert.Equal(t, int64(rate*12/10), (*evs)[2].Frame) // 0.5+0.3+0.4
		assert.Equal(t, SpeechEnd, (*evs)[3].Kind)
	})
}

// TestEnergyAbsoluteFloorLive: a tone under the default −55 dBFS
// absolute floor fires nothing until SetAbsoluteFloorDB lowers the
// gate mid-stream.
func TestEnergyAbsoluteFloorLive(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	evs := collectEvents(det)

	amp := ampForDBFS(-60)
	var sig []float64
	sig = appendSilence(sig, rate/2, 1)
	sig = appendTone(sig, 1000, amp, rate*2, rate, 1)

	// First 1.5 s: tone at −60 dBFS stays under the absolute gate.
	split := rate * 3 / 2
	processChunks(det, sig[:split], 160, 1)
	assert.Empty(t, *evs)
	assert.False(t, det.Active())

	// Lower the gate live; the tone is immediately voiced.
	require.NoError(t, det.SetAbsoluteFloorDB(-70))
	processChunks(det, sig[split:], 160, 1)
	require.NotEmpty(t, *evs)
	assert.Equal(t, SpeechStart, (*evs)[0].Kind)
	assert.GreaterOrEqual(t, (*evs)[0].Frame, int64(split), "the start must not predate the live setter taking effect")
	assert.True(t, det.Active())
}

// TestEnergyLiveThresholds exercises SetThresholdsDB semantics: pair
// validation, default re-selection, and mid-stream effect.
func TestEnergyLiveThresholds(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)

	require.ErrorIs(t, det.SetThresholdsDB(3, 5), ErrBadThreshold)
	require.ErrorIs(t, det.SetThresholdsDB(4, 4), ErrBadThreshold)
	require.ErrorIs(t, det.SetThresholdsDB(math.NaN(), 3), ErrBadThreshold)
	require.ErrorIs(t, det.SetThresholdsDB(math.Inf(1), 3), ErrBadThreshold)
	require.NoError(t, det.SetThresholdsDB(0, 0)) // re-select defaults
	require.NoError(t, det.SetThresholdsDB(20, 10))

	// With a +20 dB on-threshold, a tone 12 dB over the floor is not
	// voiced...
	evs := collectEvents(det)
	amp := ampForDBFS(-50) // floor settles at −90 clamp → E−F ≈ 40? use abs floor: −50 > −55.
	var sig []float64
	sig = appendSilence(sig, rate/2, 1)
	sig = appendTone(sig, 1000, amp, rate, rate, 1)
	processChunks(det, sig, 160, 1)
	require.NotEmpty(t, *evs, "20 dB over floor still fires against the −90 floor")

	// ...but a huge threshold blocks even that.
	det2, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	require.NoError(t, det2.SetThresholdsDB(60, 50))
	evs2 := collectEvents(det2)
	processChunks(det2, sig, 160, 1)
	assert.Empty(t, *evs2)

	// Setter validation for onset/hangover.
	require.ErrorIs(t, det.SetOnset(-time.Millisecond), ErrBadConfig)
	require.ErrorIs(t, det.SetHangover(-time.Millisecond), ErrBadConfig)
	require.NoError(t, det.SetHangover(time.Nanosecond))
	require.ErrorIs(t, det.SetAbsoluteFloorDB(math.NaN()), ErrBadThreshold)
	require.ErrorIs(t, det.SetAbsoluteFloorDB(math.Inf(1)), ErrBadThreshold)
	require.NoError(t, det.SetAbsoluteFloorDB(math.Inf(-1)))
}

// TestEnergyChunkInvariance: sample-by-sample feeding produces the
// exact same events as one giant Process call.
func TestEnergyChunkInvariance(t *testing.T) {
	const rate = 16000
	amp := ampForDBFS(-23)
	var sig []float64
	sig = appendSilence(sig, rate/2, 1)
	sig = appendTone(sig, 1000, amp, rate/2, rate, 1)
	sig = appendSilence(sig, rate/2, 1)

	run := func(chunkFrames int) []SpeechEvent {
		det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
		require.NoError(t, err)
		evs := collectEvents(det)
		processChunks(det, sig, chunkFrames, 1)
		return *evs
	}

	whole := run(len(sig))
	require.Len(t, whole, 2)
	assert.Equal(t, whole, run(1), "1-frame chunks must be decision-identical")
	assert.Equal(t, whole, run(997), "prime-sized chunks must be decision-identical")
}

// TestEnergyMultichannel: the detector hears speech through the
// stereo and >2-channel downmix paths.
func TestEnergyMultichannel(t *testing.T) {
	const rate = 16000
	amp := ampForDBFS(-23)
	for _, ch := range []int{2, 4} {
		det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: ch})
		require.NoError(t, err)
		evs := collectEvents(det)

		var sig []float64
		sig = appendSilence(sig, rate/2, ch)
		sig = appendTone(sig, 1000, amp, rate/2, rate, ch)
		processChunks(det, sig, 160, ch)

		require.NotEmpty(t, *evs, "channels=%d", ch)
		assert.Equal(t, SpeechStart, (*evs)[0].Kind)
		assert.Equal(t, int64(rate/2), (*evs)[0].Frame, "channels=%d", ch)
	}
}

// TestEnergyReset: Reset restarts positions at zero, clears state and
// readers, keeps configuration and subscriptions.
func TestEnergyReset(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	evs := collectEvents(det)

	var sig []float64
	sig = appendSilence(sig, rate/2, 1)
	sig = appendTone(sig, 1000, ampForDBFS(-23), rate/2, rate, 1)
	sig = appendSilence(sig, rate/2, 1)

	processChunks(det, sig, 160, 1)
	require.Len(t, *evs, 2)

	det.Reset()
	assert.False(t, det.Active())
	assert.Equal(t, 0.0, det.Probability())
	_, ok := det.LastTransition()
	assert.False(t, ok, "Reset must clear LastTransition")

	// The subscription survives Reset and positions restart at 0.
	processChunks(det, sig, 160, 1)
	require.Len(t, *evs, 4)
	assert.Equal(t, (*evs)[0], (*evs)[2], "a re-run after Reset must reproduce the first run exactly")
	assert.Equal(t, (*evs)[1], (*evs)[3])
}

// TestEnergyNaNFirstFrameDoesNotInitFloor: a non-finite first frame
// must not seed the adaptive floor — floorInit stays false until a
// finite frame arrives, at which point the floor initialises exactly
// as if that frame had been first all along.
func TestEnergyNaNFirstFrameDoesNotInitFloor(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)

	nanFrame := make([]float64, 160) // one 10 ms decision frame
	for i := range nanFrame {
		nanFrame[i] = math.NaN()
	}
	det.Process(nanFrame)
	assert.False(t, det.floorInit, "a non-finite first frame must not initialise the floor")
	assert.False(t, det.Active())

	// A second, still non-finite (this time +Inf) frame changes nothing.
	infFrame := make([]float64, 160)
	for i := range infFrame {
		infFrame[i] = math.Inf(1)
	}
	det.Process(infFrame)
	assert.False(t, det.floorInit)

	// The first FINITE frame (silence) initialises the floor normally.
	det.Process(make([]float64, 160))
	assert.True(t, det.floorInit)
	assert.False(t, math.IsNaN(det.floor))
	assert.False(t, math.IsInf(det.floor, 0))
}

// TestEnergyNaNMidStreamRecovers: a run of non-finite samples in the
// middle of an otherwise clean stream must not poison the adaptive
// floor — the detector must still cleanly detect a tone burst that
// follows, exactly as it would without the NaN gap ever having
// happened, rather than being permanently deaf (or permanently
// "voiced") from that point on.
func TestEnergyNaNMidStreamRecovers(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	evs := collectEvents(det)

	nanRun := make([]float64, rate/10) // 100 ms of NaN
	for i := range nanRun {
		nanRun[i] = math.NaN()
	}

	var sig []float64
	sig = appendSilence(sig, rate/2, 1) // establish a clean floor
	sig = append(sig, nanRun...)        // NaN gap: must not corrupt state
	sig = appendTone(sig, 1000, ampForDBFS(-23), rate/2, rate, 1)
	sig = appendSilence(sig, rate/2, 1)
	processChunks(det, sig, 160, 1)

	require.Len(t, *evs, 2, "the detector must recover and fire a clean Start/End pair for the tone burst after the NaN gap")
	assert.Equal(t, SpeechStart, (*evs)[0].Kind)
	assert.Equal(t, SpeechEnd, (*evs)[1].Kind)
	assert.False(t, math.IsNaN(det.floor), "the floor must never absorb a non-finite reading")
	assert.False(t, det.Active())
}

// TestEnergyThresholdFloat32Collapse: an (on, off) pair well-formed at
// float64 precision but that collapses to on == off once quantised to
// the float32 the atomic pack actually stores must be rejected up
// front — accepting it would let SetThresholdsDB/NewEnergyDetector
// silently produce a zero (or, for a slightly larger gap, inverted)
// hysteresis band that the float64-only validation could never see.
func TestEnergyThresholdFloat32Collapse(t *testing.T) {
	const rate = 16000

	on, off := 6.0, 6.0-1e-9
	require.Equal(t, float32(on), float32(off), "test fixture must actually reproduce the float32 collapse")

	_, err := NewEnergyDetector(EnergyConfig{
		SampleRate:     rate,
		Channels:       1,
		OnThresholdDB:  on,
		OffThresholdDB: off,
	})
	require.ErrorIs(t, err, ErrBadThreshold)

	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	require.ErrorIs(t, det.SetThresholdsDB(on, off), ErrBadThreshold)

	// A gap that DOES survive float32 quantisation must still be
	// accepted (the fix must not over-reject well-formed pairs).
	require.NoError(t, det.SetThresholdsDB(6.0, 5.0))
}
