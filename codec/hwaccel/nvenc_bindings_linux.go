//go:build linux

// Low-level purego bindings to the three NVIDIA userspace libraries the
// nvenc backend drives, with no cgo: every symbol is resolved with
// purego.Dlopen + RegisterLibFunc, and the parser callbacks with
// purego.NewCallback. The libraries (libcuda.so.1, libnvidia-encode.so.1,
// libnvcuvid.so.1) are dlopen'd once at first use and never released; they
// are installed as part of the NVIDIA display driver, so a host with no
// driver fails the dlopen cleanly (see loadNVENC -> ErrHardwareUnavailable).
//
//	libcuda.so.1          — CUDA Driver API: device + context management and
//	                        the device-memory staging used to upload NV12
//	                        frames and read decoded surfaces back.
//	libnvidia-encode.so.1 — the NVENCODE API. The single exported entry,
//	                        NvEncodeAPICreateInstance, fills an
//	                        NV_ENCODE_API_FUNCTION_LIST function-pointer table;
//	                        every other encode call goes through that table.
//	libnvcuvid.so.1       — the NVDEC / cuvid API: a bitstream parser that
//	                        drives decode through three C callbacks plus the
//	                        decoder object that produces NV12 surfaces.
//
// # ABI notes
//
// The structs below mirror the NVIDIA Video Codec SDK 13.0 headers
// (nvEncodeAPI.h NVENCAPI 13.0, cuviddec.h, nvcuvid.h, and the CUDA driver
// cuda.h) BYTE-FOR-BYTE: field order, integer widths (note CUdeviceptr and
// the cuvid `unsigned long` fields are 8 bytes on 64-bit Linux; the CUDA
// driver enums are 4-byte ints; NVENC GUIDs are the 16-byte Microsoft GUID),
// the C-bitfield words (Go has no bitfields, so each run of C bitfields is
// mirrored as the backing uint32 word and assembled with shifts), array
// extents, and the large trailing reserved padding that version-stamps each
// struct. The struct sizes are pinned in nvenc_abi_linux_test.go against the
// values compiled from the published SDK 13.0 headers; a layout mismatch
// silently corrupts the driver's view, so these are not to be "tidied".
//
// Each NVENC struct carries a `version` word built with NVENCAPI_STRUCT_VERSION
// (see nvencStructVersion); the cuvid/CUDA structs are unversioned. The
// version macro is SDK-version-sensitive — these constants target SDK 13.0
// (NVENCAPI 13.0). See the header comment in nvenc_linux.go for the
// hardware-verification status.

package hwaccel

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ---- version stamping (nvEncodeAPI.h, SDK 13.0) ----------------------

// NVENC API version constants from nvEncodeAPI.h for SDK 13.0:
//
//	#define NVENCAPI_MAJOR_VERSION 13
//	#define NVENCAPI_MINOR_VERSION 0
//	#define NVENCAPI_VERSION (MAJOR | (MINOR << 24))
const (
	nvencAPIMajorVersion = 13
	nvencAPIMinorVersion = 0
	// nvencAPIVersion == 0x0000000d for SDK 13.0.
	nvencAPIVersion = nvencAPIMajorVersion | (nvencAPIMinorVersion << 24)
)

// nvencStructVersion mirrors the NVENCAPI_STRUCT_VERSION(ver) macro:
//
//	((uint32_t)NVENCAPI_VERSION | ((ver)<<16) | (0x7 << 28))
//
// Some structs additionally OR in (1u<<31); those callers add it explicitly
// (see the *Ver constants below) to match the header's exact value.
func nvencStructVersion(ver uint32) uint32 {
	return uint32(nvencAPIVersion) | (ver << 16) | (0x7 << 28)
}

// Per-struct version words, computed exactly as the SDK 13.0 header macros
// (the (1u<<31) bit is folded in here where the header sets it). Pinned by
// nvenc_abi_linux_test.go.
var (
	nvFunctionListVer       = nvencStructVersion(2)             // 0x7002000d
	nvOpenSessionExVer      = nvencStructVersion(1)             // 0x7001000d
	nvInitializeParamsVer   = nvencStructVersion(7) | (1 << 31) // 0xf007000d
	nvConfigVer             = nvencStructVersion(9) | (1 << 31) // 0xf009000d
	nvPicParamsVer          = nvencStructVersion(7) | (1 << 31) // 0xf007000d
	nvCreateInputBufferVer  = nvencStructVersion(2)             // 0x7002000d
	nvCreateBitstreamBufVer = nvencStructVersion(1)             // 0x7001000d
	nvLockBitstreamVer      = nvencStructVersion(2) | (1 << 31) // 0xf002000d
	nvLockInputBufferVer    = nvencStructVersion(1)             // 0x7001000d
)

// NVENC scalar enum constants (nvEncodeAPI.h).
const (
	nvEncSuccess int32 = 0 // NV_ENC_SUCCESS

	nvEncDeviceTypeCUDA uint32 = 0x1 // NV_ENC_DEVICE_TYPE_CUDA

	nvEncBufferFormatNV12 uint32 = 0x00000001 // NV_ENC_BUFFER_FORMAT_NV12

	// NV_ENC_PIC_STRUCT
	nvEncPicStructFrame uint32 = 0x01

	// NV_ENC_PIC_TYPE
	nvEncPicTypeIDR uint32 = 0x03

	// NV_ENC_PIC_FLAGS
	nvEncPicFlagForceIDR     uint32 = 0x2
	nvEncPicFlagOutputSPSPPS uint32 = 0x4
	nvEncPicFlagEOS          uint32 = 0x8

	// NV_ENC_TUNING_INFO
	nvEncTuningInfoHighQuality uint32 = 1
	nvEncTuningInfoLowLatency  uint32 = 2
	nvEncTuningInfoLossless    uint32 = 4
)

// nvGUID mirrors the 16-byte Microsoft GUID used to identify codecs, presets
// and profiles (nvEncodeAPI.h: uint32 Data1; uint16 Data2; uint16 Data3;
// uint8 Data4[8]).
type nvGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]uint8
}

func (g nvGUID) equal(o nvGUID) bool { return g == o }

// Codec / preset / profile GUIDs, byte-for-byte from nvEncodeAPI.h SDK 13.0.
var (
	nvEncCodecH264GUID = nvGUID{0x6bc82762, 0x4e63, 0x4ca4, [8]uint8{0xaa, 0x85, 0x1e, 0x50, 0xf3, 0x21, 0xf6, 0xbf}}
	nvEncCodecHEVCGUID = nvGUID{0x790cdc88, 0x4522, 0x4d7b, [8]uint8{0x94, 0x25, 0xbd, 0xa9, 0x97, 0x5f, 0x76, 0x03}}
	// NV_ENC_CODEC_AV1_GUID — AV1 encode (Ada / Ada-Lovelace NVENC and later).
	nvEncCodecAV1GUID = nvGUID{0x0a352289, 0x0aa7, 0x4759, [8]uint8{0x86, 0x2d, 0x5d, 0x15, 0xcd, 0x16, 0xd2, 0x54}}

	// Preset P4 is the SDK's balanced "medium" preset in the P1..P7 scale.
	nvEncPresetP4GUID = nvGUID{0x90a7b826, 0xdf06, 0x4862, [8]uint8{0xb9, 0xd2, 0xcd, 0x6d, 0x73, 0xa0, 0x86, 0x81}}

	nvEncH264ProfileBaselineGUID = nvGUID{0x0727bcaa, 0x78c4, 0x4c83, [8]uint8{0x8c, 0x2f, 0xef, 0x3d, 0xff, 0x26, 0x7c, 0x6a}}
	nvEncH264ProfileMainGUID     = nvGUID{0x60b5c1d4, 0x67fe, 0x4790, [8]uint8{0x94, 0xd5, 0xc4, 0x72, 0x6d, 0x7b, 0x6e, 0x6d}}
	nvEncH264ProfileHighGUID     = nvGUID{0xe7cbc309, 0x4f7a, 0x4b89, [8]uint8{0xaf, 0x2a, 0xd5, 0x37, 0xc9, 0x2b, 0xe3, 0x10}}
	nvEncHEVCProfileMainGUID     = nvGUID{0xb514c39a, 0xb55b, 0x40fa, [8]uint8{0x87, 0x8f, 0xf1, 0x25, 0x3b, 0x4d, 0xfd, 0xec}}
	nvEncHEVCProfileMain10GUID   = nvGUID{0xfa4d2b6c, 0x3a5b, 0x411a, [8]uint8{0x80, 0x18, 0x0a, 0x3f, 0x5e, 0x3c, 0x9b, 0xe5}}
	// NV_ENC_AV1_PROFILE_MAIN_GUID — the only AV1 profile NVENC exposes.
	nvEncAV1ProfileMainGUID = nvGUID{0x5f2a39f5, 0xf14e, 0x4f95, [8]uint8{0x9a, 0x9e, 0xb7, 0x6d, 0x56, 0x8f, 0xcf, 0x97}}
)

// ---- NVENC structs (nvEncodeAPI.h, SDK 13.0) -------------------------

// nvEncodeAPIFunctionList mirrors NV_ENCODE_API_FUNCTION_LIST: a version word,
// a reserved word, then the encode entry-point pointers in header order,
// reserved1, the recon/MV/ME entries, the SDK-13 additions, and a large
// reserved2[275] tail. NvEncodeAPICreateInstance fills the pointers. Total
// size 2552 bytes (pinned). Each function pointer is read out and bound with
// purego.NewCallFnPtr-style invocation via a registered Go func value.
type nvEncodeAPIFunctionList struct {
	Version  uint32
	Reserved uint32

	NvEncOpenEncodeSession         uintptr
	NvEncGetEncodeGUIDCount        uintptr
	NvEncGetEncodeProfileGUIDCount uintptr
	NvEncGetEncodeProfileGUIDs     uintptr
	NvEncGetEncodeGUIDs            uintptr
	NvEncGetInputFormatCount       uintptr
	NvEncGetInputFormats           uintptr
	NvEncGetEncodeCaps             uintptr
	NvEncGetEncodePresetCount      uintptr
	NvEncGetEncodePresetGUIDs      uintptr
	NvEncGetEncodePresetConfig     uintptr
	NvEncInitializeEncoder         uintptr
	NvEncCreateInputBuffer         uintptr
	NvEncDestroyInputBuffer        uintptr
	NvEncCreateBitstreamBuffer     uintptr
	NvEncDestroyBitstreamBuffer    uintptr
	NvEncEncodePicture             uintptr
	NvEncLockBitstream             uintptr
	NvEncUnlockBitstream           uintptr
	NvEncLockInputBuffer           uintptr
	NvEncUnlockInputBuffer         uintptr
	NvEncGetEncodeStats            uintptr
	NvEncGetSequenceParams         uintptr
	NvEncRegisterAsyncEvent        uintptr
	NvEncUnregisterAsyncEvent      uintptr
	NvEncMapInputResource          uintptr
	NvEncUnmapInputResource        uintptr
	NvEncDestroyEncoder            uintptr
	NvEncInvalidateRefFrames       uintptr
	NvEncOpenEncodeSessionEx       uintptr
	NvEncRegisterResource          uintptr
	NvEncUnregisterResource        uintptr
	NvEncReconfigureEncoder        uintptr
	Reserved1                      uintptr
	NvEncCreateMVBuffer            uintptr
	NvEncDestroyMVBuffer           uintptr
	NvEncRunMotionEstimationOnly   uintptr
	NvEncGetLastErrorString        uintptr
	NvEncSetIOCudaStreams          uintptr
	NvEncGetEncodePresetConfigEx   uintptr
	NvEncGetSequenceParamEx        uintptr
	NvEncRestoreEncoderState       uintptr
	NvEncLookaheadPicture          uintptr

	Reserved2 [275]uintptr
}

// nvEncOpenEncodeSessionExParams mirrors NV_ENC_OPEN_ENCODE_SESSION_EX_PARAMS
// (1552 bytes). DeviceType is NV_ENC_DEVICE_TYPE_CUDA; Device is the CUcontext.
type nvEncOpenEncodeSessionExParams struct {
	Version    uint32
	DeviceType uint32 // NV_ENC_DEVICE_TYPE
	Device     uintptr
	Reserved   uintptr
	APIVersion uint32
	Reserved1  [253]uint32
	Reserved2  [64]uintptr
}

// nvEncInitializeParams mirrors NV_ENC_INITIALIZE_PARAMS (1800 bytes). The
// run of C bitfields after enablePTD is mirrored as BitFields (one uint32);
// MaxMEHintCountsPerBlock is two 16-byte ME-hint-count blocks (NVENC packs
// each as a single uint32 bitfield word + a uint32[3] reserved tail).
type nvEncInitializeParams struct {
	Version      uint32
	EncodeGUID   nvGUID
	PresetGUID   nvGUID
	EncodeWidth  uint32
	EncodeHeight uint32
	DARWidth     uint32
	DARHeight    uint32
	FrameRateNum uint32
	FrameRateDen uint32

	EnableEncodeAsync uint32
	EnablePTD         uint32
	BitFields         uint32 // reportSliceOffsets:1 .. reservedBitFields:19
	PrivDataSize      uint32
	Reserved          uint32
	PrivData          uintptr
	EncodeConfig      uintptr // NV_ENC_CONFIG* (NULL => use presetGUID)
	MaxEncodeWidth    uint32
	MaxEncodeHeight   uint32

	MaxMEHintCountsPerBlock [2]nvencExternalMEHintCounts
	TuningInfo              uint32 // NV_ENC_TUNING_INFO
	BufferFormat            uint32 // NV_ENC_BUFFER_FORMAT
	NumStateBuffers         uint32
	OutputStatsLevel        uint32 // NV_ENC_OUTPUT_STATS_LEVEL

	Reserved1 [284]uint32
	Reserved2 [64]uintptr
}

// nvencExternalMEHintCounts mirrors NVENC_EXTERNAL_ME_HINT_COUNTS_PER_BLOCKTYPE
// (16 bytes): one uint32 bitfield word followed by uint32[3] reserved.
type nvencExternalMEHintCounts struct {
	BitFields uint32 // numCandsPerBlk16x16:4 .. reserved:8
	Reserved1 [3]uint32
}

// nvEncCreateInputBuffer mirrors NV_ENC_CREATE_INPUT_BUFFER (776 bytes).
// InputBuffer is filled on return (NV_ENC_INPUT_PTR).
type nvEncCreateInputBuffer struct {
	Version       uint32
	Width         uint32
	Height        uint32
	MemoryHeap    uint32 // NV_ENC_MEMORY_HEAP (deprecated)
	BufferFmt     uint32 // NV_ENC_BUFFER_FORMAT
	Reserved      uint32
	InputBuffer   uintptr // [out]
	PSysMemBuffer uintptr
	Reserved1     [58]uint32
	Reserved2     [63]uintptr
}

// nvEncCreateBitstreamBuffer mirrors NV_ENC_CREATE_BITSTREAM_BUFFER (776
// bytes). BitstreamBuffer is filled on return (NV_ENC_OUTPUT_PTR).
type nvEncCreateBitstreamBuffer struct {
	Version            uint32
	Size               uint32 // deprecated
	MemoryHeap         uint32 // deprecated
	Reserved           uint32
	BitstreamBuffer    uintptr // [out]
	BitstreamBufferPtr uintptr
	Reserved1          [58]uint32
	Reserved2          [64]uintptr
}

// nvEncLockInputBuffer mirrors NV_ENC_LOCK_INPUT_BUFFER (1544 bytes).
// BufferDataPtr and Pitch are filled on return.
type nvEncLockInputBuffer struct {
	Version       uint32
	BitFields     uint32 // doNotWait:1 reservedBitFields:31
	InputBuffer   uintptr
	BufferDataPtr unsafe.Pointer // [out] mapped CPU pointer into the input buffer
	Pitch         uint32         // [out]
	Reserved1     [251]uint32
	Reserved2     [64]uintptr
}

// nvEncLockBitstream mirrors NV_ENC_LOCK_BITSTREAM (1544 bytes). The fields
// up to bitstreamBufferPtr are the load-bearing ones we read; the remainder
// is faithfully reproduced so the struct size and the driver's writes land
// where expected.
type nvEncLockBitstream struct {
	Version               uint32
	BitFields             uint32         // doNotWait:1 ltrFrame:1 getRCStats:1 reservedBitFields:29
	OutputBitstream       uintptr        // [in]
	SliceOffsets          uintptr        // [in,out] uint32*
	FrameIdx              uint32         // [out]
	HWEncodeStatus        uint32         // [out]
	NumSlices             uint32         // [out]
	BitstreamSizeInBytes  uint32         // [out]
	OutputTimeStamp       uint64         // [out]
	OutputDuration        uint64         // [out]
	BitstreamBufferPtr    unsafe.Pointer // [out] CPU pointer to the generated bitstream
	PictureType           uint32         // [out] NV_ENC_PIC_TYPE
	PictureStruct         uint32         // [out] NV_ENC_PIC_STRUCT
	FrameAvgQP            uint32         // [out]
	FrameSatd             uint32         // [out]
	LTRFrameIdx           uint32         // [out]
	LTRFrameBitmap        uint32         // [out]
	TemporalID            uint32         // [out]
	IntraMBCount          uint32         // [out]
	InterMBCount          uint32         // [out]
	AverageMVX            int32          // [out]
	AverageMVY            int32          // [out]
	AlphaLayerSizeInBytes uint32
	OutputStatsPtrSize    uint32
	Reserved              uint32
	OutputStatsPtr        uintptr
	FrameIdxDisplay       uint32
	Reserved1             [219]uint32
	Reserved2             [63]uintptr
	ReservedInternal      [8]uint32
}

// nvEncCodecPicParams mirrors the NV_ENC_CODEC_PIC_PARAMS union. The union's
// size is that of its largest member (the AV1 pic params, 1544 bytes) — NOT
// the uint32[256] (1024-byte) "reserved" alternative, which is merely the
// minimum. We encode all-intra IDR pictures and leave the per-codec fields
// zero (the driver fills sensible defaults), so the union is carried as opaque
// zeroed storage of the correct full size (1544 bytes). The union is 8-byte
// aligned in C (its members carry pointers), so the leading uint64 pins the
// alignment; without it Go would 4-align the union and codecPicParams would
// land 4 bytes early, shifting the whole tail of NV_ENC_PIC_PARAMS.
type nvEncCodecPicParams struct {
	_        uint64      // force 8-byte alignment to match the C union
	Reserved [192]uint64 // 8 (align word) + 192*8 == 1544 bytes
}

// nvEncPicParams mirrors NV_ENC_PIC_PARAMS (3360 bytes). The fields up to
// CodecPicParams are the ones we set; everything past it is reproduced for
// size fidelity. The two ME-hint-count blocks and trailing reserved arrays
// match the header exactly.
type nvEncPicParams struct {
	Version         uint32
	InputWidth      uint32
	InputHeight     uint32
	InputPitch      uint32
	EncodePicFlags  uint32
	FrameIdx        uint32
	InputTimeStamp  uint64
	InputDuration   uint64
	InputBuffer     uintptr // NV_ENC_INPUT_PTR
	OutputBitstream uintptr // NV_ENC_OUTPUT_PTR
	CompletionEvent uintptr
	BufferFmt       uint32 // NV_ENC_BUFFER_FORMAT
	PictureStruct   uint32 // NV_ENC_PIC_STRUCT
	PictureType     uint32 // NV_ENC_PIC_TYPE
	CodecPicParams  nvEncCodecPicParams

	MEHintCountsPerBlock [2]nvencExternalMEHintCounts
	MEExternalHints      uintptr
	Reserved2            [7]uint32
	Reserved5            [2]uintptr
	QPDeltaMap           uintptr
	QPDeltaMapSize       uint32
	ReservedBitFields    uint32
	MEHintRefPicDist     [2]uint16
	Reserved4            uint32
	AlphaBuffer          uintptr
	MEExternalSbHints    uintptr
	MESbHintsCount       uint32
	StateBufferIdx       uint32
	OutputReconBuffer    uintptr
	Reserved3            [284]uint32
	Reserved6            [57]uintptr
}

// nvEncCapsParam mirrors NV_ENC_CAPS_PARAM: a version word, a capsToQuery
// enum, and a reserved[62] tail. Used to query NV_ENC_CAPS_* via
// nvEncGetEncodeCaps.
type nvEncCapsParam struct {
	Version     uint32
	CapsToQuery uint32 // NV_ENC_CAPS enum
	Reserved    [62]uint32
}

// nvEncCapNumMaxBFrames etc are the NV_ENC_CAPS enum values we may query.
const nvEncCapsParamVer = 1 // NVENCAPI_STRUCT_VERSION(1) folded below

// ---- CUDA driver structs (cuda.h) ------------------------------------

// CUDA driver result code for success.
const cudaSuccess int32 = 0 // CUDA_SUCCESS

// CUmemorytype values (cuda.h).
const (
	cuMemoryTypeHost   uint32 = 1
	cuMemoryTypeDevice uint32 = 2
)

// cudaMemcpy2D mirrors CUDA_MEMCPY2D (128 bytes). The CUmemorytype enums are
// 4-byte ints each followed by 4 bytes of natural alignment padding before
// the host pointer; the device pointers are CUdeviceptr (8 bytes). Pinned by
// nvenc_abi_linux_test.go.
type cudaMemcpy2D struct {
	SrcXInBytes   uintptr
	SrcY          uintptr
	SrcMemoryType uint32
	_             uint32 // pad before srcHost
	SrcHost       uintptr
	SrcDevice     uint64 // CUdeviceptr
	SrcArray      uintptr
	SrcPitch      uintptr

	DstXInBytes   uintptr
	DstY          uintptr
	DstMemoryType uint32
	_             uint32 // pad before dstHost
	DstHost       uintptr
	DstDevice     uint64 // CUdeviceptr
	DstArray      uintptr
	DstPitch      uintptr

	WidthInBytes uintptr
	Height       uintptr
}

// ---- cuvid (NVDEC) structs (cuviddec.h / nvcuvid.h) ------------------

// cudaVideoCodec values (cuviddec.h).
const (
	cudaVideoCodecH264 uint32 = 4
	cudaVideoCodecHEVC uint32 = 8
	cudaVideoCodecVP9  uint32 = 10
	cudaVideoCodecAV1  uint32 = 11
)

// cudaVideoChromaFormat420 (cuviddec.h: 4:2:0 == 1).
const cudaVideoChromaFormat420 uint32 = 1

// cudaVideoSurfaceFormatNV12 (cuviddec.h: 0).
const cudaVideoSurfaceFormatNV12 uint32 = 0

// CUVID_PKT_* source-data-packet flags (nvcuvid.h).
const (
	cuvidPktEndOfStream  uint32 = 0x01
	cuvidPktTimestamp    uint32 = 0x02
	cuvidPktEndOfPicture uint32 = 0x04
)

// cuvidParserParams mirrors CUVIDPARSERPARAMS (136 bytes). The bitfield word
// (bAnnexb:1 bMemoryOptimize:1 uReserved:30) is BitFields. The five callback
// slots take purego.NewCallback trampolines (only the first three are used).
type cuvidParserParams struct {
	CodecType              uint32 // cudaVideoCodec
	UlMaxNumDecodeSurfaces uint32
	UlClockRate            uint32
	UlErrorThreshold       uint32
	UlMaxDisplayDelay      uint32
	BitFields              uint32 // bAnnexb:1 bMemoryOptimize:1 uReserved:30
	UReserved1             [4]uint32
	PUserData              uintptr
	PfnSequenceCallback    uintptr
	PfnDecodePicture       uintptr
	PfnDisplayPicture      uintptr
	PfnGetOperatingPoint   uintptr
	PfnGetSEIMsg           uintptr
	PvReserved2            [5]uintptr
	PExtVideoInfo          uintptr
}

// cuvidSourceDataPacket mirrors CUVIDSOURCEDATAPACKET (32 bytes). Flags and
// PayloadSize are `unsigned long` (8 bytes on 64-bit Linux).
type cuvidSourceDataPacket struct {
	Flags       uint64
	PayloadSize uint64
	Payload     uintptr // const unsigned char*
	Timestamp   int64   // CUvideotimestamp
}

// cuvideoFormat mirrors CUVIDEOFORMAT (64 bytes) — the fields the sequence
// callback reads. The signal-description and aspect-ratio sub-structs are
// reproduced as their backing words for size fidelity.
type cuvideoFormat struct {
	Codec                uint32 // cudaVideoCodec
	FrameRateNum         uint32
	FrameRateDen         uint32
	ProgressiveSequence  uint8
	BitDepthLumaMinus8   uint8
	BitDepthChromaMinus8 uint8
	MinNumDecodeSurfaces uint8
	CodedWidth           uint32
	CodedHeight          uint32
	DisplayAreaLeft      int32
	DisplayAreaTop       int32
	DisplayAreaRight     int32
	DisplayAreaBottom    int32
	ChromaFormat         uint32 // cudaVideoChromaFormat
	Bitrate              uint32
	DisplayAspectRatioX  int32
	DisplayAspectRatioY  int32
	VideoSignalDesc      uint32 // packed video_signal_description (4 bytes)
	SeqHdrDataLength     uint32
}

// cuvidParserDispInfo mirrors CUVIDPARSERDISPINFO (24 bytes): the display
// callback's picture index, field flags, and timestamp.
type cuvidParserDispInfo struct {
	PictureIndex     int32
	ProgressiveFrame int32
	TopFieldFirst    int32
	RepeatFirstField int32
	Timestamp        int64 // CUvideotimestamp
}

// cuvidDecodeCreateInfo mirrors CUVIDDECODECREATEINFO (176 bytes). The
// `unsigned long` fields are 8 bytes on 64-bit Linux; the two short[4]
// rectangles are packed as four int16s each. Pinned by the ABI test.
type cuvidDecodeCreateInfo struct {
	UlWidth             uint64
	UlHeight            uint64
	UlNumDecodeSurfaces uint64
	CodecType           uint32 // cudaVideoCodec
	ChromaFormat        uint32 // cudaVideoChromaFormat
	UlCreationFlags     uint64
	BitDepthMinus8      uint64
	UlIntraDecodeOnly   uint64
	UlMaxWidth          uint64
	UlMaxHeight         uint64
	Reserved1           uint64

	DisplayAreaLeft   int16
	DisplayAreaTop    int16
	DisplayAreaRight  int16
	DisplayAreaBottom int16

	OutputFormat        uint32 // cudaVideoSurfaceFormat
	DeinterlaceMode     uint32 // cudaVideoDeinterlaceMode
	UlTargetWidth       uint64
	UlTargetHeight      uint64
	UlNumOutputSurfaces uint64
	VidLock             uintptr // CUvideoctxlock

	TargetRectLeft   int16
	TargetRectTop    int16
	TargetRectRight  int16
	TargetRectBottom int16

	EnableHistogram uint64
	Reserved2       [4]uint64
}

// cuvidProcParams mirrors CUVIDPROCPARAMS (264 bytes), passed to
// cuvidMapVideoFrame64. We set progressive_frame and leave the rest zero.
type cuvidProcParams struct {
	ProgressiveFrame int32
	SecondField      int32
	TopFieldFirst    int32
	UnpairedField    int32
	ReservedFlags    uint32
	ReservedZero     uint32
	RawInputDptr     uint64
	RawInputPitch    uint32
	RawInputFormat   uint32
	RawOutputDptr    uint64
	RawOutputPitch   uint32
	Reserved1        uint32
	OutputStream     uintptr // CUstream
	Reserved         [46]uint32
	HistogramDptr    uintptr
	Reserved2        [1]uintptr
}

// cuvidPicParams mirrors CUVIDPICPARAMS (4280 bytes). The parser fills the
// whole struct (including the large CodecSpecific union) and hands it to the
// pfnDecodePicture callback; the decoder passes it straight to
// cuvidDecodePicture. We never author its fields — only its address flows
// through — so the codec union is carried as opaque storage of the exact
// size. CurrPicIdx is read in the callback for diagnostics.
type cuvidPicParams struct {
	PicWidthInMbs     int32
	FrameHeightInMbs  int32
	CurrPicIdx        int32
	FieldPicFlag      int32
	BottomFieldFlag   int32
	SecondField       int32
	NBitstreamDataLen uint32
	PBitstreamData    uintptr
	NNumSlices        uint32
	PSliceDataOffsets uintptr
	RefPicFlag        int32
	IntraPicFlag      int32
	Reserved          [30]uint32
	CodecSpecific     [1024]uint32
}

// ---- the bound library ------------------------------------------------

// nvLib holds every dynamically-resolved symbol the nvenc backend uses
// across the three NVIDIA libraries, plus the NVENCODE function-pointer table
// filled by NvEncodeAPICreateInstance and the through-table call wrappers.
type nvLib struct {
	// --- libcuda (CUDA Driver API) ---
	cuInit           func(flags uint32) int32
	cuDeviceGetCount func(count *int32) int32
	cuDeviceGet      func(dev *int32, ordinal int32) int32
	cuCtxCreate      func(pctx *uintptr, flags uint32, dev int32) int32
	cuCtxDestroy     func(ctx uintptr) int32
	cuCtxPushCurrent func(ctx uintptr) int32
	cuCtxPopCurrent  func(pctx *uintptr) int32
	cuMemAlloc       func(dptr *uint64, bytesize uint64) int32
	cuMemFree        func(dptr uint64) int32
	cuMemcpy2D       func(copy *cudaMemcpy2D) int32
	cuMemcpyDtoH     func(dstHost unsafe.Pointer, srcDevice uint64, byteCount uint64) int32

	// --- libnvidia-encode ---
	nvEncodeAPICreateInstance func(fnList *nvEncodeAPIFunctionList) int32
	fns                       nvEncodeAPIFunctionList

	// Through-table call wrappers, registered against the function pointers
	// the table is filled with (purego.NewCallback is not needed for these —
	// they are plain C calls we invoke through the resolved pointers).
	openSessionEx          func(params *nvEncOpenEncodeSessionExParams, encoder *uintptr) int32
	getEncodeGUIDCount     func(encoder uintptr, count *uint32) int32
	getEncodeGUIDs         func(encoder uintptr, guids *nvGUID, size uint32, count *uint32) int32
	initializeEncoder      func(encoder uintptr, params *nvEncInitializeParams) int32
	createInputBuffer      func(encoder uintptr, params *nvEncCreateInputBuffer) int32
	destroyInputBuffer     func(encoder uintptr, buf uintptr) int32
	createBitstreamBuffer  func(encoder uintptr, params *nvEncCreateBitstreamBuffer) int32
	destroyBitstreamBuffer func(encoder uintptr, buf uintptr) int32
	lockInputBuffer        func(encoder uintptr, params *nvEncLockInputBuffer) int32
	unlockInputBuffer      func(encoder uintptr, buf uintptr) int32
	encodePicture          func(encoder uintptr, params *nvEncPicParams) int32
	lockBitstream          func(encoder uintptr, params *nvEncLockBitstream) int32
	unlockBitstream        func(encoder uintptr, buf uintptr) int32
	destroyEncoder         func(encoder uintptr) int32

	// --- libnvcuvid (NVDEC / cuvid) ---
	cuvidCreateVideoParser  func(parser *uintptr, params *cuvidParserParams) int32
	cuvidParseVideoData     func(parser uintptr, packet *cuvidSourceDataPacket) int32
	cuvidDestroyVideoParser func(parser uintptr) int32
	cuvidCreateDecoder      func(decoder *uintptr, createInfo *cuvidDecodeCreateInfo) int32
	cuvidDestroyDecoder     func(decoder uintptr) int32
	cuvidDecodePicture      func(decoder uintptr, picParams *cuvidPicParams) int32
	cuvidMapVideoFrame64    func(decoder uintptr, picIdx int32, devPtr *uint64, pitch *uint32, vpp *cuvidProcParams) int32
	cuvidUnmapVideoFrame64  func(decoder uintptr, devPtr uint64) int32
	cuvidCtxLockCreate      func(lock *uintptr, ctx uintptr) int32
	cuvidCtxLockDestroy     func(lock uintptr) int32
}

var (
	nvOnce sync.Once
	nvRef  *nvLib
	nvErr  error
)

// loadNVENC dlopens libcuda.so.1, libnvidia-encode.so.1 and libnvcuvid.so.1,
// binds the CUDA + cuvid symbols directly, and calls NvEncodeAPICreateInstance
// to fill the NVENCODE function-pointer table. Memoised; the libraries are
// never dlclose'd (process-wide load). On a host with no NVIDIA driver the
// first Dlopen fails and the error propagates so the backend is not
// registered (backend_linux.go skips it) and Available() returns false.
func loadNVENC() (*nvLib, error) {
	nvOnce.Do(func() {
		cuda, err := dlopenFirst("libcuda.so.1", "libcuda.so")
		if err != nil {
			nvErr = errors.Join(errors.New("hwaccel: dlopen libcuda failed"), err)
			return
		}
		enc, err := dlopenFirst("libnvidia-encode.so.1", "libnvidia-encode.so")
		if err != nil {
			nvErr = errors.Join(errors.New("hwaccel: dlopen libnvidia-encode failed"), err)
			return
		}
		// libnvcuvid is only needed for the decode path; treat its absence as
		// non-fatal so an encode-only host (encode lib present, cuvid not)
		// still registers. A nil decode handle is checked at NewDecoder.
		cuvid, _ := dlopenFirst("libnvcuvid.so.1", "libnvcuvid.so")

		l := new(nvLib)

		purego.RegisterLibFunc(&l.cuInit, cuda, "cuInit")
		purego.RegisterLibFunc(&l.cuDeviceGetCount, cuda, "cuDeviceGetCount")
		purego.RegisterLibFunc(&l.cuDeviceGet, cuda, "cuDeviceGet")
		purego.RegisterLibFunc(&l.cuCtxCreate, cuda, "cuCtxCreate_v2")
		purego.RegisterLibFunc(&l.cuCtxDestroy, cuda, "cuCtxDestroy_v2")
		purego.RegisterLibFunc(&l.cuCtxPushCurrent, cuda, "cuCtxPushCurrent_v2")
		purego.RegisterLibFunc(&l.cuCtxPopCurrent, cuda, "cuCtxPopCurrent_v2")
		purego.RegisterLibFunc(&l.cuMemAlloc, cuda, "cuMemAlloc_v2")
		purego.RegisterLibFunc(&l.cuMemFree, cuda, "cuMemFree_v2")
		purego.RegisterLibFunc(&l.cuMemcpy2D, cuda, "cuMemcpy2D_v2")
		purego.RegisterLibFunc(&l.cuMemcpyDtoH, cuda, "cuMemcpyDtoH_v2")

		purego.RegisterLibFunc(&l.nvEncodeAPICreateInstance, enc, "NvEncodeAPICreateInstance")

		// Fill the function-pointer table.
		l.fns.Version = nvFunctionListVer
		if st := l.nvEncodeAPICreateInstance(&l.fns); st != nvEncSuccess {
			nvErr = errors.Join(errors.New("hwaccel: NvEncodeAPICreateInstance failed"),
				nvStatusError(st))
			return
		}

		// Bind the through-table wrappers to the resolved function pointers.
		purego.RegisterFunc(&l.openSessionEx, l.fns.NvEncOpenEncodeSessionEx)
		purego.RegisterFunc(&l.getEncodeGUIDCount, l.fns.NvEncGetEncodeGUIDCount)
		purego.RegisterFunc(&l.getEncodeGUIDs, l.fns.NvEncGetEncodeGUIDs)
		purego.RegisterFunc(&l.initializeEncoder, l.fns.NvEncInitializeEncoder)
		purego.RegisterFunc(&l.createInputBuffer, l.fns.NvEncCreateInputBuffer)
		purego.RegisterFunc(&l.destroyInputBuffer, l.fns.NvEncDestroyInputBuffer)
		purego.RegisterFunc(&l.createBitstreamBuffer, l.fns.NvEncCreateBitstreamBuffer)
		purego.RegisterFunc(&l.destroyBitstreamBuffer, l.fns.NvEncDestroyBitstreamBuffer)
		purego.RegisterFunc(&l.lockInputBuffer, l.fns.NvEncLockInputBuffer)
		purego.RegisterFunc(&l.unlockInputBuffer, l.fns.NvEncUnlockInputBuffer)
		purego.RegisterFunc(&l.encodePicture, l.fns.NvEncEncodePicture)
		purego.RegisterFunc(&l.lockBitstream, l.fns.NvEncLockBitstream)
		purego.RegisterFunc(&l.unlockBitstream, l.fns.NvEncUnlockBitstream)
		purego.RegisterFunc(&l.destroyEncoder, l.fns.NvEncDestroyEncoder)

		if cuvid != 0 {
			purego.RegisterLibFunc(&l.cuvidCreateVideoParser, cuvid, "cuvidCreateVideoParser")
			purego.RegisterLibFunc(&l.cuvidParseVideoData, cuvid, "cuvidParseVideoData")
			purego.RegisterLibFunc(&l.cuvidDestroyVideoParser, cuvid, "cuvidDestroyVideoParser")
			purego.RegisterLibFunc(&l.cuvidCreateDecoder, cuvid, "cuvidCreateDecoder")
			purego.RegisterLibFunc(&l.cuvidDestroyDecoder, cuvid, "cuvidDestroyDecoder")
			purego.RegisterLibFunc(&l.cuvidDecodePicture, cuvid, "cuvidDecodePicture")
			purego.RegisterLibFunc(&l.cuvidMapVideoFrame64, cuvid, "cuvidMapVideoFrame64")
			purego.RegisterLibFunc(&l.cuvidUnmapVideoFrame64, cuvid, "cuvidUnmapVideoFrame64")
			purego.RegisterLibFunc(&l.cuvidCtxLockCreate, cuvid, "cuvidCtxLockCreate")
			purego.RegisterLibFunc(&l.cuvidCtxLockDestroy, cuvid, "cuvidCtxLockDestroy")
		}

		nvRef = l
	})
	return nvRef, nvErr
}

// hasDecode reports whether libnvcuvid was dlopen'd and its symbols bound.
func (l *nvLib) hasDecode() bool { return l != nil && l.cuvidCreateVideoParser != nil }

// dlopenFirst tries each candidate soname in turn, returning the first that
// opens. It returns the last error if none open.
func dlopenFirst(names ...string) (uintptr, error) {
	var lastErr error
	for _, n := range names {
		h, err := purego.Dlopen(n, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err == nil {
			return h, nil
		}
		lastErr = err
	}
	return 0, lastErr
}
