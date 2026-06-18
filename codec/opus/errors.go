package opus

import "errors"

var (
	ErrBadSampleRate  = errors.New("opus: unsupported sample rate; must be 8000, 12000, 16000, 24000, or 48000")
	ErrBadChannels    = errors.New("opus: channels must be 1 or 2")
	ErrFormatMismatch = errors.New("opus: audio format does not match encoder")
)
