package nativeopus

import "math"

// Port of libopus/celt/float_cast.h.
//
// The C header selects one of several platform-specific float→int
// conversion paths (SSE cvtss2si, aarch64 vcvtns_s32_f32, lrintf,
// etc.). Every modern path rounds to nearest with ties-to-even — the
// only outlier is the archaic `(int)(floor(.5+flt))` fallback (round
// half away from zero), which triggers a compile-time warning in C
// and is never selected on any target we care about.
//
// math.RoundToEven provides the same semantics in Go, so this file
// is a straight transcription of the float2int / FLOAT2INT16 /
// FLOAT2INT24 helpers. The FLOAT2SIG variant gated on FIXED_POINT in
// the C header is already handled by arch.go for the float build.
//
// DISABLE_FLOAT_API is undefined in the libopus build we target, so
// FLOAT2INT16 and FLOAT2INT24 are both exposed.

// float2int rounds x to the nearest opus_int32, ties-to-even.
// C: lrintf(x) on the modern paths; vcvtns_s32_f32(x) on aarch64.
func float2int(x float32) opus_int32 {
	return opus_int32(math.RoundToEven(float64(x)))
}

// FLOAT2INT16 scales x by CELT_SIG_SCALE, saturates to the int16
// range, and returns the nearest int16. C: unchanged logic.
func FLOAT2INT16(x float32) opus_int16 {
	x = x * float32(CELT_SIG_SCALE)
	x = MAX32(x, -32768)
	x = MIN32(x, 32767)
	return opus_int16(float2int(x))
}

// FLOAT2INT24 scales x by CELT_SIG_SCALE*256, saturates to the int24
// range (symmetric clamp [-2^24, 2^24]), and returns the nearest int32.
func FLOAT2INT24(x float32) opus_int32 {
	x = x * (float32(CELT_SIG_SCALE) * 256.0)
	x = MAX32(x, -16777216)
	x = MIN32(x, 16777216)
	return float2int(x)
}
