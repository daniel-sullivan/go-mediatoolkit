package aec

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
)

// runEchoScenario drives a fixed, deterministic synthetic echo path
// (delayed/attenuated copy of a sine render signal, no randomness so
// two Cancellers built from different CancellerConfig values can be
// compared bit-for-bit or numerically) through c and returns the
// processed capture buffer plus the final Metrics snapshot.
func runEchoScenario(t *testing.T, c *Canceller, sampleRate int) ([]float64, Metrics) {
	t.Helper()
	const delayFrames = 320 // 20ms
	const echoGain = 0.5
	const duration = 2 * time.Second

	n := int(duration.Seconds() * float64(sampleRate))
	render := make([]float64, n)
	generators.SineInto(render, 300, sampleRate)
	capture := make([]float64, n)
	for i := 0; i < n; i++ {
		if i >= delayFrames {
			capture[i] = echoGain * render[i-delayFrames]
		}
	}

	const chunk = 160
	for i := 0; i+chunk <= n; i += chunk {
		if err := c.FeedFarEnd(render[i : i+chunk]); err != nil {
			t.Fatalf("FeedFarEnd: %v", err)
		}
		c.Process(capture[i : i+chunk])
	}
	return capture, c.Metrics()
}

// TestNewCanceller_NilTuningMatchesDefaultConfig checks
// CancellerConfig.Tuning's documented nil behaviour: nil must select
// config.DefaultConfig() exactly, so a Canceller built with a nil
// Tuning and one built with an explicit &config.DefaultConfig() copy
// produce bit-identical processed output and identical Metrics over
// the same input — the "no change for an existing caller" guarantee.
func TestNewCanceller_NilTuningMatchesDefaultConfig(t *testing.T) {
	const sampleRate = 16000
	cfgBase := CancellerConfig{SampleRate: sampleRate, CaptureChannels: 1, RenderChannels: 1}

	nilTuning, err := NewCanceller(cfgBase)
	if err != nil {
		t.Fatalf("NewCanceller(nil Tuning): %v", err)
	}

	explicit := config.DefaultConfig()
	cfgExplicit := cfgBase
	cfgExplicit.Tuning = &explicit
	explicitTuning, err := NewCanceller(cfgExplicit)
	if err != nil {
		t.Fatalf("NewCanceller(explicit DefaultConfig Tuning): %v", err)
	}

	outNil, metricsNil := runEchoScenario(t, nilTuning, sampleRate)
	outExplicit, metricsExplicit := runEchoScenario(t, explicitTuning, sampleRate)

	if !reflect.DeepEqual(outNil, outExplicit) {
		t.Fatalf("nil Tuning output differs from explicit config.DefaultConfig() Tuning output")
	}
	if metricsNil != metricsExplicit {
		t.Fatalf("nil Tuning Metrics = %+v, want %+v (explicit config.DefaultConfig() Tuning)", metricsNil, metricsExplicit)
	}
}

// TestNewCanceller_TuningChangesBehaviour checks that a non-default
// Tuning actually reaches the engine: drastically loosening the
// suppressor's normal-tuning transparency thresholds (still within
// config.Validate's valid 0..100 range, so this must NOT be rejected)
// must measurably change Process's output relative to
// config.DefaultConfig() on the identical input.
func TestNewCanceller_TuningChangesBehaviour(t *testing.T) {
	const sampleRate = 16000
	cfgBase := CancellerConfig{SampleRate: sampleRate, CaptureChannels: 1, RenderChannels: 1}

	def, err := NewCanceller(cfgBase)
	if err != nil {
		t.Fatalf("NewCanceller(default): %v", err)
	}

	tuned := config.DefaultConfig()
	// Loosen the suppressor's normal-tuning masking thresholds far past
	// their defaults (EnrTransparent .3/.07, EnrSuppress .4/.1) toward
	// the top of Validate's 0..100 range: a much more transparent
	// (less-suppressing) suppressor, which should leave visibly more
	// residual echo in the output than the default tuning.
	tuned.Suppressor.NormalTuning.MaskLF.EnrTransparent = 50
	tuned.Suppressor.NormalTuning.MaskLF.EnrSuppress = 60
	tuned.Suppressor.NormalTuning.MaskHF.EnrTransparent = 50
	tuned.Suppressor.NormalTuning.MaskHF.EnrSuppress = 60
	cfgTuned := cfgBase
	cfgTuned.Tuning = &tuned
	tunedCanceller, err := NewCanceller(cfgTuned)
	if err != nil {
		t.Fatalf("NewCanceller(tuned): %v", err)
	}

	outDefault, _ := runEchoScenario(t, def, sampleRate)
	outTuned, _ := runEchoScenario(t, tunedCanceller, sampleRate)

	if reflect.DeepEqual(outDefault, outTuned) {
		t.Fatalf("tuned Suppressor.NormalTuning produced identical output to config.DefaultConfig() — Tuning did not reach the engine")
	}

	tail := sampleRate / 2
	defaultDB := rmsDB(outDefault[len(outDefault)-tail:])
	tunedDB := rmsDB(outTuned[len(outTuned)-tail:])
	t.Logf("tail RMS: default=%.1f dBFS tuned=%.1f dBFS", defaultDB, tunedDB)
	const minDeltaDB = 1.0
	if delta := tunedDB - defaultDB; delta < minDeltaDB {
		t.Fatalf("tuned-vs-default tail RMS delta = %.2f dB, want >= %.1f dB (a measurable behaviour change)", delta, minDeltaDB)
	}
}

// TestNewCanceller_InvalidTuning checks that a Tuning value
// config.Validate would have to clamp is rejected with ErrBadArg
// (naming the offending field), rather than silently substituted with
// the clamped value — see CancellerConfig.Tuning's doc comment.
// Delay.DownSamplingFactor must be exactly 4 or 8 (config.Validate's
// very first check); 3 is neither.
func TestNewCanceller_InvalidTuning(t *testing.T) {
	tuning := config.DefaultConfig()
	tuning.Delay.DownSamplingFactor = 3

	_, err := NewCanceller(CancellerConfig{
		SampleRate: 16000, CaptureChannels: 1, RenderChannels: 1,
		Tuning: &tuning,
	})
	if !errors.Is(err, ErrBadArg) {
		t.Fatalf("NewCanceller(invalid Tuning) error = %v, want ErrBadArg", err)
	}
	if !strings.Contains(err.Error(), "DownSamplingFactor") {
		t.Fatalf("NewCanceller(invalid Tuning) error = %q, want it to name the offending field (DownSamplingFactor)", err.Error())
	}
}

// TestValidateTuning_ValidConfigPassesThrough checks that a Tuning
// value already inside every range config.Validate enforces (the
// unmodified config.DefaultConfig()) is never rejected.
func TestValidateTuning_ValidConfigPassesThrough(t *testing.T) {
	c := config.DefaultConfig()
	if err := validateTuning(&c); err != nil {
		t.Fatalf("validateTuning(DefaultConfig()) = %v, want nil", err)
	}
}
