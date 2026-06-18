package devices

import "errors"

var (
	// ErrBackendUnavailable is returned by GetSystem when no audio-device
	// backend has been implemented for the current platform.
	ErrBackendUnavailable = errors.New("devices: no backend available for this platform")

	// ErrNotSupported is returned by a Backend's Watch method when the
	// backend cannot deliver native events; the System falls back to
	// polling List on an interval.
	ErrNotSupported = errors.New("devices: operation not supported by backend")

	// ErrWrongDirection is returned by OpenOutput/OpenInput when the
	// passed Device's Direction does not match the call.
	ErrWrongDirection = errors.New("devices: device direction does not match call")

	// ErrNilCallback is returned when OpenOutput or OpenInput is called
	// without a callback.
	ErrNilCallback = errors.New("devices: callback must not be nil")

	// ErrDeviceNotFound is returned by OpenOutput/OpenInput when the
	// platform no longer reports a device with the requested ID.
	ErrDeviceNotFound = errors.New("devices: device not found")

	// ErrInvalidFormat is returned when the requested StreamFormat is
	// not usable (e.g. zero channels or sample rate the backend rejects).
	ErrInvalidFormat = errors.New("devices: invalid stream format")
)
