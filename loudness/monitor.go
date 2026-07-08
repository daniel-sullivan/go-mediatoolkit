package loudness

import (
	"sync"
	"time"
)

// Monitor is a goroutine-safe, pass-through metering mutations.Processor
// wrapping a Meter, built for live buses — most concretely, a mixer's
// master chain (see mixer.Config.Processors): the mixer's Process runs on
// its dedicated mix goroutine while a UI or telemetry goroutine wants to
// read current loudness/peak figures concurrently. Meter itself is
// documented NOT safe for concurrent use; Monitor exists purely to add a
// mutex around one, so both sides can touch it safely.
//
// # Cost
//
// Process only feeds the wrapped Meter — it never mutates the audio it is
// given, so inserting a Monitor into a processor chain is metering-only:
// no gain change, no added latency, no silence-when-idle behaviour to
// account for (contrast with e.g. a Limiter, which is length-preserving
// but does introduce latency).
//
// # Long-running / 24/7 buses
//
// A Monitor never stops accumulating history on its own (a live bus has
// no natural "end of programme"): with ModeIntegrated or ModeLRA the
// default history is unbounded, matching Meter's own default. For a bus
// that runs continuously, either construct with ModeHistogram (bounded,
// fixed-size histogram accounting) or call SetMaxHistory /
// SetMaxWindow periodically — both are exposed here as mutex-guarded
// pass-throughs to the wrapped Meter for exactly this reason.
type Monitor struct {
	mu    sync.Mutex
	meter *Meter
}

// NewMonitor constructs a Monitor for sampleRate Hz, channels interleaved
// channels, and the measurements selected by mode. Validation is
// delegated entirely to NewMeter: it returns ErrBadSampleRate,
// ErrBadChannels, or ErrBadConfig under the same conditions NewMeter
// does.
func NewMonitor(sampleRate, channels int, mode Mode) (*Monitor, error) {
	m, err := NewMeter(sampleRate, channels, mode)
	if err != nil {
		return nil, err
	}
	return &Monitor{meter: m}, nil
}

// Process feeds samples into the wrapped Meter and leaves the audio
// itself completely untouched — Monitor is metering-only. It satisfies
// mutations.Processor, so it can be dropped straight into a
// mixer.Config.Processors chain or any other Processor chain.
//
// Process cannot return an error (the mutations.Processor interface
// doesn't allow one), but Meter.AddFrames can fail on a buffer that
// isn't a whole number of frames. Since a mixer (or any other well-behaved
// caller) always delivers whole frames, Process instead computes the
// aligned prefix of samples up front and meters only that: any trailing
// partial frame at the end of samples — which should never happen in
// practice — is silently dropped rather than panicking or corrupting the
// meter's state.
func (mon *Monitor) Process(samples []float64) {
	mon.mu.Lock()
	defer mon.mu.Unlock()

	aligned := len(samples) - len(samples)%mon.meter.Channels()
	if aligned <= 0 {
		return
	}
	// aligned is a multiple of the channel count by construction, so
	// AddFrames cannot return ErrUnalignedSamples here.
	_ = mon.meter.AddFrames(samples[:aligned])
}

// Reset clears the wrapped Meter's measured history, keeping its
// configuration (sample rate, channel count, mode, channel-map
// overrides, and any SetMaxWindow/SetMaxHistory settings). See
// Meter.Reset.
func (mon *Monitor) Reset() {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	mon.meter.Reset()
}

// SetChannel overrides the weighting role of channel i (0-based). See
// Meter.SetChannel for the returned errors.
func (mon *Monitor) SetChannel(i int, ch Channel) error {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.SetChannel(i, ch)
}

// SetMaxWindow sets the maximum trailing window LoudnessWindow may query.
// See Meter.SetMaxWindow for the returned errors and sizing caveats.
func (mon *Monitor) SetMaxWindow(d time.Duration) error {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.SetMaxWindow(d)
}

// SetMaxHistory bounds how much loudness history the integrated-loudness
// and LRA measurements retain. See Meter.SetMaxHistory — in particular,
// prefer calling this (or constructing with ModeHistogram) on any Monitor
// backing a 24/7 bus, since the default history is unbounded.
func (mon *Monitor) SetMaxHistory(d time.Duration) error {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.SetMaxHistory(d)
}

// Momentary returns the momentary loudness (trailing 400 ms) in LUFS. See
// Meter.Momentary.
func (mon *Monitor) Momentary() (float64, error) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.Momentary()
}

// ShortTerm returns the short-term loudness (trailing 3 s) in LUFS. See
// Meter.ShortTerm.
func (mon *Monitor) ShortTerm() (float64, error) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.ShortTerm()
}

// Integrated returns the gated integrated loudness over everything
// processed so far, in LUFS. See Meter.Integrated.
func (mon *Monitor) Integrated() (float64, error) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.Integrated()
}

// Range returns the loudness range (EBU Tech 3342) in LU. See
// Meter.Range.
func (mon *Monitor) Range() (float64, error) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.Range()
}

// RelativeThreshold returns the relative gating threshold in LUFS. See
// Meter.RelativeThreshold.
func (mon *Monitor) RelativeThreshold() (float64, error) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.RelativeThreshold()
}

// SamplePeak returns the maximum linear sample peak seen on channel ch
// across all processed frames. See Meter.SamplePeak.
func (mon *Monitor) SamplePeak(ch int) (float64, error) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.SamplePeak(ch)
}

// PrevSamplePeak returns the maximum linear sample peak on channel ch
// from the most recent Process call only. See Meter.PrevSamplePeak.
func (mon *Monitor) PrevSamplePeak(ch int) (float64, error) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.PrevSamplePeak(ch)
}

// TruePeak returns the maximum linear true (inter-sample) peak seen on
// channel ch across all processed frames. See Meter.TruePeak.
func (mon *Monitor) TruePeak(ch int) (float64, error) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.TruePeak(ch)
}

// PrevTruePeak returns the maximum linear true peak on channel ch from
// the most recent Process call only. See Meter.PrevTruePeak.
func (mon *Monitor) PrevTruePeak(ch int) (float64, error) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.meter.PrevTruePeak(ch)
}

// SampleRate reports the sample rate the Monitor was constructed with.
// The wrapped Meter's configuration is immutable after construction, so
// this forwards to it without locking.
func (mon *Monitor) SampleRate() int { return mon.meter.SampleRate() }

// Channels reports the interleaved channel count the Monitor expects.
// Immutable after construction; safe to call without locking.
func (mon *Monitor) Channels() int { return mon.meter.Channels() }

// Mode reports the measurement mode bitmask the Monitor was constructed
// with. Immutable after construction; safe to call without locking.
func (mon *Monitor) Mode() Mode { return mon.meter.Mode() }
