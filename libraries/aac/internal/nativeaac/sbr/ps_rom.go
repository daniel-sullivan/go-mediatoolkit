// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Parametric-stereo (HE-AAC v2) ROM tables, ported 1:1 from the vendored
// Fraunhofer FDK-AAC sbr_rom.cpp (the "constants used in psdec.cpp" block plus
// the PS Huffman codebooks and bin/env count tables). These feed the PS
// bitstream parse (psbitdec.cpp) and the slot-based stereo upmix (psdec.cpp).
//
// Integer/fixed-point: ScaleFactors / ScaleFactorsFine / Alphas are FIXP_DBL
// (Q31, but pre-shifted right by 1 — see the C comment "the values of the
// following 3 tables are shiftet right by 1 !"). The Huffman codebooks are the
// SCHAR [N][2] node tables decode_huff_cw walks.

// psFixNoEnvDecode maps the 2-bit FIX_BORDERS noEnv code to {0,1,2,4}.
//
// C: FDK_sbrDecoder_aFixNoEnvDecode (sbr_rom.cpp:1050).
var psFixNoEnvDecode = [4]uint8{0, 1, 2, 4}

// PS Huffman codebooks (sbr_rom.cpp:1053-1099). Stored as [][2]int8 so the shared
// decodeHuffmanCW (huff_dec.go) walks them identically to the SBR codebooks.

// aBookPsIidTimeDecode is the IID coarse delta-time codebook (sbr_rom.cpp:1053).
var aBookPsIidTimeDecode = huffman{
	{-64, 1}, {-65, 2}, {-63, 3}, {-66, 4}, {-62, 5}, {-67, 6},
	{-61, 7}, {-68, 8}, {-60, 9}, {-69, 10}, {-59, 11}, {-70, 12},
	{-58, 13}, {-57, 14}, {-71, 15}, {16, 17}, {-56, -72}, {18, 21},
	{19, 20}, {-55, -78}, {-77, -76}, {22, 25}, {23, 24}, {-75, -74},
	{-73, -54}, {26, 27}, {-53, -52}, {-51, -50},
}

// aBookPsIidFreqDecode is the IID coarse delta-freq codebook (sbr_rom.cpp:1060).
var aBookPsIidFreqDecode = huffman{
	{-64, 1}, {2, 3}, {-63, -65}, {4, 5}, {-62, -66}, {6, 7},
	{-61, -67}, {8, 9}, {-68, -60}, {-59, 10}, {-69, 11}, {-58, 12},
	{-70, 13}, {-71, 14}, {-57, 15}, {16, 17}, {-56, -72}, {18, 19},
	{-55, -54}, {20, 21}, {-73, -53}, {22, 24}, {-74, 23}, {-75, -78},
	{25, 26}, {-77, -76}, {-52, 27}, {-51, -50},
}

// aBookPsIccTimeDecode is the ICC delta-time codebook (sbr_rom.cpp:1067).
var aBookPsIccTimeDecode = huffman{
	{-64, 1}, {-63, 2}, {-65, 3}, {-62, 4}, {-66, 5}, {-61, 6}, {-67, 7},
	{-60, 8}, {-68, 9}, {-59, 10}, {-69, 11}, {-58, 12}, {-70, 13}, {-71, -57},
}

// aBookPsIccFreqDecode is the ICC delta-freq codebook (sbr_rom.cpp:1071).
var aBookPsIccFreqDecode = huffman{
	{-64, 1}, {-63, 2}, {-65, 3}, {-62, 4}, {-66, 5}, {-61, 6}, {-67, 7},
	{-60, 8}, {-59, 9}, {-68, 10}, {-58, 11}, {-69, 12}, {-57, 13}, {-70, -71},
}

// aBookPsIidFineTimeDecode is the IID-fine delta-time codebook (sbr_rom.cpp:1077).
var aBookPsIidFineTimeDecode = huffman{
	{1, -64}, {-63, 2}, {3, -65}, {4, 59}, {5, 7}, {6, -67},
	{-68, -60}, {-61, 8}, {9, 11}, {-59, 10}, {-70, -58}, {12, 41},
	{13, 20}, {14, -71}, {-55, 15}, {-53, 16}, {17, -77}, {18, 19},
	{-85, -84}, {-46, -45}, {-57, 21}, {22, 40}, {23, 29}, {-51, 24},
	{25, 26}, {-83, -82}, {27, 28}, {-90, -38}, {-92, -91}, {30, 37},
	{31, 34}, {32, 33}, {-35, -34}, {-37, -36}, {35, 36}, {-94, -93},
	{-89, -39}, {38, -79}, {39, -81}, {-88, -40}, {-74, -54}, {42, -69},
	{43, 44}, {-72, -56}, {45, 52}, {46, 50}, {47, -76}, {-49, 48},
	{-47, 49}, {-87, -41}, {-52, 51}, {-78, -50}, {53, -73}, {54, -75},
	{55, 57}, {56, -80}, {-86, -42}, {-48, 58}, {-44, -43}, {-66, -62},
}

// aBookPsIidFineFreqDecode is the IID-fine delta-freq codebook (sbr_rom.cpp:1089).
var aBookPsIidFineFreqDecode = huffman{
	{1, -64}, {2, 4}, {3, -65}, {-66, -62}, {-63, 5}, {6, 7},
	{-67, -61}, {8, 9}, {-68, -60}, {10, 11}, {-69, -59}, {12, 13},
	{-70, -58}, {14, 18}, {-57, 15}, {16, -72}, {-54, 17}, {-75, -53},
	{19, 37}, {-56, 20}, {21, -73}, {22, 29}, {23, -76}, {24, -78},
	{25, 28}, {26, 27}, {-85, -43}, {-83, -45}, {-81, -47}, {-52, 30},
	{-50, 31}, {32, -79}, {33, 34}, {-82, -46}, {35, 36}, {-90, -89},
	{-92, -91}, {38, -71}, {-55, 39}, {40, -74}, {41, 50}, {42, -77},
	{-49, 43}, {44, 47}, {45, 46}, {-86, -42}, {-88, -87}, {48, 49},
	{-39, -38}, {-41, -40}, {-51, 51}, {52, 59}, {53, 56}, {54, 55},
	{-35, -34}, {-37, -36}, {57, 58}, {-94, -93}, {-84, -44}, {-80, -48},
}

// PS dequantization tables used in psdec.cpp (sbr_rom.cpp:1101-1130).
//
// The values of ScaleFactors / ScaleFactorsFine / Alphas are pre-shifted right
// by 1 (see C comment at sbr_rom.cpp:1103). FIXP_DBL == int32 Q-format.

// psScaleFactors are the coarse IID scale factors, indexed [noIidSteps ± idx].
//
// C: ScaleFactors[NO_IID_LEVELS] (sbr_rom.cpp:1104).
var psScaleFactors = [psNoIidLevels]int32{
	0x5a5ded00, 0x59cd0400, 0x58c29680, 0x564c2e80, 0x52a3d480,
	0x4c8be080, 0x46df3080, 0x40000000, 0x384ba5c0, 0x304c2980,
	0x24e9f640, 0x1b4a2940, 0x11b5c0a0, 0x0b4e2540, 0x0514ea90,
}

// psScaleFactorsFine are the fine IID scale factors (sbr_rom.cpp:1110).
var psScaleFactorsFine = [psNoIidLevelsFine]int32{
	0x5a825c00, 0x5a821c00, 0x5a815100, 0x5a7ed000, 0x5a76e600, 0x5a5ded00,
	0x5a39b880, 0x59f1fd00, 0x5964d680, 0x5852ca00, 0x564c2e80, 0x54174480,
	0x50ea7500, 0x4c8be080, 0x46df3080, 0x40000000, 0x384ba5c0, 0x304c2980,
	0x288dd240, 0x217a2900, 0x1b4a2940, 0x13c5ece0, 0x0e2b0090, 0x0a178ef0,
	0x072ab798, 0x0514ea90, 0x02dc5944, 0x019bf87c, 0x00e7b173, 0x00824b8b,
	0x00494568,
}

// psAlphas are the ICC rotation angles, indexed by the ICC parameter index.
//
// C: Alphas[NO_ICC_LEVELS] (sbr_rom.cpp:1118).
var psAlphas = [psNoIccLevels]int32{
	0x00000000, 0x0b6b5be0, 0x12485f80, 0x1da2fa40,
	0x2637ebc0, 0x3243f6c0, 0x466b7480, 0x6487ed80,
}

// psBins2GroupMap20 maps each of the NO_IID_GROUPS sub-subband groups to its
// 20-band stereo bin.
//
// C: bins2groupMap20[NO_IID_GROUPS] (sbr_rom.cpp:1123).
var psBins2GroupMap20 = [psNoIidGroups]uint8{
	0, 0, 1, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
}

// psNoIidBins gives the number of IID bins per freq-resolution (low/mid/hi).
//
// C: FDK_sbrDecoder_aNoIidBins[3] (sbr_rom.cpp:1126).
var psNoIidBins = [3]uint8{psNoLowResIidBins, psNoMidResIidBins, psNoHiResIidBins}

// psNoIccBins gives the number of ICC bins per freq-resolution (low/mid/hi).
//
// C: FDK_sbrDecoder_aNoIccBins[3] (sbr_rom.cpp:1129).
var psNoIccBins = [3]uint8{psNoLowResIccBins, psNoMidResIccBins, psNoHiResIccBins}
