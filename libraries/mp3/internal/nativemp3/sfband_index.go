// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Encoder scalefactor-band index tables — a 1:1 translation of LAME 3.100's
// const scalefac_struct sfBandIndex[9] (quantize_pvt.c:100). lame_init_params
// (init.go) selects one row by (samplerate_index + 3*version + 6*(out<16000))
// and copies its long (.l) and short (.s) boundaries into gfc->scalefac_band.
// The nine rows are, in order: 22.05/24/16 kHz (MPEG-2 LSF), 44.1/48/32 kHz
// (MPEG-1), then 11.025/12/8 kHz (MPEG-2.5). The psfb21 / psfb12 sub-band
// arrays are all-zero in the table (lame_init_params derives the real
// partitioned values), so only .L and .S are carried; .Psfb21 / .Psfb12 are
// left at their zero value, matching the C's zero initialisers.
//
// Field values mirror the C arrays verbatim. The MPEG-2.5 short rows divide
// their literals by 3 in the C source; the divided integer results are written
// out here (e.g. 12/3 -> 4) so the table is a plain int literal.
var sfBandIndex = [9]ScalefacBand{
	{ // Table B.2.b: 22.05 kHz
		L: [1 + SBMAXl]int{0, 6, 12, 18, 24, 30, 36, 44, 54, 66, 80, 96, 116, 140, 168, 200, 238, 284, 336, 396, 464, 522, 576},
		S: [1 + SBMAXs]int{0, 4, 8, 12, 18, 24, 32, 42, 56, 74, 100, 132, 174, 192},
	},
	{ // Table B.2.c: 24 kHz
		L: [1 + SBMAXl]int{0, 6, 12, 18, 24, 30, 36, 44, 54, 66, 80, 96, 114, 136, 162, 194, 232, 278, 332, 394, 464, 540, 576},
		S: [1 + SBMAXs]int{0, 4, 8, 12, 18, 26, 36, 48, 62, 80, 104, 136, 180, 192},
	},
	{ // Table B.2.a: 16 kHz
		L: [1 + SBMAXl]int{0, 6, 12, 18, 24, 30, 36, 44, 54, 66, 80, 96, 116, 140, 168, 200, 238, 284, 336, 396, 464, 522, 576},
		S: [1 + SBMAXs]int{0, 4, 8, 12, 18, 26, 36, 48, 62, 80, 104, 134, 174, 192},
	},
	{ // Table B.8.b: 44.1 kHz
		L: [1 + SBMAXl]int{0, 4, 8, 12, 16, 20, 24, 30, 36, 44, 52, 62, 74, 90, 110, 134, 162, 196, 238, 288, 342, 418, 576},
		S: [1 + SBMAXs]int{0, 4, 8, 12, 16, 22, 30, 40, 52, 66, 84, 106, 136, 192},
	},
	{ // Table B.8.c: 48 kHz
		L: [1 + SBMAXl]int{0, 4, 8, 12, 16, 20, 24, 30, 36, 42, 50, 60, 72, 88, 106, 128, 156, 190, 230, 276, 330, 384, 576},
		S: [1 + SBMAXs]int{0, 4, 8, 12, 16, 22, 28, 38, 50, 64, 80, 100, 126, 192},
	},
	{ // Table B.8.a: 32 kHz
		L: [1 + SBMAXl]int{0, 4, 8, 12, 16, 20, 24, 30, 36, 44, 54, 66, 82, 102, 126, 156, 194, 240, 296, 364, 448, 550, 576},
		S: [1 + SBMAXs]int{0, 4, 8, 12, 16, 22, 30, 42, 58, 78, 104, 138, 180, 192},
	},
	{ // MPEG-2.5 11.025 kHz
		L: [1 + SBMAXl]int{0, 6, 12, 18, 24, 30, 36, 44, 54, 66, 80, 96, 116, 140, 168, 200, 238, 284, 336, 396, 464, 522, 576},
		S: [1 + SBMAXs]int{0, 4, 8, 12, 18, 26, 36, 48, 62, 80, 104, 134, 174, 192},
	},
	{ // MPEG-2.5 12 kHz
		L: [1 + SBMAXl]int{0, 6, 12, 18, 24, 30, 36, 44, 54, 66, 80, 96, 116, 140, 168, 200, 238, 284, 336, 396, 464, 522, 576},
		S: [1 + SBMAXs]int{0, 4, 8, 12, 18, 26, 36, 48, 62, 80, 104, 134, 174, 192},
	},
	{ // MPEG-2.5 8 kHz
		L: [1 + SBMAXl]int{0, 12, 24, 36, 48, 60, 72, 88, 108, 132, 160, 192, 232, 280, 336, 400, 476, 566, 568, 570, 572, 574, 576},
		S: [1 + SBMAXs]int{0, 8, 16, 24, 36, 52, 72, 96, 124, 160, 162, 164, 166, 192},
	},
}
