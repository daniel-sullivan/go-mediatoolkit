// This file ports modules/audio_processing/aec3/
// subband_nearend_detector.{h,cc}: SubbandNearendDetector, the
// opt-in (config.Suppressor.UseSubbandNearendDetection) NearendDetector
// implementation.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// SubbandNearendDetector selects whether the suppressor is in the
// nearend or echo state, based on a comparison of nearend power in
// two configured subbands. C: subband_nearend_detector.{h,cc}'s
// SubbandNearendDetector.
type SubbandNearendDetector struct {
	config                config.SubbandNearendDetectionConfig
	numCaptureChannels    int
	nearendSmoothers      []*MovingAverage
	oneOverSubbandLength1 float32
	oneOverSubbandLength2 float32
	nearendState          bool
}

// NewSubbandNearendDetector mirrors SubbandNearendDetector's C++
// constructor.
func NewSubbandNearendDetector(config config.SubbandNearendDetectionConfig, numCaptureChannels int) *SubbandNearendDetector {
	smoothers := make([]*MovingAverage, numCaptureChannels)
	for ch := range smoothers {
		smoothers[ch] = NewMovingAverage(FFTLengthBy2Plus1, config.NearendAverageBlocks)
	}
	return &SubbandNearendDetector{
		config:                config,
		numCaptureChannels:    numCaptureChannels,
		nearendSmoothers:      smoothers,
		oneOverSubbandLength1: 1.0 / float32(config.Subband1.High-config.Subband1.Low+1),
		oneOverSubbandLength2: 1.0 / float32(config.Subband2.High-config.Subband2.Low+1),
	}
}

// IsNearendState returns whether the current state is the nearend
// state. C: SubbandNearendDetector::IsNearendState.
func (s *SubbandNearendDetector) IsNearendState() bool { return s.nearendState }

// Update updates the state selection based on the latest spectral
// estimates. C: SubbandNearendDetector::Update.
func (s *SubbandNearendDetector) Update(nearendSpectrum, residualEchoSpectrum, comfortNoiseSpectrum [][FFTLengthBy2Plus1]float32, initialState bool) {
	s.nearendState = false
	for ch := 0; ch < s.numCaptureChannels; ch++ {
		noise := comfortNoiseSpectrum[ch]
		var nearend [FFTLengthBy2Plus1]float32
		in := nearendSpectrum[ch]
		s.nearendSmoothers[ch].Average(in[:], nearend[:])

		// Noise power of the first region.
		var noiseSum float32
		for k := s.config.Subband1.Low; k <= s.config.Subband1.High; k++ {
			noiseSum += noise[k]
		}
		noisePower := mul32(noiseSum, s.oneOverSubbandLength1)

		// Nearend power of the first region.
		var nearendSum1 float32
		for k := s.config.Subband1.Low; k <= s.config.Subband1.High; k++ {
			nearendSum1 += nearend[k]
		}
		nearendPowerSubband1 := mul32(nearendSum1, s.oneOverSubbandLength1)

		// Nearend power of the second region.
		var nearendSum2 float32
		for k := s.config.Subband2.Low; k <= s.config.Subband2.High; k++ {
			nearendSum2 += nearend[k]
		}
		nearendPowerSubband2 := mul32(nearendSum2, s.oneOverSubbandLength2)

		// One channel is sufficient to trigger nearend state.
		s.nearendState = s.nearendState ||
			(nearendPowerSubband1 < mul32(s.config.NearendThreshold, nearendPowerSubband2) &&
				nearendPowerSubband1 > mul32(s.config.SnrThreshold, noisePower))
	}
}
