package nativeflac

// 1:1 port of libflac/src/libFLAC/stream_encoder_framing.c.
//
// The framing layer turns the encoder's in-memory metadata blocks,
// frame headers, and subframe structures into the exact bit stream
// libFLAC emits. It is the write-side mirror of the metadata/frame/
// subframe readers in metadata_decode.go / frame.go / subframe.go and
// writes through the BitWriter ported in bitwriter.go.
//
// Every entry point returns a bool that follows libFLAC's convention:
// false means a BitWriter operation failed (out of memory / range
// check). The functions assume the BitWriter is byte aligned on entry,
// just as libFLAC asserts.

// Metadata-block field widths, mirroring the FLAC__STREAM_METADATA_*
// macros in FLAC/format.h that the framing layer references.
// StreamMetadataIsLastLen / TypeLen / LengthLen and the STREAMINFO /
// APPLICATION widths already live in metadata_decode.go / format.go.
const (
	// CUESHEET widths (format.h:731–735, 697–703, 664–666).
	CuesheetMediaCatalogNumberLen = 128 * 8 // bits
	CuesheetLeadInLen             = 64
	CuesheetIsCDLen               = 1
	CuesheetReservedLen           = 7 + 258*8
	CuesheetNumTracksLen          = 8

	CuesheetTrackOffsetLen      = 64
	CuesheetTrackNumberLen      = 8
	CuesheetTrackISRCLen        = 12 * 8 // bits
	CuesheetTrackTypeLen        = 1
	CuesheetTrackPreEmphasisLen = 1
	CuesheetTrackReservedLen    = 6 + 13*8
	CuesheetTrackNumIndicesLen  = 8

	CuesheetIndexOffsetLen   = 64
	CuesheetIndexNumberLen   = 8
	CuesheetIndexReservedLen = 3 * 8

	// PICTURE widths (format.h:820–827).
	PictureTypeLen              = 32
	PictureMimeTypeLengthLen    = 32
	PictureDescriptionLengthLen = 32
	PictureWidthLen             = 32
	PictureHeightLen            = 32
	PictureDepthLen             = 32
	PictureColorsLen            = 32
	PictureDataLengthLen        = 32
)

// SeekPoint — port of FLAC__StreamMetadata_SeekPoint (format.h:580).
type SeekPoint struct {
	SampleNumber uint64
	StreamOffset uint64
	FrameSamples uint32
}

// SeekTable — port of FLAC__StreamMetadata_SeekTable (format.h:618).
type SeekTable struct {
	Points []SeekPoint
}

// Application — port of FLAC__StreamMetadata_Application (format.h:572).
type Application struct {
	ID   [4]byte
	Data []byte
}

// VorbisCommentEntry — port of
// FLAC__StreamMetadata_VorbisComment_Entry (format.h:631). Length is
// the byte count of Entry; the trailing NUL libFLAC keeps for
// convenience is not part of the framed data.
type VorbisCommentEntry struct {
	Length uint32
	Entry  []byte
}

// VorbisComment — port of FLAC__StreamMetadata_VorbisComment
// (format.h:640).
type VorbisComment struct {
	VendorString VorbisCommentEntry
	NumComments  uint32
	Comments     []VorbisCommentEntry
}

// CueSheetIndex — port of FLAC__StreamMetadata_CueSheet_Index
// (format.h:651).
type CueSheetIndex struct {
	Offset uint64
	Number byte
}

// CueSheetTrack — port of FLAC__StreamMetadata_CueSheet_Track
// (format.h:674). ISRC is the fixed 12-byte field (the C struct keeps a
// trailing NUL in char[13]; only 12 bytes are framed).
type CueSheetTrack struct {
	Offset      uint64
	Number      byte
	ISRC        [12]byte
	Type        uint32 // 1 bit
	PreEmphasis uint32 // 1 bit
	NumIndices  byte
	Indices     []CueSheetIndex
}

// CueSheet — port of FLAC__StreamMetadata_CueSheet (format.h:714).
// MediaCatalogNumber is the fixed 128-byte field (the C struct keeps a
// trailing NUL in char[129]; only 128 bytes are framed).
type CueSheet struct {
	MediaCatalogNumber [128]byte
	LeadIn             uint64
	IsCD               bool
	NumTracks          uint32
	Tracks             []CueSheetTrack
}

// Picture — port of FLAC__StreamMetadata_Picture (format.h:790).
// MimeType / Description hold the raw byte contents framed verbatim;
// libFLAC stores them as NUL-terminated C strings and frames strlen()
// of each — the Go port frames len() of the slice, so callers must not
// include a trailing NUL.
type Picture struct {
	Type        uint32
	MimeType    []byte
	Description []byte
	Width       uint32
	Height      uint32
	Depth       uint32
	Colors      uint32
	DataLength  uint32
	Data        []byte
}

// StreamMetadata — port of FLAC__StreamMetadata (format.h:850). Only
// the union member selected by Type is consulted by AddMetadataBlock.
type StreamMetadata struct {
	Type          MetadataType
	IsLast        bool
	Length        uint32
	StreamInfo    StreamInfo
	Application   Application
	SeekTable     SeekTable
	VorbisComment VorbisComment
	CueSheet      CueSheet
	Picture       Picture
	// Unknown is the raw body for blocks whose Type is >=
	// MetadataTypeUndefined (FLAC__StreamMetadata_Unknown).
	Unknown []byte
}

// AddMetadataBlock — port of FLAC__add_metadata_block
// (stream_encoder_framing.c:47). Frames one metadata block (header +
// body) into bw. When updateVendorString is true and the block is a
// VORBIS_COMMENT, libFLAC overrides any caller vendor string with
// VendorString and adjusts the framed length accordingly
// (stream_encoder_framing.c:65–68); the Go port preserves that.
func AddMetadataBlock(metadata *StreamMetadata, bw *BitWriter, updateVendorString bool) bool {
	vendorStringLength := uint32(len(VendorString))
	startBits := bw.GetInputBitsUnconsumed()

	// FLAC__ASSERT(FLAC__bitwriter_is_byte_aligned(bw)) — caller invariant.

	if !bw.WriteRawUint32(b2u(metadata.IsLast), StreamMetadataIsLastLen) {
		return false
	}
	if !bw.WriteRawUint32(uint32(metadata.Type), StreamMetadataTypeLen) {
		return false
	}

	// First, for VORBIS_COMMENTs, adjust the length to reflect our
	// vendor string.
	metadataLength := metadata.Length
	if metadata.Type == MetadataTypeVorbisComment && updateVendorString {
		metadataLength -= metadata.VorbisComment.VendorString.Length
		metadataLength += vendorStringLength
	}
	// double protection
	if metadataLength >= (1 << StreamMetadataLengthLen) {
		return false
	}
	if !bw.WriteRawUint32(metadataLength, StreamMetadataLengthLen) {
		return false
	}

	switch metadata.Type {
	case MetadataTypeStreamInfo:
		si := &metadata.StreamInfo
		if !bw.WriteRawUint32(si.MinBlockSize, StreamInfoMinBlockSizeLen) {
			return false
		}
		if !bw.WriteRawUint32(si.MaxBlockSize, StreamInfoMaxBlockSizeLen) {
			return false
		}
		if !bw.WriteRawUint32(si.MinFrameSize, StreamInfoMinFrameSizeLen) {
			return false
		}
		if !bw.WriteRawUint32(si.MaxFrameSize, StreamInfoMaxFrameSizeLen) {
			return false
		}
		if !bw.WriteRawUint32(si.SampleRate, StreamInfoSampleRateLen) {
			return false
		}
		if !bw.WriteRawUint32(si.Channels-1, StreamInfoChannelsLen) {
			return false
		}
		if !bw.WriteRawUint32(si.BitsPerSample-1, StreamInfoBitsPerSampleLen) {
			return false
		}
		if si.TotalSamples >= (uint64(1) << StreamInfoTotalSamplesLen) {
			if !bw.WriteRawUint64(0, StreamInfoTotalSamplesLen) {
				return false
			}
		} else {
			if !bw.WriteRawUint64(si.TotalSamples, StreamInfoTotalSamplesLen) {
				return false
			}
		}
		if !bw.WriteByteBlock(si.MD5Sum[:]) {
			return false
		}

	case MetadataTypePadding:
		if !bw.WriteZeroes(metadata.Length * 8) {
			return false
		}

	case MetadataTypeApplication:
		if !bw.WriteByteBlock(metadata.Application.ID[:]) {
			return false
		}
		// length - (APPLICATION_ID_LEN/8) bytes of data.
		if !bw.WriteByteBlock(metadata.Application.Data[:metadata.Length-(StreamMetadataApplicationIDLen/8)]) {
			return false
		}

	case MetadataTypeSeekTable:
		for i := range metadata.SeekTable.Points {
			p := &metadata.SeekTable.Points[i]
			if !bw.WriteRawUint64(p.SampleNumber, SeekpointSampleNumberLen) {
				return false
			}
			if !bw.WriteRawUint64(p.StreamOffset, SeekpointStreamOffsetLen) {
				return false
			}
			if !bw.WriteRawUint32(p.FrameSamples, SeekpointFrameSamplesLen) {
				return false
			}
		}

	case MetadataTypeVorbisComment:
		vc := &metadata.VorbisComment
		if updateVendorString {
			if !bw.WriteRawUint32LittleEndian(vendorStringLength) {
				return false
			}
			if !bw.WriteByteBlock([]byte(VendorString)) {
				return false
			}
		} else {
			if !bw.WriteRawUint32LittleEndian(vc.VendorString.Length) {
				return false
			}
			if !bw.WriteByteBlock(vc.VendorString.Entry[:vc.VendorString.Length]) {
				return false
			}
		}
		if !bw.WriteRawUint32LittleEndian(vc.NumComments) {
			return false
		}
		for i := uint32(0); i < vc.NumComments; i++ {
			c := &vc.Comments[i]
			if !bw.WriteRawUint32LittleEndian(c.Length) {
				return false
			}
			if !bw.WriteByteBlock(c.Entry[:c.Length]) {
				return false
			}
		}

	case MetadataTypeCuesheet:
		cs := &metadata.CueSheet
		if !bw.WriteByteBlock(cs.MediaCatalogNumber[:CuesheetMediaCatalogNumberLen/8]) {
			return false
		}
		if !bw.WriteRawUint64(cs.LeadIn, CuesheetLeadInLen) {
			return false
		}
		if !bw.WriteRawUint32(b2u(cs.IsCD), CuesheetIsCDLen) {
			return false
		}
		if !bw.WriteZeroes(CuesheetReservedLen) {
			return false
		}
		if !bw.WriteRawUint32(cs.NumTracks, CuesheetNumTracksLen) {
			return false
		}
		for i := uint32(0); i < cs.NumTracks; i++ {
			track := &cs.Tracks[i]
			if !bw.WriteRawUint64(track.Offset, CuesheetTrackOffsetLen) {
				return false
			}
			if !bw.WriteRawUint32(uint32(track.Number), CuesheetTrackNumberLen) {
				return false
			}
			if !bw.WriteByteBlock(track.ISRC[:CuesheetTrackISRCLen/8]) {
				return false
			}
			if !bw.WriteRawUint32(track.Type, CuesheetTrackTypeLen) {
				return false
			}
			if !bw.WriteRawUint32(track.PreEmphasis, CuesheetTrackPreEmphasisLen) {
				return false
			}
			if !bw.WriteZeroes(CuesheetTrackReservedLen) {
				return false
			}
			if !bw.WriteRawUint32(uint32(track.NumIndices), CuesheetTrackNumIndicesLen) {
				return false
			}
			for j := uint32(0); j < uint32(track.NumIndices); j++ {
				indx := &track.Indices[j]
				if !bw.WriteRawUint64(indx.Offset, CuesheetIndexOffsetLen) {
					return false
				}
				if !bw.WriteRawUint32(uint32(indx.Number), CuesheetIndexNumberLen) {
					return false
				}
				if !bw.WriteZeroes(CuesheetIndexReservedLen) {
					return false
				}
			}
		}

	case MetadataTypePicture:
		pic := &metadata.Picture
		if !bw.WriteRawUint32(pic.Type, PictureTypeLen) {
			return false
		}
		// libFLAC frames strlen(mime_type); MimeType holds the raw bytes.
		ln := uint32(len(pic.MimeType))
		if !bw.WriteRawUint32(ln, PictureMimeTypeLengthLen) {
			return false
		}
		if !bw.WriteByteBlock(pic.MimeType[:ln]) {
			return false
		}
		ln = uint32(len(pic.Description))
		if !bw.WriteRawUint32(ln, PictureDescriptionLengthLen) {
			return false
		}
		if !bw.WriteByteBlock(pic.Description[:ln]) {
			return false
		}
		if !bw.WriteRawUint32(pic.Width, PictureWidthLen) {
			return false
		}
		if !bw.WriteRawUint32(pic.Height, PictureHeightLen) {
			return false
		}
		if !bw.WriteRawUint32(pic.Depth, PictureDepthLen) {
			return false
		}
		if !bw.WriteRawUint32(pic.Colors, PictureColorsLen) {
			return false
		}
		if !bw.WriteRawUint32(pic.DataLength, PictureDataLengthLen) {
			return false
		}
		if !bw.WriteByteBlock(pic.Data[:pic.DataLength]) {
			return false
		}

	default:
		if !bw.WriteByteBlock(metadata.Unknown[:metadata.Length]) {
			return false
		}
	}

	// Now check whether metadata block length was correct.
	lengthInBits := bw.GetInputBitsUnconsumed()
	if lengthInBits < startBits {
		return false
	}
	lengthInBits -= startBits
	if lengthInBits%8 != 0 || lengthInBits != (metadataLength*8+32) {
		return false
	}

	return true
}

// FrameAddHeader — port of FLAC__frame_add_header
// (stream_encoder_framing.c:245). Frames the audio frame header
// (sync, blocking strategy, coded block size / sample rate / channel
// assignment / bits-per-sample, UTF-8 frame-or-sample number, any
// follow-on block-size/sample-rate bytes, and the CRC-8 trailer).
func FrameAddHeader(header *FrameHeader, bw *BitWriter) bool {
	var u, blocksizeHint, sampleRateHint uint32

	// FLAC__ASSERT(FLAC__bitwriter_is_byte_aligned(bw)) — caller invariant.

	if !bw.WriteRawUint32(FrameHeaderSync, FrameHeaderSyncLen) {
		return false
	}
	if !bw.WriteRawUint32(0, FrameHeaderReservedLen) {
		return false
	}
	blocking := uint32(1)
	if header.NumberType == FrameNumberTypeFrameNumber {
		blocking = 0
	}
	if !bw.WriteRawUint32(blocking, FrameHeaderBlockingLen) {
		return false
	}

	blocksizeHint = 0
	switch header.Blocksize {
	case 192:
		u = 1
	case 576:
		u = 2
	case 1152:
		u = 3
	case 2304:
		u = 4
	case 4608:
		u = 5
	case 256:
		u = 8
	case 512:
		u = 9
	case 1024:
		u = 10
	case 2048:
		u = 11
	case 4096:
		u = 12
	case 8192:
		u = 13
	case 16384:
		u = 14
	case 32768:
		u = 15
	default:
		if header.Blocksize <= 0x100 {
			blocksizeHint = 6
			u = 6
		} else {
			blocksizeHint = 7
			u = 7
		}
	}
	if !bw.WriteRawUint32(u, FrameHeaderBlockSizeLen) {
		return false
	}

	sampleRateHint = 0
	switch header.SampleRate {
	case 88200:
		u = 1
	case 176400:
		u = 2
	case 192000:
		u = 3
	case 8000:
		u = 4
	case 16000:
		u = 5
	case 22050:
		u = 6
	case 24000:
		u = 7
	case 32000:
		u = 8
	case 44100:
		u = 9
	case 48000:
		u = 10
	case 96000:
		u = 11
	default:
		switch {
		case header.SampleRate <= 255000 && header.SampleRate%1000 == 0:
			sampleRateHint = 12
			u = 12
		case header.SampleRate <= 655350 && header.SampleRate%10 == 0:
			sampleRateHint = 14
			u = 14
		case header.SampleRate <= 0xffff:
			sampleRateHint = 13
			u = 13
		default:
			u = 0
		}
	}
	if !bw.WriteRawUint32(u, FrameHeaderSampleRateLen) {
		return false
	}

	switch header.ChannelAssignment {
	case ChannelAssignmentIndependent:
		u = header.Channels - 1
	case ChannelAssignmentLeftSide:
		u = 8
	case ChannelAssignmentRightSide:
		u = 9
	case ChannelAssignmentMidSide:
		u = 10
	default:
		// FLAC__ASSERT(0)
		return false
	}
	if !bw.WriteRawUint32(u, FrameHeaderChannelLen) {
		return false
	}

	switch header.BitsPerSample {
	case 8:
		u = 1
	case 12:
		u = 2
	case 16:
		u = 4
	case 20:
		u = 5
	case 24:
		u = 6
	case 32:
		u = 7
	default:
		u = 0
	}
	if !bw.WriteRawUint32(u, FrameHeaderBitsLen) {
		return false
	}

	if !bw.WriteRawUint32(0, FrameHeaderZeroPadLen) {
		return false
	}

	if header.NumberType == FrameNumberTypeFrameNumber {
		if !bw.WriteUTF8Uint32(uint32(header.Number)) {
			return false
		}
	} else {
		if !bw.WriteUTF8Uint64(header.Number) {
			return false
		}
	}

	if blocksizeHint != 0 {
		nbits := uint32(16)
		if blocksizeHint == 6 {
			nbits = 8
		}
		if !bw.WriteRawUint32(header.Blocksize-1, nbits) {
			return false
		}
	}

	switch sampleRateHint {
	case 12:
		if !bw.WriteRawUint32(header.SampleRate/1000, 8) {
			return false
		}
	case 13:
		if !bw.WriteRawUint32(header.SampleRate, 16) {
			return false
		}
	case 14:
		if !bw.WriteRawUint32(header.SampleRate/10, 16) {
			return false
		}
	}

	// write the CRC
	crc, ok := bw.GetWriteCRC8()
	if !ok {
		return false
	}
	if !bw.WriteRawUint32(uint32(crc), FrameHeaderCRCLen) {
		return false
	}

	return true
}

// SubframeAddConstant — port of FLAC__subframe_add_constant
// (stream_encoder_framing.c:393).
func SubframeAddConstant(subframe *SubframeConstantData, subframeBps, wastedBits uint32, bw *BitWriter) bool {
	ok := bw.WriteRawUint32(
		SubframeTypeConstantByteAlignedMask|b2u(wastedBits != 0),
		SubframeZeroPadLen+SubframeTypeLen+SubframeWastedBitsFlagLen) &&
		writeWastedBits(bw, wastedBits) &&
		bw.WriteRawInt64(subframe.Value, subframeBps)
	return ok
}

// SubframeAddFixed — port of FLAC__subframe_add_fixed
// (stream_encoder_framing.c:406).
func SubframeAddFixed(subframe *SubframeFixedData, residualSamples, subframeBps, wastedBits uint32, bw *BitWriter) bool {
	if !bw.WriteRawUint32(
		SubframeTypeFixedByteAlignedMask|(subframe.Order<<1)|b2u(wastedBits != 0),
		SubframeZeroPadLen+SubframeTypeLen+SubframeWastedBitsFlagLen) {
		return false
	}
	if wastedBits != 0 {
		if !bw.WriteUnaryUnsigned(wastedBits - 1) {
			return false
		}
	}

	for i := uint32(0); i < subframe.Order; i++ {
		if !bw.WriteRawInt64(subframe.Warmup[i], subframeBps) {
			return false
		}
	}

	if !addEntropyCodingMethod(bw, &subframe.EntropyCoding) {
		return false
	}
	switch subframe.EntropyCoding.Type {
	case EntropyCodingMethodPartitionedRice, EntropyCodingMethodPartitionedRice2:
		if !addResidualPartitionedRice(
			bw,
			subframe.Residual,
			residualSamples,
			subframe.Order,
			subframe.EntropyCoding.Contents.Parameters,
			subframe.EntropyCoding.Contents.RawBits,
			subframe.EntropyCoding.PartitionOrder,
			subframe.EntropyCoding.Type == EntropyCodingMethodPartitionedRice2,
		) {
			return false
		}
	default:
		// FLAC__ASSERT(0)
		return false
	}

	return true
}

// SubframeAddLPC — port of FLAC__subframe_add_lpc
// (stream_encoder_framing.c:444).
func SubframeAddLPC(subframe *SubframeLPCData, residualSamples, subframeBps, wastedBits uint32, bw *BitWriter) bool {
	if !bw.WriteRawUint32(
		SubframeTypeLPCByteAlignedMask|((subframe.Order-1)<<1)|b2u(wastedBits != 0),
		SubframeZeroPadLen+SubframeTypeLen+SubframeWastedBitsFlagLen) {
		return false
	}
	if wastedBits != 0 {
		if !bw.WriteUnaryUnsigned(wastedBits - 1) {
			return false
		}
	}

	for i := uint32(0); i < subframe.Order; i++ {
		if !bw.WriteRawInt64(subframe.Warmup[i], subframeBps) {
			return false
		}
	}

	if !bw.WriteRawUint32(subframe.QLPCoeffPrecision-1, SubframeLPCQLPCoeffPrecisionLen) {
		return false
	}
	if !bw.WriteRawInt32(int32(subframe.QuantizationLevel), SubframeLPCQLPShiftLen) {
		return false
	}
	for i := uint32(0); i < subframe.Order; i++ {
		if !bw.WriteRawInt32(subframe.QLPCoeff[i], subframe.QLPCoeffPrecision) {
			return false
		}
	}

	if !addEntropyCodingMethod(bw, &subframe.EntropyCoding) {
		return false
	}
	switch subframe.EntropyCoding.Type {
	case EntropyCodingMethodPartitionedRice, EntropyCodingMethodPartitionedRice2:
		if !addResidualPartitionedRice(
			bw,
			subframe.Residual,
			residualSamples,
			subframe.Order,
			subframe.EntropyCoding.Contents.Parameters,
			subframe.EntropyCoding.Contents.RawBits,
			subframe.EntropyCoding.PartitionOrder,
			subframe.EntropyCoding.Type == EntropyCodingMethodPartitionedRice2,
		) {
			return false
		}
	default:
		// FLAC__ASSERT(0)
		return false
	}

	return true
}

// SubframeAddVerbatim — port of FLAC__subframe_add_verbatim
// (stream_encoder_framing.c:490).
func SubframeAddVerbatim(subframe *SubframeVerbatimData, samples, subframeBps, wastedBits uint32, bw *BitWriter) bool {
	if !bw.WriteRawUint32(
		SubframeTypeVerbatimByteAlignedMask|b2u(wastedBits != 0),
		SubframeZeroPadLen+SubframeTypeLen+SubframeWastedBitsFlagLen) {
		return false
	}
	if wastedBits != 0 {
		if !bw.WriteUnaryUnsigned(wastedBits - 1) {
			return false
		}
	}

	if subframe.Type == VerbatimDataInt32 {
		signal := subframe.Data32
		for i := uint32(0); i < samples; i++ {
			if !bw.WriteRawInt32(signal[i], subframeBps) {
				return false
			}
		}
	} else {
		signal := subframe.Data64
		for i := uint32(0); i < samples; i++ {
			if !bw.WriteRawInt64(signal[i], subframeBps) {
				return false
			}
		}
	}

	return true
}

// addEntropyCodingMethod — port of add_entropy_coding_method_
// (stream_encoder_framing.c:522).
func addEntropyCodingMethod(bw *BitWriter, method *EntropyCodingMethod) bool {
	if !bw.WriteRawUint32(uint32(method.Type), EntropyCodingMethodTypeLen) {
		return false
	}
	switch method.Type {
	case EntropyCodingMethodPartitionedRice, EntropyCodingMethodPartitionedRice2:
		if !bw.WriteRawUint32(method.PartitionOrder, EntropyCodingMethodPartitionedRiceOrder) {
			return false
		}
	default:
		// FLAC__ASSERT(0)
		return false
	}
	return true
}

// addResidualPartitionedRice — port of add_residual_partitioned_rice_
// (stream_encoder_framing.c:538).
func addResidualPartitionedRice(bw *BitWriter, residual []int32, residualSamples, predictorOrder uint32, riceParameters, rawBits []uint32, partitionOrder uint32, isExtended bool) bool {
	plen := uint32(EntropyCodingMethodPartitionedRiceParam)
	pesc := uint32(EntropyCodingPartitionedRiceEscape)
	if isExtended {
		plen = EntropyCodingMethodPartitionedRice2Param
		pesc = EntropyCodingPartitionedRice2Escape
	}

	if partitionOrder == 0 {
		if rawBits[0] == 0 {
			if !bw.WriteRawUint32(riceParameters[0], plen) {
				return false
			}
			if !bw.WriteRiceSignedBlock(residual[:residualSamples], riceParameters[0]) {
				return false
			}
		} else {
			// FLAC__ASSERT(rice_parameters[0] == 0)
			if !bw.WriteRawUint32(pesc, plen) {
				return false
			}
			if !bw.WriteRawUint32(rawBits[0], EntropyCodingMethodPartitionedRiceRawLen) {
				return false
			}
			for i := uint32(0); i < residualSamples; i++ {
				if !bw.WriteRawInt32(residual[i], rawBits[0]) {
					return false
				}
			}
		}
		return true
	}

	var k, kLast uint32
	defaultPartitionSamples := (residualSamples + predictorOrder) >> partitionOrder
	for i := uint32(0); i < (uint32(1) << partitionOrder); i++ {
		partitionSamples := defaultPartitionSamples
		if i == 0 {
			partitionSamples -= predictorOrder
		}
		k += partitionSamples
		if rawBits[i] == 0 {
			if !bw.WriteRawUint32(riceParameters[i], plen) {
				return false
			}
			if !bw.WriteRiceSignedBlock(residual[kLast:k], riceParameters[i]) {
				return false
			}
		} else {
			if !bw.WriteRawUint32(pesc, plen) {
				return false
			}
			if !bw.WriteRawUint32(rawBits[i], EntropyCodingMethodPartitionedRiceRawLen) {
				return false
			}
			for j := kLast; j < k; j++ {
				if !bw.WriteRawInt32(residual[j], rawBits[i]) {
					return false
				}
			}
		}
		kLast = k
	}
	return true
}

// writeWastedBits emits the unary wasted-bits run only when wastedBits
// is non-zero, mirroring the inline ternary in FLAC__subframe_add_constant.
func writeWastedBits(bw *BitWriter, wastedBits uint32) bool {
	if wastedBits != 0 {
		return bw.WriteUnaryUnsigned(wastedBits - 1)
	}
	return true
}

// wb mirrors C's `(wasted_bits? 1:0)` used in the subframe-type byte.
func wb(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// b2u converts a bool to the uint32 1/0 the C framing writes for
// FLAC__bool fields (is_last, is_cd).
func b2u(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}
