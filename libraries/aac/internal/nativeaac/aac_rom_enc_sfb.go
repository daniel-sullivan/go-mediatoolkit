// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// Encoder-side scalefactor-band WIDTH ROM, 1:1 transcription of the
// p_FDKaacEnc_<rate>_long_1024 / p_FDKaacEnc_<rate>_short_128 SFB_PARAM_LONG /
// SFB_PARAM_SHORT tables in libAACenc/src/aacEnc_rom.cpp:735-809. These are the
// per-band WIDTHS (not cumulative offsets — that is the decode-side
// aac_rom_sfb.go ROM) that FDKaacEnc_initSfbTable accumulates into sfbOffset.
// Every value is an integer band width; pure ROM, bit-identical regardless of
// build tag. No float anywhere.

// sfbParamLong ports SFB_PARAM_LONG (psy_configuration.h:153-156): the
// scalefactor-band count plus the per-band widths for long blocks.
type sfbParamLong struct {
	sfbCnt   int   // number of scalefactor bands
	sfbWidth []int // width of each scalefactor band (long blocks)
}

// sfbParamShort ports SFB_PARAM_SHORT (psy_configuration.h:158-162): the
// scalefactor-band count plus the per-band widths for short blocks.
type sfbParamShort struct {
	sfbCnt   int   // number of scalefactor bands
	sfbWidth []int // width of each scalefactor band (short blocks)
}

// The 1024-line long / 128-line short SFB-width tables (aacEnc_rom.cpp:735-809).
var (
	pFDKaacEnc8000Long1024 = sfbParamLong{
		40, []int{12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 16,
			16, 16, 16, 16, 16, 16, 20, 20, 20, 20, 24, 24, 24, 28,
			28, 32, 36, 36, 40, 44, 48, 52, 56, 60, 64, 80}}
	pFDKaacEnc8000Short128 = sfbParamShort{
		15, []int{4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 8, 12, 16, 20, 20}}

	pFDKaacEnc11025Long1024 = sfbParamLong{
		43, []int{8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 12, 12, 12, 12,
			12, 12, 12, 12, 12, 16, 16, 16, 16, 20, 20, 20, 24, 24, 28,
			28, 32, 36, 40, 40, 44, 48, 52, 56, 60, 64, 64, 64}}
	pFDKaacEnc11025Short128 = sfbParamShort{
		15, []int{4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 12, 12, 16, 20, 20}}

	pFDKaacEnc12000Long1024 = sfbParamLong{
		43, []int{8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 12, 12, 12, 12,
			12, 12, 12, 12, 12, 16, 16, 16, 16, 20, 20, 20, 24, 24, 28,
			28, 32, 36, 40, 40, 44, 48, 52, 56, 60, 64, 64, 64}}
	pFDKaacEnc12000Short128 = sfbParamShort{
		15, []int{4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 12, 12, 16, 20, 20}}

	pFDKaacEnc16000Long1024 = sfbParamLong{
		43, []int{8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 12, 12, 12, 12,
			12, 12, 12, 12, 12, 16, 16, 16, 16, 20, 20, 20, 24, 24, 28,
			28, 32, 36, 40, 40, 44, 48, 52, 56, 60, 64, 64, 64}}
	pFDKaacEnc16000Short128 = sfbParamShort{
		15, []int{4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 12, 12, 16, 20, 20}}

	pFDKaacEnc22050Long1024 = sfbParamLong{
		47, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 8, 8,
			8, 8, 8, 8, 8, 12, 12, 12, 12, 16, 16, 16, 20, 20, 24, 24,
			28, 28, 32, 36, 36, 40, 44, 48, 52, 52, 64, 64, 64, 64, 64}}
	pFDKaacEnc22050Short128 = sfbParamShort{
		15, []int{4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 12, 12, 16, 16, 20}}

	pFDKaacEnc24000Long1024 = sfbParamLong{
		47, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 8, 8,
			8, 8, 8, 8, 8, 12, 12, 12, 12, 16, 16, 16, 20, 20, 24, 24,
			28, 28, 32, 36, 36, 40, 44, 48, 52, 52, 64, 64, 64, 64, 64}}
	pFDKaacEnc24000Short128 = sfbParamShort{
		15, []int{4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 12, 12, 16, 16, 20}}

	pFDKaacEnc32000Long1024 = sfbParamLong{
		51, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 8, 8, 8, 8,
			12, 12, 12, 12, 16, 16, 20, 20, 24, 24, 28, 28, 32, 32, 32, 32, 32,
			32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32}}
	pFDKaacEnc32000Short128 = sfbParamShort{
		14, []int{4, 4, 4, 4, 4, 8, 8, 8, 12, 12, 12, 16, 16, 16}}

	pFDKaacEnc44100Long1024 = sfbParamLong{
		49, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 8, 8, 8, 8,
			12, 12, 12, 12, 16, 16, 20, 20, 24, 24, 28, 28, 32, 32, 32, 32, 32,
			32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 96}}
	pFDKaacEnc44100Short128 = sfbParamShort{
		14, []int{4, 4, 4, 4, 4, 8, 8, 8, 12, 12, 12, 16, 16, 16}}

	pFDKaacEnc48000Long1024 = sfbParamLong{
		49, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 8, 8, 8, 8,
			12, 12, 12, 12, 16, 16, 20, 20, 24, 24, 28, 28, 32, 32, 32, 32, 32,
			32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 96}}
	pFDKaacEnc48000Short128 = sfbParamShort{
		14, []int{4, 4, 4, 4, 4, 8, 8, 8, 12, 12, 12, 16, 16, 16}}

	pFDKaacEnc64000Long1024 = sfbParamLong{
		47, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8,
			8, 8, 12, 12, 12, 16, 16, 16, 20, 24, 24, 28, 36, 40, 40, 40,
			40, 40, 40, 40, 40, 40, 40, 40, 40, 40, 40, 40, 40, 40, 40}}
	pFDKaacEnc64000Short128 = sfbParamShort{
		12, []int{4, 4, 4, 4, 4, 4, 8, 8, 8, 16, 28, 36}}

	pFDKaacEnc88200Long1024 = sfbParamLong{
		41, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
			8, 8, 8, 8, 8, 12, 12, 12, 12, 12, 16, 16, 24, 28,
			36, 44, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64}}
	pFDKaacEnc88200Short128 = sfbParamShort{
		12, []int{4, 4, 4, 4, 4, 4, 8, 8, 8, 16, 28, 36}}

	pFDKaacEnc96000Long1024 = sfbParamLong{
		41, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
			8, 8, 8, 8, 8, 12, 12, 12, 12, 12, 16, 16, 24, 28,
			36, 44, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64}}
	pFDKaacEnc96000Short128 = sfbParamShort{
		12, []int{4, 4, 4, 4, 4, 4, 8, 8, 8, 16, 28, 36}}
)

// sfbInfoTabEntry ports the SFB_INFO_TAB struct (psy_configuration.cpp:111-115):
// a sample rate mapped to its long/short SFB_PARAM tables. paramShort==nil for
// the low-delay (granuleLength 512/480) tables.
type sfbInfoTabEntry struct {
	sampleRate int
	paramLong  *sfbParamLong
	paramShort *sfbParamShort
}

// sfbInfoTab ports the sfbInfoTab[] table (psy_configuration.cpp:117-131): the
// standard 12 sampling rates for granuleLength 1024/960.
var sfbInfoTab = []sfbInfoTabEntry{
	{8000, &pFDKaacEnc8000Long1024, &pFDKaacEnc8000Short128},
	{11025, &pFDKaacEnc11025Long1024, &pFDKaacEnc11025Short128},
	{12000, &pFDKaacEnc12000Long1024, &pFDKaacEnc12000Short128},
	{16000, &pFDKaacEnc16000Long1024, &pFDKaacEnc16000Short128},
	{22050, &pFDKaacEnc22050Long1024, &pFDKaacEnc22050Short128},
	{24000, &pFDKaacEnc24000Long1024, &pFDKaacEnc24000Short128},
	{32000, &pFDKaacEnc32000Long1024, &pFDKaacEnc32000Short128},
	{44100, &pFDKaacEnc44100Long1024, &pFDKaacEnc44100Short128},
	{48000, &pFDKaacEnc48000Long1024, &pFDKaacEnc48000Short128},
	{64000, &pFDKaacEnc64000Long1024, &pFDKaacEnc64000Short128},
	{88200, &pFDKaacEnc88200Long1024, &pFDKaacEnc88200Short128},
	{96000, &pFDKaacEnc96000Long1024, &pFDKaacEnc96000Short128},
}

// The low-delay 512-line long tables (psy_configuration.cpp:134-147): file-local
// statics p_22050_long_512 / p_32000_long_512 / p_44100_long_512.
var (
	p22050Long512 = sfbParamLong{
		31, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 12, 12,
			12, 16, 20, 24, 28, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32}}
	p32000Long512 = sfbParamLong{
		37, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 8, 8,
			12, 12, 12, 12, 16, 16, 16, 20, 24, 24, 28, 32, 32, 32, 32, 32, 32, 32}}
	p44100Long512 = sfbParamLong{
		36, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 8,
			8, 8, 12, 12, 12, 12, 16, 20, 24, 28, 32, 32, 32, 32, 32, 32, 32, 52}}
)

// sfbInfoTabLD512 ports sfbInfoTabLD512[] (psy_configuration.cpp:149-159).
var sfbInfoTabLD512 = []sfbInfoTabEntry{
	{8000, &p22050Long512, nil}, {11025, &p22050Long512, nil},
	{12000, &p22050Long512, nil}, {16000, &p22050Long512, nil},
	{22050, &p22050Long512, nil}, {24000, &p22050Long512, nil},
	{32000, &p32000Long512, nil}, {44100, &p44100Long512, nil},
	{48000, &p44100Long512, nil}, {64000, &p44100Long512, nil},
	{88200, &p44100Long512, nil}, {96000, &p44100Long512, nil},
	{128000, &p44100Long512, nil}, {176400, &p44100Long512, nil},
	{192000, &p44100Long512, nil}, {256000, &p44100Long512, nil},
	{352800, &p44100Long512, nil}, {384000, &p44100Long512, nil},
}

// The low-delay 480-line long tables (psy_configuration.cpp:161-174): file-local
// statics p_22050_long_480 / p_32000_long_480 / p_44100_long_480.
var (
	p22050Long480 = sfbParamLong{
		30, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 12,
			12, 12, 16, 20, 24, 28, 32, 32, 32, 32, 32, 32, 32, 32, 32}}
	p32000Long480 = sfbParamLong{
		37, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 8,
			8, 8, 8, 12, 12, 12, 16, 16, 20, 24, 32, 32, 32, 32, 32, 32, 32, 32}}
	p44100Long480 = sfbParamLong{
		35, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 8, 8, 8, 8,
			8, 12, 12, 12, 12, 12, 16, 16, 24, 28, 32, 32, 32, 32, 32, 32, 48}}
)

// sfbInfoTabLD480 ports sfbInfoTabLD480[] (psy_configuration.cpp:176-186).
var sfbInfoTabLD480 = []sfbInfoTabEntry{
	{8000, &p22050Long480, nil}, {11025, &p22050Long480, nil},
	{12000, &p22050Long480, nil}, {16000, &p22050Long480, nil},
	{22050, &p22050Long480, nil}, {24000, &p22050Long480, nil},
	{32000, &p32000Long480, nil}, {44100, &p44100Long480, nil},
	{48000, &p44100Long480, nil}, {64000, &p44100Long480, nil},
	{88200, &p44100Long480, nil}, {96000, &p44100Long480, nil},
	{128000, &p44100Long480, nil}, {176400, &p44100Long480, nil},
	{192000, &p44100Long480, nil}, {256000, &p44100Long480, nil},
	{352800, &p44100Long480, nil}, {384000, &p44100Long480, nil},
}
