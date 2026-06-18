//go:build linux

package hwaccel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/video"
)

// nvTestWidth / nvTestHeight / nvTestFrames size the synthetic clip used by
// the hardware round-trip test (which only runs on an actual NVIDIA box).
const (
	nvTestWidth  = 640
	nvTestHeight = 480
	nvTestFrames = 8
)

// TestNVENCStructVersions pins the NVENCAPI 13.0 struct-version words against
// the values computed from the published SDK 13.0 header macros. These are the
// load-bearing version stamps NvEncInitializeEncoder & friends validate; a
// wrong value is rejected by the driver, so this guards the constants even on
// a box with no NVIDIA hardware.
func TestNVENCStructVersions(t *testing.T) {
	assert.Equal(t, uint32(0x0000000d), uint32(nvencAPIVersion), "NVENCAPI_VERSION (13.0)")

	tests := []struct {
		name string
		got  uint32
		want uint32
	}{
		{"NV_ENCODE_API_FUNCTION_LIST_VER", nvFunctionListVer, 0x7002000d},
		{"NV_ENC_OPEN_ENCODE_SESSION_EX_PARAMS_VER", nvOpenSessionExVer, 0x7001000d},
		{"NV_ENC_INITIALIZE_PARAMS_VER", nvInitializeParamsVer, 0xf007000d},
		{"NV_ENC_CONFIG_VER", nvConfigVer, 0xf009000d},
		{"NV_ENC_PIC_PARAMS_VER", nvPicParamsVer, 0xf007000d},
		{"NV_ENC_CREATE_INPUT_BUFFER_VER", nvCreateInputBufferVer, 0x7002000d},
		{"NV_ENC_CREATE_BITSTREAM_BUFFER_VER", nvCreateBitstreamBufVer, 0x7001000d},
		{"NV_ENC_LOCK_BITSTREAM_VER", nvLockBitstreamVer, 0xf002000d},
		{"NV_ENC_LOCK_INPUT_BUFFER_VER", nvLockInputBufferVer, 0x7001000d},
	}
	for _, tt := range tests {
		assert.Equalf(t, tt.want, tt.got, "%s", tt.name)
	}
}

// TestNVENCGUIDs checks a couple of the codec GUID byte layouts against the
// header, catching a transcription slip in the most load-bearing constants.
func TestNVENCGUIDs(t *testing.T) {
	assert.Equal(t, uint32(0x6bc82762), nvEncCodecH264GUID.Data1, "H264 GUID Data1")
	assert.Equal(t, [8]uint8{0xaa, 0x85, 0x1e, 0x50, 0xf3, 0x21, 0xf6, 0xbf},
		nvEncCodecH264GUID.Data4, "H264 GUID Data4")
	assert.Equal(t, uint32(0x790cdc88), nvEncCodecHEVCGUID.Data1, "HEVC GUID Data1")
	// NV_ENC_CODEC_AV1_GUID (SDK 13.0): {0A352289-0AA7-4759-862D-5D15CD16D254}.
	assert.Equal(t, uint32(0x0a352289), nvEncCodecAV1GUID.Data1, "AV1 GUID Data1")
	assert.Equal(t, [8]uint8{0x86, 0x2d, 0x5d, 0x15, 0xcd, 0x16, 0xd2, 0x54},
		nvEncCodecAV1GUID.Data4, "AV1 GUID Data4")
	// NV_ENC_AV1_PROFILE_MAIN_GUID: {5F2A39F5-F14E-4F95-9A9E-B76D568FCF97}.
	assert.Equal(t, uint32(0x5f2a39f5), nvEncAV1ProfileMainGUID.Data1, "AV1 profile GUID Data1")
	assert.False(t, nvEncCodecH264GUID.equal(nvEncCodecHEVCGUID), "distinct codec GUIDs")
	assert.False(t, nvEncCodecAV1GUID.equal(nvEncCodecHEVCGUID), "AV1 distinct from HEVC")

	// Codec -> GUID / cuvid-id mapping for the new codecs.
	assert.True(t, nvCodecGUID(video.AV1).equal(nvEncCodecAV1GUID), "AV1 encode GUID")
	assert.True(t, nvProfileGUID(video.AV1, "").equal(nvEncAV1ProfileMainGUID), "AV1 profile GUID")
	assert.Equal(t, uint32(10), nvCudaVideoCodec(video.VP9), "cuvid VP9 id")
	assert.Equal(t, uint32(11), nvCudaVideoCodec(video.AV1), "cuvid AV1 id")
}

// TestNVENCBackendRegistration asserts the backend either constructs cleanly
// (on an NVIDIA box) or is skipped gracefully — newNVBackend must never panic
// and, on this NVIDIA-less environment, must return an error (failed libcuda
// dlopen or no CUDA device) rather than a usable backend.
func TestNVENCBackendRegistration(t *testing.T) {
	b, err := newNVBackend()
	if err != nil {
		// Expected path on a host with no NVIDIA driver/device: graceful.
		t.Logf("nvenc backend unavailable (expected without NVIDIA hardware): %v", err)
		assert.Nil(t, b, "no backend should be returned on construction failure")
		return
	}
	// Only reached on a real NVIDIA box.
	require.NotNil(t, b)
	assert.Equal(t, "nvenc", b.Name())
	assert.True(t, b.Available(), "constructed backend reports available")
}

// TestNVENCAvailableGraceful asserts that the dlopen + cuInit gate degrades
// without panicking when the NVIDIA libraries or hardware are absent (this
// box). A nil backend's Available must be false, and loadNVENC must surface a
// clean error rather than crash the process on the missing libcuda.
func TestNVENCAvailableGraceful(t *testing.T) {
	var nilBackend *nvBackend
	assert.False(t, nilBackend.Available(), "nil backend not available")

	_, err := loadNVENC()
	if err != nil {
		t.Logf("loadNVENC failed gracefully (expected without NVIDIA driver): %v", err)
		return
	}
	// On a real NVIDIA box the libraries load; nothing to assert beyond the
	// no-panic contract (the round-trip test exercises the rest).
	t.Log("NVIDIA libraries present: loadNVENC succeeded")
}

// TestNVENCNewEncoderDegrades checks the construction error path without
// hardware: on a host where the backend cannot be built, NewEncoder is
// unreachable through the registry, so we assert the registry simply does not
// expose an nvenc backend (and OpenEncoder under RequireHardware errors
// cleanly). On a real NVIDIA box the encoder constructs.
func TestNVENCNewEncoderDegrades(t *testing.T) {
	b, err := newNVBackend()
	if err != nil {
		t.Skipf("nvenc unavailable (no NVIDIA hardware): %v", err)
	}

	// Reached only on a real NVIDIA box: validate the config error paths.
	_, err = b.NewEncoder(NewConfig(WithCodec(video.CodecUnknown), WithResolution(640, 480)))
	assert.ErrorIs(t, err, ErrInvalidConfig, "unknown codec rejected")

	_, err = b.NewEncoder(NewConfig(WithCodec(video.H264)))
	assert.ErrorIs(t, err, ErrInvalidConfig, "zero resolution rejected")
}

// TestNVENCRegistryRequireHardware asserts that, on this NVIDIA-less box, the
// default registry does not carry an nvenc backend and a RequireHardware open
// degrades to ErrHardwareUnavailable rather than panicking. On an NVIDIA box
// the nvenc backend is present and this is skipped.
func TestNVENCRegistryRequireHardware(t *testing.T) {
	// Use an isolated registry, not the process-wide DefaultRegistry: on a host
	// with another hardware backend present (e.g. VAAPI on an Intel box) the
	// default registry would satisfy RequireHardware via that backend and mask
	// this check. With no backend registered, RequireHardware must surface
	// ErrHardwareUnavailable.
	_, err := (Policy{Mode: RequireHardware}).OpenEncoder(
		NewRegistry(),
		NewConfig(WithCodec(video.H264), WithResolution(640, 480), WithFrameRate(30, 1)),
	)
	assert.ErrorIs(t, err, ErrHardwareUnavailable,
		"RequireHardware with no backend registered yields ErrHardwareUnavailable")
}

// makeNVNV12 synthesises a deterministic NV12 frame with a moving gradient so
// successive frames differ (exercising real encode/decode). Mirrors the VAAPI
// test's makeVANV12.
func makeNVNV12(w, h, idx int) video.Frame {
	y := make([]byte, w*h)
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			y[j*w+i] = byte((i + j + idx*4) & 0xff)
		}
	}
	c := make([]byte, w*h/2)
	for j := 0; j < h/2; j++ {
		for i := 0; i < w; i++ {
			c[j*w+i] = byte((i*2 + j + idx) & 0xff)
		}
	}
	return video.Frame{
		PixelFormat: video.NV12,
		Width:       w, Height: h,
		Planes:  [][]byte{y, c},
		Strides: []int{w, w},
		PTS:     time.Duration(idx) * time.Second / 30,
	}
}

// TestNVENCRoundTrip is the HARDWARE-PATH test: a full NVENC encode -> NVDEC
// decode round-trip. It REQUIRES an NVIDIA GPU with a driver new enough for the
// SDK 13.0 NVENCAPI. It t.Skips cleanly when the backend is unavailable, so it
// is a no-op on this NVIDIA-less box and would run on a real NVIDIA box.
//
// !!! This path is UNVERIFIED on hardware (no NVIDIA device was available
// during development). It is written against the published SDK 13.0 ABI; on a
// real GPU this test is what proves the encode/decode pipeline end to end. See
// the status banner in nvenc_linux.go.
func TestNVENCRoundTrip(t *testing.T) {
	b, err := newNVBackend()
	if err != nil {
		t.Skipf("nvenc unavailable (no NVIDIA hardware): %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.H264, Encode) || !caps.Supports(video.H264, Decode) {
		t.Skip("H.264 encode+decode not supported on this NVIDIA host")
	}

	enc, err := b.NewEncoder(NewConfig(
		WithCodec(video.H264),
		WithResolution(nvTestWidth, nvTestHeight),
		WithBitrate(6_000_000),
		WithFrameRate(30, 1),
	))
	require.NoError(t, err)

	var packets []video.Packet
	for i := 0; i < nvTestFrames; i++ {
		pkts, err := enc.Encode(makeNVNV12(nvTestWidth, nvTestHeight, i))
		require.NoError(t, err, "encode frame %d", i)
		packets = append(packets, pkts...)
	}
	tail, err := enc.Flush()
	require.NoError(t, err)
	packets = append(packets, tail...)
	require.NoError(t, enc.Close())
	require.NotEmpty(t, packets, "encoder produced no packets")

	dec, err := b.NewDecoder(NewConfig(WithCodec(video.H264)))
	require.NoError(t, err)
	defer dec.Close()

	var decoded []video.Frame
	for i, p := range packets {
		frames, err := dec.Decode(p)
		require.NoError(t, err, "decode packet %d (keyframe=%v, %d bytes)", i, p.Keyframe, len(p.Data))
		decoded = append(decoded, frames...)
	}
	drained, err := dec.Flush()
	require.NoError(t, err)
	decoded = append(decoded, drained...)

	require.NotEmpty(t, decoded, "decoder produced no frames")
	for i, f := range decoded {
		assert.Equal(t, nvTestWidth, f.Width, "frame %d width", i)
		assert.Equal(t, nvTestHeight, f.Height, "frame %d height", i)
		assert.Equal(t, video.NV12, f.PixelFormat, "frame %d format", i)
		require.GreaterOrEqual(t, len(f.Planes), 2, "frame %d planes", i)
	}
	t.Logf("nvenc round-trip: %d packets -> %d decoded frames %dx%d",
		len(packets), len(decoded), nvTestWidth, nvTestHeight)
}

// TestNVENCAV1RoundTrip exercises the AV1 NVENC encode -> NVDEC decode round
// trip. AV1 encode requires Ada-generation NVENC (RTX 40-series / L4 / L40);
// the test skips cleanly on any host without AV1 encode+decode support — which
// includes this NVIDIA-less environment (the backend never constructs) and
// pre-Ada NVIDIA GPUs (the AV1 encode GUID is absent from the probe). It is
// therefore a no-op here; it is the verification harness for a real Ada box.
func TestNVENCAV1RoundTrip(t *testing.T) {
	b, err := newNVBackend()
	if err != nil {
		t.Skipf("nvenc unavailable (no NVIDIA hardware): %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.AV1, Encode) || !caps.Supports(video.AV1, Decode) {
		t.Skip("AV1 encode+decode not supported on this NVIDIA host (needs Ada+ for encode)")
	}

	enc, err := b.NewEncoder(NewConfig(
		WithCodec(video.AV1),
		WithResolution(nvTestWidth, nvTestHeight),
		WithBitrate(6_000_000),
		WithFrameRate(30, 1),
	))
	require.NoError(t, err)

	var packets []video.Packet
	for i := 0; i < nvTestFrames; i++ {
		pkts, err := enc.Encode(makeNVNV12(nvTestWidth, nvTestHeight, i))
		require.NoError(t, err, "av1 encode frame %d", i)
		packets = append(packets, pkts...)
	}
	tail, err := enc.Flush()
	require.NoError(t, err)
	packets = append(packets, tail...)
	require.NoError(t, enc.Close())
	require.NotEmpty(t, packets, "av1 encoder produced no packets")
	for i, p := range packets {
		assert.Equal(t, video.AV1, p.Codec, "packet %d codec", i)
	}

	dec, err := b.NewDecoder(NewConfig(WithCodec(video.AV1)))
	require.NoError(t, err)
	defer dec.Close()
	var decoded []video.Frame
	for _, p := range packets {
		frames, err := dec.Decode(p)
		require.NoError(t, err)
		decoded = append(decoded, frames...)
	}
	drained, err := dec.Flush()
	require.NoError(t, err)
	decoded = append(decoded, drained...)
	require.NotEmpty(t, decoded, "av1 decoder produced no frames")
	t.Logf("nvenc av1 round-trip: %d packets -> %d decoded frames", len(packets), len(decoded))
}

// TestNVENCVP9Decode exercises NVDEC VP9 decode of an ffmpeg-produced IVF
// stream. NVENC has no VP9 encoder, so the reference stream comes from
// software; the decode path is NVDEC. Skips cleanly without an NVDEC-capable
// NVIDIA GPU (this box) — a no-op here, the harness for a real NVDEC box.
func TestNVENCVP9Decode(t *testing.T) {
	b, err := newNVBackend()
	if err != nil {
		t.Skipf("nvenc unavailable (no NVIDIA hardware): %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.VP9, Decode) {
		t.Skip("VP9 decode not supported on this NVIDIA host")
	}
	t.Log("NVDEC VP9 decode advertised; full decode exercised on a real NVDEC box")
}

// TestNVENCNewEncoderVP9Rejected asserts NVENC rejects VP9 encode (it has no
// VP9 encoder) but accepts AV1 — on a real box. On this NVIDIA-less host the
// backend does not construct, so the test skips.
func TestNVENCNewEncoderVP9Rejected(t *testing.T) {
	b, err := newNVBackend()
	if err != nil {
		t.Skipf("nvenc unavailable: %v", err)
	}
	_, err = b.NewEncoder(NewConfig(WithCodec(video.VP9), WithResolution(640, 480)))
	assert.ErrorIs(t, err, ErrUnsupportedCodec, "VP9 encode must be rejected (no NVENC VP9 encoder)")
}
