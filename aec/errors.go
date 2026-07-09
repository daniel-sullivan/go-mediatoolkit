package aec

import "errors"

// Sentinel errors for the aec package, matching the naming convention
// every other package in this repo uses ("aec: " prefix, e.g.
// libraries/flac's ErrBadArg/ErrBadSampleRate).
var (
	// ErrBadArg indicates an invalid argument to a constructor or
	// method: an unsupported sample rate/channel count, or a samples
	// slice whose length doesn't match the configured channel count.
	ErrBadArg = errors.New("aec: bad argument")

	// ErrClosed is reserved for a future Close/lifecycle method; nothing
	// in the current public API returns it (Canceller has no Close, so
	// there is nothing to be "already closed"). Kept defined rather
	// than removed so the "aec: " sentinel-error convention doesn't
	// need to be re-established from scratch if that need arises.
	ErrClosed = errors.New("aec: already closed")
)
