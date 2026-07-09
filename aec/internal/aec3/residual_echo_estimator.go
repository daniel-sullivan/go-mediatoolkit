// This file ports modules/audio_processing/aec3/
// residual_echo_estimator.{h,cc}: ResidualEchoEstimator, which
// estimates the power spectrum of the residual echo remaining after
// echo removal.
//
// Dropped per this port's conventions (see aec_state.go's top
// comment for the established "no field-trial system" convention,
// under which every field_trial::IsEnabled(...) gate defaults to
// disabled):
//   - GetTransparentModeGain always returns kDefaultTransparentModeGain
//     (the field-trial gates it checks don't exist in this port).
//   - GetEarlyReflectionsDefaultModeGain/
//     GetLateReflectionsDefaultModeGain collapse to
//     config.EpStrength.DefaultGain (their field-trial branch is
//     unreachable).
//   - UseErleOnsetCompensationInDominantNearend collapses to
//     config.EpStrength.ErleOnsetCompensationInDominantNearend (ORed
//     with an always-false field-trial gate).
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// kDefaultTransparentModeGain == kDefaultTransparentModeGain.
const kDefaultTransparentModeGain = 0.01

// reverbType mirrors ResidualEchoEstimator::ReverbType.
type reverbType int

const (
	reverbTypeLinear reverbType = iota
	reverbTypeNonLinear
)

// ResidualEchoEstimator estimates the power spectrum of the residual
// echo. C: residual_echo_estimator.{h,cc}'s ResidualEchoEstimator.
type ResidualEchoEstimator struct {
	config                                 config.Config
	numRenderChannels                      int
	earlyReflectionsTransparentModeGain    float32
	lateReflectionsTransparentModeGain     float32
	earlyReflectionsGeneralGain            float32
	lateReflectionsGeneralGain             float32
	erleOnsetCompensationInDominantNearend bool
	x2NoiseFloor                           [FFTLengthBy2Plus1]float32
	x2NoiseFloorCounter                    [FFTLengthBy2Plus1]int
	echoReverb                             ReverbModel
}

// NewResidualEchoEstimator mirrors ResidualEchoEstimator's C++
// constructor.
func NewResidualEchoEstimator(config config.Config, numRenderChannels int) *ResidualEchoEstimator {
	r := &ResidualEchoEstimator{
		config:                                 config,
		numRenderChannels:                      numRenderChannels,
		earlyReflectionsTransparentModeGain:    kDefaultTransparentModeGain,
		lateReflectionsTransparentModeGain:     kDefaultTransparentModeGain,
		earlyReflectionsGeneralGain:            config.EpStrength.DefaultGain,
		lateReflectionsGeneralGain:             config.EpStrength.DefaultGain,
		erleOnsetCompensationInDominantNearend: config.EpStrength.ErleOnsetCompensationInDominantNearend,
	}
	r.reset()
	return r
}

func (r *ResidualEchoEstimator) reset() {
	r.echoReverb.Reset()
	for k := range r.x2NoiseFloorCounter {
		r.x2NoiseFloorCounter[k] = r.config.EchoModel.NoiseFloorHold
	}
	for k := range r.x2NoiseFloor {
		r.x2NoiseFloor[k] = r.config.EchoModel.MinNoiseFloorPower
	}
}

// getRenderIndexesToAnalyze computes the [idxStart, idxStop) indexes
// used for computing spectral power over the blocks surrounding the
// delay. C: (anonymous namespace)'s GetRenderIndexesToAnalyze.
func getRenderIndexesToAnalyze(spectrumBuffer *SpectrumBuffer, echoModel config.EchoModelConfig, filterDelayBlocks int) (idxStart, idxStop int) {
	windowStart := filterDelayBlocks - echoModel.RenderPreWindowSize
	if windowStart < 0 {
		windowStart = 0
	}
	windowEnd := filterDelayBlocks + echoModel.RenderPostWindowSize
	idxStart = spectrumBuffer.OffsetIndex(spectrumBuffer.Read, windowStart)
	idxStop = spectrumBuffer.OffsetIndex(spectrumBuffer.Read, windowEnd+1)
	return idxStart, idxStop
}

// linearEstimate estimates the residual echo power based on the ERLE
// and the linear power estimate. C: (anonymous namespace)'s
// LinearEstimate.
func linearEstimate(s2Linear [][FFTLengthBy2Plus1]float32, erle [][FFTLengthBy2Plus1]float32, r2 [][FFTLengthBy2Plus1]float32) {
	numCaptureChannels := len(r2)
	for ch := 0; ch < numCaptureChannels; ch++ {
		for k := 0; k < FFTLengthBy2Plus1; k++ {
			r2[ch][k] = s2Linear[ch][k] / erle[ch][k]
		}
	}
}

// nonLinearEstimate estimates the residual echo power based on the
// estimate of the echo path gain. C: (anonymous namespace)'s
// NonLinearEstimate.
func nonLinearEstimate(echoPathGain float32, x2 [FFTLengthBy2Plus1]float32, r2 [][FFTLengthBy2Plus1]float32) {
	numCaptureChannels := len(r2)
	for ch := 0; ch < numCaptureChannels; ch++ {
		for k := 0; k < FFTLengthBy2Plus1; k++ {
			r2[ch][k] = mul32(x2[k], echoPathGain)
		}
	}
}

// applyNoiseGate applies a soft noise gate to the echo generating
// power. C: (anonymous namespace)'s ApplyNoiseGate.
func applyNoiseGate(config config.EchoModelConfig, x2 *[FFTLengthBy2Plus1]float32) {
	for k := 0; k < FFTLengthBy2Plus1; k++ {
		if config.NoiseGatePower > x2[k] {
			v := sub32(x2[k], mul32(config.NoiseGateSlope, sub32(config.NoiseGatePower, x2[k])))
			if v < 0 {
				v = 0
			}
			x2[k] = v
		}
	}
}

// echoGeneratingPower estimates the echo generating signal power as
// gated maximal power over a time window. C: (anonymous namespace)'s
// EchoGeneratingPower.
func echoGeneratingPower(numRenderChannels int, spectrumBuffer *SpectrumBuffer, echoModel config.EchoModelConfig, filterDelayBlocks int, x2 *[FFTLengthBy2Plus1]float32) {
	idxStart, idxStop := getRenderIndexesToAnalyze(spectrumBuffer, echoModel, filterDelayBlocks)

	for k := range x2 {
		x2[k] = 0
	}
	if numRenderChannels == 1 {
		for k := idxStart; k != idxStop; k = spectrumBuffer.IncIndex(k) {
			for j := 0; j < FFTLengthBy2Plus1; j++ {
				if spectrumBuffer.Buffer[k][0][j] > x2[j] {
					x2[j] = spectrumBuffer.Buffer[k][0][j]
				}
			}
		}
	} else {
		for k := idxStart; k != idxStop; k = spectrumBuffer.IncIndex(k) {
			var renderPower [FFTLengthBy2Plus1]float32
			for ch := 0; ch < numRenderChannels; ch++ {
				channelPower := spectrumBuffer.Buffer[k][ch]
				for j := 0; j < FFTLengthBy2Plus1; j++ {
					renderPower[j] = add32(renderPower[j], channelPower[j])
				}
			}
			for j := 0; j < FFTLengthBy2Plus1; j++ {
				if renderPower[j] > x2[j] {
					x2[j] = renderPower[j]
				}
			}
		}
	}
}

// Estimate estimates the residual echo power. C:
// ResidualEchoEstimator::Estimate.
func (r *ResidualEchoEstimator) Estimate(aecState *AecState, renderBuffer *RenderBuffer, s2Linear [][FFTLengthBy2Plus1]float32, y2 [][FFTLengthBy2Plus1]float32, dominantNearend bool, r2 [][FFTLengthBy2Plus1]float32, r2Unbounded [][FFTLengthBy2Plus1]float32) {
	numCaptureChannels := len(r2)

	// Estimate the power of the stationary noise in the render signal.
	r.updateRenderNoisePower(renderBuffer)

	// Estimate the residual echo power.
	if aecState.UsableLinearEstimate() {
		// When there is saturated echo, assume the same spectral content as
		// is present in the microphone signal.
		if aecState.SaturatedEcho() {
			for ch := 0; ch < numCaptureChannels; ch++ {
				r2[ch] = y2[ch]
				r2Unbounded[ch] = y2[ch]
			}
		} else {
			onsetCompensated := r.erleOnsetCompensationInDominantNearend || !dominantNearend
			erle := aecState.Erle(onsetCompensated)
			linearEstimate(s2Linear, erle, r2)
			erleUnbounded := aecState.ErleUnbounded()
			linearEstimate(s2Linear, erleUnbounded, r2Unbounded)
		}

		r.updateReverb(reverbTypeLinear, aecState, renderBuffer, dominantNearend)
		r.addReverb(r2)
		r.addReverb(r2Unbounded)
	} else {
		echoPathGain := r.getEchoPathGain(aecState, true)

		// When there is saturated echo, assume the same spectral content as
		// is present in the microphone signal.
		if aecState.SaturatedEcho() {
			for ch := 0; ch < numCaptureChannels; ch++ {
				r2[ch] = y2[ch]
				r2Unbounded[ch] = y2[ch]
			}
		} else {
			// Estimate the echo generating signal power.
			var x2 [FFTLengthBy2Plus1]float32
			echoGeneratingPower(r.numRenderChannels, renderBuffer.GetSpectrumBuffer(), r.config.EchoModel, aecState.MinDirectPathFilterDelay(), &x2)
			if !aecState.UseStationarityProperties() {
				applyNoiseGate(r.config.EchoModel, &x2)
			}

			// Subtract the stationary noise power to avoid stationary noise
			// causing excessive echo suppression.
			for k := 0; k < FFTLengthBy2Plus1; k++ {
				v := sub32(x2[k], mul32(r.config.EchoModel.StationaryGateSlope, r.x2NoiseFloor[k]))
				if v < 0 {
					v = 0
				}
				x2[k] = v
			}

			nonLinearEstimate(echoPathGain, x2, r2)
			nonLinearEstimate(echoPathGain, x2, r2Unbounded)
		}

		if r.config.EchoModel.ModelReverbInNonlinearMode && !aecState.TransparentModeActive() {
			r.updateReverb(reverbTypeNonLinear, aecState, renderBuffer, dominantNearend)
			r.addReverb(r2)
			r.addReverb(r2Unbounded)
		}
	}

	if aecState.UseStationarityProperties() {
		// Scale the echo according to echo audibility.
		var residualScaling [FFTLengthBy2Plus1]float32
		aecState.GetResidualEchoScaling(residualScaling[:])
		for ch := 0; ch < numCaptureChannels; ch++ {
			for k := 0; k < FFTLengthBy2Plus1; k++ {
				r2[ch][k] = mul32(r2[ch][k], residualScaling[k])
				r2Unbounded[ch][k] = mul32(r2Unbounded[ch][k], residualScaling[k])
			}
		}
	}
}

// updateRenderNoisePower updates the estimate for the power of the
// stationary noise component in the render signal. C:
// ResidualEchoEstimator::UpdateRenderNoisePower.
func (r *ResidualEchoEstimator) updateRenderNoisePower(renderBuffer *RenderBuffer) {
	x2 := renderBuffer.Spectrum(0)
	renderPower := x2[0]
	if r.numRenderChannels > 1 {
		var renderPowerData [FFTLengthBy2Plus1]float32
		for ch := 0; ch < r.numRenderChannels; ch++ {
			channelPower := x2[ch]
			for k := 0; k < FFTLengthBy2Plus1; k++ {
				renderPowerData[k] = add32(renderPowerData[k], channelPower[k])
			}
		}
		renderPower = renderPowerData[:]
	}

	// Estimate the stationary noise power in a minimum statistics manner.
	for k := 0; k < FFTLengthBy2Plus1; k++ {
		// Decrease rapidly.
		if renderPower[k] < r.x2NoiseFloor[k] {
			r.x2NoiseFloor[k] = renderPower[k]
			r.x2NoiseFloorCounter[k] = 0
		} else {
			// Increase in a delayed, leaky manner.
			if r.x2NoiseFloorCounter[k] >= r.config.EchoModel.NoiseFloorHold {
				v := mul32(r.x2NoiseFloor[k], 1.1)
				if v < r.config.EchoModel.MinNoiseFloorPower {
					v = r.config.EchoModel.MinNoiseFloorPower
				}
				r.x2NoiseFloor[k] = v
			} else {
				r.x2NoiseFloorCounter[k]++
			}
		}
	}
}

// updateReverb updates the reverb estimation. C:
// ResidualEchoEstimator::UpdateReverb.
func (r *ResidualEchoEstimator) updateReverb(rt reverbType, aecState *AecState, renderBuffer *RenderBuffer, dominantNearend bool) {
	// Choose reverb partition based on what type of echo power model is
	// used.
	var firstReverbPartition int
	if rt == reverbTypeLinear {
		firstReverbPartition = aecState.FilterLengthBlocks() + 1
	} else {
		firstReverbPartition = aecState.MinDirectPathFilterDelay() + 1
	}

	// Compute render power for the reverb.
	x2 := renderBuffer.Spectrum(firstReverbPartition)
	renderPower := x2[0]
	if r.numRenderChannels > 1 {
		var renderPowerData [FFTLengthBy2Plus1]float32
		for ch := 0; ch < r.numRenderChannels; ch++ {
			channelPower := x2[ch]
			for k := 0; k < FFTLengthBy2Plus1; k++ {
				renderPowerData[k] = add32(renderPowerData[k], channelPower[k])
			}
		}
		renderPower = renderPowerData[:]
	}

	// Update the reverb estimate.
	reverbDecay := aecState.ReverbDecay(dominantNearend)
	if rt == reverbTypeLinear {
		r.echoReverb.UpdateReverb(renderPower, aecState.GetReverbFrequencyResponse(), reverbDecay)
	} else {
		echoPathGain := r.getEchoPathGain(aecState, false)
		r.echoReverb.UpdateReverbNoFreqShaping(renderPower, echoPathGain, reverbDecay)
	}
}

// addReverb adds the estimated power of the reverb to the residual
// echo power. C: ResidualEchoEstimator::AddReverb.
func (r *ResidualEchoEstimator) addReverb(r2 [][FFTLengthBy2Plus1]float32) {
	numCaptureChannels := len(r2)
	reverbPower := r.echoReverb.Reverb()
	for ch := 0; ch < numCaptureChannels; ch++ {
		for k := 0; k < FFTLengthBy2Plus1; k++ {
			r2[ch][k] = add32(r2[ch][k], reverbPower[k])
		}
	}
}

// getEchoPathGain chooses the echo path gain to use. C:
// ResidualEchoEstimator::GetEchoPathGain.
func (r *ResidualEchoEstimator) getEchoPathGain(aecState *AecState, gainForEarlyReflections bool) float32 {
	var gainAmplitude float32
	if aecState.TransparentModeActive() {
		if gainForEarlyReflections {
			gainAmplitude = r.earlyReflectionsTransparentModeGain
		} else {
			gainAmplitude = r.lateReflectionsTransparentModeGain
		}
	} else {
		if gainForEarlyReflections {
			gainAmplitude = r.earlyReflectionsGeneralGain
		} else {
			gainAmplitude = r.lateReflectionsGeneralGain
		}
	}
	return mul32(gainAmplitude, gainAmplitude)
}
