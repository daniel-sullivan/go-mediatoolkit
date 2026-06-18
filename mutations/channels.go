package mutations

// DownmixStereoToMono writes one mono sample per stereo frame into
// dst, averaging the L and R channels. stereo is interleaved
// [L0,R0,L1,R1,...]. Returns the number of mono samples written (= the
// number of input frames consumed).
func DownmixStereoToMono(stereo []float64, dst []float64) int {
	frames := len(stereo) / 2
	if frames > len(dst) {
		frames = len(dst)
	}
	for i := 0; i < frames; i++ {
		dst[i] = 0.5 * (stereo[i*2] + stereo[i*2+1])
	}
	return frames
}

// UpmixMonoToStereo duplicates each mono sample into an interleaved
// L/R pair in dst. Returns the number of stereo samples written
// (frames * 2).
func UpmixMonoToStereo(mono []float64, dst []float64) int {
	frames := len(mono)
	if frames*2 > len(dst) {
		frames = len(dst) / 2
	}
	for i := 0; i < frames; i++ {
		dst[i*2] = mono[i]
		dst[i*2+1] = mono[i]
	}
	return frames * 2
}
