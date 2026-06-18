//go:build !cgo

package flac

// decoderParsesTags reports whether the active decoder backend parses
// VORBIS_COMMENT metadata (vendor string + tags). The native pure-Go backend
// intentionally does not surface comment metadata (Vendor() returns "" and
// Tags() returns nil), matching the Opus decoder's behavior, so the
// round-trip does not preserve vendor or tags.
const decoderParsesTags = false
