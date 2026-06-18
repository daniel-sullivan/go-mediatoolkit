package timeline

import "errors"

var (
	ErrNilSource       = errors.New("timeline: cue source is nil")
	ErrNegativeStart   = errors.New("timeline: cue start is negative")
	ErrTimelineClosed  = errors.New("timeline: timeline is closed")
	ErrBadSampleRate   = errors.New("timeline: sample rate must be positive")
	ErrBadChannels     = errors.New("timeline: channel count must be positive")
	ErrFormatMismatch  = errors.New("timeline: cue source format does not match timeline")
	ErrPartialFrame    = errors.New("timeline: buffer length is not a whole number of frames")
	ErrSeekOutOfRange  = errors.New("timeline: seek target is outside available range")
	ErrUnboundedSource = errors.New("timeline: append requires a source with finite duration")
	ErrNotSeekable     = errors.New("timeline: cue at seek target is not seekable")
)
