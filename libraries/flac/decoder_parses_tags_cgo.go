//go:build cgo

package flac

// decoderParsesTags reports whether the active decoder backend parses
// VORBIS_COMMENT metadata (vendor string + tags). The cgo backend wraps
// libFLAC, which parses the comment block, so the encode/decode round-trip
// preserves vendor and tags.
const decoderParsesTags = true
