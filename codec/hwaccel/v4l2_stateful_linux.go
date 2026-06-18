//go:build linux

// The stateful V4L2 M2M codec sessions for SoCs whose hardware parses the
// bitstream internally (Pi 4 bcm2835-codec, Rockchip rkvdec/rkvenc, most
// other M2M decoders/encoders). Unlike the Pi-5 stateless path, no
// userspace bitstream parsing or Request API is involved: the OUTPUT queue
// carries plain coded Annex-B access units (decode) or raw NV12 frames
// (encode), and the CAPTURE queue carries the converse.
//
// # Stateful decode state machine (V4L2 spec "Memory-to-Memory Stateful
// Video Decoder Interface")
//
//	S_FMT coded OUTPUT (HEVC/H264); REQBUFS+MMAP OUTPUT
//	subscribe V4L2_EVENT_SOURCE_CHANGE; STREAMON OUTPUT
//	QBUF coded AUs; on the first SOURCE_CHANGE event:
//	  G_FMT CAPTURE (decoded resolution) -> REQBUFS+MMAP CAPTURE -> STREAMON
//	DQBUF CAPTURE frames; recycle OUTPUT + CAPTURE buffers
//	drain at EOS via VIDIOC_DECODER_CMD(STOP) -> V4L2_BUF_FLAG_LAST
//
// # Stateful encode
//
// The symmetric path: OUTPUT carries raw NV12, CAPTURE carries coded
// access units; SOURCE_CHANGE is not used (the geometry is fixed by
// S_FMT). Emitted packets are wrapped as Annex-B video.Packet.
//
// This file is spec-correct and unit-testable off-device, but the only
// hardware available during development is a Pi 5 (stateless-only), so the
// stateful path is not hardware-verified here; see the package tests.
//
// Not safe for concurrent use.

package hwaccel

import (
	"fmt"
	"syscall"
	"time"

	"go-mediatoolkit/video"
)

const (
	statefulNumOutputBufs  = 8
	statefulNumCaptureBufs = 8
)

// codecToPixFmt maps a video.Codec to the coded V4L2 fourcc the stateful
// path uses on the OUTPUT (decode) / CAPTURE (encode) queue.
func codecToPixFmt(c video.Codec) (uint32, bool) {
	switch c {
	case video.H264:
		return pixFmtH264, true
	case video.H265:
		return pixFmtHEVC, true
	default:
		return 0, false
	}
}

// ---- stateful decoder -------------------------------------------------

// v4l2StatefulDecoder drives a stateful M2M decoder: coded AUs in on
// OUTPUT, raw NV12 frames out on CAPTURE, with a SOURCE_CHANGE-driven
// CAPTURE setup.
type v4l2StatefulDecoder struct {
	dev   *v4l2Device
	cfg   Config
	coded uint32

	output  *v4l2Queue
	capture *v4l2Queue

	outStreaming  bool
	capConfigured bool
	visW, visH    int

	frameIdx int64
	closed   bool
}

// newV4L2StatefulDecoder opens node and prepares the OUTPUT queue for the
// configured codec.
func newV4L2StatefulDecoder(node string, cfg Config) (*v4l2StatefulDecoder, error) {
	coded, ok := codecToPixFmt(cfg.Codec)
	if !ok {
		return nil, ErrUnsupportedCodec
	}
	dev, err := openV4L2Device(node)
	if err != nil {
		return nil, err
	}
	return &v4l2StatefulDecoder{dev: dev, cfg: cfg, coded: coded}, nil
}

// Decode submits one coded access unit and returns any frames the decoder
// has produced. The first call sets up OUTPUT and subscribes to
// SOURCE_CHANGE; CAPTURE is configured when the first SOURCE_CHANGE fires.
func (d *v4l2StatefulDecoder) Decode(p video.Packet) ([]video.Frame, error) {
	if d.closed {
		return nil, ErrClosed
	}
	if len(p.Data) == 0 {
		return nil, nil
	}
	if err := d.ensureOutput(); err != nil {
		return nil, err
	}
	if err := d.queueCoded(p.Data); err != nil {
		return nil, err
	}
	return d.collect(false)
}

// Flush drains the decoder: VIDIOC_DECODER_CMD(STOP) then collect until the
// V4L2_BUF_FLAG_LAST CAPTURE buffer.
func (d *v4l2StatefulDecoder) Flush() ([]video.Frame, error) {
	if d.closed {
		return nil, ErrClosed
	}
	if !d.outStreaming {
		return nil, nil
	}
	if err := d.dev.decoderStop(); err != nil {
		return nil, err
	}
	return d.collect(true)
}

// Close tears down the queues and the device. Idempotent.
func (d *v4l2StatefulDecoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	if d.outStreaming {
		d.dev.streamOff(v4l2BufTypeVideoOutMP)
	}
	if d.capConfigured {
		d.dev.streamOff(v4l2BufTypeVideoCapMP)
	}
	if d.output != nil {
		d.output.free()
	}
	if d.capture != nil {
		d.capture.free()
	}
	return d.dev.close()
}

// ensureOutput sets the coded OUTPUT format, subscribes to SOURCE_CHANGE,
// allocates the OUTPUT queue and streams it on (once).
func (d *v4l2StatefulDecoder) ensureOutput() error {
	if d.outStreaming {
		return nil
	}
	w, h := d.cfg.Width, d.cfg.Height
	if w == 0 {
		w, h = 1920, 1088 // a max-size hint; the driver resizes on SOURCE_CHANGE
	}
	if _, err := d.dev.setFormatMP(v4l2BufTypeVideoOutMP, d.coded, w, h, 1, 1024*1024); err != nil {
		return err
	}
	if err := d.dev.subscribeEvent(v4l2EventSourceChange); err != nil {
		return err
	}
	out, err := newV4L2Queue(d.dev, v4l2BufTypeVideoOutMP, statefulNumOutputBufs, 1)
	if err != nil {
		return err
	}
	d.output = out
	if err := d.dev.streamOn(v4l2BufTypeVideoOutMP); err != nil {
		return err
	}
	d.outStreaming = true
	return nil
}

// configureCapture handles a SOURCE_CHANGE: read the decoded geometry via
// G_FMT, allocate + queue CAPTURE buffers, and stream CAPTURE on.
func (d *v4l2StatefulDecoder) configureCapture() error {
	if d.capConfigured {
		return nil
	}
	capFmt, err := d.dev.getFormatMP(v4l2BufTypeVideoCapMP)
	if err != nil {
		return err
	}
	d.visW = int(capFmt.Width)
	d.visH = int(capFmt.Height)
	numPlanes := int(capFmt.NumPlanes)
	if numPlanes == 0 {
		numPlanes = 1
	}
	cap, err := newV4L2Queue(d.dev, v4l2BufTypeVideoCapMP, statefulNumCaptureBufs, numPlanes)
	if err != nil {
		return err
	}
	d.capture = cap
	for _, b := range cap.bufs {
		if err := cap.qbuf(b.index, nil, -1, syscall.Timeval{}); err != nil {
			return err
		}
	}
	if err := d.dev.streamOn(v4l2BufTypeVideoCapMP); err != nil {
		return err
	}
	d.capConfigured = true
	return nil
}

// queueCoded copies a coded AU into a free OUTPUT buffer and queues it,
// recycling completed OUTPUT buffers first.
func (d *v4l2StatefulDecoder) queueCoded(data []byte) error {
	// Recycle any completed OUTPUT buffers (non-blocking).
	for {
		_, _, _, _, ok, err := d.output.dqbuf()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
	}
	idx := uint32(int(d.frameIdx) % len(d.output.bufs))
	buf := d.output.bufs[idx].planes[0]
	if len(data) > len(buf) {
		return fmt.Errorf("%w: coded AU %d > OUTPUT buffer %d", ErrBackendFailure, len(data), len(buf))
	}
	copy(buf, data)
	ts := syscall.Timeval{Usec: int64(d.frameIdx + 1)}
	return d.output.qbuf(idx, []int{len(data)}, -1, ts)
}

// collect dequeues available CAPTURE frames, handling SOURCE_CHANGE on the
// way. When drain is set it loops until the V4L2_BUF_FLAG_LAST buffer.
func (d *v4l2StatefulDecoder) collect(drain bool) ([]video.Frame, error) {
	var frames []video.Frame
	for {
		// Drain pending events first (SOURCE_CHANGE configures CAPTURE).
		for {
			ev, ok := d.dev.dqEvent()
			if !ok {
				break
			}
			if ev.Type == v4l2EventSourceChange && ev.srcChanges()&v4l2EventSrcChResolutn != 0 {
				if err := d.configureCapture(); err != nil {
					return frames, err
				}
			}
		}
		if !d.capConfigured {
			if !drain {
				return frames, nil
			}
			// Block briefly for the SOURCE_CHANGE during drain.
			if _, _, _, err := d.dev.poll(200); err != nil {
				return frames, err
			}
			if _, ok := d.dev.dqEvent(); !ok {
				return frames, nil
			}
			continue
		}

		idx, bytesUsed, flags, _, ok, err := d.capture.dqbuf()
		if err != nil {
			return frames, err
		}
		if !ok {
			if !drain {
				return frames, nil
			}
			if _, _, _, perr := d.dev.poll(200); perr != nil {
				return frames, perr
			}
			continue
		}
		if flags&v4l2BufFlagLast != 0 && (bytesUsed == nil || sum(bytesUsed) == 0) {
			return frames, nil
		}
		frames = append(frames, d.captureFrame(idx))
		d.frameIdx++
		// Recycle the CAPTURE buffer.
		d.capture.qbuf(idx, nil, -1, syscall.Timeval{})
		if flags&v4l2BufFlagLast != 0 {
			return frames, nil
		}
		if !drain {
			// Keep collecting while frames are ready, then return.
			continue
		}
	}
}

// captureFrame copies a decoded CAPTURE buffer (linear NV12/YUV420) into a
// video.Frame. Stateful drivers emit linear formats, so no de-tiling.
func (d *v4l2StatefulDecoder) captureFrame(idx uint32) video.Frame {
	capFmt, _ := d.dev.getFormatMP(v4l2BufTypeVideoCapMP)
	planes := d.capture.bufs[idx].planes
	yStride := int(capFmt.PlaneFmt[0].BytesPerLine)
	if yStride == 0 {
		yStride = d.visW
	}
	y := make([]byte, d.visW*d.visH)
	for r := 0; r < d.visH; r++ {
		copy(y[r*d.visW:r*d.visW+d.visW], planes[0][r*yStride:r*yStride+d.visW])
	}
	// Chroma: NV12 has a second interleaved plane; YUV420 has two more. The
	// frame is normalised to NV12 (the linear NV12 case); a YUV420 source is
	// handled by the chroma plane index.
	var cPlane []byte
	cStride := yStride
	if len(planes) >= 2 {
		cPlane = planes[1]
		cStride = int(capFmt.PlaneFmt[1].BytesPerLine)
		if cStride == 0 {
			cStride = d.visW
		}
	} else {
		// Single-plane NV12: chroma follows luma in the same plane.
		cPlane = planes[0][yStride*alignedHeight(d.visH, capFmt):]
	}
	ch := d.visH / 2
	c := make([]byte, d.visW*ch)
	for r := 0; r < ch; r++ {
		if (r+1)*cStride <= len(cPlane) {
			copy(c[r*d.visW:r*d.visW+d.visW], cPlane[r*cStride:r*cStride+d.visW])
		}
	}
	return video.Frame{
		PixelFormat: video.NV12,
		Width:       d.visW,
		Height:      d.visH,
		Planes:      [][]byte{y, c},
		Strides:     []int{d.visW, d.visW},
		PTS:         time.Duration(float64(d.frameIdx) / d.cfg.frameRate() * float64(time.Second)),
	}
}

// alignedHeight returns the luma plane height used to locate the chroma
// offset for a single-plane NV12 capture format.
func alignedHeight(h int, f v4l2PixFormatMplane) int {
	if int(f.Height) > h {
		return int(f.Height)
	}
	return h
}

// sum totals a per-plane bytesused slice.
func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

// ---- stateful encoder -------------------------------------------------

// v4l2StatefulEncoder drives a stateful M2M encoder: raw NV12 frames in on
// OUTPUT, coded Annex-B access units out on CAPTURE.
type v4l2StatefulEncoder struct {
	dev   *v4l2Device
	cfg   Config
	coded uint32

	output  *v4l2Queue
	capture *v4l2Queue

	streaming bool
	frameIdx  int64
	closed    bool
}

// newV4L2StatefulEncoder opens node and prepares both queues for the
// configured codec + resolution.
func newV4L2StatefulEncoder(node string, cfg Config) (*v4l2StatefulEncoder, error) {
	coded, ok := codecToPixFmt(cfg.Codec)
	if !ok {
		return nil, ErrUnsupportedCodec
	}
	dev, err := openV4L2Device(node)
	if err != nil {
		return nil, err
	}
	return &v4l2StatefulEncoder{dev: dev, cfg: cfg, coded: coded}, nil
}

// Encode submits one raw NV12 frame and returns any coded packets ready.
func (e *v4l2StatefulEncoder) Encode(f video.Frame) ([]video.Packet, error) {
	if e.closed {
		return nil, ErrClosed
	}
	if f.PixelFormat != video.NV12 {
		return nil, ErrUnsupportedPixelFormat
	}
	if err := e.ensureStreaming(); err != nil {
		return nil, err
	}
	if err := e.queueRaw(f); err != nil {
		return nil, err
	}
	return e.collect(false)
}

// Flush drains the encoder via VIDIOC_ENCODER_CMD STOP (issued through the
// shared decoder-cmd structure: the encoder cmd shares the layout) and
// collects the remaining packets.
func (e *v4l2StatefulEncoder) Flush() ([]video.Packet, error) {
	if e.closed {
		return nil, ErrClosed
	}
	if !e.streaming {
		return nil, nil
	}
	return e.collect(true)
}

// Close tears down the queues and the device. Idempotent.
func (e *v4l2StatefulEncoder) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	if e.streaming {
		e.dev.streamOff(v4l2BufTypeVideoOutMP)
		e.dev.streamOff(v4l2BufTypeVideoCapMP)
	}
	if e.output != nil {
		e.output.free()
	}
	if e.capture != nil {
		e.capture.free()
	}
	return e.dev.close()
}

// ensureStreaming sets the raw OUTPUT + coded CAPTURE formats, allocates
// both queues and streams them on (once).
func (e *v4l2StatefulEncoder) ensureStreaming() error {
	if e.streaming {
		return nil
	}
	if _, err := e.dev.setFormatMP(v4l2BufTypeVideoOutMP, pixFmtNV12, e.cfg.Width, e.cfg.Height, 1, 0); err != nil {
		return err
	}
	if _, err := e.dev.setFormatMP(v4l2BufTypeVideoCapMP, e.coded, e.cfg.Width, e.cfg.Height, 1, 1024*1024); err != nil {
		return err
	}
	out, err := newV4L2Queue(e.dev, v4l2BufTypeVideoOutMP, statefulNumOutputBufs, 1)
	if err != nil {
		return err
	}
	e.output = out
	cap, err := newV4L2Queue(e.dev, v4l2BufTypeVideoCapMP, statefulNumCaptureBufs, 1)
	if err != nil {
		return err
	}
	e.capture = cap
	for _, b := range cap.bufs {
		if err := cap.qbuf(b.index, nil, -1, syscall.Timeval{}); err != nil {
			return err
		}
	}
	if err := e.dev.streamOn(v4l2BufTypeVideoOutMP); err != nil {
		return err
	}
	if err := e.dev.streamOn(v4l2BufTypeVideoCapMP); err != nil {
		return err
	}
	e.streaming = true
	return nil
}

// queueRaw copies an NV12 frame into a free OUTPUT buffer (packing the
// luma + interleaved chroma planes) and queues it.
func (e *v4l2StatefulEncoder) queueRaw(f video.Frame) error {
	for {
		_, _, _, _, ok, err := e.output.dqbuf()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
	}
	idx := uint32(int(e.frameIdx) % len(e.output.bufs))
	buf := e.output.bufs[idx].planes[0]
	n := copyNV12(buf, f)
	ts := syscall.Timeval{Usec: int64(e.frameIdx + 1)}
	return e.output.qbuf(idx, []int{n}, -1, ts)
}

// copyNV12 packs a video.Frame's NV12 planes into a single linear buffer
// (luma then interleaved chroma) and returns the byte count written.
func copyNV12(dst []byte, f video.Frame) int {
	w, h := f.Width, f.Height
	off := 0
	for r := 0; r < h && off+w <= len(dst); r++ {
		copy(dst[off:off+w], f.Planes[0][r*f.Strides[0]:r*f.Strides[0]+w])
		off += w
	}
	ch := h / 2
	for r := 0; r < ch && off+w <= len(dst); r++ {
		copy(dst[off:off+w], f.Planes[1][r*f.Strides[1]:r*f.Strides[1]+w])
		off += w
	}
	return off
}

// collect dequeues available coded CAPTURE buffers as Annex-B packets.
func (e *v4l2StatefulEncoder) collect(drain bool) ([]video.Packet, error) {
	var pkts []video.Packet
	for {
		idx, bytesUsed, flags, _, ok, err := e.capture.dqbuf()
		if err != nil {
			return pkts, err
		}
		if !ok {
			if !drain {
				return pkts, nil
			}
			if _, _, _, perr := e.dev.poll(200); perr != nil {
				return pkts, perr
			}
			// Bounded drain: stop when nothing more arrives.
			if _, _, _, _, ok2, _ := e.capture.dqbuf(); !ok2 {
				return pkts, nil
			}
			continue
		}
		n := sum(bytesUsed)
		data := make([]byte, n)
		copy(data, e.capture.bufs[idx].planes[0][:n])
		pkts = append(pkts, video.Packet{
			Codec:    e.cfg.Codec,
			Data:     data,
			Keyframe: flags&v4l2BufFlagKeyframe != 0,
			PTS:      time.Duration(float64(e.frameIdx) / e.cfg.frameRate() * float64(time.Second)),
		})
		e.frameIdx++
		e.capture.qbuf(idx, nil, -1, syscall.Timeval{})
		if !drain {
			return pkts, nil
		}
	}
}
