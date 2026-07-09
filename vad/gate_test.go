package vad

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func TestNewGateValidation(t *testing.T) {
	det := newStubDetector(48000, 2)
	valid := GateConfig{SampleRate: 48000, Channels: 2, Detector: det}

	tests := []struct {
		name    string
		mutate  func(*GateConfig)
		wantErr error
	}{
		{"valid", func(c *GateConfig) {}, nil},
		{"nil detector", func(c *GateConfig) { c.Detector = nil }, ErrNilDetector},
		{"zero rate", func(c *GateConfig) { c.SampleRate = 0 }, ErrBadSampleRate},
		{"zero channels", func(c *GateConfig) { c.Channels = 0 }, ErrBadChannels},
		{"too many channels", func(c *GateConfig) { c.Channels = 65 }, ErrBadChannels},
		{"detector rate mismatch", func(c *GateConfig) { c.Detector = newStubDetector(44100, 2) }, ErrBadConfig},
		{"detector channel mismatch", func(c *GateConfig) { c.Detector = newStubDetector(48000, 1) }, ErrBadConfig},
		{"negative attack", func(c *GateConfig) { c.Attack = -time.Millisecond }, ErrBadConfig},
		{"negative release", func(c *GateConfig) { c.Release = -time.Millisecond }, ErrBadConfig},
		{"negative lookahead", func(c *GateConfig) { c.Lookahead = -time.Millisecond }, ErrBadConfig},
		{"positive floor", func(c *GateConfig) { c.FloorDB = 1 }, ErrBadConfig},
		{"NaN floor", func(c *GateConfig) { c.FloorDB = math.NaN() }, ErrBadConfig},
		{"-Inf floor ok", func(c *GateConfig) { c.FloorDB = math.Inf(-1) }, nil},
		{"partial floor ok", func(c *GateConfig) { c.FloorDB = -40 }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			g, err := NewGate(cfg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, g)
		})
	}
}

// TestGateFloorExactness: once the release ramp completes, the closed
// gain equals the configured floor EXACTLY (the linear ramp clamps to
// its target, no asymptotic tail).
func TestGateFloorExactness(t *testing.T) {
	const rate = 48000
	det := newStubDetector(rate, 1)
	g, err := NewGate(GateConfig{
		SampleRate: rate, Channels: 1, Detector: det,
		FloorDB: -40, Release: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	// Gate starts closed; open it first so the release has work to do.
	det.active.Store(true)
	buf := ones(rate / 2)
	g.Process(buf)
	assert.Equal(t, 1.0, buf[len(buf)-1], "fully open on a DC signal passes unity exactly")

	det.active.Store(false)
	buf = ones(rate / 2)
	g.Process(buf)
	want := mutations.Decibels(-40)
	assert.Equal(t, want, buf[len(buf)-1], "closed gain must equal the floor exactly")
	assert.InDelta(t, -40.0, g.GainDB(), 1e-12)

	// Full-mute default floor closes to exactly zero.
	det2 := newStubDetector(rate, 1)
	g2, err := NewGate(GateConfig{SampleRate: rate, Channels: 1, Detector: det2})
	require.NoError(t, err)
	buf = ones(rate / 4)
	g2.Process(buf)
	assert.Equal(t, 0.0, buf[len(buf)-1])
	assert.True(t, math.IsInf(g2.GainDB(), -1))
}

// TestGateClickBound: the per-sample gain change is hard-bounded by
// the linear ramp steps, across open/close transitions.
func TestGateClickBound(t *testing.T) {
	const rate = 48000
	attack := 5 * time.Millisecond   // 240 frames → step 1/240
	release := 20 * time.Millisecond // 960 frames → step 1/960
	det := newStubDetector(rate, 1)
	g, err := NewGate(GateConfig{
		SampleRate: rate, Channels: 1, Detector: det,
		Attack: attack, Release: release,
	})
	require.NoError(t, err)

	// A DC-1 signal makes the output the gain trajectory itself.
	// Toggle the detector every 50 ms for a second.
	var out []float64
	for seg := 0; seg < 20; seg++ {
		det.active.Store(seg%2 == 0)
		buf := ones(rate / 20)
		g.Process(buf)
		out = append(out, buf...)
	}

	maxStep := 1.0 / 240
	worst := 0.0
	for i := 1; i < len(out); i++ {
		if d := math.Abs(out[i] - out[i-1]); d > worst {
			worst = d
		}
	}
	assert.LessOrEqual(t, worst, maxStep+1e-12,
		"per-sample gain delta must never exceed the attack ramp step")
	assert.Greater(t, worst, 0.0, "the gate must actually have moved")
}

// TestGateLookaheadDelayExactness: with the detector pinned open, the
// gate is a pure delay of exactly LatencyFrames().
func TestGateLookaheadDelayExactness(t *testing.T) {
	const (
		rate      = 48000
		lookahead = 5 * time.Millisecond // 240 frames
	)
	det := newStubDetector(rate, 1)
	det.active.Store(true)
	g, err := NewGate(GateConfig{
		SampleRate: rate, Channels: 1, Detector: det,
		Lookahead: lookahead, Attack: time.Nanosecond, // instant open
	})
	require.NoError(t, err)
	require.Equal(t, 240, g.LatencyFrames())
	require.Equal(t, lookahead, g.Latency())

	const impulseAt = 1000
	buf := make([]float64, rate/10)
	buf[impulseAt] = 1
	g.Process(buf)

	for i := 0; i < len(buf); i++ {
		if i == impulseAt+240 {
			assert.Equal(t, 1.0, buf[i], "impulse must emerge exactly LatencyFrames later at unity gain")
		} else {
			assert.Equal(t, 0.0, buf[i], "no energy anywhere else (index %d)", i)
		}
	}
}

// TestGateOpensBeforeOnset: with Lookahead = DecisionLatency, the
// audio that TRIGGERED the detection emerges after the gate has
// already opened — the onset survives. Without lookahead the same
// onset is clipped. Uses a real EnergyDetector end to end.
func TestGateOpensBeforeOnset(t *testing.T) {
	const rate = 16000
	burstStart := rate / 2
	makeSig := func() []float64 {
		var sig []float64
		sig = appendSilence(sig, burstStart, 1)
		sig = appendTone(sig, 1000, 0.5, rate/2, rate, 1)
		return sig
	}

	// onsetEnergy measures the gated output's energy over the burst's
	// first 20 ms, at its DELAYED output position.
	run := func(lookahead time.Duration) float64 {
		det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
		require.NoError(t, err)
		g, err := NewGate(GateConfig{
			SampleRate: rate, Channels: 1, Detector: det,
			Lookahead: lookahead,
		})
		require.NoError(t, err)

		sig := makeSig()
		out := make([]float64, 0, len(sig))
		// 10 ms chunks, mixer-style.
		for off := 0; off < len(sig); off += 160 {
			end := off + 160
			if end > len(sig) {
				end = len(sig)
			}
			chunk := append([]float64(nil), sig[off:end]...)
			g.Process(chunk)
			out = append(out, chunk...)
		}
		pos := burstStart + g.LatencyFrames()
		sum := 0.0
		for i := pos; i < pos+320 && i < len(out); i++ {
			sum += out[i] * out[i]
		}
		return sum
	}

	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	withLookahead := run(det.DecisionLatency())
	without := run(0)
	assert.Greater(t, withLookahead, 5*without,
		"lookahead == DecisionLatency must preserve onset energy a zero-lookahead gate clips")
	// The preserved onset should be most of the dry burst energy
	// (0.5 amplitude sine over 320 samples ≈ 40 of sum-of-squares).
	assert.Greater(t, withLookahead, 20.0)
}

// TestGateFeedsDetector: the gate owns feeding its detector — events
// fire from within gate.Process, and gate.Reset resets the detector.
func TestGateFeedsDetector(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	evs := collectEvents(det)

	g, err := NewGate(GateConfig{SampleRate: rate, Channels: 1, Detector: det})
	require.NoError(t, err)

	var sig []float64
	sig = appendSilence(sig, rate/2, 1)
	sig = appendTone(sig, 1000, 0.5, rate/2, rate, 1)
	processChunks(g, sig, 160, 1)

	require.NotEmpty(t, *evs, "detector events must fire from within Gate.Process")
	assert.Equal(t, SpeechStart, (*evs)[0].Kind)
	assert.Equal(t, int64(rate/2), (*evs)[0].Frame)
	assert.True(t, det.Active())

	g.Reset()
	assert.False(t, det.Active(), "Gate.Reset must reset the detector it owns")
	_, ok := det.LastTransition()
	assert.False(t, ok)
}

// TestGateSetters: live setters validate and take effect.
func TestGateSetters(t *testing.T) {
	const rate = 48000
	det := newStubDetector(rate, 1)
	g, err := NewGate(GateConfig{SampleRate: rate, Channels: 1, Detector: det})
	require.NoError(t, err)

	require.ErrorIs(t, g.SetFloorDB(1), ErrBadConfig)
	require.ErrorIs(t, g.SetFloorDB(math.NaN()), ErrBadConfig)
	require.NoError(t, g.SetFloorDB(math.Inf(-1)))
	require.ErrorIs(t, g.SetAttack(-time.Second), ErrBadConfig)
	require.ErrorIs(t, g.SetRelease(-time.Second), ErrBadConfig)
	require.NoError(t, g.SetAttack(time.Nanosecond))
	require.NoError(t, g.SetRelease(time.Nanosecond))

	// New floor is reached through the (now instant) release.
	require.NoError(t, g.SetFloorDB(-20))
	buf := ones(480)
	g.Process(buf)
	assert.Equal(t, mutations.Decibels(-20), buf[len(buf)-1])
}

// ones returns n samples of DC 1.0.
func ones(n int) []float64 {
	buf := make([]float64, n)
	for i := range buf {
		buf[i] = 1
	}
	return buf
}
