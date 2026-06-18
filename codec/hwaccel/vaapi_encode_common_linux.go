//go:build linux

// Shared VAAPI encoder helpers used by both the H.264 and H.265 paths:
// rate-control buffer construction, packed-header submission, QP / bitrate
// selection, and Annex-B framing of authored parameter sets.

package hwaccel

import (
	"fmt"
	"unsafe"

	"go-mediatoolkit/video"
)

// startCodeVA is the 4-byte Annex-B NAL start code.
var startCodeVA = []byte{0x00, 0x00, 0x00, 0x01}

// annexB prepends the 4-byte start code to a single NAL unit (NAL header +
// EBSP). The input is the raw NAL (header byte + RBSP, no emulation
// prevention); this inserts emulation-prevention bytes and the start code.
func annexB(nal []byte) []byte {
	ebsp := rbspToEBSP(nal)
	out := make([]byte, 0, len(ebsp)+4)
	out = append(out, startCodeVA...)
	out = append(out, ebsp...)
	return out
}

// bitrate returns the configured target bitrate, defaulting to a generous
// value scaled by resolution when unset (keeps the all-intra round trip
// near-lossless).
func (e *vaEncoder) bitrate() int {
	if e.cfg.Bitrate > 0 {
		return e.cfg.Bitrate
	}
	// ~2 bits per pixel per second at the configured frame rate, a generous
	// default for an all-intra near-lossless transcode.
	bpp := float64(e.width*e.height) * e.cfg.frameRate() * 2.0
	return int(bpp)
}

// pickQP picks the slice QP. A low QP keeps the all-intra round trip
// near-lossless so the test's MAE bound holds; the CBR rate controller may
// still adjust, but pic_init_qp anchors it.
func (e *vaEncoder) pickQP() int { return 22 }

// rateControlMode returns the VAConfigAttribRateControl value for the codec.
// H.264/H.265 use constant-QP (the quantiser is carried in pic_init_qp);
// VP9/AV1 on the iHD low-power encoder drive CBR with a target bitrate (their
// picture buffers carry the base qindex directly but the entrypoint still
// negotiates a rate-control mode).
func (e *vaEncoder) rateControlMode() uint32 {
	switch e.codec {
	case video.VP9, video.AV1:
		return vaRCCBR
	default:
		return vaRCCQP
	}
}

// rateControlBuffer builds a fused VAEncMiscParameterBuffer(type=RateControl)
// + VAEncMiscParameterRateControl for CBR at the configured bitrate.
func (e *vaEncoder) rateControlBuffer() vaEncMiscParameterRateControl {
	return vaEncMiscParameterRateControl{
		TypeTag:          vaEncMiscParameterTypeRateControl,
		BitsPerSecond:    uint32(e.bitrate()),
		TargetPercentage: 100, // CBR
		WindowSize:       1000,
		InitialQP:        uint32(e.pickQP()),
		MinQP:            1,
		MaxQP:            51,
	}
}

// addPackedHeader submits a packed header pair: a
// VAEncPackedHeaderParameterBuffer describing the header, then a
// VAEncPackedHeaderDataBuffer with the Annex-B bytes. headerType is one of
// vaEncPackedHeaderType{Sequence,Picture,...}. The Annex-B bytes already
// carry emulation-prevention, so has_emulation_bytes is set.
func (e *vaEncoder) addPackedHeader(add addFunc, headerType uint32, annexBData []byte) error {
	if len(annexBData) == 0 {
		return nil
	}
	param := vaEncPackedHeaderParameterBuffer{
		Type:              headerType,
		BitLength:         uint32(len(annexBData) * 8),
		HasEmulationBytes: 1,
	}
	if _, err := add(vaEncPackedHeaderParameterBufferType, int(unsafe.Sizeof(param)), unsafe.Pointer(&param)); err != nil {
		return err
	}
	data := make([]byte, len(annexBData))
	copy(data, annexBData)
	if _, err := add(vaEncPackedHeaderDataBufferType, len(data), unsafe.Pointer(&data[0])); err != nil {
		return err
	}
	return nil
}

// packedHeaderAttrib returns the VAConfigAttribEncPackedHeaders value the
// codec's low-power encode path supplies its own headers for.
//
// H.264 LP: the driver generates the slice header itself, so the application
// only packs SPS+PIC (Sequence | Picture).
//
// H.265 LP on iHD: the low-power HEVC encoder does NOT emit the slice
// segment header — the application must pack it (Sequence | Slice), with
// VPS+SPS+PPS concatenated into the single Sequence packed header. This is
// exactly how ffmpeg's hevc_vaapi -low_power path drives the encoder;
// supplying Sequence|Picture (and no slice header) is what stalled the GPU
// and returned VA_STATUS_ERROR_ENCODING_ERROR.
func (e *vaEncoder) packedHeaderAttrib() uint32 {
	switch e.codec {
	case video.VP9:
		// The VP9 low-power encoder writes the entire frame including the
		// uncompressed header; the application packs nothing.
		return vaEncPackedHeaderNone
	case video.AV1:
		// The AV1 encoder requires the application to author the sequence and
		// frame-header OBUs and supply them as packed headers.
		return vaEncPackedHeaderSequence | vaEncPackedHeaderPicture
	case video.H265:
		return vaEncPackedHeaderSequence | vaEncPackedHeaderSlice
	default:
		return vaEncPackedHeaderSequence | vaEncPackedHeaderPicture
	}
}

// errVA wraps a non-zero VAStatus as ErrBackendFailure (test/helper seam).
func errVA(st int32) error { return fmt.Errorf("%w: VAStatus=%d", ErrBackendFailure, st) }

// frameRateBuffer builds a fused VAEncMiscParameterBuffer(type=FrameRate) +
// VAEncMiscParameterFrameRate. The framerate word packs denominator in the
// high 16 bits and numerator in the low 16 (per va.h); the iHD CBR/CQP
// encoder expects this buffer alongside rate control.
func (e *vaEncoder) frameRateBuffer() vaEncMiscParameterFrameRate {
	num := e.cfg.FrameRateNum
	den := e.cfg.FrameRateDen
	if num <= 0 {
		num, den = 30, 1
	}
	if den <= 0 {
		den = 1
	}
	return vaEncMiscParameterFrameRate{
		TypeTag:   vaEncMiscParameterTypeFrameRate,
		Framerate: uint32(den)<<16 | uint32(num&0xffff),
	}
}

// addRateAndFrameRate submits the frame-rate misc buffer. Constant-QP mode
// carries its quantiser in pic_init_qp and needs no rate-control buffer, so
// only the frame rate is supplied here.
func (e *vaEncoder) addRateAndFrameRate(add addFunc) error {
	fr := e.frameRateBuffer()
	_, err := add(vaEncMiscParameterBufferType, int(unsafe.Sizeof(fr)), unsafe.Pointer(&fr))
	return err
}
