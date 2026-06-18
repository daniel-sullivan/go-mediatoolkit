package mp3

import "errors"

var (
	// ErrBadArg indicates a nil reader/writer or otherwise unusable
	// argument.
	ErrBadArg = errors.New("mp3: bad argument")

	// ErrFormatMismatch indicates the [mutations.Audio] passed to
	// [codec.Encoder.Write] disagrees with the SampleRate or Channels the
	// encoder was constructed with.
	ErrFormatMismatch = errors.New("mp3: audio format does not match encoder")
)
