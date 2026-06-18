package mutations

import "math"

// Decibels converts a dB value to a linear amplitude multiplier using
// the 20*log10 amplitude convention: 0 dB → 1.0, -6 dB → 0.5,
// -20 dB → 0.1, -∞ dB → 0.
func Decibels(db float64) float64 {
	return math.Pow(10, db/20)
}

// AmplitudeToDecibels converts a linear amplitude multiplier to dB.
// A value of 0 or less returns math.Inf(-1); callers that need a
// representable floor should clamp before calling.
func AmplitudeToDecibels(amp float64) float64 {
	if amp <= 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(amp)
}
