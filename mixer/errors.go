package mixer

import "errors"

var (
	ErrBadSampleRate       = errors.New("mixer: sample rate must be positive")
	ErrBadChannels         = errors.New("mixer: channel count must be positive")
	ErrNilSource           = errors.New("mixer: source is nil")
	ErrMixerClosed         = errors.New("mixer: mixer is closed")
	ErrUnsupportedChannels = errors.New("mixer: channel conversion unsupported (only mono↔stereo)")
	ErrNilProcessor        = errors.New("mixer: Processors contains a nil element")
)
