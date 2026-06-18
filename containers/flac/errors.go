package flac

import "errors"

var (
	// ErrNotFLAC indicates the stream does not begin with the "fLaC"
	// magic four-byte signature.
	ErrNotFLAC = errors.New("flac: not a FLAC stream")

	// ErrMissingStreamInfo indicates the first metadata block was not
	// STREAMINFO. STREAMINFO is mandatory and must come first.
	ErrMissingStreamInfo = errors.New("flac: missing STREAMINFO")

	// ErrInvalidMetadata indicates a metadata block was malformed —
	// truncated body, bad VORBIS_COMMENT length, etc.
	ErrInvalidMetadata = errors.New("flac: invalid metadata block")

	// ErrUnsupportedFormat indicates the Header passed to NewWriter
	// does not describe a supportable FLAC stream (zero sample rate,
	// out-of-range bit depth, etc.). The underlying validation is in
	// libraries/flac.
	ErrUnsupportedFormat = errors.New("flac: unsupported format")

	// ErrAlreadyClosed indicates the Writer has already been closed.
	ErrAlreadyClosed = errors.New("flac: writer already closed")

	// ErrBadArg indicates an invalid argument was passed, such as a nil
	// destination writer.
	ErrBadArg = errors.New("flac: invalid argument")
)
