// Package nativemp3 is a 1:1 Go translation of the vendored minimp3 C
// reference decoder (the public-domain single-header minimp3.h by lieff).
// It exists so the public libraries/mp3 package can serve a pure-Go path
// when cgo is unavailable, and so the project's parity infrastructure can
// use the cgo build of minimp3 as a (PSNR-exact under mp3_strict) oracle.
//
// # Scope of this slice
//
// This file set covers minimp3's "main-bits" area only: the bit-level
// reader over a frame's byte payload (bs_t / bs_init / get_bits), the
// frame-sync loop that locates and validates an MPEG-1/2/2.5 Layer III
// frame in a byte run (hdr_valid, hdr_compare, mp3d_match_frame,
// mp3d_find_frame and the hdr_* field accessors they call), and the main
// data buffer reassembly across frames (the bit reservoir: reserv /
// reservBuf state plus L3_restore_reservoir / L3_save_reservoir). The
// granule decode, IMDCT, synthesis filterbank, and Huffman stages live in
// other slices and are intentionally not translated here.
//
// It also owns the frame-decode-dispatch slice (framedecode.go): the
// top-level mp3dec_decode_frame / mp3dec_init driver that detects a frame,
// fills the mp3dec_frame_info_t, dispatches to the Layer III or Layer
// I/II granule decode + synthesis, and returns the produced sample count.
// Because the dispatch is the orchestrator, the mp3dec_t / mp3dec_scratch_t
// struct fields the downstream slices consume (mdct_overlap, qmf_state,
// gr_info, grbuf, scf, syn, ist_pos) are declared on Decoder / Scratch
// below, even though other slices read and write them. The L3 granule
// decode (L3_decode) and the Layer I/II path (L12_*) are separate slices;
// the dispatch reaches them through the documented function seams in
// framedecode.go that those slices populate when they land.
//
// # Strict mode
//
// This slice is integer-only (kind=integer in the porting work-list).
// Every function below is bit-identical regardless of build tag or
// vectorization, so there is no FMA-sensitive code here and no
// _fp_strict / _fp_default split is needed. The mp3_strict build tag only
// affects the floating-point slices (IMDCT, synthesis) elsewhere in the
// package.
//
// # Layout / conventions
//
// minimp3 packs all cross-frame decoder state into a single mp3dec_t
// struct; the bit reservoir fields (reserv, reservBuf, freeFormatBytes,
// header) live on Decoder here. The remaining mp3dec_t fields
// (mdctOverlap, qmfState) belong to the synthesis slices and are declared
// alongside them. Per-frame scratch (minimp3's mp3dec_scratch_t) is
// likewise shared across slices; the maindata reservoir buffer it owns is
// declared here because L3_restore_reservoir writes into it.
//
// Every ported function carries a doc comment naming its minimp3.h C
// counterpart as file:line so a future reader can diff against the C.
//
// The package is internal to libraries/mp3. External code goes through
// libraries/mp3.NewNativeDecoder / NewNativeEncoder.
package nativemp3

import "unsafe"

// SynFlat returns a flat view over the whole Syn scratch array.
//
// minimp3 passes s->syn[0] both to L3_reorder (as a 576-float scratch buffer)
// and to mp3d_synth_granule (as the (18+15)*64-float "lins" line buffer). In C
// that argument is the first row's pointer, which decays to address the entire
// contiguous syn[33][64] block. Go's Syn[0][:] is bounded to one 64-float row,
// so this helper reconstructs the flat (18+15)*64 view from the contiguous
// backing array, matching the C aliasing exactly.
func (s *Scratch) SynFlat() []float32 {
	return unsafe.Slice(&s.Syn[0][0], len(s.Syn)*len(s.Syn[0]))
}

// GrBufFlat returns a flat view over the whole GrBuf scratch array.
//
// minimp3's s->grbuf[0] is a float* into a contiguous grbuf[2][576] block, and
// the Layer III decode, stereo, requantization, IMDCT and synthesis routines
// all address it as a flat 1152-float buffer that spans both channels (e.g.
// grbuf[576] reaches the right channel). Go's GrBuf[0][:] is bounded to the
// first 576-float channel row, so this helper reconstructs the flat 2*576 view
// from the contiguous backing array, matching the C aliasing exactly.
func (s *Scratch) GrBufFlat() []float32 {
	return unsafe.Slice(&s.GrBuf[0][0], len(s.GrBuf)*len(s.GrBuf[0]))
}

// minimp3.h frame / reservoir size constants.
//
//   - HDRSize is the MPEG audio frame header length in bytes (minimp3.h:61,
//     #define HDR_SIZE 4).
//   - MaxBitReservoirBytes bounds the carried-over main data reservoir
//     (minimp3.h:56, #define MAX_BITRESERVOIR_BYTES 511).
//   - MaxFreeFormatFrameSize bounds a free-format frame and, via
//     MaxL3FramePayloadBytes, the per-frame main data payload
//     (minimp3.h:49/54).
//   - MaxFrameSyncMatches is how many consecutive headers mp3d_match_frame
//     requires to accept a sync (minimp3.h:51, #define
//     MAX_FRAME_SYNC_MATCHES 10).
const (
	HDRSize                = 4
	MaxBitReservoirBytes   = 511
	MaxFreeFormatFrameSize = 2304
	MaxFrameSyncMatches    = 10
	MaxL3FramePayloadBytes = MaxFreeFormatFrameSize
)

// MaxSamplesPerFrame is the largest number of interleaved samples (across
// all channels) a single decoded frame can yield, sizing a caller's PCM
// output buffer (MINIMP3_MAX_SAMPLES_PER_FRAME, minimp3.h:11).
//
//	#define MINIMP3_MAX_SAMPLES_PER_FRAME (1152*2)
const MaxSamplesPerFrame = 1152 * 2

// Decoder holds minimp3's cross-frame decoder state (a 1:1 mapping of the
// fields of the C mp3dec_t struct, minimp3.h:18, that the main-bits slice
// owns).
//
// The bit reservoir is the carried-over tail of previous frames' main
// data: ReservBuf holds Reserv bytes of leftover main data that the next
// frame's main_data_begin back-pointer may reach into. Header caches the
// last accepted frame header for the fast-resync compare in the decode
// driver, and FreeFormatBytes caches a discovered free-format frame size.
//
// minimp3's mp3dec_t also carries mdctOverlap and qmfState floats for the
// synthesis slices; those are declared with that code, not here.
type Decoder struct {
	// Reserv is the number of valid bytes currently held in ReservBuf
	// (mp3dec_t.reserv, minimp3.h:21).
	Reserv int

	// FreeFormatBytes caches the detected free-format frame size in bytes,
	// or 0 for a normal (bitrate-indexed) stream (mp3dec_t.free_format_bytes,
	// minimp3.h:21).
	FreeFormatBytes int

	// Header is the last accepted 4-byte frame header (mp3dec_t.header[4],
	// minimp3.h:22).
	Header [HDRSize]byte

	// ReservBuf is the bit reservoir: up to MaxBitReservoirBytes of main
	// data carried over from previous frames (mp3dec_t.reserv_buf[511],
	// minimp3.h:22).
	ReservBuf [MaxBitReservoirBytes]byte

	// MdctOverlap is the per-channel IMDCT overlap-add history carried
	// between frames (mp3dec_t.mdct_overlap[2][9*32], minimp3.h:20). The
	// IMDCT slice reads and updates it; the frame-decode-dispatch slice
	// passes &MdctOverlap[ch] into L3_decode and clears the whole struct on
	// a lost sync via mp3dec_init.
	MdctOverlap [2][9 * 32]float32

	// QmfState is the polyphase synthesis filterbank history carried between
	// granules and frames (mp3dec_t.qmf_state[15*2*32], minimp3.h:20). The
	// synthesis slice (mp3d_synth_granule) reads and updates it.
	QmfState [15 * 2 * 32]float32
}

// Scratch holds the per-frame working state that minimp3 keeps in its
// mp3dec_scratch_t struct (minimp3.h). Only the Bs reader and the
// Maindata reservoir-reassembly buffer that the main-bits slice writes are
// declared here; the granule / IMDCT / synthesis fields belong to other
// slices and are declared with that code.
type Scratch struct {
	// Bs is the bit reader positioned over the reassembled main data
	// (mp3dec_scratch_t.bs).
	Bs BitStream

	// Maindata is the reassembled main data buffer: the carried reservoir
	// bytes followed by this frame's payload bytes, sized to hold the
	// largest possible reservoir plus payload
	// (mp3dec_scratch_t.maindata[MAX_BITRESERVOIR_BYTES +
	// MAX_L3_FRAME_PAYLOAD_BYTES], minimp3.h:235).
	Maindata [MaxBitReservoirBytes + MaxL3FramePayloadBytes]byte

	// GrInfo holds the decoded per-granule side information for up to two
	// granules of two channels (mp3dec_scratch_t.gr_info[4], minimp3.h:236).
	// L3_read_side_info fills it; the frame-decode-dispatch slice slices it
	// per granule when calling L3_decode.
	GrInfo [4]L3GrInfo

	// GrBuf is the per-channel frequency-domain working buffer: two channels
	// of 576 coefficients laid out contiguously so GrBuf[0] is a flat 1152
	// float view (mp3dec_scratch_t.grbuf[2][576], minimp3.h:237). The
	// Huffman, requantization, stereo, IMDCT and synthesis slices all read
	// and write it.
	GrBuf [2][576]float32

	// Scf is the decoded scalefactor scratch for one channel
	// (mp3dec_scratch_t.scf[40], minimp3.h:237).
	Scf [40]float32

	// Syn is the synthesis filterbank scratch (the "lins" line buffer):
	// 18+15 rows of 2*32 floats, addressed as a flat array by
	// mp3d_synth_granule (mp3dec_scratch_t.syn[18 + 15][2*32],
	// minimp3.h:237).
	Syn [18 + 15][2 * 32]float32

	// IstPos holds the intensity-stereo position table per channel
	// (mp3dec_scratch_t.ist_pos[2][39], minimp3.h:238). L3_decode_scalefactors
	// and the stereo slice read and write it.
	IstPos [2][39]uint8
}
