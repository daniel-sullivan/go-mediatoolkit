// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

// This file is the interface surface for the ADTS frame-sync-parse area: the
// transport-decoder error enum, the ADTS sync/header constants, and the parsed
// header structs. Implementations live in the feature files (adts_parse.go for
// the field parse and sync search, tp_data.go for the integer tables). 1:1 port
// of libfdk/libMpegTPDec/src/tpdec_adts.{h,cpp} and the relevant slice of
// libfdk/libMpegTPDec/src/tpdec_lib.cpp.

package nativeaac

// transportDecError mirrors the TRANSPORTDEC_ERROR enum in
// libfdk/libMpegTPDec/include/tpdec_lib.h:110. Only the values the
// frame-sync-parse slice can return are exercised here; the full enum is
// reproduced so values match the C reference exactly.
type transportDecError int

const (
	// transportDecOK : all fine. tpdec_lib.h:111.
	transportDecOK transportDecError = 0

	// tpdecSyncErrorStart marks the synchronization-error band. tpdec_lib.h:114.
	tpdecSyncErrorStart transportDecError = 0x100
	// transportDecNotEnoughBits : out of bits; provide more and retry.
	// tpdec_lib.h:115.
	transportDecNotEnoughBits transportDecError = 0x101
	// transportDecSyncError : no sync found or sync lost. tpdec_lib.h:117.
	transportDecSyncError transportDecError = 0x102

	// tpdecDecodeErrorStart marks the decode-error band. tpdec_lib.h:122.
	tpdecDecodeErrorStart transportDecError = 0x400
	// transportDecParseError : bitstream inconsistency (wrong syntax).
	// tpdec_lib.h:123.
	transportDecParseError transportDecError = 0x401
	// transportDecUnsupportedFormat : unsupported format/feature. tpdec_lib.h:125.
	transportDecUnsupportedFormat transportDecError = 0x402
	// transportDecCRCError : CRC error in bitstream data. tpdec_lib.h:127.
	transportDecCRCError transportDecError = 0x403
)

// ADTS sync/header constants. 1:1 port of the defines in
// libfdk/libMpegTPDec/src/tpdec_adts.h:108 and the field-length defines in
// tpdec_adts.cpp:149.
const (
	adtsSyncword   = 0xfff // tpdec_adts.h:108
	adtsSyncLength = 12    // tpdec_adts.h:109, in bits
	// adtsHeaderLength is the minimum header size in bits. tpdec_adts.h:110.
	adtsHeaderLength = 56
	// tpdecSyncSkip is the syncword search step in bits. tpdec_lib.cpp:1063.
	tpdecSyncSkip = 8
)

// ADTS field bit-lengths. 1:1 port of the Adts_Length_* defines in
// libfdk/libMpegTPDec/src/tpdec_adts.cpp:149.
const (
	adtsLengthSyncWord                     = 12
	adtsLengthID                           = 1
	adtsLengthLayer                        = 2
	adtsLengthProtectionAbsent             = 1
	adtsLengthProfile                      = 2
	adtsLengthSamplingFrequencyIndex       = 4
	adtsLengthPrivateBit                   = 1
	adtsLengthChannelConfiguration         = 3
	adtsLengthOriginalCopy                 = 1
	adtsLengthHome                         = 1
	adtsLengthCopyrightIdentificationBit   = 1
	adtsLengthCopyrightIdentificationStart = 1
	adtsLengthFrameLength                  = 13
	adtsLengthBufferFullness               = 11
	adtsLengthNumberOfRawDataBlocksInFrame = 2
	adtsLengthCrcCheck                     = 16
)

// adtsBS holds the parsed ADTS header fields. 1:1 port of STRUCT_ADTS_BS in
// libfdk/libMpegTPDec/src/tpdec_adts.h:122. Field types are widened to Go
// integers; the values are bit-identical to the C fixed-width fields.
type adtsBS struct {
	mpegID           uint8  // mpeg_id
	layer            uint8  // layer
	protectionAbsent uint8  // protection_absent
	profile          uint8  // profile
	sampleFreqIndex  uint8  // sample_freq_index
	privateBit       uint8  // private_bit
	channelConfig    uint8  // channel_config
	original         uint8  // original
	home             uint8  // home
	copyrightID      uint8  // copyright_id
	copyrightStart   uint8  // copyright_start
	frameLength      uint16 // frame_length
	adtsFullness     uint16 // adts_fullness
	numRawBlocks     uint8  // num_raw_blocks
	numPceBits       uint8  // num_pce_bits
}

// adts holds the persistent ADTS parser state. 1:1 port of struct STRUCT_ADTS in
// libfdk/libMpegTPDec/src/tpdec_adts.h:141. The CRC sub-state (crcInfo,
// crcReadValue) belongs to the separate crc area and is omitted from this slice;
// rawDataBlockDist is the raw-data-block distance table consulted by
// getRawDataBlockLength.
type adts struct {
	bs adtsBS // bs

	decoderCanDoMpeg4       uint8 // decoderCanDoMpeg4
	bufferFullnessStartFlag uint8 // BufferFullnesStartFlag

	rawDataBlockDist [4]uint16 // rawDataBlockDist
}
