package nativeflac

import "io"

// Decoder state enum + the orchestration struct, ported 1:1 from
// stream_decoder.c / protected/stream_decoder.h. This is the decoder's
// state machine scaffolding: the public stream_decoder.c entry points
// (process_single, process_until_end_of_metadata, …) drive these states
// through find_metadata_/read_metadata_/frame_sync_/read_frame_.
//
// The Go port narrows the C struct to the decode-only fields the native
// path needs. libFLAC threads the whole FLAC__StreamDecoder through every
// helper; we keep the equivalent fields on Decoder and pass the *BitReader
// + small slices the ported readers (frame.go, subframe.go,
// metadata_decode.go, decode_frame.go) already expect. The C callback
// contract (read/seek/tell/eof) collapses to a single io.Reader the
// BitReader's read callback drains; seeking is out of scope for the
// streaming decode path mirrored here (no seek_callback wiring).

// DecoderState — port of FLAC__StreamDecoderState
// (stream_decoder.h:202). Values match the C enum ordinals so a reader
// can diff against the reference directly.
type DecoderState uint8

const (
	// DecoderSearchForMetadata — ready to search for metadata
	// (FLAC__STREAM_DECODER_SEARCH_FOR_METADATA = 0).
	DecoderSearchForMetadata DecoderState = iota
	// DecoderReadMetadata — ready to or reading metadata.
	DecoderReadMetadata
	// DecoderSearchForFrameSync — ready to or searching for frame sync.
	DecoderSearchForFrameSync
	// DecoderReadFrame — ready to or reading a frame.
	DecoderReadFrame
	// DecoderEndOfStream — reached the end of the stream.
	DecoderEndOfStream
	// DecoderOggError — Ogg layer error (unused on the native path).
	DecoderOggError
	// DecoderSeekError — seek error (unused on the native path).
	DecoderSeekError
	// DecoderAborted — aborted by a read or write callback.
	DecoderAborted
	// DecoderMemoryAllocationError — allocation failure.
	DecoderMemoryAllocationError
	// DecoderUninitialized — needs an init_* call before use.
	DecoderUninitialized
	// DecoderEndOfLink — Ogg chain link boundary (unused here).
	DecoderEndOfLink
)

// DecoderErrorStatus — port of FLAC__StreamDecoderErrorStatus
// (stream_decoder.h:444). Passed to the error callback by
// send_error_to_client_.
type DecoderErrorStatus uint8

const (
	DecoderErrorLostSync          DecoderErrorStatus = iota // FLAC__STREAM_DECODER_ERROR_STATUS_LOST_SYNC
	DecoderErrorBadHeader                                   // _BAD_HEADER
	DecoderErrorFrameCRCMismatch                            // _FRAME_CRC_MISMATCH
	DecoderErrorUnparseableStream                           // _UNPARSEABLE_STREAM
	DecoderErrorBadMetadata                                 // _BAD_METADATA
	DecoderErrorOutOfBounds                                 // _OUT_OF_BOUNDS
	DecoderErrorMissingFrame                                // _MISSING_FRAME
)

// DecoderWriteStatus — port of FLAC__StreamDecoderWriteStatus
// (stream_decoder.h:407). Returned by the write callback.
type DecoderWriteStatus uint8

const (
	// DecoderWriteContinue — the write was OK; decoding continues
	// (FLAC__STREAM_DECODER_WRITE_STATUS_CONTINUE).
	DecoderWriteContinue DecoderWriteStatus = iota
	// DecoderWriteAbort — unrecoverable; the process call returns.
	DecoderWriteAbort
)

// DecoderWriteCallback is the Go counterpart of the libFLAC write
// callback (the `write_callback` member of FLAC__StreamDecoder). libFLAC
// hands the decoded FLAC__Frame plus a per-channel int32 buffer array;
// the Go port mirrors that — header is the parsed frame header, buffer is
// one int32 slice per channel (each header.Blocksize long). Returning
// DecoderWriteAbort aborts the decode (write_audio_frame_to_client_'s
// !=CONTINUE path, stream_decoder.c:2613).
type DecoderWriteCallback func(header *FrameHeader, buffer [][]int32) DecoderWriteStatus

// DecoderErrorCallback is the Go counterpart of the libFLAC error
// callback (`error_callback`), invoked by send_error_to_client_
// (stream_decoder.c:3638) on a lost-sync / bad-header / CRC-mismatch /
// unparseable / bad-metadata condition. May be nil.
type DecoderErrorCallback func(status DecoderErrorStatus)

// Decoder is the native counterpart of FLAC__StreamDecoder, narrowed to
// the streaming decode path. It owns the BitReader, the decode buffers
// (FrameDecodeState), the running MD5 context, and the framesync /
// metadata bookkeeping fields the ported helpers read and write.
//
// Concurrency: a Decoder is single-goroutine; callers drive it through
// ProcessSingle / ProcessUntilEndOfMetadata. No context.Context is
// stored (the read path is a plain io.Reader).
type Decoder struct {
	state DecoderState

	br *BitReader

	// write / error callbacks (libFLAC's write_callback / error_callback).
	writeCallback DecoderWriteCallback
	errorCallback DecoderErrorCallback

	// find_metadata_ / frame_sync_ bookkeeping. These mirror the
	// decoder->private_ fields the standalone ported helpers take as a
	// FindMetadataState: the doubled-0xFF lookahead cache and the two
	// frame-header warmup bytes.
	cached       bool
	lookahead    byte
	headerWarmup [2]byte

	// STREAMINFO + md5 bookkeeping (stream_decoder.c reset_decoder_internal_).
	hasStreamInfo bool
	streamInfo    StreamInfo
	doMD5Checking bool
	md5Checking   bool // the protected_->md5_checking config flag
	md5Context    MD5Context
	computedMD5   [16]byte

	// fixed-block-size tracking for FRAME_NUMBER → SAMPLE_NUMBER conversion
	// (decoder->private_->fixed_block_size / next_fixed_block_size).
	fixedBlockSize     uint32
	nextFixedBlockSize uint32

	// error_has_been_sent mirrors decoder->private_->error_has_been_sent;
	// send_error_to_client_ sets it, read_frame_ clears it each frame.
	errorHasBeenSent bool

	// decode buffers (decoder->private_->output / side_subframe).
	frame FrameDecodeState

	// clientBuf is a reusable [][]int32 handed to the write callback each
	// frame (sliced to h.Channels × h.Blocksize), avoiding a per-frame
	// make([][]int32). Backing array is allocated lazily at MaxChannels.
	clientBuf [][]int32

	// samplesDecoded mirrors decoder->private_->samples_decoded.
	samplesDecoded uint64
}

// NewDecoder allocates a Decoder with a fresh BitReader, mirroring
// FLAC__stream_decoder_new (stream_decoder.c). The decoder starts
// uninitialized; call InitStream before processing.
func NewDecoder() *Decoder {
	return &Decoder{
		state: DecoderUninitialized,
		br:    NewBitReader(),
	}
}

// InitStream — port of FLAC__stream_decoder_init_stream
// (stream_decoder.c:433), scoped to the native decode path. Wires the
// io.Reader into the BitReader's read callback, stores the write / error
// callbacks, runs the reset_decoder_internal_ initialization, and lands
// the decoder in DecoderSearchForMetadata ready for the first
// ProcessSingle.
//
// libFLAC's init_stream takes read/seek/tell/length/eof/write/metadata/
// error callbacks; the streaming native path needs only the data source
// (r), the write sink, and the error sink. EOF is detected when the
// io.Reader returns io.EOF with zero bytes — the BitReader read callback
// reports n==0, ok==true, which the ported readers treat as a starve →
// the state machine maps to end-of-stream.
//
// md5Checking selects whether the decoder accumulates and verifies the
// stream MD5 (FLAC__stream_decoder_set_md5_checking).
func (d *Decoder) InitStream(r io.Reader, write DecoderWriteCallback, onError DecoderErrorCallback, md5Checking bool) DecoderState {
	d.writeCallback = write
	d.errorCallback = onError
	d.md5Checking = md5Checking

	// FLAC__bitreader_init(decoder->private_->input, read_callback_, decoder)
	// (stream_decoder.c:401). The Go read callback mirrors read_callback_
	// (stream_decoder.c:3376), which is the function libFLAC hands the
	// bitreader as br->read_callback. Its contract is what
	// bitreader_read_from_client_ relies on to terminate its top-up loop:
	// on a successful read of >0 bytes it returns true; at end-of-stream
	// (*bytes == 0 with an END_OF_STREAM read status) it sets the decoder
	// state to END_OF_STREAM and returns FALSE (stream_decoder.c:3442–3443),
	// so the bitreader's ensureBits loop stops instead of spinning forever.
	d.br.Init(func(buf []byte) (uint, bool) {
		n, err := r.Read(buf)
		if n > 0 {
			// *bytes > 0: a normal CONTINUE read (read_callback_ returns
			// true). Any EOF reported alongside n>0 surfaces on the next
			// call when Read yields n==0.
			return uint(n), true
		}
		// *bytes == 0: end-of-stream / starve. read_callback_ sets state to
		// END_OF_STREAM and returns false; the bitreader propagates that as
		// a starve and the ported readers route it to DecoderEndOfStream via
		// their !ok branches. Returning false here (not true) is what breaks
		// bitreader_read_from_client_'s otherwise-infinite top-up loop.
		_ = err
		return 0, false
	})

	// reset_decoder_internal_ (stream_decoder.c:959).
	d.resetInternal()
	return d.state
}

// resetInternal — port of reset_decoder_internal_ (stream_decoder.c:959),
// trimmed to the native decode fields (no Ogg, no seek table).
func (d *Decoder) resetInternal() {
	d.state = DecoderSearchForMetadata
	d.hasStreamInfo = false
	d.doMD5Checking = d.md5Checking
	// A fixed-blocksize stream must stay fixed through the whole stream,
	// so this lives in reset, not flush (stream_decoder.c:973–976).
	d.fixedBlockSize = 0
	d.nextFixedBlockSize = 0

	// libFLAC finalizes any in-flight context then re-inits; crypto/md5
	// has no separate finalize cost, so we just Init a fresh context.
	d.md5Context.Init()

	d.unsetFrameSyncBookkeeping()
	d.samplesDecoded = 0
	d.errorHasBeenSent = false
}

// unsetFrameSyncBookkeeping clears the find_metadata_/frame_sync_
// lookahead cache. (reset_decoder_internal_ does not touch cached /
// lookahead directly — they are cleared by FLAC__stream_decoder_flush
// via the bitreader — but the native path keeps them on the Decoder so
// we reset them alongside the rest of the per-stream state.)
func (d *Decoder) unsetFrameSyncBookkeeping() {
	d.cached = false
	d.lookahead = 0
	d.headerWarmup = [2]byte{}
}

// State returns the decoder's current state (FLAC__stream_decoder_get_state).
func (d *Decoder) State() DecoderState { return d.state }

// StreamInfo returns the parsed STREAMINFO and whether one was seen.
func (d *Decoder) StreamInfo() (StreamInfo, bool) { return d.streamInfo, d.hasStreamInfo }
