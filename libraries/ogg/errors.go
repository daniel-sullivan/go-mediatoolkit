package ogg

import "errors"

var (
	// ErrBadArg indicates an invalid argument.
	ErrBadArg = errors.New("ogg: bad argument")

	// ErrInternalError indicates an internal library error.
	ErrInternalError = errors.New("ogg: internal error")

	// ErrStreamMismatch indicates a page was submitted to a stream
	// decoder with a non-matching serial number.
	ErrStreamMismatch = errors.New("ogg: stream serial number mismatch")

	// ErrBadVersion indicates an unsupported Ogg stream version.
	ErrBadVersion = errors.New("ogg: unsupported stream version")
)
