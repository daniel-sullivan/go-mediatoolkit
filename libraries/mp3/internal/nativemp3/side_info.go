package nativemp3

// Layer III side-information parser. L3_read_side_info reads the per-granule,
// per-channel control fields (part2_3 length, big_values, global_gain,
// scalefactor selection, block type, Huffman table selection, region counts,
// …) that follow the frame header and tell the granule decoder how to
// interpret the main data. It is pure integer bitstream work, so it is
// bit-identical regardless of build tag.

// shortBlockType is SHORT_BLOCK_TYPE (minimp3.h:59): the block_type value for
// a windowed short block.
const shortBlockType = 2

// L3GrInfo (the per-granule side-information struct, L3_gr_info_t) is declared
// in grinfo.go; L3ReadSideInfo below populates it.

// L3ReadSideInfo parses the side information for every granule/channel of the
// frame into gr and returns main_data_begin — the bit-reservoir back-pointer
// (the number of bytes before this frame's payload that its main data reaches
// into) — together with ok=true. On a malformed side info it returns
// (0, false), standing in for the vendored minimp3's "return -1" error path
// (L3_read_side_info, minimp3.h:484). NOTE: this vendored minimp3 returns the
// integer main_data_begin value (minimp3.h:606), which L3_restore_reservoir
// consumes; an earlier minimp3 variant instead returned bs->buf + bs->pos/8 and
// reassembled main data by pointer — that is NOT this reference.
//
//	static int L3_read_side_info(bs_t *bs, L3_gr_info_t *gr, const uint8_t *hdr)
//	{
//	    static const uint8_t g_scf_long[8][23] = { ... };
//	    static const uint8_t g_scf_short[8][40] = { ... };
//	    static const uint8_t g_scf_mixed[8][40] = { ... };
//
//	    unsigned tables, scfsi = 0;
//	    int main_data_begin, part_23_sum = 0;
//	    int gr_count = HDR_IS_MONO(hdr) ? 1 : 2;
//	    int sr_idx = HDR_GET_MY_SAMPLE_RATE(hdr); sr_idx -= (sr_idx != 0);
//
//	    if (HDR_TEST_MPEG1(hdr))
//	    {
//	        gr_count *= 2;
//	        main_data_begin = get_bits(bs, 9);
//	        scfsi = get_bits(bs, 7 + gr_count);
//	    } else
//	    {
//	        main_data_begin = get_bits(bs, 8 + gr_count) >> gr_count;
//	    }
//
//	    do
//	    {
//	        if (HDR_IS_MONO(hdr))
//	        {
//	            scfsi <<= 4;
//	        }
//	        gr->part_23_length = (uint16_t)get_bits(bs, 12);
//	        part_23_sum += gr->part_23_length;
//	        gr->big_values = (uint16_t)get_bits(bs, 9);
//	        if (gr->big_values > 288)
//	        {
//	            return -1;
//	        }
//	        gr->global_gain = (uint8_t)get_bits(bs, 8);
//	        gr->scalefac_compress = (uint16_t)get_bits(bs, HDR_TEST_MPEG1(hdr) ? 4 : 9);
//	        gr->sfbtab = g_scf_long[sr_idx];
//	        gr->n_long_sfb  = 22;
//	        gr->n_short_sfb = 0;
//	        if (get_bits(bs, 1))
//	        {
//	            gr->block_type = (uint8_t)get_bits(bs, 2);
//	            if (!gr->block_type)
//	            {
//	                return -1;
//	            }
//	            gr->mixed_block_flag = (uint8_t)get_bits(bs, 1);
//	            gr->region_count[0] = 7;
//	            gr->region_count[1] = 255;
//	            if (gr->block_type == SHORT_BLOCK_TYPE)
//	            {
//	                scfsi &= 0x0F0F;
//	                if (!gr->mixed_block_flag)
//	                {
//	                    gr->region_count[0] = 8;
//	                    gr->sfbtab = g_scf_short[sr_idx];
//	                    gr->n_long_sfb = 0;
//	                    gr->n_short_sfb = 39;
//	                } else
//	                {
//	                    gr->sfbtab = g_scf_mixed[sr_idx];
//	                    gr->n_long_sfb = HDR_TEST_MPEG1(hdr) ? 8 : 6;
//	                    gr->n_short_sfb = 30;
//	                }
//	            }
//	            tables = get_bits(bs, 10);
//	            tables <<= 5;
//	            gr->subblock_gain[0] = (uint8_t)get_bits(bs, 3);
//	            gr->subblock_gain[1] = (uint8_t)get_bits(bs, 3);
//	            gr->subblock_gain[2] = (uint8_t)get_bits(bs, 3);
//	        } else
//	        {
//	            gr->block_type = 0;
//	            gr->mixed_block_flag = 0;
//	            tables = get_bits(bs, 15);
//	            gr->region_count[0] = (uint8_t)get_bits(bs, 4);
//	            gr->region_count[1] = (uint8_t)get_bits(bs, 3);
//	            gr->region_count[2] = 255;
//	        }
//	        gr->table_select[0] = (uint8_t)(tables >> 10);
//	        gr->table_select[1] = (uint8_t)((tables >> 5) & 31);
//	        gr->table_select[2] = (uint8_t)((tables) & 31);
//	        gr->preflag = HDR_TEST_MPEG1(hdr) ? get_bits(bs, 1) : (gr->scalefac_compress >= 500);
//	        gr->scalefac_scale = (uint8_t)get_bits(bs, 1);
//	        gr->count1_table = (uint8_t)get_bits(bs, 1);
//	        gr->scfsi = (uint8_t)((scfsi >> 12) & 15);
//	        scfsi <<= 4;
//	        gr++;
//	    } while (--gr_count);
//
//	    if (part_23_sum + bs->pos > bs->limit + main_data_begin*8)
//	    {
//	        return -1;
//	    }
//
//	    return main_data_begin;
//	}
//
// gr must have room for the granules being read (up to 4: 2 granules × 2
// channels for MPEG-1 stereo). The C "gr++" pointer walk is expressed here by
// indexing gr[gi] as gi advances. The C "sfbtab = g_scf_long[sr_idx]" row
// pointer becomes a slice of the shared table row.
func L3ReadSideInfo(bs *BitStream, gr []L3GrInfo, hdr []byte) (mainData int, ok bool) {
	var tables uint32
	var scfsi uint32 = 0
	var mainDataBegin int
	part23Sum := 0
	grCount := 2
	if hdrIsMono(hdr) {
		grCount = 1
	}
	srIdx := hdrGetMySampleRate(hdr)
	if srIdx != 0 {
		srIdx--
	}

	if hdrTestMPEG1(hdr) != 0 {
		grCount *= 2
		mainDataBegin = int(GetBits(bs, 9))
		scfsi = GetBits(bs, 7+grCount)
	} else {
		mainDataBegin = int(GetBits(bs, 8+grCount) >> uint(grCount))
	}

	gi := 0
	for {
		g := &gr[gi]
		if hdrIsMono(hdr) {
			scfsi <<= 4
		}
		g.Part23Length = uint16(GetBits(bs, 12))
		part23Sum += int(g.Part23Length)
		g.BigValues = uint16(GetBits(bs, 9))
		if g.BigValues > 288 {
			return 0, false
		}
		g.GlobalGain = uint8(GetBits(bs, 8))
		if hdrTestMPEG1(hdr) != 0 {
			g.ScalefacCompress = uint16(GetBits(bs, 4))
		} else {
			g.ScalefacCompress = uint16(GetBits(bs, 9))
		}
		g.Sfbtab = gScfLong[srIdx][:]
		g.NLongSfb = 22
		g.NShortSfb = 0
		if GetBits(bs, 1) != 0 {
			g.BlockType = uint8(GetBits(bs, 2))
			if g.BlockType == 0 {
				return 0, false
			}
			g.MixedBlockFlag = uint8(GetBits(bs, 1))
			g.RegionCount[0] = 7
			g.RegionCount[1] = 255
			if g.BlockType == shortBlockType {
				scfsi &= 0x0F0F
				if g.MixedBlockFlag == 0 {
					g.RegionCount[0] = 8
					g.Sfbtab = gScfShort[srIdx][:]
					g.NLongSfb = 0
					g.NShortSfb = 39
				} else {
					g.Sfbtab = gScfMixed[srIdx][:]
					if hdrTestMPEG1(hdr) != 0 {
						g.NLongSfb = 8
					} else {
						g.NLongSfb = 6
					}
					g.NShortSfb = 30
				}
			}
			tables = GetBits(bs, 10)
			tables <<= 5
			g.SubblockGain[0] = uint8(GetBits(bs, 3))
			g.SubblockGain[1] = uint8(GetBits(bs, 3))
			g.SubblockGain[2] = uint8(GetBits(bs, 3))
		} else {
			g.BlockType = 0
			g.MixedBlockFlag = 0
			tables = GetBits(bs, 15)
			g.RegionCount[0] = uint8(GetBits(bs, 4))
			g.RegionCount[1] = uint8(GetBits(bs, 3))
			g.RegionCount[2] = 255
		}
		g.TableSelect[0] = uint8(tables >> 10)
		g.TableSelect[1] = uint8((tables >> 5) & 31)
		g.TableSelect[2] = uint8(tables & 31)
		if hdrTestMPEG1(hdr) != 0 {
			g.Preflag = uint8(GetBits(bs, 1))
		} else if g.ScalefacCompress >= 500 {
			g.Preflag = 1
		} else {
			g.Preflag = 0
		}
		g.ScalefacScale = uint8(GetBits(bs, 1))
		g.Count1Table = uint8(GetBits(bs, 1))
		g.Scfsi = uint8((scfsi >> 12) & 15)
		scfsi <<= 4
		gi++
		grCount--
		if grCount == 0 {
			break
		}
	}

	if part23Sum+bs.Pos > bs.Limit+mainDataBegin*8 {
		return 0, false
	}

	return mainDataBegin, true
}
