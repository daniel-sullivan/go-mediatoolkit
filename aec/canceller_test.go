package aec

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Canceller must satisfy mutations.Processor (see the Canceller type
// doc comment).
var _ mutations.Processor = (*Canceller)(nil)

func TestNewCanceller_Validation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     CancellerConfig
		wantErr error
	}{
		{"16k mono", CancellerConfig{SampleRate: 16000, CaptureChannels: 1, RenderChannels: 1}, nil},
		{"32k stereo", CancellerConfig{SampleRate: 32000, CaptureChannels: 2, RenderChannels: 2}, nil},
		{"48k mixed channels", CancellerConfig{SampleRate: 48000, CaptureChannels: 1, RenderChannels: 2}, nil},
		{"max channels", CancellerConfig{SampleRate: 16000, CaptureChannels: maxChannels, RenderChannels: maxChannels}, nil},
		{"unsupported rate 44100", CancellerConfig{SampleRate: 44100, CaptureChannels: 1, RenderChannels: 1}, ErrBadArg},
		{"unsupported rate 8000", CancellerConfig{SampleRate: 8000, CaptureChannels: 1, RenderChannels: 1}, ErrBadArg},
		{"zero rate", CancellerConfig{SampleRate: 0, CaptureChannels: 1, RenderChannels: 1}, ErrBadArg},
		{"zero capture channels", CancellerConfig{SampleRate: 16000, CaptureChannels: 0, RenderChannels: 1}, ErrBadArg},
		{"negative capture channels", CancellerConfig{SampleRate: 16000, CaptureChannels: -1, RenderChannels: 1}, ErrBadArg},
		{"too many capture channels", CancellerConfig{SampleRate: 16000, CaptureChannels: maxChannels + 1, RenderChannels: 1}, ErrBadArg},
		{"zero render channels", CancellerConfig{SampleRate: 16000, CaptureChannels: 1, RenderChannels: 0}, ErrBadArg},
		{"too many render channels", CancellerConfig{SampleRate: 16000, CaptureChannels: 1, RenderChannels: maxChannels + 1}, ErrBadArg},
		{"always-engaged suppressor", CancellerConfig{SampleRate: 16000, CaptureChannels: 1, RenderChannels: 1, SuppressorGating: SuppressorGatingAlwaysEngaged}, nil},
		{"unknown suppressor gating", CancellerConfig{SampleRate: 16000, CaptureChannels: 1, RenderChannels: 1, SuppressorGating: SuppressorGatingAlwaysEngaged + 1}, ErrBadArg},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCanceller(tc.cfg)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("NewCanceller(%+v) error = %v, want %v", tc.cfg, err, tc.wantErr)
				}
				if c != nil {
					t.Fatalf("NewCanceller(%+v) returned non-nil Canceller alongside error", tc.cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewCanceller(%+v) unexpected error: %v", tc.cfg, err)
			}
			if c == nil {
				t.Fatalf("NewCanceller(%+v) returned nil Canceller with no error", tc.cfg)
			}
		})
	}
}

func TestFeedFarEnd_Misaligned(t *testing.T) {
	c, err := NewCanceller(CancellerConfig{SampleRate: 16000, CaptureChannels: 1, RenderChannels: 2})
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}

	if err := c.FeedFarEnd(make([]float64, 3)); !errors.Is(err, ErrBadArg) {
		t.Fatalf("FeedFarEnd(3 samples, 2 channels) error = %v, want ErrBadArg", err)
	}
	if err := c.FeedFarEnd(make([]float64, 4)); err != nil {
		t.Fatalf("FeedFarEnd(4 samples, 2 channels): unexpected error: %v", err)
	}
	// A zero-length call is a degenerate but aligned (0 % channels == 0)
	// call: it must not error.
	if err := c.FeedFarEnd(nil); err != nil {
		t.Fatalf("FeedFarEnd(nil): unexpected error: %v", err)
	}
}

// TestProcess_AlignedPrefix verifies Process's aligned-prefix behavior
// (see the Process doc comment and loudness.Monitor.Process's
// precedent): a trailing partial frame — samples[aligned:] where
// aligned := len(samples) - len(samples)%CaptureChannels — must be left
// completely untouched.
func TestProcess_AlignedPrefix(t *testing.T) {
	c, err := NewCanceller(CancellerConfig{SampleRate: 16000, CaptureChannels: 2, RenderChannels: 2})
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}

	const sentinel = 123456.0 // far outside any real audio value
	samples := []float64{0, 0, 0, 0, 0, 0, sentinel}
	c.Process(samples)
	if samples[6] != sentinel {
		t.Fatalf("trailing unaligned sample was touched: got %v, want untouched sentinel %v", samples[6], sentinel)
	}

	// aligned <= 0: a single sample with 2 configured channels never
	// forms a whole frame, so Process must return immediately, again
	// leaving the buffer untouched.
	single := []float64{sentinel}
	c.Process(single)
	if single[0] != sentinel {
		t.Fatalf("Process touched a buffer shorter than one channel-frame: got %v, want untouched sentinel %v", single[0], sentinel)
	}
}

// TestLatency verifies Latency() is fixed at exactly one 10ms AEC3
// frame, independent of SampleRate (see Latency's doc comment for the
// derivation).
func TestLatency(t *testing.T) {
	for _, rate := range []int{16000, 32000, 48000} {
		c, err := NewCanceller(CancellerConfig{SampleRate: rate, CaptureChannels: 1, RenderChannels: 1})
		if err != nil {
			t.Fatalf("NewCanceller(rate=%d): %v", rate, err)
		}
		if got, want := c.Latency(), 10*time.Millisecond; got != want {
			t.Fatalf("rate=%d: Latency() = %v, want %v", rate, got, want)
		}
	}
}

// TestReset_MatchesFreshInstance drives one Canceller through an
// arbitrary "dirty" stream, resets it, then drives it and a second,
// freshly constructed Canceller through an IDENTICAL subsequent
// stream: per Canceller.Reset's doc comment, the two must produce
// bit-identical processed output and identical Metrics.
func TestReset_MatchesFreshInstance(t *testing.T) {
	cfg := CancellerConfig{SampleRate: 16000, CaptureChannels: 1, RenderChannels: 1}

	a, err := NewCanceller(cfg)
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}

	// Dirty `a` with a prior stream, some SetAudioBufferDelay calls, and
	// a few oddly-sized Process calls to exercise the internal
	// accumulators/delay-line before reset.
	dirtyRender := make([]float64, 5000)
	generators.SineInto(dirtyRender, 300, cfg.SampleRate)
	dirtyCapture := make([]float64, 5000)
	generators.SineInto(dirtyCapture, 300, cfg.SampleRate)
	for i := range dirtyCapture {
		dirtyCapture[i] *= 0.3
	}
	for i := 0; i < len(dirtyRender); i += 77 {
		end := i + 77
		if end > len(dirtyRender) {
			end = len(dirtyRender)
		}
		if err := a.FeedFarEnd(dirtyRender[i:end]); err != nil {
			t.Fatalf("dirtying FeedFarEnd: %v", err)
		}
		a.Process(dirtyCapture[i:end])
	}
	a.SetAudioBufferDelay(13 * time.Millisecond)
	a.Process(dirtyCapture[:160])

	a.Reset()

	b, err := NewCanceller(cfg)
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}

	// Now drive both `a` (post-reset) and `b` (fresh) through an
	// identical stream and compare bit-for-bit.
	render := make([]float64, 8000)
	generators.SineInto(render, 440, cfg.SampleRate)
	captureBase := make([]float64, 8000)
	generators.SineInto(captureBase, 440, cfg.SampleRate)
	for i := range captureBase {
		captureBase[i] *= 0.4
	}
	captureA := append([]float64(nil), captureBase...)
	captureB := append([]float64(nil), captureBase...)

	if err := a.FeedFarEnd(render); err != nil {
		t.Fatalf("a.FeedFarEnd: %v", err)
	}
	a.Process(captureA)

	if err := b.FeedFarEnd(render); err != nil {
		t.Fatalf("b.FeedFarEnd: %v", err)
	}
	b.Process(captureB)

	if !reflect.DeepEqual(captureA, captureB) {
		t.Fatalf("post-reset output does not match a fresh instance's output")
	}
	if ma, mb := a.Metrics(), b.Metrics(); ma != mb {
		t.Fatalf("post-reset Metrics = %+v, want %+v (fresh instance)", ma, mb)
	}
}

// rmsDB returns the RMS level of samples in dBFS (20*log10(rms)),
// or a large negative sentinel for a silent (all-zero) buffer.
func rmsDB(samples []float64) float64 {
	if len(samples) == 0 {
		return math.Inf(-1)
	}
	var sum float64
	for _, v := range samples {
		sum += v * v
	}
	rms := math.Sqrt(sum / float64(len(samples)))
	if rms <= 0 {
		return -300
	}
	return 20 * math.Log10(rms)
}

// TestCanceller_EchoReduction drives a synthetic, purely linear echo
// path (a single attenuated, fixed-delay copy of the render signal,
// plus a small near-end noise floor standing in for whatever the near
// end "actually" says) through a Canceller and checks that the
// residual echo, measured after AEC3 has had several seconds to
// converge, is dramatically reduced relative to the original.
//
// The render stimulus is white noise, not a pure tone. A pure
// single-frequency sine is a pathological choice here: both AEC3's
// cross-correlation delay estimator and its adaptive FIR filter
// identify the echo path from statistics of the render signal alone,
// and a tone of period T makes any delay ambiguous with that same
// delay plus or minus a whole number of periods (autocorrelation is
// just as high at every such shift), while simultaneously exciting
// only a single frequency bin — leaving the filter's estimate at every
// other frequency undetermined. (Measured directly: swapping this
// test's stimulus for a 350Hz tone converged to a bogus ~200ms delay
// estimate and reduced the echo by under 10dB against a true 20ms
// delay — a test-harness artifact, not a Canceller defect; broadband
// noise on the identical code path converges to a delay estimate
// within a couple of ms of the truth and a >25dB reduction.) White
// noise, like real speech/music, is broadband and non-periodic, so it
// persistently excites every frequency and makes every candidate delay
// distinguishable — the standard stimulus choice for testing acoustic
// echo cancellers and delay estimators.
//
// Threshold rationale: the synthetic echo path (20ms delay, 0.5 linear
// gain) sits well inside AEC3's default adaptive filter length (far
// exceeding a 20ms delay) and has no nonlinearity or time-variance for
// the filter to fail to model, so after several seconds of convergence
// the adaptive filter alone should remove the large majority of it,
// leaving the signal dominated by the (uncancellable, because it is
// uncorrelated with the render reference) near-end noise floor. The
// noise floor sits at -60 dBFS (amplitude 0.001) against a
// full-strength original echo at roughly -21 dBFS (0.3 render
// amplitude, 0.5 gain), a substantial gap; a 20 dB reduction threshold
// is conservative relative to the ~26 dB this scenario measures in
// practice, leaving headroom for run-to-run convergence jitter while
// still being a meaningful, failure-sensitive bound.
func TestCanceller_EchoReduction(t *testing.T) {
	const sampleRate = 16000
	const delayFrames = 320 // 20ms
	const echoGain = 0.5
	const renderAmplitude = 0.3
	const noiseAmplitude = 0.001
	const duration = 6 * time.Second

	cfg := CancellerConfig{SampleRate: sampleRate, CaptureChannels: 1, RenderChannels: 1}
	c, err := NewCanceller(cfg)
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}

	n := int(duration.Seconds() * sampleRate)
	rng := rand.New(rand.NewSource(1))
	render := make([]float64, n)
	for i := range render {
		render[i] = renderAmplitude * (2*rng.Float64() - 1)
	}

	capture := make([]float64, n)
	captureOrig := make([]float64, n)
	for i := 0; i < n; i++ {
		var echo float64
		if i >= delayFrames {
			echo = echoGain * render[i-delayFrames]
		}
		noise := noiseAmplitude * (2*rng.Float64() - 1)
		capture[i] = echo + noise
	}
	copy(captureOrig, capture)

	const chunk = sampleRate / 100 // one 10ms AEC3 frame
	for i := 0; i+chunk <= n; i += chunk {
		if err := c.FeedFarEnd(render[i : i+chunk]); err != nil {
			t.Fatalf("FeedFarEnd: %v", err)
		}
		c.Process(capture[i : i+chunk])
	}

	// Compare the last 1 second of the ORIGINAL (uncancelled) signal
	// against the last 1 second of the PROCESSED signal. Process's
	// output is delayed by exactly Latency() (one chunk, given
	// chunk==one AEC3 frame here), but both windows are a full second
	// long and taken from the converged tail, so the 10ms offset is
	// immaterial to the RMS comparison.
	tail := sampleRate // last 1s
	beforeDB := rmsDB(captureOrig[n-tail:])
	afterDB := rmsDB(capture[n-tail:])
	reduction := beforeDB - afterDB

	t.Logf("echo reduction: before=%.1f dBFS after=%.1f dBFS reduction=%.1f dB", beforeDB, afterDB, reduction)
	t.Logf("metrics after convergence: %+v", c.Metrics())

	const minReductionDB = 20.0
	if reduction < minReductionDB {
		t.Fatalf("echo reduction = %.1f dB, want >= %.1f dB (before=%.1f after=%.1f dBFS)", reduction, minReductionDB, beforeDB, afterDB)
	}
}

// TestCanceller_MetricsConvergence checks that, on the same kind of
// synthetic echo scenario as TestCanceller_EchoReduction (see that
// test's doc comment for why the stimulus must be broadband noise, not
// a tone), GetMetrics reports a converged delay estimate close to the
// true 20ms path delay after several seconds of convergence.
func TestCanceller_MetricsConvergence(t *testing.T) {
	const sampleRate = 16000
	const delayFrames = 320 // 20ms
	const echoGain = 0.5
	const renderAmplitude = 0.3
	const duration = 6 * time.Second

	cfg := CancellerConfig{SampleRate: sampleRate, CaptureChannels: 1, RenderChannels: 1}
	c, err := NewCanceller(cfg)
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}

	n := int(duration.Seconds() * sampleRate)
	rng := rand.New(rand.NewSource(42))
	render := make([]float64, n)
	for i := range render {
		render[i] = renderAmplitude * (2*rng.Float64() - 1)
	}
	capture := make([]float64, n)
	for i := 0; i < n; i++ {
		if i >= delayFrames {
			capture[i] = echoGain * render[i-delayFrames]
		}
	}

	const chunk = sampleRate / 100
	for i := 0; i+chunk <= n; i += chunk {
		if err := c.FeedFarEnd(render[i : i+chunk]); err != nil {
			t.Fatalf("FeedFarEnd: %v", err)
		}
		c.Process(capture[i : i+chunk])
	}

	m := c.Metrics()
	t.Logf("converged metrics: %+v", m)

	const wantDelayMs = 20
	const delayToleranceMs = 10
	if diff := m.DelayMs - wantDelayMs; diff < -delayToleranceMs || diff > delayToleranceMs {
		t.Errorf("DelayMs = %d, want within %dms of %dms", m.DelayMs, delayToleranceMs, wantDelayMs)
	}
	if m.EchoReturnLossEnhancement <= 0 {
		t.Errorf("EchoReturnLossEnhancement = %v, want > 0 (suppressor should show some measured improvement once converged)", m.EchoReturnLossEnhancement)
	}
}

// TestCanceller_ConcurrentMetricsAndDelay exercises exactly the
// documented-safe concurrent surface: one goroutine owns FeedFarEnd and
// Process (serialized on itself), while a second goroutine repeatedly
// calls Metrics() and SetAudioBufferDelay() concurrently with the
// first. This must NOT be read as evidence that FeedFarEnd/Process
// themselves are safe to call concurrently from multiple goroutines —
// they are not (see the Canceller type doc comment) — this test
// deliberately never does that.
func TestCanceller_ConcurrentMetricsAndDelay(t *testing.T) {
	cfg := CancellerConfig{SampleRate: 16000, CaptureChannels: 1, RenderChannels: 1}
	c, err := NewCanceller(cfg)
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}

	const n = 16000 * 2
	render := make([]float64, n)
	generators.SineInto(render, 400, cfg.SampleRate)
	capture := make([]float64, n)
	for i := range capture {
		capture[i] = render[i] * 0.2
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		d := time.Duration(0)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = c.Metrics()
			c.SetAudioBufferDelay(d)
			d += time.Millisecond
			if d > 50*time.Millisecond {
				d = 0
			}
		}
	}()

	const chunk = 160
	for i := 0; i+chunk <= n; i += chunk {
		if err := c.FeedFarEnd(render[i : i+chunk]); err != nil {
			t.Fatalf("FeedFarEnd: %v", err)
		}
		c.Process(capture[i : i+chunk])
	}
	close(stop)
	wg.Wait()
}

// TestFeedFarEnd_RenderQueueOverflow checks the pacing contract
// documented on the Canceller type (Pacing section) and FeedFarEnd:
// feeding more than ~1s (RenderTransferQueueSizeFrames == 100 10ms
// frames) of render ahead of any Process call must silently drop the
// excess, with the drop count surfaced via
// Metrics().RenderFramesDropped, while normally paced feeding (one
// FeedFarEnd call per Process call) must never drop anything.
func TestFeedFarEnd_RenderQueueOverflow(t *testing.T) {
	const sampleRate = 16000
	const chunk = sampleRate / 100 // one 10ms AEC3 frame

	t.Run("overfeed", func(t *testing.T) {
		c, err := NewCanceller(CancellerConfig{SampleRate: sampleRate, CaptureChannels: 1, RenderChannels: 1})
		if err != nil {
			t.Fatalf("NewCanceller: %v", err)
		}

		if got := c.Metrics().RenderFramesDropped; got != 0 {
			t.Fatalf("RenderFramesDropped before feeding anything = %d, want 0", got)
		}

		const framesFed = 150 // > 100-frame (RenderTransferQueueSizeFrames) cap
		frame := make([]float64, chunk)
		for i := 0; i < framesFed; i++ {
			if err := c.FeedFarEnd(frame); err != nil {
				t.Fatalf("FeedFarEnd: %v", err)
			}
		}

		const wantDropped = framesFed - 100
		if got := c.Metrics().RenderFramesDropped; got != wantDropped {
			t.Fatalf("RenderFramesDropped after overfeeding %d frames = %d, want %d", framesFed, got, wantDropped)
		}
	})

	t.Run("normal pacing", func(t *testing.T) {
		c, err := NewCanceller(CancellerConfig{SampleRate: sampleRate, CaptureChannels: 1, RenderChannels: 1})
		if err != nil {
			t.Fatalf("NewCanceller: %v", err)
		}

		render := make([]float64, chunk)
		capture := make([]float64, chunk)
		for i := 0; i < 300; i++ { // several multiples of the 100-frame cap
			if err := c.FeedFarEnd(render); err != nil {
				t.Fatalf("FeedFarEnd: %v", err)
			}
			c.Process(capture)
		}

		if got := c.Metrics().RenderFramesDropped; got != 0 {
			t.Fatalf("RenderFramesDropped under normal pacing = %d, want 0", got)
		}
	})
}

// TestCanceller_NonFiniteInputClamped checks the clamp-and-map-non-
// finite guard documented on FeedFarEnd/Process (deinterleaveToFloatS16):
// NaN, +/-Inf, and merely out-of-[-1,1]-range samples fed to either
// stream must never leave the engine's internal state or Process's
// output non-finite, and — critically — must not permanently wedge the
// canceller: clean audio fed afterward must still converge and cancel
// echo normally, proving the engine recovered rather than latching a
// NaN/Inf into some persistent filter state.
func TestCanceller_NonFiniteInputClamped(t *testing.T) {
	const sampleRate = 16000
	const delayFrames = 320 // 20ms
	const echoGain = 0.5
	const renderAmplitude = 0.3
	const chunk = sampleRate / 100

	cfg := CancellerConfig{SampleRate: sampleRate, CaptureChannels: 1, RenderChannels: 1}
	c, err := NewCanceller(cfg)
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}

	// Hostile burst: NaN, +Inf, -Inf, and out-of-range +/-2.0 samples,
	// on BOTH the render and capture paths, for several frames.
	hostileValues := []float64{math.NaN(), math.Inf(1), math.Inf(-1), 2.0, -2.0, 0}
	for burst := 0; burst < 20; burst++ {
		render := make([]float64, chunk)
		capture := make([]float64, chunk)
		for i := range render {
			render[i] = hostileValues[i%len(hostileValues)]
			capture[i] = hostileValues[(i+3)%len(hostileValues)]
		}
		if err := c.FeedFarEnd(render); err != nil {
			t.Fatalf("FeedFarEnd: %v", err)
		}
		c.Process(capture)

		for i, v := range capture {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("burst %d: Process output[%d] = %v, want finite", burst, i, v)
			}
		}
	}
	if m := c.Metrics(); math.IsNaN(m.EchoReturnLoss) || math.IsInf(m.EchoReturnLoss, 0) ||
		math.IsNaN(m.EchoReturnLossEnhancement) || math.IsInf(m.EchoReturnLossEnhancement, 0) {
		t.Fatalf("Metrics after hostile input is non-finite: %+v", m)
	}

	// Recovery: drive the same clean synthetic echo scenario
	// TestCanceller_EchoReduction uses, on the SAME (already hostile-
	// input-exposed) Canceller, and require it still converges and
	// cancels echo -- proving the hostile burst above didn't leave any
	// persistent NaN/Inf lodged in the engine's adaptive filter state.
	const duration = 6 * time.Second
	n := int(duration.Seconds() * sampleRate)
	rng := rand.New(rand.NewSource(2))
	render := make([]float64, n)
	for i := range render {
		render[i] = renderAmplitude * (2*rng.Float64() - 1)
	}
	capture := make([]float64, n)
	captureOrig := make([]float64, n)
	for i := 0; i < n; i++ {
		var echo float64
		if i >= delayFrames {
			echo = echoGain * render[i-delayFrames]
		}
		capture[i] = echo
	}
	copy(captureOrig, capture)

	for i := 0; i+chunk <= n; i += chunk {
		if err := c.FeedFarEnd(render[i : i+chunk]); err != nil {
			t.Fatalf("FeedFarEnd: %v", err)
		}
		c.Process(capture[i : i+chunk])
	}

	tail := sampleRate
	beforeDB := rmsDB(captureOrig[n-tail:])
	afterDB := rmsDB(capture[n-tail:])
	reduction := beforeDB - afterDB
	t.Logf("post-recovery echo reduction: before=%.1f dBFS after=%.1f dBFS reduction=%.1f dB", beforeDB, afterDB, reduction)

	const minReductionDB = 20.0
	if reduction < minReductionDB {
		t.Fatalf("post-recovery echo reduction = %.1f dB, want >= %.1f dB: hostile input left the canceller degraded", reduction, minReductionDB)
	}
	for i, v := range capture {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("recovery output[%d] = %v, want finite", i, v)
		}
	}
}

// TestSetAudioBufferDelay_ExtremeValues checks the documented tolerant
// handling of out-of-range delays (SetAudioBufferDelay's doc comment):
// a negative or unreasonably large delay must not panic, and the
// canceller must keep operating normally afterward.
func TestSetAudioBufferDelay_ExtremeValues(t *testing.T) {
	c, err := NewCanceller(CancellerConfig{SampleRate: 16000, CaptureChannels: 1, RenderChannels: 1})
	if err != nil {
		t.Fatalf("NewCanceller: %v", err)
	}

	extreme := []time.Duration{
		-time.Hour,
		-time.Millisecond,
		0,
		time.Hour,
		time.Duration(math.MaxInt64),
	}
	for _, d := range extreme {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("SetAudioBufferDelay(%v) panicked: %v", d, r)
				}
			}()
			c.SetAudioBufferDelay(d)
		}()

		// The canceller must keep processing normally after an extreme
		// delay hint: drive one frame through and require finite,
		// unpanicked output.
		render := make([]float64, 160)
		capture := make([]float64, 160)
		generators.SineInto(render, 400, 16000)
		copy(capture, render)
		if err := c.FeedFarEnd(render); err != nil {
			t.Fatalf("FeedFarEnd after SetAudioBufferDelay(%v): %v", d, err)
		}
		c.Process(capture)
		for i, v := range capture {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("after SetAudioBufferDelay(%v): Process output[%d] = %v, want finite", d, i, v)
			}
		}
	}
}

// TestClockdriftLevel_String is a light sanity check that the
// exported ClockdriftLevel constants are distinct, since Metrics
// equality/logging (see TestReset_MatchesFreshInstance,
// TestCanceller_MetricsConvergence) relies on them being plain,
// comparable int values.
func TestClockdriftLevel_Values(t *testing.T) {
	levels := []ClockdriftLevel{ClockdriftLevelNone, ClockdriftLevelProbable, ClockdriftLevelVerified}
	seen := map[ClockdriftLevel]bool{}
	for _, l := range levels {
		if seen[l] {
			t.Fatalf("duplicate ClockdriftLevel value: %v", l)
		}
		seen[l] = true
	}
}

func ExampleNewCanceller() {
	c, err := NewCanceller(CancellerConfig{SampleRate: 16000, CaptureChannels: 1, RenderChannels: 1})
	if err != nil {
		fmt.Println(err)
		return
	}
	render := make([]float64, 160)
	capture := make([]float64, 160)
	if err := c.FeedFarEnd(render); err != nil {
		fmt.Println(err)
		return
	}
	c.Process(capture)
	fmt.Println(c.Latency())
	// Output: 10ms
}
