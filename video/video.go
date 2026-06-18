// Package video defines the minimal shared types used by the
// hardware-codec-offload framework (codec/hwaccel) and its backends.
//
// It intentionally carries no codec logic — only the data the
// encode/decode seam exchanges: a raw, uncompressed [Frame] (a planar
// YUV image plus timing) and an encoded [Packet] (a compressed
// access unit plus timing and keyframe metadata). Backends in
// codec/hwaccel translate these to and from the native surface types
// of the underlying accelerator (CVPixelBuffer / CMSampleBuffer on
// Apple, V4L2 buffers on Linux SoCs, VASurface, NVENC input buffers).
//
// # Data layout
//
// A Frame holds planar pixel data in [Frame.Planes], one []byte per
// plane, with the matching row stride in [Frame.Strides]. The plane
// count and meaning are fixed by [Frame.PixelFormat]:
//
//   - NV12  — 2 planes: [0] = Y (w×h), [1] = interleaved Cb/Cr (w×h/2,
//     one (Cb,Cr) byte pair per 2×2 luma block).
//   - I420  — 3 planes: [0] = Y (w×h), [1] = Cb (w/2×h/2),
//     [2] = Cr (w/2×h/2).
//
// Strides may exceed the visible width when a backend requires aligned
// rows; consumers must honour the stride, not assume stride == width.
//
// # Concurrency
//
// Frame and Packet are plain value/slice carriers and are not
// synchronised. Ownership of the backing slices transfers to the
// callee on a Write/Encode call and back to the caller on a
// Read/Decode return; do not mutate a Frame's planes after handing it
// to an encoder until the call returns.
package video

import "time"

// Codec identifies a compressed video bitstream format. It is the unit
// of capability negotiation: backends advertise which Codecs they can
// encode and/or decode, and a [Packet] is tagged with the Codec its
// bytes belong to.
type Codec uint8

const (
	// CodecUnknown is the zero value and never names a real format.
	CodecUnknown Codec = iota
	// H264 is ITU-T H.264 / MPEG-4 AVC.
	H264
	// H265 is ITU-T H.265 / MPEG-H HEVC.
	H265
	// VP9 is the Google/Alliance-for-Open-Media VP9 bitstream.
	VP9
	// AV1 is the Alliance-for-Open-Media AV1 bitstream.
	AV1
)

// String returns the lowercase canonical token for the codec
// ("h264", "h265", "vp9", "av1", or "unknown"), matching the tokens used
// in configuration and capability reporting.
func (c Codec) String() string {
	switch c {
	case H264:
		return "h264"
	case H265:
		return "h265"
	case VP9:
		return "vp9"
	case AV1:
		return "av1"
	default:
		return "unknown"
	}
}

// PixelFormat names the memory layout of a raw [Frame]'s planes.
type PixelFormat uint8

const (
	// PixelFormatUnknown is the zero value and names no layout.
	PixelFormatUnknown PixelFormat = iota
	// NV12 is 8-bit 4:2:0 with a luma plane and an interleaved
	// chroma plane (2 planes).
	NV12
	// I420 is 8-bit 4:2:0 planar with separate Cb and Cr planes
	// (3 planes). Also known as YUV420P.
	I420
)

// String returns the lowercase token for the pixel format.
func (p PixelFormat) String() string {
	switch p {
	case NV12:
		return "nv12"
	case I420:
		return "i420"
	default:
		return "unknown"
	}
}

// Planes returns the number of planes a Frame in this format carries:
// 2 for NV12, 3 for I420, 0 for unknown.
func (p PixelFormat) Planes() int {
	switch p {
	case NV12:
		return 2
	case I420:
		return 3
	default:
		return 0
	}
}

// Frame is a single raw, uncompressed video frame: planar YUV pixel
// data plus geometry and a presentation timestamp.
//
// Width and Height are the visible (coded) dimensions in luma pixels.
// Planes and Strides are parallel slices of length
// PixelFormat.Planes(); Planes[i] is plane i's bytes and Strides[i]
// its row stride in bytes (>= the plane's visible row width).
type Frame struct {
	PixelFormat PixelFormat
	Width       int
	Height      int
	Planes      [][]byte
	Strides     []int
	// PTS is the presentation timestamp of the frame relative to the
	// stream start.
	PTS time.Duration
}

// Packet is a single encoded access unit: the compressed bytes for one
// frame (or, for parameter-set-carrying keyframes, the parameter sets
// followed by the frame) plus timing and keyframe metadata.
//
// Data holds the bitstream in the backend's native packaging, which
// depends on the Codec:
//
//   - H264 / H265 — Annex-B: start-code-prefixed NAL units. The
//     VideoToolbox encoder converts its length-prefixed (AVCC/HVCC)
//     output to Annex-B, and keyframe packets are prefixed with the
//     parameter sets (SPS/PPS for H.264; VPS/SPS/PPS for H.265) so the
//     stream is decodable from any keyframe.
//   - VP9 — one coded VP9 frame (or a superframe: a superframe-indexed
//     concatenation of frames). VP9 is NOT Annex-B and carries no start
//     codes; Data is the raw uncompressed-header + compressed-header +
//     tile bytes for the access unit, exactly as carried in an IVF frame
//     payload or a WebM block. There are no separate parameter sets —
//     every keyframe's uncompressed header is self-describing.
//   - AV1 — one Temporal Unit: a concatenation of length-delimited OBUs
//     (each with obu_has_size_field set) covering one displayable frame,
//     i.e. the sequence-header OBU (on keyframes) + frame/tile-group
//     OBUs. AV1 is NOT Annex-B; Data is the raw OBU stream as carried in
//     an IVF frame payload or a WebM block. Keyframe packets carry the
//     sequence-header OBU so the stream is decodable from any keyframe.
type Packet struct {
	Codec Codec
	Data  []byte
	// Keyframe reports whether this access unit is an IDR / sync
	// sample that can be decoded without reference to earlier frames.
	Keyframe bool
	// PTS is the presentation timestamp; DTS the decode timestamp.
	// For codecs without B-frames the two are equal.
	PTS time.Duration
	DTS time.Duration
}
