//go:build linux

// Raw V4L2 + media-controller ioctl plumbing for the v4l2 backend, with
// CGO disabled: every kernel ABI structure, constant, and ioctl request
// number is declared by hand here and driven through syscall.Syscall(
// SYS_IOCTL, ...). The struct layouts are byte-exact against the on-box
// headers (linux/videodev2.h, linux/v4l2-controls.h, linux/media.h on the
// 6.12 aarch64 kernel the backend was developed against): field order,
// padding, and union sizing all matter because the structs cross the
// syscall boundary verbatim.
//
// # Multiplanar M2M
//
// Every device the backend drives is a multiplanar memory-to-memory codec
// (V4L2_CAP_VIDEO_M2M_MPLANE): the coded side is the OUTPUT queue
// (V4L2_BUF_TYPE_VIDEO_OUTPUT_MPLANE) and the raw side is the CAPTURE
// queue (V4L2_BUF_TYPE_VIDEO_CAPTURE_MPLANE). All buffers are
// V4L2_MEMORY_MMAP and mapped with syscall.Mmap.
//
// # Request API
//
// The Pi-5 stateless HEVC decoder needs a per-frame media-controller
// request fd (MEDIA_IOC_REQUEST_ALLOC on /dev/mediaN), onto which the
// per-frame controls are attached via V4L2_CTRL_WHICH_REQUEST_VAL and the
// OUTPUT buffer is bound via V4L2_BUF_FLAG_REQUEST_FD; the request is then
// driven with MEDIA_REQUEST_IOC_QUEUE / _REINIT.

package hwaccel

import (
	"syscall"
	"unsafe"
)

// ---- ioctl request-code arithmetic -----------------------------------
//
// The kernel encodes ioctl numbers as
// dir(2) | size(14) | type(8) | nr(8). _IOR/_IOW/_IOWR mirror the C
// macros; the V4L2 ioctls use type 'V' and the media ones type '|'.

const (
	iocNone  = 0
	iocWrite = 1
	iocRead  = 2

	iocNRBits   = 8
	iocTypeBits = 8
	iocSizeBits = 14

	iocNRShift   = 0
	iocTypeShift = iocNRShift + iocNRBits
	iocSizeShift = iocTypeShift + iocTypeBits
	iocDirShift  = iocSizeShift + iocSizeBits
)

func ioc(dir, typ, nr, size uintptr) uintptr {
	return dir<<iocDirShift | typ<<iocTypeShift | nr<<iocNRShift | size<<iocSizeShift
}

func ior(typ, nr, size uintptr) uintptr  { return ioc(iocRead, typ, nr, size) }
func iow(typ, nr, size uintptr) uintptr  { return ioc(iocWrite, typ, nr, size) }
func iowr(typ, nr, size uintptr) uintptr { return ioc(iocRead|iocWrite, typ, nr, size) }
func io(typ, nr uintptr) uintptr         { return ioc(iocNone, typ, nr, 0) }

const (
	iocTypeV   = 'V'
	iocTypeReq = '|'
)

// VIDIOC_* request numbers. Sizes are the Go struct sizes, which equal
// the C struct sizes the on-box header reports.
var (
	vidiocQueryCap       = ior(iocTypeV, 0, unsafe.Sizeof(v4l2Capability{}))
	vidiocEnumFmt        = iowr(iocTypeV, 2, unsafe.Sizeof(v4l2Fmtdesc{}))
	vidiocGFmt           = iowr(iocTypeV, 4, unsafe.Sizeof(v4l2Format{}))
	vidiocSFmt           = iowr(iocTypeV, 5, unsafe.Sizeof(v4l2Format{}))
	vidiocReqbufs        = iowr(iocTypeV, 8, unsafe.Sizeof(v4l2Requestbuffers{}))
	vidiocQuerybuf       = iowr(iocTypeV, 9, unsafe.Sizeof(v4l2Buffer{}))
	vidiocQBuf           = iowr(iocTypeV, 15, unsafe.Sizeof(v4l2Buffer{}))
	vidiocDQBuf          = iowr(iocTypeV, 17, unsafe.Sizeof(v4l2Buffer{}))
	vidiocStreamOn       = iow(iocTypeV, 18, unsafe.Sizeof(int32(0)))
	vidiocStreamOff      = iow(iocTypeV, 19, unsafe.Sizeof(int32(0)))
	vidiocTryFmt         = iowr(iocTypeV, 64, unsafe.Sizeof(v4l2Format{}))
	vidiocGExtCtrls      = iowr(iocTypeV, 71, unsafe.Sizeof(v4l2ExtControls{}))
	vidiocSExtCtrls      = iowr(iocTypeV, 72, unsafe.Sizeof(v4l2ExtControls{}))
	vidiocDQEvent        = ior(iocTypeV, 89, unsafe.Sizeof(v4l2Event{}))
	vidiocSubscribeEvent = iow(iocTypeV, 90, unsafe.Sizeof(v4l2EventSubscription{}))
	vidiocDecoderCmd     = iowr(iocTypeV, 96, unsafe.Sizeof(v4l2DecoderCmd{}))
	vidiocQueryExtCtrl   = iowr(iocTypeV, 103, unsafe.Sizeof(v4l2QueryExtCtrl{}))
)

// Media-controller request ioctls. MEDIA_IOC_REQUEST_ALLOC returns the
// request fd in an int passed by reference; the per-request QUEUE/REINIT
// are issued on the request fd itself and take no argument.
var (
	mediaIOCRequestAlloc  = ior(iocTypeReq, 0x05, unsafe.Sizeof(int32(0)))
	mediaRequestIOCQueue  = io(iocTypeReq, 0x80)
	mediaRequestIOCReinit = io(iocTypeReq, 0x81)
)

// ---- enum / flag constants -------------------------------------------

const (
	v4l2CapVideoM2M        = 0x00008000
	v4l2CapVideoM2MMplane  = 0x00004000
	v4l2CapStreaming       = 0x04000000
	v4l2CapDeviceCaps      = 0x80000000
	v4l2CapExtPixFormat    = 0x00200000
	v4l2BufTypeVideoCapMP  = 9
	v4l2BufTypeVideoOutMP  = 10
	v4l2MemoryMMAP         = 1
	v4l2FieldNone          = 1
	v4l2BufFlagKeyframe    = 0x00000008
	v4l2BufFlagLast        = 0x00100000
	v4l2BufFlagRequestFD   = 0x00800000
	v4l2EventSourceChange  = 5
	v4l2EventEOS           = 2
	v4l2EventSrcChResolutn = 1 << 0
	v4l2DecCmdStop         = 1
	v4l2DecCmdStart        = 0
)

// FourCC pixel formats. v4l2_fourcc packs four ASCII bytes little-endian.
func fourcc(a, b, c, d byte) uint32 {
	return uint32(a) | uint32(b)<<8 | uint32(c)<<16 | uint32(d)<<24
}

var (
	pixFmtHEVC         = fourcc('H', 'E', 'V', 'C') // stateful coded HEVC
	pixFmtHEVCSlice    = fourcc('S', '2', '6', '5') // stateless parsed-slice HEVC
	pixFmtH264         = fourcc('H', '2', '6', '4')
	pixFmtNV12         = fourcc('N', 'V', '1', '2') // linear NV12
	pixFmtYUV420       = fourcc('Y', 'U', '1', '2')
	pixFmtNV12Col128   = fourcc('N', 'C', '1', '2') // Broadcom SAND128 tiled NV12
	pixFmtNV1210Col128 = fourcc('N', 'C', '3', '0')
)

// Stateless HEVC control IDs (V4L2_CID_CODEC_STATELESS_BASE + n).
const (
	v4l2CtrlClassCodecStateless = 0x00a40000
	v4l2CidCodecStatelessBase   = v4l2CtrlClassCodecStateless | 0x900

	v4l2CidStatelessHEVCSPS           = v4l2CidCodecStatelessBase + 400
	v4l2CidStatelessHEVCPPS           = v4l2CidCodecStatelessBase + 401
	v4l2CidStatelessHEVCSliceParams   = v4l2CidCodecStatelessBase + 402
	v4l2CidStatelessHEVCScalingMatrix = v4l2CidCodecStatelessBase + 403
	v4l2CidStatelessHEVCDecodeParams  = v4l2CidCodecStatelessBase + 404
	v4l2CidStatelessHEVCDecodeMode    = v4l2CidCodecStatelessBase + 405
	v4l2CidStatelessHEVCStartCode     = v4l2CidCodecStatelessBase + 406

	v4l2StatelessHEVCDecodeModeFrameBased = 1
	v4l2StatelessHEVCStartCodeNone        = 0

	v4l2CtrlWhichCurVal     = 0
	v4l2CtrlWhichRequestVal = 0x0f010000
)

// videoMaxPlanes is VIDEO_MAX_PLANES; sizes the fixed plane arrays in the
// multiplanar format/buffer structs.
const videoMaxPlanes = 8

// ---- kernel structs (byte-exact) -------------------------------------

// v4l2Capability mirrors struct v4l2_capability (104 bytes).
type v4l2Capability struct {
	Driver       [16]byte
	Card         [32]byte
	BusInfo      [32]byte
	Version      uint32
	Capabilities uint32
	DeviceCaps   uint32
	Reserved     [3]uint32
}

// v4l2PlanePixFormat mirrors struct v4l2_plane_pix_format (packed, 20B).
type v4l2PlanePixFormat struct {
	SizeImage    uint32
	BytesPerLine uint32
	Reserved     [6]uint16
}

// v4l2PixFormatMplane mirrors struct v4l2_pix_format_mplane (packed, 192B).
type v4l2PixFormatMplane struct {
	Width        uint32
	Height       uint32
	PixelFormat  uint32
	Field        uint32
	Colorspace   uint32
	PlaneFmt     [videoMaxPlanes]v4l2PlanePixFormat
	NumPlanes    uint8
	Flags        uint8
	YcbcrEnc     uint8
	Quantization uint8
	XferFunc     uint8
	Reserved     [7]uint8
}

// v4l2Format mirrors struct v4l2_format. The union is sized by raw_data
// [200]; type plus that union is 4 + 200 = 204, padded to 208 (the union
// holds an 8-byte-aligned member so the whole struct aligns to 8).
type v4l2Format struct {
	Type uint32
	_    uint32 // alignment of the 8-byte-aligned union
	Raw  [200]byte
}

// pixMP returns a typed view of the pix_format_mplane union member.
func (f *v4l2Format) pixMP() *v4l2PixFormatMplane {
	return (*v4l2PixFormatMplane)(unsafe.Pointer(&f.Raw[0]))
}

// v4l2Requestbuffers mirrors struct v4l2_requestbuffers (20 bytes).
type v4l2Requestbuffers struct {
	Count        uint32
	Type         uint32
	Memory       uint32
	Capabilities uint32
	Flags        uint8
	Reserved     [3]uint8
}

// v4l2Plane mirrors struct v4l2_plane (64 bytes). The m union is 8 bytes
// (it holds an unsigned long / pointer); only mem_offset (uint32) is used.
type v4l2Plane struct {
	BytesUsed  uint32
	Length     uint32
	M          uint64 // union { mem_offset; userptr; fd }
	DataOffset uint32
	Reserved   [11]uint32
}

// v4l2Buffer mirrors struct v4l2_buffer (88 bytes). timestamp is a
// struct timeval (two longs = 16 bytes on 64-bit). The m union is 8 bytes
// and holds either an offset, a userptr, or a *planes pointer (the
// multiplanar case). The trailing union exposes request_fd.
type v4l2Buffer struct {
	Index     uint32
	Type      uint32
	BytesUsed uint32
	Flags     uint32
	Field     uint32
	Timestamp syscall.Timeval // struct timeval (16B on arm64)
	Timecode  v4l2Timecode
	Sequence  uint32
	Memory    uint32
	M         uint64 // union { offset; userptr; *planes; fd }
	Length    uint32
	Reserved2 uint32
	RequestFD int32 // union { request_fd; reserved }
}

// v4l2Timecode mirrors struct v4l2_timecode (16 bytes); unused but holds
// layout.
type v4l2Timecode struct {
	Type     uint32
	Flags    uint32
	Frames   uint8
	Seconds  uint8
	Minutes  uint8
	Hours    uint8
	Userbits [4]uint8
}

// v4l2ExtControl mirrors struct v4l2_ext_control, which is
// __attribute__((packed)) and 20 bytes: id(4) size(4) reserved2(4) then an
// 8-byte union with no alignment padding before it. Go would 8-align a
// uint64, inflating the struct to 24, so the union is held as a raw
// [8]byte and accessed through helpers — keeping the layout byte-exact.
type v4l2ExtControl struct {
	ID       uint32
	Size     uint32
	Reserved uint32
	Value    [8]byte // packed union { s32 value; ...; void *ptr }
}

// setPtr stores a pointer payload (for the variable-size HEVC controls).
func (c *v4l2ExtControl) setPtr(p unsafe.Pointer) {
	*(*uintptr)(unsafe.Pointer(&c.Value[0])) = uintptr(p)
}

// setS32 stores a 32-bit inline value (for the menu controls).
func (c *v4l2ExtControl) setS32(v int32) {
	*(*int32)(unsafe.Pointer(&c.Value[0])) = v
}

// v4l2ExtControls mirrors struct v4l2_ext_controls (32 bytes). Which is
// the ctrl_class/which union; RequestFD carries the request fd when which
// == V4L2_CTRL_WHICH_REQUEST_VAL.
type v4l2ExtControls struct {
	Which     uint32
	Count     uint32
	ErrorIdx  uint32
	RequestFD int32
	Reserved  [1]uint32
	Controls  uint64 // struct v4l2_ext_control *
}

// v4l2EventSubscription mirrors struct v4l2_event_subscription (32 bytes).
type v4l2EventSubscription struct {
	Type     uint32
	ID       uint32
	Flags    uint32
	Reserved [5]uint32
}

// v4l2Event mirrors struct v4l2_event (136 bytes). The u union is 64
// bytes (data[64]); for SOURCE_CHANGE the first 4 bytes are the changes
// bitmask. timestamp is a struct timespec (two longs = 16 bytes).
type v4l2Event struct {
	Type      uint32
	U         [64]byte
	Pending   uint32
	Sequence  uint32
	Timestamp syscall.Timespec
	ID        uint32
	Reserved  [8]uint32
}

// srcChanges returns the source-change bitmask from a SOURCE_CHANGE event.
func (e *v4l2Event) srcChanges() uint32 {
	return *(*uint32)(unsafe.Pointer(&e.U[0]))
}

// v4l2Fmtdesc mirrors struct v4l2_fmtdesc (64 bytes).
type v4l2Fmtdesc struct {
	Index       uint32
	Type        uint32
	Flags       uint32
	Description [32]byte
	PixelFormat uint32
	MbusCode    uint32
	Reserved    [3]uint32
}

// v4l2DecoderCmd mirrors struct v4l2_decoder_cmd (64 bytes: cmd + flags +
// a union sized by raw.data[16]).
type v4l2DecoderCmd struct {
	Cmd   uint32
	Flags uint32
	Raw   [16]uint32
}

// v4l2QueryExtCtrl mirrors the leading fields of struct v4l2_query_ext_ctrl;
// only used to detect whether a control id exists on a node. The full
// struct is large; we declare it at its true size so the ioctl number's
// embedded size matches the kernel's.
type v4l2QueryExtCtrl struct {
	ID           uint32
	Type         uint32
	Name         [32]byte
	Minimum      int64
	Maximum      int64
	Step         uint64
	DefaultValue int64
	Flags        uint32
	ElemSize     uint32
	Elems        uint32
	NrOfDims     uint32
	Dims         [4]uint32
	Reserved     [32]uint32
}

// ---- ioctl wrapper ----------------------------------------------------

// ioctl issues a raw SYS_IOCTL and returns the errno (0 on success). arg
// is the address of the request structure (or an int for STREAMON etc.).
func ioctl(fd int, req uintptr, arg unsafe.Pointer) syscall.Errno {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	return errno
}

// ---- mmap helpers -----------------------------------------------------

// mmapBuffer maps a single MMAP plane at the kernel mem_offset.
func mmapBuffer(fd int, offset uint32, length int) ([]byte, error) {
	return syscall.Mmap(fd, int64(offset), length,
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
}

// munmapBuffer unmaps a previously mapped plane.
func munmapBuffer(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	return syscall.Munmap(b)
}
