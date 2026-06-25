//go:build linux

// The vaapi backend: a hwaccel.Backend driving VA-API (libva) on an
// Intel/AMD GPU through the purego bindings in vabindings_linux.go. It
// opens the DRM render node (/dev/dri/renderD128) directly with a
// syscall, derives a VADisplay from it (vaGetDisplayDRM), and runs
// vaInitialize once at construction; the display is shared by every
// encoder and decoder the backend builds.
//
// # Directions
//
//   - Decode uses VAEntrypointVLD for H.264 and H.265, reading the
//     decoded NV12 surface back into a video.Frame.
//   - Encode uses VAEntrypointEncSliceLP (low-power) CBR for H.264 and
//     H.265, uploading the input NV12 frame and emitting Annex-B packets
//     with self-authored parameter-set headers on keyframes.
//
// VAAPI is a hardware path: it never falls back to software. A
// construction or runtime failure surfaces as ErrBackendFailure (wrapping
// the VAStatus) so the policy walk records it and moves on.

package hwaccel

import (
	"fmt"
	"sync"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// renderNodePath is the headless DRM render node the backend opens. It is
// the conventional first render node on a single-GPU host (the Arc box).
const renderNodePath = "/dev/dri/renderD128"

// vaBackend is the VA-API hwaccel.Backend. The VADisplay is created once
// and shared; per-encoder/decoder state (config, context, surfaces) is
// built on top of it.
type vaBackend struct {
	lib     *vaLib
	fd      int
	display uintptr

	mu        sync.Mutex
	probed    bool
	probeCaps Capabilities
}

// newVABackend dlopens libva, opens the render node, derives a VADisplay
// and runs vaInitialize. Any failure returns an error so the backend is
// not registered (backend_linux.go skips it).
func newVABackend() (*vaBackend, error) {
	lib, err := loadVA()
	if err != nil {
		return nil, err
	}
	fd, err := openRenderNode(renderNodePath)
	if err != nil {
		return nil, err
	}
	display := lib.vaGetDisplayDRM(int32(fd))
	if display == 0 {
		closeFD(fd)
		return nil, ErrHardwareUnavailable
	}
	var major, minor int32
	if st := lib.vaInitialize(display, &major, &minor); st != vaStatusSuccess {
		closeFD(fd)
		return nil, fmt.Errorf("%w: vaInitialize VAStatus=%d", ErrBackendFailure, st)
	}
	return &vaBackend{lib: lib, fd: fd, display: display}, nil
}

func (b *vaBackend) Name() string { return "vaapi" }

// Available reports whether libva dlopen'd, the render node opened, and
// vaInitialize succeeded — all of which already happened in newVABackend,
// so a constructed backend is by definition available.
func (b *vaBackend) Available() bool {
	return b != nil && b.display != 0
}

// Probe queries the driver for the profiles and entrypoints it exposes and
// maps the H.264 / H.265 ones onto Capabilities. VLD entrypoints mark
// decode support; EncSliceLP (or EncSlice) entrypoints mark encode
// support. Results are cached.
func (b *vaBackend) Probe() (Capabilities, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.probed {
		return b.probeCaps, nil
	}

	profiles, err := b.queryProfiles()
	if err != nil {
		return Capabilities{}, err
	}

	// Accumulate per-codec capability from the profile/entrypoint matrix.
	h264 := CodecCapability{Codec: video.H264}
	h265 := CodecCapability{Codec: video.H265}
	vp9 := CodecCapability{Codec: video.VP9}
	av1 := CodecCapability{Codec: video.AV1}
	for _, p := range profiles {
		codec, profileName, ok := codecForProfile(p)
		if !ok {
			continue
		}
		eps, err := b.queryEntrypoints(p)
		if err != nil {
			continue
		}
		var dec, enc bool
		for _, ep := range eps {
			switch ep {
			case vaEntrypointVLD:
				dec = true
			case vaEntrypointEncSliceLP, vaEntrypointEncSlice:
				enc = true
			}
		}
		var cc *CodecCapability
		switch codec {
		case video.H264:
			cc = &h264
		case video.H265:
			cc = &h265
		case video.VP9:
			cc = &vp9
		case video.AV1:
			cc = &av1
		default:
			continue
		}
		cc.Decode = cc.Decode || dec
		cc.Encode = cc.Encode || enc
		if dec || enc {
			cc.Profiles = appendUnique(cc.Profiles, profileName)
		}
	}

	caps := Capabilities{Backend: b.Name()}
	for _, cc := range []CodecCapability{h264, h265, vp9, av1} {
		if cc.Decode || cc.Encode {
			caps.Codecs = append(caps.Codecs, cc)
		}
	}

	b.probeCaps = caps
	b.probed = true
	return caps, nil
}

// queryProfiles returns the driver's supported VAProfile list.
func (b *vaBackend) queryProfiles() ([]int32, error) {
	maxN := b.lib.vaMaxNumProfiles(b.display)
	if maxN <= 0 {
		return nil, fmt.Errorf("%w: vaMaxNumProfiles=%d", ErrBackendFailure, maxN)
	}
	list := make([]int32, maxN)
	var n int32
	if st := b.lib.vaQueryConfigProfiles(b.display, &list[0], &n); st != vaStatusSuccess {
		return nil, fmt.Errorf("%w: vaQueryConfigProfiles VAStatus=%d", ErrBackendFailure, st)
	}
	return list[:n], nil
}

// queryEntrypoints returns the entrypoints the driver exposes for profile.
func (b *vaBackend) queryEntrypoints(profile int32) ([]uint32, error) {
	maxN := b.lib.vaMaxNumEntrypoints(b.display)
	if maxN <= 0 {
		return nil, fmt.Errorf("%w: vaMaxNumEntrypoints=%d", ErrBackendFailure, maxN)
	}
	list := make([]uint32, maxN)
	var n int32
	if st := b.lib.vaQueryConfigEntrypoints(b.display, profile, &list[0], &n); st != vaStatusSuccess {
		return nil, fmt.Errorf("%w: vaQueryConfigEntrypoints VAStatus=%d", ErrBackendFailure, st)
	}
	return list[:n], nil
}

// NewEncoder constructs a VA-API low-power CBR encoder for cfg. H.264, H.265,
// VP9 and AV1 are driven through the EncSliceLP entrypoint.
func (b *vaBackend) NewEncoder(cfg Config) (Encoder, error) {
	if err := cfg.validateEncode(); err != nil {
		return nil, err
	}
	switch cfg.Codec {
	case video.H264, video.H265, video.VP9, video.AV1:
	default:
		return nil, ErrUnsupportedCodec
	}
	return newVAEncoder(b, cfg)
}

// NewDecoder constructs a VA-API VLD decoder for cfg. The VA config,
// context and surfaces are built lazily from the first keyframe's
// parameter sets (H.264/H.265) or uncompressed/OBU header (VP9/AV1).
func (b *vaBackend) NewDecoder(cfg Config) (Decoder, error) {
	if err := cfg.validateDecode(); err != nil {
		return nil, err
	}
	switch cfg.Codec {
	case video.H264, video.H265, video.VP9, video.AV1:
	default:
		return nil, ErrUnsupportedCodec
	}
	return newVADecoder(b, cfg), nil
}

// ---- shared helpers ---------------------------------------------------

// vaProfileFor returns the VAProfile to use for a codec/profile pair when
// building an encode or decode config. H.264 defaults to High (the
// superset the Arc exposes for both VLD and EncSliceLP); H.265 to Main.
func vaProfileFor(codec video.Codec, profile string) int32 {
	if codec == video.H265 {
		if profile == "main10" {
			return vaProfileHEVCMain10
		}
		return vaProfileHEVCMain
	}
	switch profile {
	case "baseline", "constrained_baseline":
		return vaProfileH264ConstrainedBaseline
	case "main":
		return vaProfileH264Main
	default:
		return vaProfileH264High
	}
}

// codecForProfile maps a probed VAProfile to a video.Codec and the
// canonical profile token. Only the profiles the backend can drive are
// recognised; others return ok=false.
func codecForProfile(p int32) (video.Codec, string, bool) {
	switch p {
	case vaProfileH264ConstrainedBaseline:
		return video.H264, "baseline", true
	case vaProfileH264Main:
		return video.H264, "main", true
	case vaProfileH264High:
		return video.H264, "high", true
	case vaProfileHEVCMain:
		return video.H265, "main", true
	case vaProfileHEVCMain10:
		return video.H265, "main10", true
	case vaProfileVP9Profile0:
		return video.VP9, "profile0", true
	case vaProfileVP9Profile1:
		return video.VP9, "profile1", true
	case vaProfileVP9Profile2:
		return video.VP9, "profile2", true
	case vaProfileVP9Profile3:
		return video.VP9, "profile3", true
	case vaProfileAV1Profile0:
		return video.AV1, "profile0", true
	case vaProfileAV1Profile1:
		return video.AV1, "profile1", true
	default:
		return video.CodecUnknown, "", false
	}
}

// appendUnique appends s to xs unless already present.
func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}
