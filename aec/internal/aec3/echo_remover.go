// This file ports modules/audio_processing/aec3/echo_remover.{h,cc}:
// EchoRemover, the top-level orchestrator that removes echo from a
// block of capture samples using the linear filter (Subtractor),
// residual echo estimator, suppression gain/filter, and comfort noise
// generator.
//
// Dropped/collapsed per this port's conventions:
//   - The abstract EchoRemover interface plus EchoRemoverImpl/
//     EchoRemover::Create factory: collapsed into a single concrete
//     EchoRemover type with a plain constructor, matching every other
//     ported component in this package (AecState, Subtractor, ...).
//   - ApmDataDumper (all DumpWav/DumpRaw calls) and the
//     instance_count_ used only to number its instances: pure debug
//     instrumentation, no numerical effect.
//   - The Aec3Optimization parameter (see suppression_gain.go's top
//     comment; this package is scalar-only).
//   - The kMaxNumChannelsOnStack/NumChannelsOnHeap stack-vs-heap
//     scratch-buffer partitioning: a pure C++ memory-layout
//     micro-optimization with no numerical effect. This port always
//     allocates the per-block scratch buffers with make(), which Go's
//     escape analysis handles equivalently.
//   - RTC_LOG_V gain-change-detected logging: debug only.
//   - RTC_DCHECK assertions: debug-only, no numerical effect.
package aec3

import (
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
)

// linearEchoPower computes the linear echo power spectrum S2 from
// the refined-filter FFT output E and the microphone FFT signal Y. C:
// (anonymous)'s LinearEchoPower.
func linearEchoPower(E, Y *FFTData, S2 *[FFTLengthBy2Plus1]float32) {
	for k := 0; k < FFTLengthBy2Plus1; k++ {
		diffRe := sub32(Y.Re[k], E.Re[k])
		diffIm := sub32(Y.Im[k], E.Im[k])
		S2[k] = muladd(diffRe, diffRe, diffIm, diffIm)
	}
}

// signalTransition fades between two input signals (from, to) using a
// fixed-size transition, writing the result into out. If from and to
// reference the exact same underlying array (mirroring C++'s
// rtc::ArrayView equality, which compares data pointer and size), the
// result is copied directly with no fade, matching upstream exactly
// (the fade formula is not guaranteed bit-exact to a plain copy). C:
// (anonymous)'s SignalTransition.
func signalTransition(from, to, out []float32) {
	if len(from) == len(to) && (len(from) == 0 || &from[0] == &to[0]) {
		copy(out, to)
		return
	}

	const kTransitionSize = 30
	const kOneByTransitionSizePlusOne = 1.0 / (kTransitionSize + 1)

	for k := 0; k < kTransitionSize; k++ {
		a := mul32(float32(k+1), kOneByTransitionSizePlusOne)
		out[k] = muladd(a, to[k], sub32(1, a), from[k])
	}
	copy(out[kTransitionSize:], to[kTransitionSize:])
}

// windowedPaddedFft computes a windowed (square root Hanning) padded
// FFT and updates the related memory. C: (anonymous)'s
// WindowedPaddedFft.
func windowedPaddedFft(fft *Aec3FFT, v, vOld []float32, V *FFTData) {
	fft.PaddedFft(v, vOld, WindowSqrtHanning, V)
	copy(vOld, v)
}

// Metrics mirrors EchoControl::Metrics. C: api/audio/echo_control.h's
// EchoControl::Metrics. EchoReturnLoss/EchoReturnLossEnhancement are
// populated by EchoRemover.GetMetrics; DelayMs is populated by
// BlockProcessor.GetMetrics (block_processor.go), matching upstream's
// split between EchoRemoverImpl::GetMetrics and
// BlockProcessorImpl::GetMetrics, which write into the same
// EchoControl::Metrics struct from two different classes.
type Metrics struct {
	EchoReturnLoss            float64
	EchoReturnLossEnhancement float64
	DelayMs                   int
}

// EchoRemover removes the echo from the capture signal. C:
// echo_remover.{h,cc}'s EchoRemover/EchoRemoverImpl.
type EchoRemover struct {
	config                config.Config
	fft                   *Aec3FFT
	sampleRateHz          int
	numRenderChannels     int
	numCaptureChannels    int
	useCoarseFilterOutput bool
	subtractor            *Subtractor
	suppressionGain       *SuppressionGain
	cng                   *ComfortNoiseGenerator
	suppressionFilter     *SuppressionFilter
	renderSignalAnalyzer  *RenderSignalAnalyzer
	residualEchoEstimator *ResidualEchoEstimator
	echoLeakageDetected   bool
	captureOutputUsed     bool
	aecState              *AecState
	metrics               *EchoRemoverMetrics
	suppressorGate        *suppressorGate
	eOld                  [][FFTLengthBy2]float32
	yOld                  [][FFTLengthBy2]float32
	blockCounter          int
	gainChangeHangover    int

	refinedFilterOutputLastSelected bool
}

// NewEchoRemover mirrors EchoRemoverImpl's C++ constructor
// (EchoRemover::Create + EchoRemoverImpl's constructor).
func NewEchoRemover(config config.Config, sampleRateHz, numRenderChannels, numCaptureChannels int) *EchoRemover {
	r := &EchoRemover{
		config:                          config,
		fft:                             NewAec3FFT(),
		sampleRateHz:                    sampleRateHz,
		numRenderChannels:               numRenderChannels,
		numCaptureChannels:              numCaptureChannels,
		useCoarseFilterOutput:           config.Filter.EnableCoarseFilterOutputUsage,
		subtractor:                      NewSubtractor(config, numRenderChannels, numCaptureChannels),
		suppressionGain:                 NewSuppressionGain(config, numCaptureChannels),
		cng:                             NewComfortNoiseGenerator(config, numCaptureChannels),
		suppressionFilter:               NewSuppressionFilter(sampleRateHz, numCaptureChannels),
		renderSignalAnalyzer:            NewRenderSignalAnalyzer(config),
		residualEchoEstimator:           NewResidualEchoEstimator(config, numRenderChannels),
		captureOutputUsed:               true,
		aecState:                        NewAecState(config, numCaptureChannels),
		metrics:                         NewEchoRemoverMetrics(),
		suppressorGate:                  newSuppressorGate(),
		eOld:                            make([][FFTLengthBy2]float32, numCaptureChannels),
		yOld:                            make([][FFTLengthBy2]float32, numCaptureChannels),
		refinedFilterOutputLastSelected: true,
	}
	return r
}

// GetMetrics reads the current metrics. C: EchoRemoverImpl::GetMetrics.
func (r *EchoRemover) GetMetrics(metrics *Metrics) {
	// Echo return loss (ERL) is inverted to go from gain to attenuation.
	// C++'s std::log10(aec_state_.ErlTimeDomain()) resolves to the
	// float overload (ErlTimeDomain() returns float), rounding to
	// float32 precision *before* widening to double for the -10.0
	// multiply; mirror that explicitly, since math.Log10 alone would
	// compute the log in float64 precision and produce a different
	// (more precise, but non-bit-exact) result.
	metrics.EchoReturnLoss = -10.0 * float64(float32(math.Log10(float64(r.aecState.ErlTimeDomain()))))
	metrics.EchoReturnLossEnhancement = float64(Log2TodB(r.aecState.FullBandErleLog2()))
}

// UpdateEchoLeakageStatus updates the status on whether echo leakage
// is detected in the output of the echo remover. C:
// EchoRemoverImpl::UpdateEchoLeakageStatus.
func (r *EchoRemover) UpdateEchoLeakageStatus(leakageDetected bool) {
	r.echoLeakageDetected = leakageDetected
}

// SetCaptureOutputUsage specifies whether the capture output will be
// used. C: EchoRemoverImpl::SetCaptureOutputUsage.
func (r *EchoRemover) SetCaptureOutputUsage(captureOutputUsed bool) {
	r.captureOutputUsed = captureOutputUsed
}

// SetSuppressorGating selects whether the suppressor is gated on
// demonstrated convergence. Go-port-only, beyond upstream; see
// suppressor_gate.go.
func (r *EchoRemover) SetSuppressorGating(mode SuppressorGating) {
	r.suppressorGate.SetGating(mode)
}

// SuppressorGate reports whether the suppressor is currently allowed
// to attenuate the capture path. Go-port-only, beyond upstream; see
// suppressor_gate.go.
func (r *EchoRemover) SuppressorGate() SuppressorGateState {
	return r.suppressorGate.State()
}

// formLinearFilterOutput selects which of the coarse and refined
// linear filter outputs is most appropriate to pass to the suppressor
// and forms the linear filter output by smoothly transitioning
// between those. C: EchoRemoverImpl::FormLinearFilterOutput.
func (r *EchoRemover) formLinearFilterOutput(subtractorOutput *SubtractorOutput, output []float32) {
	useRefinedOutput := true
	if r.useCoarseFilterOutput {
		// As the output of the refined adaptive filter generally should
		// be better than the coarse filter output, add a margin and
		// threshold for when choosing the coarse filter output.
		if subtractorOutput.E2CoarseScalar < mul32(0.9, subtractorOutput.E2RefinedScalar) &&
			subtractorOutput.Y2 > 30.0*30.0*BlockSize &&
			(subtractorOutput.S2Refined > 60.0*60.0*BlockSize || subtractorOutput.S2Coarse > 60.0*60.0*BlockSize) {
			useRefinedOutput = false
		} else {
			// If the refined filter is diverged, choose the filter output
			// that has the lowest power.
			if subtractorOutput.E2CoarseScalar < subtractorOutput.E2RefinedScalar &&
				subtractorOutput.Y2 < subtractorOutput.E2RefinedScalar {
				useRefinedOutput = false
			}
		}
	}

	from := subtractorOutput.ECoarseTime[:]
	if r.refinedFilterOutputLastSelected {
		from = subtractorOutput.ERefinedTime[:]
	}
	to := subtractorOutput.ECoarseTime[:]
	if useRefinedOutput {
		to = subtractorOutput.ERefinedTime[:]
	}
	signalTransition(from, to, output)
	r.refinedFilterOutputLastSelected = useRefinedOutput
}

// ProcessCapture removes the echo from a block of samples from the
// capture signal. The supplied render signal is assumed to be
// pre-aligned with the capture signal. linearOutput may be nil. C:
// EchoRemoverImpl::ProcessCapture.
func (r *EchoRemover) ProcessCapture(echoPathVariability EchoPathVariability, captureSignalSaturation bool, externalDelay *DelayEstimate, renderBuffer *RenderBuffer, linearOutput, capture *Block) {
	r.blockCounter++
	x := renderBuffer.GetBlock(0)
	y := capture

	e := make([][FFTLengthBy2]float32, r.numCaptureChannels)
	Y2 := make([][FFTLengthBy2Plus1]float32, r.numCaptureChannels)
	E2 := make([][FFTLengthBy2Plus1]float32, r.numCaptureChannels)
	R2 := make([][FFTLengthBy2Plus1]float32, r.numCaptureChannels)
	R2Unbounded := make([][FFTLengthBy2Plus1]float32, r.numCaptureChannels)
	S2Linear := make([][FFTLengthBy2Plus1]float32, r.numCaptureChannels)
	Y := make([]FFTData, r.numCaptureChannels)
	E := make([]FFTData, r.numCaptureChannels)
	comfortNoise := make([]FFTData, r.numCaptureChannels)
	highBandComfortNoise := make([]FFTData, r.numCaptureChannels)
	subtractorOutput := make([]SubtractorOutput, r.numCaptureChannels)

	r.aecState.UpdateCaptureSaturation(captureSignalSaturation)

	if echoPathVariability.AudioPathChanged() {
		// Ensure that the gain change is only acted on once per frame.
		if echoPathVariability.GainChange {
			if r.gainChangeHangover == 0 {
				const kMaxBlocksPerFrame = 3
				r.gainChangeHangover = kMaxBlocksPerFrame
			} else {
				echoPathVariability.GainChange = false
			}
		}

		r.subtractor.HandleEchoPathChange(echoPathVariability)
		r.aecState.HandleEchoPathChange(echoPathVariability)

		if echoPathVariability.DelayChange != DelayAdjustmentNone {
			r.suppressionGain.SetInitialState(true)
		}
	}
	if r.gainChangeHangover > 0 {
		r.gainChangeHangover--
	}

	// Analyze the render signal. C++ passes AecState::MinDirectPathFilterDelay()
	// (a plain int) where std::optional<size_t> is expected, which implicitly
	// converts to a non-empty optional; mirror that with a non-nil pointer to
	// a local copy of the value.
	minDirectPathFilterDelay := r.aecState.MinDirectPathFilterDelay()
	r.renderSignalAnalyzer.Update(renderBuffer, &minDirectPathFilterDelay)

	// State transition.
	if r.aecState.TransitionTriggered() {
		r.subtractor.ExitInitialState()
		r.suppressionGain.SetInitialState(false)
	}

	// Perform linear echo cancellation.
	r.subtractor.Process(renderBuffer, y, r.renderSignalAnalyzer, r.aecState, subtractorOutput)

	// Compute spectra.
	for ch := 0; ch < r.numCaptureChannels; ch++ {
		r.formLinearFilterOutput(&subtractorOutput[ch], e[ch][:])
		windowedPaddedFft(r.fft, y.View(0, ch), r.yOld[ch][:], &Y[ch])
		windowedPaddedFft(r.fft, e[ch][:], r.eOld[ch][:], &E[ch])
		linearEchoPower(&E[ch], &Y[ch], &S2Linear[ch])
		Y[ch].Spectrum(Y2[ch][:])
		E[ch].Spectrum(E2[ch][:])
	}

	// Optionally return the linear filter output.
	if linearOutput != nil {
		for ch := 0; ch < r.numCaptureChannels; ch++ {
			copy(linearOutput.View(0, ch), e[ch][:])
		}
	}

	// Update the AEC state information.
	r.aecState.Update(externalDelay, r.subtractor.FilterFrequencyResponses(), r.subtractor.FilterImpulseResponses(), renderBuffer, E2, Y2, subtractorOutput)

	// Choose the linear output.
	YFft := Y
	if r.aecState.UseLinearFilterOutput() {
		YFft = E
	}

	// Estimate the comfort noise.
	r.cng.Compute(r.aecState.SaturatedCapture(), Y2, comfortNoise, highBandComfortNoise)

	// Only do the below processing if the output of the audio
	// processing module is used.
	var G [FFTLengthBy2Plus1]float32
	if r.captureOutputUsed {
		// Estimate the residual echo power.
		r.residualEchoEstimator.Estimate(r.aecState, renderBuffer, S2Linear, Y2, r.suppressionGain.IsDominantNearend(), R2, R2Unbounded)

		// Suppressor nearend estimate.
		if r.aecState.UsableLinearEstimate() {
			// E2 is bound by Y2.
			for ch := 0; ch < r.numCaptureChannels; ch++ {
				for k := range E2[ch] {
					if Y2[ch][k] < E2[ch][k] {
						E2[ch][k] = Y2[ch][k]
					}
				}
			}
		}
		nearendSpectrum := Y2
		if r.aecState.UsableLinearEstimate() {
			nearendSpectrum = E2
		}

		// Suppressor echo estimate.
		echoSpectrum := R2
		if r.aecState.UsableLinearEstimate() {
			echoSpectrum = S2Linear
		}

		// Determine if the suppressor should assume clock drift.
		clockDrift := r.config.EchoRemovalControl.HasClockDrift || echoPathVariability.ClockDrift

		// Compute preferred gains.
		var highBandsGain float32
		r.suppressionGain.GetGain(nearendSpectrum, echoSpectrum, R2, R2Unbounded, r.cng.NoiseSpectrum(), r.renderSignalAnalyzer, r.aecState, x, clockDrift, &highBandsGain, &G)

		// Relax the gains towards unity while the canceller has not
		// shown that it is modelling a real echo path. A no-op unless
		// the caller opted in (see suppressor_gate.go). Applied to the
		// suppressor's output rather than its state, so the suppressor
		// keeps adapting and is already tracking when the gate engages.
		r.suppressorGate.Update(r.aecState, externalDelay)
		r.suppressorGate.Apply(&G, &highBandsGain)

		r.suppressionFilter.ApplyGain(comfortNoise, highBandComfortNoise, G, highBandsGain, YFft, y)
	} else {
		for k := range G {
			G[k] = 0
		}
	}

	// Update the metrics.
	r.metrics.Update(r.aecState, r.cng.NoiseSpectrum()[0], G)
}
