//go:build cgo && aec_oracle

package ec3

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// This slice drives the REAL, fetched AEC3 C++ oracle's top-level
// webrtc::EchoCanceller3 (see cgo.go/shim.h/shim.cc) directly against
// this port's aec/internal/aec3.EchoCanceller3 -- the exact type
// aec.Canceller (the public API, Part 4) wraps -- frame by frame,
// requiring BIT-EXACT agreement (no tolerance) on:
//
//   - every processed capture frame (the actual echo-cancelled audio
//     ProcessCapture produces), and
//   - every GetMetrics() snapshot (EchoReturnLoss,
//     EchoReturnLossEnhancement, DelayMs), taken once per frame.
//
// # Clockdrift is not compared here
//
// The spec for this slice called for comparing clockdrift bit-exactly
// alongside audio and GetMetrics on every frame. That isn't possible
// at this integration boundary: unlike EchoReturnLoss/
// EchoReturnLossEnhancement/DelayMs (all fields of
// EchoControl::Metrics), upstream's webrtc::EchoCanceller3 exposes NO
// public accessor for clockdrift level at all (see shim.h's
// aec3_get_metrics doc comment; confirmed directly against
// echo_canceller3.h and echo_control.h in the fetched oracle tree --
// ClockdriftDetector's level lives several layers down, inside
// RenderDelayController, and is never surfaced through
// EchoCanceller3's or EchoControl's public interface). This is not a
// gap introduced by this port or by this test's design: it is a
// property of the real class being wrapped. Clockdrift IS already
// bit-exact-parity-tested, at the layer where the oracle actually
// exposes it: see the aecstate, delaypipe, and echoremover slices
// (which wrap RenderDelayController/AecState/EchoRemover directly and
// do have the necessary internal-header access). This slice instead
// only asserts that aec3.EchoCanceller3.Clockdrift() -- a Go-port-only
// convenience accessor with no upstream counterpart at this level --
// returns one of its three valid values on every frame, as a
// non-comparative sanity check.
//
// # Scenario/rate/channel matrix
//
// Scenario shapes reuse the echoremover slice's pattern (bulk fixed
// delays, mid-stream delay change, clock drift, double-talk,
// saturation, silence, gain change), adapted from block/single-band
// granularity to full 10ms-frame/full-band granularity (the domain
// EchoCanceller3's own AnalyzeRender/AnalyzeCapture/ProcessCapture
// operate in -- band-splitting happens inside EchoCanceller3 itself
// and is not driven directly here).
//
//   - 16kHz mono (1x1): the full 7-scenario list.
//   - 32kHz and 48kHz mono: the bulk-delay, double-talk, and
//     clock-drift scenarios (the three explicitly required at
//     non-16kHz rates).
//   - 16kHz stereo (2x2): the same bulk-delay/double-talk/drift subset.
//   - 16kHz 2x1 and 1x2 (differing render/capture channel counts):
//     one bulk-delay scenario each -- cheap to add given the driver
//     already parameterizes channel counts independently, so included
//     per the spec's "only if cheap" allowance.
func TestEc3Parity(t *testing.T) {
	t.Run("16kHz_mono_full", func(t *testing.T) {
		for _, sc := range fullScenarioSet(16000, 1, 1) {
			sc := sc
			t.Run(sc.name, func(t *testing.T) { runScenario(t, sc) })
		}
	})
	t.Run("32kHz_mono_subset", func(t *testing.T) {
		for _, sc := range coreScenarioSet(32000, 1, 1) {
			sc := sc
			t.Run(sc.name, func(t *testing.T) { runScenario(t, sc) })
		}
	})
	t.Run("48kHz_mono_subset", func(t *testing.T) {
		for _, sc := range coreScenarioSet(48000, 1, 1) {
			sc := sc
			t.Run(sc.name, func(t *testing.T) { runScenario(t, sc) })
		}
	})
	t.Run("16kHz_stereo_subset", func(t *testing.T) {
		for _, sc := range coreScenarioSet(16000, 2, 2) {
			sc := sc
			t.Run(sc.name, func(t *testing.T) { runScenario(t, sc) })
		}
	})
	t.Run("16kHz_mixed_channels", func(t *testing.T) {
		t.Run("2x1", func(t *testing.T) { runScenario(t, bulkDelayScenario(16000, 2, 1, 0x1111)) })
		t.Run("1x2", func(t *testing.T) { runScenario(t, bulkDelayScenario(16000, 1, 2, 0x2222)) })
	})
}

// lcg is the same minimal, Go-version-independent deterministic PRNG
// the other parity slices use (Numerical Recipes constants) -- not
// math/rand, whose algorithm is not covered by its compatibility
// guarantee.
type lcg uint32

func (l *lcg) next() float32 {
	*l = *l*1664525 + 1013904223
	return float32(int32(*l)) / float32(math.MaxInt32)
}

// scenario fully describes one run: the stream shape/size, and the
// three streams (render, capture -- channel-major, numFrames*frameLen
// samples per channel, FloatS16 domain) plus optional per-frame event
// maps (frame index -> value; a missing key means "no event/false").
type scenario struct {
	name            string
	sampleRate      int
	renderChannels  int
	captureChannels int
	numFrames       int

	render  [][]float32
	capture [][]float32

	levelChangeAt map[int]bool
	setDelayAt    map[int]int // frame index -> delay ms to apply via SetAudioBufferDelay before that frame
}

func fullScenarioSet(rate, renderCh, captureCh int) []scenario {
	s := coreScenarioSet(rate, renderCh, captureCh)
	s = append(s,
		delayChangeScenario(rate, renderCh, captureCh),
		saturationScenario(rate, renderCh, captureCh),
		silenceWindowScenario(rate, renderCh, captureCh),
		gainChangeScenario(rate, renderCh, captureCh),
		delayHintScenario(rate, renderCh, captureCh),
	)
	return s
}

func coreScenarioSet(rate, renderCh, captureCh int) []scenario {
	return []scenario{
		bulkDelayScenario(rate, renderCh, captureCh, 0xC0FFEE),
		doubleTalkScenario(rate, renderCh, captureCh),
		clockDriftScenario(rate, renderCh, captureCh),
	}
}

const (
	numFramesDefault = 500 // 5s at every supported rate (frame = 10ms)
	renderAmplitude  = 4000.0
	noiseAmplitude   = 20.0
	nearEndAmplitude = 6000.0
)

// makeMultiChannelNoise returns ch independent broadband noise
// channels, each numFrames*frameLen samples, seeded distinctly per
// channel (seed ^ channel index) so channels are genuinely
// decorrelated -- exercising real multichannel behavior rather than
// duplicating one channel across all of them.
func makeMultiChannelNoise(ch, numFrames, frameLen int, seed uint32, amplitude float32) [][]float32 {
	out := make([][]float32, ch)
	for c := 0; c < ch; c++ {
		rng := lcg(seed ^ (uint32(c) * 0x9E3779B1))
		buf := make([]float32, numFrames*frameLen)
		for i := range buf {
			buf[i] = rng.next() * amplitude
		}
		out[c] = buf
	}
	return out
}

// applyEcho fills capture[c] with gain*render[c%len(render)] delayed
// by delaySamples, plus a small independent noise floor -- a pure
// linear acoustic echo path, the simplest signal an echo canceller
// must suppress. delaySamples/gain may vary per output sample via
// delayAt/gainAt (nil means constant delaySamplesConst/gainConst).
func applyEcho(renderCh [][]float32, captureCh int, n int, delaySamplesConst int, gainConst float32,
	delayAt func(i int) int, gainAt func(i int) float32, noiseSeed uint32) [][]float32 {
	capture := make([][]float32, captureCh)
	for c := 0; c < captureCh; c++ {
		rng := lcg(noiseSeed ^ (uint32(c) * 0x85EBCA6B))
		buf := make([]float32, n)
		render := renderCh[c%len(renderCh)]
		for i := 0; i < n; i++ {
			delaySamples := delaySamplesConst
			if delayAt != nil {
				delaySamples = delayAt(i)
			}
			gain := gainConst
			if gainAt != nil {
				gain = gainAt(i)
			}
			var echo float32
			srcIdx := i - delaySamples
			if srcIdx >= 0 && srcIdx < len(render) {
				echo = gain * render[srcIdx]
			}
			buf[i] = echo + rng.next()*noiseAmplitude
		}
		capture[c] = buf
	}
	return capture
}

func bulkDelayScenario(rate, renderCh, captureCh int, seed uint32) scenario {
	frameLen := rate / 100
	n := numFramesDefault * frameLen
	render := makeMultiChannelNoise(renderCh, numFramesDefault, frameLen, seed, renderAmplitude)
	delaySamples := (rate * 50) / 1000 // 50ms, well inside the default filter length
	capture := applyEcho(render, captureCh, n, delaySamples, 0.5, nil, nil, seed^0xABCD)
	return scenario{
		name:            "bulk_delay_50ms",
		sampleRate:      rate,
		renderChannels:  renderCh,
		captureChannels: captureCh,
		numFrames:       numFramesDefault,
		render:          render,
		capture:         capture,
	}
}

func delayChangeScenario(rate, renderCh, captureCh int) scenario {
	frameLen := rate / 100
	numFrames := numFramesDefault
	n := numFrames * frameLen
	render := makeMultiChannelNoise(renderCh, numFrames, frameLen, 0xD0D0, renderAmplitude)
	delayBefore := (rate * 20) / 1000 // 20ms
	delayAfter := (rate * 150) / 1000 // 150ms, e.g. a device switch mid-call
	switchSample := n / 2
	delayAt := func(i int) int {
		if i < switchSample {
			return delayBefore
		}
		return delayAfter
	}
	capture := applyEcho(render, captureCh, n, 0, 0.5, delayAt, nil, 0xD0D0^0x1234)
	return scenario{
		name:            "delay_change_midstream",
		sampleRate:      rate,
		renderChannels:  renderCh,
		captureChannels: captureCh,
		numFrames:       numFrames,
		render:          render,
		capture:         capture,
	}
}

func clockDriftScenario(rate, renderCh, captureCh int) scenario {
	frameLen := rate / 100
	numFrames := numFramesDefault
	n := numFrames * frameLen
	render := makeMultiChannelNoise(renderCh, numFrames, frameLen, 0xC10CC, renderAmplitude)
	baseDelay := (rate * 40) / 1000 // 40ms
	// A slow, monotonic delay drift: +1 sample of delay every 2000
	// samples (~0.05% clock-rate mismatch, well within the "drifting
	// crystal oscillators on independent devices" range the package
	// doc comment describes) -- simulates render and capture streams
	// running on independently-clocked hardware.
	delayAt := func(i int) int {
		return baseDelay + i/2000
	}
	capture := applyEcho(render, captureCh, n, 0, 0.5, delayAt, nil, 0xC10CC^0x5678)
	return scenario{
		name:            "clock_drift",
		sampleRate:      rate,
		renderChannels:  renderCh,
		captureChannels: captureCh,
		numFrames:       numFrames,
		render:          render,
		capture:         capture,
	}
}

func doubleTalkScenario(rate, renderCh, captureCh int) scenario {
	frameLen := rate / 100
	numFrames := numFramesDefault
	n := numFrames * frameLen
	render := makeMultiChannelNoise(renderCh, numFrames, frameLen, 0xDA1C, renderAmplitude)
	delaySamples := (rate * 30) / 1000 // 30ms
	capture := applyEcho(render, captureCh, n, delaySamples, 0.5, nil, nil, 0xDA1C^0x9999)

	// Independent near-end speech-like content, active for the middle
	// third of the stream: both ends "talking" simultaneously.
	start := n / 3
	end := 2 * n / 3
	for c := 0; c < captureCh; c++ {
		rng := lcg(0xDA1C ^ (uint32(c) * 0x2545F491))
		for i := start; i < end; i++ {
			capture[c][i] += rng.next() * nearEndAmplitude
		}
	}
	return scenario{
		name:            "double_talk",
		sampleRate:      rate,
		renderChannels:  renderCh,
		captureChannels: captureCh,
		numFrames:       numFrames,
		render:          render,
		capture:         capture,
	}
}

func saturationScenario(rate, renderCh, captureCh int) scenario {
	frameLen := rate / 100
	numFrames := numFramesDefault
	n := numFrames * frameLen
	render := makeMultiChannelNoise(renderCh, numFrames, frameLen, 0x5A70, renderAmplitude)
	delaySamples := (rate * 25) / 1000
	capture := applyEcho(render, captureCh, n, delaySamples, 0.5, nil, nil, 0x5A70^0x1357)

	// A saturation burst (clipped at the int16-equivalent extremes,
	// >= the 32700 threshold AnalyzeCapture's detectSaturation uses)
	// covering one tenth of the stream.
	start := n / 2
	end := start + n/10
	for c := 0; c < captureCh; c++ {
		for i := start; i < end; i++ {
			if capture[c][i] >= 0 {
				capture[c][i] = 32760
			} else {
				capture[c][i] = -32760
			}
		}
	}
	return scenario{
		name:            "saturation_burst",
		sampleRate:      rate,
		renderChannels:  renderCh,
		captureChannels: captureCh,
		numFrames:       numFrames,
		render:          render,
		capture:         capture,
	}
}

func silenceWindowScenario(rate, renderCh, captureCh int) scenario {
	frameLen := rate / 100
	numFrames := numFramesDefault
	n := numFrames * frameLen
	render := makeMultiChannelNoise(renderCh, numFrames, frameLen, 0x51EE, renderAmplitude)
	delaySamples := (rate * 35) / 1000
	capture := applyEcho(render, captureCh, n, delaySamples, 0.5, nil, nil, 0x51EE^0x8642)

	// A silence window (both render and capture zeroed) covering the
	// middle tenth of the stream, then resuming: proves both engines
	// handle -- and bit-exactly agree through -- a stream that drops
	// to true silence and comes back, with no NaN/instability.
	start := n / 2
	end := start + n/10
	for c := 0; c < renderCh; c++ {
		for i := start; i < end; i++ {
			render[c][i] = 0
		}
	}
	for c := 0; c < captureCh; c++ {
		for i := start; i < end; i++ {
			capture[c][i] = 0
		}
	}
	return scenario{
		name:            "silence_window",
		sampleRate:      rate,
		renderChannels:  renderCh,
		captureChannels: captureCh,
		numFrames:       numFrames,
		render:          render,
		capture:         capture,
	}
}

func gainChangeScenario(rate, renderCh, captureCh int) scenario {
	frameLen := rate / 100
	numFrames := numFramesDefault
	n := numFrames * frameLen
	render := makeMultiChannelNoise(renderCh, numFrames, frameLen, 0x6A16, renderAmplitude)
	delaySamples := (rate * 45) / 1000
	changeSample := n / 2
	gainAt := func(i int) float32 {
		if i < changeSample {
			return 0.3
		}
		return 0.8 // an abrupt capture-side gain step (e.g. an AGC step)
	}
	// delayAt intentionally nil: only gain steps, matching this
	// scenario's intent (a constant acoustic delay, stepped echo gain).
	capture := applyEcho(render, captureCh, n, delaySamples, 0, nil, gainAt, 0x6A16^0x7531)

	changeFrame := changeSample / frameLen
	return scenario{
		name:            "gain_change",
		sampleRate:      rate,
		renderChannels:  renderCh,
		captureChannels: captureCh,
		numFrames:       numFrames,
		render:          render,
		capture:         capture,
		levelChangeAt:   map[int]bool{changeFrame: true},
	}
}

// delayHintScenario exercises SetAudioBufferDelay (Part 4's
// Canceller.SetAudioBufferDelay forwards to exactly this call): an
// external delay estimate matching the true acoustic delay is
// supplied partway through an otherwise-ordinary bulk-delay scenario.
func delayHintScenario(rate, renderCh, captureCh int) scenario {
	frameLen := rate / 100
	numFrames := numFramesDefault
	n := numFrames * frameLen
	render := makeMultiChannelNoise(renderCh, numFrames, frameLen, 0xDE1A, renderAmplitude)
	delayMs := 60
	delaySamples := (rate * delayMs) / 1000
	capture := applyEcho(render, captureCh, n, delaySamples, 0.5, nil, nil, 0xDE1A^0x2468)

	hintFrame := numFrames / 4
	return scenario{
		name:            "external_delay_hint",
		sampleRate:      rate,
		renderChannels:  renderCh,
		captureChannels: captureCh,
		numFrames:       numFrames,
		render:          render,
		capture:         capture,
		setDelayAt:      map[int]int{hintFrame: delayMs},
	}
}

func interleave(chanMajor [][]float32, channels, frameIdx, frameLen int, dst []float32) {
	base := frameIdx * frameLen
	for ch := 0; ch < channels; ch++ {
		src := chanMajor[ch][base : base+frameLen]
		for i, v := range src {
			dst[i*channels+ch] = v
		}
	}
}

func runScenario(t *testing.T, sc scenario) {
	t.Helper()
	frameLen := sc.sampleRate / 100

	goEC := aec3.NewEchoCanceller3(config.DefaultConfig(), sc.sampleRate, sc.renderChannels, sc.captureChannels)
	oracle, err := newOracleCanceller(sc.sampleRate, sc.renderChannels, sc.captureChannels)
	if err != nil {
		t.Fatalf("newOracleCanceller: %v", err)
	}
	defer oracle.close()

	goRenderFrame := make([][]float32, sc.renderChannels)
	for ch := range goRenderFrame {
		goRenderFrame[ch] = make([]float32, frameLen)
	}
	goCaptureFrame := make([][]float32, sc.captureChannels)
	for ch := range goCaptureFrame {
		goCaptureFrame[ch] = make([]float32, frameLen)
	}

	oracleRenderInterleaved := make([]float32, sc.renderChannels*frameLen)
	oracleCaptureInterleaved := make([]float32, sc.captureChannels*frameLen)

	for f := 0; f < sc.numFrames; f++ {
		if delayMs, ok := sc.setDelayAt[f]; ok {
			goEC.SetAudioBufferDelay(delayMs)
			oracle.setAudioBufferDelay(delayMs)
		}

		for ch := 0; ch < sc.renderChannels; ch++ {
			copy(goRenderFrame[ch], sc.render[ch][f*frameLen:(f+1)*frameLen])
		}
		interleave(sc.render, sc.renderChannels, f, frameLen, oracleRenderInterleaved)

		goEC.AnalyzeRender(goRenderFrame)
		oracle.analyzeRender(oracleRenderInterleaved, frameLen)

		for ch := 0; ch < sc.captureChannels; ch++ {
			copy(goCaptureFrame[ch], sc.capture[ch][f*frameLen:(f+1)*frameLen])
		}
		interleave(sc.capture, sc.captureChannels, f, frameLen, oracleCaptureInterleaved)

		goEC.AnalyzeCapture(goCaptureFrame)
		oracle.analyzeCapture(oracleCaptureInterleaved, frameLen)

		levelChange := sc.levelChangeAt[f]
		goEC.ProcessCapture(goCaptureFrame, levelChange)
		oracle.processCapture(oracleCaptureInterleaved, frameLen, levelChange)

		for ch := 0; ch < sc.captureChannels; ch++ {
			for i := 0; i < frameLen; i++ {
				got := goCaptureFrame[ch][i]
				want := oracleCaptureInterleaved[i*sc.captureChannels+ch]
				if got != want {
					t.Fatalf("%s: frame %d channel %d sample %d mismatch: go %v, oracle %v", sc.name, f, ch, i, got, want)
				}
			}
		}

		goMetrics := goEC.GetMetrics()
		oracleMetrics := oracle.getMetrics()
		if goMetrics.EchoReturnLoss != oracleMetrics.EchoReturnLoss {
			t.Fatalf("%s: frame %d EchoReturnLoss mismatch: go %v, oracle %v", sc.name, f, goMetrics.EchoReturnLoss, oracleMetrics.EchoReturnLoss)
		}
		if goMetrics.EchoReturnLossEnhancement != oracleMetrics.EchoReturnLossEnhancement {
			t.Fatalf("%s: frame %d EchoReturnLossEnhancement mismatch: go %v, oracle %v", sc.name, f, goMetrics.EchoReturnLossEnhancement, oracleMetrics.EchoReturnLossEnhancement)
		}
		if goMetrics.DelayMs != oracleMetrics.DelayMs {
			t.Fatalf("%s: frame %d DelayMs mismatch: go %v, oracle %v", sc.name, f, goMetrics.DelayMs, oracleMetrics.DelayMs)
		}

		// Clockdrift has no oracle counterpart at this level (see this
		// file's doc comment) -- just a validity sanity check.
		if cd := goEC.Clockdrift(); cd < aec3.ClockdriftLevelNone || cd > aec3.ClockdriftLevelVerified {
			t.Fatalf("%s: frame %d Clockdrift() returned an invalid value: %v", sc.name, f, cd)
		}
	}
}
