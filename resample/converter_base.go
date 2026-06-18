package resample

import "math"

// converterBase holds state shared by the simple converters (ZOH and Linear).
type converterBase struct {
	channels     int
	dirty        bool
	lastValue    []float64 // per-channel, length == channels
	lastRatio    float64
	lastPosition float64
}

func newConverterBase(channels int) converterBase {
	return converterBase{
		channels:  channels,
		lastValue: make([]float64, channels),
	}
}

func (b *converterBase) reset() {
	b.dirty = false
	b.lastRatio = 0
	b.lastPosition = 0
	for i := range b.lastValue {
		b.lastValue[i] = 0
	}
}

func (b *converterBase) setRatio(ratio Ratio) error {
	r := ratio.Float64()
	if !isValidRatioF(r) {
		return ErrBadSrcRatio
	}
	b.lastRatio = r
	return nil
}

// lrint rounds to nearest integer with ties to even, matching C's lrint().
func lrint(x float64) int {
	return int(math.RoundToEven(x))
}

// fmodOne returns the fractional part of x, matching the C library's fmod_one.
// Result is in [0, 1).
func fmodOne(x float64) float64 {
	res := x - math.RoundToEven(x)
	if res < 0.0 {
		return res + 1.0
	}
	return res
}
