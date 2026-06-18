// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Huffman bit-length data the takehiro bit-counting slice reads.
//
// LAME's takehiro.c count_bit_* routines never touch the Huffman code words
// (the *HB tables) — they only sum bit LENGTHS. They read three kinds of
// length data from tables.c:
//
//   - per-table hlen arrays (ht[t].hlen) for the no-ESC tables, indexed by the
//     packed pair x1*xlen + x2 (count_bit_noESC / count_bit_noESC_from3);
//   - the precomputed packed tables largetbl / table23 / table56, each entry
//     holding two tables' lengths in the high/low 16 bits so one lookup costs
//     both candidate tables at once (count_bit_ESC / count_bit_noESC_from2);
//   - the count1 quadruple bit lengths t32l / t33l (noquant_count_bits).
//
// Every array below is a verbatim transcription of the tables.c data named in
// its comment. They are integer data and bit-identical regardless of build
// tag. The hlen arrays are wired into the shared ht[] header array (declared
// in huffman_encode.go) by populateHuffmanLengths, called from huffman_init
// so the count routines find ht[t].Hlen / ht[t].Xlen / ht[t].Linmax
// populated. The code-word (.Table) members ht[] also carries belong to a
// separate tables.c port slice (huffman_codewords.go's populateHuffmanCodeWords,
// also called from huffman_init); the counters
// here never read them.

// t1l is tables.c:280's t1l[2*2]: ht[1].hlen, read by count_bit_noESC.
var t1l = []uint8{
	1, 4,
	3, 5,
}

// t7l / t8l / t9l are tables.c's t7l / t8l / t9l[6*6]: ht[7..9].hlen, read by
// count_bit_noESC_from3 when the base table is 7.
var t7l = []uint8{
	1, 4, 7, 9, 9, 10,
	4, 6, 8, 9, 9, 10,
	7, 7, 9, 10, 10, 11,
	8, 9, 10, 11, 11, 11,
	8, 9, 10, 11, 11, 12,
	9, 10, 11, 12, 12, 12,
}

var t8l = []uint8{
	2, 4, 7, 9, 9, 10,
	4, 4, 6, 10, 10, 10,
	7, 6, 8, 10, 10, 11,
	9, 10, 10, 11, 11, 12,
	9, 9, 10, 11, 12, 12,
	10, 10, 11, 11, 13, 13,
}

var t9l = []uint8{
	3, 4, 6, 7, 9, 10,
	4, 5, 6, 7, 8, 10,
	5, 6, 7, 8, 9, 10,
	7, 7, 8, 9, 9, 10,
	8, 8, 9, 9, 10, 11,
	9, 9, 10, 10, 11, 11,
}

// t10l / t11l / t12l are tables.c's t10l / t11l / t12l[8*8]: ht[10..12].hlen,
// read by count_bit_noESC_from3 when the base table is 10.
var t10l = []uint8{
	1, 4, 7, 9, 10, 10, 10, 11,
	4, 6, 8, 9, 10, 11, 10, 10,
	7, 8, 9, 10, 11, 12, 11, 11,
	8, 9, 10, 11, 12, 12, 11, 12,
	9, 10, 11, 12, 12, 12, 12, 12,
	10, 11, 12, 12, 13, 13, 12, 13,
	9, 10, 11, 12, 12, 12, 13, 13,
	10, 10, 11, 12, 12, 13, 13, 13,
}

var t11l = []uint8{
	2, 4, 6, 8, 9, 10, 9, 10,
	4, 5, 6, 8, 10, 10, 9, 10,
	6, 7, 8, 9, 10, 11, 10, 10,
	8, 8, 9, 11, 10, 12, 10, 11,
	9, 10, 10, 11, 11, 12, 11, 12,
	9, 10, 11, 12, 12, 13, 12, 13,
	9, 9, 9, 10, 11, 12, 12, 12,
	9, 9, 10, 11, 12, 12, 12, 12,
}

var t12l = []uint8{
	4, 4, 6, 8, 9, 10, 10, 10,
	4, 5, 6, 7, 9, 9, 10, 10,
	6, 6, 7, 8, 9, 10, 9, 10,
	7, 7, 8, 8, 9, 10, 10, 10,
	8, 8, 9, 9, 10, 10, 10, 11,
	9, 9, 10, 10, 10, 11, 10, 11,
	9, 9, 9, 10, 10, 11, 11, 12,
	10, 10, 10, 11, 11, 11, 11, 12,
}

// t13l / t15l are tables.c's t13l / t15l[16*16]: ht[13] / ht[15].hlen, read by
// count_bit_noESC_from3 when the base table is 13. t16_5l (the "apparently not
// used" ht[14].hlen) sits between them so ht[13+1]==ht[14] resolves.
var t13l = []uint8{
	1, 5, 7, 8, 9, 10, 10, 11, 10, 11, 12, 12, 13, 13, 14, 14,
	4, 6, 8, 9, 10, 10, 11, 11, 11, 11, 12, 12, 13, 14, 14, 14,
	7, 8, 9, 10, 11, 11, 12, 12, 11, 12, 12, 13, 13, 14, 15, 15,
	8, 9, 10, 11, 11, 12, 12, 12, 12, 13, 13, 13, 13, 14, 15, 15,
	9, 9, 11, 11, 12, 12, 13, 13, 12, 13, 13, 14, 14, 15, 15, 16,
	10, 10, 11, 12, 12, 12, 13, 13, 13, 13, 14, 13, 15, 15, 16, 16,
	10, 11, 12, 12, 13, 13, 13, 13, 13, 14, 14, 14, 15, 15, 16, 16,
	11, 11, 12, 13, 13, 13, 14, 14, 14, 14, 15, 15, 15, 16, 18, 18,
	10, 10, 11, 12, 12, 13, 13, 14, 14, 14, 14, 15, 15, 16, 17, 17,
	11, 11, 12, 12, 13, 13, 13, 15, 14, 15, 15, 16, 16, 16, 18, 17,
	11, 12, 12, 13, 13, 14, 14, 15, 14, 15, 16, 15, 16, 17, 18, 19,
	12, 12, 12, 13, 14, 14, 14, 14, 15, 15, 15, 16, 17, 17, 17, 18,
	12, 13, 13, 14, 14, 15, 14, 15, 16, 16, 17, 17, 17, 18, 18, 18,
	13, 13, 14, 15, 15, 15, 16, 16, 16, 16, 16, 17, 18, 17, 18, 18,
	14, 14, 14, 15, 15, 15, 17, 16, 16, 19, 17, 17, 17, 19, 18, 18,
	13, 14, 15, 16, 16, 16, 17, 16, 17, 17, 18, 18, 21, 20, 21, 18,
}

var t16_5l = []uint8{
	1, 5, 7, 9, 10, 10, 11, 11, 12, 12, 12, 13, 13, 13, 14, 11,
	4, 6, 8, 9, 10, 11, 11, 11, 12, 12, 12, 13, 14, 13, 14, 11,
	7, 8, 9, 10, 11, 11, 12, 12, 13, 12, 13, 13, 13, 14, 14, 12,
	9, 9, 10, 11, 11, 12, 12, 12, 13, 13, 14, 14, 14, 15, 15, 13,
	10, 10, 11, 11, 12, 12, 13, 13, 13, 14, 14, 14, 15, 15, 15, 12,
	10, 10, 11, 11, 12, 13, 13, 14, 13, 14, 14, 15, 15, 15, 16, 13,
	11, 11, 11, 12, 13, 13, 13, 13, 14, 14, 14, 14, 15, 15, 16, 13,
	11, 11, 12, 12, 13, 13, 13, 14, 14, 15, 15, 15, 15, 17, 17, 13,
	11, 12, 12, 13, 13, 13, 14, 14, 15, 15, 15, 15, 16, 16, 16, 13,
	12, 12, 12, 13, 13, 14, 14, 15, 15, 15, 15, 16, 15, 16, 15, 14,
	12, 13, 12, 13, 14, 14, 14, 14, 15, 16, 16, 16, 17, 17, 16, 13,
	13, 13, 13, 13, 14, 14, 15, 16, 16, 16, 16, 16, 16, 15, 16, 14,
	13, 14, 14, 14, 14, 15, 15, 15, 15, 17, 16, 16, 16, 16, 18, 14,
	15, 14, 14, 14, 15, 15, 16, 16, 16, 18, 17, 17, 17, 19, 17, 14,
	14, 15, 13, 14, 16, 16, 15, 16, 16, 17, 18, 17, 19, 17, 16, 14,
	11, 11, 11, 12, 12, 13, 13, 13, 14, 14, 14, 14, 14, 14, 14, 12,
}

var t15l = []uint8{
	3, 5, 6, 8, 8, 9, 10, 10, 10, 11, 11, 12, 12, 12, 13, 14,
	5, 5, 7, 8, 9, 9, 10, 10, 10, 11, 11, 12, 12, 12, 13, 13,
	6, 7, 7, 8, 9, 9, 10, 10, 10, 11, 11, 12, 12, 13, 13, 13,
	7, 8, 8, 9, 9, 10, 10, 11, 11, 11, 12, 12, 12, 13, 13, 13,
	8, 8, 9, 9, 10, 10, 11, 11, 11, 11, 12, 12, 12, 13, 13, 13,
	9, 9, 9, 10, 10, 10, 11, 11, 11, 11, 12, 12, 13, 13, 13, 14,
	10, 9, 10, 10, 10, 11, 11, 11, 11, 12, 12, 12, 13, 13, 14, 14,
	10, 10, 10, 11, 11, 11, 11, 12, 12, 12, 12, 12, 13, 13, 13, 14,
	10, 10, 10, 11, 11, 11, 11, 12, 12, 12, 12, 13, 13, 14, 14, 14,
	10, 10, 11, 11, 11, 11, 12, 12, 12, 13, 13, 13, 13, 14, 14, 14,
	11, 11, 11, 11, 12, 12, 12, 12, 12, 13, 13, 13, 13, 14, 15, 14,
	11, 11, 11, 11, 12, 12, 12, 12, 13, 13, 13, 13, 14, 14, 14, 15,
	12, 12, 11, 12, 12, 12, 13, 13, 13, 13, 13, 13, 14, 14, 15, 15,
	12, 12, 12, 12, 12, 13, 13, 13, 13, 14, 14, 14, 14, 14, 15, 15,
	13, 13, 13, 13, 13, 13, 13, 13, 14, 14, 14, 14, 15, 15, 14, 15,
	13, 13, 13, 13, 13, 13, 13, 14, 14, 14, 14, 14, 15, 15, 15, 15,
}

// t32l / t33l are tables.c:398 / 403: the count1 quadruple Huffman bit
// lengths (codebooks 32 / 33), read directly by noquant_count_bits and
// best_huffman_divide. (Transcribed as their summed constants, matching the
// `a + b` literals in tables.c.)
var t32l = []int{
	1 + 0, 4 + 1, 4 + 1, 5 + 2, 4 + 1, 6 + 2, 5 + 2, 6 + 3,
	4 + 1, 5 + 2, 5 + 2, 6 + 3, 5 + 2, 6 + 3, 6 + 3, 6 + 4,
}

var t33l = []int{
	4 + 0, 4 + 1, 4 + 1, 4 + 2, 4 + 1, 4 + 2, 4 + 2, 4 + 3,
	4 + 1, 4 + 2, 4 + 2, 4 + 3, 4 + 2, 4 + 3, 4 + 3, 4 + 4,
}

// largetbl is tables.c:458's largetbl[16*16]: each entry packs
// (ht[16].hlen[i] << 16) + ht[24].hlen[i], so one lookup yields both ESC-table
// candidates' lengths. Read by count_bit_ESC.
var largetbl = []uint32{
	0x010004, 0x050005, 0x070007, 0x090008, 0x0a0009, 0x0a000a, 0x0b000a, 0x0b000b,
	0x0c000b, 0x0c000c, 0x0c000c, 0x0d000c, 0x0d000c, 0x0d000c, 0x0e000d, 0x0a000a,
	0x040005, 0x060006, 0x080007, 0x090008, 0x0a0009, 0x0b000a, 0x0b000a, 0x0b000b,
	0x0c000b, 0x0c000b, 0x0c000c, 0x0d000c, 0x0e000c, 0x0d000c, 0x0e000c, 0x0a000a,
	0x070007, 0x080007, 0x090008, 0x0a0009, 0x0b0009, 0x0b000a, 0x0c000a, 0x0c000b,
	0x0d000b, 0x0c000b, 0x0d000b, 0x0d000c, 0x0d000c, 0x0e000c, 0x0e000d, 0x0b0009,
	0x090008, 0x090008, 0x0a0009, 0x0b0009, 0x0b000a, 0x0c000a, 0x0c000a, 0x0c000b,
	0x0d000b, 0x0d000b, 0x0e000b, 0x0e000c, 0x0e000c, 0x0f000c, 0x0f000c, 0x0c0009,
	0x0a0009, 0x0a0009, 0x0b0009, 0x0b000a, 0x0c000a, 0x0c000a, 0x0d000a, 0x0d000b,
	0x0d000b, 0x0e000b, 0x0e000c, 0x0e000c, 0x0f000c, 0x0f000c, 0x0f000d, 0x0b0009,
	0x0a000a, 0x0a0009, 0x0b000a, 0x0b000a, 0x0c000a, 0x0d000a, 0x0d000b, 0x0e000b,
	0x0d000b, 0x0e000b, 0x0e000c, 0x0f000c, 0x0f000c, 0x0f000c, 0x10000c, 0x0c0009,
	0x0b000a, 0x0b000a, 0x0b000a, 0x0c000a, 0x0d000a, 0x0d000b, 0x0d000b, 0x0d000b,
	0x0e000b, 0x0e000c, 0x0e000c, 0x0e000c, 0x0f000c, 0x0f000c, 0x10000d, 0x0c0009,
	0x0b000b, 0x0b000a, 0x0c000a, 0x0c000a, 0x0d000b, 0x0d000b, 0x0d000b, 0x0e000b,
	0x0e000c, 0x0f000c, 0x0f000c, 0x0f000c, 0x0f000c, 0x11000d, 0x11000d, 0x0c000a,
	0x0b000b, 0x0c000b, 0x0c000b, 0x0d000b, 0x0d000b, 0x0d000b, 0x0e000b, 0x0e000b,
	0x0f000b, 0x0f000c, 0x0f000c, 0x0f000c, 0x10000c, 0x10000d, 0x10000d, 0x0c000a,
	0x0c000b, 0x0c000b, 0x0c000b, 0x0d000b, 0x0d000b, 0x0e000b, 0x0e000b, 0x0f000c,
	0x0f000c, 0x0f000c, 0x0f000c, 0x10000c, 0x0f000d, 0x10000d, 0x0f000d, 0x0d000a,
	0x0c000c, 0x0d000b, 0x0c000b, 0x0d000b, 0x0e000b, 0x0e000c, 0x0e000c, 0x0e000c,
	0x0f000c, 0x10000c, 0x10000c, 0x10000d, 0x11000d, 0x11000d, 0x10000d, 0x0c000a,
	0x0d000c, 0x0d000c, 0x0d000b, 0x0d000b, 0x0e000b, 0x0e000c, 0x0f000c, 0x10000c,
	0x10000c, 0x10000c, 0x10000c, 0x10000d, 0x10000d, 0x0f000d, 0x10000d, 0x0d000a,
	0x0d000c, 0x0e000c, 0x0e000c, 0x0e000c, 0x0e000c, 0x0f000c, 0x0f000c, 0x0f000c,
	0x0f000c, 0x11000c, 0x10000d, 0x10000d, 0x10000d, 0x10000d, 0x12000d, 0x0d000a,
	0x0f000c, 0x0e000c, 0x0e000c, 0x0e000c, 0x0f000c, 0x0f000c, 0x10000c, 0x10000c,
	0x10000d, 0x12000d, 0x11000d, 0x11000d, 0x11000d, 0x13000d, 0x11000d, 0x0d000a,
	0x0e000d, 0x0f000c, 0x0d000c, 0x0e000c, 0x10000c, 0x10000c, 0x0f000c, 0x10000d,
	0x10000d, 0x11000d, 0x12000d, 0x11000d, 0x13000d, 0x11000d, 0x10000d, 0x0d000a,
	0x0a0009, 0x0a0009, 0x0a0009, 0x0b0009, 0x0b0009, 0x0c0009, 0x0c0009, 0x0c0009,
	0x0d0009, 0x0d0009, 0x0d0009, 0x0d000a, 0x0d000a, 0x0d000a, 0x0d000a, 0x0a0006,
}

// table23 is tables.c:497's table23[3*3]: (ht[2].hlen[i] << 16) + ht[3].hlen[i].
// Read by count_bit_noESC_from2 when the base table is 2.
var table23 = []uint32{
	0x010002, 0x040003, 0x070007,
	0x040004, 0x050004, 0x070007,
	0x060006, 0x070007, 0x080008,
}

// table56 is tables.c:507's table56[4*4]: (ht[5].hlen[i] << 16) + ht[6].hlen[i].
// Read by count_bit_noESC_from2 when the base table is 5.
var table56 = []uint32{
	0x010003, 0x040004, 0x070006, 0x080008, 0x040004, 0x050004, 0x080006, 0x090007,
	0x070005, 0x080006, 0x090007, 0x0a0008, 0x080007, 0x080007, 0x090008, 0x0a0009,
}

// populateHuffmanLengths wires the xlen / linmax / hlen members of the shared
// ht[] codebook header array (tables.c:409's ht[HTN], declared in
// huffman_encode.go) for the no-ESC and ESC tables the count_bit_* routines
// read. It mirrors the static initializer of ht[] in tables.c: each entry's
// {xlen, linmax, table, hlen}. Only the lengths (hlen) and the geometry
// (xlen / linmax) are needed for bit COUNTING; the code-word (.Table)
// members are populated by the tables.c port slice that owns the *HB arrays.
// huffman_init calls this once.
func populateHuffmanLengths() {
	// tables.c:409 ht[HTN] = { {xlen,linmax,table,hlen}, ... }
	set := func(i int, xlen, linmax uint, hlen []uint8) {
		ht[i].Xlen = xlen
		ht[i].Linmax = linmax
		ht[i].Hlen = hlen
	}
	// 0..15: no-ESC tables (count_bit_noESC / from2 / from3 read .Hlen).
	set(0, 0, 0, nil)
	set(1, 2, 0, t1l)
	// ht[2], ht[3] lengths come from table23; ht[5], ht[6] from table56.
	// count_bit_noESC_from2 reads those packed tables, not ht[].Hlen, but the
	// xlen geometry it reads (ht[t1].xlen) must still be correct.
	set(2, 3, 0, nil)
	set(3, 3, 0, nil)
	set(4, 0, 0, nil) // apparently not used
	set(5, 4, 0, nil)
	set(6, 4, 0, nil)
	set(7, 6, 0, t7l)
	set(8, 6, 0, t8l)
	set(9, 6, 0, t9l)
	set(10, 8, 0, t10l)
	set(11, 8, 0, t11l)
	set(12, 8, 0, t12l)
	set(13, 16, 0, t13l)
	set(14, 0, 0, t16_5l) // apparently not used (ht[13+1] for from3)
	set(15, 16, 0, t15l)
	// 16..23: ESC table 16 family. count_bit_ESC reads ht[choice].xlen as
	// linbits and choose_table reads ht[].linmax to pick choice/choice2.
	set(16, 1, 1, nil)
	set(17, 2, 3, nil)
	set(18, 3, 7, nil)
	set(19, 4, 15, nil)
	set(20, 6, 63, nil)
	set(21, 8, 255, nil)
	set(22, 10, 1023, nil)
	set(23, 13, 8191, nil)
	// 24..31: ESC table 24 family.
	set(24, 4, 15, nil)
	set(25, 5, 31, nil)
	set(26, 6, 63, nil)
	set(27, 7, 127, nil)
	set(28, 8, 255, nil)
	set(29, 9, 511, nil)
	set(30, 11, 2047, nil)
	set(31, 13, 8191, nil)
	// 32..33: count1 quadruple tables (noquant_count_bits reads t32l/t33l
	// directly, not ht[].Hlen).
	set(32, 0, 0, nil)
	set(33, 0, 0, nil)
}
