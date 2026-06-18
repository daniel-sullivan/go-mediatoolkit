package opus

import "errors"

var (
	// ErrInvalidPacket indicates a corrupted or malformed Opus packet.
	ErrInvalidPacket = errors.New("opus: invalid packet")

	// ErrBadArg indicates an invalid argument (unsupported sample rate, channel count, etc).
	ErrBadArg = errors.New("opus: bad argument")

	// ErrBufferTooSmall indicates the output buffer is too small for the decoded frame.
	ErrBufferTooSmall = errors.New("opus: output buffer too small")

	// ErrInternalError indicates an internal codec error.
	ErrInternalError = errors.New("opus: internal error")

	// ErrUnimplemented indicates a codec mode or feature that is not yet implemented.
	ErrUnimplemented = errors.New("opus: unimplemented")

	// ErrInternal is a shorter alias for ErrInternalError used by the native wiring.
	ErrInternal = ErrInternalError

	// ErrInvalidState indicates the codec state is invalid for the requested op.
	ErrInvalidState = errors.New("opus: invalid state")

	// ErrAllocFail indicates an internal allocation failure.
	ErrAllocFail = errors.New("opus: allocation failed")
)
