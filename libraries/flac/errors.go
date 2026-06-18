package flac

import "errors"

var (
	// ErrBadArg indicates an invalid argument: unsupported sample rate,
	// channel count, bit depth, nil reader/writer, or a buf whose length
	// is not a multiple of the configured channel count.
	ErrBadArg = errors.New("flac: bad argument")

	// ErrBadSampleRate indicates a sample rate outside [1, MaxSampleRate].
	ErrBadSampleRate = errors.New("flac: bad sample rate")

	// ErrBadChannels indicates a channel count outside [1, MaxChannels].
	ErrBadChannels = errors.New("flac: bad channel count")

	// ErrBadBitDepth indicates a bit depth outside [MinBitsPerSample,
	// MaxBitsPerSample].
	ErrBadBitDepth = errors.New("flac: bad bit depth")

	// ErrInvalidStream indicates a corrupted or malformed FLAC stream.
	ErrInvalidStream = errors.New("flac: invalid stream")

	// ErrUnsupportedStream indicates a stream feature the wrapper does
	// not handle (e.g., an Ogg-FLAC stream passed to the native FLAC
	// decoder).
	ErrUnsupportedStream = errors.New("flac: unsupported stream")

	// ErrMD5Mismatch indicates the MD5 checksum in STREAMINFO does not
	// match the decoded sample data. Returned only when [WithMD5Check]
	// is enabled.
	ErrMD5Mismatch = errors.New("flac: MD5 mismatch")

	// ErrEncoderVerify indicates the encoder's self-verification path
	// disagreed with the input. Returned only when [WithVerify] is
	// enabled.
	ErrEncoderVerify = errors.New("flac: encoder verification failed")

	// ErrAllocFail indicates an allocation failure in the C library.
	ErrAllocFail = errors.New("flac: allocation failed")

	// ErrInternal indicates an unexpected internal codec error.
	ErrInternal = errors.New("flac: internal error")

	// ErrClosed indicates the Decoder or Encoder has already been closed.
	ErrClosed = errors.New("flac: already closed")
)

// errInitWithStatus wraps ErrInternal with the libFLAC init-status
// string for diagnostics. Used by the cgo init paths so callers see
// something more actionable than "internal error".
func errInitWithStatus(status string) error {
	return &initError{status: status}
}

type initError struct{ status string }

func (e *initError) Error() string { return "flac: init failed: " + e.status }
func (e *initError) Unwrap() error { return ErrInternal }
