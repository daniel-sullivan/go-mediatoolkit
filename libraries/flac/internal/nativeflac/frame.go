package nativeflac

// FLAC frame structures, mirroring FLAC/format.h. Field semantics
// match libFLAC; the subframe variant types are flat structs the
// stream_decoder fills in as it parses each subframe.

// SubframeType — port of FLAC__SubframeType (format.h:264).
type SubframeType uint8

const (
	SubframeConstant SubframeType = 0
	SubframeVerbatim SubframeType = 1
	SubframeFixed    SubframeType = 2
	SubframeLPC      SubframeType = 3
)

// ChannelAssignment — port of FLAC__ChannelAssignment (format.h:388).
// Stereo decorrelation modes are decoded in-place during
// undo_channel_coding (stream_decoder.c:undo_channel_coding).
type ChannelAssignment uint8

const (
	ChannelAssignmentIndependent ChannelAssignment = 0
	ChannelAssignmentLeftSide    ChannelAssignment = 1
	ChannelAssignmentRightSide   ChannelAssignment = 2
	ChannelAssignmentMidSide     ChannelAssignment = 3
)

// FrameNumberType — port of FLAC__FrameNumberType (format.h:403).
//
// libFLAC's read_frame_header_ converts FRAME_NUMBER to SAMPLE_NUMBER
// once the fixed block size is known; the Go port follows the same
// rule so callers see SAMPLE_NUMBER on every parsed header.
type FrameNumberType uint8

const (
	FrameNumberTypeFrameNumber  FrameNumberType = 0
	FrameNumberTypeSampleNumber FrameNumberType = 1
)

// FrameHeader — port of FLAC__FrameHeader (format.h:418).
type FrameHeader struct {
	Blocksize         uint32
	SampleRate        uint32
	Channels          uint32
	ChannelAssignment ChannelAssignment
	BitsPerSample     uint32
	NumberType        FrameNumberType
	// Number is the frame-or-sample number; consult NumberType.
	// libFLAC stores both in a union; in Go we keep both fields as
	// uint64 and rely on NumberType.
	Number uint64
	CRC    uint8
}

// SubframeConstantData — port of FLAC__Subframe_Constant (format.h:281).
type SubframeConstantData struct {
	Value int64
}

// VerbatimDataType — port of FLAC__VerbatimSubframeDataType
// (format.h:286).
type VerbatimDataType uint8

const (
	VerbatimDataInt32 VerbatimDataType = 0
	VerbatimDataInt64 VerbatimDataType = 1
)

// SubframeVerbatimData — port of FLAC__Subframe_Verbatim (format.h:294).
type SubframeVerbatimData struct {
	Data32 []int32
	Data64 []int64
	Type   VerbatimDataType
}

// SubframeFixedData — port of FLAC__Subframe_Fixed (format.h:305).
type SubframeFixedData struct {
	EntropyCoding EntropyCodingMethod
	Order         uint32
	Warmup        [MaxFixedOrder]int64
	Residual      []int32
}

// SubframeLPCData — port of FLAC__Subframe_LPC (format.h:322).
type SubframeLPCData struct {
	EntropyCoding     EntropyCodingMethod
	Order             uint32
	QLPCoeffPrecision uint32
	QuantizationLevel int
	QLPCoeff          [MaxLPCOrder]int32
	Warmup            [MaxLPCOrder]int64
	Residual          []int32
}

// Subframe — port of FLAC__Subframe (format.h:351). Only one of the
// data fields is meaningful; consult Type.
type Subframe struct {
	Type       SubframeType
	WastedBits uint32
	Constant   SubframeConstantData
	Verbatim   SubframeVerbatimData
	Fixed      SubframeFixedData
	LPC        SubframeLPCData
}

// EntropyCodingMethodType — port of FLAC__EntropyCodingMethodType
// (format.h:202).
type EntropyCodingMethodType uint8

const (
	EntropyCodingMethodPartitionedRice  EntropyCodingMethodType = 0
	EntropyCodingMethodPartitionedRice2 EntropyCodingMethodType = 1
)

// PartitionedRiceContents — port of
// FLAC__EntropyCodingMethod_PartitionedRiceContents (format.h:215).
type PartitionedRiceContents struct {
	Parameters []uint32
	RawBits    []uint32
	// CapacityByOrder mirrors capacity_by_order: the partition order the
	// Parameters / RawBits slices are currently sized for (each holds
	// 1<<CapacityByOrder entries). The encoder's
	// PartitionedRiceContentsEnsureSize (encoder_state.go) only grows.
	CapacityByOrder uint32
}

// EntropyCodingMethod — port of FLAC__EntropyCodingMethod (format.h:230).
type EntropyCodingMethod struct {
	Type           EntropyCodingMethodType
	PartitionOrder uint32
	Contents       PartitionedRiceContents
}

// Frame — port of FLAC__Frame (format.h:480).
type Frame struct {
	Header    FrameHeader
	Subframes [MaxChannels]Subframe
	Footer    uint16 // CRC-16
}

// FrameHeaderStatus carries the result of a parse attempt. The libFLAC
// stream_decoder funnels the same conditions through its state
// machine + send_error_to_client_; the Go port surfaces them as a
// status enum for the caller to translate.
type FrameHeaderStatus uint8

const (
	// FrameHeaderOK — header successfully parsed.
	FrameHeaderOK FrameHeaderStatus = iota

	// FrameHeaderReadError — read callback failed mid-parse. The
	// underlying error is the BitReader's responsibility to surface.
	FrameHeaderReadError

	// FrameHeaderBadHeader — sync code reappeared inside the header,
	// CRC-8 mismatch, malformed UTF-8 sample number, or invalid
	// blocksize > 65535. libFLAC's BAD_HEADER error status; caller
	// should resync on the next sync code.
	FrameHeaderBadHeader

	// FrameHeaderUnparseable — header parsed structurally but
	// references reserved values (sample-rate code 15, future
	// channel-assignment codes, reserved zero-pad bit set). libFLAC's
	// UNPARSEABLE_STREAM error status.
	FrameHeaderUnparseable
)

// ReadFrameHeaderInput is the contextual data libFLAC's state machine
// supplies via decoder->private_; the Go port takes them as plain
// arguments.
type ReadFrameHeaderInput struct {
	// HeaderWarmup is the first two bytes of the frame header (the
	// sync code 0xFF 0xF8/F9), already consumed by the caller during
	// frame_sync_. libFLAC stashes them in
	// decoder->private_->header_warmup; we pass them in directly.
	HeaderWarmup [2]byte

	// HasStreamInfo indicates whether StreamInfo is populated. When
	// false, header fields that fall back to STREAMINFO (sample rate
	// code 0, bits-per-sample code 0) cause Unparseable.
	HasStreamInfo           bool
	StreamInfoSampleRate    uint32
	StreamInfoBitsPerSample uint32
	StreamInfoMinBlockSize  uint32
	StreamInfoMaxBlockSize  uint32

	// FixedBlockSize, when non-zero, is the running fixed-block-size
	// hint stream_decoder.c maintains for streams that lack STREAMINFO
	// but use frame numbers. Set on entry to allow correct
	// FRAME_NUMBER → SAMPLE_NUMBER conversion.
	FixedBlockSize uint32
}

// ReadFrameHeader — port of read_frame_header_ (stream_decoder.c:2624).
//
// The bit reader must be byte-aligned (libFLAC asserts this on entry).
// Returns the parsed header on FrameHeaderOK; on
// FrameHeaderBadHeader / Unparseable the caller may still have
// partially advanced the bit reader (matching libFLAC's behaviour
// where the reader is left at the failing byte). On FrameHeaderReadError
// the underlying read callback failed.
//
// Sets nextFixedBlockSize to a non-zero value when the parsed header
// resolves to a fixed-blocksize stream and the caller should remember
// the block size for the NEXT frame (libFLAC's
// decoder->private_->next_fixed_block_size).
func ReadFrameHeader(br *BitReader, in ReadFrameHeaderInput) (h FrameHeader, nextFixedBlockSize uint32, status FrameHeaderStatus) {
	// raw_header collects the header bytes for the trailing CRC-8
	// check; matches the byte stash libFLAC builds at lines 2629–2638.
	var rawHeader [16]byte
	rawHeader[0] = in.HeaderWarmup[0]
	rawHeader[1] = in.HeaderWarmup[1]
	rawLen := 2

	isUnparseable := false

	// Reserved bit must be 0 (line 2641).
	if rawHeader[1]&0x02 != 0 {
		isUnparseable = true
	}

	// Read 2 more bytes; reject any 0xFF (sync inside header → original
	// sync was wrong; lines 2666–2678).
	for i := 0; i < 2; i++ {
		x, ok := br.ReadRawUint32(8)
		if !ok {
			return h, 0, FrameHeaderReadError
		}
		if x == 0xFF {
			return h, 0, FrameHeaderBadHeader
		}
		rawHeader[rawLen] = byte(x)
		rawLen++
	}

	// Blocksize encoding (lines 2680–2710).
	var blocksizeHint uint32
	switch x := uint32(rawHeader[2]) >> 4; x {
	case 0:
		isUnparseable = true
	case 1:
		h.Blocksize = 192
	case 2, 3, 4, 5:
		h.Blocksize = 576 << (x - 2)
	case 6, 7:
		blocksizeHint = x
	default: // 8..15
		h.Blocksize = 256 << (x - 8)
	}

	// Sample-rate encoding (lines 2712–2762).
	var sampleRateHint uint32
	switch x := uint32(rawHeader[2]) & 0x0F; x {
	case 0:
		if in.HasStreamInfo {
			h.SampleRate = in.StreamInfoSampleRate
		} else {
			isUnparseable = true
		}
	case 1:
		h.SampleRate = 88200
	case 2:
		h.SampleRate = 176400
	case 3:
		h.SampleRate = 192000
	case 4:
		h.SampleRate = 8000
	case 5:
		h.SampleRate = 16000
	case 6:
		h.SampleRate = 22050
	case 7:
		h.SampleRate = 24000
	case 8:
		h.SampleRate = 32000
	case 9:
		h.SampleRate = 44100
	case 10:
		h.SampleRate = 48000
	case 11:
		h.SampleRate = 96000
	case 12, 13, 14:
		sampleRateHint = x
	case 15:
		// Reserved code that libFLAC explicitly maps to BAD_HEADER
		// (line 2758) — it would have re-emitted the sync byte
		// inside the header.
		return h, 0, FrameHeaderBadHeader
	}

	// Channel assignment + count (lines 2765–2786).
	x := uint32(rawHeader[3]) >> 4
	if x&8 != 0 {
		h.Channels = 2
		switch x & 7 {
		case 0:
			h.ChannelAssignment = ChannelAssignmentLeftSide
		case 1:
			h.ChannelAssignment = ChannelAssignmentRightSide
		case 2:
			h.ChannelAssignment = ChannelAssignmentMidSide
		default:
			isUnparseable = true
		}
	} else {
		h.Channels = x + 1
		h.ChannelAssignment = ChannelAssignmentIndependent
	}

	// Bits per sample (lines 2788–2819).
	switch x := (uint32(rawHeader[3]) & 0x0E) >> 1; x {
	case 0:
		if in.HasStreamInfo {
			h.BitsPerSample = in.StreamInfoBitsPerSample
		} else {
			isUnparseable = true
		}
	case 1:
		h.BitsPerSample = 8
	case 2:
		h.BitsPerSample = 12
	case 3:
		isUnparseable = true
	case 4:
		h.BitsPerSample = 16
	case 5:
		h.BitsPerSample = 20
	case 6:
		h.BitsPerSample = 24
	case 7:
		h.BitsPerSample = 32
	}

	// Reserved bit (line 2823).
	if rawHeader[3]&0x01 != 0 {
		isUnparseable = true
	}

	// Frame / sample number — the variable-blocking strategy bit at
	// raw_header[1]&0x01 picks the encoding, but a STREAMINFO with
	// min_blocksize != max_blocksize ALSO forces variable blocking
	// for legacy reasons (lines 2828–2856).
	variable := rawHeader[1]&0x01 != 0
	if !variable && in.HasStreamInfo &&
		in.StreamInfoMinBlockSize != in.StreamInfoMaxBlockSize {
		variable = true
	}
	if variable {
		// libFLAC's read_utf8_uint64 appends bytes starting at
		// raw_header[raw_header_len]; we mirror that by handing it a
		// sub-slice rooted at rawLen.
		xx, rl, ok := br.ReadUTF8Uint64(rawHeader[rawLen:])
		if !ok {
			return h, 0, FrameHeaderReadError
		}
		rawLen += rl
		if xx == 0xFFFFFFFFFFFFFFFF {
			return h, 0, FrameHeaderBadHeader
		}
		h.NumberType = FrameNumberTypeSampleNumber
		h.Number = xx
	} else {
		xx, rl, ok := br.ReadUTF8Uint32(rawHeader[rawLen:])
		if !ok {
			return h, 0, FrameHeaderReadError
		}
		rawLen += rl
		if xx == 0xFFFFFFFF {
			return h, 0, FrameHeaderBadHeader
		}
		h.NumberType = FrameNumberTypeFrameNumber
		h.Number = uint64(xx)
	}

	// Blocksize follow-on (8 or 16 bits) when blocksizeHint was set.
	if blocksizeHint != 0 {
		x, ok := br.ReadRawUint32(8)
		if !ok {
			return h, 0, FrameHeaderReadError
		}
		rawHeader[rawLen] = byte(x)
		rawLen++
		if blocksizeHint == 7 {
			lo, ok := br.ReadRawUint32(8)
			if !ok {
				return h, 0, FrameHeaderReadError
			}
			rawHeader[rawLen] = byte(lo)
			rawLen++
			x = (x << 8) | lo
		}
		h.Blocksize = x + 1
		if h.Blocksize > 65535 {
			return h, 0, FrameHeaderBadHeader
		}
	}

	// Sample-rate follow-on.
	if sampleRateHint != 0 {
		x, ok := br.ReadRawUint32(8)
		if !ok {
			return h, 0, FrameHeaderReadError
		}
		rawHeader[rawLen] = byte(x)
		rawLen++
		if sampleRateHint != 12 {
			lo, ok := br.ReadRawUint32(8)
			if !ok {
				return h, 0, FrameHeaderReadError
			}
			rawHeader[rawLen] = byte(lo)
			rawLen++
			x = (x << 8) | lo
		}
		switch sampleRateHint {
		case 12:
			h.SampleRate = x * 1000
		case 13:
			h.SampleRate = x
		default: // 14
			h.SampleRate = x * 10
		}
	}

	// CRC-8 trailer.
	crcVal, ok := br.ReadRawUint32(8)
	if !ok {
		return h, 0, FrameHeaderReadError
	}
	h.CRC = byte(crcVal)
	if CRC8(rawHeader[:rawLen]) != h.CRC {
		return h, 0, FrameHeaderBadHeader
	}

	// Convert FRAME_NUMBER to SAMPLE_NUMBER if needed (lines 2917–2939).
	if h.NumberType == FrameNumberTypeFrameNumber {
		fn := h.Number
		h.NumberType = FrameNumberTypeSampleNumber
		switch {
		case in.FixedBlockSize != 0:
			h.Number = uint64(in.FixedBlockSize) * fn
		case in.HasStreamInfo:
			if in.StreamInfoMinBlockSize == in.StreamInfoMaxBlockSize {
				h.Number = uint64(in.StreamInfoMinBlockSize) * fn
				nextFixedBlockSize = in.StreamInfoMaxBlockSize
			} else {
				isUnparseable = true
			}
		case fn == 0:
			h.Number = 0
			nextFixedBlockSize = h.Blocksize
		default:
			h.Number = uint64(h.Blocksize) * fn
		}
	}

	// libFLAC sets `next_fixed_block_size` BEFORE its `is_unparseable`
	// branch (stream_decoder.c:2917–2945), so the field is observable
	// even on Unparseable returns. We mirror that by carrying
	// nextFixedBlockSize through both arms.
	if isUnparseable {
		return h, nextFixedBlockSize, FrameHeaderUnparseable
	}
	return h, nextFixedBlockSize, FrameHeaderOK
}
