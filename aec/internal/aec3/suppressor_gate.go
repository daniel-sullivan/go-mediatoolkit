// This file has no upstream counterpart: it is a Go-port-only addition
// that gates the suppressor on demonstrated convergence.
//
// AEC3's suppressor attenuates the capture path whenever its residual-
// echo model says echo is present, and that model is driven by the
// render (far-end) signal. If the capture signal carries no echo
// correlated with render at all — because a canceller upstream (an
// operating-system or browser-side echo canceller, a headset, or a
// network endpoint that cancels before transmitting) already removed
// it — the adaptive filter has nothing to model, never converges, and
// the suppressor spends the whole far-end burst attenuating genuine
// near-end speech. On a full-duplex voice path that suppresses
// interruptions: the near end has to talk over the far end for a
// second or more before it is heard.
//
// Upstream's own remedy for a missing echo path is transparent mode
// (transparent_mode.go), which backs the suppressor off once the
// filter demonstrably should have converged but has not. It is
// deliberately slow and sticky: it cannot activate before
// 6*kNumBlocksPerSecond blocks of active render have accumulated, a
// single spurious convergence blip latches
// recent_convergence_during_activity_, and clearing that latch needs
// 60*kNumBlocksPerSecond blocks of *contiguous* active non-converged
// render. That profile suits its purpose — noticing that an echo path
// which was there has gone away — but it leaves the first several
// seconds of every stream unprotected, which is exactly the window an
// interruption falls in.
//
// This gate covers the complementary case: a stream that never had a
// correlated echo path to begin with. It holds the suppressor inert
// until the canceller has produced positive evidence that it is
// modelling a real echo, so an unconverged suppressor can never
// attenuate the capture path. The two mechanisms compose: this one
// handles echo that was never present, transparent mode handles echo
// that stopped being present mid-stream.
package aec3

const (
	// suppressorGateFairChanceBlocks is how many active, unsaturated
	// render blocks the canceller is given to produce evidence before
	// the gate concludes there is no echo path to model. It is one full
	// MatchedFilterLagAggregator histogram window (kNumBlocksPerSecond
	// entries, see matched_filter_lag_aggregator.go): the shortest
	// horizon over which the delay estimator can reach its Converged
	// selection threshold, and therefore the shortest honest "fair
	// chance". Measured in render blocks rather than wall clock, so a
	// silent far end never burns the window.
	suppressorGateFairChanceBlocks = NumBlocksPerSecond

	// suppressorGateErleEvidenceDB is the fullband ERLE above which the
	// linear filter is taken to be cancelling a real echo. Against a
	// reference uncorrelated with the capture signal the least-squares
	// optimum is the zero filter, whose ERLE is 0 dB, so a sustained
	// reduction of this size cannot be produced without a genuine echo
	// to subtract.
	suppressorGateErleEvidenceDB = 3.0

	// suppressorGateRampBlocks spreads a gate transition over 100ms so
	// neither direction steps the capture gain audibly.
	suppressorGateRampBlocks = NumBlocksPerSecond / 10
)

// SuppressorGating selects whether the suppressor is gated on
// demonstrated convergence. Go-port-only, beyond upstream (which has
// no such control); the zero value reproduces upstream behaviour
// exactly, so every ported path is unaffected unless a caller opts in.
type SuppressorGating int

const (
	// SuppressorGatingDisabled leaves the suppressor always live, as
	// upstream AEC3 does.
	SuppressorGatingDisabled SuppressorGating = iota
	// SuppressorGatingOnConvergence holds the suppressor inert until the
	// canceller demonstrates it is modelling a real echo path.
	SuppressorGatingOnConvergence
)

// SuppressorGateState reports whether the suppressor is currently
// allowed to attenuate the capture path.
type SuppressorGateState int

const (
	// SuppressorGateEngaged: the suppressor's gains are applied
	// unmodified. Also the state whenever gating is disabled.
	SuppressorGateEngaged SuppressorGateState = iota
	// SuppressorGateTransitioning: the gate is ramping between engaged
	// and bypassed.
	SuppressorGateTransitioning
	// SuppressorGateBypassed: the suppressor is inert — its gains are
	// forced to unity and the capture path passes through unattenuated.
	SuppressorGateBypassed
)

// suppressorGate decides, per capture block, how much of the
// suppressor's attenuation to let through.
//
// The decision is deliberately monotone rather than a pair of tuned
// hold timers: convergenceSeen latches on the first evidence and is
// never cleared, and renderBlocks only ever grows, so across a stream
// the gate can bypass at most once and re-engage at most once, in that
// order. It cannot oscillate — in particular it cannot drop in and out
// of gating within an utterance. Only a fresh EchoRemover starts the
// sequence over.
type suppressorGate struct {
	mode SuppressorGating

	// convergenceSeen latches once the canceller has demonstrated that
	// it is modelling a real echo path.
	convergenceSeen bool

	// renderBlocks is a monotone count of active, unsaturated render
	// blocks: the running maximum of AecState's own counter, which an
	// echo path change resets.
	renderBlocks int

	// ramp is 0 when the suppressor is fully engaged and 1 when it is
	// fully bypassed.
	ramp float32
}

func newSuppressorGate() *suppressorGate { return &suppressorGate{} }

// SetGating selects the gating policy. Takes effect from the next
// block; the gate keeps whatever evidence it has already accumulated.
func (g *suppressorGate) SetGating(mode SuppressorGating) { g.mode = mode }

// State reports the gate's current position.
func (g *suppressorGate) State() SuppressorGateState {
	switch {
	case g.ramp >= 1:
		return SuppressorGateBypassed
	case g.ramp <= 0:
		return SuppressorGateEngaged
	default:
		return SuppressorGateTransitioning
	}
}

// delayLocked reports whether the delay estimator has settled on a
// dominant lag. MatchedFilterLagAggregator only labels an estimate
// DelayEstimateQualityRefined once its histogram peak has passed the
// configured Converged threshold, so the label is exactly the
// aggregator's own "reliable delay found" verdict, already threaded
// down to the echo remover. Nil (no estimate, including every stream
// configured to take its delay from the caller rather than estimate
// one) is not a lock.
func delayLocked(externalDelay *DelayEstimate) bool {
	return externalDelay != nil && externalDelay.Quality == DelayEstimateQualityRefined
}

// Update folds this block's evidence into the gate and advances the
// ramp. Call once per capture block, after AecState.Update.
//
// Two independent signals count as evidence, either alone sufficient:
// a locked delay estimate, and a fullband ERLE clear of its floor. The
// first is the faster of the two; the second is the only one available
// when the delay comes from the caller instead of the estimator. The
// ERL estimate is deliberately not consulted: it drifts off its floor
// on a starved canceller driven by intermittent render, so it cannot
// distinguish the two cases.
func (g *suppressorGate) Update(aecState *AecState, externalDelay *DelayEstimate) {
	if g.mode == SuppressorGatingDisabled {
		g.ramp = 0
		return
	}

	if delayLocked(externalDelay) || Log2TodB(aecState.FullBandErleLog2()) > suppressorGateErleEvidenceDB {
		g.convergenceSeen = true
	}
	if n := aecState.StrongNotSaturatedRenderBlocks(); n > g.renderBlocks {
		g.renderBlocks = n
	}

	const step = 1.0 / suppressorGateRampBlocks
	if !g.convergenceSeen && g.renderBlocks > suppressorGateFairChanceBlocks {
		if g.ramp += step; g.ramp > 1 {
			g.ramp = 1
		}
		return
	}
	if g.ramp -= step; g.ramp < 0 {
		g.ramp = 0
	}
}

// Apply relaxes the suppressor's computed gains towards unity by the
// current ramp position, so a fully bypassed gate leaves both the
// per-bin lower-band gain and the upper-band gain at exactly 1. At
// unity gain SuppressionFilter.ApplyGain is an identity on the
// spectrum it is handed (its comfort-noise contribution is scaled by
// sqrt(1-g^2), which vanishes), so the capture path passes through
// unattenuated.
func (g *suppressorGate) Apply(lowBandGain *[FFTLengthBy2Plus1]float32, highBandsGain *float32) {
	if g.ramp <= 0 {
		return
	}
	for k := range lowBandGain {
		lowBandGain[k] += g.ramp * (1 - lowBandGain[k])
	}
	*highBandsGain += g.ramp * (1 - *highBandsGain)
}
