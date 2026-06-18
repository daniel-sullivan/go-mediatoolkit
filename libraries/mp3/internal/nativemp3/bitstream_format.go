// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// MP3 bitstream FRAME ASSEMBLER — LAME's format_bitstream and the framing
// machinery around it. This is a 1:1 translation of the vendored LAME 3.100
// encoder (liblame/libmp3lame/bitstream.c, copyright Mark Taylor / Takehiro
// Tominaga). It is the encode-side counterpart that turns a quantized,
// Huffman-coded granule (the gr_info side info plus the bit writer in
// bitstream_encode.go and the emitters in huffman_encode.go) into the framed
// Layer III byte stream the decoder reads.
//
// # Scope of this slice ("bitstream-format")
//
// This file covers bitstream.c's frame-assembly area, the piece
// bitstream_encode.go's doc comment lists as "intentionally not translated
// here":
//
//   - calcFrameLength / getframebits — the per-frame bit budget.
//   - get_max_frame_buffer_size_by_constraint — the buffer-constraint policy
//     init reaches through the EncoderStages seam.
//   - writeheader — the side-info bit packer into the header ring buffer.
//   - CRC_update / CRC_writeheader — the optional error-protection CRC16.
//   - drain_into_ancillary — stuffing / ancillary-data padding.
//   - encodeSideInfo2 — emits the 4-byte frame header + side information.
//   - writeMainData — emits scalefactors + Huffman-coded coefficients.
//   - compute_flushbits / flush_bitstream / add_dummy_byte — the end-of-stream
//     flush plumbing.
//   - do_copy_buffer / copy_buffer — drain the internal bit buffer to the user
//     buffer.
//   - format_bitstream — the top-level frame assembler.
//
// The bit-reservoir framing (ResvFrameBegin / ResvMaxBits / ResvAdjust /
// ResvFrameEnd) is a sibling file (reservoir_encode.go) translating LAME's
// reservoir.c; format_bitstream consumes the resvDrain_pre / resvDrain_post /
// main_data_begin those routines compute.
//
// # Strict mode
//
// This slice is integer-only (kind=integer in the porting work-list): every
// function here is integer bit manipulation with one exception — ResvMaxBits in
// reservoir_encode.go does two double-precision scalings (ResvMax *= 0.9 and
// targBits -= .1*mean_bits) routed through the //go:noinline helpers in
// reservoir_encode_fp_strict.go / reservoir_encode_fp_default.go so the strict
// build cannot fuse the multiply into an FMA. Nothing in bitstream_format.go
// touches floating point, so it is bit-identical regardless of build tag or
// vectorization.
//
// # Layout / conventions
//
// Like the rest of the encoder port, every function hangs off the unified
// context LameInternalFlags (context.go), the Go stand-in for the `gfc`
// pointer, and reads/writes its Cfg (SessionConfig), OvEnc (EncResult), SvEnc
// (EncStateVar — the header ring + reservoir state), Bs (EncBitStream) and
// L3Side (IIISideInfo) sub-structs. Every ported function carries a doc comment
// naming its bitstream.c C counterpart as file:line.

// maxLength is bitstream.c:50 #define MAX_LENGTH 32 — the working bit-width cap
// (already defined once in context.go as MaxLength; referenced here only via
// that const). The C asserts j < MAX_LENGTH-2 are debug-only invariants the
// port relies on the caller contract for, matching bitstream_encode.go.

// calcFrameLength returns the frame length in bits for a given bitrate (kbps)
// and padding flag (calcFrameLength, bitstream.c:60).
//
//	static int
//	calcFrameLength(SessionConfig_t const *const cfg, int kbps, int pad)
//	{
//	    return 8 * ((cfg->version + 1) * 72000 * kbps / cfg->samplerate_out + pad);
//	}
func (gfc *LameInternalFlags) calcFrameLength(kbps, pad int) int {
	cfg := &gfc.Cfg
	return 8 * ((cfg.Version+1)*72000*kbps/cfg.SamplerateOut + pad)
}

// getframebits computes the number of bits in the current Layer III frame from
// the chosen bitrate index (or the average bitrate in free format) and the
// padding flag (getframebits, bitstream.c:70).
//
//	int
//	getframebits(const lame_internal_flags * gfc)
//	{
//	    SessionConfig_t const *const cfg = &gfc->cfg;
//	    EncResult_t const *const eov = &gfc->ov_enc;
//	    int     bit_rate;
//	    if (eov->bitrate_index)
//	        bit_rate = bitrate_table[cfg->version][eov->bitrate_index];
//	    else
//	        bit_rate = cfg->avg_bitrate;
//	    assert(8 <= bit_rate && bit_rate <= 640);
//	    return calcFrameLength(cfg, bit_rate, eov->padding);
//	}
func (gfc *LameInternalFlags) getframebits() int {
	cfg := &gfc.Cfg
	eov := &gfc.OvEnc

	var bitRate int
	if eov.BitrateIndex != 0 {
		bitRate = bitrateTable[cfg.Version][eov.BitrateIndex]
	} else {
		bitRate = cfg.AvgBitrate
	}
	return gfc.calcFrameLength(bitRate, eov.Padding)
}

// Mdb* are the buffer-constraint policies get_max_frame_buffer_size_by_constraint
// selects between (encoder.h MDB_DEFAULT / MDB_STRICT_ISO / MDB_MAXIMUM).
//
//	#define MDB_DEFAULT 0
//	#define MDB_STRICT_ISO 1
//	#define MDB_MAXIMUM 2
const (
	mdbConstraintDefault   = 0 // MDB_DEFAULT
	mdbConstraintStrictISO = 1 // MDB_STRICT_ISO
	mdbConstraintMaximum   = 2 // MDB_MAXIMUM
)

// getMaxFrameBufferSizeByConstraint returns the maximum mp3 buffer size in bits
// the encoder may use for one frame under the given decoder-buffer constraint
// (get_max_frame_buffer_size_by_constraint, bitstream.c:91). lame_init_params
// calls it through the EncoderStages seam to seed cfg->buffer_constraint.
//
//	int
//	get_max_frame_buffer_size_by_constraint(SessionConfig_t const * cfg, int constraint)
//	{
//	    int     maxmp3buf = 0;
//	    if (cfg->avg_bitrate > 320) {
//	        if (constraint == MDB_STRICT_ISO)
//	            maxmp3buf = calcFrameLength(cfg, cfg->avg_bitrate, 0);
//	        else
//	            maxmp3buf = 7680 * (cfg->version + 1);
//	    }
//	    else {
//	        int     max_kbps;
//	        if (cfg->samplerate_out < 16000)
//	            max_kbps = bitrate_table[cfg->version][8];
//	        else
//	            max_kbps = bitrate_table[cfg->version][14];
//	        switch (constraint) {
//	        default:
//	        case MDB_DEFAULT:    maxmp3buf = 8 * 1440; break;
//	        case MDB_STRICT_ISO: maxmp3buf = calcFrameLength(cfg, max_kbps, 0); break;
//	        case MDB_MAXIMUM:    maxmp3buf = 7680 * (cfg->version + 1); break;
//	        }
//	    }
//	    return maxmp3buf;
//	}
func (gfc *LameInternalFlags) getMaxFrameBufferSizeByConstraint(constraint int) int {
	cfg := &gfc.Cfg
	maxmp3buf := 0
	if cfg.AvgBitrate > 320 {
		// in freeformat the buffer is constant
		if constraint == mdbConstraintStrictISO {
			maxmp3buf = gfc.calcFrameLength(cfg.AvgBitrate, 0)
		} else {
			// maximum allowed bits per granule are 7680
			maxmp3buf = 7680 * (cfg.Version + 1)
		}
	} else {
		var maxKbps int
		if cfg.SamplerateOut < 16000 {
			maxKbps = bitrateTable[cfg.Version][8] // default: allow 64 kbps (MPEG-2.5)
		} else {
			maxKbps = bitrateTable[cfg.Version][14]
		}
		switch constraint {
		default:
			fallthrough
		case mdbConstraintDefault:
			// Bouvigne's more lax interpretation: size of a 320kbps 32kHz frame.
			maxmp3buf = 8 * 1440
		case mdbConstraintStrictISO:
			maxmp3buf = gfc.calcFrameLength(maxKbps, 0)
		case mdbConstraintMaximum:
			maxmp3buf = 7680 * (cfg.Version + 1)
		}
	}
	return maxmp3buf
}

// drainIntoAncillary writes remainingBits of stuffing / ancillary data into the
// bit stream: first the literal "LAME" tag bytes (0x4c 0x41 0x4d 0x45), then the
// short version string, then alternating ancillary_flag bits
// (drain_into_ancillary, bitstream.c:225).
//
//	inline static void
//	drain_into_ancillary(lame_internal_flags * gfc, int remainingBits)
//	{
//	    SessionConfig_t const *const cfg = &gfc->cfg;
//	    EncStateVar_t *const esv = &gfc->sv_enc;
//	    int     i;
//	    if (remainingBits >= 8) { putbits2(gfc, 0x4c, 8); remainingBits -= 8; }
//	    if (remainingBits >= 8) { putbits2(gfc, 0x41, 8); remainingBits -= 8; }
//	    if (remainingBits >= 8) { putbits2(gfc, 0x4d, 8); remainingBits -= 8; }
//	    if (remainingBits >= 8) { putbits2(gfc, 0x45, 8); remainingBits -= 8; }
//	    if (remainingBits >= 32) {
//	        const char *const version = get_lame_short_version();
//	        if (remainingBits >= 32)
//	            for (i = 0; i < (int) strlen(version) && remainingBits >= 8; ++i) {
//	                remainingBits -= 8;
//	                putbits2(gfc, version[i], 8);
//	            }
//	    }
//	    for (; remainingBits >= 1; remainingBits -= 1) {
//	        putbits2(gfc, esv->ancillary_flag, 1);
//	        esv->ancillary_flag ^= !cfg->disable_reservoir;
//	    }
//	}
//
// lameShortVersion is get_lame_short_version()'s return for the vendored LAME
// (version.c:86): with LAME 3.100 release / patch 0 the #else branch yields
// MAJOR "." MINOR, i.e. "3.100" — the exact bytes drain_into_ancillary emits.
func (gfc *LameInternalFlags) drainIntoAncillary(remainingBits int) {
	cfg := &gfc.Cfg
	esv := &gfc.SvEnc

	if remainingBits >= 8 {
		gfc.PutBits2(0x4c, 8)
		remainingBits -= 8
	}
	if remainingBits >= 8 {
		gfc.PutBits2(0x41, 8)
		remainingBits -= 8
	}
	if remainingBits >= 8 {
		gfc.PutBits2(0x4d, 8)
		remainingBits -= 8
	}
	if remainingBits >= 8 {
		gfc.PutBits2(0x45, 8)
		remainingBits -= 8
	}

	if remainingBits >= 32 {
		version := lameShortVersion
		if remainingBits >= 32 {
			for i := 0; i < len(version) && remainingBits >= 8; i++ {
				remainingBits -= 8
				gfc.PutBits2(int(version[i]), 8)
			}
		}
	}

	for ; remainingBits >= 1; remainingBits -= 1 {
		gfc.PutBits2(esv.AncillaryFlag, 1)
		// C: ancillary_flag ^= !cfg->disable_reservoir (0/1).
		if cfg.DisableReservoir == 0 {
			esv.AncillaryFlag ^= 1
		}
	}
}

// lameShortVersion is the constant get_lame_short_version() returns for the
// vendored LAME 3.100 build (version.c:86, LAME_TYPE_VERSION==2 release,
// LAME_PATCH_VERSION==0 -> the #else branch "MAJOR"."MINOR" -> "3.100"). The C
// reads it via strlen() in drain_into_ancillary; the port indexes its bytes.
const lameShortVersion = "3.100"

// writeheader appends the low j bits of val MSB-first into the current header
// ring slot's byte buffer, advancing that slot's bit cursor (writeheader,
// bitstream.c:269).
//
//	inline static void
//	writeheader(lame_internal_flags * gfc, int val, int j)
//	{
//	    EncStateVar_t *const esv = &gfc->sv_enc;
//	    int     ptr = esv->header[esv->h_ptr].ptr;
//	    while (j > 0) {
//	        int const k = Min(j, 8 - (ptr & 7));
//	        j -= k;
//	        esv->header[esv->h_ptr].buf[ptr >> 3] |= ((val >> j)) << (8 - (ptr & 7) - k);
//	        ptr += k;
//	    }
//	    esv->header[esv->h_ptr].ptr = ptr;
//	}
func (gfc *LameInternalFlags) writeheader(val, j int) {
	esv := &gfc.SvEnc
	ptr := esv.Header[esv.HPtr].Ptr

	for j > 0 {
		k := 8 - (ptr & 7)
		if j < k {
			k = j
		}
		j -= k
		esv.Header[esv.HPtr].Buf[ptr>>3] |= byte((val >> uint(j)) << uint(8-(ptr&7)-k))
		ptr += k
	}
	esv.Header[esv.HPtr].Ptr = ptr
}

// crcUpdate folds one byte (value) into the running CRC-16 used for Layer III
// error protection (CRC_update, bitstream.c:287).
//
//	static int
//	CRC_update(int value, int crc)
//	{
//	    int     i;
//	    value <<= 8;
//	    for (i = 0; i < 8; i++) {
//	        value <<= 1;
//	        crc <<= 1;
//	        if (((crc ^ value) & 0x10000))
//	            crc ^= CRC16_POLYNOMIAL;
//	    }
//	    return crc;
//	}
func crcUpdate(value, crc int) int {
	value <<= 8
	for i := 0; i < 8; i++ {
		value <<= 1
		crc <<= 1
		if (crc^value)&0x10000 != 0 {
			crc ^= crc16Polynomial
		}
	}
	return crc
}

// crc16Polynomial is util.h:83 #define CRC16_POLYNOMIAL 0x8005.
const crc16Polynomial = 0x8005

// crcWriteheader computes the CRC-16 over header bytes [2],[3] and [6 ..
// sideinfo_len) and stores it into header bytes [4],[5] (CRC_writeheader,
// bitstream.c:303). header is the current ring slot's byte buffer.
//
//	void
//	CRC_writeheader(lame_internal_flags const *gfc, char *header)
//	{
//	    SessionConfig_t const *const cfg = &gfc->cfg;
//	    int     crc = 0xffff;
//	    int     i;
//	    crc = CRC_update(((unsigned char *) header)[2], crc);
//	    crc = CRC_update(((unsigned char *) header)[3], crc);
//	    for (i = 6; i < cfg->sideinfo_len; i++)
//	        crc = CRC_update(((unsigned char *) header)[i], crc);
//	    header[4] = crc >> 8;
//	    header[5] = crc & 255;
//	}
func (gfc *LameInternalFlags) crcWriteheader(header []byte) {
	cfg := &gfc.Cfg
	crc := 0xffff // (jo) init crc16 for error_protection

	crc = crcUpdate(int(header[2]), crc)
	crc = crcUpdate(int(header[3]), crc)
	for i := 6; i < cfg.SideinfoLen; i++ {
		crc = crcUpdate(int(header[i]), crc)
	}

	header[4] = byte(crc >> 8)
	header[5] = byte(crc & 255)
}

// encodeSideInfo2 builds the next frame's 4-byte MPEG header and Layer III side
// information into the header ring slot h_ptr, then schedules that slot's write
// timing and advances h_ptr (encodeSideInfo2, bitstream.c:320). bitsPerFrame is
// getframebits() for the current frame.
//
// The C body is a long sequence of writeheader() calls; the port reproduces it
// field-for-field. The MPEG-1 (cfg->version == 1) and MPEG-2 branches differ in
// the main_data_begin width (9 vs 8 bits), the private-bits width, the presence
// of scfsi, the granule count (2 vs 1) and scalefac_compress width (4 vs 9).
// The table_select==14 -> 16 remaps mutate gi in place exactly as the C does
// (gi is non-const there). (encodeSideInfo2, bitstream.c:320.)
func (gfc *LameInternalFlags) encodeSideInfo2(bitsPerFrame int) {
	cfg := &gfc.Cfg
	eov := &gfc.OvEnc
	esv := &gfc.SvEnc
	l3Side := &gfc.L3Side

	esv.Header[esv.HPtr].Ptr = 0
	// memset(header[h_ptr].buf, 0, sideinfo_len)
	for i := 0; i < cfg.SideinfoLen; i++ {
		esv.Header[esv.HPtr].Buf[i] = 0
	}

	if cfg.SamplerateOut < 16000 {
		gfc.writeheader(0xffe, 12)
	} else {
		gfc.writeheader(0xfff, 12)
	}
	gfc.writeheader(cfg.Version, 1)
	gfc.writeheader(4-3, 2)
	gfc.writeheader(boolToInt(cfg.ErrorProtection == 0), 1)
	gfc.writeheader(eov.BitrateIndex, 4)
	gfc.writeheader(cfg.SamplerateIndex, 2)
	gfc.writeheader(eov.Padding, 1)
	gfc.writeheader(cfg.Extension, 1)
	gfc.writeheader(cfg.Mode, 2)
	gfc.writeheader(eov.ModeExt, 2)
	gfc.writeheader(cfg.Copyright, 1)
	gfc.writeheader(cfg.Original, 1)
	gfc.writeheader(cfg.Emphasis, 2)
	if cfg.ErrorProtection != 0 {
		gfc.writeheader(0, 16) // dummy
	}

	if cfg.Version == 1 {
		// MPEG1
		gfc.writeheader(l3Side.MainDataBegin, 9)

		if cfg.ChannelsOut == 2 {
			gfc.writeheader(l3Side.PrivateBits, 3)
		} else {
			gfc.writeheader(l3Side.PrivateBits, 5)
		}

		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			for band := 0; band < 4; band++ {
				gfc.writeheader(l3Side.Scfsi[ch][band], 1)
			}
		}

		for gr := 0; gr < 2; gr++ {
			for ch := 0; ch < cfg.ChannelsOut; ch++ {
				gi := &l3Side.Tt[gr][ch]
				gfc.writeheader(gi.Part23Length+gi.Part2Length, 12)
				gfc.writeheader(gi.BigValues/2, 9)
				gfc.writeheader(gi.GlobalGain, 8)
				gfc.writeheader(gi.ScalefacCompress, 4)

				if gi.BlockType != NormType {
					gfc.writeheader(1, 1) // window_switching_flag
					gfc.writeheader(gi.BlockType, 2)
					gfc.writeheader(gi.MixedBlockFlag, 1)

					if gi.TableSelect[0] == 14 {
						gi.TableSelect[0] = 16
					}
					gfc.writeheader(gi.TableSelect[0], 5)
					if gi.TableSelect[1] == 14 {
						gi.TableSelect[1] = 16
					}
					gfc.writeheader(gi.TableSelect[1], 5)

					gfc.writeheader(gi.SubblockGain[0], 3)
					gfc.writeheader(gi.SubblockGain[1], 3)
					gfc.writeheader(gi.SubblockGain[2], 3)
				} else {
					gfc.writeheader(0, 1) // window_switching_flag
					if gi.TableSelect[0] == 14 {
						gi.TableSelect[0] = 16
					}
					gfc.writeheader(gi.TableSelect[0], 5)
					if gi.TableSelect[1] == 14 {
						gi.TableSelect[1] = 16
					}
					gfc.writeheader(gi.TableSelect[1], 5)
					if gi.TableSelect[2] == 14 {
						gi.TableSelect[2] = 16
					}
					gfc.writeheader(gi.TableSelect[2], 5)

					gfc.writeheader(gi.Region0Count, 4)
					gfc.writeheader(gi.Region1Count, 3)
				}
				gfc.writeheader(gi.Preflag, 1)
				gfc.writeheader(gi.ScalefacScale, 1)
				gfc.writeheader(gi.Count1tableSelect, 1)
			}
		}
	} else {
		// MPEG2
		gfc.writeheader(l3Side.MainDataBegin, 8)
		gfc.writeheader(l3Side.PrivateBits, cfg.ChannelsOut)

		gr := 0
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			gi := &l3Side.Tt[gr][ch]
			gfc.writeheader(gi.Part23Length+gi.Part2Length, 12)
			gfc.writeheader(gi.BigValues/2, 9)
			gfc.writeheader(gi.GlobalGain, 8)
			gfc.writeheader(gi.ScalefacCompress, 9)

			if gi.BlockType != NormType {
				gfc.writeheader(1, 1) // window_switching_flag
				gfc.writeheader(gi.BlockType, 2)
				gfc.writeheader(gi.MixedBlockFlag, 1)

				if gi.TableSelect[0] == 14 {
					gi.TableSelect[0] = 16
				}
				gfc.writeheader(gi.TableSelect[0], 5)
				if gi.TableSelect[1] == 14 {
					gi.TableSelect[1] = 16
				}
				gfc.writeheader(gi.TableSelect[1], 5)

				gfc.writeheader(gi.SubblockGain[0], 3)
				gfc.writeheader(gi.SubblockGain[1], 3)
				gfc.writeheader(gi.SubblockGain[2], 3)
			} else {
				gfc.writeheader(0, 1) // window_switching_flag
				if gi.TableSelect[0] == 14 {
					gi.TableSelect[0] = 16
				}
				gfc.writeheader(gi.TableSelect[0], 5)
				if gi.TableSelect[1] == 14 {
					gi.TableSelect[1] = 16
				}
				gfc.writeheader(gi.TableSelect[1], 5)
				if gi.TableSelect[2] == 14 {
					gi.TableSelect[2] = 16
				}
				gfc.writeheader(gi.TableSelect[2], 5)

				gfc.writeheader(gi.Region0Count, 4)
				gfc.writeheader(gi.Region1Count, 3)
			}

			gfc.writeheader(gi.ScalefacScale, 1)
			gfc.writeheader(gi.Count1tableSelect, 1)
		}
	}

	if cfg.ErrorProtection != 0 {
		// (jo) error_protection: add crc16 information to header
		gfc.crcWriteheader(gfc.SvEnc.Header[esv.HPtr].Buf[:])
	}

	// scope: schedule the slot's write timing and advance h_ptr.
	old := esv.HPtr
	esv.HPtr = (old + 1) & (MaxHeaderBuf - 1)
	esv.Header[esv.HPtr].WriteTiming = esv.Header[old].WriteTiming + bitsPerFrame
	// C ERRORFs on h_ptr == w_ptr (header buffer overflow); a debug-only
	// diagnostic with no effect on the emitted bytes, so the port omits it.
}

// writeMainData emits the granule scalefactors and Huffman-coded coefficients
// for every (gr, ch) of the frame and returns the total bits written
// (writeMainData, bitstream.c:685). It is the main_data() payload that follows
// the side info encodeSideInfo2 buffered.
//
// The MPEG-1 branch writes each granule's scalefactors with the slen1/slen2
// widths from the scalefac_compress tables, skipping bands flagged -1 (scfsi
// reuse); the MPEG-2 (LSF) branch partitions the scalefactors via
// sfb_partition_table / slen[]. Both then emit Short/LongHuffmancodebits + the
// count1 region. The C debug asserts comparing data_bits against the quantizer's
// part2_3_length / part2_length are debug-only and omitted. (writeMainData,
// bitstream.c:685.)
func (gfc *LameInternalFlags) writeMainData() int {
	cfg := &gfc.Cfg
	l3Side := &gfc.L3Side
	totBits := 0

	if cfg.Version == 1 {
		// MPEG 1
		for gr := 0; gr < 2; gr++ {
			for ch := 0; ch < cfg.ChannelsOut; ch++ {
				gi := &l3Side.Tt[gr][ch]
				slen1 := slen1Tab[gi.ScalefacCompress]
				slen2 := slen2Tab[gi.ScalefacCompress]
				dataBits := 0

				var sfb int
				for sfb = 0; sfb < gi.Sfbdivide; sfb++ {
					if gi.Scalefac[sfb] == -1 {
						continue // scfsi is used
					}
					gfc.PutBits2(gi.Scalefac[sfb], slen1)
					dataBits += slen1
				}
				for ; sfb < gi.Sfbmax; sfb++ {
					if gi.Scalefac[sfb] == -1 {
						continue // scfsi is used
					}
					gfc.PutBits2(gi.Scalefac[sfb], slen2)
					dataBits += slen2
				}

				if gi.BlockType == ShortType {
					dataBits += gfc.shortHuffmancodebits(gi)
				} else {
					dataBits += gfc.longHuffmancodebits(gi)
				}
				dataBits += gfc.huffmanCoderCount1(gi)
				totBits += dataBits
			} // for ch
		} // for gr
	} else {
		// MPEG 2
		gr := 0
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			gi := &l3Side.Tt[gr][ch]
			scaleBits := 0
			dataBits := 0

			sfb := 0
			sfbPartition := 0

			if gi.BlockType == ShortType {
				for ; sfbPartition < 4; sfbPartition++ {
					sfbs := gi.SfbPartitionTable[sfbPartition] / 3
					slen := gi.Slen[sfbPartition]
					for i := 0; i < sfbs; i, sfb = i+1, sfb+1 {
						gfc.PutBits2(maxInt(gi.Scalefac[sfb*3+0], 0), slen)
						gfc.PutBits2(maxInt(gi.Scalefac[sfb*3+1], 0), slen)
						gfc.PutBits2(maxInt(gi.Scalefac[sfb*3+2], 0), slen)
						scaleBits += 3 * slen
					}
				}
				dataBits += gfc.shortHuffmancodebits(gi)
			} else {
				for ; sfbPartition < 4; sfbPartition++ {
					sfbs := gi.SfbPartitionTable[sfbPartition]
					slen := gi.Slen[sfbPartition]
					for i := 0; i < sfbs; i, sfb = i+1, sfb+1 {
						gfc.PutBits2(maxInt(gi.Scalefac[sfb], 0), slen)
						scaleBits += slen
					}
				}
				dataBits += gfc.longHuffmancodebits(gi)
			}
			dataBits += gfc.huffmanCoderCount1(gi)
			totBits += scaleBits + dataBits
		} // for ch
	} // for gr
	return totBits
}

// computeFlushbits returns the number of bits that must be added to flush all
// mp3 frames currently buffered (equal to the reservoir size) and, via
// totalBytesOutput, the byte size of the mp3 buffer if the stream were flushed
// now (compute_flushbits, bitstream.c:801).
//
// The C ERRORFs on flushbits < 0 ("strange error flushing buffer"); a
// diagnostic the port omits (it changes no returned value).
func (gfc *LameInternalFlags) computeFlushbits(totalBytesOutput *int) int {
	cfg := &gfc.Cfg
	esv := &gfc.SvEnc

	firstPtr := esv.WPtr // first header to add to bitstream
	lastPtr := esv.HPtr - 1
	if lastPtr == -1 {
		lastPtr = MaxHeaderBuf - 1
	}

	// add this many bits to bitstream so we can flush all headers
	flushbits := esv.Header[lastPtr].WriteTiming - gfc.Bs.Totbit
	*totalBytesOutput = flushbits

	if flushbits >= 0 {
		// some headers have not yet been written; reduce by their size
		remainingHeaders := 1 + lastPtr - firstPtr
		if lastPtr < firstPtr {
			remainingHeaders = 1 + lastPtr - firstPtr + MaxHeaderBuf
		}
		flushbits -= remainingHeaders * 8 * cfg.SideinfoLen
	}

	// add bits so the last frame is complete (some decoders drop it otherwise)
	bitsPerFrame := gfc.getframebits()
	flushbits += bitsPerFrame
	*totalBytesOutput += bitsPerFrame
	// round up
	if *totalBytesOutput%8 != 0 {
		*totalBytesOutput = 1 + (*totalBytesOutput / 8)
	} else {
		*totalBytesOutput = *totalBytesOutput / 8
	}
	*totalBytesOutput += gfc.Bs.BufByteIdx + 1

	return flushbits
}

// flushBitstream pads every buffered frame out with ancillary data so the whole
// reservoir is flushed, then resets the reservoir state (flush_bitstream,
// bitstream.c:862).
//
//	void
//	flush_bitstream(lame_internal_flags * gfc)
//	{
//	    ...
//	    if ((flushbits = compute_flushbits(gfc, &nbytes)) < 0) return;
//	    drain_into_ancillary(gfc, flushbits);
//	    esv->ResvSize = 0;
//	    l3_side->main_data_begin = 0;
//	}
func (gfc *LameInternalFlags) flushBitstream() {
	esv := &gfc.SvEnc
	l3Side := &gfc.L3Side

	var nbytes int
	flushbits := gfc.computeFlushbits(&nbytes)
	if flushbits < 0 {
		return
	}
	gfc.drainIntoAncillary(flushbits)

	// the C asserts header[last_ptr].write_timing + getframebits == bs.totbit;
	// a debug-only invariant the port relies on the caller contract for.

	// we have padded out all frames with ancillary data (== filling the
	// bitreservoir with ancillary data), so:
	esv.ResvSize = 0
	l3Side.MainDataBegin = 0
}

// addDummyByte writes n copies of val into the stream without triggering header
// splices, shifting every buffered header's write timing forward by the bits
// emitted (add_dummy_byte, bitstream.c:892).
//
//	void
//	add_dummy_byte(lame_internal_flags * gfc, unsigned char val, unsigned int n)
//	{
//	    EncStateVar_t *const esv = &gfc->sv_enc;
//	    int     i;
//	    while (n-- > 0u) {
//	        putbits_noheaders(gfc, val, 8);
//	        for (i = 0; i < MAX_HEADER_BUF; ++i)
//	            esv->header[i].write_timing += 8;
//	    }
//	}
func (gfc *LameInternalFlags) addDummyByte(val byte, n uint) {
	esv := &gfc.SvEnc
	for ; n > 0; n-- {
		gfc.PutBitsNoHeaders(int(val), 8)
		for i := 0; i < MaxHeaderBuf; i++ {
			esv.Header[i].WriteTiming += 8
		}
	}
}

// doCopyBuffer drains the internal bit buffer into buffer and resets the writer,
// returning the byte count written (or -1 if buffer is too small)
// (do_copy_buffer, bitstream.c:1055).
//
//	static int
//	do_copy_buffer(lame_internal_flags * gfc, unsigned char *buffer, int size)
//	{
//	    Bit_stream_struc *const bs = &gfc->bs;
//	    int const minimum = bs->buf_byte_idx + 1;
//	    if (minimum <= 0) return 0;
//	    if (minimum > size) return -1;
//	    memcpy(buffer, bs->buf, minimum);
//	    bs->buf_byte_idx = -1;
//	    bs->buf_bit_idx = 0;
//	    return minimum;
//	}
func (gfc *LameInternalFlags) doCopyBuffer(buffer []byte, size int) int {
	bs := &gfc.Bs
	minimum := bs.BufByteIdx + 1
	if minimum <= 0 {
		return 0
	}
	if minimum > size {
		return -1 // buffer is too small
	}
	copy(buffer[:minimum], bs.Buf[:minimum])
	bs.BufByteIdx = -1
	bs.BufBitIdx = 0
	return minimum
}

// copyBuffer copies the internal mp3 bit buffer into the user buffer, returning
// the byte count (copy_buffer, bitstream.c:1078). mp3data==0 marks the bytes as
// id3/VBR tag data, mp3data==1 as real mp3 frame data.
//
//	int
//	copy_buffer(lame_internal_flags * gfc, unsigned char *buffer, int size, int mp3data)
//	{
//	    int const minimum = do_copy_buffer(gfc, buffer, size);
//	    if (minimum > 0 && mp3data) {
//	        UpdateMusicCRC(&gfc->nMusicCRC, buffer, minimum);
//	        gfc->VBR_seek_table.nBytesWritten += minimum;
//	        return do_gain_analysis(gfc, buffer, minimum);
//	    }
//	    return minimum;
//	}
//
// The mp3data side effects are owned by other translation units the bitstream
// slice does not port: UpdateMusicCRC + VBR_seek_table.nBytesWritten belong to
// VbrTag.c (the Xing/LAME-header seek table), and do_gain_analysis to
// gain_analysis.c — and the latter compiles to `return minimum;` unless
// DECODE_ON_THE_FLY is defined (it is NOT in the vendored config.h). The unified
// context does not yet carry nMusicCRC / VBR_seek_table; until the VbrTag slice
// adds them, the port performs the bit-exact buffer drain (the bitstream.c part)
// and leaves the music-CRC / seek-table accumulation to that slice. The returned
// byte count is identical (do_gain_analysis returns its `minimum` argument).
func (gfc *LameInternalFlags) copyBuffer(buffer []byte, size, mp3data int) int {
	minimum := gfc.doCopyBuffer(buffer, size)
	if minimum > 0 && mp3data != 0 {
		// UpdateMusicCRC(&gfc->nMusicCRC, buffer, minimum) and
		// gfc->VBR_seek_table.nBytesWritten += minimum: the Xing/LAME tag's
		// running music CRC and audio byte count (VbrTag.c, now ported in
		// vbrtag.go). lame_get_lametag_frame embeds both in the final tag frame.
		UpdateMusicCRC(&gfc.NMusicCRC, buffer, minimum)
		gfc.VBRSeekTable.NBytesWritten += uint64(minimum)
		// do_gain_analysis(gfc, buffer, minimum) reduces to `return minimum`
		// (DECODE_ON_THE_FLY undefined in config.h).
		return minimum
	}
	return minimum
}

// formatBitstream assembles one frame: it drains the pre-reservoir stuffing,
// emits the side info and main data, drains the post-reservoir stuffing, then
// updates the main_data_begin back-pointer for the next frame (format_bitstream,
// bitstream.c:917). It always returns 0.
//
//	int
//	format_bitstream(lame_internal_flags * gfc)
//	{
//	    ...
//	    bitsPerFrame = getframebits(gfc);
//	    drain_into_ancillary(gfc, l3_side->resvDrain_pre);
//	    encodeSideInfo2(gfc, bitsPerFrame);
//	    bits = 8 * cfg->sideinfo_len;
//	    bits += writeMainData(gfc);
//	    drain_into_ancillary(gfc, l3_side->resvDrain_post);
//	    bits += l3_side->resvDrain_post;
//	    l3_side->main_data_begin += (bitsPerFrame - bits) / 8;
//	    ... (two ERRORF consistency checks) ...
//	    if (gfc->bs.totbit > 1000000000) { ... reset totbit + write_timings ... }
//	    return 0;
//	}
//
// The two ERRORF blocks (compute_flushbits vs ResvSize, and main_data_begin*8 vs
// ResvSize) are diagnostics that emit no bytes; the second one's recovery
// (esv->ResvSize = main_data_begin*8) only runs on the error path, so the port
// omits both. The totbit-overflow rebase (every ~8h at 128kbps) is kept 1:1.
func (gfc *LameInternalFlags) formatBitstream() int {
	cfg := &gfc.Cfg
	esv := &gfc.SvEnc
	l3Side := &gfc.L3Side

	bitsPerFrame := gfc.getframebits()
	gfc.drainIntoAncillary(l3Side.ResvDrainPre)

	gfc.encodeSideInfo2(bitsPerFrame)
	bits := 8 * cfg.SideinfoLen
	bits += gfc.writeMainData()
	gfc.drainIntoAncillary(l3Side.ResvDrainPost)
	bits += l3Side.ResvDrainPost

	l3Side.MainDataBegin += (bitsPerFrame - bits) / 8

	if gfc.Bs.Totbit > 1000000000 {
		// to avoid totbit overflow (at 8h encoding at 128kbs), reset the counter
		for i := 0; i < MaxHeaderBuf; i++ {
			esv.Header[i].WriteTiming -= gfc.Bs.Totbit
		}
		gfc.Bs.Totbit = 0
	}

	return 0
}

// boolToInt returns 1 for true and 0 for false, mirroring how C evaluates the
// boolean expressions (!cfg->error_protection) that encodeSideInfo2 feeds to
// writeheader as 0/1 ints.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
