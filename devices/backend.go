package devices

import "context"

// Backend is the platform-specific seam that produces device state and
// change events. It is deliberately narrow: implementations ask the OS
// and return; all caching, diffing, and polling-fallback logic lives in
// System.
//
// Implementations are only constructed through the package-private
// newPlatformBackend function and are not exported.
type Backend interface {
	// List returns the current set of devices visible to the OS.
	List(ctx context.Context) ([]Device, error)

	// Watch returns a channel that delivers native change events from
	// the OS. The channel is closed when ctx is cancelled.
	//
	// Backends that cannot deliver native events return ErrNotSupported;
	// System then falls back to polling List.
	Watch(ctx context.Context) (<-chan Event, error)

	// OpenOutput opens a playback stream on dev. The stream is returned
	// in the idle state; callers invoke Stream.Start to begin playback.
	OpenOutput(dev Device, format StreamFormat, cb OutputCallback) (Stream, error)

	// OpenInput opens a capture stream on dev. Same lifecycle rules as
	// OpenOutput.
	OpenInput(dev Device, format StreamFormat, cb InputCallback) (Stream, error)

	// Close releases any OS resources held by the backend.
	Close() error
}
