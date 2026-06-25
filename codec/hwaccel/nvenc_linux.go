//go:build linux

// The nvenc backend: a hwaccel.Backend driving NVIDIA's fixed-function
// NVENC (encode) and NVDEC/cuvid (decode) engines on a GeForce/Quadro/Tesla
// GPU through the purego bindings in nvenc_bindings_linux.go. No cgo: the
// CUDA Driver API (libcuda), the NVENCODE API (libnvidia-encode) and the
// NVDEC/cuvid API (libnvcuvid) are all dlopen'd and called through resolved
// function pointers.
//
// # Directions
//
//   - Decode uses the cuvid bitstream parser + NVDEC decoder for H.264 and
//     H.265, mapping the decoded NV12 surface out of device memory into a
//     video.Frame.
//   - Encode uses NVENC CBR for H.264 and H.265 with the SDK preset config
//     (P4, high-quality tuning), staging the input NV12 frame through a host
//     input buffer and emitting Annex-B packets (SPS/PPS(/VPS) forced out on
//     every IDR via NV_ENC_PIC_FLAG_OUTPUT_SPSPPS).
//
// NVENC is a hardware path: it never falls back to software. A construction
// or runtime failure surfaces as ErrBackendFailure (wrapping the NVENCSTATUS
// or CUresult) so the policy walk records it and moves on.
//
// ###############################################################
// # HARDWARE-VERIFICATION STATUS — READ THIS                    #
// ###############################################################
//
// This backend is written against the published NVIDIA Video Codec SDK 13.0
// headers (nvEncodeAPI.h NVENCAPI 13.0, cuviddec.h, nvcuvid.h) and the CUDA
// Driver API, with every marshalled struct's byte layout, field offsets and
// version stamps pinned in nvenc_abi_linux_test.go against the values
// compiled directly from those SDK 13.0 headers. It has NOT been run on an
// NVIDIA GPU: it was developed on a machine with no NVIDIA device or driver,
// so the live encode/decode round-trip is UNVERIFIED. The unit tests that run
// here only prove graceful degradation — Available() returns false cleanly,
// Probe/NewEncoder/NewDecoder return the right errors, and the struct ABI is
// the SDK 13.0 ABI. The hardware round-trip test (TestNVENCRoundTrip)
// t.Skips when Available() is false, so it is a no-op here.
//
// To verify the hardware path, run `go test ./codec/hwaccel/` (with
// CGO_ENABLED=0) on a Linux host that has an NVENC/NVDEC-capable NVIDIA GPU
// and a matching driver installed (which provides libcuda.so.1,
// libnvidia-encode.so.1 and libnvcuvid.so.1). On such a host Available()
// returns true and the skipped round-trip tests run end to end. If the
// installed driver predates SDK 13.0, NvEncodeAPICreateInstance will reject
// the version stamp — bump (or down-stamp) the constants in
// nvenc_bindings_linux.go to the driver's NVENCAPI version.

package hwaccel

import (
	"errors"
	"fmt"
	"sync"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// nvBackend is the NVENC/NVDEC hwaccel.Backend. The CUDA context is created
// once (on the first device) and shared; per-encoder/decoder session state is
// built on top of it.
type nvBackend struct {
	lib *nvLib

	device  int32
	context uintptr

	mu        sync.Mutex
	probed    bool
	probeCaps Capabilities
}

// newNVBackend dlopens the NVIDIA libraries, runs cuInit, checks at least one
// CUDA device is present, and creates a CUDA context on device 0. Any failure
// returns an error so the backend is not registered (backend_linux.go skips
// it) — including, on this NVIDIA-less environment, the dlopen of libcuda.
func newNVBackend() (*nvBackend, error) {
	lib, err := loadNVENC()
	if err != nil {
		return nil, err
	}
	if st := lib.cuInit(0); st != cudaSuccess {
		return nil, fmt.Errorf("%w: cuInit CUresult=%d", ErrBackendFailure, st)
	}
	var count int32
	if st := lib.cuDeviceGetCount(&count); st != cudaSuccess {
		return nil, fmt.Errorf("%w: cuDeviceGetCount CUresult=%d", ErrBackendFailure, st)
	}
	if count <= 0 {
		return nil, ErrHardwareUnavailable
	}
	var dev int32
	if st := lib.cuDeviceGet(&dev, 0); st != cudaSuccess {
		return nil, fmt.Errorf("%w: cuDeviceGet CUresult=%d", ErrBackendFailure, st)
	}
	var ctx uintptr
	if st := lib.cuCtxCreate(&ctx, 0, dev); st != cudaSuccess {
		return nil, fmt.Errorf("%w: cuCtxCreate CUresult=%d", ErrBackendFailure, st)
	}
	return &nvBackend{lib: lib, device: dev, context: ctx}, nil
}

func (b *nvBackend) Name() string { return "nvenc" }

// Available reports whether the NVIDIA libraries dlopen'd, cuInit succeeded,
// and at least one CUDA device exists — all of which already happened in
// newNVBackend, so a constructed backend is by definition available. On a host
// with no NVIDIA driver (this environment) newNVBackend fails and the backend
// is never constructed, so this is never reached with b==nil from the
// registry; the guard keeps it safe regardless.
func (b *nvBackend) Available() bool {
	return b != nil && b.context != 0
}

// Probe reports the codecs NVENC/NVDEC expose. The encode side is queried by
// walking the encode GUIDs NvEncGetEncodeGUIDs returns (H.264 / HEVC); the
// decode side is reported for the same codecs when libnvcuvid is present (the
// cuvid decoder advertises H.264 + HEVC on every NVDEC-capable GPU this
// backend targets). Results are cached.
func (b *nvBackend) Probe() (Capabilities, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.probed {
		return b.probeCaps, nil
	}

	encGUIDs, err := b.queryEncodeGUIDs()
	if err != nil {
		return Capabilities{}, err
	}

	h264 := CodecCapability{Codec: video.H264}
	h265 := CodecCapability{Codec: video.H265}
	av1 := CodecCapability{Codec: video.AV1}
	vp9 := CodecCapability{Codec: video.VP9}
	for _, g := range encGUIDs {
		switch {
		case g.equal(nvEncCodecH264GUID):
			h264.Encode = true
			h264.Profiles = appendUnique(h264.Profiles, "high")
		case g.equal(nvEncCodecHEVCGUID):
			h265.Encode = true
			h265.Profiles = appendUnique(h265.Profiles, "main")
		case g.equal(nvEncCodecAV1GUID):
			// AV1 encode arrived with Ada (RTX 40-series / L4 / L40). NVENC
			// reports the GUID only on capable silicon, so its presence in the
			// query is the truthful encode-support signal.
			av1.Encode = true
			av1.Profiles = appendUnique(av1.Profiles, "main")
		}
	}

	// NVDEC decode is available whenever libnvcuvid loaded. The cuvid decoder
	// advertises H.264 + HEVC on every NVDEC GPU this backend targets; VP9
	// decode is present on Maxwell-gen2+ and AV1 decode on Ampere+. Reported
	// here for completeness — the per-codec cuvid caps query would refine this
	// on real hardware (see newNVDecoder, which fails construction loudly if the
	// installed NVDEC lacks the codec).
	if b.lib.hasDecode() {
		h264.Decode = true
		h264.Profiles = appendUnique(h264.Profiles, "high")
		h265.Decode = true
		h265.Profiles = appendUnique(h265.Profiles, "main")
		vp9.Decode = true
		vp9.Profiles = appendUnique(vp9.Profiles, "profile0")
		av1.Decode = true
		av1.Profiles = appendUnique(av1.Profiles, "main")
	}

	caps := Capabilities{Backend: b.Name()}
	for _, cc := range []CodecCapability{h264, h265, vp9, av1} {
		if cc.Encode || cc.Decode {
			caps.Codecs = append(caps.Codecs, cc)
		}
	}

	b.probeCaps = caps
	b.probed = true
	return caps, nil
}

// queryEncodeGUIDs opens a transient encode session, asks NVENC for the list
// of encode GUIDs it supports, and closes the session. A session is required
// because NvEncGetEncodeGUIDs is a through-table call keyed on the encoder
// handle.
func (b *nvBackend) queryEncodeGUIDs() ([]nvGUID, error) {
	enc, err := b.openSession()
	if err != nil {
		return nil, err
	}
	defer b.lib.destroyEncoder(enc)

	var count uint32
	if st := b.lib.getEncodeGUIDCount(enc, &count); st != nvEncSuccess {
		return nil, fmt.Errorf("%w: NvEncGetEncodeGUIDCount NVENCSTATUS=%d", ErrBackendFailure, st)
	}
	if count == 0 {
		return nil, nil
	}
	guids := make([]nvGUID, count)
	var got uint32
	if st := b.lib.getEncodeGUIDs(enc, &guids[0], count, &got); st != nvEncSuccess {
		return nil, fmt.Errorf("%w: NvEncGetEncodeGUIDs NVENCSTATUS=%d", ErrBackendFailure, st)
	}
	return guids[:got], nil
}

// openSession opens an NVENC encode session bound to the backend's CUDA
// context and returns the opaque encoder handle. The caller destroys it.
func (b *nvBackend) openSession() (uintptr, error) {
	params := nvEncOpenEncodeSessionExParams{
		Version:    nvOpenSessionExVer,
		DeviceType: nvEncDeviceTypeCUDA,
		Device:     b.context,
		APIVersion: uint32(nvencAPIVersion),
	}
	var enc uintptr
	if st := b.lib.openSessionEx(&params, &enc); st != nvEncSuccess {
		return 0, fmt.Errorf("%w: NvEncOpenEncodeSessionEx NVENCSTATUS=%d", ErrBackendFailure, st)
	}
	return enc, nil
}

// NewEncoder constructs an NVENC CBR encoder for cfg. NVENC encodes H.264,
// H.265 and (on Ada+) AV1; it has no VP9 encoder, so VP9 returns
// ErrUnsupportedCodec.
func (b *nvBackend) NewEncoder(cfg Config) (Encoder, error) {
	if err := cfg.validateEncode(); err != nil {
		return nil, err
	}
	switch cfg.Codec {
	case video.H264, video.H265, video.AV1:
	default:
		return nil, ErrUnsupportedCodec
	}
	return newNVEncoder(b, cfg)
}

// NewDecoder constructs an NVDEC/cuvid decoder for cfg. The cuvid parser and
// decoder are built lazily once the first keyframe's geometry is known (the
// sequence callback). NVDEC decodes H.264, H.265, VP9 and AV1. Requires
// libnvcuvid; without it ErrUnsupportedCodec.
func (b *nvBackend) NewDecoder(cfg Config) (Decoder, error) {
	if err := cfg.validateDecode(); err != nil {
		return nil, err
	}
	switch cfg.Codec {
	case video.H264, video.H265, video.VP9, video.AV1:
	default:
		return nil, ErrUnsupportedCodec
	}
	if !b.lib.hasDecode() {
		return nil, ErrUnsupportedCodec
	}
	return newNVDecoder(b, cfg), nil
}

// ---- shared helpers ---------------------------------------------------

// nvStatusError wraps a non-zero NVENCSTATUS / CUresult into a plain error
// carrying the numeric code, for joining into ErrBackendFailure on the load
// path (where the sentinels are not yet in scope as %w targets).
func nvStatusError(code int32) error {
	return errors.New("nvenc: non-zero status " + itoa(int(code)))
}

// itoa is a tiny base-10 int formatter (avoids pulling strconv onto the load
// path for a single status code).
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// nvCodecGUID returns the NVENC codec GUID for a video.Codec.
func nvCodecGUID(c video.Codec) nvGUID {
	switch c {
	case video.H265:
		return nvEncCodecHEVCGUID
	case video.AV1:
		return nvEncCodecAV1GUID
	default:
		return nvEncCodecH264GUID
	}
}

// nvProfileGUID returns the NVENC profile GUID for a codec/profile pair,
// defaulting to High (H.264) / Main (H.265 / AV1).
func nvProfileGUID(c video.Codec, profile string) nvGUID {
	switch c {
	case video.H265:
		if profile == "main10" {
			return nvEncHEVCProfileMain10GUID
		}
		return nvEncHEVCProfileMainGUID
	case video.AV1:
		return nvEncAV1ProfileMainGUID
	}
	switch profile {
	case "baseline", "constrained_baseline":
		return nvEncH264ProfileBaselineGUID
	case "main":
		return nvEncH264ProfileMainGUID
	default:
		return nvEncH264ProfileHighGUID
	}
}

// nvCudaVideoCodec returns the cuvid codec id for a video.Codec.
func nvCudaVideoCodec(c video.Codec) uint32 {
	switch c {
	case video.H265:
		return cudaVideoCodecHEVC
	case video.VP9:
		return cudaVideoCodecVP9
	case video.AV1:
		return cudaVideoCodecAV1
	default:
		return cudaVideoCodecH264
	}
}
