package nativeopus

// CELT shared-helpers test shims.

func ExportTestResamplingFactor(rate int32) int { return resampling_factor(opus_int32(rate)) }

func ExportTestInitCaps(h CeltModeHandle, cap []int, LM, C int) {
	init_caps(h.p, cap, LM, C)
}

func ExportTestBitsToBitrate(bits, Fs, frameSize int32) int32 {
	return int32(bits_to_bitrate(opus_int32(bits), opus_int32(Fs), opus_int32(frameSize)))
}

func ExportTestBitrateToBits(bitrate, Fs, frameSize int32) int32 {
	return int32(bitrate_to_bits(opus_int32(bitrate), opus_int32(Fs), opus_int32(frameSize)))
}

// ExportTestCombFilter runs comb_filter on an explicit buffer with
// pre-roll already populated by the caller. xOff / yOff mark the
// "logical sample 0" so the filter can safely read x[xOff-T-2 ..
// xOff+N+1].
func ExportTestCombFilter(y []float32, yOff int, x []float32, xOff int,
	T0, T1, N int, g0, g1 float32, tapset0, tapset1 int,
	window []float32, overlap int) {
	comb_filter(y, yOff, x, xOff, T0, T1, N,
		opus_val16(g0), opus_val16(g1), tapset0, tapset1, window, overlap, 0)
}
