// This file ports modules/audio_processing/aec3/
// reverb_model_estimator.{h,cc}: ReverbModelEstimator, which drives
// the per-capture-channel ReverbDecayEstimator and
// ReverbFrequencyResponse instances.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// ReverbModelEstimator estimates the model parameters for the
// reverberant echo. C: reverb_model_estimator.{h,cc}'s
// ReverbModelEstimator.
type ReverbModelEstimator struct {
	reverbDecayEstimators    []*ReverbDecayEstimator
	reverbFrequencyResponses []*ReverbFrequencyResponse
}

// NewReverbModelEstimator constructs a ReverbModelEstimator with one
// decay estimator and frequency response per capture channel. C:
// ReverbModelEstimator::ReverbModelEstimator.
func NewReverbModelEstimator(config config.Config, numCaptureChannels int) *ReverbModelEstimator {
	e := &ReverbModelEstimator{
		reverbDecayEstimators:    make([]*ReverbDecayEstimator, numCaptureChannels),
		reverbFrequencyResponses: make([]*ReverbFrequencyResponse, numCaptureChannels),
	}
	for ch := 0; ch < numCaptureChannels; ch++ {
		e.reverbDecayEstimators[ch] = NewReverbDecayEstimator(config)
		e.reverbFrequencyResponses[ch] = NewReverbFrequencyResponse(config.EpStrength.UseConservativeTailFrequencyResponse)
	}
	return e
}

// Update updates the estimates based on new data. linearFilterQualities
// entries are nil to mirror std::optional's empty state. C:
// ReverbModelEstimator::Update.
func (e *ReverbModelEstimator) Update(impulseResponses [][]float32, frequencyResponses [][][FFTLengthBy2Plus1]float32, linearFilterQualities []*float32, filterDelaysBlocks []int, usableLinearEstimates []bool, stationaryBlock bool) {
	numCaptureChannels := len(e.reverbDecayEstimators)

	for ch := 0; ch < numCaptureChannels; ch++ {
		// Estimate the frequency response for the reverb.
		e.reverbFrequencyResponses[ch].Update(frequencyResponses[ch], filterDelaysBlocks[ch], linearFilterQualities[ch], stationaryBlock)

		// Estimate the reverb decay.
		e.reverbDecayEstimators[ch].Update(impulseResponses[ch], linearFilterQualities[ch], filterDelaysBlocks[ch], usableLinearEstimates[ch], stationaryBlock)
	}
}

// ReverbDecay returns the exponential decay of the reverberant echo.
// mild indicates which exponential decay to return, the default one
// or a milder one. TODO(peah): Correct to properly support multiple
// channels. C: ReverbModelEstimator::ReverbDecay.
func (e *ReverbModelEstimator) ReverbDecay(mild bool) float32 {
	return e.reverbDecayEstimators[0].Decay(mild)
}

// GetReverbFrequencyResponse returns the frequency response of the
// reverberant echo. TODO(peah): Correct to properly support multiple
// channels. C: ReverbModelEstimator::GetReverbFrequencyResponse.
func (e *ReverbModelEstimator) GetReverbFrequencyResponse() [FFTLengthBy2Plus1]float32 {
	return e.reverbFrequencyResponses[0].FrequencyResponse()
}
