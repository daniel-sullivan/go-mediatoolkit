// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// ENCODE-side TNS ROM tables, ported 1:1 from libAACenc/src/aacenc_tns.cpp.
// These are the integer FIXP_DBL autocorrelation windows and the per-sample-rate
// maximum-band table FDKaacEnc_InitTnsConfiguration reads. They are distinct
// from the DECODE-side tns_max_bands_tbl (tns_rom.go) — the encoder ships its
// own tnsMaxBandsTab1024 ROM. Pure integer constants, no float, no aac_strict
// gating.

// encAcfWindowLong ports acfWindowLong[12+3+1] (aacenc_tns.cpp:113-116): the
// integer FIXP_DBL autocorrelation window for the AAC-LC long block, copied into
// TNS_CONFIG.acfWindow by FDKaacEnc_InitTnsConfiguration.
var encAcfWindowLong = [16]int32{
	0x7fffffff, 0x7fb80000, 0x7ee00000, 0x7d780000, 0x7b800000, 0x78f80000,
	0x75e00000, 0x72380000, 0x6e000000, 0x69380000, 0x63e00000, 0x5df80000,
	0x57800000, 0x50780000, 0x48e00000, 0x40b80000,
}

// encAcfWindowShort ports acfWindowShort[4+3+1] (aacenc_tns.cpp:118-120): the
// integer FIXP_DBL autocorrelation window for the short block.
var encAcfWindowShort = [8]int32{
	0x7fffffff, 0x7e000000, 0x78000000, 0x6e000000,
	0x60000000, 0x4e000000, 0x38000000, 0x1e000000,
}

// tnsMaxTabEntry ports the TNS_MAX_TAB_ENTRY struct (aacenc_tns.cpp:265-269):
// the sampling rate and the {long, short} maximum-band pair.
type tnsMaxTabEntry struct {
	samplingRate int
	maxBands     [2]int // long==0, short==1 (SCHAR in C)
}

// tnsMaxBandsTab1024 ports tnsMaxBandsTab1024[] (aacenc_tns.cpp:283-286): the
// encoder's granuleLength-1024 maximum-band-per-sample-rate table consumed by
// getTnsMaxBands. SCHAR maxBands kept as int (values fit). Order is descending
// sample rate, matching the linear scan in getTnsMaxBands.
var tnsMaxBandsTab1024 = []tnsMaxTabEntry{
	{96000, [2]int{31, 9}}, {88200, [2]int{31, 9}}, {64000, [2]int{34, 10}}, {48000, [2]int{40, 14}},
	{44100, [2]int{42, 14}}, {32000, [2]int{51, 14}}, {24000, [2]int{46, 14}}, {22050, [2]int{46, 14}},
	{16000, [2]int{42, 14}}, {12000, [2]int{42, 14}}, {11025, [2]int{42, 14}}, {8000, [2]int{39, 14}},
}

// getTnsMaxBands ports the static getTnsMaxBands (aacenc_tns.cpp:272-324) for the
// AAC-LC granule lengths (960/1024 -> tnsMaxBandsTab1024). It scans the table in
// declared order, latching maxBands[long/short] for each entry, and stops at the
// first entry whose samplingRate the input sampleRate meets or exceeds. Returns
// -1 for an unsupported granule length (only 960/1024 are implemented for
// AAC-LC; the 120/128/240/256/480/512 LD/short-transform tables are out of
// scope). isShortBlock selects the short column.
func getTnsMaxBands(sampleRate, granuleLength int, isShortBlock int) int {
	numBands := -1
	var pMaxBandsTab []tnsMaxTabEntry
	switch granuleLength {
	case 960, 1024:
		pMaxBandsTab = tnsMaxBandsTab1024
	default:
		return -1
	}

	col := 0
	if isShortBlock != 0 {
		col = 1
	}
	for i := 0; i < len(pMaxBandsTab); i++ {
		numBands = pMaxBandsTab[i].maxBands[col]
		if sampleRate >= pMaxBandsTab[i].samplingRate {
			break
		}
	}
	return numBands
}
