//go:build linux

// VA-API low-power (VAEntrypointEncSliceLP) CBR encode for H.264 and
// H.265. The encoder is all-intra: every frame is coded as an IDR with no
// inter prediction, which keeps the reference-picture bookkeeping trivial
// and makes each packet independently decodable — the form an NVR /
// elementary-stream consumer wants. The driver encodes the slice; this
// code authors the SPS/PPS (and VPS for H.265) RBSPs, submits them as
// packed headers so the driver embeds them into IDR coded buffers, then
// reads the coded buffer back and rewrites it to Annex-B.
//
// # Frame flow
//
//	upload NV12 -> vaPutImage to the input surface
//	build seq/pic/slice/rate-control param buffers + packed SPS/PPS(/VPS)
//	vaBeginPicture(surface) -> vaRenderPicture(bufs) -> vaEndPicture
//	vaSyncSurface(surface)
//	map the coded buffer, walk the VACodedBufferSegment chain
//	-> Annex-B video.Packet (start-code prefixed)
//
// Not safe for concurrent use.

package hwaccel

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// vaEncoder drives a single VA-API low-power encode pipeline. All state is
// created lazily on the first Encode (once the input geometry is known
// from cfg) and torn down in Close. Not safe for concurrent use.
type vaEncoder struct {
	b     *vaBackend
	cfg   Config
	codec video.Codec

	width   int
	height  int
	profile int32

	configID uint32
	context  uint32
	surface  uint32
	codedBuf uint32
	started  bool

	// Authored parameter-set RBSPs (Annex-B form, start-code prefixed),
	// cached so non-keyframe paths and packet prefixing reuse them.
	sps []byte
	pps []byte
	vps []byte

	frameIdx int64
	closed   bool
}

// newVAEncoder validates cfg and returns an encoder; the VA pipeline is
// built on the first Encode call.
//
// VP9 and AV1 low-power encode are gated off on the Intel iHD media-driver
// (verified against intel-media-va-driver-non-free 25.4.2 on an Arc A380 / DG2,
// libva 2.22): although vainfo advertises VAEntrypointEncSliceLP for
// VAProfileVP9Profile0..3 and VAProfileAV1Profile0, the driver's picture
// submission is not functional. A bare, spec-conformant VAEncPicture submission
// — and, independently, ffmpeg's own production vp9_vaapi / av1_vaapi encoders
// fed the identical input on the same box — both fail: vp9_vaapi returns
// VA_STATUS_ERROR_ENCODING_ERROR (24) from vaEndPicture, and av1_vaapi cannot
// even open ("Function not implemented"). The QSV/oneVPL path (vp9_qsv) does
// encode VP9 on this silicon, so the limitation is in the raw VAAPI encode
// kernels of this driver build, not the hardware. The parameter buffers built
// by buildVP9Params/buildAV1Params are layout-verified and left in place so the
// path lights up on a driver that fixes the entrypoint; until then the encoder
// degrades loudly via ErrEncodeUnsupportedOnDriver rather than emitting broken
// packets. (VP9/AV1 *decode* is fully functional — see vaapi_decode_vp9_linux.go.)
func newVAEncoder(b *vaBackend, cfg Config) (Encoder, error) {
	w, h := cfg.Width, cfg.Height
	if w <= 0 || h <= 0 {
		return nil, ErrInvalidConfig
	}
	if cfg.Codec == video.VP9 || cfg.Codec == video.AV1 {
		return nil, ErrEncodeUnsupportedOnDriver
	}
	return &vaEncoder{
		b:        b,
		cfg:      cfg,
		codec:    cfg.Codec,
		width:    w,
		height:   h,
		profile:  vaProfileFor(cfg.Codec, cfg.Profile),
		configID: vaInvalidID,
		context:  vaInvalidID,
		surface:  vaInvalidSurface,
		codedBuf: vaInvalidID,
	}, nil
}

// ensurePipeline lazily creates the VA config, context, input surface and
// coded buffer, and authors the parameter-set RBSPs.
func (e *vaEncoder) ensurePipeline() error {
	if e.started {
		return nil
	}
	l := e.b.lib
	dpy := e.b.display

	// Config: EncSliceLP, YUV420 RT format, constant-QP rate control (the
	// low-power encoder's simplest, HRD-free mode — ample for an all-intra
	// near-lossless transcode), and packed sequence/picture headers so we
	// can supply our own SPS/PPS/VPS.
	attrs := []vaConfigAttrib{
		{Type: vaConfigAttribRTFormat, Value: vaRTFormatYUV420},
		{Type: vaConfigAttribRateControl, Value: e.rateControlMode()},
		{Type: vaConfigAttribEncPackedHeaders, Value: e.packedHeaderAttrib()},
	}
	var configID uint32
	st := l.vaCreateConfig(dpy, e.profile, vaEntrypointEncSliceLP,
		unsafe.Pointer(&attrs[0]), int32(len(attrs)), &configID)
	if st != vaStatusSuccess {
		return fmt.Errorf("%w: vaCreateConfig(enc) VAStatus=%d", ErrBackendFailure, st)
	}
	e.configID = configID

	// Surface at the exact coded size, allocated exactly the way ffmpeg's
	// hwframes pool allocates its encode input surfaces: a VA-managed
	// (GPU-tiled) NV12 surface, tagged with both VASurfaceAttribMemoryType
	// (== VA) and VASurfaceAttribPixelFormat (== NV12). The iHD HEVC
	// low-power encoder reads from the surface's native tiled layout and
	// mishandles the default allocation it picks when the pixel format is
	// not pinned; H.264 tolerates it but HEVC stalls the engine and returns
	// VA_STATUS_ERROR_ENCODING_ERROR. (Until this was corrected the
	// PixelFormat attribute was mis-tagged as MinWidth and silently ignored,
	// which was the sole cause of the HEVC-LP encode failure.) The encoder is
	// all-intra so one input/reconstruct surface suffices.
	surfAttrs := []vaSurfaceAttrib{
		{
			Type:  vaSurfaceAttribMemoryType,
			Flags: vaSurfaceAttribFlagSettable,
			Value: vaGenericValue{Type: vaGenericValueTypeInteger, IntVal: int64(vaSurfaceAttribMemTypeVA)},
		},
		{
			Type:  vaSurfaceAttribPixelFormat,
			Flags: vaSurfaceAttribFlagSettable,
			Value: vaGenericValue{Type: vaGenericValueTypeInteger, IntVal: int64(vaFourCCNV12)},
		},
	}
	var surf uint32
	st = l.vaCreateSurfaces(dpy, vaRTFormatYUV420, uint32(e.width), uint32(e.height),
		&surf, 1, unsafe.Pointer(&surfAttrs[0]), uint32(len(surfAttrs)))
	if st != vaStatusSuccess {
		return fmt.Errorf("%w: vaCreateSurfaces(enc) VAStatus=%d", ErrBackendFailure, st)
	}
	e.surface = surf

	// Context with no render-target hints (NULL/0), matching the iHD
	// encoder's own usage; the input surface is bound per-picture instead.
	var ctx uint32
	st = l.vaCreateContext(dpy, configID, int32(e.width), int32(e.height), vaProgressive,
		nil, 0, &ctx)
	if st != vaStatusSuccess {
		return fmt.Errorf("%w: vaCreateContext(enc) VAStatus=%d", ErrBackendFailure, st)
	}
	e.context = ctx

	// Coded output buffer sized generously for an intra frame (the
	// libva-utils sizing heuristic: w*h*400/256).
	codedSize := uint32(alignUp(e.width, 16) * alignUp(e.height, 16) * 400 / 256)
	var cb uint32
	st = l.vaCreateBuffer(dpy, ctx, vaEncCodedBufferType, codedSize, 1, nil, &cb)
	if st != vaStatusSuccess {
		return fmt.Errorf("%w: vaCreateBuffer(coded) VAStatus=%d", ErrBackendFailure, st)
	}
	e.codedBuf = cb

	e.authorParameterSets()
	e.started = true
	return nil
}

// Encode uploads one NV12 frame, encodes it as an IDR, and returns the
// Annex-B packet. I420 input is converted to NV12 on upload.
func (e *vaEncoder) Encode(f video.Frame) ([]video.Packet, error) {
	if e.closed {
		return nil, ErrClosed
	}
	if f.PixelFormat != video.NV12 && f.PixelFormat != video.I420 {
		return nil, ErrUnsupportedPixelFormat
	}
	if err := e.ensurePipeline(); err != nil {
		return nil, err
	}
	if err := e.uploadFrame(f); err != nil {
		return nil, err
	}

	pkt, err := e.encodeIDR()
	if err != nil {
		return nil, err
	}
	pkt.PTS = e.framePTS()
	pkt.DTS = pkt.PTS
	e.frameIdx++
	return []video.Packet{pkt}, nil
}

// Flush is a no-op: the encoder emits each frame synchronously in Encode.
func (e *vaEncoder) Flush() ([]video.Packet, error) {
	if e.closed {
		return nil, ErrClosed
	}
	return nil, nil
}

// Close tears down the VA pipeline. Idempotent.
func (e *vaEncoder) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	l := e.b.lib
	dpy := e.b.display
	if e.codedBuf != vaInvalidID {
		l.vaDestroyBuffer(dpy, e.codedBuf)
		e.codedBuf = vaInvalidID
	}
	if e.context != vaInvalidID {
		l.vaDestroyContext(dpy, e.context)
		e.context = vaInvalidID
	}
	if e.surface != vaInvalidSurface {
		s := e.surface
		l.vaDestroySurfaces(dpy, &s, 1)
		e.surface = vaInvalidSurface
	}
	if e.configID != vaInvalidID {
		l.vaDestroyConfig(dpy, e.configID)
		e.configID = vaInvalidID
	}
	return nil
}

// framePTS returns the presentation timestamp for the current frame.
func (e *vaEncoder) framePTS() time.Duration {
	return time.Duration(float64(e.frameIdx) / e.cfg.frameRate() * float64(time.Second))
}

// uploadFrame writes the frame's planes directly into the input surface's
// native memory via vaDeriveImage (the path the iHD encoder itself uses):
// derive an image backed by the surface, map it, copy the luma and
// (interleaved) chroma honouring the derived pitches/offsets, unmap, and
// destroy the derived image. This writes into the surface's exact tiled
// layout, which the HEVC low-power encoder requires (a separately-created
// linear image + vaPutImage is not accepted on this path).
func (e *vaEncoder) uploadFrame(f video.Frame) error {
	l := e.b.lib
	dpy := e.b.display

	var img vaImage
	if st := l.vaDeriveImage(dpy, e.surface, unsafe.Pointer(&img)); st != vaStatusSuccess {
		return fmt.Errorf("%w: vaDeriveImage(enc) VAStatus=%d", ErrBackendFailure, st)
	}
	defer l.vaDestroyImage(dpy, img.ImageID)

	var base unsafe.Pointer
	if st := l.vaMapBuffer(dpy, img.Buf, &base); st != vaStatusSuccess {
		return fmt.Errorf("%w: vaMapBuffer(derived) VAStatus=%d", ErrBackendFailure, st)
	}
	dst := unsafe.Slice((*byte)(base), img.DataSize)

	yPitch := int(img.Pitches[0])
	cPitch := int(img.Pitches[1])
	yOff := int(img.Offsets[0])
	cOff := int(img.Offsets[1])

	// Luma: copy row by row honouring source and destination strides.
	srcYStride := f.Strides[0]
	for row := 0; row < e.height; row++ {
		copy(dst[yOff+row*yPitch:yOff+row*yPitch+e.width],
			f.Planes[0][row*srcYStride:row*srcYStride+e.width])
	}

	cw := e.width / 2
	ch := e.height / 2
	if f.PixelFormat == video.NV12 {
		srcCStride := f.Strides[1]
		for row := 0; row < ch; row++ {
			copy(dst[cOff+row*cPitch:cOff+row*cPitch+e.width],
				f.Planes[1][row*srcCStride:row*srcCStride+e.width])
		}
	} else { // I420 -> NV12 interleave
		uStride := f.Strides[1]
		vStride := f.Strides[2]
		for row := 0; row < ch; row++ {
			d := dst[cOff+row*cPitch:]
			u := f.Planes[1][row*uStride:]
			v := f.Planes[2][row*vStride:]
			for col := 0; col < cw; col++ {
				d[2*col] = u[col]
				d[2*col+1] = v[col]
			}
		}
	}

	if st := l.vaUnmapBuffer(dpy, img.Buf); st != vaStatusSuccess {
		return fmt.Errorf("%w: vaUnmapBuffer(derived) VAStatus=%d", ErrBackendFailure, st)
	}
	return nil
}

// encodeIDR submits the per-frame parameter buffers and packed headers,
// runs the encode, and reads back the coded buffer as an Annex-B packet.
func (e *vaEncoder) encodeIDR() (video.Packet, error) {
	l := e.b.lib
	dpy := e.b.display

	var bufs []uint32
	free := func() {
		for _, id := range bufs {
			l.vaDestroyBuffer(dpy, id)
		}
	}
	defer free()

	add := func(typ uint32, size int, data unsafe.Pointer) (uint32, error) {
		var id uint32
		if st := l.vaCreateBuffer(dpy, e.context, typ, uint32(size), 1, data, &id); st != vaStatusSuccess {
			return 0, fmt.Errorf("%w: vaCreateBuffer(type=%d) VAStatus=%d", ErrBackendFailure, typ, st)
		}
		bufs = append(bufs, id)
		return id, nil
	}

	// Sequence + picture + slice + rate-control parameter buffers.
	switch e.codec {
	case video.H265:
		if err := e.buildHEVCParams(add); err != nil {
			return video.Packet{}, err
		}
	case video.VP9:
		if err := e.buildVP9Params(add); err != nil {
			return video.Packet{}, err
		}
	case video.AV1:
		if err := e.buildAV1Params(add); err != nil {
			return video.Packet{}, err
		}
	default:
		if err := e.buildH264Params(add); err != nil {
			return video.Packet{}, err
		}
	}

	if st := l.vaBeginPicture(dpy, e.context, e.surface); st != vaStatusSuccess {
		return video.Packet{}, fmt.Errorf("%w: vaBeginPicture(enc) VAStatus=%d", ErrBackendFailure, st)
	}
	if st := l.vaRenderPicture(dpy, e.context, &bufs[0], int32(len(bufs))); st != vaStatusSuccess {
		return video.Packet{}, fmt.Errorf("%w: vaRenderPicture(enc) VAStatus=%d", ErrBackendFailure, st)
	}
	if st := l.vaEndPicture(dpy, e.context); st != vaStatusSuccess {
		return video.Packet{}, fmt.Errorf("%w: vaEndPicture(enc) VAStatus=%d", ErrBackendFailure, st)
	}
	if st := l.vaSyncSurface(dpy, e.surface); st != vaStatusSuccess {
		return video.Packet{}, fmt.Errorf("%w: vaSyncSurface(enc) VAStatus=%d", ErrBackendFailure, st)
	}

	return e.readCodedBuffer()
}

// readCodedBuffer maps the coded buffer, concatenates the
// VACodedBufferSegment chain, and converts it to Annex-B. The low-power
// encoder with packed sequence/picture headers already embeds the
// SPS/PPS(/VPS) into the IDR coded buffer, so the bytes are emitted as-is
// (start codes present); the result is keyframe=true.
func (e *vaEncoder) readCodedBuffer() (video.Packet, error) {
	l := e.b.lib
	dpy := e.b.display

	var base unsafe.Pointer
	if st := l.vaMapBuffer(dpy, e.codedBuf, &base); st != vaStatusSuccess {
		return video.Packet{}, fmt.Errorf("%w: vaMapBuffer(coded) VAStatus=%d", ErrBackendFailure, st)
	}
	defer l.vaUnmapBuffer(dpy, e.codedBuf)

	var out []byte
	seg := (*vaCodedBufferSegment)(base)
	for seg != nil {
		if seg.Size > 0 && seg.Buf != nil {
			out = append(out, unsafe.Slice((*byte)(seg.Buf), seg.Size)...)
		}
		if seg.Next == nil {
			break
		}
		seg = (*vaCodedBufferSegment)(seg.Next)
	}
	if len(out) == 0 {
		return video.Packet{}, fmt.Errorf("%w: empty coded buffer", ErrBackendFailure)
	}
	data := make([]byte, len(out))
	copy(data, out)

	return video.Packet{
		Codec:    e.codec,
		Data:     data,
		Keyframe: true,
	}, nil
}
