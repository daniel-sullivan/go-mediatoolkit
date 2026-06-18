package codec

import "errors"

var (
	ErrBadChannelCount = errors.New("codec: channel count must be >= 1")
	ErrBadSampleRate   = errors.New("codec: sample rate must be > 0")
)
