package nativeflac

import "sort"

// 1:1 port of libflac/src/libFLAC/format.c, scoped to the helpers the
// decoder + encoder pipelines actually use. Metadata-object lifecycle
// helpers (seektable sort, cuesheet/picture legality) live in their own
// files when those subsystems land.
//
// Constants come straight from FLAC/format.h. The libFLAC FLAC_API
// const variables exist for ABI stability across shared-library
// versions; in Go we collapse them to compile-time constants.

// FLAC__STREAM_SYNC* — the four-byte "fLaC" magic at the start of every
// native FLAC stream (format.c:61–63).
const (
	StreamSyncString0 byte   = 'f'
	StreamSyncString1 byte   = 'L'
	StreamSyncString2 byte   = 'a'
	StreamSyncString3 byte   = 'C'
	StreamSync        uint32 = 0x664C6143
	StreamSyncLen     uint32 = 32 // bits
)

// STREAMINFO bit-layout lengths (format.c:65–73). Each gives the field
// width in bits within the metadata block body.
const (
	StreamInfoMinBlockSizeLen  = 16
	StreamInfoMaxBlockSizeLen  = 16
	StreamInfoMinFrameSizeLen  = 24
	StreamInfoMaxFrameSizeLen  = 24
	StreamInfoSampleRateLen    = 20
	StreamInfoChannelsLen      = 3
	StreamInfoBitsPerSampleLen = 5
	StreamInfoTotalSamplesLen  = 36
	StreamInfoMD5SumLen        = 128
)

// SEEKTABLE / VORBIS_COMMENT / CUESHEET / FRAME / SUBFRAME bit-layout
// lengths (format.c:77–149). The encoder uses these to pack metadata
// blocks; the decoder mirrors them when parsing.
const (
	SeekpointSampleNumberLen = 64
	SeekpointStreamOffsetLen = 64
	SeekpointFrameSamplesLen = 16

	SeekpointPlaceholder uint64 = 0xFFFFFFFFFFFFFFFF

	VorbisCommentEntryLengthLen = 32
	VorbisCommentNumCommentsLen = 32

	FrameHeaderSync          = 0x3FFE // 14-bit
	FrameHeaderSyncLen       = 14
	FrameHeaderReservedLen   = 1
	FrameHeaderBlockingLen   = 1
	FrameHeaderBlockSizeLen  = 4
	FrameHeaderSampleRateLen = 4
	FrameHeaderChannelLen    = 4
	FrameHeaderBitsLen       = 3
	FrameHeaderZeroPadLen    = 1
	FrameHeaderCRCLen        = 8

	FrameFooterCRCLen = 16

	EntropyCodingMethodTypeLen               = 2
	EntropyCodingMethodPartitionedRiceOrder  = 4
	EntropyCodingMethodPartitionedRiceParam  = 4
	EntropyCodingMethodPartitionedRice2Param = 5
	EntropyCodingMethodPartitionedRiceRawLen = 5

	EntropyCodingPartitionedRiceEscape  = 15 // (1<<4)-1
	EntropyCodingPartitionedRice2Escape = 31 // (1<<5)-1

	SubframeLPCQLPCoeffPrecisionLen = 4
	SubframeLPCQLPShiftLen          = 5
	SubframeZeroPadLen              = 1
	SubframeTypeLen                 = 6
	SubframeWastedBitsFlagLen       = 1

	SubframeTypeConstantByteAlignedMask = 0x00
	SubframeTypeVerbatimByteAlignedMask = 0x02
	SubframeTypeFixedByteAlignedMask    = 0x10
	SubframeTypeLPCByteAlignedMask      = 0x40
)

// FLAC predictor / partitioning bounds (FLAC/format.h:145, 148).
const (
	MaxFixedOrder         = 4
	MaxRicePartitionOrder = 15
)

// FormatSampleRateIsValid — port of FLAC__format_sample_rate_is_valid
// (format.c:209). True iff sr fits in the 20-bit STREAMINFO field.
func FormatSampleRateIsValid(sr uint32) bool {
	return sr <= MaxSampleRate
}

// FormatBlocksizeIsSubset — port of FLAC__format_blocksize_is_subset
// (format.c:218). The "subset" sub-format restricts blocksize so a
// streaming decoder can run with bounded buffers (RFC 9639 §11.2).
func FormatBlocksizeIsSubset(blocksize, sampleRate uint32) bool {
	if blocksize > 16384 {
		return false
	}
	if sampleRate <= 48000 && blocksize > 4608 {
		return false
	}
	return true
}

// FormatSampleRateIsSubset — port of FLAC__format_sample_rate_is_subset
// (format.c:228). Subset streams have sample rates that fit in the
// 16-bit explicit field (or are >= 65536 but divisible by 10).
func FormatSampleRateIsSubset(sr uint32) bool {
	if !FormatSampleRateIsValid(sr) {
		return false
	}
	if sr >= (1<<16)*10 { // >= 655360
		return false
	}
	if sr >= (1<<16) && sr%10 != 0 {
		return false
	}
	return true
}

// MaxRicePartitionOrderFromBlocksize — port of
// FLAC__format_get_max_rice_partition_order_from_blocksize (format.c:540).
// Returns the largest partition order such that blocksize is divisible
// by 2^order, capped at MaxRicePartitionOrder. Used when choosing
// rice partitions for an encoded subframe.
func MaxRicePartitionOrderFromBlocksize(blocksize uint32) uint32 {
	var order uint32
	for blocksize&1 == 0 {
		order++
		blocksize >>= 1
	}
	if order > MaxRicePartitionOrder {
		return MaxRicePartitionOrder
	}
	return order
}

// MaxRicePartitionOrderFromBlocksizeLimited — port of
// FLAC__format_get_max_rice_partition_order_from_blocksize_limited_max_and_predictor_order
// (format.c:550). Caps the partition order so each partition still
// holds enough samples for the predictor warm-up.
func MaxRicePartitionOrderFromBlocksizeLimited(limit, blocksize, predictorOrder uint32) uint32 {
	order := limit
	for order > 0 && (blocksize>>order) <= predictorOrder {
		order--
	}
	return order
}

// utf8Len — port of utf8len_ (format.c:322). Returns the number of
// bytes in the leading UTF-8 codepoint of s, or 0 if invalid /
// overlong / a surrogate / a non-character. Caller guarantees s is
// non-empty.
//
// Pure UTF-8 length without decoding to a rune; matches libFLAC's
// rejection rules byte-for-byte (overlong sequences, surrogate
// halves, U+FFFE/U+FFFF). The Go stdlib's utf8.RuneLen / DecodeRune
// rejects different things (e.g. overlongs are mapped to RuneError
// with len 1), so we keep the explicit port.
func utf8Len(s []byte) uint32 {
	if len(s) == 0 {
		return 0
	}
	switch {
	case s[0]&0x80 == 0:
		return 1
	case s[0]&0xE0 == 0xC0:
		if len(s) < 2 || s[1]&0xC0 != 0x80 {
			return 0
		}
		if s[0]&0xFE == 0xC0 { // overlong
			return 0
		}
		return 2
	case s[0]&0xF0 == 0xE0:
		if len(s) < 3 || s[1]&0xC0 != 0x80 || s[2]&0xC0 != 0x80 {
			return 0
		}
		if s[0] == 0xE0 && s[1]&0xE0 == 0x80 { // overlong
			return 0
		}
		if s[0] == 0xED && s[1]&0xE0 == 0xA0 { // surrogate
			return 0
		}
		if s[0] == 0xEF && s[1] == 0xBF && s[2]&0xFE == 0xBE { // FFFE/FFFF
			return 0
		}
		return 3
	case s[0]&0xF8 == 0xF0:
		if len(s) < 4 || s[1]&0xC0 != 0x80 || s[2]&0xC0 != 0x80 || s[3]&0xC0 != 0x80 {
			return 0
		}
		if s[0] == 0xF0 && s[1]&0xF0 == 0x80 { // overlong
			return 0
		}
		return 4
	case s[0]&0xFC == 0xF8:
		if len(s) < 5 || s[1]&0xC0 != 0x80 || s[2]&0xC0 != 0x80 || s[3]&0xC0 != 0x80 || s[4]&0xC0 != 0x80 {
			return 0
		}
		if s[0] == 0xF8 && s[1]&0xF8 == 0x80 { // overlong
			return 0
		}
		return 5
	case s[0]&0xFE == 0xFC:
		if len(s) < 6 || s[1]&0xC0 != 0x80 || s[2]&0xC0 != 0x80 || s[3]&0xC0 != 0x80 || s[4]&0xC0 != 0x80 || s[5]&0xC0 != 0x80 {
			return 0
		}
		if s[0] == 0xFC && s[1]&0xFC == 0x80 { // overlong
			return 0
		}
		return 6
	}
	return 0
}

// SeekpointLength — FLAC__STREAM_METADATA_SEEKPOINT_LENGTH: each framed
// seekpoint is 18 bytes (64+64+16 bits).
const SeekpointLength = 18

// PictureType — port of the FLAC__StreamMetadata_Picture_Type enum
// (format.h:739). Only the two file-icon types are consulted by the
// encoder's metadata validation.
type PictureType uint32

const (
	pictureTypeOther            PictureType = 0
	pictureTypeFileIconStandard PictureType = 1
	pictureTypeFileIcon         PictureType = 2
)

// FormatSeektableIsLegal — port of FLAC__format_seektable_is_legal
// (format.c:242). The framed table must fit the 24-bit metadata length
// and its non-placeholder sample numbers must strictly increase.
func FormatSeektableIsLegal(seekTable *SeekTable) bool {
	if uint64(len(seekTable.Points))*SeekpointLength >= (1 << StreamMetadataLengthLen) {
		return false
	}
	var prevSampleNumber uint64
	gotPrev := false
	for i := range seekTable.Points {
		if gotPrev {
			if seekTable.Points[i].SampleNumber != SeekpointPlaceholder &&
				seekTable.Points[i].SampleNumber <= prevSampleNumber {
				return false
			}
		}
		prevSampleNumber = seekTable.Points[i].SampleNumber
		gotPrev = true
	}
	return true
}

// FormatSeektableSort — port of FLAC__format_seektable_sort
// (format.c:281). Sorts the seekpoints by sample number, uniquifies them
// (collapsing runs of equal non-placeholder sample numbers to the first),
// shifts the kept points to the front, fills the trailing slots with
// placeholders, and returns the count of unique points kept. The C qsort
// uses seekpoint_compare_ (format.c:269), a plain sample_number ordering.
func FormatSeektableSort(seekTable *SeekTable) uint32 {
	if len(seekTable.Points) == 0 {
		return 0
	}

	// sort the seekpoints (seekpoint_compare_).
	sort.Slice(seekTable.Points, func(a, b int) bool {
		return seekTable.Points[a].SampleNumber < seekTable.Points[b].SampleNumber
	})

	// uniquify the seekpoints.
	first := true
	var j uint32
	n := uint32(len(seekTable.Points))
	for i := uint32(0); i < n; i++ {
		if seekTable.Points[i].SampleNumber != SeekpointPlaceholder {
			if !first {
				if seekTable.Points[i].SampleNumber == seekTable.Points[j-1].SampleNumber {
					continue
				}
			}
		}
		first = false
		seekTable.Points[j] = seekTable.Points[i]
		j++
	}

	for i := j; i < n; i++ {
		seekTable.Points[i].SampleNumber = SeekpointPlaceholder
		seekTable.Points[i].StreamOffset = 0
		seekTable.Points[i].FrameSamples = 0
	}

	return j
}

// FormatCuesheetIsLegal — port of FLAC__format_cuesheet_is_legal
// (format.c:422). checkCDDASubset gates the CD-DA-specific constraints.
// The C `violation` out-parameter is dropped (the encoder only consults
// the bool result).
func FormatCuesheetIsLegal(cueSheet *CueSheet, checkCDDASubset bool) bool {
	if checkCDDASubset {
		if cueSheet.LeadIn < 2*44100 {
			return false
		}
		if cueSheet.LeadIn%588 != 0 {
			return false
		}
	}
	if cueSheet.NumTracks == 0 {
		return false
	}
	if checkCDDASubset && cueSheet.Tracks[cueSheet.NumTracks-1].Number != 170 {
		return false
	}
	for i := uint32(0); i < cueSheet.NumTracks; i++ {
		tr := &cueSheet.Tracks[i]
		if tr.Number == 0 {
			return false
		}
		if checkCDDASubset {
			if !((tr.Number >= 1 && tr.Number <= 99) || tr.Number == 170) {
				return false
			}
		}
		if checkCDDASubset && tr.Offset%588 != 0 {
			return false
		}
		if i < cueSheet.NumTracks-1 {
			if tr.NumIndices == 0 {
				return false
			}
			if tr.Indices[0].Number > 1 {
				return false
			}
		}
		for j := uint32(0); j < uint32(tr.NumIndices); j++ {
			if checkCDDASubset && tr.Indices[j].Offset%588 != 0 {
				return false
			}
			if j > 0 {
				if tr.Indices[j].Number != tr.Indices[j-1].Number+1 {
					return false
				}
			}
		}
	}
	return true
}

// FormatPictureIsLegal — port of FLAC__format_picture_is_legal
// (format.c:501). The MIME type must be printable ASCII (0x20-0x7e) and
// the description must be valid UTF-8. libFLAC scans NUL-terminated C
// strings; the Go port scans the full byte slices (callers must not
// embed a trailing NUL).
func FormatPictureIsLegal(picture *Picture) bool {
	for _, c := range picture.MimeType {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	off := 0
	for off < len(picture.Description) {
		n := utf8Len(picture.Description[off:])
		if n == 0 {
			return false
		}
		off += int(n)
	}
	return true
}

// FormatVorbisCommentEntryNameIsLegal — port of
// FLAC__format_vorbiscomment_entry_name_is_legal (format.c:363).
// Names must contain only printable ASCII (0x20..0x7d) excluding '='
// (0x3d). Empty names are legal.
func FormatVorbisCommentEntryNameIsLegal(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < 0x20 || c == 0x3D || c > 0x7D {
			return false
		}
	}
	return true
}

// FormatVorbisCommentEntryValueIsLegal — port of
// FLAC__format_vorbiscomment_entry_value_is_legal (format.c:372).
// Values must be valid UTF-8 (per libFLAC's strict utf8Len rules).
// length == 0 is legal (empty value).
func FormatVorbisCommentEntryValueIsLegal(value []byte) bool {
	off := 0
	for off < len(value) {
		n := utf8Len(value[off:])
		if n == 0 {
			return false
		}
		off += int(n)
	}
	return off == len(value)
}

// FormatVorbisCommentEntryIsLegal — port of
// FLAC__format_vorbiscomment_entry_is_legal (format.c:396). The entry
// is "NAME=VALUE" where NAME obeys the printable-ASCII rules and
// VALUE is valid UTF-8.
func FormatVorbisCommentEntryIsLegal(entry []byte) bool {
	eq := -1
	for i, b := range entry {
		if b == '=' {
			eq = i
			break
		}
		if b < 0x20 || b > 0x7D {
			return false
		}
	}
	if eq < 0 {
		return false
	}
	return FormatVorbisCommentEntryValueIsLegal(entry[eq+1:])
}
