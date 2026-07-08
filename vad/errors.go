package vad

import "errors"

// Sentinel errors returned by the vad package. All messages are
// prefixed "vad: " per repo convention. Constructors return these
// wrapped in nothing — compare with errors.Is or direct equality.
var (
	// ErrBadSampleRate is returned when a configuration's SampleRate
	// is below the engine's minimum (Energy: 100 Hz — below that a
	// 10 ms decision frame holds less than one sample; WebRTC/Silero:
	// 8000 Hz).
	ErrBadSampleRate = errors.New("vad: sample rate too low")

	// ErrBadChannels is returned when a channel count is not in 1..64.
	ErrBadChannels = errors.New("vad: channel count must be in 1..64")

	// ErrBadConfig is returned for configuration problems not covered
	// by a more specific sentinel: negative durations, NaN dB values,
	// a positive gate floor or ducking depth, or a Detector whose
	// sample rate/channel count doesn't match the Gate/Ducker it is
	// injected into.
	ErrBadConfig = errors.New("vad: invalid configuration")

	// ErrBadMode is returned when a WebRTC aggressiveness mode is
	// outside 0..3.
	ErrBadMode = errors.New("vad: mode must be in 0..3")

	// ErrBadFrameDuration is returned when a WebRTC frame duration is
	// not one of 10, 20, or 30 ms.
	ErrBadFrameDuration = errors.New("vad: frame duration must be 10, 20, or 30 ms")

	// ErrBadThreshold is returned when a detection threshold (or
	// threshold pair) is out of range: NaN/infinite values, or an
	// on/off pair whose off threshold is not strictly below its on
	// threshold (the hysteresis band would be empty or inverted).
	ErrBadThreshold = errors.New("vad: invalid detection threshold")

	// ErrNilDetector is returned when a GateConfig/DuckerConfig has a
	// nil Detector — both processors are meaningless without one.
	ErrNilDetector = errors.New("vad: detector must not be nil")
)
