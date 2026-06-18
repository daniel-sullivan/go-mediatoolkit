//go:build darwin

// Low-level purego bindings to Apple VideoToolbox, CoreMedia,
// CoreVideo, and CoreFoundation, used by the videotoolbox backend. No
// cgo: every symbol is resolved with purego.Dlopen + RegisterLibFunc,
// every callback with purego.NewCallback. Frameworks are dlopen'd once
// at first use and never released.
//
// # ABI notes
//
//   - CMTime is a 24-byte struct passed BY VALUE. purego supports
//     struct-by-value on darwin arm64/amd64; the Go cmTime mirrors the
//     C layout exactly (no implicit padding — the fields already align).
//   - The VTCompressionOutputCallback receives only uintptr-sized
//     arguments (two void*, an OSStatus int32, a VTEncodeInfoFlags
//     uint32, and a CMSampleBufferRef pointer), so purego.NewCallback
//     can host it. The refcon void* carries a registry index back to
//     the owning encoder (we do not pass Go pointers into C).

package hwaccel

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// CoreFoundation type constants and OSType codec tags.
const (
	// kCMVideoCodecType_H264 == 'avc1', _HEVC == 'hvc1'. FourCC values
	// from <CoreMedia/CMFormatDescription.h>. kCMVideoCodecType_VP9 == 'vp09'
	// and kCMVideoCodecType_AV1 == 'av01' (the codec tags VideoToolbox uses
	// for its VP9 / AV1 decoders; AV1 decode arrived on Apple silicon with the
	// M3 generation).
	kCMVideoCodecTypeH264 uint32 = 'a'<<24 | 'v'<<16 | 'c'<<8 | '1'
	kCMVideoCodecTypeHEVC uint32 = 'h'<<24 | 'v'<<16 | 'c'<<8 | '1'
	kCMVideoCodecTypeVP9  uint32 = 'v'<<24 | 'p'<<16 | '0'<<8 | '9'
	kCMVideoCodecTypeAV1  uint32 = 'a'<<24 | 'v'<<16 | '0'<<8 | '1'

	// CVPixelFormatType FourCCs. NV12 = '420v' (video range) — the
	// VideoToolbox-preferred bi-planar 4:2:0. I420 = 'y420' (planar).
	kCVPixelFormatType420YpCbCr8BiPlanarVideoRange uint32 = '4'<<24 | '2'<<16 | '0'<<8 | 'v'
	kCVPixelFormatTypeYpCbCr8Planar                uint32 = 'y'<<24 | '4'<<16 | '2'<<8 | '0'

	// Full-range siblings the hardware decoder may hand back: NV12 full
	// range = '420f', planar I420 full range = 'y420' has no distinct
	// full-range FourCC, but 'f420' is the full-range planar form. We map
	// both ranges to the same video.PixelFormat (range is not modelled).
	kCVPixelFormatType420YpCbCr8BiPlanarFullRange uint32 = '4'<<24 | '2'<<16 | '0'<<8 | 'f'
	kCVPixelFormatTypeYpCbCr8PlanarFullRange      uint32 = 'f'<<24 | '4'<<16 | '2'<<8 | '0'

	// CFNumber type identifiers from <CoreFoundation/CFNumber.h>.
	kCFNumberSInt32Type  int64 = 3
	kCFNumberFloat64Type int64 = 6

	// kCFBooleanTrue / kCFBooleanFalse are read as exported data
	// symbols at load time.

	// CVReturn / OSStatus success.
	noErr int32 = 0
	// kCVReturnSuccess == 0.

	// kCVPixelBufferLock_ReadOnly is passed to
	// CVPixelBufferLockBaseAddress when we only read the decoded planes.
	kCVPixelBufferLock_ReadOnly uint64 = 1

	// VTDecodeFrame flags. We run synchronous (no async output queue) so
	// the output callback fires before DecodeFrame returns: that means we
	// pass 0 (neither kVTDecodeFrame_EnableAsynchronousDecompression nor
	// _EnableTemporalProcessing), which is the simplest in-order path.
	kVTDecodeFrameFlagsSync uint32 = 0

	// kCMBlockBufferAssureMemoryNowFlag forces CMBlockBufferCreateWithMemoryBlock
	// to allocate its backing store immediately so we can copy the AU
	// bytes in without VT holding a reference to Go memory.
	kCMBlockBufferAssureMemoryNowFlag uint32 = 1 << 0
)

// cmTime mirrors CMTime: { CMTimeValue value; CMTimeScale timescale;
// CMTimeFlags flags; CMTimeEpoch epoch }. Sizes: int64, int32, uint32,
// int64 => 24 bytes, naturally aligned (no extra padding needed because
// the int32+uint32 pair fills an 8-byte slot before the trailing
// int64). Passed by value to VideoToolbox.
type cmTime struct {
	Value     int64
	Timescale int32
	Flags     uint32
	Epoch     int64
}

// kCMTimeFlagsValid marks a CMTime as carrying a real value.
const kCMTimeFlagsValid uint32 = 1 << 0

// makeCMTime builds a valid CMTime for value/timescale.
func makeCMTime(value int64, timescale int32) cmTime {
	return cmTime{Value: value, Timescale: timescale, Flags: kCMTimeFlagsValid}
}

// vtLib holds every dynamically-resolved symbol the backend uses,
// across the four frameworks.
type vtLib struct {
	// --- CoreFoundation ---
	CFRelease                 func(ref uintptr)
	CFRetain                  func(ref uintptr) uintptr
	CFDictionaryCreate        func(alloc uintptr, keys *uintptr, values *uintptr, n int64, keyCB uintptr, valCB uintptr) uintptr
	CFNumberCreate            func(alloc uintptr, theType int64, valuePtr unsafe.Pointer) uintptr
	CFStringCreateWithCString func(alloc uintptr, cstr *byte, encoding uint32) uintptr
	CFDictionaryGetValue      func(dict uintptr, key uintptr) uintptr
	CFDictionaryGetCount      func(dict uintptr) int64
	CFDictionaryContainsKey   func(dict uintptr, key uintptr) bool
	CFBooleanGetValue         func(b uintptr) bool
	CFArrayGetCount           func(arr uintptr) int64
	CFArrayGetValueAtIndex    func(arr uintptr, idx int64) uintptr
	CFDataCreate              func(alloc uintptr, bytes *byte, length int64) uintptr

	// CoreFoundation exported constant pointers (read as data symbols).
	kCFTypeDictionaryKeyCallBacks   uintptr
	kCFTypeDictionaryValueCallBacks uintptr
	kCFBooleanTrue                  uintptr
	kCFBooleanFalse                 uintptr
	kCFAllocatorDefault             uintptr // NULL == default; kept 0

	// --- CoreVideo ---
	CVPixelBufferCreateWithPlanarBytes func(
		alloc uintptr, width uint64, height uint64, pixelFormat uint32,
		dataPtr unsafe.Pointer, dataSize uint64, numPlanes uint64,
		planeBaseAddrs *uintptr, planeWidths *uint64, planeHeights *uint64,
		planeBytesPerRow *uint64, releaseCB uintptr, releaseRefCon uintptr,
		pixelBufferAttributes uintptr, pixelBufferOut *uintptr) int32
	CVPixelBufferRelease func(pb uintptr)

	// Decode-side CoreVideo: read back the planes of a decoded
	// CVPixelBuffer. The base address is only valid between Lock and
	// Unlock (we pass kCVPixelBufferLock_ReadOnly == 1).
	CVPixelBufferLockBaseAddress       func(pb uintptr, lockFlags uint64) int32
	CVPixelBufferUnlockBaseAddress     func(pb uintptr, unlockFlags uint64) int32
	CVPixelBufferGetWidth              func(pb uintptr) uint64
	CVPixelBufferGetHeight             func(pb uintptr) uint64
	CVPixelBufferGetPixelFormatType    func(pb uintptr) uint32
	CVPixelBufferGetPlaneCount         func(pb uintptr) uint64
	CVPixelBufferIsPlanar              func(pb uintptr) bool
	CVPixelBufferGetBaseAddress        func(pb uintptr) unsafe.Pointer
	CVPixelBufferGetBytesPerRow        func(pb uintptr) uint64
	CVPixelBufferGetBaseAddressOfPlane func(pb uintptr, plane uint64) unsafe.Pointer
	CVPixelBufferGetBytesPerRowOfPlane func(pb uintptr, plane uint64) uint64
	CVPixelBufferGetWidthOfPlane       func(pb uintptr, plane uint64) uint64
	CVPixelBufferGetHeightOfPlane      func(pb uintptr, plane uint64) uint64

	// --- CoreMedia ---
	CMSampleBufferGetDataBuffer                        func(sb uintptr) uintptr
	CMSampleBufferGetFormatDescription                 func(sb uintptr) uintptr
	CMSampleBufferGetSampleAttachmentsArray            func(sb uintptr, create bool) uintptr
	CMBlockBufferGetDataLength                         func(bb uintptr) uint64
	CMBlockBufferCopyDataBytes                         func(bb uintptr, offset uint64, length uint64, dst unsafe.Pointer) int32
	CMVideoFormatDescriptionGetH264ParameterSetAtIndex func(
		fd uintptr, idx uint64, paramSetOut **byte, sizeOut *uint64,
		countOut *uint64, nalHdrLenOut *int32) int32
	CMVideoFormatDescriptionGetHEVCParameterSetAtIndex func(
		fd uintptr, idx uint64, paramSetOut **byte, sizeOut *uint64,
		countOut *uint64, nalHdrLenOut *int32) int32

	// Decode-side CoreMedia: build a CMVideoFormatDescription from the
	// parsed parameter sets, wrap an AVCC access unit in a
	// CMBlockBuffer, and bind it into a CMSampleBuffer for the decoder.
	CMVideoFormatDescriptionCreateFromH264ParameterSets func(
		alloc uintptr, paramSetCount uint64, paramSetPointers **byte,
		paramSetSizes *uint64, nalUnitHeaderLength int32, fmtDescOut *uintptr) int32
	CMVideoFormatDescriptionCreateFromHEVCParameterSets func(
		alloc uintptr, paramSetCount uint64, paramSetPointers **byte,
		paramSetSizes *uint64, nalUnitHeaderLength int32, extensions uintptr,
		fmtDescOut *uintptr) int32
	// CMVideoFormatDescriptionCreate builds a CMVideoFormatDescription from a
	// codec type + dimensions + an extensions dictionary (carrying the codec
	// configuration record atom — av1C for AV1 — under
	// SampleDescriptionExtensionAtoms). Used for the codecs without a
	// dedicated CreateFrom*ParameterSets helper (VP9, AV1).
	CMVideoFormatDescriptionCreate func(
		alloc uintptr, codecType uint32, width int32, height int32,
		extensions uintptr, fmtDescOut *uintptr) int32
	CMBlockBufferCreateWithMemoryBlock func(
		alloc uintptr, memoryBlock unsafe.Pointer, blockLength uint64,
		blockAllocator uintptr, customBlockSource uintptr, offsetToData uint64,
		dataLength uint64, flags uint32, blockBufferOut *uintptr) int32
	CMBlockBufferReplaceDataBytes func(
		sourceBytes unsafe.Pointer, destBuffer uintptr, offsetIntoDest uint64,
		dataLength uint64) int32
	CMSampleBufferCreateReady func(
		alloc uintptr, dataBuffer uintptr, formatDesc uintptr, numSamples int64,
		numSampleTimingEntries int64, sampleTimingArray unsafe.Pointer,
		numSampleSizeEntries int64, sampleSizeArray *uint64, sampleBufferOut *uintptr) int32

	// kCMSampleAttachmentKey_NotSync is the CFString key whose presence
	// (== kCFBooleanTrue) on a sample's first attachment dictionary
	// marks it as NOT a sync sample (i.e. not a keyframe).
	kCMSampleAttachmentKey_NotSync uintptr

	// --- VideoToolbox ---
	VTCompressionSessionCreate func(
		alloc uintptr, width int32, height int32, codecType uint32,
		encoderSpec uintptr, srcImgAttrs uintptr, compAlloc uintptr,
		outputCB uintptr, outputCBRefCon uintptr, sessionOut *uintptr) int32
	VTCompressionSessionEncodeFrame func(
		session uintptr, imageBuffer uintptr, pts cmTime, duration cmTime,
		frameProps uintptr, srcFrameRefCon uintptr, infoFlagsOut *uint32) int32
	VTCompressionSessionCompleteFrames        func(session uintptr, completeUntil cmTime) int32
	VTCompressionSessionPrepareToEncodeFrames func(session uintptr) int32
	VTCompressionSessionInvalidate            func(session uintptr)
	VTSessionSetProperty                      func(session uintptr, key uintptr, value uintptr) int32
	// VTSessionCopySupportedPropertyDictionary returns the property
	// dictionary a live session supports. Used as a secondary probe
	// signal on a successfully-created session.
	VTSessionCopySupportedPropertyDictionary func(session uintptr, dictOut *uintptr) int32
	VTIsHardwareDecodeSupported              func(codecType uint32) bool

	// --- VideoToolbox decompression ---
	VTDecompressionSessionCreate func(
		alloc uintptr, videoFormatDesc uintptr, decoderSpec uintptr,
		destImgBufAttrs uintptr, outputCallbackRecord unsafe.Pointer,
		sessionOut *uintptr) int32
	VTDecompressionSessionDecodeFrame func(
		session uintptr, sampleBuffer uintptr, decodeFlags uint32,
		srcFrameRefCon uintptr, infoFlagsOut *uint32) int32
	VTDecompressionSessionWaitForAsynchronousFrames func(session uintptr) int32
	VTDecompressionSessionInvalidate                func(session uintptr)

	// VideoToolbox exported property-key string constants we set.
	kVTCompressionPropertyKey_RealTime                                 uintptr
	kVTCompressionPropertyKey_ProfileLevel                             uintptr
	kVTCompressionPropertyKey_AverageBitRate                           uintptr
	kVTCompressionPropertyKey_MaxKeyFrameInterval                      uintptr
	kVTCompressionPropertyKey_ExpectedFrameRate                        uintptr
	kVTCompressionPropertyKey_AllowFrameReordering                     uintptr
	kVTVideoEncoderSpecification_EnableHardwareAcceleratedVideoEncoder uintptr
}

var (
	vtOnce sync.Once
	vtRef  *vtLib
	vtErr  error
)

// fwPath is the absolute path to a system framework binary.
func fwPath(name string) string {
	return "/System/Library/Frameworks/" + name + ".framework/" + name
}

// loadVT dlopens the four frameworks and binds every symbol. Memoised;
// the frameworks are never dlclose'd (process-wide load).
func loadVT() (*vtLib, error) {
	vtOnce.Do(func() {
		cf, err := purego.Dlopen(fwPath("CoreFoundation"), purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			vtErr = errors.Join(errors.New("hwaccel: dlopen CoreFoundation failed"), err)
			return
		}
		cv, err := purego.Dlopen(fwPath("CoreVideo"), purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			vtErr = errors.Join(errors.New("hwaccel: dlopen CoreVideo failed"), err)
			return
		}
		cm, err := purego.Dlopen(fwPath("CoreMedia"), purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			vtErr = errors.Join(errors.New("hwaccel: dlopen CoreMedia failed"), err)
			return
		}
		vt, err := purego.Dlopen(fwPath("VideoToolbox"), purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			vtErr = errors.Join(errors.New("hwaccel: dlopen VideoToolbox failed"), err)
			return
		}

		l := &vtLib{}

		// CoreFoundation funcs.
		purego.RegisterLibFunc(&l.CFRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&l.CFRetain, cf, "CFRetain")
		purego.RegisterLibFunc(&l.CFDictionaryCreate, cf, "CFDictionaryCreate")
		purego.RegisterLibFunc(&l.CFNumberCreate, cf, "CFNumberCreate")
		purego.RegisterLibFunc(&l.CFStringCreateWithCString, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&l.CFDictionaryGetValue, cf, "CFDictionaryGetValue")
		purego.RegisterLibFunc(&l.CFDictionaryGetCount, cf, "CFDictionaryGetCount")
		purego.RegisterLibFunc(&l.CFDictionaryContainsKey, cf, "CFDictionaryContainsKey")
		purego.RegisterLibFunc(&l.CFBooleanGetValue, cf, "CFBooleanGetValue")
		purego.RegisterLibFunc(&l.CFArrayGetCount, cf, "CFArrayGetCount")
		purego.RegisterLibFunc(&l.CFArrayGetValueAtIndex, cf, "CFArrayGetValueAtIndex")
		purego.RegisterLibFunc(&l.CFDataCreate, cf, "CFDataCreate")

		// CoreFoundation data symbols.
		l.kCFTypeDictionaryKeyCallBacks = mustSym(cf, "kCFTypeDictionaryKeyCallBacks")
		l.kCFTypeDictionaryValueCallBacks = mustSym(cf, "kCFTypeDictionaryValueCallBacks")
		l.kCFBooleanTrue = derefSym(cf, "kCFBooleanTrue")
		l.kCFBooleanFalse = derefSym(cf, "kCFBooleanFalse")

		// CoreVideo.
		purego.RegisterLibFunc(&l.CVPixelBufferCreateWithPlanarBytes, cv, "CVPixelBufferCreateWithPlanarBytes")
		purego.RegisterLibFunc(&l.CVPixelBufferRelease, cv, "CVPixelBufferRelease")
		purego.RegisterLibFunc(&l.CVPixelBufferLockBaseAddress, cv, "CVPixelBufferLockBaseAddress")
		purego.RegisterLibFunc(&l.CVPixelBufferUnlockBaseAddress, cv, "CVPixelBufferUnlockBaseAddress")
		purego.RegisterLibFunc(&l.CVPixelBufferGetWidth, cv, "CVPixelBufferGetWidth")
		purego.RegisterLibFunc(&l.CVPixelBufferGetHeight, cv, "CVPixelBufferGetHeight")
		purego.RegisterLibFunc(&l.CVPixelBufferGetPixelFormatType, cv, "CVPixelBufferGetPixelFormatType")
		purego.RegisterLibFunc(&l.CVPixelBufferGetPlaneCount, cv, "CVPixelBufferGetPlaneCount")
		purego.RegisterLibFunc(&l.CVPixelBufferIsPlanar, cv, "CVPixelBufferIsPlanar")
		purego.RegisterLibFunc(&l.CVPixelBufferGetBaseAddress, cv, "CVPixelBufferGetBaseAddress")
		purego.RegisterLibFunc(&l.CVPixelBufferGetBytesPerRow, cv, "CVPixelBufferGetBytesPerRow")
		purego.RegisterLibFunc(&l.CVPixelBufferGetBaseAddressOfPlane, cv, "CVPixelBufferGetBaseAddressOfPlane")
		purego.RegisterLibFunc(&l.CVPixelBufferGetBytesPerRowOfPlane, cv, "CVPixelBufferGetBytesPerRowOfPlane")
		purego.RegisterLibFunc(&l.CVPixelBufferGetWidthOfPlane, cv, "CVPixelBufferGetWidthOfPlane")
		purego.RegisterLibFunc(&l.CVPixelBufferGetHeightOfPlane, cv, "CVPixelBufferGetHeightOfPlane")

		// CoreMedia.
		purego.RegisterLibFunc(&l.CMSampleBufferGetDataBuffer, cm, "CMSampleBufferGetDataBuffer")
		purego.RegisterLibFunc(&l.CMSampleBufferGetFormatDescription, cm, "CMSampleBufferGetFormatDescription")
		purego.RegisterLibFunc(&l.CMSampleBufferGetSampleAttachmentsArray, cm, "CMSampleBufferGetSampleAttachmentsArray")
		purego.RegisterLibFunc(&l.CMBlockBufferGetDataLength, cm, "CMBlockBufferGetDataLength")
		purego.RegisterLibFunc(&l.CMBlockBufferCopyDataBytes, cm, "CMBlockBufferCopyDataBytes")
		purego.RegisterLibFunc(&l.CMVideoFormatDescriptionGetH264ParameterSetAtIndex, cm, "CMVideoFormatDescriptionGetH264ParameterSetAtIndex")
		purego.RegisterLibFunc(&l.CMVideoFormatDescriptionGetHEVCParameterSetAtIndex, cm, "CMVideoFormatDescriptionGetHEVCParameterSetAtIndex")
		regFunc(&l.CMVideoFormatDescriptionCreateFromH264ParameterSets, cm, "CMVideoFormatDescriptionCreateFromH264ParameterSets")
		regFunc(&l.CMVideoFormatDescriptionCreateFromHEVCParameterSets, cm, "CMVideoFormatDescriptionCreateFromHEVCParameterSets")
		regFunc(&l.CMVideoFormatDescriptionCreate, cm, "CMVideoFormatDescriptionCreate")
		purego.RegisterLibFunc(&l.CMBlockBufferCreateWithMemoryBlock, cm, "CMBlockBufferCreateWithMemoryBlock")
		purego.RegisterLibFunc(&l.CMBlockBufferReplaceDataBytes, cm, "CMBlockBufferReplaceDataBytes")
		purego.RegisterLibFunc(&l.CMSampleBufferCreateReady, cm, "CMSampleBufferCreateReady")
		l.kCMSampleAttachmentKey_NotSync = derefSym(cm, "kCMSampleAttachmentKey_NotSync")

		// VideoToolbox. These all resolve on a normal install; bind
		// them gracefully (regFunc) so a future macOS that drops or
		// renames one degrades to a clear runtime error instead of a
		// load-time panic. The spec-level
		// VTCopySupportedPropertyDictionaryForEncoderSpecification was
		// removed from the exported surface on recent macOS, so the
		// encode probe creates a throwaway session instead.
		regFunc(&l.VTCompressionSessionCreate, vt, "VTCompressionSessionCreate")
		regFunc(&l.VTCompressionSessionEncodeFrame, vt, "VTCompressionSessionEncodeFrame")
		regFunc(&l.VTCompressionSessionCompleteFrames, vt, "VTCompressionSessionCompleteFrames")
		regFunc(&l.VTCompressionSessionPrepareToEncodeFrames, vt, "VTCompressionSessionPrepareToEncodeFrames")
		regFunc(&l.VTCompressionSessionInvalidate, vt, "VTCompressionSessionInvalidate")
		regFunc(&l.VTSessionSetProperty, vt, "VTSessionSetProperty")
		regFunc(&l.VTSessionCopySupportedPropertyDictionary, vt, "VTSessionCopySupportedPropertyDictionary")
		regFunc(&l.VTIsHardwareDecodeSupported, vt, "VTIsHardwareDecodeSupported")
		regFunc(&l.VTDecompressionSessionCreate, vt, "VTDecompressionSessionCreate")
		regFunc(&l.VTDecompressionSessionDecodeFrame, vt, "VTDecompressionSessionDecodeFrame")
		regFunc(&l.VTDecompressionSessionWaitForAsynchronousFrames, vt, "VTDecompressionSessionWaitForAsynchronousFrames")
		regFunc(&l.VTDecompressionSessionInvalidate, vt, "VTDecompressionSessionInvalidate")

		// VideoToolbox property-key string constants.
		l.kVTCompressionPropertyKey_RealTime = derefSym(vt, "kVTCompressionPropertyKey_RealTime")
		l.kVTCompressionPropertyKey_ProfileLevel = derefSym(vt, "kVTCompressionPropertyKey_ProfileLevel")
		l.kVTCompressionPropertyKey_AverageBitRate = derefSym(vt, "kVTCompressionPropertyKey_AverageBitRate")
		l.kVTCompressionPropertyKey_MaxKeyFrameInterval = derefSym(vt, "kVTCompressionPropertyKey_MaxKeyFrameInterval")
		l.kVTCompressionPropertyKey_ExpectedFrameRate = derefSym(vt, "kVTCompressionPropertyKey_ExpectedFrameRate")
		l.kVTCompressionPropertyKey_AllowFrameReordering = derefSym(vt, "kVTCompressionPropertyKey_AllowFrameReordering")
		l.kVTVideoEncoderSpecification_EnableHardwareAcceleratedVideoEncoder = derefSym(vt, "kVTVideoEncoderSpecification_EnableHardwareAcceleratedVideoEncoder")

		vtRef = l
	})
	return vtRef, vtErr
}

// regFunc binds a C function into fnPtr if the symbol resolves, and is a
// no-op (leaving fnPtr nil) if it does not. Unlike purego.RegisterLibFunc
// it never panics on a missing symbol, so the loader tolerates a macOS
// that has dropped or renamed an optional VideoToolbox entry point; a
// caller of an unbound function gets a nil-call panic at use time with a
// clear stack instead of a load-time crash.
func regFunc(fnPtr any, handle uintptr, name string) {
	if _, err := purego.Dlsym(handle, name); err != nil {
		return
	}
	purego.RegisterLibFunc(fnPtr, handle, name)
}

// mustSym resolves an exported symbol's ADDRESS (used for struct
// constants like the CFDictionary callback vtables, which are passed by
// pointer). Returns 0 if unresolved.
func mustSym(handle uintptr, name string) uintptr {
	p, err := purego.Dlsym(handle, name)
	if err != nil {
		return 0
	}
	return p
}

// derefSym resolves an exported pointer-typed constant (e.g. a
// CFStringRef or CFBooleanRef constant) and DEREFERENCES it once: the
// symbol's address holds the actual CFTypeRef value. Returns 0 if
// unresolved.
func derefSym(handle uintptr, name string) uintptr {
	p, err := purego.Dlsym(handle, name)
	if err != nil || p == 0 {
		return 0
	}
	return *(*uintptr)(unsafe.Pointer(p))
}

// cfString creates a CFString from a Go string (UTF-8) and returns the
// CFStringRef. The caller owns the reference (CFRelease when done).
// kCFStringEncodingUTF8 == 0x08000100.
func (l *vtLib) cfString(s string) uintptr {
	b := append([]byte(s), 0)
	return l.CFStringCreateWithCString(0, &b[0], 0x08000100)
}

// cfNumberInt32 boxes an int32 as a CFNumber.
func (l *vtLib) cfNumberInt32(v int32) uintptr {
	return l.CFNumberCreate(0, kCFNumberSInt32Type, unsafe.Pointer(&v))
}

// cfNumberFloat64 boxes a float64 as a CFNumber.
func (l *vtLib) cfNumberFloat64(v float64) uintptr {
	return l.CFNumberCreate(0, kCFNumberFloat64Type, unsafe.Pointer(&v))
}
