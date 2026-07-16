package duplex

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/loudness"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/vad"
)

// TestEngine_AECLoopbackSuppressesEchoThenHearsRealSpeech is the
// end-to-end duplex scenario the engine exists for: far-end audio
// plays out while the capture path receives a delayed, attenuated
// copy of it (the acoustic echo) over a realistic microphone noise
// floor. After the canceller converges, the echo must not register as
// speech; genuine near-end speech superimposed mid-playout must.
//
// The capture synthesis mixes a ~−45 dBFS noise floor into every
// frame (a real microphone always has one), and the detector is
// tuned with FloorRise: time.Second so its adaptive floor anchors to
// the post-AEC idle level within the session: the canceller's output
// starts from a zero-primed delay line and its suppressor attenuates
// even the mic noise, so the floor climbs from its −90 dB minimum —
// at the 4 s default rise it would still sit ~12 dB below the echo
// residual when speech freezes it, leaving the release threshold
// (floor+Off) below the residual, and the utterance could never
// close.
//
// Timeline, at one manual tick per 10 ms frame:
//
//	ticks    0..299  echo only — convergence grace, events ignored
//	ticks  300..599  echo only — NO SpeechStart may fire
//	ticks  600..649  echo + a 440 Hz near-end tone — SpeechStart must fire
//	ticks  650..799  echo only again — the utterance must close
func TestEngine_AECLoopbackSuppressesEchoThenHearsRealSpeech(t *testing.T) {
	const (
		rate            = 16000
		frames          = 800
		echoDelayFrames = 6   // 60ms acoustic path
		echoGain        = 0.5 // −6 dB reflection
		graceFrames     = 300
		speechFrom      = 600
		speechTo        = 650
		micNoiseAmp     = 0.01 // uniform, ≈ −44.7 dBFS RMS
		minReductionDB  = 15.0
	)

	// Broadband far-end (AEC3's delay estimator needs broadband, not
	// a tone), normalized to a known −16.5 dBFS RMS so the energy
	// detector's absolute floor below is meaningful.
	far := generators.PinkNoise(frames*frameDuration, rate, 1)
	scale := 0.15 / loudness.RMS(far.Data)
	for i := range far.Data {
		far.Data[i] *= scale
	}

	// The absolute floor sits between the converged residual (echo
	// residual + mic noise, ≈ −43 dBFS if the canceller achieves the
	// asserted 15 dB reduction on the ≈ −22 dBFS echo) and the
	// near-end tone (−13.5 dBFS RMS): the residual can never gate
	// through, the tone always can.
	det, err := vad.NewEnergyDetector(vad.EnergyConfig{
		SampleRate:      rate,
		Channels:        1,
		AbsoluteFloorDB: -30,
		FloorRise:       time.Second,
	})
	require.NoError(t, err)

	probe := new(captureProbe) // records post-AEC capture frames
	e, err := New(Config{
		SampleRate:   rate,
		Channels:     1,
		Detector:     det,
		AEC:          new(AECConfig),
		CaptureChain: []mutations.Processor{probe},
		PreRoll:      300 * time.Millisecond,
		EventBuffer:  1 << 15,
	})
	require.NoError(t, err)
	require.NoError(t, e.FeedChunk(far.Data))
	e.SetAudioBufferDelay(echoDelayFrames * frameDuration)

	emitted := make([][]float64, 0, frames)
	e.SetOutput(func(frame []float64, seq int64) {
		emitted = append(emitted, append([]float64(nil), frame...))
	})

	var echoRMSSum, echoRMSN float64 // pre-AEC echo level over the measurement window
	var sawStartDuringEcho, sawStartDuringSpeech, sawStop bool
	rng := rand.New(rand.NewSource(2))

	for k := 0; k < frames; k++ {
		e.tick()

		// Synthesize this tick's capture: the freshly-emitted render
		// audio comes back 60ms later at −6dB over the mic noise
		// floor, plus — in the speech window — a genuine near-end
		// tone.
		capture := make([]float64, rate/100)
		for i := range capture {
			capture[i] = (rng.Float64()*2 - 1) * micNoiseAmp
		}
		if k >= echoDelayFrames {
			for i, s := range emitted[k-echoDelayFrames] {
				capture[i] += s * echoGain
			}
		}
		if k >= speechFrom && k < speechTo {
			for i := range capture {
				n := (k-speechFrom)*len(capture) + i
				capture[i] += 0.3 * math.Sin(2*math.Pi*440*float64(n)/rate)
			}
		}
		if k >= graceFrames && k < speechFrom {
			echoRMSSum += loudness.RMS(capture)
			echoRMSN++
		}
		require.NoError(t, e.Push(capture, int64(k*10)))

		for _, ev := range drainEvents(e) {
			if _, ok := ev.(SpeechStop); ok && k >= speechTo {
				sawStop = true
			}
			if _, ok := ev.(SpeechStart); !ok {
				continue
			}
			switch {
			case k < graceFrames: // convergence grace — ignored
			case k < speechFrom:
				sawStartDuringEcho = true
			default:
				sawStartDuringSpeech = true
			}
		}
	}

	assert.False(t, sawStartDuringEcho, "converged echo must never register as speech")
	assert.True(t, sawStartDuringSpeech, "genuine near-end speech during playout must register")
	assert.True(t, sawStop, "the utterance must close once the near-end tone ends")

	// Echo reduction: post-AEC residual over the last second of the
	// echo-only phase vs the pre-AEC echo level over the whole
	// measured window.
	var residual []float64
	for _, f := range probe.frames[speechFrom-100 : speechFrom] {
		residual = append(residual, f...)
	}
	echoDB := 20 * math.Log10(echoRMSSum/echoRMSN)
	residualDB := 20 * math.Log10(loudness.RMS(residual))
	reduction := echoDB - residualDB
	t.Logf("echo=%.1f dBFS residual=%.1f dBFS reduction=%.1f dB metrics=%+v",
		echoDB, residualDB, reduction, e.Metrics().AEC)
	assert.GreaterOrEqual(t, reduction, minReductionDB, "canceller must converge on the loopback echo")
	assert.Positive(t, e.Metrics().AEC.EchoReturnLossEnhancement, "suppressor must report measured improvement")
	assert.Zero(t, e.Metrics().AEC.RenderFramesDropped, "render and capture stayed paced together")
}

// BenchmarkEngineTick_AECSileroPreRoll measures one fully-loaded tick
// — paced render with far-end feed, AEC echo cancellation, Silero
// neural VAD, pre-roll banking — the per-10ms budget a realtime voice
// agent burns per session. Sessions/core ≈ 10ms / (ns/op).
func BenchmarkEngineTick_AECSileroPreRoll(b *testing.B) {
	const rate = 16000
	det, err := vad.NewSileroDetector(vad.SileroConfig{SampleRate: rate, Channels: 1})
	if err != nil {
		b.Fatal(err)
	}
	e, err := New(Config{
		SampleRate:  rate,
		Channels:    1,
		Detector:    det,
		AEC:         new(AECConfig),
		PreRoll:     300 * time.Millisecond,
		EventBuffer: 1 << 15,
	})
	if err != nil {
		b.Fatal(err)
	}

	far := generators.PinkNoise(time.Second, rate, 1)
	for i := range far.Data {
		far.Data[i] *= 0.3
	}
	frame := rate / 100
	capture := make([]float64, frame)
	var prev []float64

	e.SetOutput(func(f []float64, seq int64) {
		// Loop the emitted audio back as next tick's echo.
		prev = append(prev[:0], f...)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		off := i * frame % len(far.Data)
		if off+frame <= len(far.Data) {
			if err := e.FeedChunk(far.Data[off : off+frame]); err != nil {
				b.Fatal(err)
			}
		}
		for j := range capture {
			capture[j] = 0
		}
		for j, s := range prev {
			capture[j] = s * 0.5
		}
		if err := e.Push(capture, int64(i*10)); err != nil {
			b.Fatal(err)
		}
		e.tick()
		drainEvents(e)
	}
}
