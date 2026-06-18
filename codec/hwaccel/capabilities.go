package hwaccel

import "go-mediatoolkit/video"

// Capabilities is the result of a backend's Probe: the precise set of
// codecs it can encode and/or decode on the current host, including the
// profiles it advertises. Unlike Backend.Available (a cheap "does the
// library/device exist" gate), Capabilities reflects an actual query of
// the accelerator and may be expensive to obtain. Probe results are
// cached by the backend.
type Capabilities struct {
	// Backend is the reporting backend's Name.
	Backend string
	// Codecs lists every codec the backend exposes, each annotated with
	// the supported directions and profiles.
	Codecs []CodecCapability
}

// CodecCapability describes what a backend can do with a single codec.
type CodecCapability struct {
	Codec  video.Codec
	Encode bool
	Decode bool
	// Profiles names the codec profiles the backend advertises, e.g.
	// "baseline", "main", "high" for H.264 or "main", "main10" for
	// H.265. May be empty if the backend does not report profiles.
	Profiles []string
}

// Supports reports whether the capability set includes codec in the
// given direction.
func (c Capabilities) Supports(codec video.Codec, dir Direction) bool {
	for _, cc := range c.Codecs {
		if cc.Codec != codec {
			continue
		}
		switch dir {
		case Encode:
			return cc.Encode
		case Decode:
			return cc.Decode
		}
	}
	return false
}

// Direction selects the encode or the decode half of a backend.
type Direction uint8

const (
	// Encode is the raw-frame-to-packet direction.
	Encode Direction = iota
	// Decode is the packet-to-raw-frame direction.
	Decode
)

// String returns "encode" or "decode".
func (d Direction) String() string {
	if d == Decode {
		return "decode"
	}
	return "encode"
}
