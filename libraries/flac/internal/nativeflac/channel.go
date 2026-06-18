package nativeflac

// Frame-footer CRC verification + inverse stereo decorrelation, ported
// 1:1 from stream_decoder.c's read_frame_ footer block and
// undo_channel_coding. This is pure integer arithmetic — no FP.
//
// The bit reader tracks a running CRC-16 over every consumed byte once
// the caller arms it via ResetReadCRC16 at the start of the frame
// (read_frame_ seeds it with the two header-warmup bytes folded
// through FLAC__CRC16_UPDATE, stream_decoder.c:2384–2387). At the
// footer the running CRC is the expected value; the 16-bit footer the
// stream carries must match it.

// FrameFooterCRCLen (== FLAC__FRAME_FOOTER_CRC_LEN, format.h:475) is
// declared in format.go alongside the other FLAC__*_LEN constants.

// CRC16Seed folds the two frame-header warmup bytes into the CRC-16
// seed read_frame_ hands to ResetReadCRC16 (stream_decoder.c:2384–2387:
// frame_crc starts at 0 and is updated through FLAC__CRC16_UPDATE for
// each warmup byte). Equivalent to updateCRC16 over the two bytes from
// a zero seed.
func CRC16Seed(warmup0, warmup1 byte) uint16 {
	return updateCRC16([]byte{warmup0, warmup1}, 0)
}

// ReadFrameFooterCRC — port of the footer block of read_frame_
// (stream_decoder.c:2440–2452). Snapshots the bit reader's running
// CRC-16 (the value computed from the input stream so far), reads the
// 16-bit footer the stream carries, and reports whether they match.
//
// The caller must have armed CRC tracking with ResetReadCRC16 at the
// start of the frame and must be byte-aligned here (read_zero_padding_
// has run). On a read-callback failure ok is false; otherwise ok is
// true and match reports the comparison libFLAC gates undo_channel_coding
// on (frame_crc == x at stream_decoder.c:2452).
func ReadFrameFooterCRC(br *BitReader) (match bool, ok bool) {
	// frame_crc = FLAC__bitreader_get_read_crc16(...) — snapshot BEFORE
	// reading the footer, so the footer bytes themselves are not folded
	// into the expected CRC. GetReadCRC16 folds everything consumed so
	// far up to the current (byte-aligned) position.
	expected := br.GetReadCRC16()
	x, rok := br.ReadRawUint32(FrameFooterCRCLen)
	if !rok {
		return false, false
	}
	return uint16(x) == expected, true
}

// UndoChannelCoding — port of undo_channel_coding
// (stream_decoder.c:3477). Inverts the stereo decorrelation in place
// across the two per-channel sample buffers.
//
// output holds the two decoded channels (FLAC__int32 *output[] in
// libFLAC). For the LEFT_SIDE / RIGHT_SIDE / MID_SIDE assignments the
// side channel may be carried in one of two places, exactly as libFLAC
// arranges it:
//
//   - bps < 32: the side samples live in output[1] (int32). Pass
//     sideInUse == false; side64 is ignored.
//   - bps == 32: the side samples are 33-bit wide and live in the
//     separate side64 buffer (FLAC__int64 *side_subframe). Pass
//     sideInUse == true; output[1] still receives the reconstructed
//     right channel.
//
// blocksize is the number of samples per channel. INDEPENDENT is a
// no-op. The integer ops, widths, and the (mid<<1)|(side&1) mid
// reconstruction trick match libFLAC byte-for-byte; the bps<32 MID_SIDE
// path operates entirely in 32-bit (FLAC__int32 mid,side) and the
// bps==32 path in 64-bit (FLAC__int64 mid).
func UndoChannelCoding(
	assignment ChannelAssignment,
	output0 []int32,
	output1 []int32,
	side64 []int64,
	sideInUse bool,
	blocksize uint32,
) {
	switch assignment {
	case ChannelAssignmentIndependent:
		// do nothing
	case ChannelAssignmentLeftSide:
		for i := uint32(0); i < blocksize; i++ {
			if sideInUse {
				// output[1][i] = output[0][i] - side[i]
				output1[i] = int32(int64(output0[i]) - side64[i])
			} else {
				output1[i] = output0[i] - output1[i]
			}
		}
	case ChannelAssignmentRightSide:
		for i := uint32(0); i < blocksize; i++ {
			if sideInUse {
				// output[0][i] = output[1][i] + side[i]
				output0[i] = int32(int64(output1[i]) + side64[i])
			} else {
				output0[i] += output1[i]
			}
		}
	case ChannelAssignmentMidSide:
		for i := uint32(0); i < blocksize; i++ {
			if !sideInUse {
				// FLAC__int32 mid, side; (stream_decoder.c:3506–3512)
				mid := output0[i]
				side := output1[i]
				// mid = ((uint32_t) mid) << 1;
				mid = int32(uint32(mid) << 1)
				// mid |= (side & 1);
				mid |= side & 1
				output0[i] = (mid + side) >> 1
				output1[i] = (mid - side) >> 1
			} else {
				// bps == 32 (stream_decoder.c:3514–3520)
				side := side64[i]
				// FLAC__int64 mid = ((uint64_t)output[0][i]) << 1;
				// The C cast (uint64_t)(FLAC__int32) sign-extends the
				// int32 to 64-bit two's complement, so we go through
				// int64 (not uint32) before re-reading as uint64.
				mid := int64(uint64(int64(output0[i])) << 1)
				mid |= side & 1
				output0[i] = int32((mid + side) >> 1)
				output1[i] = int32((mid - side) >> 1)
			}
		}
	}
}
