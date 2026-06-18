//go:build linux

// The v4l2 backend: a hwaccel.Backend driving Linux V4L2 memory-to-memory
// stateless and stateful video codecs through raw ioctl syscalls (no cgo,
// no libv4l). It enumerates every /dev/videoN multiplanar M2M node, probes
// each for the codecs it can decode/encode, and classifies it as one of
// two architectures:
//
//   - STATELESS (Request API) — the node advertises a parsed-slice coded
//     format (S265 = V4L2_PIX_FMT_HEVC_SLICE) on its OUTPUT queue and
//     exposes the V4L2_CID_STATELESS_HEVC_* controls. The Pi-5
//     rpi-hevc-dec (/dev/video19) is this. Userspace parses the full HEVC
//     bitstream and drives one media-controller request per frame; see
//     v4l2_stateless_hevc_linux.go.
//   - STATEFUL — the node advertises a plain coded format (HEVC/H264) the
//     hardware parses internally (Pi 4 bcm2835-codec, Rockchip, etc.); the
//     classic M2M state machine in v4l2_stateful_linux.go drives it, plus
//     the symmetric stateful encode path.
//
// V4L2 is a hardware path: it never falls back to software. A construction
// or runtime failure surfaces as ErrBackendFailure (wrapping the ioctl
// errno) so the policy walk records it and moves on; an unsupported codec
// surfaces as ErrUnsupportedCodec.

package hwaccel

import (
	"sync"

	"go-mediatoolkit/video"
)

// v4l2NodeKind classifies an M2M node by decode architecture.
type v4l2NodeKind uint8

const (
	v4l2KindNone      v4l2NodeKind = iota
	v4l2KindStateless              // Request API (parsed-slice coded format)
	v4l2KindStateful               // self-parsing M2M (plain coded format)
)

// v4l2Node is a probed M2M video node: its path, classification, and the
// codecs it can decode / encode.
type v4l2Node struct {
	path    string
	driver  string
	decKind v4l2NodeKind
	encKind v4l2NodeKind
	decodes map[video.Codec]bool
	encodes map[video.Codec]bool
}

// v4l2Backend is the V4L2 hwaccel.Backend. It caches the probed node set.
type v4l2Backend struct {
	mu        sync.Mutex
	probed    bool
	nodes     []v4l2Node
	probeCaps Capabilities
}

// newV4L2Backend constructs the backend if the host exposes at least one
// M2M multiplanar video node. It returns an error otherwise so
// backend_linux.go skips registration on hosts without V4L2 codecs.
func newV4L2Backend() (*v4l2Backend, error) {
	if !anyM2MMplaneNode() {
		return nil, ErrHardwareUnavailable
	}
	return new(v4l2Backend), nil
}

func (b *v4l2Backend) Name() string { return "v4l2" }

// Available reports whether any /dev/videoN exposes a multiplanar M2M
// codec capability — a cheap gate before the full Probe.
func (b *v4l2Backend) Available() bool {
	return anyM2MMplaneNode()
}

// anyM2MMplaneNode reports whether any video node is a multiplanar M2M
// codec device.
func anyM2MMplaneNode() bool {
	for _, p := range listVideoNodes() {
		dev, err := openV4L2Device(p)
		if err != nil {
			continue
		}
		m2m := dev.isM2MMplane()
		dev.close()
		if m2m {
			return true
		}
	}
	return false
}

// Probe enumerates every M2M node and builds the capability set. Results
// are cached.
func (b *v4l2Backend) Probe() (Capabilities, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.probed {
		return b.probeCaps, nil
	}

	b.nodes = probeNodes()

	h264 := CodecCapability{Codec: video.H264}
	h265 := CodecCapability{Codec: video.H265}
	for _, n := range b.nodes {
		if n.decodes[video.H264] {
			h264.Decode = true
		}
		if n.encodes[video.H264] {
			h264.Encode = true
		}
		if n.decodes[video.H265] {
			h265.Decode = true
		}
		if n.encodes[video.H265] {
			h265.Encode = true
		}
	}

	caps := Capabilities{Backend: b.Name()}
	if h264.Decode || h264.Encode {
		caps.Codecs = append(caps.Codecs, h264)
	}
	if h265.Decode || h265.Encode {
		caps.Codecs = append(caps.Codecs, h265)
	}
	b.probeCaps = caps
	b.probed = true
	return caps, nil
}

// probeNodes opens and classifies every M2M multiplanar node.
func probeNodes() []v4l2Node {
	var out []v4l2Node
	for _, p := range listVideoNodes() {
		dev, err := openV4L2Device(p)
		if err != nil {
			continue
		}
		if !dev.isM2MMplane() {
			dev.close()
			continue
		}
		node := classifyNode(dev)
		dev.close()
		if node.decKind != v4l2KindNone || node.encKind != v4l2KindNone {
			out = append(out, node)
		}
	}
	return out
}

// classifyNode inspects a node's OUTPUT/CAPTURE formats and controls to
// decide whether it is a stateless or stateful decoder and/or a stateful
// encoder, and which codecs it handles.
func classifyNode(dev *v4l2Device) v4l2Node {
	n := v4l2Node{
		path:    dev.path,
		driver:  dev.driverName(),
		decodes: make(map[video.Codec]bool),
		encodes: make(map[video.Codec]bool),
	}

	outFmts := dev.enumFormats(v4l2BufTypeVideoOutMP)
	capFmts := dev.enumFormats(v4l2BufTypeVideoCapMP)

	outHasCoded, outCodecs := codedFormats(outFmts)
	capHasCoded, capCodecs := codedFormats(capFmts)

	// Decode: coded format on OUTPUT, raw on CAPTURE.
	if outHasCoded {
		stateless := containsFourCC(outFmts, pixFmtHEVCSlice) &&
			dev.hasControl(v4l2CidStatelessHEVCSPS)
		if stateless {
			n.decKind = v4l2KindStateless
			n.decodes[video.H265] = true // S265 is HEVC parsed-slice
		} else {
			n.decKind = v4l2KindStateful
			for c := range outCodecs {
				n.decodes[c] = true
			}
		}
	}

	// Encode: coded format on CAPTURE, raw on OUTPUT.
	if capHasCoded && !outHasCoded {
		n.encKind = v4l2KindStateful
		for c := range capCodecs {
			n.encodes[c] = true
		}
	}
	return n
}

// codedFormats reports whether a format list contains a coded (HEVC/H264,
// plain or parsed-slice) format and which video.Codecs it maps to.
func codedFormats(fmts []uint32) (bool, map[video.Codec]bool) {
	codecs := make(map[video.Codec]bool)
	for _, f := range fmts {
		switch f {
		case pixFmtHEVC, pixFmtHEVCSlice:
			codecs[video.H265] = true
		case pixFmtH264:
			codecs[video.H264] = true
		}
	}
	return len(codecs) > 0, codecs
}

// containsFourCC reports whether fmts contains the given fourcc.
func containsFourCC(fmts []uint32, fourcc uint32) bool {
	for _, f := range fmts {
		if f == fourcc {
			return true
		}
	}
	return false
}

// NewDecoder routes to the stateless or stateful decoder session for the
// first node that decodes cfg.Codec.
func (b *v4l2Backend) NewDecoder(cfg Config) (Decoder, error) {
	if err := cfg.validateDecode(); err != nil {
		return nil, err
	}
	if _, err := b.Probe(); err != nil {
		return nil, err
	}
	for _, n := range b.nodes {
		if !n.decodes[cfg.Codec] {
			continue
		}
		switch n.decKind {
		case v4l2KindStateless:
			return newV4L2StatelessDecoder(n.path, cfg)
		case v4l2KindStateful:
			return newV4L2StatefulDecoder(n.path, cfg)
		}
	}
	return nil, ErrUnsupportedCodec
}

// NewEncoder routes to the stateful encoder session for the first node
// that encodes cfg.Codec.
func (b *v4l2Backend) NewEncoder(cfg Config) (Encoder, error) {
	if err := cfg.validateEncode(); err != nil {
		return nil, err
	}
	if _, err := b.Probe(); err != nil {
		return nil, err
	}
	for _, n := range b.nodes {
		if n.encodes[cfg.Codec] && n.encKind == v4l2KindStateful {
			return newV4L2StatefulEncoder(n.path, cfg)
		}
	}
	return nil, ErrUnsupportedCodec
}
