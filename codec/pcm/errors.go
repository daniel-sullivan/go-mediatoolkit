package pcm

import "errors"

var (
	ErrUnsupportedFormat = errors.New("pcm: unsupported sample format")
	ErrShortWrite        = errors.New("pcm: short write to output")
	ErrFormatMismatch    = errors.New("pcm: audio format does not match encoder")
)
