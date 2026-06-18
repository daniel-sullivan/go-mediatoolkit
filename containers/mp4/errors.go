package mp4

import "errors"

var (
	// ErrNotMP4 indicates the stream is not a recognisable ISOBMFF/MP4
	// file (no leading ftyp box, or a corrupt initial box header).
	ErrNotMP4 = errors.New("mp4: not an ISOBMFF/MP4 stream")

	// ErrInvalidBox indicates a box header or body was malformed — a size
	// smaller than its header, a body that overruns its parent, etc.
	ErrInvalidBox = errors.New("mp4: invalid box")

	// ErrMissingMoov indicates the file has no moov box, so no track or
	// sample-table metadata is available.
	ErrMissingMoov = errors.New("mp4: missing moov box")

	// ErrMissingEsds indicates the audio sample entry has no esds box, so
	// the AAC AudioSpecificConfig could not be recovered.
	ErrMissingEsds = errors.New("mp4: missing esds (AudioSpecificConfig)")

	// ErrInvalidSampleTable indicates the stsz / stsc / stco tables are
	// inconsistent or truncated and cannot locate the AAC access units.
	ErrInvalidSampleTable = errors.New("mp4: invalid sample table")

	// ErrUnsupportedCodec indicates the track's sample entry is not AAC.
	ErrUnsupportedCodec = errors.New("mp4: unsupported codec (expected AAC)")

	// ErrBadArg indicates an invalid argument, such as a nil reader or
	// destination writer.
	ErrBadArg = errors.New("mp4: invalid argument")

	// ErrAlreadyClosed indicates a Writer method was called after Close.
	ErrAlreadyClosed = errors.New("mp4: writer already closed")
)
