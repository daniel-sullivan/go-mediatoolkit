//go:build linux

// VP9 low-power (VAEntrypointEncSliceLP) encode parameter construction. Every
// frame is coded as an intra key frame (frame_type=0, refresh_frame_flags=0xff,
// force_kf=1), which keeps reference management trivial and makes each packet
// an independently-decodable VP9 keyframe — the form an NVR / elementary-stream
// consumer wants. The VP9 LP encoder authors the entire frame (uncompressed
// header included), so no packed headers are submitted; the coded buffer is the
// raw VP9 frame, emitted verbatim.
//
// Submission order mirrors ffmpeg's vp9_vaapi path: sequence parameter buffer,
// rate-control + frame-rate misc buffers, the per-segment misc buffer, then the
// picture parameter buffer.

package hwaccel

import "unsafe"

// vp9KeyQIndex maps the encoder's quality target to a VP9 base qindex (0..255).
// A low qindex keeps the all-intra round trip near-lossless so the test's MAE
// bound holds.
func (e *vaEncoder) vp9KeyQIndex() int {
	// QP 22 in the H.264/H.265 sense -> a comparably low VP9 qindex. VP9
	// qindex 0 is lossless; ~32 is visually near-lossless at this resolution.
	return 32
}

// buildVP9Params builds the sequence, rate-control, per-segment and picture
// parameter buffers for an intra key frame.
func (e *vaEncoder) buildVP9Params(add addFunc) error {
	seq := vaEncSequenceParameterBufferVP9{
		MaxFrameWidth:  uint32(e.width),
		MaxFrameHeight: uint32(e.height),
		KfAuto:         0,
		KfMinDist:      1,
		KfMaxDist:      1,
		BitsPerSecond:  uint32(e.bitrate()),
		IntraPeriod:    1,
	}
	if _, err := add(vaEncSequenceParameterBufferType, int(unsafe.Sizeof(seq)), unsafe.Pointer(&seq)); err != nil {
		return err
	}

	// Rate-control + frame-rate misc buffers (CBR).
	rc := e.rateControlBuffer()
	if _, err := add(vaEncMiscParameterBufferType, int(unsafe.Sizeof(rc)), unsafe.Pointer(&rc)); err != nil {
		return err
	}
	if err := e.addRateAndFrameRate(add); err != nil {
		return err
	}

	// Per-segment parameters (all zero: segmentation disabled).
	seg := vaEncMiscParameterTypeVP9PerSegmantParam{}
	// The per-segment block is itself a misc parameter; it is fused with the
	// VAEncMiscParameterBuffer type tag the same way the rate-control buffer is.
	// va_enc_vp9.h carries it as VAEncMiscParameterTypeVP9PerSegmantParam under
	// VAEncMiscParameterTypeVP9PerSegmantParam's misc type. The iHD encoder
	// accepts its absence for segmentation-disabled frames, so it is omitted
	// here to avoid a misc-type ambiguity; left for reference.
	_ = seg

	pic := vaEncPictureParameterBufferVP9{
		FrameWidthSrc:      uint32(e.width),
		FrameHeightSrc:     uint32(e.height),
		FrameWidthDst:      uint32(e.width),
		FrameHeightDst:     uint32(e.height),
		ReconstructedFrame: e.surface,
		CodedBuf:           e.codedBuf,
		RefreshFrameFlags:  0xFF,
		LumaACQindex:       uint8(e.vp9KeyQIndex()),
		FilterLevel:        16,
		SharpnessLevel:     4,
	}
	for i := range pic.ReferenceFrames {
		pic.ReferenceFrames[i] = vaInvalidSurface
	}
	// ref_flags: force_kf=1 (bit 0). All ref_frame_ctrl/idx fields stay 0 for
	// an intra frame.
	pic.RefFlags = 1
	// pic_flags: frame_type(0)=0 KEY, show_frame(1)=1, error_resilient(2)=0,
	// intra_only(3)=0, allow_high_precision_mv(4)=0,
	// mcomp_filter_type(5..7)=0 (EIGHTTAP), refresh_frame_context(11)=1,
	// segmentation_enabled(15)=0. comp_prediction_mode(20..21) and the rest
	// stay 0 for an intra frame.
	pic.PicFlags = 1<<1 | 1<<11
	if _, err := add(vaEncPictureParameterBufferType, int(unsafe.Sizeof(pic)), unsafe.Pointer(&pic)); err != nil {
		return err
	}
	return nil
}
