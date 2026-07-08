package loudness

import (
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── construction ─────────────────────────────────────────────────────────

func TestNewMonitorValidation(t *testing.T) {
	tests := []struct {
		name     string
		rate     int
		channels int
		mode     Mode
		wantErr  error
	}{
		{"ok stereo all", 48000, 2, ModeAll, nil},
		{"zero rate", 0, 2, ModeIntegrated, ErrBadSampleRate},
		{"zero channels", 48000, 0, ModeIntegrated, ErrBadChannels},
		{"too many channels", 48000, 65, ModeIntegrated, ErrBadChannels},
		{"histogram-only mode", 48000, 2, ModeHistogram, ErrBadConfig},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mon, err := NewMonitor(tc.rate, tc.channels, tc.mode)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, mon)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, mon)
			assert.Equal(t, tc.rate, mon.SampleRate())
			assert.Equal(t, tc.channels, mon.Channels())
			assert.Equal(t, tc.mode, mon.Mode())
		})
	}
}

// ── pass-through: audio is never mutated ────────────────────────────────

func TestMonitorProcessLeavesSamplesUnchanged(t *testing.T) {
	mon, err := NewMonitor(48000, 2, ModeAll)
	require.NoError(t, err)

	samples := testSine(0.4, 440, 48000, 0, 4800, 2)
	original := append([]float64(nil), samples...)

	mon.Process(samples)

	assert.Equal(t, original, samples, "Process must not mutate the buffer")
}

// ── readings match an equivalently-fed bare Meter exactly ──────────────

func TestMonitorReadingsMatchBareMeter(t *testing.T) {
	const (
		rate     = 48000
		freq     = 997.0
		amp      = 0.3
		channels = 2
		chunk    = 4800 // 100 ms
		chunks   = 40   // 4 s total
	)

	mon, err := NewMonitor(rate, channels, ModeAll)
	require.NoError(t, err)
	meter, err := NewMeter(rate, channels, ModeAll)
	require.NoError(t, err)

	for i := 0; i < chunks; i++ {
		buf := testSine(amp, freq, rate, i*chunk, chunk, channels)
		mon.Process(buf)
		require.NoError(t, meter.AddFrames(buf))
	}

	monInteg, err := mon.Integrated()
	require.NoError(t, err)
	meterInteg, err := meter.Integrated()
	require.NoError(t, err)
	assert.Equal(t, meterInteg, monInteg)

	monMom, err := mon.Momentary()
	require.NoError(t, err)
	meterMom, err := meter.Momentary()
	require.NoError(t, err)
	assert.Equal(t, meterMom, monMom)

	monST, err := mon.ShortTerm()
	require.NoError(t, err)
	meterST, err := meter.ShortTerm()
	require.NoError(t, err)
	assert.Equal(t, meterST, monST)

	monRange, err := mon.Range()
	require.NoError(t, err)
	meterRange, err := meter.Range()
	require.NoError(t, err)
	assert.Equal(t, meterRange, monRange)

	monRel, err := mon.RelativeThreshold()
	require.NoError(t, err)
	meterRel, err := meter.RelativeThreshold()
	require.NoError(t, err)
	assert.Equal(t, meterRel, monRel)

	for ch := 0; ch < channels; ch++ {
		monSP, err := mon.SamplePeak(ch)
		require.NoError(t, err)
		meterSP, err := meter.SamplePeak(ch)
		require.NoError(t, err)
		assert.Equal(t, meterSP, monSP, "SamplePeak(%d)", ch)

		monPSP, err := mon.PrevSamplePeak(ch)
		require.NoError(t, err)
		meterPSP, err := meter.PrevSamplePeak(ch)
		require.NoError(t, err)
		assert.Equal(t, meterPSP, monPSP, "PrevSamplePeak(%d)", ch)

		monTP, err := mon.TruePeak(ch)
		require.NoError(t, err)
		meterTP, err := meter.TruePeak(ch)
		require.NoError(t, err)
		assert.Equal(t, meterTP, monTP, "TruePeak(%d)", ch)

		monPTP, err := mon.PrevTruePeak(ch)
		require.NoError(t, err)
		meterPTP, err := meter.PrevTruePeak(ch)
		require.NoError(t, err)
		assert.Equal(t, meterPTP, monPTP, "PrevTruePeak(%d)", ch)
	}

	// Reset drives both back to the same fresh state.
	mon.Reset()
	meter.Reset()
	monInteg, err = mon.Integrated()
	require.NoError(t, err)
	meterInteg, err = meter.Integrated()
	require.NoError(t, err)
	assert.Equal(t, meterInteg, monInteg)
	assert.True(t, math.IsInf(monInteg, -1))
}

// TestMonitorSetChannelSetMaxWindowHistoryPassThrough exercises the
// remaining mutex-guarded pass-throughs against the sentinel errors
// their Meter equivalents already document.
func TestMonitorSetChannelSetMaxWindowHistoryPassThrough(t *testing.T) {
	mon, err := NewMonitor(48000, 2, ModeAll)
	require.NoError(t, err)

	assert.NoError(t, mon.SetChannel(0, ChannelLeft))
	assert.ErrorIs(t, mon.SetChannel(5, ChannelLeft), ErrBadChannelIndex)

	assert.ErrorIs(t, mon.SetMaxWindow(0), ErrBadWindow)
	assert.NoError(t, mon.SetMaxWindow(500_000_000)) // 500ms, as time.Duration nanoseconds

	assert.ErrorIs(t, mon.SetMaxHistory(0), ErrBadWindow)
	assert.NoError(t, mon.SetMaxHistory(10_000_000_000)) // 10s
}

// ── misaligned buffers ───────────────────────────────────────────────────

func TestMonitorMisalignedBufferMetersAlignedPrefixOnly(t *testing.T) {
	const (
		rate     = 48000
		channels = 2
	)
	mon, err := NewMonitor(rate, channels, ModeAll)
	require.NoError(t, err)
	meter, err := NewMeter(rate, channels, ModeAll)
	require.NoError(t, err)

	full := testSine(0.5, 300, rate, 0, 4800, channels) // 9600 samples, aligned
	// Drop the final sample so the buffer is one sample short of a
	// whole frame (9599 samples, not a multiple of 2).
	misaligned := full[:len(full)-1]
	alignedPrefix := full[:len(full)-2] // the last full frame dropped too

	require.NotPanics(t, func() {
		mon.Process(misaligned)
	})
	require.NoError(t, meter.AddFrames(alignedPrefix))

	monInteg, err := mon.Integrated()
	require.NoError(t, err)
	meterInteg, err := meter.Integrated()
	require.NoError(t, err)
	assert.Equal(t, meterInteg, monInteg,
		"Process on a misaligned buffer should meter exactly the aligned prefix")
}

func TestMonitorEmptyAndShortBuffersDoNotPanic(t *testing.T) {
	mon, err := NewMonitor(48000, 2, ModeAll)
	require.NoError(t, err)

	assert.NotPanics(t, func() { mon.Process(nil) })
	assert.NotPanics(t, func() { mon.Process([]float64{}) })
	assert.NotPanics(t, func() { mon.Process([]float64{0.5}) }) // shorter than one frame
}

// ── concurrency ──────────────────────────────────────────────────────────

// TestMonitorConcurrentProcessAndReadersRace drives Process from one
// goroutine while hammering every reader from others, meant to be run
// with -race: Monitor exists specifically so this pattern (mixer mix
// goroutine writing, UI goroutines reading) is safe.
func TestMonitorConcurrentProcessAndReadersRace(t *testing.T) {
	const (
		rate       = 48000
		channels   = 2
		chunk      = 4800 // 100 ms
		iterations = 50
	)
	mon, err := NewMonitor(rate, channels, ModeAll)
	require.NoError(t, err)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stop)
		for i := 0; i < iterations; i++ {
			buf := testSine(0.4, 997, rate, i*chunk, chunk, channels)
			mon.Process(buf)
		}
	}()

	readers := []func(){
		func() { _, _ = mon.Momentary() },
		func() { _, _ = mon.ShortTerm() },
		func() { _, _ = mon.Integrated() },
		func() { _, _ = mon.Range() },
		func() { _, _ = mon.RelativeThreshold() },
		func() { _, _ = mon.SamplePeak(0) },
		func() { _, _ = mon.PrevSamplePeak(1) },
		func() { _, _ = mon.TruePeak(0) },
		func() { _, _ = mon.PrevTruePeak(1) },
	}
	for _, read := range readers {
		wg.Add(1)
		go func(read func()) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					read()
				}
			}
		}(read)
	}

	wg.Wait()
}
