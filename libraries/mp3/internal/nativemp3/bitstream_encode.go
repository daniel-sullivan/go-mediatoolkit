// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// MP3 bitstream WRITER — the encoder-side bit packer that LAME's bitstream.c
// uses to emit Huffman-coded main data and side info. This is a 1:1
// translation of the vendored LAME 3.100 encoder (liblame/libmp3lame/
// bitstream.c, copyright Mark Taylor / Takehiro Tominaga), a different C
// reference than the minimp3 DECODER that the rest of this package ports: the
// reader (BitStream / GetBits in bitstream.go) consumes a frame's bytes
// MSB-first for decode, whereas EncBitStream / PutBits2 below PRODUCE those
// bytes for encode. The two coexist in nativemp3 because libraries/mp3 serves
// a single pure-Go encode+decode surface; their names are disambiguated by the
// Enc / minimp3 prefixes.
//
// # Scope of this slice
//
// This file set covers bitstream.c's "huffman-encode" area: the low-level bit
// writer (putbits2 / putbits_noheaders / putheader_bits, here) plus the
// Huffman code emitters (huffman_coder_count1 / Huffmancode /
// ShortHuffmancodebits / LongHuffmancodebits, in huffman_encode.go). The frame
// assembler (format_bitstream, encodeSideInfo, writeMainData), the CRC, the
// reservoir drain and the flush/copy plumbing live in other slices and are
// intentionally not translated here.
//
// # Strict mode
//
// This slice is integer-only (kind=integer in the porting work-list): the bit
// writer is pure integer shifting and the only float touch in the Huffman
// emitters is the sign test xr[i] < 0.0f (a comparison, not arithmetic). There
// is no floating-point multiply or add anywhere in the area, so there is no
// FMA-sensitive code and no _fp_strict / _fp_default split. Every function is
// bit-identical regardless of build tag or vectorization.
//
// # Layout / conventions
//
// LAME threads all encoder state through one lame_internal_flags struct
// (util.h:454). The huffman-encode functions read only a handful of its
// members — the bit-stream buffer (gfc->bs), the header ring buffer
// (gfc->sv_enc.header / w_ptr), the per-frame scalefactor-band table
// (gfc->scalefac_band) and a couple of config fields (gfc->cfg.sideinfo_len).
// The bit-stream buffer, header ring, and config the bit writer touches now
// live on the single unified context, LameInternalFlags (context.go) — its
// EncBitStream / EncStateVar / HeaderInfo sub-structs and SessionConfig, plus
// the MaxHeaderBuf / MaxHeaderLen / MaxLength consts, were formerly declared
// here as EncFlags / EncStateVar / EncBitStream / EncSessionConfig and are now
// defined once. LameInternalFlags is the receiver the bit writer and Huffman
// emitters hang off, the Go stand-in for the `gfc` pointer.
//
// Every ported function carries a doc comment naming its bitstream.c C
// counterpart as file:line so a future reader can diff against the C.

// putheaderBits splices the buffered side-info header for the current
// write-pointer slot into the bit stream and advances w_ptr
// (putheader_bits, bitstream.c:133).
//
//	static void
//	putheader_bits(lame_internal_flags * gfc)
//	{
//	    SessionConfig_t const *const cfg = &gfc->cfg;
//	    EncStateVar_t *const esv = &gfc->sv_enc;
//	    Bit_stream_struc *bs = &gfc->bs;
//	    memcpy(&bs->buf[bs->buf_byte_idx], esv->header[esv->w_ptr].buf, cfg->sideinfo_len);
//	    bs->buf_byte_idx += cfg->sideinfo_len;
//	    bs->totbit += cfg->sideinfo_len * 8;
//	    esv->w_ptr = (esv->w_ptr + 1) & (MAX_HEADER_BUF - 1);
//	}
func (gfc *LameInternalFlags) putheaderBits() {
	cfg := &gfc.Cfg
	esv := &gfc.SvEnc
	bs := &gfc.Bs
	copy(bs.Buf[bs.BufByteIdx:bs.BufByteIdx+cfg.SideinfoLen], esv.Header[esv.WPtr].Buf[:cfg.SideinfoLen])
	bs.BufByteIdx += cfg.SideinfoLen
	bs.Totbit += cfg.SideinfoLen * 8
	esv.WPtr = (esv.WPtr + 1) & (MaxHeaderBuf - 1)
}

// PutBits2 writes the low j bits of val into the output stream MSB-first,
// splicing in a frame header whenever the running bit count reaches a buffered
// header's write_timing (putbits2, bitstream.c:152).
//
//	inline static void
//	putbits2(lame_internal_flags * gfc, int val, int j)
//	{
//	    EncStateVar_t const *const esv = &gfc->sv_enc;
//	    Bit_stream_struc *bs;
//	    bs = &gfc->bs;
//	    assert(j < MAX_LENGTH - 2);
//	    while (j > 0) {
//	        int     k;
//	        if (bs->buf_bit_idx == 0) {
//	            bs->buf_bit_idx = 8;
//	            bs->buf_byte_idx++;
//	            if (esv->header[esv->w_ptr].write_timing == bs->totbit) {
//	                putheader_bits(gfc);
//	            }
//	            bs->buf[bs->buf_byte_idx] = 0;
//	        }
//	        k = Min(j, bs->buf_bit_idx);
//	        j -= k;
//	        bs->buf_bit_idx -= k;
//	        bs->buf[bs->buf_byte_idx] |= ((val >> j) << bs->buf_bit_idx);
//	        bs->totbit += k;
//	    }
//	}
//
// The C asserts (j < MAX_LENGTH-2, buf_byte_idx < BUFFER_SIZE,
// write_timing >= totbit, buf_bit_idx < MAX_LENGTH) are debug-only invariants;
// the port relies on the same caller contract rather than re-checking them, and
// the buf[] OR/shift is done in int to mirror the C `unsigned char |= (int)`.
func (gfc *LameInternalFlags) PutBits2(val, j int) {
	esv := &gfc.SvEnc
	bs := &gfc.Bs

	for j > 0 {
		if bs.BufBitIdx == 0 {
			bs.BufBitIdx = 8
			bs.BufByteIdx++
			if esv.Header[esv.WPtr].WriteTiming == bs.Totbit {
				gfc.putheaderBits()
			}
			bs.Buf[bs.BufByteIdx] = 0
		}

		k := j
		if bs.BufBitIdx < k {
			k = bs.BufBitIdx
		}
		j -= k

		bs.BufBitIdx -= k

		bs.Buf[bs.BufByteIdx] |= byte((val >> uint(j)) << uint(bs.BufBitIdx))
		bs.Totbit += k
	}
}

// PutBitsNoHeaders writes the low j bits of val into the output stream
// MSB-first WITHOUT splicing in any frame header — used for ancillary/stuffing
// data that must not trigger a header write (putbits_noheaders,
// bitstream.c:188).
//
//	inline static void
//	putbits_noheaders(lame_internal_flags * gfc, int val, int j)
//	{
//	    Bit_stream_struc *bs;
//	    bs = &gfc->bs;
//	    assert(j < MAX_LENGTH - 2);
//	    while (j > 0) {
//	        int     k;
//	        if (bs->buf_bit_idx == 0) {
//	            bs->buf_bit_idx = 8;
//	            bs->buf_byte_idx++;
//	            bs->buf[bs->buf_byte_idx] = 0;
//	        }
//	        k = Min(j, bs->buf_bit_idx);
//	        j -= k;
//	        bs->buf_bit_idx -= k;
//	        bs->buf[bs->buf_byte_idx] |= ((val >> j) << bs->buf_bit_idx);
//	        bs->totbit += k;
//	    }
//	}
func (gfc *LameInternalFlags) PutBitsNoHeaders(val, j int) {
	bs := &gfc.Bs

	for j > 0 {
		if bs.BufBitIdx == 0 {
			bs.BufBitIdx = 8
			bs.BufByteIdx++
			bs.Buf[bs.BufByteIdx] = 0
		}

		k := j
		if bs.BufBitIdx < k {
			k = bs.BufBitIdx
		}
		j -= k

		bs.BufBitIdx -= k

		bs.Buf[bs.BufByteIdx] |= byte((val >> uint(j)) << uint(bs.BufBitIdx))
		bs.Totbit += k
	}
}
