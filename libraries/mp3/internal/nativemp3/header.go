package nativemp3

// Frame header field accessors — 1:1 translations of the HDR_* macros in
// minimp3.h. Each takes a slice positioned at the 4-byte MPEG audio frame
// header (h[0..3]), mirroring the C "const uint8_t *h".

// hdrIsMono reports whether the header selects single-channel mode
// (HDR_IS_MONO, minimp3.h:62).
//
//	#define HDR_IS_MONO(h) (((h[3]) & 0xC0) == 0xC0)
func hdrIsMono(h []byte) bool { return (h[3] & 0xC0) == 0xC0 }

// hdrIsFreeFormat reports whether the bitrate index marks a free-format
// frame (HDR_IS_FREE_FORMAT, minimp3.h:64).
//
//	#define HDR_IS_FREE_FORMAT(h) (((h[2]) & 0xF0) == 0)
func hdrIsFreeFormat(h []byte) bool { return (h[2] & 0xF0) == 0 }

// hdrIsMSStereo reports whether the header selects MPEG joint stereo with
// mid/side (MS) coding active (HDR_IS_MS_STEREO, minimp3.h:63).
//
//	#define HDR_IS_MS_STEREO(h) (((h[3]) & 0xE0) == 0x60)
func hdrIsMSStereo(h []byte) bool { return (h[3] & 0xE0) == 0x60 }

// hdrTestMSStereo returns the (non-boolean) MS-stereo mode-extension bit
// (HDR_TEST_MS_STEREO, minimp3.h:70).
//
//	#define HDR_TEST_MS_STEREO(h) ((h[3]) & 0x20)
func hdrTestMSStereo(h []byte) int { return int(h[3] & 0x20) }

// hdrIsCRC reports whether the frame carries a 16-bit CRC after the header
// (HDR_IS_CRC, minimp3.h:65).
//
//	#define HDR_IS_CRC(h) (!((h[1]) & 1))
func hdrIsCRC(h []byte) bool { return (h[1] & 1) == 0 }

// hdrTestPadding returns the (non-boolean) padding flag bit
// (HDR_TEST_PADDING, minimp3.h:66).
//
//	#define HDR_TEST_PADDING(h) ((h[2]) & 0x2)
func hdrTestPadding(h []byte) int { return int(h[2] & 0x2) }

// hdrTestMPEG1 returns the (non-boolean) MPEG-1 version bit
// (HDR_TEST_MPEG1, minimp3.h:67).
//
//	#define HDR_TEST_MPEG1(h) ((h[1]) & 0x8)
func hdrTestMPEG1(h []byte) int { return int(h[1] & 0x8) }

// hdrTestNotMPEG25 returns the (non-boolean) "not MPEG-2.5" version bit
// (HDR_TEST_NOT_MPEG25, minimp3.h:68).
//
//	#define HDR_TEST_NOT_MPEG25(h) ((h[1]) & 0x10)
func hdrTestNotMPEG25(h []byte) int { return int(h[1] & 0x10) }

// hdrGetLayer returns the raw layer field (HDR_GET_LAYER, minimp3.h:73).
//
//	#define HDR_GET_LAYER(h) (((h[1]) >> 1) & 3)
func hdrGetLayer(h []byte) int { return int((h[1] >> 1) & 3) }

// hdrGetBitrate returns the raw bitrate index (HDR_GET_BITRATE, minimp3.h:74).
//
//	#define HDR_GET_BITRATE(h) ((h[2]) >> 4)
func hdrGetBitrate(h []byte) int { return int(h[2] >> 4) }

// hdrGetSampleRate returns the raw sample-rate index (HDR_GET_SAMPLE_RATE,
// minimp3.h:75).
//
//	#define HDR_GET_SAMPLE_RATE(h) (((h[2]) >> 2) & 3)
func hdrGetSampleRate(h []byte) int { return int((h[2] >> 2) & 3) }

// hdrGetMySampleRate returns minimp3's combined sample-rate index that folds
// in the MPEG version bits, used to index the Layer III scale-factor-band
// tables (HDR_GET_MY_SAMPLE_RATE, minimp3.h:76).
//
//	#define HDR_GET_MY_SAMPLE_RATE(h) (HDR_GET_SAMPLE_RATE(h) + (((h[1] >> 3) & 1) + ((h[1] >> 4) & 1))*3)
func hdrGetMySampleRate(h []byte) int {
	return hdrGetSampleRate(h) + (int((h[1]>>3)&1)+int((h[1]>>4)&1))*3
}

// hdrIsFrame576 reports whether the frame carries 576 samples per granule
// (HDR_IS_FRAME_576, minimp3.h:77).
//
//	#define HDR_IS_FRAME_576(h) ((h[1] & 14) == 2)
func hdrIsFrame576(h []byte) bool { return (h[1] & 14) == 2 }

// hdrIsLayer1 reports whether the frame is Layer I (HDR_IS_LAYER_1,
// minimp3.h:78).
//
//	#define HDR_IS_LAYER_1(h) ((h[1] & 6) == 6)
func hdrIsLayer1(h []byte) bool { return (h[1] & 6) == 6 }

// hdrValid reports whether h is a structurally valid MPEG audio frame
// header (hdr_valid, minimp3.h:264).
//
//	static int hdr_valid(const uint8_t *h)
//	{
//	    return h[0] == 0xff &&
//	        ((h[1] & 0xF0) == 0xf0 || (h[1] & 0xFE) == 0xe2) &&
//	        (HDR_GET_LAYER(h) != 0) &&
//	        (HDR_GET_BITRATE(h) != 15) &&
//	        (HDR_GET_SAMPLE_RATE(h) != 3);
//	}
func hdrValid(h []byte) bool {
	return h[0] == 0xff &&
		((h[1]&0xF0) == 0xf0 || (h[1]&0xFE) == 0xe2) &&
		hdrGetLayer(h) != 0 &&
		hdrGetBitrate(h) != 15 &&
		hdrGetSampleRate(h) != 3
}

// hdrCompare reports whether h2 is a valid header matching h1 in the
// version, layer, sample-rate, and free-format fields (hdr_compare,
// minimp3.h:273).
//
//	static int hdr_compare(const uint8_t *h1, const uint8_t *h2)
//	{
//	    return hdr_valid(h2) &&
//	        ((h1[1] ^ h2[1]) & 0xFE) == 0 &&
//	        ((h1[2] ^ h2[2]) & 0x0C) == 0 &&
//	        !(HDR_IS_FREE_FORMAT(h1) ^ HDR_IS_FREE_FORMAT(h2));
//	}
func hdrCompare(h1, h2 []byte) bool {
	return hdrValid(h2) &&
		((h1[1]^h2[1])&0xFE) == 0 &&
		((h1[2]^h2[2])&0x0C) == 0 &&
		hdrIsFreeFormat(h1) == hdrIsFreeFormat(h2)
}

// hdrBitrateKbps returns the frame's bitrate in kbps from the version,
// layer, and bitrate-index fields (hdr_bitrate_kbps, minimp3.h:281).
//
//	static unsigned hdr_bitrate_kbps(const uint8_t *h)
//	{
//	    static const uint8_t halfrate[2][3][15] = { ... };
//	    return 2*halfrate[!!HDR_TEST_MPEG1(h)][HDR_GET_LAYER(h) - 1][HDR_GET_BITRATE(h)];
//	}
func hdrBitrateKbps(h []byte) uint {
	halfrate := [2][3][15]uint8{
		{
			{0, 4, 8, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 72, 80},
			{0, 4, 8, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 72, 80},
			{0, 16, 24, 28, 32, 40, 48, 56, 64, 72, 80, 88, 96, 112, 128},
		},
		{
			{0, 16, 20, 24, 28, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160},
			{0, 16, 24, 28, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192},
			{0, 16, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224},
		},
	}
	mpeg1 := 0
	if hdrTestMPEG1(h) != 0 {
		mpeg1 = 1
	}
	return 2 * uint(halfrate[mpeg1][hdrGetLayer(h)-1][hdrGetBitrate(h)])
}

// hdrSampleRateHz returns the frame's sample rate in Hz from the
// sample-rate index and version bits (hdr_sample_rate_hz, minimp3.h:290).
//
//	static unsigned hdr_sample_rate_hz(const uint8_t *h)
//	{
//	    static const unsigned g_hz[3] = { 44100, 48000, 32000 };
//	    return g_hz[HDR_GET_SAMPLE_RATE(h)] >> (int)!HDR_TEST_MPEG1(h) >> (int)!HDR_TEST_NOT_MPEG25(h);
//	}
func hdrSampleRateHz(h []byte) uint {
	gHz := [3]uint{44100, 48000, 32000}
	notMPEG1 := 0
	if hdrTestMPEG1(h) == 0 {
		notMPEG1 = 1
	}
	notMPEG25 := 0
	if hdrTestNotMPEG25(h) == 0 {
		notMPEG25 = 1
	}
	return gHz[hdrGetSampleRate(h)] >> uint(notMPEG1) >> uint(notMPEG25)
}

// hdrFrameSamples returns the number of samples per channel the frame
// carries (hdr_frame_samples, minimp3.h:296).
//
//	static unsigned hdr_frame_samples(const uint8_t *h)
//	{
//	    return HDR_IS_LAYER_1(h) ? 384 : (1152 >> (int)HDR_IS_FRAME_576(h));
//	}
func hdrFrameSamples(h []byte) uint {
	if hdrIsLayer1(h) {
		return 384
	}
	shift := 0
	if hdrIsFrame576(h) {
		shift = 1
	}
	return 1152 >> uint(shift)
}

// hdrFrameBytes returns the frame length in bytes (excluding any padding
// slot), falling back to freeFormatSize for free-format frames
// (hdr_frame_bytes, minimp3.h:301).
//
//	static int hdr_frame_bytes(const uint8_t *h, int free_format_size)
//	{
//	    int frame_bytes = hdr_frame_samples(h)*hdr_bitrate_kbps(h)*125/hdr_sample_rate_hz(h);
//	    if (HDR_IS_LAYER_1(h))
//	    {
//	        frame_bytes &= ~3; /* slot align */
//	    }
//	    return frame_bytes ? frame_bytes : free_format_size;
//	}
func hdrFrameBytes(h []byte, freeFormatSize int) int {
	frameBytes := int(hdrFrameSamples(h)) * int(hdrBitrateKbps(h)) * 125 / int(hdrSampleRateHz(h))
	if hdrIsLayer1(h) {
		frameBytes &^= 3 // slot align
	}
	if frameBytes != 0 {
		return frameBytes
	}
	return freeFormatSize
}

// hdrPadding returns the frame's padding slot size in bytes (hdr_padding,
// minimp3.h:311).
//
//	static int hdr_padding(const uint8_t *h)
//	{
//	    return HDR_TEST_PADDING(h) ? (HDR_IS_LAYER_1(h) ? 4 : 1) : 0;
//	}
func hdrPadding(h []byte) int {
	if hdrTestPadding(h) != 0 {
		if hdrIsLayer1(h) {
			return 4
		}
		return 1
	}
	return 0
}
