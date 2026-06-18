package mutations

import "math"

// Trim removes samples from the start and/or end of buf where fn returns true.
// It returns a sub-slice of buf (no allocation). The mode controls which end(s)
// are trimmed.
func Trim(buf []float64, mode TrimMode, fn func(sample float64) bool) []float64 {
	if len(buf) == 0 {
		return buf
	}

	start := 0
	end := len(buf)

	if mode == TrimStart || mode == TrimBoth {
		for start < end && fn(buf[start]) {
			start++
		}
	}
	if mode == TrimEnd || mode == TrimBoth {
		for end > start && fn(buf[end-1]) {
			end--
		}
	}

	return buf[start:end]
}

// TrimAll removes all samples where fn returns true, shrinking the buffer.
// Returns a new slice; does not modify the original.
func TrimAll(buf []float64, fn func(sample float64) bool) []float64 {
	out := make([]float64, 0, len(buf))
	for _, v := range buf {
		if !fn(v) {
			out = append(out, v)
		}
	}
	return out
}

// TrimSilence removes silent samples (absolute value <= threshold) from the
// start and/or end of buf. Returns a sub-slice of buf.
func TrimSilence(buf []float64, mode TrimMode, threshold float64) []float64 {
	return Trim(buf, mode, func(s float64) bool {
		return math.Abs(s) <= threshold
	})
}

// TrimSilenceAll removes all silent samples (absolute value <= threshold)
// from buf, regardless of position. Returns a new slice.
func TrimSilenceAll(buf []float64, threshold float64) []float64 {
	return TrimAll(buf, func(s float64) bool {
		return math.Abs(s) <= threshold
	})
}

// TrimMode controls which end(s) of a buffer are trimmed.
type TrimMode int

const (
	TrimStart TrimMode = iota // Trim from the beginning only.
	TrimEnd                   // Trim from the end only.
	TrimBoth                  // Trim from both start and end.
)
