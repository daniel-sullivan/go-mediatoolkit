// Package mp3 reads and writes ID3 metadata around a native MP3 stream.
//
// An MP3 file is a continuous sequence of self-framed MP3 audio frames,
// optionally bracketed by ID3 metadata: an ID3v2 tag (artist, title, album,
// …) preceding the audio frames, and optionally a fixed-size 128-byte ID3v1
// tag at the end of the file. The audio framing itself is owned by the codec
// — this container layer only parses and projects the ID3 metadata.
//
// A [Reader] parses the leading ID3v2 tag (and the trailing ID3v1 tag when
// the stream is seekable) and exposes a continuous [io.Reader] over the
// original byte stream — including the ID3 bytes — so callers can pipe it
// straight into [github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3.NewDecoder]. (The decoder
// skips ID3 frames itself, so no re-seeking is required.)
//
// A [Writer] wraps a [libraries/mp3.Encoder]. It projects a [containers.Header]
// onto an ID3v2 tag written ahead of the audio frames, then forwards
// interleaved samples to the encoder. The encoder owns audio framing; the
// container layer adds the ID3 metadata bytes on top.
//
// Metadata is normalized onto [containers.StandardTags]: ID3 frame IDs
// (TPE1, TIT2, TALB, …) are mapped to the Vorbis-comment-style standard tag
// names used across the toolkit.
package mp3

import "github.com/daniel-sullivan/go-mediatoolkit/containers"

// Header is the container Header specialised to MP3 Extras.
type Header = containers.Header[Extras]
