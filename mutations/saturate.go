package mutations

import "math"

// Saturator maps a single sample to a bounded output. Saturators are
// pure functions: no state, no accumulation, callable per sample. They
// are the shaping stage applied after a mix sum to keep the output in
// [-1, 1] without harsh clipping artefacts.
type Saturator func(x float64) float64

// SoftSaturationThreshold is the linear-passthrough limit used by
// SoftSaturate. Below this magnitude the signal is unmodified; above
// it the output is pulled through a tanh tail that asymptotes to ±1.
// 0.8 gives roughly 4dB of headroom before shaping begins.
const SoftSaturationThreshold = 0.8

// SoftSaturate passes |x| ≤ SoftSaturationThreshold through unchanged
// and smoothly maps overshoot toward ±1 via a tanh tail. Typical
// default for a final mix stage.
func SoftSaturate(x float64) float64 {
	const t = SoftSaturationThreshold
	if x > t {
		over := (x - t) / (1 - t)
		return t + (1-t)*math.Tanh(over)
	}
	if x < -t {
		over := (x + t) / (1 - t)
		return -t + (1-t)*math.Tanh(over)
	}
	return x
}

// HardClip clamps x to [-1, 1]. Cheap but harsh — introduces sharp
// harmonic distortion above unity. Useful when correctness (no
// overshoot) matters more than colouration.
func HardClip(x float64) float64 {
	if x > 1 {
		return 1
	}
	if x < -1 {
		return -1
	}
	return x
}

// TanhSaturate applies math.Tanh over the full input range. Smooth
// everywhere and symmetric, but colours low levels too (tanh(0.5) ≈
// 0.46, about 8% below unity). Useful when gentle non-linear warmth
// is desired across the whole signal rather than only on overshoot.
func TanhSaturate(x float64) float64 {
	return math.Tanh(x)
}

// ApplySaturator maps sat over samples in place. A nil sat is a
// no-op.
func ApplySaturator(samples []float64, sat Saturator) {
	if sat == nil {
		return
	}
	for i, v := range samples {
		samples[i] = sat(v)
	}
}
