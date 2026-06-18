package nativeopus

// 9f-D test shims: threshold tables + leaf helpers from opus_encoder.c.

// ExportDecideFec wraps decide_fec. Returns (ret, updatedBandwidth).
func ExportDecideFec(useInBandFEC, PacketLossPerc, lastFec, mode, bandwidth int, rate int32) (int, int) {
	bw := bandwidth
	r := decide_fec(useInBandFEC, PacketLossPerc, lastFec, mode, &bw, opus_int32(rate))
	return r, bw
}

// ExportComputeSilkRateForHybrid wraps compute_silk_rate_for_hybrid.
func ExportComputeSilkRateForHybrid(rate, bandwidth, frame20ms, vbr, fec, channels int) int {
	return compute_silk_rate_for_hybrid(rate, bandwidth, frame20ms, vbr, fec, channels)
}

// ExportComputeEquivRate wraps compute_equiv_rate.
func ExportComputeEquivRate(bitrate int32, channels, frameRate, vbr, mode, complexity, loss int) int32 {
	return int32(compute_equiv_rate(opus_int32(bitrate), channels, frameRate, vbr, mode, complexity, loss))
}

// ExportComputeFrameEnergy wraps compute_frame_energy.
func ExportComputeFrameEnergy(pcm []float32, frameSize, channels, arch int) float32 {
	return float32(compute_frame_energy(pcm, frameSize, channels, arch))
}

// ExportDecideDtxMode wraps decide_dtx_mode.
func ExportDecideDtxMode(activity, nbNoActivityMsQ1, frameSizeMsQ1 int) (int, int) {
	x := nbNoActivityMsQ1
	r := decide_dtx_mode(opus_int(activity), &x, frameSizeMsQ1)
	return r, x
}

// ExportComputeRedundancyBytes wraps compute_redundancy_bytes.
func ExportComputeRedundancyBytes(maxDataBytes, bitrateBps int32, frameRate, channels int) int {
	return compute_redundancy_bytes(opus_int32(maxDataBytes), opus_int32(bitrateBps), frameRate, channels)
}

// ExportBandwidthThresholds returns the four bandwidth threshold
// tables (8 entries each) in declaration order.
func ExportBandwidthThresholds() (monoVoice, monoMusic, stereoVoice, stereoMusic [8]int32) {
	for i := 0; i < 8; i++ {
		monoVoice[i] = int32(mono_voice_bandwidth_thresholds[i])
		monoMusic[i] = int32(mono_music_bandwidth_thresholds[i])
		stereoVoice[i] = int32(stereo_voice_bandwidth_thresholds[i])
		stereoMusic[i] = int32(stereo_music_bandwidth_thresholds[i])
	}
	return
}

// ExportStereoThresholds returns the stereo voice/music bit-rate thresholds.
func ExportStereoThresholds() (voice, music int32) {
	return int32(stereo_voice_threshold), int32(stereo_music_threshold)
}

// ExportModeThresholds returns mode_thresholds[2][2].
func ExportModeThresholds() [2][2]int32 {
	var out [2][2]int32
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			out[i][j] = int32(mode_thresholds[i][j])
		}
	}
	return out
}

// ExportFecThresholds returns fec_thresholds.
func ExportFecThresholds() [10]int32 {
	var out [10]int32
	for i := 0; i < 10; i++ {
		out[i] = int32(fec_thresholds[i])
	}
	return out
}
