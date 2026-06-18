// Package wav reads and writes RIFF/WAVE audio files.
//
// A [Reader] parses the RIFF header, optional metadata chunks (LIST/INFO,
// bext), and exposes the data chunk payload as an [io.Reader] of raw PCM
// bytes. Callers feed that into [go-mediatoolkit/codec/pcm.NewDecoder] to
// obtain float64 samples.
//
// A [Writer] emits a RIFF/WAVE header from a user-supplied
// [containers.Header] and exposes an [io.Writer] that accepts raw PCM
// bytes. Close backpatches the RIFF/data chunk sizes, so the output must
// be an [io.WriteSeeker].
//
// Supported PCM formats: uint8, int16, int24, int32, float32. Multi-channel
// WAVEFORMATEXTENSIBLE files are parsed but only the basic PCM subformats
// are honoured.
package wav

import "go-mediatoolkit/containers"

// Header is the container Header specialised to WAV Extras.
type Header = containers.Header[Extras]
