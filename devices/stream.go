package devices

import "errors"

// StreamFormat describes the sample format of an audio stream. All three
// fields are caller hints: backends that cannot honour a field leave the
// accepted value visible on Stream.Format after Open returns.
//
// SampleRate is in Hz. Channels is the interleave count (1 = mono,
// 2 = stereo). Frames is the requested number of sample frames per
// callback; a frame is one sample per channel, so a callback buffer
// holds Frames*Channels float64 values. Pass 0 for Frames to accept the
// backend default — typically a handful of milliseconds.
type StreamFormat struct {
	SampleRate int
	Channels   int
	Frames     int
}

// OutputCallback fills buf with interleaved float64 samples in [-1, 1].
// len(buf) is always Format.Frames * Format.Channels for the owning
// stream. The callback is invoked on a backend-owned goroutine or a
// platform realtime thread; it must not allocate, take locks the caller
// also takes on other goroutines, or perform I/O. Unfilled samples must
// be explicitly zeroed — the backend passes the buffer through without
// clearing.
type OutputCallback func(buf []float64)

// InputCallback receives interleaved float64 samples in [-1, 1]. Same
// buffer-size and realtime rules apply as for OutputCallback. The buffer
// is reused across invocations; copy out anything that must outlive the
// call.
type InputCallback func(buf []float64)

// Stream is one in-flight playback or capture session. Open returns a
// stream in the idle state; Start begins driving the callback, Stop
// halts it, Close releases resources. Close is safe to call on a
// running stream — it implies Stop.
//
// A Stream is not safe for concurrent use. Start/Stop/Close from a
// single goroutine only.
type Stream interface {
	// Start asks the backend to begin calling the registered callback.
	// Idempotent while running; returns ErrStreamClosed after Close.
	Start() error

	// Stop halts callback delivery. Idempotent while stopped.
	Stop() error

	// Close stops the stream (if running) and releases backend state.
	// Calling Close more than once returns nil.
	Close() error

	// Format reports the format the backend actually negotiated, which
	// may differ from what was requested at Open time.
	Format() StreamFormat
}

// ErrStreamClosed is returned when a caller operates on a Stream that
// has been closed.
var ErrStreamClosed = errors.New("devices: stream closed")

// OpenOutput opens a playback stream targeting dev. cb is invoked on a
// backend goroutine or realtime thread to produce samples. The caller
// must call Start to begin playback and Close to release resources.
//
// dev must describe an Output device previously obtained from List or
// Snapshot; passing a stale device whose ID the OS has since removed
// returns an error without side effects.
func (s *System) OpenOutput(dev Device, format StreamFormat, cb OutputCallback) (Stream, error) {
	if dev.Direction != Output {
		return nil, ErrWrongDirection
	}
	if cb == nil {
		return nil, ErrNilCallback
	}
	return s.backend.OpenOutput(dev, format, cb)
}

// OpenInput opens a capture stream from dev. See OpenOutput for lifetime
// and callback rules; cb receives captured samples instead of producing
// them.
func (s *System) OpenInput(dev Device, format StreamFormat, cb InputCallback) (Stream, error) {
	if dev.Direction != Input {
		return nil, ErrWrongDirection
	}
	if cb == nil {
		return nil, ErrNilCallback
	}
	return s.backend.OpenInput(dev, format, cb)
}
