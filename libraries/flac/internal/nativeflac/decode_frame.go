package nativeflac

import "math"

// read_subframe_ dispatch + read_frame_ orchestration, ported 1:1 from
// stream_decoder.c. These compose the per-subframe readers in
// subframe.go, the inverse stereo decorrelation + footer CRC in
// channel.go, and ReadFrameHeader in frame.go into a whole-frame decode.
//
// libFLAC threads everything through decoder->private_; the Go port
// gathers the per-frame mutable state into FrameDecodeState so the
// caller owns the buffers. The control flow, magic numbers, bps
// adjustments, and wasted-bits shift all match the C byte-for-byte.

// FrameDecodeState holds the per-frame decode buffers libFLAC keeps in
// decoder->private_. The caller allocates the output channels and the
// shared side buffer; ReadFrame fills them.
//
// Layout mirrors libFLAC:
//   - Output[ch] is FLAC__int32 *output[channel] — one int32 buffer per
//     channel, each at least Blocksize long.
//   - Side is FLAC__int64 *side_subframe — a single shared int64 buffer
//     (at least Blocksize long) used when a subframe needs 33-bit width
//     (bps == 33 after side-channel +1, or wasted-bits pushes bps to 33).
//   - SideInUse mirrors decoder->private_->side_subframe_in_use; ReadFrame
//     resets it to false on entry (stream_decoder.c:2381) and the
//     subframe readers set it true when they route through Side.
//
// The Subframe scratch structs (one per channel) capture the parsed
// metadata exactly as the standalone subframe readers fill them.
type FrameDecodeState struct {
	Output    [][]int32
	Side      []int64
	SideInUse bool
	Subframes [MaxChannels]Subframe
}

// FrameStatus is the result of a whole-frame decode attempt. It mirrors
// the way read_frame_ funnels conditions through the decoder state
// machine + send_error_to_client_.
type FrameStatus uint8

const (
	// FrameOK — frame fully parsed; the footer CRC matched. Decoded
	// samples are in state.Output (after undo_channel_coding when
	// fullDecode is set).
	FrameOK FrameStatus = iota

	// FrameReadError — a read callback starved mid-frame
	// (read_frame_ returns false; libFLAC's read_callback_ sets the
	// state). The caller cannot continue.
	FrameReadError

	// FrameLostSync — a subframe or the header lost sync, or a
	// reserved/unparseable code appeared, or the footer CRC mismatched.
	// libFLAC transitions to SEARCH_FOR_FRAME_SYNC; the caller should
	// resync on the next sync code. This collapses libFLAC's
	// LOST_SYNC / UNPARSEABLE_STREAM / FRAME_CRC_MISMATCH error statuses
	// (they all share the same recovery path).
	FrameLostSync

	// FrameBadHeader — read_frame_header_ rejected the header
	// (CRC-8 mismatch, sync inside header, bad sample number). libFLAC
	// leaves the decoder in SEARCH_FOR_FRAME_SYNC; the caller resyncs.
	FrameBadHeader

	// FrameOutOfBounds — the footer CRC matched and the frame parsed, but
	// after undo_channel_coding a decoded sample did not fit the declared
	// bits_per_sample (stream_decoder.c:2457–2473). libFLAC sends
	// OUT_OF_BOUNDS and drops to SEARCH_FOR_FRAME_SYNC, so the frame is NOT
	// written to the client and NOT folded into the MD5 (got_a_frame stays
	// false). The caller resyncs.
	FrameOutOfBounds
)

// ReadSubframe — port of read_subframe_ (stream_decoder.c:2950).
//
// Reads the 8-bit subframe type byte, decodes the wasted-bits unary
// count, maps the type bits to CONSTANT / VERBATIM / FIXED / LPC,
// extracts the predictor order, dispatches to the matching reader, then
// (when fullDecode) applies the wasted-bits left shift to the channel
// output.
//
// channel selects state.Output[channel] / state.Subframes[channel]. bps
// is the effective per-channel bits-per-sample the caller already
// adjusted for side-channel decorrelation (read_frame_ adds 1 to the
// side channel before calling). blocksize is the frame block size.
//
// On a starved reader returns FrameReadError. On a lost-sync / reserved
// / unparseable code returns FrameLostSync (matching libFLAC's
// SEARCH_FOR_FRAME_SYNC transition, which read_frame_ observes via the
// state check). FrameOK means the subframe parsed.
func ReadSubframe(
	br *BitReader,
	state *FrameDecodeState,
	channel, bps, blocksize uint32,
	fullDecode bool,
) FrameStatus {
	// FLAC__bitreader_read_raw_uint32(..., &x, 8) — the type byte.
	x, ok := br.ReadRawUint32(8)
	if !ok {
		return FrameReadError
	}

	wastedBits := (x & 1) != 0
	x &= 0xfe

	sub := &state.Subframes[channel]

	if wastedBits {
		u, ok := br.ReadUnaryUnsigned()
		if !ok {
			return FrameReadError
		}
		sub.WastedBits = u + 1
		if sub.WastedBits >= bps {
			// send_error_to_client_(LOST_SYNC); state = SEARCH.
			return FrameLostSync
		}
		bps -= sub.WastedBits
	} else {
		sub.WastedBits = 0
	}

	// Map the type bits — "Lots of magic numbers here" (line 2977).
	switch {
	case x&0x80 != 0:
		// Top bit set is reserved → LOST_SYNC.
		return FrameLostSync
	case x == 0:
		if st := readSubframeConstantDispatch(br, state, channel, bps, blocksize, fullDecode); st != FrameOK {
			return st
		}
	case x == 2:
		if st := readSubframeVerbatimDispatch(br, state, channel, bps, blocksize, fullDecode); st != FrameOK {
			return st
		}
	case x < 16:
		// 0x04..0x0E reserved → UNPARSEABLE_STREAM.
		return FrameLostSync
	case x <= 24:
		predictorOrder := (x >> 1) & 7
		if blocksize <= predictorOrder {
			return FrameLostSync
		}
		st := readSubframeFixedDispatch(br, state, channel, bps, blocksize, predictorOrder, fullDecode)
		if st != FrameOK {
			return st
		}
	case x < 64:
		// 0x32..0x3E reserved → UNPARSEABLE_STREAM.
		return FrameLostSync
	default:
		predictorOrder := ((x >> 1) & 31) + 1
		if blocksize <= predictorOrder {
			return FrameLostSync
		}
		st := readSubframeLPCDispatch(br, state, channel, bps, blocksize, predictorOrder, fullDecode)
		if st != FrameOK {
			return st
		}
	}

	// Apply the wasted-bits left shift (stream_decoder.c:3028–3046).
	if wastedBits && fullDecode {
		shift := sub.WastedBits
		if bps+shift < 33 {
			out := state.Output[channel]
			for i := uint32(0); i < blocksize; i++ {
				// val is read back as uint32 then shifted; matches the
				// C `uint32_t val = output[i]; output[i] = val << x`.
				val := uint32(out[i])
				out[i] = int32(val << shift)
			}
		} else {
			// bps is never 33 when there are wasted bits, so the side
			// buffer was not already in use (the C ASSERT).
			state.SideInUse = true
			out := state.Output[channel]
			for i := uint32(0); i < blocksize; i++ {
				// C: uint64_t val = output[channel][i]; — output is
				// FLAC__int32, so the widen-to-uint64 SIGN-extends.
				val := uint64(int64(out[i]))
				state.Side[i] = int64(val << shift)
			}
		}
	}

	return FrameOK
}

// readSubframeConstantDispatch wraps ReadSubframeConstant and routes the
// decoded constant into the int32 output (bps <= 32) or the int64 side
// buffer (bps > 32), exactly as read_subframe_constant_ does
// (stream_decoder.c:3064–3076).
func readSubframeConstantDispatch(br *BitReader, state *FrameDecodeState, channel, bps, blocksize uint32, fullDecode bool) FrameStatus {
	sub := &state.Subframes[channel]
	st := ReadSubframeConstant(br, sub, bps)
	if st == SubframeReadError {
		return FrameReadError
	}
	if st != SubframeOK {
		return FrameLostSync
	}
	if fullDecode {
		v := sub.Constant.Value
		if bps <= 32 {
			out := state.Output[channel]
			for i := uint32(0); i < blocksize; i++ {
				out[i] = int32(v)
			}
		} else {
			state.SideInUse = true
			for i := uint32(0); i < blocksize; i++ {
				state.Side[i] = v
			}
		}
	}
	return FrameOK
}

// readSubframeVerbatimDispatch wraps ReadSubframeVerbatim and copies the
// decoded raw samples into the int32 output (bps < 33) or the int64 side
// buffer (bps == 33), matching read_subframe_verbatim_
// (stream_decoder.c:3260).
func readSubframeVerbatimDispatch(br *BitReader, state *FrameDecodeState, channel, bps, blocksize uint32, fullDecode bool) FrameStatus {
	sub := &state.Subframes[channel]
	// ReadSubframeVerbatim needs caller-sized scratch slices; size them
	// to blocksize for whichever width applies.
	if bps < 33 {
		if uint32(len(sub.Verbatim.Data32)) < blocksize {
			sub.Verbatim.Data32 = make([]int32, blocksize)
		}
	} else {
		if uint32(len(sub.Verbatim.Data64)) < blocksize {
			sub.Verbatim.Data64 = make([]int64, blocksize)
		}
	}
	// read_subframe_verbatim_ sets side_subframe_in_use UNCONDITIONALLY
	// for the 33-bit path (stream_decoder.c:3288 — outside the
	// do_full_decode guard), unlike the constant/fixed/lpc readers which
	// gate it. Mirror that ordering.
	if bps >= 33 {
		state.SideInUse = true
	}
	st := ReadSubframeVerbatim(br, sub, blocksize, bps)
	if st == SubframeReadError {
		return FrameReadError
	}
	if st != SubframeOK {
		return FrameLostSync
	}
	if fullDecode {
		if bps < 33 {
			out := state.Output[channel]
			copy(out[:blocksize], sub.Verbatim.Data32[:blocksize])
		} else {
			copy(state.Side[:blocksize], sub.Verbatim.Data64[:blocksize])
		}
	}
	return FrameOK
}

// readSubframeFixedDispatch wraps ReadSubframeFixed, selecting the int32
// output (bps < 33) or the int64 side buffer (bps == 33) the same way
// read_subframe_fixed_ does (stream_decoder.c:3135–3150).
func readSubframeFixedDispatch(br *BitReader, state *FrameDecodeState, channel, bps, blocksize, order uint32, fullDecode bool) FrameStatus {
	sub := &state.Subframes[channel]
	if uint32(len(sub.Fixed.Residual)) < blocksize-order {
		sub.Fixed.Residual = make([]int32, blocksize-order)
	}
	var out32 []int32
	var out64 []int64
	if bps < 33 {
		out32 = state.Output[channel]
	} else {
		out64 = state.Side
		if fullDecode {
			state.SideInUse = true
		}
	}
	st := ReadSubframeFixed(br, sub, blocksize, bps, order, out32, out64, fullDecode)
	return mapSubframeStatus(st)
}

// readSubframeLPCDispatch wraps ReadSubframeLPC, selecting the int32
// output (bps <= 32) or the int64 side buffer (bps == 33) as
// read_subframe_lpc_ does (stream_decoder.c).
func readSubframeLPCDispatch(br *BitReader, state *FrameDecodeState, channel, bps, blocksize, order uint32, fullDecode bool) FrameStatus {
	sub := &state.Subframes[channel]
	if uint32(len(sub.LPC.Residual)) < blocksize-order {
		sub.LPC.Residual = make([]int32, blocksize-order)
	}
	var out32 []int32
	var out64 []int64
	if bps <= 32 {
		out32 = state.Output[channel]
	} else {
		out64 = state.Side
		if fullDecode {
			state.SideInUse = true
		}
	}
	st := ReadSubframeLPC(br, sub, blocksize, bps, order, out32, out64, fullDecode)
	return mapSubframeStatus(st)
}

// mapSubframeStatus translates a SubframeStatus from the fixed/LPC
// readers into the whole-frame FrameStatus. SubframeBadFrame and
// SubframeUnparseable both land on the SEARCH_FOR_FRAME_SYNC recovery
// path libFLAC uses (FrameLostSync).
func mapSubframeStatus(st SubframeStatus) FrameStatus {
	switch st {
	case SubframeOK:
		return FrameOK
	case SubframeReadError:
		return FrameReadError
	default:
		return FrameLostSync
	}
}

// ReadFrame — port of read_frame_ (stream_decoder.c:2373).
//
// Drives a whole frame: seeds the footer CRC-16 from the two
// header-warmup bytes, parses the frame header (in.HeaderWarmup must
// carry the two sync bytes already consumed during frame_sync_), loops
// the channels invoking ReadSubframe with the per-channel bps (the side
// channel of a decorrelated stereo pair gets +1), reads the zero padding,
// verifies the footer CRC, and — when fullDecode — undoes the channel
// coding so state.Output holds the final per-channel samples.
//
// The caller must size state.Output to in's channel count with each
// channel buffer >= the parsed blocksize, and state.Side >= blocksize
// (only touched for 32-bit streams or wasted-bits-to-33 subframes). Since
// the blocksize is only known after the header parse, callers that don't
// know it up front should size to MaxBlockSize.
//
// Returns the parsed header alongside the status. On FrameOK the footer
// CRC matched (libFLAC's frame_crc == x gate). FrameLostSync covers the
// header-resync case, any subframe lost-sync/unparseable, and a footer
// CRC mismatch. nextFixedBlockSize forwards ReadFrameHeader's value.
func ReadFrame(
	br *BitReader,
	state *FrameDecodeState,
	in ReadFrameHeaderInput,
	fullDecode bool,
) (h FrameHeader, nextFixedBlockSize uint32, status FrameStatus) {
	state.SideInUse = false

	// Init the CRC: seed with the two header-warmup bytes
	// (stream_decoder.c:2384–2387).
	br.ResetReadCRC16(CRC16Seed(in.HeaderWarmup[0], in.HeaderWarmup[1]))

	h, nextFixedBlockSize, hs := ReadFrameHeader(br, in)
	switch hs {
	case FrameHeaderOK:
		// fall through
	case FrameHeaderReadError:
		return h, nextFixedBlockSize, FrameReadError
	case FrameHeaderBadHeader:
		return h, nextFixedBlockSize, FrameBadHeader
	default: // FrameHeaderUnparseable
		return h, nextFixedBlockSize, FrameLostSync
	}

	for channel := uint32(0); channel < h.Channels; channel++ {
		// Figure the correct per-channel bits-per-sample
		// (stream_decoder.c:2396–2421).
		bps := h.BitsPerSample
		switch h.ChannelAssignment {
		case ChannelAssignmentIndependent:
			// no adjustment needed
		case ChannelAssignmentLeftSide:
			if channel == 1 {
				bps++
			}
		case ChannelAssignmentRightSide:
			if channel == 0 {
				bps++
			}
		case ChannelAssignmentMidSide:
			if channel == 1 {
				bps++
			}
		}

		st := ReadSubframe(br, state, channel, bps, h.Blocksize, fullDecode)
		if st == FrameReadError {
			return h, nextFixedBlockSize, FrameReadError
		}
		if st != FrameOK {
			// Subframe lost sync — libFLAC drops to
			// SEARCH_FOR_FRAME_SYNC and stops processing the frame.
			return h, nextFixedBlockSize, st
		}
	}

	// read_zero_padding_ (stream_decoder.c:2437).
	if zs := ReadZeroPadding(br); zs != SubframeOK {
		if zs == SubframeReadError {
			return h, nextFixedBlockSize, FrameReadError
		}
		return h, nextFixedBlockSize, FrameLostSync
	}

	// Footer CRC-16 (stream_decoder.c:2440–2452). The CRC tracker has
	// been folding every consumed byte since ResetReadCRC16; the snapshot
	// is the expected value, the 16-bit footer must match.
	match, ok := ReadFrameFooterCRC(br)
	if !ok {
		return h, nextFixedBlockSize, FrameReadError
	}
	if !match {
		// FRAME_CRC_MISMATCH → SEARCH_FOR_FRAME_SYNC.
		return h, nextFixedBlockSize, FrameLostSync
	}

	if fullDecode {
		// Undo any special channel coding (stream_decoder.c:2456).
		UndoChannelCoding(
			h.ChannelAssignment,
			pickChannel(state.Output, 0),
			pickChannel(state.Output, 1),
			state.Side,
			state.SideInUse,
			h.Blocksize,
		)

		// Check whether decoded data actually fits bps
		// (stream_decoder.c:2457–2473). On any out-of-range sample libFLAC
		// sends OUT_OF_BOUNDS and drops to SEARCH_FOR_FRAME_SYNC, so the
		// frame is neither written nor folded into MD5.
		for channel := uint32(0); channel < h.Channels; channel++ {
			// int shift_bits = 32 - bits_per_sample;
			// int lower_limit = INT32_MIN >> shift_bits;
			// int upper_limit = INT32_MAX >> shift_bits;
			shiftBits := 32 - int32(h.BitsPerSample)
			lowerLimit := int32(math.MinInt32) >> shiftBits
			upperLimit := int32(math.MaxInt32) >> shiftBits
			out := state.Output[channel]
			for i := uint32(0); i < h.Blocksize; i++ {
				if out[i] < lowerLimit || out[i] > upperLimit {
					return h, nextFixedBlockSize, FrameOutOfBounds
				}
			}
		}
	}

	return h, nextFixedBlockSize, FrameOK
}

// pickChannel returns output[i] or nil when the index is out of range
// (mono streams have no channel 1). UndoChannelCoding only touches
// channels 0/1 for the stereo decorrelation modes, all of which require
// channels == 2, so the nil is never dereferenced for valid streams.
func pickChannel(output [][]int32, i int) []int32 {
	if i < len(output) {
		return output[i]
	}
	return nil
}
