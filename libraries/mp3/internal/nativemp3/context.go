// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Unified LAME encoder context.
//
// This file is the 1:1 Go mirror of LAME 3.100's central encoder context
// struct lame_internal_flags (util.h:463) and the sub-structs it aggregates
// (SessionConfig_t util.h:356, EncStateVar_t util.h:247, EncResult_t
// util.h:304, QntStateVar_t util.h:319, ATH_t util.h:166, PsyConst_t
// util.h:209, PsyStateVar_t util.h:220, PsyResult_t util.h:240, the
// III_side_info_t / gr_info / scalefac_struct of l3side.h). The C reference is
// the vendored tree at libraries/mp3/liblame/libmp3lame/util.h and l3side.h.
//
// # Why this file exists (cross-slice unification)
//
// The encoder slices were ported independently, and each declared its own
// partial mirror of the bits of lame_internal_flags it happened to touch:
//
//   - the huffman-encode slice (bitstream_encode.go / huffman_encode.go) had
//     EncFlags + EncStateVar + EncSessionConfig + GrInfo;
//   - the frame-encode dispatcher (frame_encode.go) had FELameInternalFlags +
//     FEEncStateVar + FEEncResult + FEGrInfo;
//   - the mdct-analysis slice (mdct_analysis_filterbank.go) had MDCTEncStateVar
//     + MDCTGrInfo;
//   - the psychoacoustic model (psymodel.go) had SessionConfig + PsyModel.
//
// Each was documented as "unification is a later slice's job". This file is
// that slice: it defines the single LameInternalFlags context plus the full
// SessionConfig / EncStateVar / EncResult / GrInfo / III_side_info_t mirrors,
// and the other slices' receivers are re-pointed at it. The unification is a
// pure struct-merge + field-routing change — every ported math kernel is left
// byte-for-byte identical. The merged structs are field-supersets of the
// per-slice subsets, so no kernel had to change which member it indexes.
//
// LAME threads a single `lame_internal_flags *gfc` pointer through the entire
// encoder; LameInternalFlags is the Go stand-in for that pointer, and the
// receiver of every method the C invokes on gfc.
//
// # Floating-point type
//
// LAME's `FLOAT` typedef is `float` (32-bit); see machine.h. Every FLOAT field
// below is therefore float32. The per-frame FP arithmetic that reads/writes
// these fields is routed through the //go:noinline helpers in the *_fp_strict.go
// files so the mp3_strict build separately-rounds like the cgo oracle; the
// struct layout itself carries no arithmetic.

// ---------------------------------------------------------------------------
// Bit-writer (bitstream.c) — Bit_stream_struc + the header ring buffer.
// ---------------------------------------------------------------------------

// Bit-writer size constants from util.h.
//
//   - MaxHeaderBuf is the depth of the header ring buffer (util.h:273,
//     #define MAX_HEADER_BUF 256); it must stay a power of two because
//     putbits2 advances w_ptr with `& (MAX_HEADER_BUF - 1)`.
//   - MaxHeaderLen bounds a single buffered frame header (util.h:274,
//     #define MAX_HEADER_LEN 40, "max size of header is 38").
//   - MaxLength is bitstream.c's working bit width cap (bitstream.c:50,
//     #define MAX_LENGTH 32); putbits2 asserts j < MAX_LENGTH-2.
const (
	MaxHeaderBuf = 256
	MaxHeaderLen = 40
	MaxLength    = 32
)

// SFBMAX is l3side.h:25 #define SFBMAX (SBMAX_s*3): the maximum scalefactor-band
// count across long and short blocks, sizing gr_info's per-band arrays.
const SFBMAX = SBMAXs * 3

// HeaderInfo is one slot of LAME's per-frame header ring buffer (the anonymous
// struct in EncStateVar_t, util.h:275).
//
//	struct {
//	    int     write_timing;
//	    int     ptr;
//	    char    buf[MAX_HEADER_LEN];
//	} header[MAX_HEADER_BUF];
//
// WriteTiming is the totbit count at which putbits2 must splice this header's
// bytes into the stream; Ptr is the bit cursor used while the side-info writer
// fills Buf; Buf holds the assembled header + side info bytes.
type HeaderInfo struct {
	WriteTiming int                // header[].write_timing
	Ptr         int                // header[].ptr
	Buf         [MaxHeaderLen]byte // header[].buf (char -> byte)
}

// EncBitStream is LAME's output bit stream (Bit_stream_struc, util.h:136).
//
//	typedef struct bit_stream_struc {
//	    unsigned char *buf;
//	    int     buf_size;
//	    int     totbit;
//	    int     buf_byte_idx;
//	    int     buf_bit_idx;
//	} Bit_stream_struc;
//
// Buf is the output byte buffer, Totbit the running bit count, BufByteIdx the
// index of the byte currently being filled, and BufBitIdx the number of free
// bits remaining in that byte (it counts DOWN from 8, MSB-first). LAME's C
// asserts buf_byte_idx < BUFFER_SIZE; the Go port leaves Buf sized by the
// caller and relies on those bounds, exactly as the C buffer is pre-sized.
type EncBitStream struct {
	Buf        []byte // bs->buf
	BufSize    int    // bs->buf_size
	Totbit     int    // bs->totbit
	BufByteIdx int    // bs->buf_byte_idx
	BufBitIdx  int    // bs->buf_bit_idx
}

// ---------------------------------------------------------------------------
// EncStateVar_t (util.h:247) — the full encoder state-var struct, merging the
// header ring buffer (bitstream slice), the subband-sample / amp_filter
// carry (mdct slice) and the padding accumulator / PE FIR history (frame
// dispatcher). Only the members the ported slices touch are mapped; the
// resampler / mfbuf / VBR-tag members the C carries are out of the ported
// slices' scope and intentionally omitted (they belong to later slices).
// ---------------------------------------------------------------------------

// EncStateVar is LAME's EncStateVar_t (util.h:247).
type EncStateVar struct {
	// SbSample is the polyphase subband-sample history,
	// sb_sample[ch][gr][18][SBLIMIT] (util.h:249). window_subband writes the
	// current granule's frames and mdct_sub48 reads two granules' worth.
	SbSample [2][2][18][SBLIMIT]float32

	// AmpFilter is the per-band amplitude scaling (sv_enc.amp_filter[32],
	// util.h:250); a band below 1e-12 is zeroed, below 1.0 is attenuated.
	AmpFilter [SBLIMIT]float32

	// PefirBuf is the 19-tap perceptual-entropy history the CBR/ABR smoothing
	// FIR convolves (sv_enc.pefirbuf[19], util.h:259).
	PefirBuf [19]float32

	// FracSpF is the fractional samples-per-frame the padding accumulator
	// subtracts each frame (sv_enc.frac_SpF, util.h:262).
	FracSpF int

	// SlotLag is the running padding accumulator; when it goes negative the
	// frame is padded and SamplerateOut is added back (sv_enc.slot_lag,
	// util.h:263).
	SlotLag int

	// Header is the per-frame header ring buffer (sv_enc.header[MAX_HEADER_BUF],
	// util.h:275). putheader_bits drains slot w_ptr into the bit stream; the
	// side info writer (other slice) fills slots and advances h_ptr.
	Header [MaxHeaderBuf]HeaderInfo

	// HPtr / WPtr are the header ring's write / read cursors (sv_enc.h_ptr /
	// w_ptr, util.h:281/282).
	HPtr int
	WPtr int

	// AncillaryFlag is sv_enc.ancillary_flag (util.h:283).
	AncillaryFlag int

	// ResvSize / ResvMax are the bit reservoir occupancy / capacity in bits
	// (sv_enc.ResvSize / ResvMax, util.h:286/287).
	ResvSize int
	ResvMax  int

	// Mfbuf is the per-channel mf (main-flow) sample ring the PCM driver chops
	// into 576*mode_gr-sample frames (sv_enc.mfbuf[2][MFSIZE], util.h:291). The C
	// statically sizes it MFSIZE; the port slices it to MFSIZE up front so
	// fill_buffer's memcpy and the frame shift index it identically.
	Mfbuf [2][MFSIZE]float32

	// MfSize is the number of valid samples currently buffered in Mfbuf
	// (sv_enc.mf_size, util.h:299).
	MfSize int

	// MfSamplesToEncode counts the input + delay/padding samples still owed to
	// the encoder, draining one frame at a time so the flush knows when the real
	// audio is exhausted (sv_enc.mf_samples_to_encode, util.h:300).
	MfSamplesToEncode int
}

// MFSIZE is util.h:294 (#define MFSIZE (3*1152 + ENCDELAY - MDCTDELAY)): the
// depth of the mf sample ring (3*1152 + 576 - 48 = 3984).
const MFSIZE = 3*1152 + ENCDELAY - MDCTDELAY

// ENCDELAY / POSTDELAY are encoder.h:57/72 (#define ENCDELAY 576, #define
// POSTDELAY 1152): the internal encoder look-ahead delay and the 50%-MDCT-overlap
// post-roll the PCM driver pads the stream with.
const (
	ENCDELAY  = 576
	POSTDELAY = 1152
)

// ---------------------------------------------------------------------------
// EncResult_t (util.h:304) — per-frame encoder result.
// ---------------------------------------------------------------------------

// EncResult is LAME's EncResult_t (util.h:304); only the members the ported
// slices touch are mapped (the bitrate/blocktype histograms are filled by the
// not-yet-ported updateStats slice).
type EncResult struct {
	// BitrateIndex is the chosen bitrate-table index for the frame
	// (ov_enc.bitrate_index, util.h:309); lame_init_params seeds it.
	BitrateIndex int

	// FrameNumber counts frames encoded so far (ov_enc.frame_number,
	// util.h:310); the dispatcher increments it after each frame.
	FrameNumber int

	// Padding is TRUE when the current frame carries a padding slot
	// (ov_enc.padding, util.h:311).
	Padding int

	// ModeExt is the chosen stereo coding for the frame, one of the MPG_MD_*
	// values (ov_enc.mode_ext, util.h:312).
	ModeExt int

	// EncoderDelay / EncoderPadding mirror ov_enc.encoder_delay /
	// encoder_padding (util.h:313/314).
	EncoderDelay   int
	EncoderPadding int
}

// ---------------------------------------------------------------------------
// VBR_seek_info_t (util.h:148) — Xing/LAME VBR seek-table bookkeeping.
// ---------------------------------------------------------------------------

// VbrSeekInfo is LAME's VBR_seek_info_t (util.h:148): the running bag of
// per-frame bitrates Xing_seek_table reduces into the 100-entry TOC, plus the
// frame count, byte count and the embedded-tag frame size. InitVbrTag seeds it,
// AddVbrFrame appends to it (via addVbr) and lame_get_lametag_frame reads it.
type VbrSeekInfo struct {
	Sum  int   // sum: what we have seen so far
	Seen int   // seen: how many frames we have seen in this chunk
	Want int   // want: how many frames we want to collect into one chunk
	Pos  int   // pos: actual position in our bag
	Size int   // size: size of our bag
	Bag  []int // bag: pointer to our bag

	NVbrNumFrames uint   // nVbrNumFrames
	NBytesWritten uint64 // nBytesWritten (C unsigned long)

	TotalFrameSize uint // TotalFrameSize: the embedded Xing/LAME header frame size
}

// ---------------------------------------------------------------------------
// RpgResult_t (util.h) — ReplayGain analysis result (gfc->ov_rpg).
// ---------------------------------------------------------------------------

// RpgResult is the subset of LAME's RpgResult_t the VbrTag.c port reads
// (gfc->ov_rpg.RadioGain / PeakSample). PutLameVBR only consults these when
// cfg.FindReplayGain / cfg.FindPeakSample are set; the public encoder leaves
// both off, so for a -V2 stream they stay zero.
type RpgResult struct {
	RadioGain  int     // ov_rpg.RadioGain
	PeakSample float32 // ov_rpg.PeakSample (C FLOAT)
}

// ---------------------------------------------------------------------------
// QntStateVar_t (util.h:319) — quantizer state. Only the members
// lame_init_params populates are mapped here; the per-band scratch arrays
// belong to the quantize.c slice.
// ---------------------------------------------------------------------------

// QntStateVar is the subset of LAME's QntStateVar_t (util.h:319) that
// lame_init_params writes.
type QntStateVar struct {
	// Longfact / Shortfact are the nspsytune per-scalefactor-band masking
	// adjustment factors (sv_qnt.longfact[SBMAX_l] / shortfact[SBMAX_s],
	// util.h:321/322); iteration_init fills them from the bass/alto/treble/
	// sfb21 dB adjustments and calc_xmin multiplies them into the per-band xmin.
	Longfact  [SBMAXl]float32
	Shortfact [SBMAXs]float32

	// MaskingLower mirrors sv_qnt.masking_lower (util.h:323), the quantizer
	// masking the psymodel multiplies into per-band masking.
	MaskingLower float32

	// OldValue / CurrentStep are bin_search_StepSize's per-channel carry of the
	// last granule's global_gain and binary-search step (sv_qnt.OldValue[2] /
	// CurrentStep[2], util.h:326/327).
	OldValue    [2]int
	CurrentStep [2]int

	// Pseudohalf is the per-scalefactor-band substep-shaping flag
	// (sv_qnt.pseudohalf[SFBMAX], util.h:328); init_xrpow seeds it,
	// amp_scalefac_bands toggles it and count_bits reads it.
	Pseudohalf [SFBMAX]int

	// MaskAdjust / MaskAdjustShort are the dbQ adjustments (sv_qnt.mask_adjust /
	// mask_adjust_short, util.h:324/325).
	MaskAdjust      float32
	MaskAdjustShort float32

	// Sfb21Extra enables the extra sfb21 scalefactor band (sv_qnt.sfb21_extra,
	// util.h:329); set by lame_init_params per VBR mode and samplerate.
	Sfb21Extra int

	// SubstepShaping mirrors sv_qnt.substep_shaping (util.h:330).
	SubstepShaping int

	// BvScf is the precomputed big-value region0/region1 split table
	// (sv_qnt.bv_scf[576], util.h:338, C `char`). huffman_init (takehiro.go)
	// fills it; noquant_count_bits reads bv_scf[i-2] / bv_scf[i-1] to seed a
	// long block's region0_count / region1_count. The C element type is `char`;
	// the stored region counts are small and non-negative, so int8 mirrors it.
	BvScf [576]int8
}

// ---------------------------------------------------------------------------
// scalefac_struct (l3side.h:28) — the scalefactor-band boundary tables.
// ---------------------------------------------------------------------------

// ScalefacBand is LAME's scalefac_struct (l3side.h:28): the long / short
// scalefactor-band boundaries plus the partitioned sfb21 / sfb12 sub-band
// boundaries lame_init_params derives from them. (The psymodel reads only L
// and S.)
type ScalefacBand struct {
	L      [1 + SBMAXl]int // scalefac_band.l
	S      [1 + SBMAXs]int // scalefac_band.s
	Psfb21 [1 + PSFB21]int // scalefac_band.psfb21
	Psfb12 [1 + PSFB12]int // scalefac_band.psfb12
}

// ---------------------------------------------------------------------------
// gr_info (l3side.h:46) — the full per-granule side-information struct.
// This single mirror merges the disjoint field subsets the huffman-encode,
// mdct-analysis and frame-dispatch slices each declared. Field names and
// types mirror the C members so every ported emitter / MDCT writer / dispatch
// read indexes them identically.
// ---------------------------------------------------------------------------

// GrInfo is LAME's gr_info (l3side.h:46). The huffman emitters read Xr / L3Enc
// / BigValues / Count1 / TableSelect / Region0Count / Region1Count /
// Count1tableSelect; mdct_sub48 fills Xr and reads BlockType / MixedBlockFlag;
// the frame dispatcher sets BlockType / MixedBlockFlag. The remaining members
// (Scalefac, the part2/sfb geometry) are populated and consumed by the
// quantizer / side-info slices and are mapped for layout completeness.
type GrInfo struct {
	// Xr holds the 576 (signed) MDCT frequency lines (gr_info.xr[576]);
	// mdct_sub48 fills them, the huffman emitters read their sign.
	Xr [576]float32

	// L3Enc holds the 576 quantized magnitudes to Huffman-code
	// (gr_info.l3_enc[576]).
	L3Enc [576]int

	// Scalefac holds the SFBMAX scalefactors (gr_info.scalefac[SFBMAX]).
	Scalefac [SFBMAX]int

	// XrpowMax is the granule's max |xr|^(3/4) (gr_info.xrpow_max).
	XrpowMax float32

	// Part23Length is the scalefactor + Huffman data length in bits
	// (gr_info.part2_3_length).
	Part23Length int

	// BigValues is the number of big-value pairs (gr_info.big_values).
	BigValues int

	// Count1 is the index one past the last count1 line (gr_info.count1).
	Count1 int

	// GlobalGain is the quantizer step-size exponent (gr_info.global_gain).
	GlobalGain int

	// ScalefacCompress selects the scalefactor bit-length table
	// (gr_info.scalefac_compress).
	ScalefacCompress int

	// BlockType is the granule's window/block type (gr_info.block_type):
	// NormType / StartType / ShortType / StopType.
	BlockType int

	// MixedBlockFlag is set when a short-block granule mixes a long block in
	// its lowest two subbands (gr_info.mixed_block_flag).
	MixedBlockFlag int

	// TableSelect holds the big-value Huffman codebook index for each of the
	// three regions (gr_info.table_select[3]).
	TableSelect [3]int

	// SubblockGain holds the per-window gain for short blocks
	// (gr_info.subblock_gain[3+1]).
	SubblockGain [3 + 1]int

	// Region0Count / Region1Count are the scalefactor-band counts of the first
	// two big-value regions (gr_info.region0_count / region1_count).
	Region0Count int
	Region1Count int

	// Preflag enables the high-frequency scalefactor preamplification table
	// (gr_info.preflag).
	Preflag int

	// ScalefacScale selects the scalefactor scaling step (gr_info.scalefac_scale).
	ScalefacScale int

	// Count1tableSelect picks the count1 codebook: 0 -> ht[32], 1 -> ht[33]
	// (gr_info.count1table_select).
	Count1tableSelect int

	// Part2Length is the scalefactor-only bit length (gr_info.part2_length).
	Part2Length int

	// SfbLmax / SfbSmin / PsyLmax / Sfbmax / Psymax / Sfbdivide are the
	// block-geometry boundaries (gr_info.sfb_lmax / sfb_smin / psy_lmax /
	// sfbmax / psymax / sfbdivide).
	SfbLmax   int
	SfbSmin   int
	PsyLmax   int
	Sfbmax    int
	Psymax    int
	Sfbdivide int

	// Width / Window hold the per-scalefactor-band width and window index
	// (gr_info.width[SFBMAX] / window[SFBMAX]).
	Width  [SFBMAX]int
	Window [SFBMAX]int

	// Count1bits is the count1-region bit length (gr_info.count1bits).
	Count1bits int

	// SfbPartitionTable / Slen are the LSF scalefactor partitioning
	// (gr_info.sfb_partition_table / slen[4]); SfbPartitionTable points into a
	// static const C table, carried as a slice.
	SfbPartitionTable []int
	Slen              [4]int

	// MaxNonzeroCoeff is the index past the last nonzero coefficient
	// (gr_info.max_nonzero_coeff).
	MaxNonzeroCoeff int

	// EnergyAboveCutoff marks bands with energy above the lowpass cutoff
	// (gr_info.energy_above_cutoff[SFBMAX]).
	EnergyAboveCutoff [SFBMAX]byte
}

// ---------------------------------------------------------------------------
// III_side_info_t (l3side.h:86) — the whole-frame side info.
// ---------------------------------------------------------------------------

// IIISideInfo is LAME's III_side_info_t (l3side.h:86): the per-granule,
// per-channel gr_info plus the reservoir back-pointer, private bits, reservoir
// drain counts and scalefactor-selection bits.
type IIISideInfo struct {
	Tt            [2][2]GrInfo // tt[gr][ch]
	MainDataBegin int          // main_data_begin
	PrivateBits   int          // private_bits
	ResvDrainPre  int          // resvDrain_pre
	ResvDrainPost int          // resvDrain_post
	Scfsi         [2][4]int    // scfsi[ch][band]
}

// ---------------------------------------------------------------------------
// The unified context.
// ---------------------------------------------------------------------------

// LameInternalFlags is LAME's lame_internal_flags (util.h:463): the single
// encoder context every stage threads through. It is the Go stand-in for the
// `gfc` pointer and the receiver of every method the C invokes on gfc. Only the
// members the ported slices reach are mapped; the not-yet-ported areas
// (resampler init, VBR seek table, ReplayGain, id3 tag spec, the CPU-feature
// function pointers) are omitted and join as their slices land.
type LameInternalFlags struct {
	// LameInitParamsSuccessful / LameEncodeFrameInit / IterationInitInit are
	// the one-shot init latches (lame_init_params_successful util.h:486,
	// lame_encode_frame_init util.h:487, iteration_init_init util.h:488).
	LameInitParamsSuccessful int
	LameEncodeFrameInit      int
	IterationInitInit        int

	// Cfg is the immutable session configuration (gfc->cfg, util.h:491).
	Cfg SessionConfig

	// Bs is the output bit stream (gfc->bs, util.h:494).
	Bs EncBitStream

	// L3Side is the whole-frame side info (gfc->l3_side, util.h:495); L3Side.Tt
	// is the per-granule, per-channel gr_info indexed [gr][ch].
	L3Side IIISideInfo

	// ScalefacBand holds the scalefactor-band boundary tables (gfc->scalefac_band,
	// util.h:497).
	ScalefacBand ScalefacBand

	// SvPsy / OvPsy are the psychoacoustic state and result (gfc->sv_psy /
	// ov_psy, util.h:499/500).
	SvPsy PsyStateVar
	OvPsy PsyResult

	// SvEnc / OvEnc are the encoder state and result (gfc->sv_enc / ov_enc,
	// util.h:501/502).
	SvEnc EncStateVar
	OvEnc EncResult

	// SvQnt is the quantizer state (gfc->sv_qnt, util.h:503).
	SvQnt QntStateVar

	// OvRpg is the ReplayGain analysis result (gfc->ov_rpg, util.h); PutLameVBR
	// reads RadioGain / PeakSample when cfg.FindReplayGain / FindPeakSample are
	// set.
	OvRpg RpgResult

	// VBRSeekTable is the Xing/LAME VBR seek-table bookkeeping (gfc->VBR_seek_table,
	// util.h:525); InitVbrTag seeds it and AddVbrFrame appends each frame's bitrate.
	VBRSeekTable VbrSeekInfo

	// NMusicCRC is the running CRC-16 over the emitted mp3 audio bytes
	// (gfc->nMusicCRC, util.h:510); copy_buffer updates it via UpdateMusicCRC and
	// PutLameVBR embeds it in the LAME tag.
	NMusicCRC uint16

	// ATH is the threshold-of-hearing state (gfc->ATH, util.h:527); allocated
	// by lame_init_params.
	ATH *ATH

	// CdPsy is the session-constant psychoacoustic data (gfc->cd_psy,
	// util.h:529); allocated by psymodel_init.
	CdPsy *PsyConst

	// Stages is the seam onto the heavy per-stage callees the dispatcher and
	// init reach (psy model, MDCT, iteration loops, bitstream, init helpers).
	// In the C these are direct calls into the other translation units; the
	// Go port routes them through this interface while those callees land
	// incrementally (mirroring the C's translation-unit boundaries).
	Stages EncoderStages
}

// SessionConfig is LAME's SessionConfig_t (util.h:356): the immutable
// per-session configuration lame_init_params computes once and every encode
// stage reads. This single mirror merges the field subsets the psychoacoustic
// model, the bit writer and the frame dispatcher each declared. Field names and
// types mirror the C members so the translated logic indexes them identically.
// Only the members the ported slices read, plus those lame_init_params writes,
// are mapped; the filter / ReplayGain / id3 members the C carries are populated
// by not-yet-ported slices.
type SessionConfig struct {
	// Version is 0 for MPEG-2/2.5, 1 for MPEG-1 (cfg->version); SamplerateIndex
	// is the samplerate-table column (cfg->samplerate_index); SideinfoLen is the
	// side-information length in bytes (cfg->sideinfo_len).
	Version         int
	SamplerateIndex int
	SideinfoLen     int

	// NoiseShaping / NoiseShapingAmp / SubblockGain / UseBestHuffman /
	// FullOuterLoop select quantizer behaviour (cfg->noise_shaping /
	// noise_shaping_amp / subblock_gain / use_best_huffman / full_outer_loop).
	// lame_init_qval (set_get.c) fills NoiseShaping / NoiseShapingAmp /
	// SubblockGain / UseBestHuffman / FullOuterLoop from gfp->quality; the
	// quantize.c iteration loop (amp_scalefac_bands / outer_loop) branches on
	// them.
	NoiseShaping    int
	NoiseShapingAmp int
	SubblockGain    int
	UseBestHuffman  int
	FullOuterLoop   int

	// SamplerateIn / SamplerateOut are the input / output sample rates in Hz
	// (cfg->samplerate_in / samplerate_out).
	SamplerateIn  int
	SamplerateOut int

	// ChannelsIn / ChannelsOut are the input / output channel counts
	// (cfg->channels_in / channels_out).
	ChannelsIn  int
	ChannelsOut int

	// ModeGr is the number of granules per frame (cfg->mode_gr): 2 for MPEG-1,
	// 1 for MPEG-2/2.5.
	ModeGr int

	// ForceMs forces mid/side stereo (cfg->force_ms); requires Mode ==
	// JOINT_STEREO.
	ForceMs int

	// QuantComp / QuantCompShort select the quantization comparison
	// (cfg->quant_comp / quant_comp_short).
	QuantComp      int
	QuantCompShort int

	// UseTemporalMaskingEffect / UseSafeJointStereo are psy-model toggles
	// (cfg->use_temporal_masking_effect / use_safe_joint_stereo).
	UseTemporalMaskingEffect int
	UseSafeJointStereo       int

	// Preset is the chosen preset id (cfg->preset).
	Preset int

	// Vbr is the VBR mode (cfg->vbr): one of vbrOff/vbrMt/vbrRh/vbrAbr/vbrMtrh.
	Vbr int

	// VbrAvgBitrateKbps / VbrMinBitrateIndex / VbrMaxBitrateIndex / AvgBitrate
	// are the VBR/CBR bitrate parameters (cfg->vbr_avg_bitrate_kbps /
	// vbr_min_bitrate_index / vbr_max_bitrate_index / avg_bitrate).
	VbrAvgBitrateKbps  int
	VbrMinBitrateIndex int
	VbrMaxBitrateIndex int
	AvgBitrate         int

	// EnforceMinBitrate / FindReplayGain / FindPeakSample / DecodeOnTheFly /
	// Analysis / DisableReservoir / BufferConstraint / FreeFormat /
	// WriteLameTag are the boolean session flags (cfg->enforce_min_bitrate /
	// findReplayGain / findPeakSample / decode_on_the_fly / analysis /
	// disable_reservoir / buffer_constraint / free_format / write_lame_tag).
	EnforceMinBitrate int
	FindReplayGain    int
	FindPeakSample    int
	DecodeOnTheFly    int
	Analysis          int
	DisableReservoir  int
	BufferConstraint  int
	FreeFormat        int
	WriteLameTag      int

	// ErrorProtection / Copyright / Original / Extension / Emphasis are the
	// frame-header flag bits (cfg->error_protection / copyright / original /
	// extension / emphasis).
	ErrorProtection int
	Copyright       int
	Original        int
	Extension       int
	Emphasis        int

	// Mode is the MPEG channel mode (cfg->mode, MPEG_mode); ShortBlocks is the
	// short-block policy (cfg->short_blocks, short_block_t).
	Mode        int
	ShortBlocks int

	// LowpassFreq / HighpassFreq are the filter cutoff frequencies in Hz
	// (cfg->lowpassfreq / highpassfreq).
	LowpassFreq  int
	HighpassFreq int

	// InterChRatio / Msfix are the inter-channel and Naoki M/S adjustments
	// (cfg->interChRatio / msfix).
	InterChRatio float32
	Msfix        float32

	// ATHOffsetDb / ATHOffsetFactor / ATHcurve are the ATH shaping parameters
	// (cfg->ATH_offset_db / ATH_offset_factor / ATHcurve). ATHtype / ATHonly /
	// ATHshort / NoATH select the ATH formula and usage (cfg->ATHtype / ATHonly
	// / ATHshort / noATH). ATHfixpoint mirrors cfg->ATHfixpoint.
	ATHOffsetDb     float32
	ATHOffsetFactor float32
	ATHcurve        float32
	ATHtype         int
	ATHonly         int
	ATHshort        int
	NoATH           int
	ATHfixpoint     float32

	// AdjustAltoDb / AdjustBassDb / AdjustTrebleDb / AdjustSfb21Db are the
	// nspsytune per-region adjustments (cfg->adjust_alto_db / adjust_bass_db /
	// adjust_treble_db / adjust_sfb21_db).
	AdjustAltoDb   float32
	AdjustBassDb   float32
	AdjustTrebleDb float32
	AdjustSfb21Db  float32

	// CompressionRatio is sizeof(wav)/sizeof(mp3) (cfg->compression_ratio).
	CompressionRatio float32

	// Lowpass1 / Lowpass2 / Highpass1 / Highpass2 are the normalized passband
	// bounds (cfg->lowpass1 / lowpass2 / highpass1 / highpass2).
	Lowpass1  float32
	Lowpass2  float32
	Highpass1 float32
	Highpass2 float32

	// PcmTransform is the 2x2 input scaling / downmix matrix
	// (cfg->pcm_transform[2][2]).
	PcmTransform [2][2]float32

	// Minval is the psymodel minval floor (cfg->minval).
	Minval float32
}

// EncoderStages is the seam onto the not-yet-translated heavy stages of the
// encode pipeline and the init helpers lame_init_params calls out to. Each
// method is the Go stand-in for the C function the dispatcher / init reaches in
// another translation unit; their call sites mirror the C 1:1. A later slice
// supplies a concrete implementation wiring the real psymodel.c / newmdct.c /
// quantize.c / bitstream.c / presets.c ports; the dispatcher and init own only
// the orchestration. Defining the stages as an interface keeps the translated
// control flow exact and the default build green while the callees land.
type EncoderStages interface {
	// MdctSub48 is newmdct.c's mdct_sub48 (encoder.c:224 prime / encoder.c:405
	// frame): the polyphase + MDCT analysis over both channels' PCM, writing
	// GrInfo.Xr.
	MdctSub48(gfc *LameInternalFlags, w0, w1 []float32)

	// L3PsychoAnalVbr is psymodel.c's L3psycho_anal_vbr (encoder.c:374): the
	// per-granule perceptual analysis. It fills the LR and MS maskings,
	// perceptual entropy, total energy and per-channel block types, and returns
	// 0 on success (non-zero aborts the frame with -4).
	L3PsychoAnalVbr(gfc *LameInternalFlags, bufp [2][]float32, gr int,
		maskingLR, maskingMS *[2][2]III_psy_ratio,
		pe, peMS *[2]float32, totEner *[4]float32, blocktype *[2]int) int

	// AdjustATH is psymodel.c's adjust_ATH (encoder.c:397): the per-frame
	// auto-adjust of the threshold of hearing.
	AdjustATH(gfc *LameInternalFlags)

	// CBRIterationLoop is quantize.c's CBR_iteration_loop (encoder.c:523).
	CBRIterationLoop(gfc *LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]III_psy_ratio)

	// ABRIterationLoop is quantize.c's ABR_iteration_loop (encoder.c:526).
	ABRIterationLoop(gfc *LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]III_psy_ratio)

	// VBROldIterationLoop is quantize.c's VBR_old_iteration_loop (encoder.c:529).
	VBROldIterationLoop(gfc *LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]III_psy_ratio)

	// VBRNewIterationLoop is quantize.c's VBR_new_iteration_loop (encoder.c:533).
	VBRNewIterationLoop(gfc *LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]III_psy_ratio)

	// FormatBitstream is bitstream.c's format_bitstream (encoder.c:544).
	FormatBitstream(gfc *LameInternalFlags)

	// CopyBuffer is bitstream.c's copy_buffer (encoder.c:547): drain the
	// internal bit buffer into mp3buf, returning the byte count (or <0 on a
	// buffer-too-small error).
	CopyBuffer(gfc *LameInternalFlags, mp3buf []byte, mp3bufSize, mt int) int

	// AddVbrFrame is VbrTag.c's AddVbrFrame (encoder.c:551).
	AddVbrFrame(gfc *LameInternalFlags)

	// UpdateStats is lame.c's updateStats (encoder.c:571).
	UpdateStats(gfc *LameInternalFlags)

	// InitParamsPpflt is lame.c's lame_init_params_ppflt (lame.c:105,
	// lame.c:910): designs the polyphase lowpass/highpass filter from the
	// cfg->lowpass*/highpass* bounds. A separate (filter-design) slice; the
	// init port calls it through this seam.
	InitParamsPpflt(gfc *LameInternalFlags)

	// ApplyPreset is presets.c's apply_preset (lame.c:995/1020/1057): expands
	// the requested preset/bitrate into the dozens of gfp tuning fields. A
	// separate (presets) slice; lame_init_params calls it through this seam,
	// passing the C `int bitrate` argument and the cbr flag.
	ApplyPreset(gfc *LameInternalFlags, gfp *LameGlobalFlags, bitrate, cbr int)

	// InitQval is set_get.c / lame.c's lame_init_qval (lame.c:1129): fills the
	// internal qval-derived noise-shaping settings from gfp->quality. A separate
	// (qval) slice; called through this seam.
	InitQval(gfc *LameInternalFlags, gfp *LameGlobalFlags)

	// IterationInit is quantize_pvt.c's iteration_init (lame.c:1278): the
	// one-time quantizer table / pow20 / ipow20 setup and the iteration-loop
	// function-pointer selection. A separate (quantize) slice; called through
	// this seam.
	IterationInit(gfc *LameInternalFlags)

	// PsymodelInit is psymodel.c's psymodel_init (lame.c:1279): the one-time
	// psychoacoustic constant setup (FFT windows, CB2SB mappings, ATH curves).
	// The pure-Go port already exists as (*LameInternalFlags).InitPsyModel; this
	// seam method lets a wiring slice route to it (or to the cgo oracle) while
	// keeping the init driver's call site 1:1 with the C.
	PsymodelInit(gfc *LameInternalFlags, gfp *LameGlobalFlags) int

	// GetMaxFrameBufferSizeByConstraint mirrors lame.c's
	// get_max_frame_buffer_size_by_constraint (lame.c:1281).
	GetMaxFrameBufferSizeByConstraint(gfc *LameInternalFlags, strictISO int) int

	// OptimumBandwidth is lame.c's optimum_bandwidth (lame.c:196): the
	// bitrate-driven lowpass/highpass auto-bandwidth design used only when the
	// user left the lowpass frequency unset. It is a filter-design helper that
	// belongs with the ppflt slice; the init port reaches it through this seam
	// and returns the chosen lower/upper limits.
	OptimumBandwidth(bitrate int) (lower, upper float64)

	// OptimumSamplefreq is lame.c's optimum_samplefreq (lame.c:276): picks the
	// output sample rate from the lowpass cutoff and input rate when the user
	// left the output rate unset. Filter-design helper reached through this
	// seam.
	OptimumSamplefreq(lowpassFreq, inputSamplefreq int) int
}
