package aac

import "errors"

var (
	// ErrFormatMismatch indicates the audio format passed to Write does
	// not match the format the encoder was constructed for.
	ErrFormatMismatch = errors.New("aac: audio format does not match encoder")

	// ErrBadConfig indicates the AudioSpecificConfig passed to NewDecoder
	// is missing or describes an unsupported stream.
	ErrBadConfig = errors.New("aac: invalid or missing AudioSpecificConfig")
)
