// Package mutations provides audio buffer manipulation functions such as
// interleaving, deinterleaving, and format conversion.
package mutations

// Interleave combines separate per-channel buffers into a single interleaved
// buffer. For example, given left [L0, L1, L2] and right [R0, R1, R2], the
// result is [L0, R0, L1, R1, L2, R2].
//
// Channels may have different lengths. The output length is determined by the
// longest channel; shorter channels are zero-padded.
func Interleave(channels [][]float64) []float64 {
	if len(channels) == 0 {
		return nil
	}
	numChannels := len(channels)
	frames := 0
	for _, ch := range channels {
		if len(ch) > frames {
			frames = len(ch)
		}
	}
	out := make([]float64, frames*numChannels)
	for i := 0; i < frames; i++ {
		for ch := 0; ch < numChannels; ch++ {
			if i < len(channels[ch]) {
				out[i*numChannels+ch] = channels[ch][i]
			}
			// else: zero (default)
		}
	}
	return out
}

// Deinterleave splits an interleaved buffer into separate per-channel buffers.
// For example, given [L0, R0, L1, R1, L2, R2] with numChannels=2, the result
// is [[L0, L1, L2], [R0, R1, R2]].
func Deinterleave(interleaved []float64, numChannels int) [][]float64 {
	if numChannels <= 0 || len(interleaved) == 0 {
		return nil
	}
	frames := len(interleaved) / numChannels
	out := make([][]float64, numChannels)
	for ch := 0; ch < numChannels; ch++ {
		out[ch] = make([]float64, frames)
	}
	for i := 0; i < frames; i++ {
		for ch := 0; ch < numChannels; ch++ {
			out[ch][i] = interleaved[i*numChannels+ch]
		}
	}
	return out
}
