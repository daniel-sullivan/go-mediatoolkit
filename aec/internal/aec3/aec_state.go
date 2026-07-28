// This file ports modules/audio_processing/aec3/aec_state.{h,cc}:
// AecState, which handles the state and the conditions for the echo
// removal functionality.
//
// Dropped per this port's conventions:
//   - ApmDataDumper (data_dumper_), the static instance_count_ that
//     exists only to number ApmDataDumper instances, and every
//     Dump/DumpRaw call in AecState::Update: pure debug
//     instrumentation with no effect on the numerical output,
//     consistent with this port's established convention of omitting
//     ApmDataDumper entirely (see e.g. subtractor.go's top comment).
//   - Three field-trial gates, all field_trial::IsEnabled(...) calls
//     that resolve to a fixed value under this port's "no
//     field-trial system" convention (every gate defaults to
//     disabled), each simplified and documented at its call site
//     below:
//   - DeactivateInitialStateResetAtEchoPathChange()
//     ("WebRTC-Aec3DeactivateInitialStateResetKillSwitch") is always
//     false, so HandleEchoPathChange's
//     "if (!deactivate_initial_state_reset_at_echo_path_change_)"
//     guard always passes: initialState.Reset() runs
//     unconditionally.
//   - FullResetAtEchoPathChange()
//     ("WebRTC-Aec3AecStateFullResetKillSwitch", negated) is always
//     true, so "full_reset_at_echo_path_change_" collapses to a
//     constant true and the "&&" against it in HandleEchoPathChange
//     is dropped.
//   - SubtractorAnalyzerResetAtEchoPathChange()
//     ("WebRTC-Aec3AecStateSubtractorAnalyzerResetKillSwitch",
//     negated) is always true, so
//     subtractorOutputAnalyzer.HandleEchoPathChange() runs
//     unconditionally at the end of HandleEchoPathChange.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// aecStateInitialState controls the transition from the initial
// state, which in turn controls when the filter parameters for the
// initial state should be used. C: AecState::InitialState.
type aecStateInitialState struct {
	conservativeInitialPhase       bool
	initialStateSeconds            float32
	transitionTriggered            bool
	initialStateActive             bool
	strongNotSaturatedRenderBlocks int
}

// newAecStateInitialState constructs an aecStateInitialState. C:
// AecState::InitialState::InitialState.
func newAecStateInitialState(config config.Config) *aecStateInitialState {
	s := &aecStateInitialState{
		conservativeInitialPhase: config.Filter.ConservativeInitialPhase,
		initialStateSeconds:      config.Filter.InitialStateSeconds,
	}
	s.Reset()
	return s
}

// Reset resets the state to again begin in the initial state. C:
// AecState::InitialState::Reset.
func (s *aecStateInitialState) Reset() {
	s.initialStateActive = true
	s.strongNotSaturatedRenderBlocks = 0
}

// Update updates the state based on new data. C:
// AecState::InitialState::Update.
func (s *aecStateInitialState) Update(activeRender, saturatedCapture bool) {
	if activeRender && !saturatedCapture {
		s.strongNotSaturatedRenderBlocks++
	}

	prevInitialState := s.initialStateActive
	if s.conservativeInitialPhase {
		s.initialStateActive = s.strongNotSaturatedRenderBlocks < 5*NumBlocksPerSecond
	} else {
		// initialStateSeconds is a runtime configuration value (not a
		// compile-time constant), so this product -- consumed by the
		// comparison below -- is wrapped in mul32 for consistency with
		// this port's FP-strictness convention.
		s.initialStateActive = float32(s.strongNotSaturatedRenderBlocks) < mul32(s.initialStateSeconds, float32(NumBlocksPerSecond))
	}

	s.transitionTriggered = !s.initialStateActive && prevInitialState
}

// InitialStateActive returns whether the initial state is active or
// not. C: AecState::InitialState::InitialStateActive.
func (s *aecStateInitialState) InitialStateActive() bool { return s.initialStateActive }

// TransitionTriggered returns whether the transition from the
// initial state has started. C:
// AecState::InitialState::TransitionTriggered.
func (s *aecStateInitialState) TransitionTriggered() bool { return s.transitionTriggered }

// aecStateFilterDelay chooses the direct-path delay relative to the
// beginning of the filter, as well as any other data related to the
// delay used within AecState. C: AecState::FilterDelay.
type aecStateFilterDelay struct {
	delayHeadroomBlocks   int
	externalDelayReported bool
	filterDelaysBlocks    []int
	minFilterDelayBlocks  int
	externalDelay         *DelayEstimate
}

// newAecStateFilterDelay constructs an aecStateFilterDelay. C:
// AecState::FilterDelay::FilterDelay.
func newAecStateFilterDelay(config config.Config, numCaptureChannels int) *aecStateFilterDelay {
	delayHeadroomBlocks := config.Delay.DelayHeadroomSamples / BlockSize
	filterDelaysBlocks := make([]int, numCaptureChannels)
	for i := range filterDelaysBlocks {
		filterDelaysBlocks[i] = delayHeadroomBlocks
	}
	return &aecStateFilterDelay{
		delayHeadroomBlocks:  delayHeadroomBlocks,
		filterDelaysBlocks:   filterDelaysBlocks,
		minFilterDelayBlocks: delayHeadroomBlocks,
	}
}

// ExternalDelayReported returns whether an external delay has been
// reported to the AecState (from the delay estimator). C:
// AecState::FilterDelay::ExternalDelayReported.
func (d *aecStateFilterDelay) ExternalDelayReported() bool { return d.externalDelayReported }

// DirectPathFilterDelays returns the delay in blocks relative to the
// beginning of the filter that corresponds to the direct path of the
// echo, per capture channel. C:
// AecState::FilterDelay::DirectPathFilterDelays.
func (d *aecStateFilterDelay) DirectPathFilterDelays() []int { return d.filterDelaysBlocks }

// MinDirectPathFilterDelay returns the minimum delay among the
// direct path delays relative to the beginning of the filter. C:
// AecState::FilterDelay::MinDirectPathFilterDelay.
func (d *aecStateFilterDelay) MinDirectPathFilterDelay() int { return d.minFilterDelayBlocks }

// Update updates the delay estimates based on new data. C:
// AecState::FilterDelay::Update.
func (d *aecStateFilterDelay) Update(analyzerFilterDelayEstimatesBlocks []int, externalDelay *DelayEstimate, blocksWithProperFilterAdaptation int) {
	if externalDelay != nil && (d.externalDelay == nil || d.externalDelay.Delay != externalDelay.Delay) {
		ext := *externalDelay
		d.externalDelay = &ext
		d.externalDelayReported = true
	}

	delayEstimatorMayNotHaveConverged := blocksWithProperFilterAdaptation < 2*NumBlocksPerSecond
	if delayEstimatorMayNotHaveConverged && d.externalDelay != nil {
		delayGuess := d.delayHeadroomBlocks
		for i := range d.filterDelaysBlocks {
			d.filterDelaysBlocks[i] = delayGuess
		}
	} else {
		copy(d.filterDelaysBlocks, analyzerFilterDelayEstimatesBlocks)
	}

	minDelay := d.filterDelaysBlocks[0]
	for _, v := range d.filterDelaysBlocks[1:] {
		if v < minDelay {
			minDelay = v
		}
	}
	d.minFilterDelayBlocks = minDelay
}

const (
	// filterQualityStartupThresholdBlocks == kNumBlocksPerSecond *
	// 0.4f: both operands are compile-time constants (as in upstream's
	// constexpr kNumBlocksPerSecond times a literal), so this is
	// ordinary Go constant arithmetic, not a runtime float32 op --
	// no mul32 wrapping applies.
	filterQualityStartupThresholdBlocks = 0.4 * NumBlocksPerSecond
	// filterQualityResetThresholdBlocks == kNumBlocksPerSecond * 0.2f.
	filterQualityResetThresholdBlocks = 0.2 * NumBlocksPerSecond
)

// aecStateFilteringQualityAnalyzer analyzes how well the linear
// filter is, and can be expected to, perform on the current signals.
// The purpose of this is for using to select the echo suppression
// functionality as well as the input to the echo suppressor. C:
// AecState::FilteringQualityAnalyzer.
type aecStateFilteringQualityAnalyzer struct {
	useLinearFilter              bool
	overallUsableLinearEstimates bool
	filterUpdateBlocksSinceReset int
	filterUpdateBlocksSinceStart int
	convergenceSeen              bool
	usableLinearFilterEstimates  []bool
}

// newAecStateFilteringQualityAnalyzer constructs an
// aecStateFilteringQualityAnalyzer. C:
// AecState::FilteringQualityAnalyzer::FilteringQualityAnalyzer.
func newAecStateFilteringQualityAnalyzer(config config.Config, numCaptureChannels int) *aecStateFilteringQualityAnalyzer {
	return &aecStateFilteringQualityAnalyzer{
		useLinearFilter:             config.Filter.UseLinearFilter,
		usableLinearFilterEstimates: make([]bool, numCaptureChannels),
	}
}

// LinearFilterUsable returns whether the linear filter can be used
// for the echo canceller output. C:
// AecState::FilteringQualityAnalyzer::LinearFilterUsable.
func (a *aecStateFilteringQualityAnalyzer) LinearFilterUsable() bool {
	return a.overallUsableLinearEstimates
}

// UsableLinearFilterOutputs returns whether an individual filter
// output can be used for the echo canceller output. C:
// AecState::FilteringQualityAnalyzer::UsableLinearFilterOutputs.
func (a *aecStateFilteringQualityAnalyzer) UsableLinearFilterOutputs() []bool {
	return a.usableLinearFilterEstimates
}

// Reset resets the state of the analyzer. C:
// AecState::FilteringQualityAnalyzer::Reset.
func (a *aecStateFilteringQualityAnalyzer) Reset() {
	for i := range a.usableLinearFilterEstimates {
		a.usableLinearFilterEstimates[i] = false
	}
	a.overallUsableLinearEstimates = false
	a.filterUpdateBlocksSinceReset = 0
}

// Update updates the analysis based on new data. C:
// AecState::FilteringQualityAnalyzer::Update.
func (a *aecStateFilteringQualityAnalyzer) Update(activeRender, transparentMode, saturatedCapture bool, externalDelay *DelayEstimate, anyFilterConverged bool) {
	filterUpdate := activeRender && !saturatedCapture
	if filterUpdate {
		a.filterUpdateBlocksSinceReset++
		a.filterUpdateBlocksSinceStart++
	}

	a.convergenceSeen = a.convergenceSeen || anyFilterConverged

	sufficientDataToConvergeAtStartup := float32(a.filterUpdateBlocksSinceStart) > filterQualityStartupThresholdBlocks
	sufficientDataToConvergeAtReset := sufficientDataToConvergeAtStartup &&
		float32(a.filterUpdateBlocksSinceReset) > filterQualityResetThresholdBlocks

	a.overallUsableLinearEstimates = sufficientDataToConvergeAtStartup && sufficientDataToConvergeAtReset

	a.overallUsableLinearEstimates = a.overallUsableLinearEstimates && (externalDelay != nil || a.convergenceSeen)

	a.overallUsableLinearEstimates = a.overallUsableLinearEstimates && !transparentMode

	if a.useLinearFilter {
		for i := range a.usableLinearFilterEstimates {
			a.usableLinearFilterEstimates[i] = a.overallUsableLinearEstimates
		}
	}
}

// aecStateSaturationDetector detects whether the echo is to be
// considered to be saturated. C: AecState::SaturationDetector.
type aecStateSaturationDetector struct {
	saturatedEcho bool
}

// SaturatedEcho returns whether the echo is to be considered
// saturated. C: AecState::SaturationDetector::SaturatedEcho.
func (d *aecStateSaturationDetector) SaturatedEcho() bool { return d.saturatedEcho }

// Update updates the detection decision based on new data. C:
// AecState::SaturationDetector::Update.
func (d *aecStateSaturationDetector) Update(x *Block, saturatedCapture, usableLinearEstimate bool, subtractorOutput []SubtractorOutput, echoPathGain float32) {
	d.saturatedEcho = false
	if !saturatedCapture {
		return
	}

	if usableLinearEstimate {
		const saturationThreshold float32 = 20000.0
		for ch := 0; ch < len(subtractorOutput); ch++ {
			d.saturatedEcho = d.saturatedEcho ||
				subtractorOutput[ch].SRefinedMaxAbs > saturationThreshold ||
				subtractorOutput[ch].SCoarseMaxAbs > saturationThreshold
		}
	} else {
		var maxSample float32
		for ch := 0; ch < x.NumChannels(); ch++ {
			for _, v := range x.View(0, ch) {
				av := v
				if av < 0 {
					av = -av
				}
				if av > maxSample {
					maxSample = av
				}
			}
		}

		const margin float32 = 10.0
		// Both maxSample and echoPathGain are runtime values, so the
		// full product feeding the comparison below is wrapped in
		// mul32 for consistency.
		peakEchoAmplitude := mul32(mul32(maxSample, echoPathGain), margin)
		d.saturatedEcho = d.saturatedEcho || peakEchoAmplitude > 32000
	}
}

// computeAvgRenderReverb computes the average (over render channels)
// power spectrum with reverb added, at delayBlocks relative to the
// spectrum buffer's read index. C: (anonymous namespace)
// ComputeAvgRenderReverb in aec_state.cc.
func computeAvgRenderReverb(spectrumBuffer *SpectrumBuffer, delayBlocks int, reverbDecay float32, reverbModel *ReverbModel, reverbPowerSpectrum *[FFTLengthBy2Plus1]float32) {
	numRenderChannels := len(spectrumBuffer.Buffer[0])
	idxAtDelay := spectrumBuffer.OffsetIndex(spectrumBuffer.Read, delayBlocks)
	idxPast := spectrumBuffer.IncIndex(idxAtDelay)

	averageChannels := func(numRenderChannels int, spectrumBand0 [][]float32, renderPower []float32) {
		for k := range renderPower {
			renderPower[k] = 0
		}
		for ch := 0; ch < numRenderChannels; ch++ {
			for k := range renderPower {
				renderPower[k] += spectrumBand0[ch][k]
			}
		}
		normalizer := 1.0 / float32(numRenderChannels)
		for k := range renderPower {
			renderPower[k] = mul32(renderPower[k], normalizer)
		}
	}

	var x2Data [FFTLengthBy2Plus1]float32
	var x2 []float32
	if numRenderChannels > 1 {
		averageChannels(numRenderChannels, spectrumBuffer.Buffer[idxPast], x2Data[:])
		reverbModel.UpdateReverbNoFreqShaping(x2Data[:], 1.0, reverbDecay)

		averageChannels(numRenderChannels, spectrumBuffer.Buffer[idxAtDelay], x2Data[:])
		x2 = x2Data[:]
	} else {
		reverbModel.UpdateReverbNoFreqShaping(spectrumBuffer.Buffer[idxPast][0], 1.0, reverbDecay)
		x2 = spectrumBuffer.Buffer[idxAtDelay][0]
	}

	reverbPower := reverbModel.Reverb()
	for k := range x2 {
		reverbPowerSpectrum[k] = add32(x2[k], reverbPower[k])
	}
}

const (
	// residualScalingConservativeConvergenceBlocks == 1.5f *
	// kNumBlocksPerSecond, a product of two compile-time constants
	// (as in upstream) -- ordinary Go constant arithmetic, no mul32.
	residualScalingConservativeConvergenceBlocks = 1.5 * NumBlocksPerSecond
	// residualScalingDefaultConvergenceBlocks == 0.8f * kNumBlocksPerSecond.
	residualScalingDefaultConvergenceBlocks = 0.8 * NumBlocksPerSecond
)

// AecState handles the state and the conditions for the echo removal
// functionality. C: aec_state.{h,cc}'s AecState.
type AecState struct {
	config             config.Config
	numCaptureChannels int

	initialState       *aecStateInitialState
	delayState         *aecStateFilterDelay
	transparentState   TransparentMode // nil if disabled by configuration.
	filterQualityState *aecStateFilteringQualityAnalyzer
	saturationDetector aecStateSaturationDetector

	erlEstimator                   *ErlEstimator
	erleEstimator                  *ErleEstimator
	strongNotSaturatedRenderBlocks int
	blocksWithActiveRender         int
	captureSignalSaturation        bool
	filterAnalyzer                 *FilterAnalyzer
	echoAudibility                 *EchoAudibility
	reverbModelEstimator           *ReverbModelEstimator
	avgRenderReverb                *ReverbModel
	subtractorOutputAnalyzer       *SubtractorOutputAnalyzer
}

// NewAecState constructs an AecState. C: AecState::AecState.
func NewAecState(config config.Config, numCaptureChannels int) *AecState {
	return &AecState{
		config:                   config,
		numCaptureChannels:       numCaptureChannels,
		initialState:             newAecStateInitialState(config),
		delayState:               newAecStateFilterDelay(config, numCaptureChannels),
		transparentState:         NewTransparentMode(config),
		filterQualityState:       newAecStateFilteringQualityAnalyzer(config, numCaptureChannels),
		erlEstimator:             NewErlEstimator(2 * NumBlocksPerSecond),
		erleEstimator:            NewErleEstimator(2*NumBlocksPerSecond, config, numCaptureChannels),
		filterAnalyzer:           NewFilterAnalyzer(config, numCaptureChannels),
		echoAudibility:           NewEchoAudibility(config.EchoAudibility.UseStationarityPropertiesAtInit),
		reverbModelEstimator:     NewReverbModelEstimator(config, numCaptureChannels),
		avgRenderReverb:          new(ReverbModel),
		subtractorOutputAnalyzer: NewSubtractorOutputAnalyzer(numCaptureChannels),
	}
}

// UsableLinearEstimate returns whether the echo subtractor can be
// used to determine the residual echo. C:
// AecState::UsableLinearEstimate.
func (a *AecState) UsableLinearEstimate() bool {
	return a.filterQualityState.LinearFilterUsable() && a.config.Filter.UseLinearFilter
}

// UseLinearFilterOutput returns whether the echo subtractor output
// should be used as output. C: AecState::UseLinearFilterOutput.
func (a *AecState) UseLinearFilterOutput() bool {
	return a.filterQualityState.LinearFilterUsable() && a.config.Filter.UseLinearFilter
}

// ActiveRender returns whether the render signal is currently
// active. C: AecState::ActiveRender.
func (a *AecState) ActiveRender() bool { return a.blocksWithActiveRender > 200 }

// GetResidualEchoScaling returns the appropriate scaling of the
// residual echo to match the audibility. C:
// AecState::GetResidualEchoScaling.
func (a *AecState) GetResidualEchoScaling(residualScaling []float32) {
	var filterHasHadTimeToConverge bool
	if a.config.Filter.ConservativeInitialPhase {
		filterHasHadTimeToConverge = float32(a.strongNotSaturatedRenderBlocks) >= residualScalingConservativeConvergenceBlocks
	} else {
		filterHasHadTimeToConverge = float32(a.strongNotSaturatedRenderBlocks) >= residualScalingDefaultConvergenceBlocks
	}
	a.echoAudibility.GetResidualEchoScaling(filterHasHadTimeToConverge, residualScaling)
}

// UseStationarityProperties returns whether the stationary
// properties of the signals are used in the aec. C:
// AecState::UseStationarityProperties.
func (a *AecState) UseStationarityProperties() bool {
	return a.config.EchoAudibility.UseStationarityProperties
}

// Erle returns the ERLE. C: AecState::Erle.
func (a *AecState) Erle(onsetCompensated bool) [][FFTLengthBy2Plus1]float32 {
	return a.erleEstimator.Erle(onsetCompensated)
}

// ErleUnbounded returns the non-capped ERLE. C:
// AecState::ErleUnbounded.
func (a *AecState) ErleUnbounded() [][FFTLengthBy2Plus1]float32 {
	return a.erleEstimator.ErleUnbounded()
}

// FullBandErleLog2 returns the fullband ERLE estimate in log2 units.
// C: AecState::FullBandErleLog2.
func (a *AecState) FullBandErleLog2() float32 { return a.erleEstimator.FullbandErleLog2() }

// Erl returns the ERL. C: AecState::Erl.
func (a *AecState) Erl() [FFTLengthBy2Plus1]float32 { return a.erlEstimator.Erl() }

// ErlTimeDomain returns the time-domain ERL. C:
// AecState::ErlTimeDomain.
func (a *AecState) ErlTimeDomain() float32 { return a.erlEstimator.ErlTimeDomain() }

// MinDirectPathFilterDelay returns the delay estimate based on the
// linear filter. C: AecState::MinDirectPathFilterDelay.
func (a *AecState) MinDirectPathFilterDelay() int { return a.delayState.MinDirectPathFilterDelay() }

// StrongNotSaturatedRenderBlocks returns how many blocks of active,
// unsaturated render have been seen since the last echo path change.
// Read-only Go-port-only accessor for a field upstream keeps private:
// suppressor_gate.go needs it to judge how much of a chance the delay
// estimator has had to find an echo path.
func (a *AecState) StrongNotSaturatedRenderBlocks() int { return a.strongNotSaturatedRenderBlocks }

// SaturatedCapture returns whether the capture signal is saturated.
// C: AecState::SaturatedCapture.
func (a *AecState) SaturatedCapture() bool { return a.captureSignalSaturation }

// SaturatedEcho returns whether the echo signal is saturated. C:
// AecState::SaturatedEcho.
func (a *AecState) SaturatedEcho() bool { return a.saturationDetector.SaturatedEcho() }

// UpdateCaptureSaturation updates the capture signal saturation. C:
// AecState::UpdateCaptureSaturation.
func (a *AecState) UpdateCaptureSaturation(captureSignalSaturation bool) {
	a.captureSignalSaturation = captureSignalSaturation
}

// TransparentModeActive returns whether the transparent mode is
// active. C: AecState::TransparentModeActive.
func (a *AecState) TransparentModeActive() bool {
	return a.transparentState != nil && a.transparentState.Active()
}

// HandleEchoPathChange takes appropriate action at an echo path
// change. C: AecState::HandleEchoPathChange.
func (a *AecState) HandleEchoPathChange(echoPathVariability EchoPathVariability) {
	fullReset := func() {
		a.filterAnalyzer.Reset()
		a.captureSignalSaturation = false
		a.strongNotSaturatedRenderBlocks = 0
		a.blocksWithActiveRender = 0
		// DeactivateInitialStateResetAtEchoPathChange() always
		// resolves false; see file doc comment.
		a.initialState.Reset()
		if a.transparentState != nil {
			a.transparentState.Reset()
		}
		a.erleEstimator.Reset(true)
		a.erlEstimator.Reset()
		a.filterQualityState.Reset()
	}

	// FullResetAtEchoPathChange() always resolves true; see file doc
	// comment.
	if echoPathVariability.DelayChange != DelayAdjustmentNone {
		fullReset()
	} else if echoPathVariability.GainChange {
		a.erleEstimator.Reset(false)
	}

	// SubtractorAnalyzerResetAtEchoPathChange() always resolves true;
	// see file doc comment.
	a.subtractorOutputAnalyzer.HandleEchoPathChange()
}

// ReverbDecay returns the decay factor for the echo reverberation.
// The parameter mild indicates which exponential decay to return.
// The default one or a milder one that can be used during nearend
// regions. C: AecState::ReverbDecay.
func (a *AecState) ReverbDecay(mild bool) float32 { return a.reverbModelEstimator.ReverbDecay(mild) }

// GetReverbFrequencyResponse returns the frequency response of the
// reverberant echo. C: AecState::GetReverbFrequencyResponse.
func (a *AecState) GetReverbFrequencyResponse() [FFTLengthBy2Plus1]float32 {
	return a.reverbModelEstimator.GetReverbFrequencyResponse()
}

// TransitionTriggered returns whether the transition for going out
// of the initial stated has been triggered. C:
// AecState::TransitionTriggered.
func (a *AecState) TransitionTriggered() bool { return a.initialState.TransitionTriggered() }

// FilterLengthBlocks returns filter length in blocks. All filters
// have the same length, so this arbitrarily returns channel 0's
// length. C: AecState::FilterLengthBlocks.
func (a *AecState) FilterLengthBlocks() int { return a.filterAnalyzer.FilterLengthBlocks() }

// Update updates the aec state. C: AecState::Update.
func (a *AecState) Update(
	externalDelay *DelayEstimate,
	adaptiveFilterFrequencyResponses [][][FFTLengthBy2Plus1]float32,
	adaptiveFilterImpulseResponses [][]float32,
	renderBuffer *RenderBuffer,
	e2Refined, y2 [][FFTLengthBy2Plus1]float32,
	subtractorOutput []SubtractorOutput,
) {
	// Analyze the filter outputs and filters.
	var anyFilterConverged, anyCoarseFilterConverged, allFiltersDiverged bool
	a.subtractorOutputAnalyzer.Update(subtractorOutput, &anyFilterConverged, &anyCoarseFilterConverged, &allFiltersDiverged)

	var anyFilterConsistent bool
	var maxEchoPathGain float32
	a.filterAnalyzer.Update(adaptiveFilterImpulseResponses, renderBuffer, &anyFilterConsistent, &maxEchoPathGain)

	// Estimate the direct path delay of the filter.
	if a.config.Filter.UseLinearFilter {
		a.delayState.Update(a.filterAnalyzer.FilterDelaysBlocks(), externalDelay, a.strongNotSaturatedRenderBlocks)
	}

	alignedRenderBlock := renderBuffer.GetBlock(-a.delayState.MinDirectPathFilterDelay())

	// Update render counters.
	activeRender := false
	for ch := 0; ch < alignedRenderBlock.NumChannels(); ch++ {
		var renderEnergy float32
		for _, v := range alignedRenderBlock.View(0, ch) {
			renderEnergy = mla(renderEnergy, v, v)
		}
		limit := a.config.RenderLevels.ActiveRenderLimit
		if renderEnergy > mul32(mul32(limit, limit), float32(FFTLengthBy2)) {
			activeRender = true
			break
		}
	}
	if activeRender {
		a.blocksWithActiveRender++
	}
	if activeRender && !a.SaturatedCapture() {
		a.strongNotSaturatedRenderBlocks++
	}

	var avgRenderSpectrumWithReverb [FFTLengthBy2Plus1]float32
	computeAvgRenderReverb(renderBuffer.GetSpectrumBuffer(), a.delayState.MinDirectPathFilterDelay(), a.ReverbDecay(false), a.avgRenderReverb, &avgRenderSpectrumWithReverb)

	if a.config.EchoAudibility.UseStationarityProperties {
		// Update the echo audibility evaluator.
		reverb := a.avgRenderReverb.Reverb()
		a.echoAudibility.Update(renderBuffer, reverb[:], a.delayState.MinDirectPathFilterDelay(), a.delayState.ExternalDelayReported())
	}

	// Update the ERL and ERLE measures.
	if a.initialState.TransitionTriggered() {
		a.erleEstimator.Reset(false)
	}

	a.erleEstimator.Update(renderBuffer, adaptiveFilterFrequencyResponses, avgRenderSpectrumWithReverb, y2, e2Refined, a.subtractorOutputAnalyzer.ConvergedFilters())

	a.erlEstimator.Update(a.subtractorOutputAnalyzer.ConvergedFilters(), renderBuffer.Spectrum(a.delayState.MinDirectPathFilterDelay()), y2)

	// Detect and flag echo saturation.
	if a.config.EpStrength.EchoCanSaturate {
		a.saturationDetector.Update(alignedRenderBlock, a.SaturatedCapture(), a.UsableLinearEstimate(), subtractorOutput, maxEchoPathGain)
	}

	// Update the decision on whether to use the initial state
	// parameter set.
	a.initialState.Update(activeRender, a.SaturatedCapture())

	// Detect whether the transparent mode should be activated.
	if a.transparentState != nil {
		a.transparentState.Update(a.delayState.MinDirectPathFilterDelay(), anyFilterConsistent, anyFilterConverged, anyCoarseFilterConverged, allFiltersDiverged, activeRender, a.SaturatedCapture())
	}

	// Analyze the quality of the filter.
	a.filterQualityState.Update(activeRender, a.TransparentModeActive(), a.SaturatedCapture(), externalDelay, anyFilterConverged)

	// Update the reverb estimate.
	stationaryBlock := a.config.EchoAudibility.UseStationarityProperties && a.echoAudibility.IsBlockStationary()

	a.reverbModelEstimator.Update(
		a.filterAnalyzer.GetAdjustedFilters(),
		adaptiveFilterFrequencyResponses,
		a.erleEstimator.GetInstLinearQualityEstimates(),
		a.delayState.DirectPathFilterDelays(),
		a.filterQualityState.UsableLinearFilterOutputs(),
		stationaryBlock,
	)
}
