//go:build linux

// Low-level purego bindings to libva (VA-API) and libva-drm, used by the
// vaapi backend. No cgo: every symbol is resolved with purego.Dlopen +
// RegisterLibFunc. The two shared objects (libva.so.2, libva-drm.so.2)
// are dlopen'd once at first use and never released; the DRM render node
// (/dev/dri/renderD128) is opened directly with syscall.Open and handed
// to vaGetDisplayDRM.
//
// # ABI notes
//
//   - VA-API is almost entirely pointer-based: every entry point takes
//     scalar / pointer arguments only, so no purego.NewCallback (and thus
//     no struct-by-value callback caveat) is needed — the whole backend
//     is synchronous (vaSyncSurface blocks until a picture completes).
//   - Opaque handles: VADisplay is a void* (uintptr); VAStatus is an int
//     (int32); VAConfigID / VAContextID / VASurfaceID / VABufferID /
//     VAImageID are all "unsigned int" (uint32), VAGenericID.
//   - The parameter-buffer structs below mirror va/va.h, va/va_enc_h264.h,
//     va/va_enc_hevc.h and va/va_dec_hevc.h for VA-API 1.22 (libva 2.22)
//     BYTE-FOR-BYTE: field order, integer widths, array extents, bitfield
//     packing (Go has no bitfields, so each C bitfield union is mirrored
//     as the backing uint32 `value` word and assembled with shifts), and
//     the trailing va_reserved padding. A mismatch silently corrupts the
//     driver's view of the picture, so these layouts are not to be
//     "tidied".

package hwaccel

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// VA-API scalar constants from va/va.h (VA-API 1.22).
const (
	vaStatusSuccess int32 = 0x00000000

	// Profiles we care about (VAProfile enum values).
	vaProfileH264ConstrainedBaseline int32 = 13
	vaProfileH264Main                int32 = 6
	vaProfileH264High                int32 = 7
	vaProfileHEVCMain                int32 = 17
	vaProfileHEVCMain10              int32 = 18
	vaProfileVP9Profile0             int32 = 19
	vaProfileVP9Profile1             int32 = 20
	vaProfileVP9Profile2             int32 = 21
	vaProfileVP9Profile3             int32 = 22
	vaProfileAV1Profile0             int32 = 32
	vaProfileAV1Profile1             int32 = 33

	// Entrypoints (VAEntrypoint enum values).
	vaEntrypointVLD        uint32 = 1
	vaEntrypointEncSlice   uint32 = 6
	vaEntrypointEncSliceLP uint32 = 8

	// Config attribute types (VAConfigAttribType enum values).
	vaConfigAttribRTFormat         uint32 = 0
	vaConfigAttribRateControl      uint32 = 5
	vaConfigAttribEncPackedHeaders uint32 = 10

	// VAConfigAttribRTFormat value bit.
	vaRTFormatYUV420 uint32 = 0x00000001

	// VAConfigAttribRateControl value bits.
	vaRCCBR uint32 = 0x00000002
	vaRCCQP uint32 = 0x00000010

	// VAConfigAttribEncPackedHeaders value bits.
	vaEncPackedHeaderNone     uint32 = 0x00000000
	vaEncPackedHeaderSequence uint32 = 0x00000001
	vaEncPackedHeaderPicture  uint32 = 0x00000002
	vaEncPackedHeaderSlice    uint32 = 0x00000004

	// VABufferType enum values.
	vaPictureParameterBufferType         uint32 = 0
	vaIQMatrixBufferType                 uint32 = 1
	vaSliceParameterBufferType           uint32 = 4
	vaSliceDataBufferType                uint32 = 5
	vaEncCodedBufferType                 uint32 = 21
	vaEncSequenceParameterBufferType     uint32 = 22
	vaEncPictureParameterBufferType      uint32 = 23
	vaEncSliceParameterBufferType        uint32 = 24
	vaEncPackedHeaderParameterBufferType uint32 = 25
	vaEncPackedHeaderDataBufferType      uint32 = 26
	vaEncMiscParameterBufferType         uint32 = 27

	// VAEncMiscParameterType enum values.
	vaEncMiscParameterTypeFrameRate   uint32 = 0
	vaEncMiscParameterTypeRateControl uint32 = 1

	// VAEncPackedHeaderType enum values.
	vaEncPackedHeaderTypeSequence uint32 = 1
	vaEncPackedHeaderTypePicture  uint32 = 2
	vaEncPackedHeaderTypeSlice    uint32 = 3
	vaEncPackedHeaderTypeRawData  uint32 = 4

	// Slice-data flag: whole slice present in the buffer.
	vaSliceDataFlagAll uint32 = 0x00

	// VA_PROGRESSIVE flag for vaCreateContext.
	vaProgressive int32 = 0x1

	// VA_FOURCC_NV12.
	vaFourCCNV12 uint32 = 0x3231564E

	// VASurfaceAttribType / value-type / flag constants (va.h).
	// NB: the VASurfaceAttribType enum is 0-based with PixelFormat == 1 and
	// MemoryType == 6 (None=0, PixelFormat=1, MinWidth=2, MaxWidth=3,
	// MinHeight=4, MaxHeight=5, MemoryType=6, ...). Passing the wrong type
	// value silently mis-tags the attribute (e.g. 2 == MinWidth, read-only),
	// so the driver ignores the requested fourcc and picks a default tiling —
	// which the iHD HEVC low-power encoder mishandles.
	vaSurfaceAttribPixelFormat  uint32 = 1
	vaSurfaceAttribMemoryType   uint32 = 6
	vaGenericValueTypeInteger   uint32 = 1
	vaSurfaceAttribFlagSettable uint32 = 0x00000002

	// VA_SURFACE_ATTRIB_MEM_TYPE_VA: the default VA-managed (GPU-tiled)
	// surface backing the encoder reads from. ffmpeg's hwframes pool tags
	// its encode input surfaces with this explicitly.
	vaSurfaceAttribMemTypeVA uint32 = 0x00000001

	// VA_LSB_FIRST byte order for VAImageFormat.
	vaLSBFirst uint32 = 1

	// Invalid id sentinel.
	vaInvalidID      uint32 = 0xffffffff
	vaInvalidSurface uint32 = 0xffffffff

	// VAPictureH264.flags bits.
	vaPictureH264Invalid uint32 = 0x00000001

	// VAPictureHEVC.flags bits.
	vaPictureHEVCInvalid uint32 = 0x00000001
)

// vaLib holds every dynamically-resolved VA-API symbol the backend uses.
type vaLib struct {
	// --- libva-drm ---
	vaGetDisplayDRM func(fd int32) uintptr

	// --- libva core ---
	vaInitialize             func(dpy uintptr, major *int32, minor *int32) int32
	vaTerminate              func(dpy uintptr) int32
	vaMaxNumProfiles         func(dpy uintptr) int32
	vaMaxNumEntrypoints      func(dpy uintptr) int32
	vaQueryConfigProfiles    func(dpy uintptr, profileList *int32, numProfiles *int32) int32
	vaQueryConfigEntrypoints func(dpy uintptr, profile int32, entrypointList *uint32, numEntrypoints *int32) int32
	vaGetConfigAttributes    func(dpy uintptr, profile int32, entrypoint uint32, attribList unsafe.Pointer, numAttribs int32) int32
	vaCreateConfig           func(dpy uintptr, profile int32, entrypoint uint32, attribList unsafe.Pointer, numAttribs int32, configID *uint32) int32
	vaDestroyConfig          func(dpy uintptr, configID uint32) int32
	vaCreateSurfaces         func(dpy uintptr, format uint32, width uint32, height uint32, surfaces *uint32, numSurfaces uint32, attribList unsafe.Pointer, numAttribs uint32) int32
	vaDestroySurfaces        func(dpy uintptr, surfaces *uint32, numSurfaces int32) int32
	vaCreateContext          func(dpy uintptr, configID uint32, width int32, height int32, flag int32, renderTargets *uint32, numRenderTargets int32, context *uint32) int32
	vaDestroyContext         func(dpy uintptr, context uint32) int32
	vaCreateBuffer           func(dpy uintptr, context uint32, typ uint32, size uint32, numElements uint32, data unsafe.Pointer, bufID *uint32) int32
	vaMapBuffer              func(dpy uintptr, bufID uint32, pbuf *unsafe.Pointer) int32
	vaUnmapBuffer            func(dpy uintptr, bufID uint32) int32
	vaDestroyBuffer          func(dpy uintptr, bufID uint32) int32
	vaBeginPicture           func(dpy uintptr, context uint32, renderTarget uint32) int32
	vaRenderPicture          func(dpy uintptr, context uint32, buffers *uint32, numBuffers int32) int32
	vaEndPicture             func(dpy uintptr, context uint32) int32
	vaSyncSurface            func(dpy uintptr, renderTarget uint32) int32
	vaDeriveImage            func(dpy uintptr, surface uint32, image unsafe.Pointer) int32
	vaCreateImage            func(dpy uintptr, format unsafe.Pointer, width int32, height int32, image unsafe.Pointer) int32
	vaGetImage               func(dpy uintptr, surface uint32, x int32, y int32, width uint32, height uint32, image uint32) int32
	vaPutImage               func(dpy uintptr, surface uint32, image uint32, srcX int32, srcY int32, srcW uint32, srcH uint32, dstX int32, dstY int32, dstW uint32, dstH uint32) int32
	vaDestroyImage           func(dpy uintptr, image uint32) int32
}

var (
	vaOnce sync.Once
	vaRef  *vaLib
	vaErr  error
)

// loadVA dlopens libva.so.2 and libva-drm.so.2 and binds every symbol.
// Memoised; the libraries are never dlclose'd (process-wide load).
func loadVA() (*vaLib, error) {
	vaOnce.Do(func() {
		va, err := purego.Dlopen("libva.so.2", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			vaErr = errors.Join(errors.New("hwaccel: dlopen libva.so.2 failed"), err)
			return
		}
		drm, err := purego.Dlopen("libva-drm.so.2", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			vaErr = errors.Join(errors.New("hwaccel: dlopen libva-drm.so.2 failed"), err)
			return
		}

		l := new(vaLib)
		purego.RegisterLibFunc(&l.vaGetDisplayDRM, drm, "vaGetDisplayDRM")

		purego.RegisterLibFunc(&l.vaInitialize, va, "vaInitialize")
		purego.RegisterLibFunc(&l.vaTerminate, va, "vaTerminate")
		purego.RegisterLibFunc(&l.vaMaxNumProfiles, va, "vaMaxNumProfiles")
		purego.RegisterLibFunc(&l.vaMaxNumEntrypoints, va, "vaMaxNumEntrypoints")
		purego.RegisterLibFunc(&l.vaQueryConfigProfiles, va, "vaQueryConfigProfiles")
		purego.RegisterLibFunc(&l.vaQueryConfigEntrypoints, va, "vaQueryConfigEntrypoints")
		purego.RegisterLibFunc(&l.vaGetConfigAttributes, va, "vaGetConfigAttributes")
		purego.RegisterLibFunc(&l.vaCreateConfig, va, "vaCreateConfig")
		purego.RegisterLibFunc(&l.vaDestroyConfig, va, "vaDestroyConfig")
		purego.RegisterLibFunc(&l.vaCreateSurfaces, va, "vaCreateSurfaces")
		purego.RegisterLibFunc(&l.vaDestroySurfaces, va, "vaDestroySurfaces")
		purego.RegisterLibFunc(&l.vaCreateContext, va, "vaCreateContext")
		purego.RegisterLibFunc(&l.vaDestroyContext, va, "vaDestroyContext")
		purego.RegisterLibFunc(&l.vaCreateBuffer, va, "vaCreateBuffer")
		purego.RegisterLibFunc(&l.vaMapBuffer, va, "vaMapBuffer")
		purego.RegisterLibFunc(&l.vaUnmapBuffer, va, "vaUnmapBuffer")
		purego.RegisterLibFunc(&l.vaDestroyBuffer, va, "vaDestroyBuffer")
		purego.RegisterLibFunc(&l.vaBeginPicture, va, "vaBeginPicture")
		purego.RegisterLibFunc(&l.vaRenderPicture, va, "vaRenderPicture")
		purego.RegisterLibFunc(&l.vaEndPicture, va, "vaEndPicture")
		purego.RegisterLibFunc(&l.vaSyncSurface, va, "vaSyncSurface")
		purego.RegisterLibFunc(&l.vaDeriveImage, va, "vaDeriveImage")
		purego.RegisterLibFunc(&l.vaCreateImage, va, "vaCreateImage")
		purego.RegisterLibFunc(&l.vaGetImage, va, "vaGetImage")
		purego.RegisterLibFunc(&l.vaPutImage, va, "vaPutImage")
		purego.RegisterLibFunc(&l.vaDestroyImage, va, "vaDestroyImage")

		vaRef = l
	})
	return vaRef, vaErr
}

// ---- VA structs (1:1 with the VA-API 1.22 headers) -------------------

// vaConfigAttrib mirrors VAConfigAttrib { VAConfigAttribType type; uint32 value }.
type vaConfigAttrib struct {
	Type  uint32
	Value uint32
}

// vaImageFormat mirrors VAImageFormat. 8 uint32 fields + VA_PADDING_LOW(4).
type vaImageFormat struct {
	FourCC       uint32
	ByteOrder    uint32
	BitsPerPixel uint32
	Depth        uint32
	RedMask      uint32
	GreenMask    uint32
	BlueMask     uint32
	AlphaMask    uint32
	vaReserved   [4]uint32
}

// vaImage mirrors VAImage. The field order and widths (uint16 width/height,
// uint32 data_size/num_planes, pitches[3]/offsets[3], paletted fields,
// component_order[4]) match the header exactly; the embedded VAImageFormat
// is laid out inline.
type vaImage struct {
	ImageID           uint32
	Format            vaImageFormat
	Buf               uint32
	Width             uint16
	Height            uint16
	DataSize          uint32
	NumPlanes         uint32
	Pitches           [3]uint32
	Offsets           [3]uint32
	NumPaletteEntries int32
	EntryBytes        int32
	ComponentOrder    [4]int8
	vaReserved        [4]uint32
}

// vaCodedBufferSegment mirrors VACodedBufferSegment: size/bit_offset/status/
// reserved (4 uint32), then void* buf and void* next, then VA_PADDING_LOW.
type vaCodedBufferSegment struct {
	Size       uint32
	BitOffset  uint32
	Status     uint32
	Reserved   uint32
	Buf        unsafe.Pointer
	Next       unsafe.Pointer
	vaReserved [4]uint32
}

// vaEncPackedHeaderParameterBuffer mirrors VAEncPackedHeaderParameterBuffer:
// uint32 type, uint32 bit_length, uint8 has_emulation_bytes, then
// VA_PADDING_LOW. The trailing uint8 is followed by 3 bytes of padding
// before the uint32 reserved array (natural C alignment); Go inserts the
// same padding because the uint32 array forces 4-byte alignment.
type vaEncPackedHeaderParameterBuffer struct {
	Type              uint32
	BitLength         uint32
	HasEmulationBytes uint8
	_                 [3]uint8
	vaReserved        [4]uint32
}

// vaEncMiscParameterRateControl mirrors VAEncMiscParameterBuffer (a uint32
// `type` tag) immediately followed by VAEncMiscParameterRateControl. The
// driver reads one allocation holding the tag + this body, so the two are
// fused here: TypeTag is the VAEncMiscParameterBuffer.type word and the rest
// is the rate-control body byte-for-byte.
type vaEncMiscParameterRateControl struct {
	TypeTag          uint32 // VAEncMiscParameterBuffer.type
	BitsPerSecond    uint32
	TargetPercentage uint32
	WindowSize       uint32
	InitialQP        uint32
	MinQP            uint32
	BasicUnitSize    uint32
	RCFlags          uint32 // rc_flags union (value word)
	ICQQualityFactor uint32
	MaxQP            uint32
	QualityFactor    uint32
	TargetFrameSize  uint32
	vaReserved       [4]uint32
}

// vaEncMiscParameterFrameRate mirrors VAEncMiscParameterBuffer(type) fused
// with VAEncMiscParameterFrameRate: a uint32 type tag, the packed
// framerate word (denominator<<16 | numerator), the framerate_flags union
// word, and VA_PADDING_LOW.
type vaEncMiscParameterFrameRate struct {
	TypeTag        uint32 // VAEncMiscParameterBuffer.type
	Framerate      uint32
	FramerateFlags uint32
	vaReserved     [4]uint32
}

// vaGenericValue mirrors VAGenericValue: a uint32 type tag, 4 bytes of
// padding, then an 8-byte union (here used for the integer alternative).
type vaGenericValue struct {
	Type   uint32
	_      uint32
	IntVal int64 // union slot (int32 alternative stored in the low word)
}

// vaSurfaceAttrib mirrors VASurfaceAttrib: type, flags, then the value.
type vaSurfaceAttrib struct {
	Type  uint32
	Flags uint32
	Value vaGenericValue
}

// openRenderNode opens the DRM render node read-write (O_RDWR is required
// for VA-API surface allocation on the render node). Returns the fd.
func openRenderNode(path string) (int, error) {
	return openRenderNodeSyscall(path)
}
