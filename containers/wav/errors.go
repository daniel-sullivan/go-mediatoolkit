package wav

import "errors"

var (
	ErrNotRIFF           = errors.New("wav: not a RIFF file")
	ErrNotWAVE           = errors.New("wav: not a WAVE file")
	ErrMissingFmt        = errors.New("wav: fmt chunk missing or out of order")
	ErrMissingData       = errors.New("wav: data chunk not found")
	ErrUnsupportedFormat = errors.New("wav: unsupported sample format")
	ErrBadChunkSize      = errors.New("wav: invalid chunk size")
	ErrNeedSeeker        = errors.New("wav: writer requires io.WriteSeeker")
	ErrAlreadyClosed     = errors.New("wav: writer already closed")
)
