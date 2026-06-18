// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Data tables consumed by the takehiro bit-counting slice (takehiro.go).
//
// These are 1:1 transcriptions of the static const data the LAME 3.100
// encoder's takehiro.c / tables.c / quantize_pvt.c bit-counting path reads.
// They are grouped here (rather than spread across the functions that read
// them) so the slice's logic file stays a faithful function-for-function
// mirror of takehiro.c. Each table names its C counterpart as file:line.
//
// The packed Huffman bit-length tables (largetbl / table23 / table56 /
// t32l / t33l) and the per-table ht[] header wiring (xlen / linmax / hlen)
// are integer data and therefore bit-identical regardless of build tag; the
// count_bit_* routines that index them are pure integer arithmetic.
//
// NOTE on ownership. pretab (quantize_pvt.c:86) is already declared by the
// quantize_pvt slice (quantize_pvt.go), which the takehiro scale-bitcount
// functions reuse; it is NOT redeclared here. scfsi_band / nr_of_sfb_block
// live in tables.c / quantize_pvt.c and are first read by these takehiro
// functions, so they are declared here; if a later slice needs its own copy,
// fold them into a shared tables file at that time.

// subdvTable is takehiro.c:41's subdv_table[23]: the per-(scalefac-band-count)
// region0 / region1 big-value region split huffman_init uses to derive the
// bv_scf table. Indexed [scfb_anz].
var subdvTable = [23][2]int{
	{0, 0}, // 0 bands
	{0, 0}, // 1 bands
	{0, 0}, // 2 bands
	{0, 0}, // 3 bands
	{0, 0}, // 4 bands
	{0, 1}, // 5 bands
	{1, 1}, // 6 bands
	{1, 1}, // 7 bands
	{1, 2}, // 8 bands
	{2, 2}, // 9 bands
	{2, 3}, // 10 bands
	{2, 3}, // 11 bands
	{3, 4}, // 12 bands
	{3, 4}, // 13 bands
	{3, 4}, // 14 bands
	{4, 5}, // 15 bands
	{4, 5}, // 16 bands
	{4, 6}, // 17 bands
	{5, 6}, // 18 bands
	{5, 6}, // 19 bands
	{5, 7}, // 20 bands
	{6, 7}, // 21 bands
	{6, 7}, // 22 bands
}

// hufTblNoESC is takehiro.c:505's huf_tbl_noESC[15]: maps a region's max
// magnitude (via max-1) to the base no-ESC Huffman table index.
var hufTblNoESC = [15]int{
	1, 2, 5, 7, 7, 10, 10, 13, 13, 13, 13, 13, 13, 13, 13,
}

// slen1N / slen2N are takehiro.c:959's slen1_n / slen2_n[16]: the per
// scalefac_compress maximum-plus-one slen bounds. slen1Tab / slen2Tab are
// takehiro.c:961's slen1_tab / slen2_tab[16]: the slen field bit lengths.
var slen1N = [16]int{1, 1, 1, 1, 8, 2, 2, 2, 4, 4, 4, 8, 8, 8, 16, 16}
var slen2N = [16]int{1, 2, 4, 8, 1, 2, 4, 8, 2, 4, 8, 2, 4, 8, 4, 8}
var slen1Tab = [16]int{0, 0, 0, 0, 3, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4}
var slen2Tab = [16]int{0, 1, 2, 3, 0, 1, 2, 3, 1, 2, 3, 1, 2, 3, 2, 3}

// scaleShort / scaleMixed / scaleLong are takehiro.c:1114 / 1119 / 1124:
// the MPEG-1 scalefactor bit costs per scalefac_compress for short / mixed /
// long blocks (18/17/11 * slen1_tab[i] + 18/18/10 * slen2_tab[i]).
var scaleShort = [16]int{0, 18, 36, 54, 54, 36, 54, 72, 54, 72, 90, 72, 90, 108, 108, 126}
var scaleMixed = [16]int{0, 18, 36, 54, 51, 35, 53, 71, 52, 70, 88, 69, 87, 105, 104, 122}
var scaleLong = [16]int{0, 10, 20, 30, 33, 21, 31, 41, 32, 42, 52, 43, 53, 63, 64, 74}

// maxRangeSfacTab is takehiro.c:1195's max_range_sfac_tab[6][4]: the largest
// scalefactor value each MPEG-2 LSF partition may hold per table_number.
var maxRangeSfacTab = [6][4]int{
	{15, 15, 7, 7},
	{15, 15, 7, 0},
	{7, 3, 0, 0},
	{15, 31, 31, 0},
	{7, 7, 7, 0},
	{3, 3, 0, 0},
}

// scfsiBand is tables.c:562's scfsi_band[5]: the scalefactor-selection
// information band boundaries (IS section 2.4.2.7).
var scfsiBand = [5]int{0, 6, 11, 16, 21}

// nrOfSfbBlock is quantize_pvt.c:51's nr_of_sfb_block[6][3][4]: the MPEG-2
// LSF scalefactor partition sizes, indexed [table_number][row_in_table]
// [partition].
var nrOfSfbBlock = [6][3][4]int{
	{{6, 5, 5, 5}, {9, 9, 9, 9}, {6, 9, 9, 9}},
	{{6, 5, 7, 3}, {9, 9, 12, 6}, {6, 9, 12, 6}},
	{{11, 10, 0, 0}, {18, 18, 0, 0}, {15, 18, 0, 0}},
	{{7, 7, 7, 0}, {12, 12, 12, 0}, {6, 15, 12, 0}},
	{{6, 6, 6, 3}, {12, 9, 9, 6}, {6, 12, 9, 6}},
	{{8, 8, 5, 0}, {15, 12, 9, 0}, {6, 18, 9, 0}},
}

// log2Tab is the local static log2tab[] in mpeg2_scale_bitcount
// (takehiro.c:1268): floor(log2(x))+1 saturated, used to pick each LSF
// partition's slen.
var log2Tab = [16]int{0, 1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4}
