package nativemp3

// L3GrInfo is a 1:1 translation of minimp3's per-granule side-information
// struct L3_gr_info_t (minimp3.h:209).
//
//	typedef struct
//	{
//	    const uint8_t *sfbtab;
//	    uint16_t part_23_length, big_values, scalefac_compress;
//	    uint8_t global_gain, block_type, mixed_block_flag, n_long_sfb, n_short_sfb;
//	    uint8_t table_select[3], region_count[3], subblock_gain[3];
//	    uint8_t preflag, scalefac_scale, count1_table, scfsi;
//	} L3_gr_info_t;
//
// This struct is shared across the Layer III decode slices (side-info,
// scalefactor decode, Huffman, requantization, IMDCT). It is declared here,
// alongside the first slice to consume it (Huffman unpacking, which reads
// Sfbtab, BigValues, TableSelect, RegionCount and Count1Table); the slices
// that populate it — L3_read_side_info and L3_decode_scalefactors — fill in
// the remaining fields. Field names mirror the C members; Sfbtab is the
// scalefactor-band width table the C points into a static const array, so
// it is carried as a byte slice rather than a Go pointer.
type L3GrInfo struct {
	// Sfbtab points at the scalefactor-band width table for this granule
	// (L3_gr_info_t.sfbtab). Huffman consumes one entry per band via the
	// `*sfb++` walk; the slice is positioned at the first band.
	Sfbtab []byte

	// Part23Length is the combined scalefactor + Huffman data length in
	// bits (L3_gr_info_t.part_23_length).
	Part23Length uint16

	// BigValues is the number of big-value (>1) pairs to Huffman-decode
	// (L3_gr_info_t.big_values).
	BigValues uint16

	// ScalefacCompress selects the scalefactor bit-length table
	// (L3_gr_info_t.scalefac_compress).
	ScalefacCompress uint16

	// GlobalGain is the quantizer step-size exponent (L3_gr_info_t.global_gain).
	GlobalGain uint8

	// BlockType is the window/block type: 0 normal, 1 start, 2 short, 3
	// stop (L3_gr_info_t.block_type).
	BlockType uint8

	// MixedBlockFlag marks a mixed long/short block (L3_gr_info_t.mixed_block_flag).
	MixedBlockFlag uint8

	// NLongSfb is the number of long scalefactor bands (L3_gr_info_t.n_long_sfb).
	NLongSfb uint8

	// NShortSfb is the number of short scalefactor bands (L3_gr_info_t.n_short_sfb).
	NShortSfb uint8

	// TableSelect holds the Huffman codebook index for each of the three
	// big-value regions (L3_gr_info_t.table_select[3]).
	TableSelect [3]uint8

	// RegionCount holds the band count for each of the three big-value
	// regions (L3_gr_info_t.region_count[3]).
	RegionCount [3]uint8

	// SubblockGain holds the per-window gain for short blocks
	// (L3_gr_info_t.subblock_gain[3]).
	SubblockGain [3]uint8

	// Preflag enables the high-frequency scalefactor preamplification table
	// (L3_gr_info_t.preflag).
	Preflag uint8

	// ScalefacScale selects the scalefactor scaling step (L3_gr_info_t.scalefac_scale).
	ScalefacScale uint8

	// Count1Table selects the count1 (quadruple) Huffman table: 0 -> tab32,
	// 1 -> tab33 (L3_gr_info_t.count1_table).
	Count1Table uint8

	// Scfsi is the scalefactor selection information bitfield
	// (L3_gr_info_t.scfsi).
	Scfsi uint8
}
