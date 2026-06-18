package mp3

import "errors"

var (
	// ErrNotMP3 indicates the stream does not begin with a recognisable
	// MP3 frame sync or a leading ID3v2 tag.
	ErrNotMP3 = errors.New("mp3: not an MP3 stream")

	// ErrInvalidID3 indicates an ID3v2 or ID3v1 tag was malformed —
	// truncated body, bad synchsafe size, unsupported version, etc.
	ErrInvalidID3 = errors.New("mp3: invalid ID3 tag")

	// ErrUnsupportedFormat indicates the Header passed to NewWriter does
	// not describe a supportable MP3 stream (zero sample rate,
	// out-of-range channel count, etc.). The underlying validation is in
	// libraries/mp3.
	ErrUnsupportedFormat = errors.New("mp3: unsupported format")

	// ErrAlreadyClosed indicates the Writer has already been closed.
	ErrAlreadyClosed = errors.New("mp3: writer already closed")

	// ErrBadArg indicates an invalid argument was passed, such as a nil
	// destination writer.
	ErrBadArg = errors.New("mp3: invalid argument")
)
