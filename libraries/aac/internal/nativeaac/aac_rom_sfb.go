// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// This file ports the scalefactor-band-offset ("sfb_offset") ROM and the
// sampling-rate-info lookup the parse / dequant stages index, 1:1 from the
// vendored Fraunhofer FDK-AAC reference (aac_rom.cpp + channelinfo.cpp). Every
// definition here is an integer-domain ROM table or an integer lookup: the
// AAC-LC decode path is fixed-point (the spectral values are int32 FIXP_DBL),
// so these tables are bit-identical regardless of build tag. This file carries
// only the `aacfdk` license fence and no aac_strict FP split — there is no
// float anywhere in this path.

// The per-resolution scalefactor-band offset tables. Each value is the index
// of the first spectral line of a scalefactor band; the final entry is the
// total transform length. 1:1 transcription of the static const SHORT sfb_*
// arrays in aac_rom.cpp. The trailing comment on each C array states its
// scalefactor-band count (= len-1); reproduced here for cross-checking against
// the numberOfSfb* fields of sfbOffsetTables below.
//
// C counterpart: libAACdec/src/aac_rom.cpp:226 (sfb_96_1024) onward.

// 1024-line long / 128-line short tables (samplesPerFrame == 1024).
var (
	// sfb_96_1024 — aac_rom.cpp:226 (41 scfbands).
	sfb961024 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44, 48, 52,
		56, 64, 72, 80, 88, 96, 108, 120, 132, 144, 156, 172, 188, 212,
		240, 276, 320, 384, 448, 512, 576, 640, 704, 768, 832, 896, 960, 1024}
	// sfb_96_128 — aac_rom.cpp:231 (12 scfbands).
	sfb96128 = []int16{0, 4, 8, 12, 16, 20, 24, 32, 40, 48, 64, 92, 128}

	// sfb_64_1024 — aac_rom.cpp:235 (47 scfbands).
	sfb641024 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44,
		48, 52, 56, 64, 72, 80, 88, 100, 112, 124, 140, 156,
		172, 192, 216, 240, 268, 304, 344, 384, 424, 464, 504, 544,
		584, 624, 664, 704, 744, 784, 824, 864, 904, 944, 984, 1024}
	// sfb_64_128 — aac_rom.cpp:242 (12 scfbands).
	sfb64128 = []int16{0, 4, 8, 12, 16, 20, 24, 32, 40, 48, 64, 92, 128}

	// sfb_48_1024 — aac_rom.cpp:246 (49 scfbands).
	sfb481024 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 48, 56,
		64, 72, 80, 88, 96, 108, 120, 132, 144, 160, 176, 196, 216,
		240, 264, 292, 320, 352, 384, 416, 448, 480, 512, 544, 576, 608,
		640, 672, 704, 736, 768, 800, 832, 864, 896, 928, 1024}
	// sfb_48_128 — aac_rom.cpp:252 (14 scfbands).
	sfb48128 = []int16{0, 4, 8, 12, 16, 20, 28, 36, 44, 56, 68, 80, 96, 112, 128}

	// sfb_32_1024 — aac_rom.cpp:256 (51 scfbands).
	sfb321024 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 48, 56,
		64, 72, 80, 88, 96, 108, 120, 132, 144, 160, 176, 196, 216,
		240, 264, 292, 320, 352, 384, 416, 448, 480, 512, 544, 576, 608,
		640, 672, 704, 736, 768, 800, 832, 864, 896, 928, 960, 992, 1024}

	// sfb_24_1024 — aac_rom.cpp:263 (47 scfbands).
	sfb241024 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44,
		52, 60, 68, 76, 84, 92, 100, 108, 116, 124, 136, 148,
		160, 172, 188, 204, 220, 240, 260, 284, 308, 336, 364, 396,
		432, 468, 508, 552, 600, 652, 704, 768, 832, 896, 960, 1024}
	// sfb_24_128 — aac_rom.cpp:270 (15 scfbands).
	sfb24128 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 36, 44, 52, 64, 76, 92, 108, 128}

	// sfb_16_1024 — aac_rom.cpp:274 (43 scfbands).
	sfb161024 = []int16{
		0, 8, 16, 24, 32, 40, 48, 56, 64, 72, 80, 88, 100, 112, 124,
		136, 148, 160, 172, 184, 196, 212, 228, 244, 260, 280, 300, 320, 344, 368,
		396, 424, 456, 492, 532, 572, 616, 664, 716, 772, 832, 896, 960, 1024}
	// sfb_16_128 — aac_rom.cpp:280 (15 scfbands).
	sfb16128 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 32, 40, 48, 60, 72, 88, 108, 128}

	// sfb_8_1024 — aac_rom.cpp:284 (40 scfbands).
	sfb81024 = []int16{
		0, 12, 24, 36, 48, 60, 72, 84, 96, 108, 120, 132, 144, 156,
		172, 188, 204, 220, 236, 252, 268, 288, 308, 328, 348, 372, 396, 420,
		448, 476, 508, 544, 580, 620, 664, 712, 764, 820, 880, 944, 1024}
	// sfb_8_128 — aac_rom.cpp:290 (15 scfbands).
	sfb8128 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 36, 44, 52, 60, 72, 88, 108, 128}
)

// 960-line long / 120-line short tables (samplesPerFrame == 960).
var (
	// sfb_96_960 — aac_rom.cpp:294 (40 scfbands).
	sfb96960 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40,
		44, 48, 52, 56, 64, 72, 80, 88, 96, 108, 120,
		132, 144, 156, 172, 188, 212, 240, 276, 320, 384, 448,
		512, 576, 640, 704, 768, 832, 896, 960}
	// sfb_96_120 — aac_rom.cpp:299 (12 scfbands).
	sfb96120 = []int16{0, 4, 8, 12, 16, 20, 24, 32, 40, 48, 64, 92, 120}

	// sfb_64_960 — aac_rom.cpp:302 (46 scfbands).
	sfb64960 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44,
		48, 52, 56, 64, 72, 80, 88, 100, 112, 124, 140, 156,
		172, 192, 216, 240, 268, 304, 344, 384, 424, 464, 504, 544,
		584, 624, 664, 704, 744, 784, 824, 864, 904, 944, 960}
	// sfb_64_120 — aac_rom.cpp:308 (12 scfbands).
	sfb64120 = []int16{0, 4, 8, 12, 16, 20, 24, 32, 40, 48, 64, 92, 120}

	// sfb_48_960 — aac_rom.cpp:311 (49 scfbands).
	sfb48960 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 48, 56,
		64, 72, 80, 88, 96, 108, 120, 132, 144, 160, 176, 196, 216,
		240, 264, 292, 320, 352, 384, 416, 448, 480, 512, 544, 576, 608,
		640, 672, 704, 736, 768, 800, 832, 864, 896, 928, 960}
	// sfb_48_120 — aac_rom.cpp:316 (14 scfbands).
	sfb48120 = []int16{0, 4, 8, 12, 16, 20, 28, 36, 44, 56, 68, 80, 96, 112, 120}

	// sfb_32_960 — aac_rom.cpp:320 (49 scfbands).
	sfb32960 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 48, 56,
		64, 72, 80, 88, 96, 108, 120, 132, 144, 160, 176, 196, 216,
		240, 264, 292, 320, 352, 384, 416, 448, 480, 512, 544, 576, 608,
		640, 672, 704, 736, 768, 800, 832, 864, 896, 928, 960}

	// sfb_24_960 — aac_rom.cpp:326 (46 scfbands).
	sfb24960 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44,
		52, 60, 68, 76, 84, 92, 100, 108, 116, 124, 136, 148,
		160, 172, 188, 204, 220, 240, 260, 284, 308, 336, 364, 396,
		432, 468, 508, 552, 600, 652, 704, 768, 832, 896, 960}
	// sfb_24_120 — aac_rom.cpp:332 (15 scfbands).
	sfb24120 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 36, 44, 52, 64, 76, 92, 108, 120}

	// sfb_16_960 — aac_rom.cpp:336 (42 scfbands).
	sfb16960 = []int16{0, 8, 16, 24, 32, 40, 48, 56,
		64, 72, 80, 88, 100, 112, 124, 136,
		148, 160, 172, 184, 196, 212, 228, 244,
		260, 280, 300, 320, 344, 368, 396, 424,
		456, 492, 532, 572, 616, 664, 716, 772,
		832, 896, 960}
	// sfb_16_120 — aac_rom.cpp:343 (15 scfbands).
	sfb16120 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 32, 40, 48, 60, 72, 88, 108, 120}

	// sfb_8_960 — aac_rom.cpp:347 (40 scfbands).
	sfb8960 = []int16{0, 12, 24, 36, 48, 60, 72, 84, 96,
		108, 120, 132, 144, 156, 172, 188, 204, 220,
		236, 252, 268, 288, 308, 328, 348, 372, 396,
		420, 448, 476, 508, 544, 580, 620, 664, 712,
		764, 820, 880, 944, 960}
	// sfb_8_120 — aac_rom.cpp:353 (15 scfbands).
	sfb8120 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 36, 44, 52, 60, 72, 88, 108, 120}
)

// 768-line long / 96-line short tables (samplesPerFrame == 768).
var (
	// sfb_96_768 — aac_rom.cpp:357 (37 scfbands).
	sfb96768 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 32, 36,
		40, 44, 48, 52, 56, 64, 72, 80, 88, 96,
		108, 120, 132, 144, 156, 172, 188, 212, 240, 276,
		320, 384, 448, 512, 576, 640, 704, 768}
	// sfb_96_96 — aac_rom.cpp:362 (12 scfbands).
	sfb9696 = []int16{0, 4, 8, 12, 16, 20, 24, 32, 40, 48, 64, 92, 96}

	// sfb_64_768 — aac_rom.cpp:365 (41 scfbands).
	sfb64768 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40,
		44, 48, 52, 56, 64, 72, 80, 88, 100, 112, 124,
		140, 156, 172, 192, 216, 240, 268, 304, 344, 384, 424,
		464, 504, 544, 584, 624, 664, 704, 744, 768}
	// sfb_64_96 — aac_rom.cpp:372 (12 scfbands).
	sfb6496 = []int16{0, 4, 8, 12, 16, 20, 24, 32, 40, 48, 64, 92, 96}

	// sfb_48_768 — aac_rom.cpp:375 (43 scfbands).
	sfb48768 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 48,
		56, 64, 72, 80, 88, 96, 108, 120, 132, 144, 160, 176,
		196, 216, 240, 264, 292, 320, 352, 384, 416, 448, 480, 512,
		544, 576, 608, 640, 672, 704, 736, 768}
	// sfb_48_96 — aac_rom.cpp:381 (12 scfbands).
	sfb4896 = []int16{0, 4, 8, 12, 16, 20, 28, 36, 44, 56, 68, 80, 96}

	// sfb_32_768 — aac_rom.cpp:384 (43 scfbands).
	sfb32768 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 48,
		56, 64, 72, 80, 88, 96, 108, 120, 132, 144, 160, 176,
		196, 216, 240, 264, 292, 320, 352, 384, 416, 448, 480, 512,
		544, 576, 608, 640, 672, 704, 736, 768}

	// sfb_24_768 — aac_rom.cpp:390 (43 scfbands).
	sfb24768 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44,
		52, 60, 68, 76, 84, 92, 100, 108, 116, 124, 136, 148,
		160, 172, 188, 204, 220, 240, 260, 284, 308, 336, 364, 396,
		432, 468, 508, 552, 600, 652, 704, 768}
	// sfb_24_96 — aac_rom.cpp:396 (14 scfbands).
	sfb2496 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 36, 44, 52, 64, 76, 92, 96}

	// sfb_16_768 — aac_rom.cpp:399 (39 scfbands).
	sfb16768 = []int16{0, 8, 16, 24, 32, 40, 48, 56, 64,
		72, 80, 88, 100, 112, 124, 136, 148, 160,
		172, 184, 196, 212, 228, 244, 260, 280, 300,
		320, 344, 368, 396, 424, 456, 492, 532, 572,
		616, 664, 716, 768}
	// sfb_16_96 — aac_rom.cpp:405 (14 scfbands).
	sfb1696 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 32, 40, 48, 60, 72, 88, 96}

	// sfb_8_768 — aac_rom.cpp:408 (37 scfbands).
	sfb8768 = []int16{0, 12, 24, 36, 48, 60, 72, 84, 96, 108,
		120, 132, 144, 156, 172, 188, 204, 220, 236, 252,
		268, 288, 308, 328, 348, 372, 396, 420, 448, 476,
		508, 544, 580, 620, 664, 712, 764, 768}
	// sfb_8_96 — aac_rom.cpp:414 (14 scfbands).
	sfb896 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 36, 44, 52, 60, 72, 88, 96}
)

// 512-line and 480-line low-delay long-only tables (samplesPerFrame ==
// 512 / 480). These have no short-window companion.
var (
	// sfb_48_512 — aac_rom.cpp:417 (36 scfbands).
	sfb48512 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44, 48,
		52, 56, 60, 68, 76, 84, 92, 100, 112, 124, 136, 148, 164,
		184, 208, 236, 268, 300, 332, 364, 396, 428, 460, 512}
	// sfb_32_512 — aac_rom.cpp:421 (37 scfbands).
	sfb32512 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 32, 36,
		40, 44, 48, 52, 56, 64, 72, 80, 88, 96,
		108, 120, 132, 144, 160, 176, 192, 212, 236, 260,
		288, 320, 352, 384, 416, 448, 480, 512}
	// sfb_24_512 — aac_rom.cpp:426 (31 scfbands).
	sfb24512 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40,
		44, 52, 60, 68, 80, 92, 104, 120, 140, 164, 192,
		224, 256, 288, 320, 352, 384, 416, 448, 480, 512}

	// sfb_48_480 — aac_rom.cpp:431 (35 scfbands).
	sfb48480 = []int16{
		0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44, 48,
		52, 56, 64, 72, 80, 88, 96, 108, 120, 132, 144, 156, 172,
		188, 212, 240, 272, 304, 336, 368, 400, 432, 480}
	// sfb_32_480 — aac_rom.cpp:435 (37 scfbands).
	sfb32480 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 32, 36,
		40, 44, 48, 52, 56, 60, 64, 72, 80, 88,
		96, 104, 112, 124, 136, 148, 164, 180, 200, 224,
		256, 288, 320, 352, 384, 416, 448, 480}
	// sfb_24_480 — aac_rom.cpp:440 (30 scfbands).
	sfb24480 = []int16{0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40,
		44, 52, 60, 68, 80, 92, 104, 120, 140, 164, 192,
		224, 256, 288, 320, 352, 384, 416, 448, 480}
)

// sfbInfo ports the C struct SFB_INFO (aac_rom.h:147): the long- and
// short-window scalefactor-band offset tables for one (frame-length,
// sampling-rate-index) pair, plus their band counts. sfbOffsetShort is nil for
// the low-delay (512/480) rows, mirroring the C NULL.
type sfbInfo struct {
	sfbOffsetLong    []int16 // const SHORT *sfbOffsetLong
	sfbOffsetShort   []int16 // const SHORT *sfbOffsetShort
	numberOfSfbLong  uint8   // UCHAR numberOfSfbLong
	numberOfSfbShort uint8   // UCHAR numberOfSfbShort
}

// sfbOffsetTables ports the C ROM sfbOffsetTables[5][16] (aac_rom.cpp:445). The
// outer index selects the frame length (0:1024, 1:960, 2:768, 3:512, 4:480) and
// the inner index is the MPEG sampling-rate index (0..15). Rows past the 13
// defined entries are the C array's implicit zero-initialised {NULL, NULL, 0,
// 0} tail (the brace-initialiser lists only 13 of 16 columns); they are left as
// the zero value here to match exactly.
//
// C counterpart: libAACdec/src/aac_rom.cpp:445.
var sfbOffsetTables = [5][16]sfbInfo{
	{
		{sfb961024, sfb96128, 41, 12},
		{sfb961024, sfb96128, 41, 12},
		{sfb641024, sfb64128, 47, 12},
		{sfb481024, sfb48128, 49, 14},
		{sfb481024, sfb48128, 49, 14},
		{sfb321024, sfb48128, 51, 14},
		{sfb241024, sfb24128, 47, 15},
		{sfb241024, sfb24128, 47, 15},
		{sfb161024, sfb16128, 43, 15},
		{sfb161024, sfb16128, 43, 15},
		{sfb161024, sfb16128, 43, 15},
		{sfb81024, sfb8128, 40, 15},
		{sfb81024, sfb8128, 40, 15},
	},
	{
		{sfb96960, sfb96120, 40, 12},
		{sfb96960, sfb96120, 40, 12},
		{sfb64960, sfb64120, 46, 12},
		{sfb48960, sfb48120, 49, 14},
		{sfb48960, sfb48120, 49, 14},
		{sfb32960, sfb48120, 49, 14},
		{sfb24960, sfb24120, 46, 15},
		{sfb24960, sfb24120, 46, 15},
		{sfb16960, sfb16120, 42, 15},
		{sfb16960, sfb16120, 42, 15},
		{sfb16960, sfb16120, 42, 15},
		{sfb8960, sfb8120, 40, 15},
		{sfb8960, sfb8120, 40, 15},
	},
	{
		{sfb96768, sfb9696, 37, 12},
		{sfb96768, sfb9696, 37, 12},
		{sfb64768, sfb6496, 41, 12},
		{sfb48768, sfb4896, 43, 12},
		{sfb48768, sfb4896, 43, 12},
		{sfb32768, sfb4896, 43, 12},
		{sfb24768, sfb2496, 43, 14},
		{sfb24768, sfb2496, 43, 14},
		{sfb16768, sfb1696, 39, 14},
		{sfb16768, sfb1696, 39, 14},
		{sfb16768, sfb1696, 39, 14},
		{sfb8768, sfb896, 37, 14},
		{sfb8768, sfb896, 37, 14},
	},
	{
		{sfb48512, nil, 36, 0},
		{sfb48512, nil, 36, 0},
		{sfb48512, nil, 36, 0},
		{sfb48512, nil, 36, 0},
		{sfb48512, nil, 36, 0},
		{sfb32512, nil, 37, 0},
		{sfb24512, nil, 31, 0},
		{sfb24512, nil, 31, 0},
		{sfb24512, nil, 31, 0},
		{sfb24512, nil, 31, 0},
		{sfb24512, nil, 31, 0},
		{sfb24512, nil, 31, 0},
		{sfb24512, nil, 31, 0},
	},
	{
		{sfb48480, nil, 35, 0},
		{sfb48480, nil, 35, 0},
		{sfb48480, nil, 35, 0},
		{sfb48480, nil, 35, 0},
		{sfb48480, nil, 35, 0},
		{sfb32480, nil, 37, 0},
		{sfb24480, nil, 30, 0},
		{sfb24480, nil, 30, 0},
		{sfb24480, nil, 30, 0},
		{sfb24480, nil, 30, 0},
		{sfb24480, nil, 30, 0},
		{sfb24480, nil, 30, 0},
		{sfb24480, nil, 30, 0},
	},
}

// aacDecoderError mirrors the AAC_DECODER_ERROR enum
// (libAACdec/include/aacdecoder_lib.h:442); only the values getSamplingRateInfo
// can return are transcribed. Carried as the same typed-enum integer shape the
// rest of this package uses for C error enums (cf. transportDecError in
// adts.go), preserving the C return codes 1:1 rather than collapsing to a Go
// sentinel.
type aacDecoderError int

const (
	aacDecOK                aacDecoderError = 0x0000 // AAC_DEC_OK
	aacDecUnsupportedFormat aacDecoderError = 0x2003 // AAC_DEC_UNSUPPORTED_FORMAT
)

// samplingRateInfo ports the C struct SamplingRateInfo (channelinfo.h:152): the
// resolved scalefactor-band ROM (long + short offsets and band counts) plus the
// sampling-rate index / rate that select it. getSamplingRateInfo fills it.
type samplingRateInfo struct {
	scaleFactorBandsLong          []int16 // const SHORT *ScaleFactorBands_Long
	scaleFactorBandsShort         []int16 // const SHORT *ScaleFactorBands_Short
	numberOfScaleFactorBandsLong  uint8   // UCHAR NumberOfScaleFactorBands_Long
	numberOfScaleFactorBandsShort uint8   // UCHAR NumberOfScaleFactorBands_Short
	samplingRateIndex             uint32  // UINT samplingRateIndex
	samplingRate                  uint32  // UINT samplingRate
}

// getSamplingRateInfo fills t for the given frame length / sampling-rate index /
// rate, selecting the matching sfbOffsetTables row. 1:1 port of
// getSamplingRateInfo (channelinfo.cpp:225).
//
// It first resolves the sampling-rate index (when the carried index is invalid
// or the frame is the 768-line AAC-LD case it is searched from the rate against
// the ISO/IEC 13818-7 Table-38 borders), maps samplesPerFrame to the outer ROM
// index, copies the four sfbOffsetTables fields, and validates the long table.
// Returns the C AAC_DECODER_ERROR code as an aacDecoderError: aacDecOK on
// success, aacDecUnsupportedFormat for an unsupported frame length or a missing
// long table.
//
// The two trailing C FDK_ASSERTs (channelinfo.cpp:289/291 — that the long table
// terminates at samplesPerFrame and the short table at samplesPerFrame/8) are
// debug-only invariants over the ROM, not run-time error paths, so they are not
// reproduced as returns; the ROM transcription above satisfies them by
// construction.
func getSamplingRateInfo(t *samplingRateInfo, samplesPerFrame, samplingRateIndex, samplingRate uint32) aacDecoderError {
	index := 0

	// Search closest samplerate according to ISO/IEC 13818-7:2005(E) 8.2.4
	// (Table 38). channelinfo.cpp:230.
	if samplingRateIndex >= 15 || samplesPerFrame == 768 {
		borders := [12]uint32{^uint32(0), 92017, 75132, 55426, 46009, 37566,
			27713, 23004, 18783, 13856, 11502, 9391}
		samplingRateSearch := samplingRate

		if samplesPerFrame == 768 {
			samplingRateSearch = (samplingRate * 4) / 3
		}

		var i uint32
		for i = 0; i < 11; i++ {
			if borders[i] > samplingRateSearch &&
				samplingRateSearch >= borders[i+1] {
				break
			}
		}
		samplingRateIndex = i
	}

	t.samplingRateIndex = samplingRateIndex
	t.samplingRate = samplingRate

	switch samplesPerFrame {
	case 1024:
		index = 0
	case 960:
		index = 1
	case 768:
		index = 2
	case 512:
		index = 3
	case 480:
		index = 4
	default:
		return aacDecUnsupportedFormat
	}

	t.scaleFactorBandsLong = sfbOffsetTables[index][samplingRateIndex].sfbOffsetLong
	t.scaleFactorBandsShort = sfbOffsetTables[index][samplingRateIndex].sfbOffsetShort
	t.numberOfScaleFactorBandsLong = sfbOffsetTables[index][samplingRateIndex].numberOfSfbLong
	t.numberOfScaleFactorBandsShort = sfbOffsetTables[index][samplingRateIndex].numberOfSfbShort

	if t.scaleFactorBandsLong == nil ||
		t.numberOfScaleFactorBandsLong == 0 {
		t.samplingRate = 0
		return aacDecUnsupportedFormat
	}

	return aacDecOK
}
