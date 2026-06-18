package nativeflac

// Decoder metadata path — 1:1 port of the metadata-reading slice of
// libflac/src/libFLAC/stream_decoder.c (find_metadata_,
// skip_id3v2_tag_, read_metadata_ dispatch, read_metadata_streaminfo_,
// plus the length-skip path every non-STREAMINFO block takes during a
// plain decode).
//
// libFLAC threads its FLAC__StreamDecoder state struct through every
// reader; the Go port follows the same free-function shape used by
// frame.go / subframe.go: each entry point takes the *BitReader plus
// the small pieces of decoder state it actually reads or writes
// (the cached/lookahead framesync bytes and the header warm-up). The
// surrounding I/O state machine (5.7d–h) will own those fields and
// pass them in.
//
// Decode only parses STREAMINFO; PADDING / APPLICATION / SEEKTABLE /
// VORBIS_COMMENT / CUESHEET / PICTURE / unknown blocks are
// length-skipped here, but the block header (is-last flag, type,
// length) is read with the exact same bit widths and order libFLAC
// uses, so the post-read stream position matches byte-for-byte.

// Metadata block header field widths, mirroring
// FLAC__STREAM_METADATA_IS_LAST_LEN / _TYPE_LEN / _LENGTH_LEN
// (format.h:867–869) and FLAC__STREAM_METADATA_APPLICATION_ID_LEN
// (format.h:577).
const (
	StreamMetadataIsLastLen = 1
	StreamMetadataTypeLen   = 7
	StreamMetadataLengthLen = 24

	StreamMetadataApplicationIDLen = 32 // bits
)

// MetadataType — port of FLAC__MetadataType (format.h:496).
type MetadataType uint8

const (
	MetadataTypeStreamInfo    MetadataType = 0
	MetadataTypePadding       MetadataType = 1
	MetadataTypeApplication   MetadataType = 2
	MetadataTypeSeekTable     MetadataType = 3
	MetadataTypeVorbisComment MetadataType = 4
	MetadataTypeCuesheet      MetadataType = 5
	MetadataTypePicture       MetadataType = 6
	MetadataTypeUndefined     MetadataType = 7
)

// StreamInfo — port of FLAC__StreamMetadata_StreamInfo (format.h:535).
// Field semantics match libFLAC exactly: channels and bits_per_sample
// are the raw-field value plus one, as read_metadata_streaminfo_
// stores them.
type StreamInfo struct {
	MinBlockSize  uint32
	MaxBlockSize  uint32
	MinFrameSize  uint32
	MaxFrameSize  uint32
	SampleRate    uint32
	Channels      uint32
	BitsPerSample uint32
	TotalSamples  uint64
	MD5Sum        [16]byte
}

// MetadataBlockHeader — the common is-last / type / length triple at
// the head of every metadata block (read_metadata_, stream_decoder.c:1726).
type MetadataBlockHeader struct {
	IsLast bool
	Type   MetadataType
	Length uint32
}

// FindMetadataStatus is the outcome of FindMetadata.
type FindMetadataStatus uint8

const (
	// FindMetadataReadMetadata — "fLaC" marker consumed; the caller's
	// state machine should advance to READ_METADATA.
	FindMetadataReadMetadata FindMetadataStatus = iota
	// FindMetadataReadFrame — a frame sync was seen instead of a
	// metadata marker (a headerless stream); advance to READ_FRAME.
	// header_warmup[0..1] in the returned FindMetadataState hold the
	// two sync bytes already consumed.
	FindMetadataReadFrame
	// FindMetadataReadError — the read callback starved.
	FindMetadataReadError
)

// FindMetadataState carries the decoder fields find_metadata_ reads
// and writes: the one-byte lookahead cache (used when a doubled 0xFF
// forces the second byte to be re-examined) and the two-byte frame
// header warm-up (set when a frame sync is found before any "fLaC"
// marker). On entry, Cached / Lookahead reflect any byte stashed by a
// prior call; on return they reflect the new cache state.
type FindMetadataState struct {
	Cached       bool
	Lookahead    byte
	HeaderWarmup [2]byte
	// LostSync is set true the first time the scan emits a
	// LOST_SYNC error (libFLAC's send_error_to_client_ at
	// stream_decoder.c:1710). The caller forwards it to its error
	// callback. Multiple lost-sync runs in one call still report once.
	LostSync bool
}

// id3v2Tag mirrors ID3V2_TAG_ (stream_decoder.c:68): the three bytes
// "ID3" that, when seen at stream start, trigger an ID3v2 tag skip.
var id3v2Tag = [3]byte{'I', 'D', '3'}

// streamSyncString mirrors FLAC__STREAM_SYNC_STRING (format.c) — the
// "fLaC" marker.
var streamSyncString = [4]byte{'f', 'L', 'a', 'C'}

// FindMetadata — port of find_metadata_ (stream_decoder.c:1654).
//
// Scans the byte-aligned input for the "fLaC" marker, transparently
// skipping a leading ID3v2 tag, and detects the case where the stream
// begins directly with a frame sync (a headerless FLAC stream). The
// caller must be byte aligned (libFLAC asserts).
//
// Returns FindMetadataReadMetadata once "fLaC" is consumed,
// FindMetadataReadFrame if a frame sync (0xFF followed by a byte whose
// top 7 bits are 0xFE, i.e. x>>1 == 0x7C) is found first, or
// FindMetadataReadError on a starved reader. The doubled-0xFF
// look-ahead and the two header warm-up bytes are returned in st so
// the caller's frame_sync_/read_frame_ can resume exactly where
// libFLAC would.
func FindMetadata(br *BitReader, st *FindMetadataState) FindMetadataStatus {
	first := true
	var x uint32
	var i, id uint32

	for i = 0; i < 4; {
		if st.Cached {
			x = uint32(st.Lookahead)
			st.Cached = false
		} else {
			v, ok := br.ReadRawUint32(8)
			if !ok {
				return FindMetadataReadError
			}
			x = v
		}

		if x == uint32(streamSyncString[i]) {
			first = true
			i++
			id = 0
			continue
		}

		if id >= 3 {
			// libFLAC returns false here (not a valid FLAC stream and
			// not an ID3-tagged one). The caller treats it as an error.
			return FindMetadataReadError
		}

		if x == uint32(id3v2Tag[id]) {
			id++
			i = 0
			if id == 3 {
				if !skipID3v2Tag(br) {
					return FindMetadataReadError
				}
			}
			continue
		}
		id = 0
		if x == 0xFF { // first 8 frame sync bits
			st.HeaderWarmup[0] = byte(x)
			v, ok := br.ReadRawUint32(8)
			if !ok {
				return FindMetadataReadError
			}
			x = v
			// Check for two 0xFF's in a row; the second may be the start
			// of the sync code. Otherwise check whether the second byte
			// completes a sync code.
			if x == 0xFF {
				st.Lookahead = byte(x)
				st.Cached = true
			} else if x>>1 == 0x7C { // last 6 sync bits + reserved 7th bit
				st.HeaderWarmup[1] = byte(x)
				return FindMetadataReadFrame
			}
		}
		i = 0
		if first {
			st.LostSync = true
			first = false
		}
	}

	return FindMetadataReadMetadata
}

// skipID3v2Tag — port of skip_id3v2_tag_ (stream_decoder.c:2299).
// Called once the "ID3" magic has been consumed: reads the 3-byte
// version+flags field, then the 4-byte syncsafe size (7 bits per
// byte), and length-skips that many bytes.
func skipID3v2Tag(br *BitReader) bool {
	// skip the version and flags bytes
	if _, ok := br.ReadRawUint32(24); !ok {
		return false
	}
	// get the size (in bytes) to skip
	var skip uint32
	for i := 0; i < 4; i++ {
		x, ok := br.ReadRawUint32(8)
		if !ok {
			return false
		}
		skip <<= 7
		skip |= x & 0x7F
	}
	// skip the rest of the tag
	return br.SkipByteBlockAlignedNoCRC(skip)
}

// ReadMetadataStatus is the outcome of ReadMetadata.
type ReadMetadataStatus uint8

const (
	ReadMetadataOK ReadMetadataStatus = iota
	ReadMetadataReadError
	// ReadMetadataMemoryAllocationError mirrors libFLAC setting
	// FLAC__STREAM_DECODER_MEMORY_ALLOCATION_ERROR on an APPLICATION-block
	// id-length underflow (stream_decoder.c:1773–1776): the declared length
	// is shorter than the 4-byte application id it must contain. This is a
	// distinct terminal status, NOT the read-error/END_OF_STREAM path.
	ReadMetadataMemoryAllocationError
	// ReadMetadataBadMetadata mirrors libFLAC sending
	// FLAC__STREAM_DECODER_ERROR_STATUS_BAD_METADATA and dropping to
	// SEARCH_FOR_FRAME_SYNC: the block body did not fit its declared
	// length. Only reachable on the non-skipped (parsed) blocks, which
	// during a plain decode means STREAMINFO; the skip path can't
	// trigger it.
	ReadMetadataBadMetadata
)

// ReadMetadataResult collects everything ReadMetadata parsed for the
// caller's state machine.
type ReadMetadataResult struct {
	Header MetadataBlockHeader
	// HasStreamInfo is true when the block was STREAMINFO; StreamInfo
	// then holds the parsed fields. read_metadata_ also clears
	// do_md5_checking when the md5sum is all-zero (stream_decoder.c:1741);
	// MD5IsZero surfaces that so the caller can mirror the behaviour.
	HasStreamInfo bool
	StreamInfo    StreamInfo
	MD5IsZero     bool
}

// ReadMetadata — port of read_metadata_ (stream_decoder.c:1719),
// scoped to what a plain decode needs. Reads the block header, then
// either fully parses STREAMINFO or length-skips the block body.
//
// The caller must be byte aligned (libFLAC asserts). For decode-only
// use, every non-STREAMINFO block is treated as "skip_it" — libFLAC
// only parses APPLICATION/VORBIS_COMMENT/CUESHEET/PICTURE/SEEKTABLE
// when a metadata callback or filter is registered, which the
// decode-only path does not set. The block header bits and the
// resulting stream position match libFLAC byte-for-byte regardless.
//
// res.Header.IsLast reports the last-block flag so the caller can
// advance to SEARCH_FOR_FRAME_SYNC.
func ReadMetadata(br *BitReader, res *ReadMetadataResult) ReadMetadataStatus {
	x, ok := br.ReadRawUint32(StreamMetadataIsLastLen)
	if !ok {
		return ReadMetadataReadError
	}
	isLast := x != 0

	typ, ok := br.ReadRawUint32(StreamMetadataTypeLen)
	if !ok {
		return ReadMetadataReadError
	}

	length, ok := br.ReadRawUint32(StreamMetadataLengthLen)
	if !ok {
		return ReadMetadataReadError
	}

	res.Header = MetadataBlockHeader{IsLast: isLast, Type: MetadataType(typ), Length: length}

	if MetadataType(typ) == MetadataTypeStreamInfo {
		si, st := readMetadataStreamInfo(br, isLast, length)
		if st != ReadMetadataOK {
			return st
		}
		res.HasStreamInfo = true
		res.StreamInfo = si
		// stream_decoder.c:1741 — an all-zero md5sum disables checking.
		res.MD5IsZero = si.MD5Sum == [16]byte{}
		return ReadMetadataOK
	}

	// Every other block type is length-skipped during a plain decode.
	//
	// libFLAC reads the 4-byte APPLICATION id before skipping the
	// remainder (stream_decoder.c:1769–1786), so even though we discard
	// it, the consumed-byte count must match: skip the id, then the
	// real_length remainder. SEEKTABLE/PADDING/VORBIS_COMMENT/CUESHEET/
	// PICTURE/unknown all just skip `length` bytes.
	if MetadataType(typ) == MetadataTypeApplication {
		var idbuf [StreamMetadataApplicationIDLen / 8]byte
		if !br.ReadByteBlockAlignedNoCRC(idbuf[:]) {
			return ReadMetadataReadError
		}
		if length < StreamMetadataApplicationIDLen/8 { // underflow check
			// stream_decoder.c:1773–1776 — libFLAC sets
			// MEMORY_ALLOCATION_ERROR and returns false. Surface a distinct
			// status so the caller maps it to that state rather than
			// collapsing it into the read-error/END_OF_STREAM path.
			return ReadMetadataMemoryAllocationError
		}
		length -= StreamMetadataApplicationIDLen / 8
	}

	if !br.SkipByteBlockAlignedNoCRC(length) {
		return ReadMetadataReadError
	}

	return ReadMetadataOK
}

// readMetadataStreamInfo — port of read_metadata_streaminfo_
// (stream_decoder.c:1910). Parses the 34-byte STREAMINFO body, then
// length-skips any trailing bytes the declared length carries beyond
// the fixed layout (a forward-compatibility allowance libFLAC honours).
//
// Caller must be byte aligned. channels and bits_per_sample are stored
// as field+1, matching libFLAC.
func readMetadataStreamInfo(br *BitReader, isLast bool, length uint32) (StreamInfo, ReadMetadataStatus) {
	var si StreamInfo
	var usedBits uint32

	x, ok := br.ReadRawUint32(StreamInfoMinBlockSizeLen)
	if !ok {
		return si, ReadMetadataReadError
	}
	si.MinBlockSize = x
	usedBits += StreamInfoMinBlockSizeLen

	x, ok = br.ReadRawUint32(StreamInfoMaxBlockSizeLen)
	if !ok {
		return si, ReadMetadataReadError
	}
	si.MaxBlockSize = x
	usedBits += StreamInfoMaxBlockSizeLen

	x, ok = br.ReadRawUint32(StreamInfoMinFrameSizeLen)
	if !ok {
		return si, ReadMetadataReadError
	}
	si.MinFrameSize = x
	usedBits += StreamInfoMinFrameSizeLen

	x, ok = br.ReadRawUint32(StreamInfoMaxFrameSizeLen)
	if !ok {
		return si, ReadMetadataReadError
	}
	si.MaxFrameSize = x
	usedBits += StreamInfoMaxFrameSizeLen

	x, ok = br.ReadRawUint32(StreamInfoSampleRateLen)
	if !ok {
		return si, ReadMetadataReadError
	}
	si.SampleRate = x
	usedBits += StreamInfoSampleRateLen

	x, ok = br.ReadRawUint32(StreamInfoChannelsLen)
	if !ok {
		return si, ReadMetadataReadError
	}
	si.Channels = x + 1
	usedBits += StreamInfoChannelsLen

	x, ok = br.ReadRawUint32(StreamInfoBitsPerSampleLen)
	if !ok {
		return si, ReadMetadataReadError
	}
	si.BitsPerSample = x + 1
	usedBits += StreamInfoBitsPerSampleLen

	tx, ok := br.ReadRawUint64(StreamInfoTotalSamplesLen)
	if !ok {
		return si, ReadMetadataReadError
	}
	si.TotalSamples = tx
	usedBits += StreamInfoTotalSamplesLen

	if !br.ReadByteBlockAlignedNoCRC(si.MD5Sum[:]) {
		return si, ReadMetadataReadError
	}
	usedBits += 16 * 8

	// skip the rest of the block (used_bits is a whole number of bytes)
	if length < usedBits/8 {
		return si, ReadMetadataReadError
	}
	length -= usedBits / 8
	if !br.SkipByteBlockAlignedNoCRC(length) {
		return si, ReadMetadataReadError
	}

	return si, ReadMetadataOK
}
