package aec

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/generators"
)

// gateScenario is one synthetic full-duplex stream: a broadband far-end
// signal, a near-end talker, and optionally an echo of the far end
// looped back into the capture path.
type gateScenario struct {
	sampleRate int
	frames     int // 10ms frames
	echoGain   float64
	echoLag    int // frames
	gating     SuppressorGating
	// nearFrom/nearTo bound the frames carrying near-end speech.
	nearFrom, nearTo int
	// renderFrom/renderTo bound the frames carrying far-end audio;
	// zero/zero means the whole stream.
	renderFrom, renderTo int
}

// gateRun drives one scenario and returns, per frame, the near-end
// attenuation in dB (post-canceller RMS over pre-canceller RMS) and the
// suppressor state, plus the whole processed capture signal.
func gateRun(t *testing.T, sc gateScenario) (attenDB []float64, states []SuppressorState, out []float64) {
	t.Helper()

	c, err := NewCanceller(CancellerConfig{
		SampleRate:       sc.sampleRate,
		CaptureChannels:  1,
		RenderChannels:   1,
		SuppressorGating: sc.gating,
	})
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}

	n := sc.sampleRate / 100
	total := sc.frames * n
	far := generators.PinkNoise(time.Duration(sc.frames)*10*time.Millisecond, sc.sampleRate, 5)
	scale := 0.15 / rmsOf(far.Data)
	for i := range far.Data {
		far.Data[i] *= scale
	}
	renderTo := sc.renderTo
	if renderTo == 0 {
		renderTo = sc.frames
	}

	// The near-end talker: a warbling two-tone, uncorrelated with the
	// broadband far end.
	near := make([]float64, total)
	for i := range near {
		x := float64(i) / float64(sc.sampleRate)
		near[i] = 0.1 * math.Sin(2*math.Pi*300*x) * (0.5 + 0.5*math.Sin(2*math.Pi*3*x))
	}

	rng := rand.New(rand.NewSource(31))
	renderHist := make([][]float64, sc.frames)

	for k := 0; k < sc.frames; k++ {
		render := make([]float64, n)
		if k >= sc.renderFrom && k < renderTo {
			copy(render, far.Data[k*n:(k+1)*n])
		}
		renderHist[k] = render
		if err := c.FeedFarEnd(render); err != nil {
			t.Fatalf("FeedFarEnd: %v", err)
		}

		capture := make([]float64, n)
		for i := range capture {
			capture[i] = (rng.Float64()*2 - 1) * 0.003
		}
		if k >= sc.nearFrom && k < sc.nearTo {
			copy(capture, near[k*n:(k+1)*n])
		}
		if sc.echoGain > 0 && k >= sc.echoLag {
			for i, s := range renderHist[k-sc.echoLag] {
				capture[i] += s * sc.echoGain
			}
		}

		in := rmsOf(capture)
		c.Process(capture)
		attenDB = append(attenDB, 20*math.Log10((rmsOf(capture)+1e-12)/(in+1e-12)))
		states = append(states, c.Metrics().Suppressor)
		out = append(out, capture...)
	}
	return attenDB, states, out
}

func rmsOf(x []float64) float64 {
	var s float64
	for _, v := range x {
		s += v * v
	}
	if len(x) == 0 {
		return 0
	}
	return math.Sqrt(s / float64(len(x)))
}

// meanDB averages a slice of per-frame dB figures.
func meanDB(v []float64) float64 {
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

// TestSuppressorGate_StarvedCaptureIsNotAttenuated is the defect this
// gate exists for: when the capture path carries no echo correlated
// with the far end — an upstream canceller already removed it — the
// linear filter has nothing to converge on, and an ungated suppressor
// spends the far-end burst attenuating genuine near-end speech.
func TestSuppressorGate_StarvedCaptureIsNotAttenuated(t *testing.T) {
	sc := gateScenario{sampleRate: 16000, frames: 600, nearFrom: 0, nearTo: 600}

	sc.gating = SuppressorGatingAlwaysEngaged
	ungated, ungatedStates, _ := gateRun(t, sc)

	sc.gating = SuppressorGatingOnConvergence
	gated, gatedStates, _ := gateRun(t, sc)

	// Measure over the window the defect actually bites: the seconds
	// after the far end starts and before upstream's own transparent
	// mode (which needs 6s of active render) can help.
	const from, to = 100, 400
	ungatedAtten, gatedAtten := meanDB(ungated[from:to]), meanDB(gated[from:to])
	t.Logf("near-end attenuation over frames %d..%d: ungated %.1f dB, gated %.1f dB", from, to, ungatedAtten, gatedAtten)

	if ungatedAtten > -3 {
		t.Fatalf("scenario does not reproduce the defect: ungated attenuation %.1f dB, want < -3 dB", ungatedAtten)
	}
	if gatedAtten < -1.5 {
		t.Errorf("gated near-end attenuation = %.1f dB, want >= -1.5 dB (the suppressor must stay out of the capture path)", gatedAtten)
	}

	if got := gatedStates[len(gatedStates)-1]; got != SuppressorBypassed {
		t.Errorf("final suppressor state = %v, want SuppressorBypassed", got)
	}
	for i, s := range ungatedStates {
		if s != SuppressorEngaged {
			t.Fatalf("SuppressorGatingAlwaysEngaged reported state %v at frame %d, want SuppressorEngaged throughout", s, i)
		}
	}
}

// TestSuppressorGate_RealEchoIsUnchanged pins the other direction: with
// a genuine correlated echo the canceller converges, the gate never
// leaves SuppressorEngaged, and the output is bit-identical to the
// ungated canceller's — the gate costs echo cancellation nothing.
func TestSuppressorGate_RealEchoIsUnchanged(t *testing.T) {
	sc := gateScenario{sampleRate: 16000, frames: 600, echoGain: 0.5, echoLag: 12, nearFrom: 400, nearTo: 600}

	sc.gating = SuppressorGatingAlwaysEngaged
	ungatedAtten, _, ungatedOut := gateRun(t, sc)

	sc.gating = SuppressorGatingOnConvergence
	gatedAtten, gatedStates, gatedOut := gateRun(t, sc)

	for i, s := range gatedStates {
		if s != SuppressorEngaged {
			t.Fatalf("suppressor state = %v at frame %d, want SuppressorEngaged throughout a converged stream", s, i)
		}
	}
	for i := range ungatedOut {
		if ungatedOut[i] != gatedOut[i] {
			t.Fatalf("output differs from the ungated canceller at sample %d: %v vs %v", i, gatedOut[i], ungatedOut[i])
		}
	}

	// The echo must actually be cancelled, or the scenario proves
	// nothing: measure over the echo-only window.
	echoAtten := meanDB(gatedAtten[100:400])
	t.Logf("echo attenuation frames 100..400: %.1f dB (ungated %.1f dB)", echoAtten, meanDB(ungatedAtten[100:400]))
	if echoAtten > -15 {
		t.Errorf("echo attenuation = %.1f dB, want <= -15 dB", echoAtten)
	}
}

// TestSuppressorGate_EngagesOnLateEcho covers the ordering the gate
// guarantees: it may bypass once and engage once, in that order. A
// stream that starts starved bypasses, engages when a real echo path
// appears, and then stays engaged — it must not fall back out of the
// capture path when the echo goes quiet again, which would let the gate
// chatter across an utterance.
func TestSuppressorGate_EngagesOnLateEcho(t *testing.T) {
	const rate, n = 16000, 160
	c, err := NewCanceller(CancellerConfig{SampleRate: rate, CaptureChannels: 1, RenderChannels: 1})
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}

	const frames = 1200
	far := generators.PinkNoise(frames*10*time.Millisecond, rate, 9)
	scale := 0.15 / rmsOf(far.Data)
	for i := range far.Data {
		far.Data[i] *= scale
	}

	// Frames 0..399 starved, 400..799 with a real echo, 800.. starved
	// again.
	echoFrom, echoTo := 400, 800
	rng := rand.New(rand.NewSource(17))
	hist := make([][]float64, frames)
	states := make([]SuppressorState, frames)

	for k := 0; k < frames; k++ {
		render := append([]float64(nil), far.Data[k*n:(k+1)*n]...)
		hist[k] = render
		if err := c.FeedFarEnd(render); err != nil {
			t.Fatalf("FeedFarEnd: %v", err)
		}
		capture := make([]float64, n)
		for i := range capture {
			capture[i] = (rng.Float64()*2 - 1) * 0.003
		}
		if k >= echoFrom+12 && k < echoTo {
			for i, s := range hist[k-12] {
				capture[i] += s * 0.5
			}
		}
		c.Process(capture)
		states[k] = c.Metrics().Suppressor
	}

	if states[0] != SuppressorEngaged {
		t.Errorf("initial suppressor state = %v, want SuppressorEngaged", states[0])
	}
	if states[echoFrom-1] != SuppressorBypassed {
		t.Errorf("suppressor state before the echo appears = %v, want SuppressorBypassed", states[echoFrom-1])
	}

	// Count transitions between the two settled states; the gate must
	// make exactly two, bypass then engage.
	var settled []SuppressorState
	for _, s := range states {
		if s == SuppressorTransitioning {
			continue
		}
		if len(settled) == 0 || settled[len(settled)-1] != s {
			settled = append(settled, s)
		}
	}
	want := []SuppressorState{SuppressorEngaged, SuppressorBypassed, SuppressorEngaged}
	if len(settled) != len(want) {
		t.Fatalf("settled suppressor states = %v, want %v", settled, want)
	}
	for i := range want {
		if settled[i] != want[i] {
			t.Fatalf("settled suppressor states = %v, want %v", settled, want)
		}
	}

	// Once engaged, the gate must stay engaged even after the echo
	// stops — the residual-echo model, not the gate, decides what to do
	// with a path that has gone quiet.
	for k := echoTo; k < frames; k++ {
		if states[k] != SuppressorEngaged {
			t.Fatalf("suppressor state at frame %d (after the echo stopped) = %v, want SuppressorEngaged", k, states[k])
		}
	}
}

// TestSuppressorGate_TransitionIsRamped checks the gate spends several
// frames in SuppressorTransitioning rather than stepping the capture
// gain in one block.
func TestSuppressorGate_TransitionIsRamped(t *testing.T) {
	_, states, _ := gateRun(t, gateScenario{sampleRate: 16000, frames: 400, nearFrom: 0, nearTo: 400})
	var transitioning int
	for _, s := range states {
		if s == SuppressorTransitioning {
			transitioning++
		}
	}
	if transitioning < 2 {
		t.Errorf("frames reported as SuppressorTransitioning = %d, want at least 2 (the gate must ramp, not step)", transitioning)
	}
}

// TestSuppressorGate_ResetRearms confirms Reset returns the gate to its
// initial engaged state along with everything else.
func TestSuppressorGate_ResetRearms(t *testing.T) {
	const rate, n = 16000, 160
	c, err := NewCanceller(CancellerConfig{SampleRate: rate, CaptureChannels: 1, RenderChannels: 1})
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}
	far := generators.PinkNoise(4*time.Second, rate, 13)
	for i := range far.Data {
		far.Data[i] *= 0.3
	}
	for k := 0; k < 300; k++ {
		if err := c.FeedFarEnd(far.Data[k*n : (k+1)*n]); err != nil {
			t.Fatalf("FeedFarEnd: %v", err)
		}
		c.Process(make([]float64, n))
	}
	if got := c.Metrics().Suppressor; got != SuppressorBypassed {
		t.Fatalf("suppressor state before Reset = %v, want SuppressorBypassed", got)
	}
	c.Reset()
	if got := c.Metrics().Suppressor; got != SuppressorEngaged {
		t.Errorf("suppressor state after Reset = %v, want SuppressorEngaged", got)
	}
}
