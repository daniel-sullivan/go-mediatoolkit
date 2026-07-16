package duplex

import "errors"

// Sentinel errors returned by the duplex package. All messages are
// prefixed "duplex: " per repo convention. Compare with errors.Is or
// direct equality.
var (
	// ErrBadSampleRate is returned when Config.SampleRate is not a
	// positive multiple of 100 Hz (the engine frames all audio into
	// whole 10 ms frames), or — with AEC enabled — not one of the
	// rates aec.NewCanceller accepts (16000, 32000, 48000).
	ErrBadSampleRate = errors.New("duplex: invalid sample rate")

	// ErrBadChannels is returned when Config.Channels is not in 1..64.
	ErrBadChannels = errors.New("duplex: channel count must be in 1..64")

	// ErrNilDetector is returned when Config.Detector is nil — the
	// capture path is meaningless without one.
	ErrNilDetector = errors.New("duplex: detector must not be nil")

	// ErrDetectorMismatch is returned when Config.Detector was
	// constructed for a different sample rate or channel count than
	// the engine's.
	ErrDetectorMismatch = errors.New("duplex: detector stream format must match the engine")

	// ErrNilProcessor is returned when RenderChain or CaptureChain
	// contains a nil entry.
	ErrNilProcessor = errors.New("duplex: processor chains must not contain nil")

	// ErrBadConfig is returned for configuration problems not covered
	// by a more specific sentinel: negative durations, a negative
	// EventBuffer, or an invalid Ambient (empty, misaligned, shorter
	// than one frame, or negative gain).
	ErrBadConfig = errors.New("duplex: invalid configuration")

	// ErrBadFrame is returned by Push when the frame length is not a
	// whole multiple of Channels.
	ErrBadFrame = errors.New("duplex: frame length must be a whole multiple of Channels")

	// ErrCaptureOverflow is returned by Push when the bounded capture
	// queue is full: the frame is dropped (drop-newest) and counted
	// in Metrics().CaptureFramesDropped. See Push's doc comment for
	// why overload sheds the newest frame instead of blocking.
	ErrCaptureOverflow = errors.New("duplex: capture queue full, frame dropped")

	// ErrEngineStarted is returned by Start when the engine is
	// already running.
	ErrEngineStarted = errors.New("duplex: engine already started")

	// ErrEngineStopped is returned by Start, FeedChunk, and Push once
	// the engine has begun shutting down (Stop, context cancellation,
	// or a stall failure — see Err): an Engine is single-use,
	// construct a new one for a fresh session.
	ErrEngineStopped = errors.New("duplex: engine stopped")

	// ErrEventsStalled is the terminal session error recorded (see
	// Engine.Err) when an event delivery stayed blocked on a full
	// Events channel for Config.StallTimeout: the consumer is
	// considered dead, and failing the session beats freezing the
	// audio loop behind it forever.
	ErrEventsStalled = errors.New("duplex: events consumer stalled")

	// ErrTuningUnsupported is returned by the SetVAD* passthroughs
	// when the configured detector does not expose the corresponding
	// setter. Tune such a detector directly through Detector().
	ErrTuningUnsupported = errors.New("duplex: detector does not support this tuning setter")
)
