package mp3

import mp3lib "go-mediatoolkit/libraries/mp3"

// Extras carries MP3/ID3-specific metadata that does not fit the uniform
// [containers.Header] view.
type Extras struct {
	// StreamInfo mirrors the parameters of the first MPEG audio frame.
	// NewReader peeks (but does not decode) that frame's 4-byte header and
	// fills it; the container-level Header.SampleRate / Channels are populated
	// from these same fields, while StreamInfo retains the MP3-only values
	// (MPEG version, samples-per-frame, nominal bit rate). All fields are zero
	// if no parseable frame header was found.
	StreamInfo mp3lib.StreamInfo

	// ID3v2Version is the major version of the leading ID3v2 tag (e.g. 3
	// for ID3v2.3, 4 for ID3v2.4). Zero if the stream has no ID3v2 tag.
	ID3v2Version int

	// HasID3v1 reports whether a trailing 128-byte ID3v1 tag was found.
	// Only detectable on a seekable source.
	HasID3v1 bool

	// RawFrames preserves ID3v2 frames that do not map to a standard tag,
	// keyed by their four-character (ID3v2.3/2.4) frame ID. The values are
	// the raw frame bodies, excluding the frame header.
	RawFrames map[string][]byte

	// Pictures preserves APIC (attached picture) ID3v2 frames as raw frame
	// bodies. Album-art callers can parse these per the ID3v2 spec.
	Pictures [][]byte
}
