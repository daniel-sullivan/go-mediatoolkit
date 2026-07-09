// This file ports modules/audio_processing/aec3/
// transparent_mode.{h,cc}: the transparent-mode detector, which
// toggles a mode that causes the suppressor to apply less suppression
// (used e.g. when a headset makes echo unlikely).
//
// Dropped per this port's conventions: two field-trial gates.
//   - "WebRTC-Aec3TransparentModeKillSwitch": DeactivateTransparentMode()
//     is hardcoded to its default of false (not killed), so
//     TransparentMode::Create's "config.ep_strength.bounded_erl ||
//     DeactivateTransparentMode()" collapses to just
//     config.EpStrength.BoundedErl below.
//   - "WebRTC-Aec3TransparentModeHmm": ActivateTransparentModeHmm() is
//     hardcoded to its default of false, so the HMM-based
//     TransparentModeImpl variant is never selected by
//     TransparentMode::Create in upstream's out-of-the-box behavior.
//     That variant is therefore omitted from this port entirely; only
//     LegacyTransparentModeImpl is ported.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

const (
	transparentModeBlocksSinceConvergencedFilterInit = 10000
	transparentModeBlocksSinceConsistentEstimateInit = 10000
)

// TransparentMode detects and toggles the transparent mode which
// causes the suppressor to apply less suppression. C:
// transparent_mode.h's TransparentMode.
type TransparentMode interface {
	// Active returns whether the transparent mode should be active.
	Active() bool
	// Reset resets the state of the detector.
	Reset()
	// Update updates the detection decision based on new data.
	Update(filterDelayBlocks int, anyFilterConsistent, anyFilterConverged, anyCoarseFilterConverged, allFiltersDiverged, activeRender, saturatedCapture bool)
}

// NewTransparentMode constructs the transparent-mode detector, or
// returns nil if transparent mode is disabled by configuration. C:
// TransparentMode::Create.
func NewTransparentMode(config config.Config) TransparentMode {
	if config.EpStrength.BoundedErl {
		return nil
	}
	return newLegacyTransparentMode(config)
}

// legacyTransparentMode is the legacy classifier for toggling
// transparent mode. C: (anonymous namespace) LegacyTransparentModeImpl.
type legacyTransparentMode struct {
	linearAndStableEchoPath bool

	captureBlockCounter             int
	transparencyActivated           bool
	activeBlocksSinceSaneFilter     int
	saneFilterObserved              bool
	finiteErlRecentlyDetected       bool
	nonConvergedSequenceSize        int
	divergedSequenceSize            int
	activeNonConvergedSequenceSize  int
	numConvergedBlocks              int
	recentConvergenceDuringActivity bool
	strongNotSaturatedRenderBlocks  int
}

// newLegacyTransparentMode constructs a legacyTransparentMode. C:
// LegacyTransparentModeImpl::LegacyTransparentModeImpl.
func newLegacyTransparentMode(config config.Config) *legacyTransparentMode {
	return &legacyTransparentMode{
		linearAndStableEchoPath:     config.EchoRemovalControl.LinearAndStableEchoPath,
		activeBlocksSinceSaneFilter: transparentModeBlocksSinceConsistentEstimateInit,
		nonConvergedSequenceSize:    transparentModeBlocksSinceConvergencedFilterInit,
	}
}

// Active returns whether the transparent mode should be active. C:
// LegacyTransparentModeImpl::Active.
func (t *legacyTransparentMode) Active() bool { return t.transparencyActivated }

// Reset resets the state of the detector. C:
// LegacyTransparentModeImpl::Reset.
func (t *legacyTransparentMode) Reset() {
	t.nonConvergedSequenceSize = transparentModeBlocksSinceConvergencedFilterInit
	t.divergedSequenceSize = 0
	t.strongNotSaturatedRenderBlocks = 0
	if t.linearAndStableEchoPath {
		t.recentConvergenceDuringActivity = false
	}
}

// Update updates the detection decision based on new data. C:
// LegacyTransparentModeImpl::Update.
func (t *legacyTransparentMode) Update(filterDelayBlocks int, anyFilterConsistent, anyFilterConverged, anyCoarseFilterConverged, allFiltersDiverged, activeRender, saturatedCapture bool) {
	t.captureBlockCounter++
	if activeRender && !saturatedCapture {
		t.strongNotSaturatedRenderBlocks++
	}

	if anyFilterConsistent && filterDelayBlocks < 5 {
		t.saneFilterObserved = true
		t.activeBlocksSinceSaneFilter = 0
	} else if activeRender {
		t.activeBlocksSinceSaneFilter++
	}

	var saneFilterRecentlySeen bool
	if !t.saneFilterObserved {
		saneFilterRecentlySeen = t.captureBlockCounter <= 5*NumBlocksPerSecond
	} else {
		saneFilterRecentlySeen = t.activeBlocksSinceSaneFilter <= 30*NumBlocksPerSecond
	}

	if anyFilterConverged {
		t.recentConvergenceDuringActivity = true
		t.activeNonConvergedSequenceSize = 0
		t.nonConvergedSequenceSize = 0
		t.numConvergedBlocks++
	} else {
		t.nonConvergedSequenceSize++
		if t.nonConvergedSequenceSize > 20*NumBlocksPerSecond {
			t.numConvergedBlocks = 0
		}

		if activeRender {
			t.activeNonConvergedSequenceSize++
			if t.activeNonConvergedSequenceSize > 60*NumBlocksPerSecond {
				t.recentConvergenceDuringActivity = false
			}
		}
	}

	if !allFiltersDiverged {
		t.divergedSequenceSize = 0
	} else {
		t.divergedSequenceSize++
		if t.divergedSequenceSize >= 60 {
			// TODO: Change these lines to ensure proper triggering of
			// usable filter.
			t.nonConvergedSequenceSize = transparentModeBlocksSinceConvergencedFilterInit
		}
	}

	if t.activeNonConvergedSequenceSize > 60*NumBlocksPerSecond {
		t.finiteErlRecentlyDetected = false
	}
	if t.numConvergedBlocks > 50 {
		t.finiteErlRecentlyDetected = true
	}

	if t.finiteErlRecentlyDetected {
		t.transparencyActivated = false
	} else if saneFilterRecentlySeen && t.recentConvergenceDuringActivity {
		t.transparencyActivated = false
	} else {
		filterShouldHaveConverged := t.strongNotSaturatedRenderBlocks > 6*NumBlocksPerSecond
		t.transparencyActivated = filterShouldHaveConverged
	}
}
