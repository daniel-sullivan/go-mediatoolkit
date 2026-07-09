// This file ports modules/audio_processing/aec3/
// dominant_nearend_detector.{h,cc}: DominantNearendDetector, the
// default NearendDetector implementation.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// DominantNearendDetector selects whether the suppressor is in the
// nearend or echo state, based on whether the nearend is dominant
// over the echo. C: dominant_nearend_detector.{h,cc}'s
// DominantNearendDetector.
type DominantNearendDetector struct {
	enrThreshold          float32
	enrExitThreshold      float32
	snrThreshold          float32
	holdDuration          int
	triggerThreshold      int
	useDuringInitialPhase bool
	numCaptureChannels    int

	nearendState    bool
	triggerCounters []int
	holdCounters    []int
}

// NewDominantNearendDetector mirrors DominantNearendDetector's C++
// constructor.
func NewDominantNearendDetector(config config.DominantNearendDetectionConfig, numCaptureChannels int) *DominantNearendDetector {
	return &DominantNearendDetector{
		enrThreshold:          config.EnrThreshold,
		enrExitThreshold:      config.EnrExitThreshold,
		snrThreshold:          config.SnrThreshold,
		holdDuration:          config.HoldDuration,
		triggerThreshold:      config.TriggerThreshold,
		useDuringInitialPhase: config.UseDuringInitialPhase,
		numCaptureChannels:    numCaptureChannels,
		triggerCounters:       make([]int, numCaptureChannels),
		holdCounters:          make([]int, numCaptureChannels),
	}
}

// IsNearendState returns whether the current state is the nearend
// state. C: DominantNearendDetector::IsNearendState.
func (d *DominantNearendDetector) IsNearendState() bool { return d.nearendState }

// lowFrequencyEnergy sums spectrum[1:16] (16 exclusive per C++'s
// std::accumulate(spectrum.begin()+1, spectrum.begin()+16, ...)). C:
// (anonymous)'s low_frequency_energy lambda.
func lowFrequencyEnergy(spectrum [FFTLengthBy2Plus1]float32) float32 {
	var sum float32
	for k := 1; k < 16; k++ {
		sum += spectrum[k]
	}
	return sum
}

// Update updates the state selection based on the latest spectral
// estimates. C: DominantNearendDetector::Update.
func (d *DominantNearendDetector) Update(nearendSpectrum, residualEchoSpectrum, comfortNoiseSpectrum [][FFTLengthBy2Plus1]float32, initialState bool) {
	d.nearendState = false

	for ch := 0; ch < d.numCaptureChannels; ch++ {
		neSum := lowFrequencyEnergy(nearendSpectrum[ch])
		echoSum := lowFrequencyEnergy(residualEchoSpectrum[ch])
		noiseSum := lowFrequencyEnergy(comfortNoiseSpectrum[ch])

		// Detect strong active nearend if the nearend is sufficiently
		// stronger than the echo and the nearend noise.
		if (!initialState || d.useDuringInitialPhase) &&
			echoSum < mul32(d.enrThreshold, neSum) &&
			neSum > mul32(d.snrThreshold, noiseSum) {
			d.triggerCounters[ch]++
			if d.triggerCounters[ch] >= d.triggerThreshold {
				// After a period of strong active nearend activity, flag
				// nearend mode.
				d.holdCounters[ch] = d.holdDuration
				d.triggerCounters[ch] = d.triggerThreshold
			}
		} else {
			// Forget previously detected strong active nearend activity.
			d.triggerCounters[ch]--
			if d.triggerCounters[ch] < 0 {
				d.triggerCounters[ch] = 0
			}
		}

		// Exit nearend-state early at strong echo.
		if echoSum > mul32(d.enrExitThreshold, neSum) && echoSum > mul32(d.snrThreshold, noiseSum) {
			d.holdCounters[ch] = 0
		}

		// Remain in any nearend mode for a certain duration.
		d.holdCounters[ch]--
		if d.holdCounters[ch] < 0 {
			d.holdCounters[ch] = 0
		}
		d.nearendState = d.nearendState || d.holdCounters[ch] > 0
	}
}
