//go:build linux

package hwaccel

import "unsafe"

// Compile-time assertions that the hand-declared kernel ABI structs match
// the on-box C sizeof values (linux/videodev2.h, v4l2-controls.h, media.h
// on the 6.12 aarch64 kernel). A mismatch overflows a uint const and fails
// the build, catching any field/padding drift before it reaches an ioctl.
const (
	_ = uint(unsafe.Sizeof(v4l2Capability{})) - 104
	_ = 104 - uint(unsafe.Sizeof(v4l2Capability{}))
	_ = uint(unsafe.Sizeof(v4l2PixFormatMplane{})) - 192
	_ = 192 - uint(unsafe.Sizeof(v4l2PixFormatMplane{}))
	_ = uint(unsafe.Sizeof(v4l2Format{})) - 208
	_ = 208 - uint(unsafe.Sizeof(v4l2Format{}))
	_ = uint(unsafe.Sizeof(v4l2Requestbuffers{})) - 20
	_ = 20 - uint(unsafe.Sizeof(v4l2Requestbuffers{}))
	_ = uint(unsafe.Sizeof(v4l2Plane{})) - 64
	_ = 64 - uint(unsafe.Sizeof(v4l2Plane{}))
	_ = uint(unsafe.Sizeof(v4l2Buffer{})) - 88
	_ = 88 - uint(unsafe.Sizeof(v4l2Buffer{}))
	_ = uint(unsafe.Sizeof(v4l2ExtControl{})) - 20
	_ = 20 - uint(unsafe.Sizeof(v4l2ExtControl{}))
	_ = uint(unsafe.Sizeof(v4l2ExtControls{})) - 32
	_ = 32 - uint(unsafe.Sizeof(v4l2ExtControls{}))
	_ = uint(unsafe.Sizeof(v4l2Event{})) - 136
	_ = 136 - uint(unsafe.Sizeof(v4l2Event{}))
	_ = uint(unsafe.Sizeof(v4l2EventSubscription{})) - 32
	_ = 32 - uint(unsafe.Sizeof(v4l2EventSubscription{}))
	_ = uint(unsafe.Sizeof(v4l2Fmtdesc{})) - 64
	_ = 64 - uint(unsafe.Sizeof(v4l2Fmtdesc{}))
	_ = uint(unsafe.Sizeof(mediaDeviceInfo{})) - 256
	_ = 256 - uint(unsafe.Sizeof(mediaDeviceInfo{}))
	_ = uint(unsafe.Sizeof(v4l2CtrlHEVCSPS{})) - 40
	_ = 40 - uint(unsafe.Sizeof(v4l2CtrlHEVCSPS{}))
	_ = uint(unsafe.Sizeof(v4l2CtrlHEVCPPS{})) - 64
	_ = 64 - uint(unsafe.Sizeof(v4l2CtrlHEVCPPS{}))
	_ = uint(unsafe.Sizeof(v4l2CtrlHEVCSliceParams{})) - 280
	_ = 280 - uint(unsafe.Sizeof(v4l2CtrlHEVCSliceParams{}))
	_ = uint(unsafe.Sizeof(v4l2CtrlHEVCScalingMatrix{})) - 1000
	_ = 1000 - uint(unsafe.Sizeof(v4l2CtrlHEVCScalingMatrix{}))
	_ = uint(unsafe.Sizeof(v4l2CtrlHEVCDecodeParams{})) - 328
	_ = 328 - uint(unsafe.Sizeof(v4l2CtrlHEVCDecodeParams{}))
	_ = uint(unsafe.Sizeof(v4l2HEVCDPBEntry{})) - 16
	_ = 16 - uint(unsafe.Sizeof(v4l2HEVCDPBEntry{}))
	_ = uint(unsafe.Sizeof(v4l2HEVCPredWeightTable{})) - 194
	_ = 194 - uint(unsafe.Sizeof(v4l2HEVCPredWeightTable{}))
)
