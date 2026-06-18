package mixer

import "math"

// floatBits and bitsFloat alias math.Float64bits and math.Float64frombits
// so track.go can store gain in an atomic.Uint64 without a direct math
// import. Kept in a tiny file to make the intent obvious.
func floatBits(f float64) uint64 { return math.Float64bits(f) }
func bitsFloat(u uint64) float64 { return math.Float64frombits(u) }
