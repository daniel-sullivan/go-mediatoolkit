// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Layer III Huffman ENCODING — the bitstream.c routines that pack a granule's
// quantized big-value and count1 coefficients (gi->l3_enc) into Huffman codes
// and write them through the bit writer. This is a 1:1 translation of the
// vendored LAME 3.100 encoder (liblame/libmp3lame/bitstream.c): the encode-side
// counterpart to minimp3's L3_huffman decode in huffman.go. See
// bitstream_encode.go for this slice's scope, strict-mode (kind=integer:
// no FP arithmetic, only the xr<0 sign test) and layout notes.
//
// The Huffman codebooks themselves (the ht[] array of huffcodetab and the
// packed code/length tables it points at) are a separate data area ported from
// LAME's tables.c; this slice declares the ht variable it indexes but does not
// own the table data. Until the tables slice populates ht, these emitters have
// no codebooks to read — the functions are nonetheless a faithful, building
// 1:1 port of the bitstream.c logic, matching the C symbol-for-symbol.

// HuffCodeTab is LAME's per-table Huffman codebook header
// (struct huffcodetab, tables.h:70).
//
//	struct huffcodetab {
//	    const unsigned int xlen;     // max. x-index+
//	    const unsigned int linmax;   // max number to be stored in linbits
//	    const uint16_t *table;       // pointer to array[xlen][ylen]
//	    const uint8_t  *hlen;        // pointer to array[xlen][ylen]
//	};
//
// Xlen doubles as the count of ESC linbits for the large tables (Huffmancode
// reads h->xlen as `linbits`). Table holds the code words and Hlen their bit
// lengths, both indexed by the packed pair index x1*xlen + x2. The C pointers
// become slices; a nil Table/Hlen denotes a table whose data the tables slice
// has not yet filled in.
type HuffCodeTab struct {
	Xlen   uint     // huffcodetab.xlen
	Linmax uint     // huffcodetab.linmax
	Table  []uint16 // huffcodetab.table
	Hlen   []uint8  // huffcodetab.hlen
}

// htnEncode is HTN, the number of Huffman code tables: 0..31 the big-value
// codebooks, 32..33 the count1 (quadruple) codebooks (tables.h:68,
// #define HTN 34).
const htnEncode = 34

// ht is LAME's global array of Huffman codebook headers
// (const struct huffcodetab ht[HTN], tables.c:409). huffman_coder_count1
// indexes ht[count1table_select + 32]; Huffmancode indexes ht[tableindex].
// The table contents are owned by the tables.c port (a separate data area);
// this slice only reads ht, so it is declared here sized HTN and populated by
// that slice.
var ht [htnEncode]HuffCodeTab

// The per-granule side-information struct gr_info (l3side.h:46) is now defined
// once, in the unified context (context.go), as GrInfo. The huffman-encode
// emitters read its Xr / L3Enc / BigValues / Count1 / TableSelect /
// Region0Count / Region1Count / Count1tableSelect members; the remaining
// gr_info members are filled in and consumed by the quantizer and side-info
// slices.

// huffmanCoderCount1 Huffman-codes the granule's count1 region — the run of
// quadruples whose magnitudes are all 0 or 1 — and writes them through the bit
// stream, returning the number of bits emitted (huffman_coder_count1,
// bitstream.c:491).
//
//	inline static int
//	huffman_coder_count1(lame_internal_flags * gfc, gr_info const *gi)
//	{
//	    struct huffcodetab const *const h = &ht[gi->count1table_select + 32];
//	    int     i, bits = 0;
//	    int const *ix = &gi->l3_enc[gi->big_values];
//	    FLOAT const *xr = &gi->xr[gi->big_values];
//	    assert(gi->count1table_select < 2);
//	    for (i = (gi->count1 - gi->big_values) / 4; i > 0; --i) {
//	        int     huffbits = 0;
//	        int     p = 0, v;
//	        v = ix[0]; if (v) { p += 8; if (xr[0] < 0.0f) huffbits++; }
//	        v = ix[1]; if (v) { p += 4; huffbits *= 2; if (xr[1] < 0.0f) huffbits++; }
//	        v = ix[2]; if (v) { p += 2; huffbits *= 2; if (xr[2] < 0.0f) huffbits++; }
//	        v = ix[3]; if (v) { p++;    huffbits *= 2; if (xr[3] < 0.0f) huffbits++; }
//	        ix += 4; xr += 4;
//	        putbits2(gfc, huffbits + h->table[p], h->hlen[p]);
//	        bits += h->hlen[p];
//	    }
//	    return bits;
//	}
//
// The C pointer walks ix/xr over l3_enc/xr starting at big_values; the port
// indexes both arrays from a running base offset (ixBase) instead. The
// per-line `v` reads (ix[1..3]) match the C: their value gates the p/sign
// update but is otherwise only asserted (<=1) in the C debug build.
func (gfc *LameInternalFlags) huffmanCoderCount1(gi *GrInfo) int {
	h := &ht[gi.Count1tableSelect+32]
	bits := 0

	ixBase := gi.BigValues

	for i := (gi.Count1 - gi.BigValues) / 4; i > 0; i-- {
		huffbits := 0
		p := 0

		if gi.L3Enc[ixBase+0] != 0 {
			p += 8
			if gi.Xr[ixBase+0] < 0.0 {
				huffbits++
			}
		}

		if gi.L3Enc[ixBase+1] != 0 {
			p += 4
			huffbits *= 2
			if gi.Xr[ixBase+1] < 0.0 {
				huffbits++
			}
		}

		if gi.L3Enc[ixBase+2] != 0 {
			p += 2
			huffbits *= 2
			if gi.Xr[ixBase+2] < 0.0 {
				huffbits++
			}
		}

		if gi.L3Enc[ixBase+3] != 0 {
			p++
			huffbits *= 2
			if gi.Xr[ixBase+3] < 0.0 {
				huffbits++
			}
		}

		ixBase += 4
		gfc.PutBits2(huffbits+int(h.Table[p]), int(h.Hlen[p]))
		bits += int(h.Hlen[p])
	}
	return bits
}

// huffmancode Huffman-codes one big-value region [start, end) using codebook
// tableindex and writes it through the bit stream, returning the bits emitted.
// It implements the pseudocode of page 98 of the IS, including the ESC-word
// (linbits) escape used by tables 16..31 for magnitudes >= 15 (Huffmancode,
// bitstream.c:560).
//
//	inline static int
//	Huffmancode(lame_internal_flags * const gfc, const unsigned int tableindex,
//	            int start, int end, gr_info const *gi)
//	{
//	    struct huffcodetab const *const h = &ht[tableindex];
//	    unsigned int const linbits = h->xlen;
//	    int     i, bits = 0;
//	    if (!tableindex) return bits;
//	    for (i = start; i < end; i += 2) {
//	        int16_t  cbits = 0;
//	        uint16_t xbits = 0;
//	        unsigned int xlen = h->xlen;
//	        unsigned int ext = 0;
//	        unsigned int x1 = gi->l3_enc[i];
//	        unsigned int x2 = gi->l3_enc[i + 1];
//	        if (x1 != 0u) { if (gi->xr[i] < 0.0f) ext++; cbits--; }
//	        if (tableindex > 15u) {
//	            if (x1 >= 15u) { uint16_t const linbits_x1 = x1 - 15u; ext |= linbits_x1 << 1u; xbits = linbits; x1 = 15u; }
//	            if (x2 >= 15u) { uint16_t const linbits_x2 = x2 - 15u; ext <<= linbits; ext |= linbits_x2; xbits += linbits; x2 = 15u; }
//	            xlen = 16;
//	        }
//	        if (x2 != 0u) { ext <<= 1; if (gi->xr[i + 1] < 0.0f) ext++; cbits--; }
//	        x1 = x1 * xlen + x2;
//	        xbits -= cbits;
//	        cbits += h->hlen[x1];
//	        putbits2(gfc, h->table[x1], cbits);
//	        putbits2(gfc, (int)ext, xbits);
//	        bits += cbits + xbits;
//	    }
//	    return bits;
//	}
//
// The C fixed-width types are reproduced exactly so the wrap/sign arithmetic
// matches: cbits is int16_t (it goes negative: it decrements once per nonzero
// of the pair, then `xbits -= cbits` adds those back as a width), xbits/ext/x1/
// x2/xlen/linbits are unsigned (uint16_t / unsigned int). `xbits -= cbits`
// promotes both to int in C; the port mirrors that by masking xbits back to 16
// bits and cbits to its int16 range at each assignment so the final
// putbits2(ext, xbits) sees the identical width.
func (gfc *LameInternalFlags) huffmancode(tableindex uint, start, end int, gi *GrInfo) int {
	h := &ht[tableindex]
	linbits := h.Xlen
	bits := 0

	if tableindex == 0 {
		return bits
	}

	for i := start; i < end; i += 2 {
		var cbits int16 = 0
		var xbits uint16 = 0
		xlen := h.Xlen
		var ext uint = 0
		x1 := uint(gi.L3Enc[i])
		x2 := uint(gi.L3Enc[i+1])

		if x1 != 0 {
			if gi.Xr[i] < 0.0 {
				ext++
			}
			cbits--
		}

		if tableindex > 15 {
			// use ESC-words
			if x1 >= 15 {
				linbitsX1 := uint16(x1 - 15)
				ext |= uint(linbitsX1) << 1
				xbits = uint16(linbits)
				x1 = 15
			}

			if x2 >= 15 {
				linbitsX2 := uint16(x2 - 15)
				ext <<= linbits
				ext |= uint(linbitsX2)
				xbits += uint16(linbits)
				x2 = 15
			}
			xlen = 16
		}

		if x2 != 0 {
			ext <<= 1
			if gi.Xr[i+1] < 0.0 {
				ext++
			}
			cbits--
		}

		x1 = x1*xlen + x2
		xbits = uint16(int(xbits) - int(cbits))
		cbits += int16(h.Hlen[x1])

		gfc.PutBits2(int(h.Table[x1]), int(cbits))
		gfc.PutBits2(int(ext), int(xbits))
		bits += int(cbits) + int(xbits)
	}
	return bits
}

// shortHuffmancodebits Huffman-codes a short-block granule's two big-value
// regions (short blocks have no region2) and returns the bits emitted
// (ShortHuffmancodebits, bitstream.c:638).
//
//	static int
//	ShortHuffmancodebits(lame_internal_flags * gfc, gr_info const *gi)
//	{
//	    int     bits;
//	    int     region1Start;
//	    region1Start = 3 * gfc->scalefac_band.s[3];
//	    if (region1Start > gi->big_values)
//	        region1Start = gi->big_values;
//	    bits = Huffmancode(gfc, gi->table_select[0], 0, region1Start, gi);
//	    bits += Huffmancode(gfc, gi->table_select[1], region1Start, gi->big_values, gi);
//	    return bits;
//	}
func (gfc *LameInternalFlags) shortHuffmancodebits(gi *GrInfo) int {
	region1Start := 3 * gfc.ScalefacBand.S[3]
	if region1Start > gi.BigValues {
		region1Start = gi.BigValues
	}

	// short blocks do not have a region2
	bits := gfc.huffmancode(uint(gi.TableSelect[0]), 0, region1Start, gi)
	bits += gfc.huffmancode(uint(gi.TableSelect[1]), region1Start, gi.BigValues, gi)
	return bits
}

// longHuffmancodebits Huffman-codes a long-block granule's three big-value
// regions, locating the region split points via the region0/region1 band
// counts into scalefac_band.l, and returns the bits emitted
// (LongHuffmancodebits, bitstream.c:654).
//
//	static int
//	LongHuffmancodebits(lame_internal_flags * gfc, gr_info const *gi)
//	{
//	    unsigned int i;
//	    int     bigvalues, bits;
//	    int     region1Start, region2Start;
//	    bigvalues = gi->big_values;
//	    i = gi->region0_count + 1;
//	    region1Start = gfc->scalefac_band.l[i];
//	    i += gi->region1_count + 1;
//	    region2Start = gfc->scalefac_band.l[i];
//	    if (region1Start > bigvalues) region1Start = bigvalues;
//	    if (region2Start > bigvalues) region2Start = bigvalues;
//	    bits = Huffmancode(gfc, gi->table_select[0], 0, region1Start, gi);
//	    bits += Huffmancode(gfc, gi->table_select[1], region1Start, region2Start, gi);
//	    bits += Huffmancode(gfc, gi->table_select[2], region2Start, bigvalues, gi);
//	    return bits;
//	}
//
// The C loop index i is unsigned; the port keeps it int (the band counts and
// their +1 increments stay well within int and index ScalefacBand.L the same
// way).
func (gfc *LameInternalFlags) longHuffmancodebits(gi *GrInfo) int {
	bigvalues := gi.BigValues

	i := gi.Region0Count + 1
	region1Start := gfc.ScalefacBand.L[i]
	i += gi.Region1Count + 1
	region2Start := gfc.ScalefacBand.L[i]

	if region1Start > bigvalues {
		region1Start = bigvalues
	}

	if region2Start > bigvalues {
		region2Start = bigvalues
	}

	bits := gfc.huffmancode(uint(gi.TableSelect[0]), 0, region1Start, gi)
	bits += gfc.huffmancode(uint(gi.TableSelect[1]), region1Start, region2Start, gi)
	bits += gfc.huffmancode(uint(gi.TableSelect[2]), region2Start, bigvalues, gi)
	return bits
}
