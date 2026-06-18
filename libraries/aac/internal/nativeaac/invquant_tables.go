// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// This file is the 1:1 Go port of the inverse-quantization ROM tables from
// the vendored Fraunhofer FDK-AAC decoder (libAACdec/src/aac_rom.cpp). The
// tables are pure integer constants (fixed-point Q1.31 mantissas and signed
// exponents); they are bit-identical regardless of build, so no FP gating
// applies. See nativeaac.go for the overall port/parity convention.

// invQuantTableSize mirrors INV_QUANT_TABLESIZE (aac_rom.h:127).
//
// maxQuantizedValue (MAX_QUANTIZED_VALUE, channelinfo.h:148) and dfractBits
// (DFRACT_BITS, common_fix.h:113) are declared with the rest of the decoder
// constants and reused here.
const invQuantTableSize = 256

// inverseQuantTable is the 1:1 port of InverseQuantTable (aac_rom.cpp:109).
// It tabulates x^(4/3) for the 8-bit mantissa range, prescaled by 4 (the
// SCL_TAB(a) = (a >> 4) macro at aac_rom.cpp:108) to save redundant shifts in
// the inverse-quantization inner loops. Index range is
// [0, invQuantTableSize]; the final entry is the saturation endpoint.
var inverseQuantTable = [invQuantTableSize + 1]int32{
	0x32CBFD40 >> 4, 0x330FC340 >> 4, 0x33539FC0 >> 4,
	0x33979280 >> 4, 0x33DB9BC0 >> 4, 0x341FBB80 >> 4,
	0x3463F180 >> 4, 0x34A83DC0 >> 4, 0x34ECA000 >> 4,
	0x35311880 >> 4, 0x3575A700 >> 4, 0x35BA4B80 >> 4,
	0x35FF0600 >> 4, 0x3643D680 >> 4, 0x3688BCC0 >> 4,
	0x36CDB880 >> 4, 0x3712CA40 >> 4, 0x3757F1C0 >> 4,
	0x379D2F00 >> 4, 0x37E28180 >> 4, 0x3827E9C0 >> 4,
	0x386D6740 >> 4, 0x38B2FA40 >> 4, 0x38F8A2C0 >> 4,
	0x393E6080 >> 4, 0x39843380 >> 4, 0x39CA1BC0 >> 4,
	0x3A101940 >> 4, 0x3A562BC0 >> 4, 0x3A9C5340 >> 4,
	0x3AE28FC0 >> 4, 0x3B28E180 >> 4, 0x3B6F4800 >> 4,
	0x3BB5C340 >> 4, 0x3BFC5380 >> 4, 0x3C42F880 >> 4,
	0x3C89B200 >> 4, 0x3CD08080 >> 4, 0x3D176340 >> 4,
	0x3D5E5B00 >> 4, 0x3DA56700 >> 4, 0x3DEC87C0 >> 4,
	0x3E33BCC0 >> 4, 0x3E7B0640 >> 4, 0x3EC26400 >> 4,
	0x3F09D640 >> 4, 0x3F515C80 >> 4, 0x3F98F740 >> 4,
	0x3FE0A600 >> 4, 0x40286900 >> 4, 0x40704000 >> 4,
	0x40B82B00 >> 4, 0x41002A00 >> 4, 0x41483D00 >> 4,
	0x41906400 >> 4, 0x41D89F00 >> 4, 0x4220ED80 >> 4,
	0x42695000 >> 4, 0x42B1C600 >> 4, 0x42FA5000 >> 4,
	0x4342ED80 >> 4, 0x438B9E80 >> 4, 0x43D46380 >> 4,
	0x441D3B80 >> 4, 0x44662780 >> 4, 0x44AF2680 >> 4,
	0x44F83900 >> 4, 0x45415F00 >> 4, 0x458A9880 >> 4,
	0x45D3E500 >> 4, 0x461D4500 >> 4, 0x4666B800 >> 4,
	0x46B03E80 >> 4, 0x46F9D800 >> 4, 0x47438480 >> 4,
	0x478D4400 >> 4, 0x47D71680 >> 4, 0x4820FC00 >> 4,
	0x486AF500 >> 4, 0x48B50000 >> 4, 0x48FF1E80 >> 4,
	0x49494F80 >> 4, 0x49939380 >> 4, 0x49DDEA80 >> 4,
	0x4A285400 >> 4, 0x4A72D000 >> 4, 0x4ABD5E80 >> 4,
	0x4B080000 >> 4, 0x4B52B400 >> 4, 0x4B9D7A80 >> 4,
	0x4BE85380 >> 4, 0x4C333F00 >> 4, 0x4C7E3D00 >> 4,
	0x4CC94D00 >> 4, 0x4D146F80 >> 4, 0x4D5FA500 >> 4,
	0x4DAAEC00 >> 4, 0x4DF64580 >> 4, 0x4E41B180 >> 4,
	0x4E8D2F00 >> 4, 0x4ED8BF80 >> 4, 0x4F246180 >> 4,
	0x4F701600 >> 4, 0x4FBBDC00 >> 4, 0x5007B480 >> 4,
	0x50539F00 >> 4, 0x509F9B80 >> 4, 0x50EBA980 >> 4,
	0x5137C980 >> 4, 0x5183FB80 >> 4, 0x51D03F80 >> 4,
	0x521C9500 >> 4, 0x5268FC80 >> 4, 0x52B57580 >> 4,
	0x53020000 >> 4, 0x534E9C80 >> 4, 0x539B4A80 >> 4,
	0x53E80A80 >> 4, 0x5434DB80 >> 4, 0x5481BE80 >> 4,
	0x54CEB280 >> 4, 0x551BB880 >> 4, 0x5568CF80 >> 4,
	0x55B5F800 >> 4, 0x56033200 >> 4, 0x56507D80 >> 4,
	0x569DDA00 >> 4, 0x56EB4800 >> 4, 0x5738C700 >> 4,
	0x57865780 >> 4, 0x57D3F900 >> 4, 0x5821AC00 >> 4,
	0x586F7000 >> 4, 0x58BD4500 >> 4, 0x590B2B00 >> 4,
	0x59592200 >> 4, 0x59A72A80 >> 4, 0x59F54380 >> 4,
	0x5A436D80 >> 4, 0x5A91A900 >> 4, 0x5ADFF500 >> 4,
	0x5B2E5180 >> 4, 0x5B7CBF80 >> 4, 0x5BCB3E00 >> 4,
	0x5C19CD00 >> 4, 0x5C686D80 >> 4, 0x5CB71E00 >> 4,
	0x5D05DF80 >> 4, 0x5D54B200 >> 4, 0x5DA39500 >> 4,
	0x5DF28880 >> 4, 0x5E418C80 >> 4, 0x5E90A100 >> 4,
	0x5EDFC680 >> 4, 0x5F2EFC00 >> 4, 0x5F7E4280 >> 4,
	0x5FCD9900 >> 4, 0x601D0080 >> 4, 0x606C7800 >> 4,
	0x60BC0000 >> 4, 0x610B9800 >> 4, 0x615B4100 >> 4,
	0x61AAF980 >> 4, 0x61FAC300 >> 4, 0x624A9C80 >> 4,
	0x629A8600 >> 4, 0x62EA8000 >> 4, 0x633A8A00 >> 4,
	0x638AA480 >> 4, 0x63DACF00 >> 4, 0x642B0980 >> 4,
	0x647B5400 >> 4, 0x64CBAE80 >> 4, 0x651C1900 >> 4,
	0x656C9400 >> 4, 0x65BD1E80 >> 4, 0x660DB900 >> 4,
	0x665E6380 >> 4, 0x66AF1E00 >> 4, 0x66FFE880 >> 4,
	0x6750C280 >> 4, 0x67A1AC80 >> 4, 0x67F2A600 >> 4,
	0x6843B000 >> 4, 0x6894C900 >> 4, 0x68E5F200 >> 4,
	0x69372B00 >> 4, 0x69887380 >> 4, 0x69D9CB80 >> 4,
	0x6A2B3300 >> 4, 0x6A7CAA80 >> 4, 0x6ACE3180 >> 4,
	0x6B1FC800 >> 4, 0x6B716E00 >> 4, 0x6BC32400 >> 4,
	0x6C14E900 >> 4, 0x6C66BD80 >> 4, 0x6CB8A180 >> 4,
	0x6D0A9500 >> 4, 0x6D5C9800 >> 4, 0x6DAEAA00 >> 4,
	0x6E00CB80 >> 4, 0x6E52FC80 >> 4, 0x6EA53D00 >> 4,
	0x6EF78C80 >> 4, 0x6F49EB80 >> 4, 0x6F9C5980 >> 4,
	0x6FEED700 >> 4, 0x70416380 >> 4, 0x7093FF00 >> 4,
	0x70E6AA00 >> 4, 0x71396400 >> 4, 0x718C2D00 >> 4,
	0x71DF0580 >> 4, 0x7231ED00 >> 4, 0x7284E300 >> 4,
	0x72D7E880 >> 4, 0x732AFD00 >> 4, 0x737E2080 >> 4,
	0x73D15300 >> 4, 0x74249480 >> 4, 0x7477E480 >> 4,
	0x74CB4400 >> 4, 0x751EB200 >> 4, 0x75722F00 >> 4,
	0x75C5BB00 >> 4, 0x76195580 >> 4, 0x766CFF00 >> 4,
	0x76C0B700 >> 4, 0x77147E00 >> 4, 0x77685400 >> 4,
	0x77BC3880 >> 4, 0x78102B80 >> 4, 0x78642D80 >> 4,
	0x78B83E00 >> 4, 0x790C5D00 >> 4, 0x79608B00 >> 4,
	0x79B4C780 >> 4, 0x7A091280 >> 4, 0x7A5D6C00 >> 4,
	0x7AB1D400 >> 4, 0x7B064A80 >> 4, 0x7B5ACF80 >> 4,
	0x7BAF6380 >> 4, 0x7C040580 >> 4, 0x7C58B600 >> 4,
	0x7CAD7500 >> 4, 0x7D024200 >> 4, 0x7D571E00 >> 4,
	0x7DAC0800 >> 4, 0x7E010080 >> 4, 0x7E560780 >> 4,
	0x7EAB1C80 >> 4, 0x7F004000 >> 4, 0x7F557200 >> 4,
	0x7FAAB200 >> 4, 0x7FFFFFFF >> 4,
}

// mantissaTable (MantissaTable, aac_rom.cpp:205) is declared in
// aac_rom_stereo.go and reused here; both the intensity-stereo tools and the
// inverse quantizer index the same Q1.31 gain mantissas.

// exponentTable is the 1:1 port of ExponentTable (aac_rom.cpp:219): the signed
// exponents paired with mantissaTable.
var exponentTable = [4][14]int8{
	{1, 2, 3, 5, 6, 7, 9, 10, 11, 13, 14, 15, 17, 18},
	{1, 2, 3, 5, 6, 7, 9, 10, 11, 13, 14, 15, 17, 18},
	{1, 2, 4, 5, 6, 8, 9, 10, 12, 13, 14, 16, 17, 18},
	{1, 3, 4, 5, 7, 8, 9, 11, 12, 13, 15, 16, 17, 19},
}
