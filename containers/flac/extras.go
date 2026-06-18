package flac

// Extras carries FLAC-specific metadata that does not fit the uniform
// [containers.Header] view.
type Extras struct {
	// StreamInfo mirrors the STREAMINFO metadata block. The
	// container-level Header.SampleRate / Channels are populated from
	// these same fields; StreamInfo retains the FLAC-only values
	// (block / frame size bounds, total samples, MD5 signature).
	StreamInfo StreamInfo

	// Vendor is the VORBIS_COMMENT vendor string — the encoder
	// identification. Empty if the stream has no VORBIS_COMMENT block.
	Vendor string

	// SeekTable is the parsed contents of the SEEKTABLE metadata
	// block, if any. Placeholder seek points (sample number == 2^64-1)
	// are dropped during parsing.
	SeekTable []SeekPoint

	// Padding is the total byte count across all PADDING blocks.
	Padding int

	// Pictures preserves PICTURE metadata blocks as raw bodies.
	// Decoded fields are not exposed in phase 2 — callers needing
	// album art can parse the raw bytes per RFC 9639 §8.7.
	Pictures [][]byte

	// Application preserves APPLICATION metadata blocks keyed by their
	// four-byte registered ID.
	Application map[[4]byte][]byte

	// Cuesheet preserves the CUESHEET metadata block as raw bytes if
	// present. Phase 2 does not parse the structure.
	Cuesheet []byte
}

// StreamInfo is the parsed STREAMINFO block. Fields match
// [libraries/flac.StreamInfo] exactly so callers can pass it through
// without translation when constructing an encoder by hand.
type StreamInfo struct {
	MinBlockSize  int
	MaxBlockSize  int
	MinFrameSize  int
	MaxFrameSize  int
	SampleRate    int
	Channels      int
	BitsPerSample int
	TotalSamples  uint64
	MD5Signature  [16]byte
}

// SeekPoint is one entry in the SEEKTABLE metadata block.
type SeekPoint struct {
	// SampleNumber is the inter-channel sample index of the target
	// frame's first sample.
	SampleNumber uint64

	// StreamOffset is the byte offset, from the first byte of the
	// first frame header, to the target frame.
	StreamOffset uint64

	// FrameSamples is the number of samples per channel in the target
	// frame. Always > 0 for a non-placeholder point.
	FrameSamples uint16
}
