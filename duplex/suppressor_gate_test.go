package duplex

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/aec"
	"github.com/daniel-sullivan/go-mediatoolkit/vad"
)

// speechLike synthesises a deterministic speech-like signal the neural
// detector reliably scores as speech: 250ms-scale syllables built from a
// short noise burst plus a voiced nucleus whose formants sweep over a
// declining, vibrato'd fundamental. Silero scores stationary tones near
// zero, so a usable stand-in for a talker has to move the way speech
// does. f0/syl/vib are varied per talker so two callers of this
// function produce signals with no exploitable correlation.
func speechLike(seed int64, rate, n int, f0, syl, vib, targetRMS float64) []float64 {
	rng := rand.New(rand.NewSource(seed))
	out := make([]float64, n)
	var phase, sum float64
	for i := range out {
		t := float64(i) / float64(rate)
		ut := math.Mod(t, 1.5)
		f := f0 - 0.19*f0*(ut/1.5) + 6*math.Sin(2*math.Pi*vib*t)
		phase += 2 * math.Pi * f / float64(rate)

		burst := syl * 0.2
		pos := math.Mod(ut, syl)
		if pos < burst {
			out[i] = 0.15 * math.Sin(math.Pi*pos/burst) * (rng.Float64()*2 - 1)
			sum += out[i] * out[i]
			continue
		}

		fr := (pos - burst) / (syl - burst)
		dir := 1.0
		if int(ut/syl)%2 == 1 {
			dir = -1
		}
		f1, f2 := 400+dir*250*fr, 1800-dir*700*fr
		var v float64
		for k := 1; k <= 14; k++ {
			fk := float64(k) * f
			w := math.Exp(-(fk-f1)*(fk-f1)/(2*150*150)) + 0.7*math.Exp(-(fk-f2)*(fk-f2)/(2*250*250))
			v += w * math.Sin(float64(k)*phase)
		}
		env := math.Min(1, math.Min(fr*8, (1-fr)*4+0.2))
		am := 0.85 + 0.15*math.Sin(2*math.Pi*vib*0.66*t)
		out[i] = 0.95 * math.Tanh(0.25*env*am*v)
		sum += out[i] * out[i]
	}
	if r := math.Sqrt(sum / float64(n)); r > 0 {
		for i := range out {
			out[i] *= targetRMS / r
		}
	}
	return out
}

// bargeInResult is one scenario's outcome: how long after the near-end
// talker started the detector announced them, and how many times the
// echo alone announced a talker who was not there.
type bargeInResult struct {
	latency  time.Duration
	detected bool
	phantoms int
	// atOnset is the metrics snapshot taken the instant the near-end
	// talker starts, so it reflects the canceller's state after the
	// far-end-only phase rather than after the double-talk that follows.
	atOnset aec.Metrics
	aec     aec.Metrics
}

// runBargeIn plays a far-end talker for bargeInAt, then adds a near-end
// talker on top, and reports when the engine's neural detector notices.
// echoGain > 0 loops the emitted render audio back into the capture path
// (a real acoustic echo); echoGain == 0 models a capture path that
// arrives already echo-cancelled, so the canceller is starved.
func runBargeIn(t *testing.T, cfgAEC *AECConfig, echoGain float64, bargeInAt time.Duration) bargeInResult {
	t.Helper()
	const (
		rate     = 16000
		frames   = 900
		echoLagF = 12 // 120ms acoustic path
		micNoise = 0.003
		graceF   = 30 // the session's own warm-up; events here are ignored
	)
	frameLen := rate / 100
	bargeInF := int(bargeInAt / frameDuration)
	require.Greater(t, bargeInF, graceF)
	require.Less(t, bargeInF, frames-200)

	far := speechLike(11, rate, frames*frameLen, 130, 0.25, 5.0, 0.1)
	near := speechLike(29, rate, frames*frameLen, 196, 0.31, 6.7, 0.1)

	det, err := vad.NewSileroDetector(vad.SileroConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	e, err := New(Config{
		SampleRate:  rate,
		Channels:    1,
		Detector:    det,
		AEC:         cfgAEC,
		EventBuffer: 1 << 16,
	})
	require.NoError(t, err)
	require.NoError(t, e.FeedChunk(far))
	if cfgAEC != nil {
		e.SetAudioBufferDelay(echoLagF * frameDuration)
	}

	emitted := make([][]float64, 0, frames)
	e.SetOutput(func(frame []float64, seq int64) {
		emitted = append(emitted, append([]float64(nil), frame...))
	})

	rng := rand.New(rand.NewSource(97))
	res := bargeInResult{}

	for k := 0; k < frames; k++ {
		e.tick()

		if k == bargeInF {
			res.atOnset = e.Metrics().AEC
		}

		capture := make([]float64, frameLen)
		if k >= bargeInF {
			copy(capture, near[(k-bargeInF)*frameLen:(k-bargeInF+1)*frameLen])
		}
		for i := range capture {
			capture[i] += (rng.Float64()*2 - 1) * micNoise
		}
		if echoGain > 0 && k >= echoLagF {
			for i, s := range emitted[k-echoLagF] {
				capture[i] += s * echoGain
			}
		}
		require.NoError(t, e.Push(capture, int64(k*10)))

		for _, ev := range drainEvents(e) {
			if ev.Kind != EventSpeechStart {
				continue
			}
			switch {
			case k < graceF:
			case k < bargeInF:
				res.phantoms++
			case !res.detected:
				res.detected = true
				res.latency = time.Duration(k-bargeInF+1) * frameDuration
			}
		}
	}
	res.aec = e.Metrics().AEC
	return res
}

// TestEngine_BargeInIsNotSwallowedByAStarvedCanceller is the end-to-end
// form of the defect the suppressor gate fixes. On a capture path that
// arrives already echo-cancelled the canceller can never converge, and
// an ungated suppressor attenuates the near-end talker for seconds — an
// interruption the engine exists to hear goes unheard. With the gate on
// (the default) the detector hears the interruption as promptly as it
// does with no canceller at all.
func TestEngine_BargeInIsNotSwallowedByAStarvedCanceller(t *testing.T) {
	const bargeInAt = 2 * time.Second

	noAEC := runBargeIn(t, nil, 0, bargeInAt)
	ungated := runBargeIn(t, &AECConfig{SuppressorGating: aec.SuppressorGatingAlwaysEngaged}, 0, bargeInAt)
	gated := runBargeIn(t, &AECConfig{}, 0, bargeInAt)

	t.Logf("starved capture: no AEC %v, AEC always-engaged %v, AEC convergence-gated %v (suppressor %d)",
		noAEC.latency, ungated.latency, gated.latency, gated.aec.Suppressor)

	require.True(t, noAEC.detected, "the reference run must hear the near-end talker")
	require.Greater(t, ungated.latency, noAEC.latency+time.Second,
		"scenario does not reproduce the defect: an ungated suppressor should swallow the interruption")

	assert.True(t, gated.detected, "the gated canceller must hear the near-end talker")
	assert.LessOrEqual(t, gated.latency, noAEC.latency+150*time.Millisecond,
		"gated barge-in latency must come back to the no-canceller figure")
	assert.Equal(t, aec.SuppressorBypassed, gated.aec.Suppressor,
		"a canceller with nothing to cancel must gate its suppressor out of the capture path")
}

// TestEngine_BargeInWithRealEchoIsUnaffected is the other direction: a
// genuine acoustic echo converges the canceller, so the gate engages and
// stays engaged. The echo must never register as a talker, and the real
// interruption must still be heard as promptly as with the suppressor
// ungated.
func TestEngine_BargeInWithRealEchoIsUnaffected(t *testing.T) {
	const bargeInAt = 5 * time.Second

	ungated := runBargeIn(t, &AECConfig{SuppressorGating: aec.SuppressorGatingAlwaysEngaged}, 0.5, bargeInAt)
	gated := runBargeIn(t, &AECConfig{}, 0.5, bargeInAt)

	t.Logf("real echo: always-engaged %v (%d phantom), convergence-gated %v (%d phantom); at onset ERL %.1f dB ERLE %.1f dB delay %dms",
		ungated.latency, ungated.phantoms, gated.latency, gated.phantoms,
		gated.atOnset.EchoReturnLoss, gated.atOnset.EchoReturnLossEnhancement, gated.atOnset.DelayMs)

	assert.Equal(t, aec.SuppressorEngaged, gated.aec.Suppressor,
		"a converged canceller must keep its suppressor engaged")
	assert.Zero(t, gated.phantoms, "the cancelled echo must never register as a talker")
	assert.Zero(t, ungated.phantoms, "reference run: the cancelled echo must never register as a talker")
	assert.True(t, gated.detected, "the near-end talker must still be heard over the echo")
	assert.Equal(t, ungated.latency, gated.latency,
		"gating must not change behaviour on a converged stream")
	assert.Greater(t, gated.atOnset.EchoReturnLossEnhancement, 20.0,
		"the canceller must have converged on the loopback echo")
	assert.NotZero(t, gated.atOnset.DelayMs, "the delay estimator must have locked onto the loopback lag")
}
