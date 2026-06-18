//go:build linux

// Media-controller Request API helpers for the stateless decoder. Each
// frame the stateless HEVC hardware decodes needs a request fd allocated
// from the decoder's media node (/dev/mediaN via MEDIA_IOC_REQUEST_ALLOC):
// the per-frame controls (SPS/PPS/slice-params/scaling-matrix/decode-
// params) are attached to that request via VIDIOC_S_EXT_CTRLS with
// which == V4L2_CTRL_WHICH_REQUEST_VAL and the request fd, the coded
// OUTPUT buffer is bound to it with V4L2_BUF_FLAG_REQUEST_FD, and the
// request is launched with MEDIA_REQUEST_IOC_QUEUE. After the frame
// completes the request is recycled with MEDIA_REQUEST_IOC_REINIT.

package hwaccel

import (
	"fmt"
	"syscall"
	"unsafe"
)

// mediaDevice is an opened /dev/mediaN node used only to allocate request
// fds for its associated video node.
type mediaDevice struct {
	path string
	fd   int
}

// openMediaDevice opens a media controller node.
func openMediaDevice(path string) (*mediaDevice, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: open %s: %v", ErrBackendFailure, path, err)
	}
	return &mediaDevice{path: path, fd: fd}, nil
}

// close releases the media node fd.
func (m *mediaDevice) close() error {
	if m.fd < 0 {
		return nil
	}
	err := syscall.Close(m.fd)
	m.fd = -1
	return err
}

// allocRequest allocates a new request fd from the media node.
func (m *mediaDevice) allocRequest() (int, error) {
	var reqFD int32
	if errno := ioctl(m.fd, mediaIOCRequestAlloc, unsafe.Pointer(&reqFD)); errno != 0 {
		return -1, fmt.Errorf("%w: MEDIA_IOC_REQUEST_ALLOC: %v", ErrBackendFailure, errno)
	}
	return int(reqFD), nil
}

// queueRequest launches a request (MEDIA_REQUEST_IOC_QUEUE on the request
// fd). The kernel then runs the bound OUTPUT buffer through the decoder
// with the attached controls.
func queueRequest(reqFD int) error {
	if errno := ioctl(reqFD, mediaRequestIOCQueue, nil); errno != 0 {
		return fmt.Errorf("%w: MEDIA_REQUEST_IOC_QUEUE: %v", ErrBackendFailure, errno)
	}
	return nil
}

// reinitRequest recycles a completed request fd for reuse
// (MEDIA_REQUEST_IOC_REINIT).
func reinitRequest(reqFD int) error {
	if errno := ioctl(reqFD, mediaRequestIOCReinit, nil); errno != 0 {
		return fmt.Errorf("%w: MEDIA_REQUEST_IOC_REINIT: %v", ErrBackendFailure, errno)
	}
	return nil
}

// closeRequest closes a request fd.
func closeRequest(reqFD int) {
	if reqFD >= 0 {
		syscall.Close(reqFD)
	}
}

// findMediaForVideo returns the /dev/mediaN node whose topology exposes
// the given /dev/videoN interface. The association is discovered by
// matching the driver name reported by both nodes' QUERYCAP — the rpi
// decoder's media and video nodes share the driver string "rpi-hevc-dec".
// Returns "" if no matching media node is found.
func findMediaForVideo(videoDriver string) string {
	for _, mpath := range listMediaNodes() {
		fd, err := syscall.Open(mpath, syscall.O_RDWR, 0)
		if err != nil {
			continue
		}
		var info mediaDeviceInfo
		errno := ioctl(fd, mediaIOCDeviceInfo, unsafe.Pointer(&info))
		syscall.Close(fd)
		if errno != 0 {
			continue
		}
		if cstr(info.Driver[:]) == videoDriver {
			return mpath
		}
	}
	return ""
}

// mediaDeviceInfo mirrors the leading fields of struct media_device_info;
// only the driver name is read to match a media node to its video node.
type mediaDeviceInfo struct {
	Driver        [16]byte
	Model         [32]byte
	Serial        [40]byte
	BusInfo       [32]byte
	MediaVersion  uint32
	HwRevision    uint32
	DriverVersion uint32
	Reserved      [31]uint32
}

// mediaIOCDeviceInfo is MEDIA_IOC_DEVICE_INFO = _IOWR('|', 0, struct
// media_device_info).
var mediaIOCDeviceInfo = iowr(iocTypeReq, 0x00, unsafe.Sizeof(mediaDeviceInfo{}))
