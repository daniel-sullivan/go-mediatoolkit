package mutations

import "math"

// CrossfadeLoop returns a seamlessly-loopable version of samples by
// equal-power crossfading the clip's tail into its head and dropping
// the now-redundant tail. The returned slice is shorter than the
// input by fadeFrames frames.
//
// Seamless looping works as follows: during playback of the shortened
// clip, the last sample comes directly from the input's unmodified
// middle region and the first sample comes from the full tail (head
// faded out). Over the next fadeFrames the signal morphs from tail
// content to head content with constant perceived power, then settles
// into the unmodified middle for the rest of the cycle. The result is
// a continuous spectral transition at every loop wrap — no amplitude
// dip, no click.
//
// Equal-power curves (sin/cos) work well for uncorrelated content
// (noise, pads). Highly correlated content (sustained sine tones) may
// exhibit a small dip during the crossfade window because the two
// in-phase copies partially cancel; such material usually loops better
// when authored to line up at the seam natively.
//
// Caller requirements:
//   - samples must be interleaved with channels channels.
//   - fadeFrames must satisfy 2*fadeFrames*channels ≤ len(samples) so
//     head and tail regions don't overlap. Violating this returns
//     samples unchanged.
//   - fadeFrames ≤ 0 returns samples unchanged.
func CrossfadeLoop(samples []float64, fadeFrames, channels int) []float64 {
	if fadeFrames <= 0 || channels <= 0 {
		return samples
	}
	fadeSamples := fadeFrames * channels
	if fadeSamples*2 > len(samples) {
		return samples
	}
	outLen := len(samples) - fadeSamples
	out := make([]float64, outLen)
	copy(out, samples[:outLen])

	tailStart := outLen // samples index where the tail begins
	for f := 0; f < fadeFrames; f++ {
		phase := 0.5 * math.Pi * float64(f) / float64(fadeFrames)
		fadeIn := math.Sin(phase)  // head weight: 0 → 1
		fadeOut := math.Cos(phase) // tail weight: 1 → 0
		for ch := 0; ch < channels; ch++ {
			i := f*channels + ch
			out[i] = fadeIn*samples[i] + fadeOut*samples[tailStart+i]
		}
	}
	return out
}
