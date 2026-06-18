//go:build linux

// VA-API VLD decode for H.264 and H.265. The decoder consumes the
// elementary-stream form the encoder produces — Annex-B NAL units with the
// parameter sets carried inline on each keyframe — parses enough of the
// SPS/PPS (and VPS for H.265) and the slice header to fill the VA
// parameter buffers, submits one picture per access unit, and reads the
// decoded NV12 surface back into a video.Frame via vaDeriveImage.
//
// # Per-access-unit flow
//
//	split Annex-B into NALs; classify parameter sets + the slice
//	parse SPS/PPS(/VPS) -> (re)build config/context/surfaces if changed
//	build VAPictureParameterBuffer + VAIQMatrix + VASliceParameterBuffer
//	  + the slice-data buffer (the raw NAL bytes)
//	vaBeginPicture(surface) -> vaRenderPicture(bufs) -> vaEndPicture
//	vaSyncSurface(surface)
//	vaDeriveImage + copy NV12 planes -> video.Frame
//
// The decoder is single-reference / single-slice per picture (the form the
// all-intra encoder and typical NVR keyframe streams produce). Not safe for
// concurrent use.

package hwaccel

import (
	"fmt"
	"time"
	"unsafe"

	"go-mediatoolkit/video"
)

// vaDecoder drives VA-API VLD decode for one stream. The config, context
// and surface pool are (re)built lazily from the parsed parameter sets.
// Not safe for concurrent use.
type vaDecoder struct {
	b     *vaBackend
	cfg   Config
	codec video.Codec

	configID uint32
	context  uint32
	surface  uint32
	width    int
	height   int

	// Parsed, cached parameter sets backing the live context.
	h264 h264Params
	hevc hevcParams
	have bool

	// av1Seq caches the last parsed AV1 sequence header so frame OBUs that omit
	// it (mid-stream) can still be decoded.
	av1Seq *av1SeqHeader

	frameIdx int64
	closed   bool
}

// newVADecoder returns a decoder; the VA pipeline is built on the first
// keyframe once the parameter sets are known.
func newVADecoder(b *vaBackend, cfg Config) Decoder {
	return &vaDecoder{
		b:        b,
		cfg:      cfg,
		codec:    cfg.Codec,
		configID: vaInvalidID,
		context:  vaInvalidID,
		surface:  vaInvalidSurface,
	}
}

// Decode submits one Annex-B access unit and returns the decoded frame(s).
func (d *vaDecoder) Decode(p video.Packet) ([]video.Frame, error) {
	if d.closed {
		return nil, ErrClosed
	}
	if len(p.Data) == 0 {
		return nil, nil
	}

	// VP9 and AV1 are not Annex-B: the packet is a coded VP9 frame/superframe
	// or an AV1 temporal unit (OBU stream), handed to the parser whole.
	switch d.codec {
	case video.VP9:
		return d.decodeVP9(p.Data)
	case video.AV1:
		return d.decodeAV1(p.Data)
	}

	nals := splitAnnexBNALs(p.Data)
	if len(nals) == 0 {
		return nil, nil
	}
	if d.codec == video.H265 {
		return d.decodeHEVC(nals)
	}
	return d.decodeH264(nals)
}

// Flush is a no-op: each access unit is decoded synchronously in Decode.
func (d *vaDecoder) Flush() ([]video.Frame, error) {
	if d.closed {
		return nil, ErrClosed
	}
	return nil, nil
}

// Close tears down the VA pipeline. Idempotent.
func (d *vaDecoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	d.teardown()
	return nil
}

// teardown destroys the context, surface and config.
func (d *vaDecoder) teardown() {
	l := d.b.lib
	dpy := d.b.display
	if d.context != vaInvalidID {
		l.vaDestroyContext(dpy, d.context)
		d.context = vaInvalidID
	}
	if d.surface != vaInvalidSurface {
		s := d.surface
		l.vaDestroySurfaces(dpy, &s, 1)
		d.surface = vaInvalidSurface
	}
	if d.configID != vaInvalidID {
		l.vaDestroyConfig(dpy, d.configID)
		d.configID = vaInvalidID
	}
}

// ensureContext (re)creates the config/context/surface for the given coded
// dimensions and profile if they are not already current.
func (d *vaDecoder) ensureContext(profile int32, w, h int) error {
	if d.context != vaInvalidID && d.width == w && d.height == h {
		return nil
	}
	d.teardown()

	l := d.b.lib
	dpy := d.b.display

	attrs := []vaConfigAttrib{{Type: vaConfigAttribRTFormat, Value: vaRTFormatYUV420}}
	var configID uint32
	if st := l.vaCreateConfig(dpy, profile, vaEntrypointVLD,
		unsafe.Pointer(&attrs[0]), int32(len(attrs)), &configID); st != vaStatusSuccess {
		return fmt.Errorf("%w: vaCreateConfig(dec) VAStatus=%d", ErrBackendFailure, st)
	}
	d.configID = configID

	var surf uint32
	if st := l.vaCreateSurfaces(dpy, vaRTFormatYUV420, uint32(w), uint32(h),
		&surf, 1, nil, 0); st != vaStatusSuccess {
		return fmt.Errorf("%w: vaCreateSurfaces(dec) VAStatus=%d", ErrBackendFailure, st)
	}
	d.surface = surf

	var ctx uint32
	if st := l.vaCreateContext(dpy, configID, int32(w), int32(h), vaProgressive,
		&surf, 1, &ctx); st != vaStatusSuccess {
		return fmt.Errorf("%w: vaCreateContext(dec) VAStatus=%d", ErrBackendFailure, st)
	}
	d.context = ctx
	d.width = w
	d.height = h
	return nil
}

// submitPicture runs vaBeginPicture/RenderPicture/EndPicture on the given
// buffers and then vaSyncSurface, decoding into d.surface.
func (d *vaDecoder) submitPicture(bufs []uint32) error {
	l := d.b.lib
	dpy := d.b.display
	if st := l.vaBeginPicture(dpy, d.context, d.surface); st != vaStatusSuccess {
		return fmt.Errorf("%w: vaBeginPicture(dec) VAStatus=%d", ErrBackendFailure, st)
	}
	if st := l.vaRenderPicture(dpy, d.context, &bufs[0], int32(len(bufs))); st != vaStatusSuccess {
		return fmt.Errorf("%w: vaRenderPicture(dec) VAStatus=%d", ErrBackendFailure, st)
	}
	if st := l.vaEndPicture(dpy, d.context); st != vaStatusSuccess {
		return fmt.Errorf("%w: vaEndPicture(dec) VAStatus=%d", ErrBackendFailure, st)
	}
	if st := l.vaSyncSurface(dpy, d.surface); st != vaStatusSuccess {
		return fmt.Errorf("%w: vaSyncSurface(dec) VAStatus=%d", ErrBackendFailure, st)
	}
	return nil
}

// readSurface copies the decoded NV12 surface into a video.Frame via
// vaDeriveImage. The visible width/height are returned (not the aligned
// coded size).
func (d *vaDecoder) readSurface(visW, visH int) (video.Frame, error) {
	l := d.b.lib
	dpy := d.b.display

	var img vaImage
	if st := l.vaDeriveImage(dpy, d.surface, unsafe.Pointer(&img)); st != vaStatusSuccess {
		return video.Frame{}, fmt.Errorf("%w: vaDeriveImage(dec) VAStatus=%d", ErrBackendFailure, st)
	}
	defer l.vaDestroyImage(dpy, img.ImageID)

	if img.Format.FourCC != vaFourCCNV12 {
		return video.Frame{}, fmt.Errorf("%w: decoded image fourcc=0x%x not NV12", ErrBackendFailure, img.Format.FourCC)
	}

	var base unsafe.Pointer
	if st := l.vaMapBuffer(dpy, img.Buf, &base); st != vaStatusSuccess {
		return video.Frame{}, fmt.Errorf("%w: vaMapBuffer(derived dec) VAStatus=%d", ErrBackendFailure, st)
	}
	defer l.vaUnmapBuffer(dpy, img.Buf)
	src := unsafe.Slice((*byte)(base), img.DataSize)

	yPitch := int(img.Pitches[0])
	cPitch := int(img.Pitches[1])
	yOff := int(img.Offsets[0])
	cOff := int(img.Offsets[1])

	// Copy Y plane (visH rows of visW) and the interleaved chroma plane
	// (visH/2 rows of visW), each into a tightly-packed Go slice.
	y := make([]byte, visW*visH)
	for row := 0; row < visH; row++ {
		copy(y[row*visW:row*visW+visW], src[yOff+row*yPitch:yOff+row*yPitch+visW])
	}
	ch := visH / 2
	c := make([]byte, visW*ch)
	for row := 0; row < ch; row++ {
		copy(c[row*visW:row*visW+visW], src[cOff+row*cPitch:cOff+row*cPitch+visW])
	}

	return video.Frame{
		PixelFormat: video.NV12,
		Width:       visW,
		Height:      visH,
		Planes:      [][]byte{y, c},
		Strides:     []int{visW, visW},
		PTS:         time.Duration(float64(d.frameIdx) / d.cfg.frameRate() * float64(time.Second)),
	}, nil
}

// addDecodeBuf creates a VA buffer and tracks it for teardown after the
// picture is submitted.
func (d *vaDecoder) addDecodeBuf(bufs *[]uint32, typ uint32, size int, data unsafe.Pointer) error {
	var id uint32
	if st := d.b.lib.vaCreateBuffer(d.b.display, d.context, typ, uint32(size), 1, data, &id); st != vaStatusSuccess {
		return fmt.Errorf("%w: vaCreateBuffer(dec type=%d) VAStatus=%d", ErrBackendFailure, typ, st)
	}
	*bufs = append(*bufs, id)
	return nil
}

// freeDecodeBufs destroys the per-picture buffers.
func (d *vaDecoder) freeDecodeBufs(bufs []uint32) {
	for _, id := range bufs {
		d.b.lib.vaDestroyBuffer(d.b.display, id)
	}
}
