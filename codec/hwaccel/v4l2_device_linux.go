//go:build linux

// Device + multiplanar M2M queue helpers shared by the stateless and
// stateful v4l2 sessions: opening a /dev/videoN node, querying its
// capability, setting a multiplanar format, allocating and mmap-ing a
// queue of MMAP buffers, queueing/dequeueing buffers, and streamon/off.
//
// A v4l2Queue owns one direction (OUTPUT = coded, CAPTURE = raw) of a
// multiplanar M2M device: its V4L2 buffers and their mmap'd plane memory.

package hwaccel

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// v4l2Device is an opened M2M video node plus its queried capability.
type v4l2Device struct {
	path string
	fd   int
	cap  v4l2Capability
}

// openV4L2Device opens path O_RDWR and queries its capability.
func openV4L2Device(path string) (*v4l2Device, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: open %s: %v", ErrBackendFailure, path, err)
	}
	d := &v4l2Device{path: path, fd: fd}
	if errno := ioctl(fd, vidiocQueryCap, unsafe.Pointer(&d.cap)); errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("%w: VIDIOC_QUERYCAP %s: %v", ErrBackendFailure, path, errno)
	}
	return d, nil
}

// close releases the node fd.
func (d *v4l2Device) close() error {
	if d.fd < 0 {
		return nil
	}
	err := syscall.Close(d.fd)
	d.fd = -1
	return err
}

// deviceCaps returns the per-device capability bitmask (preferring
// device_caps when V4L2_CAP_DEVICE_CAPS is set, else the union caps).
func (d *v4l2Device) deviceCaps() uint32 {
	if d.cap.Capabilities&v4l2CapDeviceCaps != 0 {
		return d.cap.DeviceCaps
	}
	return d.cap.Capabilities
}

// isM2MMplane reports whether the node is a multiplanar memory-to-memory
// codec device — the shape both the stateless and stateful paths require.
func (d *v4l2Device) isM2MMplane() bool {
	return d.deviceCaps()&v4l2CapVideoM2MMplane != 0
}

// driverName returns the NUL-trimmed driver string.
func (d *v4l2Device) driverName() string {
	return cstr(d.cap.Driver[:])
}

// enumFormats returns the pixel formats the node advertises on the given
// buffer type (V4L2_BUF_TYPE_VIDEO_OUTPUT_MPLANE / CAPTURE_MPLANE).
func (d *v4l2Device) enumFormats(bufType uint32) []uint32 {
	var out []uint32
	for i := uint32(0); ; i++ {
		fd := v4l2Fmtdesc{Index: i, Type: bufType}
		if errno := ioctl(d.fd, vidiocEnumFmt, unsafe.Pointer(&fd)); errno != 0 {
			break
		}
		out = append(out, fd.PixelFormat)
	}
	return out
}

// hasControl reports whether the node exposes the given control id.
func (d *v4l2Device) hasControl(id uint32) bool {
	q := v4l2QueryExtCtrl{ID: id}
	return ioctl(d.fd, vidiocQueryExtCtrl, unsafe.Pointer(&q)) == 0
}

// setFormatMP sets a multiplanar format on bufType and returns the format
// the driver accepted (G_FMT semantics: S_FMT fills in the negotiated
// strides/sizes). planeSize, when non-zero, seeds plane_fmt[0].sizeimage
// for coded queues whose size the driver cannot infer.
func (d *v4l2Device) setFormatMP(bufType, pixfmt uint32, w, h, numPlanes, planeSize int) (v4l2PixFormatMplane, error) {
	var f v4l2Format
	f.Type = bufType
	mp := f.pixMP()
	mp.Width = uint32(w)
	mp.Height = uint32(h)
	mp.PixelFormat = pixfmt
	mp.Field = v4l2FieldNone
	mp.NumPlanes = uint8(numPlanes)
	if planeSize > 0 {
		mp.PlaneFmt[0].SizeImage = uint32(planeSize)
	}
	if errno := ioctl(d.fd, vidiocSFmt, unsafe.Pointer(&f)); errno != 0 {
		return v4l2PixFormatMplane{}, fmt.Errorf("%w: VIDIOC_S_FMT type=%d fmt=0x%x: %v",
			ErrBackendFailure, bufType, pixfmt, errno)
	}
	return *f.pixMP(), nil
}

// getFormatMP reads the current multiplanar format on bufType.
func (d *v4l2Device) getFormatMP(bufType uint32) (v4l2PixFormatMplane, error) {
	var f v4l2Format
	f.Type = bufType
	if errno := ioctl(d.fd, vidiocGFmt, unsafe.Pointer(&f)); errno != 0 {
		return v4l2PixFormatMplane{}, fmt.Errorf("%w: VIDIOC_G_FMT type=%d: %v",
			ErrBackendFailure, bufType, errno)
	}
	return *f.pixMP(), nil
}

// subscribeEvent subscribes to a V4L2 event on the node.
func (d *v4l2Device) subscribeEvent(eventType uint32) error {
	sub := v4l2EventSubscription{Type: eventType}
	if errno := ioctl(d.fd, vidiocSubscribeEvent, unsafe.Pointer(&sub)); errno != 0 {
		return fmt.Errorf("%w: VIDIOC_SUBSCRIBE_EVENT type=%d: %v", ErrBackendFailure, eventType, errno)
	}
	return nil
}

// dqEvent dequeues one pending event, or reports false if none is pending
// (EINVAL/ENOENT).
func (d *v4l2Device) dqEvent() (v4l2Event, bool) {
	var e v4l2Event
	if errno := ioctl(d.fd, vidiocDQEvent, unsafe.Pointer(&e)); errno != 0 {
		return v4l2Event{}, false
	}
	return e, true
}

// streamOn / streamOff toggle streaming on a buffer type.
func (d *v4l2Device) streamOn(bufType uint32) error {
	t := int32(bufType)
	if errno := ioctl(d.fd, vidiocStreamOn, unsafe.Pointer(&t)); errno != 0 {
		return fmt.Errorf("%w: VIDIOC_STREAMON type=%d: %v", ErrBackendFailure, bufType, errno)
	}
	return nil
}

func (d *v4l2Device) streamOff(bufType uint32) error {
	t := int32(bufType)
	if errno := ioctl(d.fd, vidiocStreamOff, unsafe.Pointer(&t)); errno != 0 {
		return fmt.Errorf("%w: VIDIOC_STREAMOFF type=%d: %v", ErrBackendFailure, bufType, errno)
	}
	return nil
}

// decoderStop issues VIDIOC_DECODER_CMD(V4L2_DEC_CMD_STOP) to drain a
// stateful decoder; the driver then emits V4L2_BUF_FLAG_LAST on the final
// CAPTURE buffer.
func (d *v4l2Device) decoderStop() error {
	cmd := v4l2DecoderCmd{Cmd: v4l2DecCmdStop}
	if errno := ioctl(d.fd, vidiocDecoderCmd, unsafe.Pointer(&cmd)); errno != 0 {
		return fmt.Errorf("%w: VIDIOC_DECODER_CMD STOP: %v", ErrBackendFailure, errno)
	}
	return nil
}

// ---- queue ------------------------------------------------------------

// v4l2Buf is one allocated, mmap'd buffer in a queue: its index and the
// mapped memory of each plane.
type v4l2Buf struct {
	index  uint32
	planes [][]byte
}

// v4l2Queue owns one direction of an M2M device: a fixed set of MMAP
// buffers and their mapped plane memory.
type v4l2Queue struct {
	dev       *v4l2Device
	bufType   uint32
	numPlanes int
	bufs      []v4l2Buf
}

// reqbufs allocates count MMAP buffers on bufType, then QUERYBUFs and
// mmaps every plane of each. numPlanes is the multiplanar plane count.
func newV4L2Queue(dev *v4l2Device, bufType uint32, count, numPlanes int) (*v4l2Queue, error) {
	rb := v4l2Requestbuffers{Count: uint32(count), Type: bufType, Memory: v4l2MemoryMMAP}
	if errno := ioctl(dev.fd, vidiocReqbufs, unsafe.Pointer(&rb)); errno != 0 {
		return nil, fmt.Errorf("%w: VIDIOC_REQBUFS type=%d count=%d: %v",
			ErrBackendFailure, bufType, count, errno)
	}
	q := &v4l2Queue{dev: dev, bufType: bufType, numPlanes: numPlanes}
	for i := uint32(0); i < rb.Count; i++ {
		buf, err := q.queryAndMap(i)
		if err != nil {
			q.free()
			return nil, err
		}
		q.bufs = append(q.bufs, buf)
	}
	return q, nil
}

// queryAndMap QUERYBUFs buffer i and mmaps each of its planes.
func (q *v4l2Queue) queryAndMap(index uint32) (v4l2Buf, error) {
	planes := make([]v4l2Plane, q.numPlanes)
	var b v4l2Buffer
	b.Index = index
	b.Type = q.bufType
	b.Memory = v4l2MemoryMMAP
	b.Length = uint32(q.numPlanes)
	b.M = uint64(uintptr(unsafe.Pointer(&planes[0])))
	if errno := ioctl(q.dev.fd, vidiocQuerybuf, unsafe.Pointer(&b)); errno != 0 {
		return v4l2Buf{}, fmt.Errorf("%w: VIDIOC_QUERYBUF idx=%d: %v", ErrBackendFailure, index, errno)
	}
	out := v4l2Buf{index: index}
	for p := 0; p < q.numPlanes; p++ {
		mem, err := mmapBuffer(q.dev.fd, uint32(planes[p].M), int(planes[p].Length))
		if err != nil {
			return v4l2Buf{}, fmt.Errorf("%w: mmap idx=%d plane=%d: %v", ErrBackendFailure, index, p, err)
		}
		out.planes = append(out.planes, mem)
	}
	return out, nil
}

// free unmaps every plane and drops the buffers (REQBUFS count=0 is left
// to the caller's streamoff/close path).
func (q *v4l2Queue) free() {
	for _, b := range q.bufs {
		for _, p := range b.planes {
			munmapBuffer(p)
		}
	}
	q.bufs = nil
}

// qbuf queues buffer i. bytesUsed[p] sets plane p's payload length (for
// OUTPUT buffers); requestFD, when >= 0, binds the buffer to a request
// (V4L2_BUF_FLAG_REQUEST_FD). timestamp is the buffer timestamp used as
// the DPB reference key for the stateless decoder.
func (q *v4l2Queue) qbuf(index uint32, bytesUsed []int, requestFD int, ts syscall.Timeval) error {
	planes := make([]v4l2Plane, q.numPlanes)
	for p := 0; p < q.numPlanes; p++ {
		if p < len(bytesUsed) {
			planes[p].BytesUsed = uint32(bytesUsed[p])
		}
	}
	var b v4l2Buffer
	b.Index = index
	b.Type = q.bufType
	b.Memory = v4l2MemoryMMAP
	b.Field = v4l2FieldNone
	b.Length = uint32(q.numPlanes)
	b.M = uint64(uintptr(unsafe.Pointer(&planes[0])))
	b.Timestamp = ts
	if requestFD >= 0 {
		b.Flags = v4l2BufFlagRequestFD
		b.RequestFD = int32(requestFD)
	}
	if errno := ioctl(q.dev.fd, vidiocQBuf, unsafe.Pointer(&b)); errno != 0 {
		return fmt.Errorf("%w: VIDIOC_QBUF type=%d idx=%d: %v", ErrBackendFailure, q.bufType, index, errno)
	}
	return nil
}

// dqbuf dequeues one completed buffer, returning its index, the per-plane
// bytesused, the buffer flags, and its timestamp. It reports ok=false on
// EAGAIN (nothing ready yet).
func (q *v4l2Queue) dqbuf() (index uint32, bytesUsed []int, flags uint32, ts syscall.Timeval, ok bool, err error) {
	planes := make([]v4l2Plane, q.numPlanes)
	var b v4l2Buffer
	b.Type = q.bufType
	b.Memory = v4l2MemoryMMAP
	b.Length = uint32(q.numPlanes)
	b.M = uint64(uintptr(unsafe.Pointer(&planes[0])))
	errno := ioctl(q.dev.fd, vidiocDQBuf, unsafe.Pointer(&b))
	if errno == syscall.EAGAIN {
		return 0, nil, 0, syscall.Timeval{}, false, nil
	}
	if errno != 0 {
		return 0, nil, 0, syscall.Timeval{}, false,
			fmt.Errorf("%w: VIDIOC_DQBUF type=%d: %v", ErrBackendFailure, q.bufType, errno)
	}
	bu := make([]int, q.numPlanes)
	for p := 0; p < q.numPlanes; p++ {
		bu[p] = int(planes[p].BytesUsed)
	}
	return b.Index, bu, b.Flags, b.Timestamp, true, nil
}

// pollIn waits up to timeout for the node to become readable/writable for
// a DQBUF or DQEVENT. It polls POLLIN|POLLOUT|POLLPRI so a single call
// covers CAPTURE-ready, OUTPUT-recyclable, and event-pending.
func (d *v4l2Device) poll(timeoutMS int) (readable, writable, priority bool, err error) {
	fds := []pollFD{{fd: int32(d.fd), events: pollIn | pollOut | pollPri}}
	n, perr := pollWait(fds, timeoutMS)
	if perr != nil {
		return false, false, false, fmt.Errorf("%w: poll: %v", ErrBackendFailure, perr)
	}
	if n == 0 {
		return false, false, false, nil
	}
	re := fds[0].revents
	return re&pollIn != 0, re&pollOut != 0, re&pollPri != 0, nil
}

// ---- poll syscall (no x/sys dependency) -------------------------------

const (
	pollIn  = 0x0001
	pollPri = 0x0002
	pollOut = 0x0004
)

type pollFD struct {
	fd      int32
	events  int16
	revents int16
}

// pollWait wraps the poll(2) syscall directly. timeoutMS < 0 blocks.
func pollWait(fds []pollFD, timeoutMS int) (int, error) {
	if len(fds) == 0 {
		return 0, nil
	}
	n, _, errno := syscall.Syscall(syscall.SYS_PPOLL,
		uintptr(unsafe.Pointer(&fds[0])), uintptr(len(fds)),
		uintptr(unsafe.Pointer(ppollTimeout(timeoutMS))))
	if errno != 0 {
		if errno == syscall.EINTR {
			return 0, nil
		}
		return 0, errno
	}
	return int(n), nil
}

// ppollTimeout builds the timespec ppoll expects, or nil for an infinite
// wait. The returned pointer is kept alive by the caller's stack frame.
func ppollTimeout(ms int) *syscall.Timespec {
	if ms < 0 {
		return nil
	}
	ts := syscall.Timespec{Sec: int64(ms / 1000), Nsec: int64(ms%1000) * 1_000_000}
	return &ts
}

// ---- misc helpers -----------------------------------------------------

// cstr trims a fixed-size NUL-padded C string.
func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// listVideoNodes returns the /dev/videoN paths present on the host.
func listVideoNodes() []string {
	entries, err := os.ReadDir("/dev")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if len(name) > 5 && name[:5] == "video" {
			out = append(out, "/dev/"+name)
		}
	}
	return out
}

// listMediaNodes returns the /dev/mediaN paths present on the host.
func listMediaNodes() []string {
	entries, err := os.ReadDir("/dev")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if len(name) > 5 && name[:5] == "media" {
			out = append(out, "/dev/"+name)
		}
	}
	return out
}
