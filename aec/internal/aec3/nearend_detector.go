// This file ports modules/audio_processing/aec3/nearend_detector.h:
// NearendDetector, the interface implemented by
// DominantNearendDetector and SubbandNearendDetector for selecting
// whether the suppressor is in the nearend or echo state.
package aec3

// NearendDetector selects whether the suppressor is in the nearend or
// echo state. C: nearend_detector.h's NearendDetector.
type NearendDetector interface {
	// IsNearendState returns whether the current state is the nearend
	// state. C: NearendDetector::IsNearendState.
	IsNearendState() bool

	// Update updates the state selection based on the latest spectral
	// estimates. C: NearendDetector::Update.
	Update(nearendSpectrum, residualEchoSpectrum, comfortNoiseSpectrum [][FFTLengthBy2Plus1]float32, initialState bool)
}
