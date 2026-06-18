package nativemp3

// Frame-decode-dispatch — minimp3's top-level decode driver. This file is a
// 1:1 translation of the main decode loop (mp3dec_decode_frame), the decoder
// reset (mp3dec_init), and the frame_info bookkeeping that drives them. It is
// the orchestrator: it detects/validates a frame, fills the FrameInfo, splits
// the main data out of the bitstream, and dispatches each granule to the
// Layer III or Layer I/II decode path followed by the synthesis filterbank.
//
// The actual per-granule decode (L3_decode) and the Layer I/II path (L12_*)
// live in separate slices. Until those slices land, the dispatch reaches them
// through the two function seams below (l3Decode, l12DecodeFrame); the owning
// slice assigns the seam in its package init. The synthesis driver
// (mp3dSynthGranule) is already translated in the IMDCT slice and is called
// directly. This indirection is purely a build seam — the dispatch control
// flow below is byte-for-byte the C; only the cross-slice callees are routed
// through the seam variables.

// FrameInfo is a 1:1 translation of minimp3's mp3dec_frame_info_t
// (minimp3.h:13).
//
//	typedef struct
//	{
//	    int frame_bytes, frame_offset, channels, hz, layer, bitrate_kbps;
//	} mp3dec_frame_info_t;
//
// mp3dec_decode_frame fills it on every call: FrameBytes is how many input
// bytes the frame (plus any leading garbage skipped to reach it) consumed,
// FrameOffset is where the frame header began within the input, and the
// remaining fields describe the decoded audio.
type FrameInfo struct {
	// FrameBytes is the total input bytes consumed: the offset to the frame
	// plus the frame size (mp3dec_frame_info_t.frame_bytes).
	FrameBytes int

	// FrameOffset is the byte offset of the accepted frame header within the
	// input buffer (mp3dec_frame_info_t.frame_offset).
	FrameOffset int

	// Channels is 1 for mono, 2 otherwise (mp3dec_frame_info_t.channels).
	Channels int

	// Hz is the output sample rate in Hz (mp3dec_frame_info_t.hz).
	Hz int

	// Layer is the MPEG audio layer: 1, 2, or 3 (mp3dec_frame_info_t.layer).
	Layer int

	// BitrateKbps is the frame bitrate in kbit/s (mp3dec_frame_info_t.bitrate_kbps).
	BitrateKbps int
}

// l3Decode is the seam to the Layer III per-granule decode slice
// (L3_decode, minimp3.h:1238). It decodes nch channels of one granule
// in-place into the scratch: scalefactors, Huffman, stereo processing,
// anti-alias, IMDCT, and sign change. gr indexes the granule's first
// L3GrInfo within s.GrInfo. The L3-granule-decode slice assigns this; the
// dispatch panics with a clear message if it is invoked before that slice
// is wired (a build-ordering bug, never a runtime input condition).
var l3Decode func(h *Decoder, s *Scratch, gr int, nch int)

// l12DecodeFrame is the seam to the Layer I/II decode path — the `#else`
// branch of mp3dec_decode_frame's layer dispatch (the L12_read_scale_info /
// L12_dequantize_granule / L12_apply_scf_384 loop and its synthesis,
// minimp3.h:1782-1803). It decodes the whole frame's Layer I/II audio into
// pcm starting at pcmOff (in samples) and returns true if the C path took
// its early `return 0` (a bitstream overflow that also calls mp3dec_init).
// The Layer I/II slice assigns this. When that slice is absent and a
// non-Layer-III frame is encountered the dispatch panics, mirroring that
// the Layer III decoder of this toolkit is the supported path.
var l12DecodeFrame func(h *Decoder, hdr []byte, bs *BitStream, s *Scratch, info *FrameInfo, pcm []int16, pcmOff int) (earlyReturn bool)

// Mp3decInit is a 1:1 translation of mp3dec_init (minimp3.h:1708): it resets
// the decoder so the next mp3dec_decode_frame re-syncs from scratch. minimp3
// only needs to invalidate the cached header for this; the rest of the
// cross-frame state is rebuilt on the next accepted frame.
//
//	void mp3dec_init(mp3dec_t *dec)
//	{
//	    dec->header[0] = 0;
//	}
func Mp3decInit(dec *Decoder) {
	dec.Header[0] = 0
}

// DecodeFrame is a 1:1 translation of mp3dec_decode_frame (minimp3.h:1713):
// the top-level frame decoder. It locates the next valid MPEG audio frame in
// mp3[:mp3Bytes], fills info, and — when pcm is non-nil — decodes the frame's
// audio into pcm as interleaved int16 samples, returning the number of
// samples produced per channel (0 on a frame that could not be decoded).
// When pcm is nil it only probes: it fills info and returns the frame's
// per-channel sample count without decoding audio.
//
// pcm receives info.Channels-interleaved samples; the caller must size it for
// at least MaxSamplesPerFrame. The C advances the pcm pointer by
// 576*channels per Layer III granule; here the running pcmOff offset (in
// samples) plays that role.
//
//	int mp3dec_decode_frame(mp3dec_t *dec, const uint8_t *mp3, int mp3_bytes, mp3d_sample_t *pcm, mp3dec_frame_info_t *info)
func DecodeFrame(dec *Decoder, mp3 []byte, mp3Bytes int, pcm []int16, info *FrameInfo) int {
	i := 0
	frameSize := 0
	success := 1

	if mp3Bytes > 4 && dec.Header[0] == 0xff && hdrCompare(dec.Header[:], mp3) {
		frameSize = hdrFrameBytes(mp3, dec.FreeFormatBytes) + hdrPadding(mp3)
		if frameSize != mp3Bytes && (frameSize+HDRSize > mp3Bytes || !hdrCompare(mp3, mp3[frameSize:])) {
			frameSize = 0
		}
	}
	if frameSize == 0 {
		// memset(dec, 0, sizeof(mp3dec_t)) — clear all cross-frame state.
		*dec = Decoder{}
		i = mp3dFindFrame(mp3, mp3Bytes, &dec.FreeFormatBytes, &frameSize)
		if frameSize == 0 || i+frameSize > mp3Bytes {
			info.FrameBytes = i
			return 0
		}
	}

	hdr := mp3[i:]
	copy(dec.Header[:], hdr[:HDRSize])
	info.FrameBytes = i + frameSize
	info.FrameOffset = i
	if hdrIsMono(hdr) {
		info.Channels = 1
	} else {
		info.Channels = 2
	}
	info.Hz = int(hdrSampleRateHz(hdr))
	info.Layer = 4 - hdrGetLayer(hdr)
	info.BitrateKbps = int(hdrBitrateKbps(hdr))

	if pcm == nil {
		return int(hdrFrameSamples(hdr))
	}

	var bsFrame BitStream
	BsInit(&bsFrame, hdr[HDRSize:], frameSize-HDRSize)
	if hdrIsCRC(hdr) {
		GetBits(&bsFrame, 16)
	}

	var scratch Scratch

	if info.Layer == 3 {
		mainDataBegin, ok := L3ReadSideInfo(&bsFrame, scratch.GrInfo[:], hdr)
		if !ok || bsFrame.Pos > bsFrame.Limit {
			Mp3decInit(dec)
			return 0
		}
		if L3RestoreReservoir(dec, &bsFrame, &scratch, mainDataBegin) {
			success = 1
		} else {
			success = 0
		}
		if success != 0 {
			grCount := 1
			if hdrTestMPEG1(hdr) != 0 {
				grCount = 2
			}
			pcmOff := 0
			for igr := 0; igr < grCount; igr++ {
				// memset(scratch.grbuf[0], 0, 576*2*sizeof(float)).
				scratch.GrBuf = [2][576]float32{}
				l3Decode(dec, &scratch, igr*info.Channels, info.Channels)
				mp3dSynthGranule(dec.QmfState[:], scratch.GrBufFlat(), 18, info.Channels, pcm[pcmOff:], scratch.SynFlat())
				pcmOff += 576 * info.Channels
			}
		}
		L3SaveReservoir(dec, &scratch)
	} else {
		if l12DecodeFrame(dec, hdr, &bsFrame, &scratch, info, pcm, 0) {
			return 0
		}
	}
	return success * int(hdrFrameSamples(dec.Header[:]))
}
