package nativeopus

// Exports for SILK resampler parity tests.

// ExportTestSilkResamplerAR2 runs the AR2 state filter.
func ExportTestSilkResamplerAR2(S []int32, in_ []int16, A_Q14 []int16) ([]int32, []int32) {
	outQ8 := make([]int32, len(in_))
	sCopy := append([]int32(nil), S...)
	silk_resampler_private_AR2(sCopy, outQ8, in_, A_Q14, int32(len(in_)))
	return outQ8, sCopy
}

func ExportTestSilkResamplerDown2(S []int32, in_ []int16) ([]int16, []int32) {
	sCopy := append([]int32(nil), S...)
	out := make([]int16, len(in_)/2)
	silk_resampler_down2(sCopy, out, in_, int32(len(in_)))
	return out, sCopy
}
func ExportTestSilkResamplerDown23(S []int32, in_ []int16) ([]int16, []int32) {
	sCopy := append([]int32(nil), S...)
	out := make([]int16, 2*len(in_)/3)
	silk_resampler_down2_3(sCopy, out, in_, int32(len(in_)))
	return out, sCopy
}
func ExportTestSilkResamplerUp2HQ(S []int32, in_ []int16) ([]int16, []int32) {
	sCopy := append([]int32(nil), S...)
	out := make([]int16, 2*len(in_))
	silk_resampler_private_up2_HQ(sCopy, out, in_, int32(len(in_)))
	return out, sCopy
}

// ExportTestSilkResampler runs the full init+resample cycle.
func ExportTestSilkResampler(FsIn, FsOut int, in_ []int16, forEnc int) ([]int16, int) {
	var st silk_resampler_state_struct
	ret := silk_resampler_init(&st, int32(FsIn), int32(FsOut), forEnc)
	if ret != 0 {
		return nil, ret
	}
	// Output size = inLen * Fs_out/Fs_in.
	outLen := len(in_) * FsOut / FsIn
	out := make([]int16, outLen)
	silk_resampler(&st, out, in_, int32(len(in_)))
	return out, 0
}
