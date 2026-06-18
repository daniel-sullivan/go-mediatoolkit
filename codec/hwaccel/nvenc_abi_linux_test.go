//go:build linux

package hwaccel

import "unsafe"

// Compile-time assertions that the hand-declared NVIDIA SDK 13.0 ABI structs
// match the C sizeof / offsetof values compiled directly from the published
// headers (nvEncodeAPI.h NVENCAPI 13.0, cuviddec.h, nvcuvid.h, and the CUDA
// driver cuda.h). A mismatch overflows a uint const and fails the build,
// catching any field/padding drift before it can corrupt the driver's view of
// a struct. The reference numbers were produced by compiling the SDK 13.0
// headers on linux/amd64 (`offsetof` / `sizeof`); see the development notes in
// nvenc_linux.go.
//
// This is the same belt-and-suspenders technique used for the kernel V4L2 ABI
// in v4l2_abi_linux_test.go: each line asserts the size/offset both ways so
// either direction of drift is a build error.
const (
	// --- NVENC structs (nvEncodeAPI.h, SDK 13.0) ---
	_ = uint(unsafe.Sizeof(nvGUID{})) - 16
	_ = 16 - uint(unsafe.Sizeof(nvGUID{}))

	_ = uint(unsafe.Sizeof(nvEncodeAPIFunctionList{})) - 2552
	_ = 2552 - uint(unsafe.Sizeof(nvEncodeAPIFunctionList{}))

	_ = uint(unsafe.Sizeof(nvEncOpenEncodeSessionExParams{})) - 1552
	_ = 1552 - uint(unsafe.Sizeof(nvEncOpenEncodeSessionExParams{}))

	_ = uint(unsafe.Sizeof(nvEncInitializeParams{})) - 1800
	_ = 1800 - uint(unsafe.Sizeof(nvEncInitializeParams{}))

	_ = uint(unsafe.Sizeof(nvencExternalMEHintCounts{})) - 16
	_ = 16 - uint(unsafe.Sizeof(nvencExternalMEHintCounts{}))

	_ = uint(unsafe.Sizeof(nvEncCreateInputBuffer{})) - 776
	_ = 776 - uint(unsafe.Sizeof(nvEncCreateInputBuffer{}))

	_ = uint(unsafe.Sizeof(nvEncCreateBitstreamBuffer{})) - 776
	_ = 776 - uint(unsafe.Sizeof(nvEncCreateBitstreamBuffer{}))

	_ = uint(unsafe.Sizeof(nvEncLockInputBuffer{})) - 1544
	_ = 1544 - uint(unsafe.Sizeof(nvEncLockInputBuffer{}))

	_ = uint(unsafe.Sizeof(nvEncLockBitstream{})) - 1544
	_ = 1544 - uint(unsafe.Sizeof(nvEncLockBitstream{}))

	_ = uint(unsafe.Sizeof(nvEncCodecPicParams{})) - 1544
	_ = 1544 - uint(unsafe.Sizeof(nvEncCodecPicParams{}))

	_ = uint(unsafe.Sizeof(nvEncPicParams{})) - 3360
	_ = 3360 - uint(unsafe.Sizeof(nvEncPicParams{}))

	// Key NVENC field offsets (load-bearing).
	_ = uint(unsafe.Offsetof(nvEncInitializeParams{}.EncodeConfig)) - 88
	_ = 88 - uint(unsafe.Offsetof(nvEncInitializeParams{}.EncodeConfig))
	_ = uint(unsafe.Offsetof(nvEncInitializeParams{}.MaxEncodeWidth)) - 96
	_ = 96 - uint(unsafe.Offsetof(nvEncInitializeParams{}.MaxEncodeWidth))
	_ = uint(unsafe.Offsetof(nvEncInitializeParams{}.TuningInfo)) - 136
	_ = 136 - uint(unsafe.Offsetof(nvEncInitializeParams{}.TuningInfo))
	_ = uint(unsafe.Offsetof(nvEncInitializeParams{}.BufferFormat)) - 140
	_ = 140 - uint(unsafe.Offsetof(nvEncInitializeParams{}.BufferFormat))

	_ = uint(unsafe.Offsetof(nvEncPicParams{}.InputTimeStamp)) - 24
	_ = 24 - uint(unsafe.Offsetof(nvEncPicParams{}.InputTimeStamp))
	_ = uint(unsafe.Offsetof(nvEncPicParams{}.InputBuffer)) - 40
	_ = 40 - uint(unsafe.Offsetof(nvEncPicParams{}.InputBuffer))
	_ = uint(unsafe.Offsetof(nvEncPicParams{}.OutputBitstream)) - 48
	_ = 48 - uint(unsafe.Offsetof(nvEncPicParams{}.OutputBitstream))
	_ = uint(unsafe.Offsetof(nvEncPicParams{}.BufferFmt)) - 64
	_ = 64 - uint(unsafe.Offsetof(nvEncPicParams{}.BufferFmt))
	_ = uint(unsafe.Offsetof(nvEncPicParams{}.PictureStruct)) - 68
	_ = 68 - uint(unsafe.Offsetof(nvEncPicParams{}.PictureStruct))
	_ = uint(unsafe.Offsetof(nvEncPicParams{}.CodecPicParams)) - 80
	_ = 80 - uint(unsafe.Offsetof(nvEncPicParams{}.CodecPicParams))
	// MEHintCountsPerBlock immediately follows the 1544-byte codec union; its
	// offset (1624) locks the union size, the highest-risk field in this file.
	_ = uint(unsafe.Offsetof(nvEncPicParams{}.MEHintCountsPerBlock)) - 1624
	_ = 1624 - uint(unsafe.Offsetof(nvEncPicParams{}.MEHintCountsPerBlock))
	_ = uint(unsafe.Offsetof(nvEncPicParams{}.OutputReconBuffer)) - 1760
	_ = 1760 - uint(unsafe.Offsetof(nvEncPicParams{}.OutputReconBuffer))

	_ = uint(unsafe.Offsetof(nvEncLockBitstream{}.OutputBitstream)) - 8
	_ = 8 - uint(unsafe.Offsetof(nvEncLockBitstream{}.OutputBitstream))
	_ = uint(unsafe.Offsetof(nvEncLockBitstream{}.BitstreamSizeInBytes)) - 36
	_ = 36 - uint(unsafe.Offsetof(nvEncLockBitstream{}.BitstreamSizeInBytes))
	_ = uint(unsafe.Offsetof(nvEncLockBitstream{}.OutputTimeStamp)) - 40
	_ = 40 - uint(unsafe.Offsetof(nvEncLockBitstream{}.OutputTimeStamp))
	_ = uint(unsafe.Offsetof(nvEncLockBitstream{}.BitstreamBufferPtr)) - 56
	_ = 56 - uint(unsafe.Offsetof(nvEncLockBitstream{}.BitstreamBufferPtr))
	_ = uint(unsafe.Offsetof(nvEncLockBitstream{}.PictureType)) - 64
	_ = 64 - uint(unsafe.Offsetof(nvEncLockBitstream{}.PictureType))

	_ = uint(unsafe.Offsetof(nvEncCreateInputBuffer{}.BufferFmt)) - 16
	_ = 16 - uint(unsafe.Offsetof(nvEncCreateInputBuffer{}.BufferFmt))
	_ = uint(unsafe.Offsetof(nvEncCreateInputBuffer{}.InputBuffer)) - 24
	_ = 24 - uint(unsafe.Offsetof(nvEncCreateInputBuffer{}.InputBuffer))

	_ = uint(unsafe.Offsetof(nvEncCreateBitstreamBuffer{}.BitstreamBuffer)) - 16
	_ = 16 - uint(unsafe.Offsetof(nvEncCreateBitstreamBuffer{}.BitstreamBuffer))

	_ = uint(unsafe.Offsetof(nvEncLockInputBuffer{}.BufferDataPtr)) - 16
	_ = 16 - uint(unsafe.Offsetof(nvEncLockInputBuffer{}.BufferDataPtr))
	_ = uint(unsafe.Offsetof(nvEncLockInputBuffer{}.Pitch)) - 24
	_ = 24 - uint(unsafe.Offsetof(nvEncLockInputBuffer{}.Pitch))

	// --- CUDA driver struct (cuda.h) ---
	_ = uint(unsafe.Sizeof(cudaMemcpy2D{})) - 128
	_ = 128 - uint(unsafe.Sizeof(cudaMemcpy2D{}))
	_ = uint(unsafe.Offsetof(cudaMemcpy2D{}.SrcMemoryType)) - 16
	_ = 16 - uint(unsafe.Offsetof(cudaMemcpy2D{}.SrcMemoryType))
	_ = uint(unsafe.Offsetof(cudaMemcpy2D{}.SrcDevice)) - 32
	_ = 32 - uint(unsafe.Offsetof(cudaMemcpy2D{}.SrcDevice))
	_ = uint(unsafe.Offsetof(cudaMemcpy2D{}.DstMemoryType)) - 72
	_ = 72 - uint(unsafe.Offsetof(cudaMemcpy2D{}.DstMemoryType))
	_ = uint(unsafe.Offsetof(cudaMemcpy2D{}.DstDevice)) - 88
	_ = 88 - uint(unsafe.Offsetof(cudaMemcpy2D{}.DstDevice))
	_ = uint(unsafe.Offsetof(cudaMemcpy2D{}.WidthInBytes)) - 112
	_ = 112 - uint(unsafe.Offsetof(cudaMemcpy2D{}.WidthInBytes))

	// --- cuvid (NVDEC) structs (cuviddec.h / nvcuvid.h) ---
	_ = uint(unsafe.Sizeof(cuvidParserParams{})) - 136
	_ = 136 - uint(unsafe.Sizeof(cuvidParserParams{}))
	_ = uint(unsafe.Sizeof(cuvidSourceDataPacket{})) - 32
	_ = 32 - uint(unsafe.Sizeof(cuvidSourceDataPacket{}))
	_ = uint(unsafe.Sizeof(cuvideoFormat{})) - 64
	_ = 64 - uint(unsafe.Sizeof(cuvideoFormat{}))
	_ = uint(unsafe.Sizeof(cuvidParserDispInfo{})) - 24
	_ = 24 - uint(unsafe.Sizeof(cuvidParserDispInfo{}))
	_ = uint(unsafe.Sizeof(cuvidDecodeCreateInfo{})) - 176
	_ = 176 - uint(unsafe.Sizeof(cuvidDecodeCreateInfo{}))
	_ = uint(unsafe.Sizeof(cuvidProcParams{})) - 264
	_ = 264 - uint(unsafe.Sizeof(cuvidProcParams{}))
	_ = uint(unsafe.Sizeof(cuvidPicParams{})) - 4280
	_ = 4280 - uint(unsafe.Sizeof(cuvidPicParams{}))

	// Key cuvid field offsets (load-bearing).
	_ = uint(unsafe.Offsetof(cuvidParserParams{}.PUserData)) - 40
	_ = 40 - uint(unsafe.Offsetof(cuvidParserParams{}.PUserData))
	_ = uint(unsafe.Offsetof(cuvidParserParams{}.PfnSequenceCallback)) - 48
	_ = 48 - uint(unsafe.Offsetof(cuvidParserParams{}.PfnSequenceCallback))

	_ = uint(unsafe.Offsetof(cuvidSourceDataPacket{}.Payload)) - 16
	_ = 16 - uint(unsafe.Offsetof(cuvidSourceDataPacket{}.Payload))
	_ = uint(unsafe.Offsetof(cuvidSourceDataPacket{}.Timestamp)) - 24
	_ = 24 - uint(unsafe.Offsetof(cuvidSourceDataPacket{}.Timestamp))

	_ = uint(unsafe.Offsetof(cuvideoFormat{}.BitDepthLumaMinus8)) - 13
	_ = 13 - uint(unsafe.Offsetof(cuvideoFormat{}.BitDepthLumaMinus8))
	_ = uint(unsafe.Offsetof(cuvideoFormat{}.MinNumDecodeSurfaces)) - 15
	_ = 15 - uint(unsafe.Offsetof(cuvideoFormat{}.MinNumDecodeSurfaces))
	_ = uint(unsafe.Offsetof(cuvideoFormat{}.CodedWidth)) - 16
	_ = 16 - uint(unsafe.Offsetof(cuvideoFormat{}.CodedWidth))
	_ = uint(unsafe.Offsetof(cuvideoFormat{}.CodedHeight)) - 20
	_ = 20 - uint(unsafe.Offsetof(cuvideoFormat{}.CodedHeight))
	_ = uint(unsafe.Offsetof(cuvideoFormat{}.DisplayAreaLeft)) - 24
	_ = 24 - uint(unsafe.Offsetof(cuvideoFormat{}.DisplayAreaLeft))
	_ = uint(unsafe.Offsetof(cuvideoFormat{}.ChromaFormat)) - 40
	_ = 40 - uint(unsafe.Offsetof(cuvideoFormat{}.ChromaFormat))

	_ = uint(unsafe.Offsetof(cuvidDecodeCreateInfo{}.DisplayAreaLeft)) - 80
	_ = 80 - uint(unsafe.Offsetof(cuvidDecodeCreateInfo{}.DisplayAreaLeft))
	_ = uint(unsafe.Offsetof(cuvidDecodeCreateInfo{}.OutputFormat)) - 88
	_ = 88 - uint(unsafe.Offsetof(cuvidDecodeCreateInfo{}.OutputFormat))
	_ = uint(unsafe.Offsetof(cuvidDecodeCreateInfo{}.UlTargetWidth)) - 96
	_ = 96 - uint(unsafe.Offsetof(cuvidDecodeCreateInfo{}.UlTargetWidth))
	_ = uint(unsafe.Offsetof(cuvidDecodeCreateInfo{}.UlNumOutputSurfaces)) - 112
	_ = 112 - uint(unsafe.Offsetof(cuvidDecodeCreateInfo{}.UlNumOutputSurfaces))
	_ = uint(unsafe.Offsetof(cuvidDecodeCreateInfo{}.VidLock)) - 120
	_ = 120 - uint(unsafe.Offsetof(cuvidDecodeCreateInfo{}.VidLock))

	_ = uint(unsafe.Offsetof(cuvidProcParams{}.RawOutputDptr)) - 40
	_ = 40 - uint(unsafe.Offsetof(cuvidProcParams{}.RawOutputDptr))
	_ = uint(unsafe.Offsetof(cuvidProcParams{}.OutputStream)) - 56
	_ = 56 - uint(unsafe.Offsetof(cuvidProcParams{}.OutputStream))

	_ = uint(unsafe.Offsetof(cuvidPicParams{}.CurrPicIdx)) - 8
	_ = 8 - uint(unsafe.Offsetof(cuvidPicParams{}.CurrPicIdx))
	_ = uint(unsafe.Offsetof(cuvidPicParams{}.NBitstreamDataLen)) - 24
	_ = 24 - uint(unsafe.Offsetof(cuvidPicParams{}.NBitstreamDataLen))
	_ = uint(unsafe.Offsetof(cuvidPicParams{}.PBitstreamData)) - 32
	_ = 32 - uint(unsafe.Offsetof(cuvidPicParams{}.PBitstreamData))
	_ = uint(unsafe.Offsetof(cuvidPicParams{}.CodecSpecific)) - 184
	_ = 184 - uint(unsafe.Offsetof(cuvidPicParams{}.CodecSpecific))
)
