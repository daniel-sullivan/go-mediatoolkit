// Package flac reads and writes the native FLAC stream container.
//
// A FLAC stream begins with the four-byte magic "fLaC", followed by a
// chain of metadata blocks (mandatory STREAMINFO first, then any of
// VORBIS_COMMENT, SEEKTABLE, APPLICATION, CUESHEET, PADDING, PICTURE),
// followed by audio frames. The structure is described in RFC 9639.
//
// A [Reader] parses the metadata chain and exposes a continuous
// [io.Reader] over the original byte stream — including the magic and
// metadata bytes — so callers can pipe it straight into
// [github.com/daniel-sullivan/go-mediatoolkit/libraries/flac.NewDecoder] without re-seeking.
//
// A [Writer] wraps a [libraries/flac.Encoder]. It projects a
// [containers.Header] onto the encoder's options (tags from
// Header.Tags, total-sample hint, compression level via [WithCompressionLevel])
// and exposes [Writer.Samples] for the caller to feed interleaved int32
// samples. The encoder owns metadata writing; the container layer adds
// no bytes of its own.
//
// For Ogg-encapsulated FLAC, see [github.com/daniel-sullivan/go-mediatoolkit/containers/ogg.NewFLACReader].
package flac

import "github.com/daniel-sullivan/go-mediatoolkit/containers"

// Header is the container Header specialised to FLAC Extras.
type Header = containers.Header[Extras]
