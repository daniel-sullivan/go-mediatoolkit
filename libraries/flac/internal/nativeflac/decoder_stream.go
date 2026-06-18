package nativeflac

// Decoder driver loop + state-machine transitions, ported 1:1 from
// stream_decoder.c: the process_single / process_until_end_of_metadata
// dispatch loops, the find_metadata_ / read_metadata_ / frame_sync_ /
// read_frame_ state transitions, the write-callback plumbing, and the
// running MD5 of decoded samples with the end-of-stream MD5 check.
//
// The standalone readers already ported (FindMetadata in
// metadata_decode.go, ReadMetadata in metadata_decode.go, ReadFrameHeader
// in frame.go, ReadFrame in decode_frame.go) carry the bit-level parse;
// the functions here own the surrounding state machine that libFLAC keeps
// in decoder->protected_->state, exactly mirroring the C control flow,
// magic numbers, and transition order.

// sendError — port of send_error_to_client_ (stream_decoder.c:3638),
// scoped to the non-seeking path. Sets error_has_been_sent and invokes
// the error callback if one is registered.
func (d *Decoder) sendError(status DecoderErrorStatus) {
	d.errorHasBeenSent = true
	if d.errorCallback != nil {
		d.errorCallback(status)
	}
}

// ProcessSingle — port of FLAC__stream_decoder_process_single
// (stream_decoder.c:1034). Runs the state machine until it has either
// emitted one audio frame (via the write callback) or reached a terminal
// state. Returns false only on an unrecoverable error (the state field
// carries the reason, matching the C "above function sets the status for
// us" convention).
func (d *Decoder) ProcessSingle() bool {
	for {
		switch d.state {
		case DecoderSearchForMetadata:
			if !d.findMetadata() {
				return false
			}
		case DecoderReadMetadata:
			if !d.readMetadata() {
				return false
			}
			return true
		case DecoderSearchForFrameSync:
			if !d.frameSync() {
				return true
			}
		case DecoderReadFrame:
			gotFrame, ok := d.readFrame(true)
			if !ok {
				return false
			}
			if gotFrame {
				return true
			}
		case DecoderEndOfStream, DecoderEndOfLink, DecoderAborted:
			return true
		default:
			return false
		}
	}
}

// ProcessUntilEndOfMetadata — port of
// FLAC__stream_decoder_process_until_end_of_metadata
// (stream_decoder.c:1072). Drives the machine through SEARCH_FOR_METADATA
// / READ_METADATA until it would advance to SEARCH_FOR_FRAME_SYNC (or hits
// a terminal state), then returns true. The STREAMINFO and any other
// metadata have been parsed/length-skipped at that point.
func (d *Decoder) ProcessUntilEndOfMetadata() bool {
	for {
		switch d.state {
		case DecoderSearchForMetadata:
			if !d.findMetadata() {
				return false
			}
		case DecoderReadMetadata:
			if !d.readMetadata() {
				return false
			}
		case DecoderSearchForFrameSync, DecoderReadFrame,
			DecoderEndOfStream, DecoderEndOfLink, DecoderAborted:
			return true
		default:
			return false
		}
	}
}

// ProcessUntilEndOfStream — port of
// FLAC__stream_decoder_process_until_end_of_stream
// (stream_decoder.c:1164), scoped to the native (non-Ogg) path. Runs the
// whole machine to completion, emitting every frame through the write
// callback. On clean end-of-stream returns true; on an unrecoverable
// error returns false with the reason in state.
func (d *Decoder) ProcessUntilEndOfStream() bool {
	for {
		switch d.state {
		case DecoderSearchForMetadata:
			if !d.findMetadata() {
				return false
			}
		case DecoderReadMetadata:
			if !d.readMetadata() {
				return false
			}
		case DecoderSearchForFrameSync:
			if !d.frameSync() && d.state != DecoderMemoryAllocationError {
				return true
			}
		case DecoderReadFrame:
			if _, ok := d.readFrame(true); !ok {
				return false
			}
		case DecoderEndOfStream, DecoderAborted:
			return true
		default:
			return false
		}
	}
}

// findMetadata — port of find_metadata_ (stream_decoder.c:1654). Wraps
// the standalone FindMetadata scanner with the decoder's state
// transitions. On a "fLaC" marker advances to READ_METADATA; on a frame
// sync seen first advances to READ_FRAME (a headerless stream); on a
// starved reader sets END_OF_STREAM and returns false.
func (d *Decoder) findMetadata() bool {
	st := FindMetadataState{
		Cached:       d.cached,
		Lookahead:    d.lookahead,
		HeaderWarmup: d.headerWarmup,
	}
	status := FindMetadata(d.br, &st)

	// Forward any lost-sync run the scanner observed (FindMetadata folds
	// libFLAC's per-call "first" LOST_SYNC report into st.LostSync).
	if st.LostSync {
		d.sendError(DecoderErrorLostSync)
	}

	// Mirror find_metadata_'s state writes back onto the decoder.
	d.cached = st.Cached
	d.lookahead = st.Lookahead
	d.headerWarmup = st.HeaderWarmup

	switch status {
	case FindMetadataReadMetadata:
		d.state = DecoderReadMetadata
		return true
	case FindMetadataReadFrame:
		d.state = DecoderReadFrame
		return true
	default: // FindMetadataReadError — read_callback_ sets the state for us.
		d.state = DecoderEndOfStream
		return false
	}
}

// readMetadata — port of read_metadata_ (stream_decoder.c:1719), scoped
// to the decode-only path (every non-STREAMINFO block is length-skipped).
// Wraps the standalone ReadMetadata parser, applies the STREAMINFO side
// effects (has_stream_info, the all-zero-md5sum → disable-md5 rule), and
// advances to SEARCH_FOR_FRAME_SYNC on the last block.
func (d *Decoder) readMetadata() bool {
	var res ReadMetadataResult
	switch ReadMetadata(d.br, &res) {
	case ReadMetadataOK:
		// fall through
	case ReadMetadataReadError:
		// read_callback_ sets the state for us.
		d.state = DecoderEndOfStream
		return false
	case ReadMetadataMemoryAllocationError:
		// APPLICATION-block id-length underflow: libFLAC sets
		// MEMORY_ALLOCATION_ERROR and returns false (stream_decoder.c:
		// 1773–1776). No error callback, distinct terminal state.
		d.state = DecoderMemoryAllocationError
		return false
	default: // ReadMetadataBadMetadata
		// libFLAC sends BAD_METADATA and drops to SEARCH_FOR_FRAME_SYNC
		// (stream_decoder.c:1843–1846). Only reachable on parsed blocks,
		// which for the decode-only path means STREAMINFO.
		d.sendError(DecoderErrorBadMetadata)
		d.state = DecoderSearchForFrameSync
		return false
	}

	if res.HasStreamInfo {
		d.hasStreamInfo = true
		d.streamInfo = res.StreamInfo
		if res.MD5IsZero {
			// stream_decoder.c:1741 — an all-zero md5sum disables checking.
			d.doMD5Checking = false
		}
	}

	if res.Header.IsLast {
		d.state = DecoderSearchForFrameSync
	}
	return true
}

// frameSync — port of frame_sync_ (stream_decoder.c:2321). Byte-aligns
// the reader if needed, then scans for the 0xFFF8 / 0xFFF9 frame sync
// code, transparently handling a doubled 0xFF via the lookahead cache.
// On a sync hit stashes the two warmup bytes, records the framesync
// location for a possible rewind, and advances to READ_FRAME (returns
// true). On a starved reader sets END_OF_STREAM and returns false.
func (d *Decoder) frameSync() bool {
	// Make sure we're byte aligned (stream_decoder.c:2327).
	if !d.br.IsConsumedByteAligned() {
		if _, ok := d.br.ReadRawUint32(d.br.BitsLeftForByteAlignment()); !ok {
			d.state = DecoderEndOfStream
			return false
		}
	}

	first := true
	for {
		var x uint32
		if d.cached {
			x = uint32(d.lookahead)
			d.cached = false
		} else {
			v, ok := d.br.ReadRawUint32(8)
			if !ok {
				d.state = DecoderEndOfStream
				return false
			}
			x = v
		}

		if x == 0xFF { // first 8 frame sync bits
			d.headerWarmup[0] = byte(x)
			v, ok := d.br.ReadRawUint32(8)
			if !ok {
				d.state = DecoderEndOfStream
				return false
			}
			x = v
			// Two 0xFF's in a row: the second may begin the sync code.
			// Otherwise check whether it ends a sync code.
			if x == 0xFF {
				d.lookahead = byte(x)
				d.cached = true
			} else if x>>1 == 0x7C { // last 6 sync bits + reserved 7th bit
				d.headerWarmup[1] = byte(x)
				d.state = DecoderReadFrame
				// Save location so read_frame_ can rewind if the frame
				// turns out invalid after the header
				// (stream_decoder.c:2358).
				d.br.SetFramesyncLocation()
				return true
			}
		}
		if first {
			d.sendError(DecoderErrorLostSync)
			first = false
		}
	}
}

// readFrame — port of read_frame_ (stream_decoder.c:2373), scoped to the
// native decode path (no seek, no missing-frame silence repair, no
// out-of-bounds check loop). Drives one frame through the standalone
// ReadFrame reader (decode_frame.go), then on success runs the MD5
// accumulate + write callback (write_audio_frame_to_client_) and advances
// state.
//
// Returns gotFrame=true when a frame was fully decoded and written;
// ok=false only on an unrecoverable error (state carries the reason).
// A lost-sync / bad-header / CRC-mismatch returns gotFrame=false, ok=true
// and lands the decoder back in SEARCH_FOR_FRAME_SYNC, exactly as the C
// does via its state checks.
func (d *Decoder) readFrame(doFullDecode bool) (gotFrame bool, ok bool) {
	// Size the per-channel output + side buffers. The blocksize is only
	// known after the header parse, so size to MaxBlockSize (the decode
	// buffers are reused across frames, like decoder->private_->output).
	d.ensureFrameBuffers()

	in := ReadFrameHeaderInput{
		HeaderWarmup:            d.headerWarmup,
		HasStreamInfo:           d.hasStreamInfo,
		StreamInfoSampleRate:    d.streamInfo.SampleRate,
		StreamInfoBitsPerSample: d.streamInfo.BitsPerSample,
		StreamInfoMinBlockSize:  d.streamInfo.MinBlockSize,
		StreamInfoMaxBlockSize:  d.streamInfo.MaxBlockSize,
		FixedBlockSize:          d.fixedBlockSize,
	}

	h, nextFixedBlockSize, status := ReadFrame(d.br, &d.frame, in, doFullDecode)

	switch status {
	case FrameOK:
		// fall through to the write path below.
	case FrameReadError:
		// read_callback_ sets the state for us (END_OF_STREAM on a clean
		// starve). read_frame_ breaks out and ends up in the rewind path.
		d.state = DecoderEndOfStream
		return false, true
	case FrameBadHeader:
		// read_frame_header_ failed CRC/sync; libFLAC emits BAD_HEADER and
		// the header reader left the state at SEARCH_FOR_FRAME_SYNC
		// (stream_decoder.c:2391–2392). Resync.
		d.sendError(DecoderErrorBadHeader)
		d.finishFrameResync()
		return false, true
	case FrameOutOfBounds:
		// The footer CRC matched but a decoded sample didn't fit bps after
		// undo_channel_coding (stream_decoder.c:2457–2473). libFLAC sends
		// OUT_OF_BOUNDS and drops to SEARCH_FOR_FRAME_SYNC; got_a_frame
		// stays false, so the frame is NOT written and NOT folded into MD5.
		d.sendError(DecoderErrorOutOfBounds)
		d.finishFrameResync()
		return false, true
	default: // FrameLostSync — subframe/header lost sync, unparseable,
		// reserved code, or footer CRC mismatch. libFLAC sends the
		// matching error and drops to SEARCH_FOR_FRAME_SYNC. The native
		// FrameStatus collapses these; report the generic recovery error.
		d.sendError(DecoderErrorFrameCRCMismatch)
		d.finishFrameResync()
		return false, true
	}

	// Got a proper frame. error_has_been_sent reset (stream_decoder.c:2556).
	d.errorHasBeenSent = false

	// We waited until here, with a valid frame and hence a correct
	// blocksize, to commit the fixed_block_size (stream_decoder.c:2598).
	if nextFixedBlockSize != 0 {
		d.fixedBlockSize = nextFixedBlockSize
	}

	d.samplesDecoded = h.Number + uint64(h.Blocksize)

	if doFullDecode {
		if d.writeAudioFrameToClient(&h) != DecoderWriteContinue {
			d.state = DecoderAborted
			return false, false
		}
	}

	d.state = DecoderSearchForFrameSync
	return true, true
}

// finishFrameResync mirrors the tail of read_frame_ that, on a
// SEARCH_FOR_FRAME_SYNC / END_OF_STREAM outcome, rewinds to just after the
// last seen framesync so the next frame_sync_ resumes there
// (stream_decoder.c:2558–2590). The native path has no seek callback, so
// only the in-buffer rewind applies; if the framesync byte has already
// been compacted out of the BitReader buffer the rewind is a no-op (as in
// libFLAC when no seek_callback is wired).
func (d *Decoder) finishFrameResync() {
	d.errorHasBeenSent = false
	d.br.RewindToAfterLastSeenFramesync()
	d.state = DecoderSearchForFrameSync
}

// writeAudioFrameToClient — port of write_audio_frame_to_client_
// (stream_decoder.c:3579), scoped to the non-seeking, non-indexing path.
// Accumulates the decoded samples into the running MD5 (disabling md5 if
// STREAMINFO was never seen) and hands the frame to the write callback.
func (d *Decoder) writeAudioFrameToClient(h *FrameHeader) DecoderWriteStatus {
	// If we never got STREAMINFO, turn off MD5 checking — there's no sum
	// to compare against (stream_decoder.c:3625).
	if !d.hasStreamInfo {
		d.doMD5Checking = false
	}
	if d.doMD5Checking {
		// bytes_per_sample = (bits_per_sample + 7) / 8 (stream_decoder.c:3628).
		bytesPerSample := (h.BitsPerSample + 7) / 8
		if !d.md5Context.Accumulate(d.frame.Output, h.Channels, h.Blocksize, bytesPerSample) {
			return DecoderWriteAbort
		}
	}
	if d.writeCallback != nil {
		// Hand the callback exactly h.Channels buffers, each h.Blocksize
		// long, reusing a Decoder-held header slice across frames.
		if cap(d.clientBuf) < int(h.Channels) {
			d.clientBuf = make([][]int32, MaxChannels)
		}
		buf := d.clientBuf[:h.Channels]
		for ch := uint32(0); ch < h.Channels; ch++ {
			buf[ch] = d.frame.Output[ch][:h.Blocksize]
		}
		return d.writeCallback(h, buf)
	}
	return DecoderWriteContinue
}

// ensureFrameBuffers — port of the allocate_output_ contract
// (stream_decoder.c:1573) for the native path: makes sure the decode
// buffers are sized for MaxChannels × MaxBlockSize. libFLAC reallocates
// per frame when the block size or channel count grows; the native path
// allocates once at the maximum and reuses across frames, matching the
// "reused buffer" semantics ReadFrame expects.
func (d *Decoder) ensureFrameBuffers() {
	// Size to the ACTUAL stream parameters from STREAMINFO rather than the
	// 65535×8 worst-case ceiling: a ~40× allocation cut on a typical
	// 4096-sample stereo stream, with far better cache locality across the
	// per-channel restore loops. Fall back to the ceiling when STREAMINFO
	// is absent (headerless stream) or carries a zero MaxBlockSize. The
	// per-channel/per-buffer len<want grow guards still allow a later,
	// larger frame to expand the buffers in place.
	wantBlock := int(d.streamInfo.MaxBlockSize)
	if !d.hasStreamInfo || wantBlock == 0 {
		wantBlock = MaxBlockSize
	}
	wantChannels := int(d.streamInfo.Channels)
	if !d.hasStreamInfo || wantChannels == 0 || wantChannels > MaxChannels {
		wantChannels = MaxChannels
	}

	if len(d.frame.Output) < wantChannels {
		// The Output header slice is cheap; size it to MaxChannels so a
		// channel-count change never reallocates the header.
		grown := make([][]int32, MaxChannels)
		copy(grown, d.frame.Output)
		d.frame.Output = grown
	}
	for ch := 0; ch < wantChannels; ch++ {
		if len(d.frame.Output[ch]) < wantBlock {
			d.frame.Output[ch] = make([]int32, wantBlock)
		}
	}
	if len(d.frame.Side) < wantBlock {
		d.frame.Side = make([]int64, wantBlock)
	}
}

// Finish — port of the end-of-stream MD5 check in
// FLAC__stream_decoder_finish (stream_decoder.c) / finish_link
// (stream_decoder.c:1134). Finalizes the running MD5 and, when md5
// checking is still enabled, compares it against the STREAMINFO md5sum.
// Returns false on a mismatch (libFLAC's md5_failed), true otherwise
// (including when checking was disabled).
func (d *Decoder) Finish() bool {
	d.computedMD5 = d.md5Context.Final()
	if d.doMD5Checking {
		if d.computedMD5 != d.streamInfo.MD5Sum {
			return false
		}
	}
	return true
}
