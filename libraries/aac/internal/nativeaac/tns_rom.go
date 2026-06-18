// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// TNS ROM: the reflection-coefficient dequantization tables and the
// per-sampling-rate TNS_MAX_BANDS limits, ported 1:1 from
// libAACdec/src/aac_rom.cpp. FIXP_TCC == FIXP_DBL (int32, Q1.31); the TCC(x)
// macro is a bare cast, so these are stored verbatim with no narrowing.

// tnsCoeff3 ports FDKaacDec_tnsCoeff3[8] (aac_rom.cpp:3229): the 3-bit-resolution
// TNS reflection-coefficient dequantization table. Indexed by Coeff[i]+4.
var tnsCoeff3 = [8]int32{
	0x81f1d1d4 - 1<<32, 0x9126146c - 1<<32, 0xadb922c4 - 1<<32, 0xd438af1f - 1<<32,
	0x00000000, 0x3789809b, 0x64130dd4, 0x7cca7016,
}

// tnsCoeff4 ports FDKaacDec_tnsCoeff4[16] (aac_rom.cpp:3232): the
// 4-bit-resolution TNS reflection-coefficient dequantization table. Indexed by
// Coeff[i]+8.
var tnsCoeff4 = [16]int32{
	0x808bc842 - 1<<32, 0x84e2e58c - 1<<32, 0x8d6b49d1 - 1<<32, 0x99da920a - 1<<32,
	0xa9c45713 - 1<<32, 0xbc9ddeb9 - 1<<32, 0xd1c2d51b - 1<<32, 0xe87ae53d - 1<<32,
	0x00000000, 0x1a9cd9b6, 0x340ff254, 0x4b3c8c29,
	0x5f1f5ebb, 0x6ed9ebba, 0x79bc385f, 0x7f4c7e5b,
}

// tnsMaxBandsTbl ports tns_max_bands_tbl[13][2] (aac_rom.cpp:3179): the AAC-LC
// TNS_MAX_BANDS per sampling-rate index; column 0 long, column 1 short
// (indexed by !IsLongBlock).
var tnsMaxBandsTbl = [13][2]uint8{
	{31, 9},  // 96000
	{31, 9},  // 88200
	{34, 10}, // 64000
	{40, 14}, // 48000
	{42, 14}, // 44100
	{51, 14}, // 32000
	{46, 14}, // 24000
	{46, 14}, // 22050
	{42, 14}, // 16000
	{42, 14}, // 12000
	{42, 14}, // 11025
	{39, 14}, //  8000
	{39, 14}, //  7350
}

// tnsMaxBandsTbl480 ports tns_max_bands_tbl_480[13] (aac_rom.cpp:3196): the
// TNS_MAX_BANDS for the 480-line low-delay frame length, indexed by sampling
// rate index. Reached only by the granuleLength==480 path of CTns_Apply.
var tnsMaxBandsTbl480 = [13]uint8{
	31, 31, 31, 31, 32, 37, 30, 30, 30, 30, 30, 30, 30,
}

// tnsMaxBandsTbl512 ports tns_max_bands_tbl_512[13] (aac_rom.cpp:3211): the
// TNS_MAX_BANDS for the 512-line low-delay frame length, indexed by sampling
// rate index. Reached only by the granuleLength==512 path of CTns_Apply.
var tnsMaxBandsTbl512 = [13]uint8{
	31, 31, 31, 31, 32, 37, 31, 31, 31, 31, 31, 31, 31,
}
