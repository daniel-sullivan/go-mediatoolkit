package nativemp3

// Cross-frame and per-frame floating-point state owned by the
// imdct-synthesis-filterbank slice. These mirror the minimp3 struct members
// that the bit-reservoir (main-bits) slice in nativemp3.go deliberately left
// to "the synthesis slices": the IMDCT overlap-add history and the synthesis
// filterbank's qmf history on the decoder, plus the granule sample buffer and
// the synthesis scratch on the per-frame scratch.

// SynthState holds the cross-frame floating-point history minimp3 keeps on
// mp3dec_t for Layer III reconstruction (minimp3.h:20):
//
//	float mdct_overlap[2][9*32], qmf_state[15*2*32];
//
// MdctOverlap is the per-channel IMDCT overlap-add carry that l3IMDCTGr reads
// and updates across granules; QmfState is the polyphase synthesis
// filterbank's windowed-sample history that mp3dSynthGranule seeds lins from
// and writes the tail of lins back into. They live on a dedicated struct
// rather than nativemp3.go's Decoder so the bit-reservoir state stays a clean
// 1:1 of the fields that slice owns; the libraries/mp3 native adapter embeds
// both.
type SynthState struct {
	// MdctOverlap is mp3dec_t.mdct_overlap[2][9*32]: per-channel IMDCT
	// overlap history, 9 floats per subband x 32 subbands.
	MdctOverlap [2][9 * 32]float32

	// QmfState is mp3dec_t.qmf_state[15*2*32]: the synthesis filterbank's
	// carried history, used as the leading 15*64 floats of mp3dSynth's lins.
	QmfState [15 * 2 * 32]float32
}

// SynthScratch holds the per-frame floating-point working buffers minimp3
// keeps on mp3dec_scratch_t (minimp3.h:237):
//
//	float grbuf[2][576], scf[40], syn[18 + 15][2*32];
//
// Grbuf is the two-channel granule sample buffer the IMDCT writes and the
// synthesis filterbank consumes (32 subbands x 18 samples per channel). Syn
// is mp3dSynthGranule's lins scratch: the qmf history followed by the rows
// the windowed synthesis produces. Scf (the scalefactor scratch) belongs to
// the requantization slice and is not declared here. These live on a
// dedicated struct, separate from nativemp3.go's Scratch (which owns only the
// main-bits reader and maindata reservoir buffer), so each slice's scratch
// stays a focused 1:1 of the fields it touches.
type SynthScratch struct {
	// Grbuf is mp3dec_scratch_t.grbuf[2][576]: per-channel granule samples,
	// subband-major (subband b, sample i at index b*18 + i).
	Grbuf [2][576]float32

	// Syn is mp3dec_scratch_t.syn[18+15][2*32] = [33][64] floats, used flat
	// as mp3dSynthGranule's lins.
	Syn [18 + 15][2 * 32]float32
}
