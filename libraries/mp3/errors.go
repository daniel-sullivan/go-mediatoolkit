package mp3

import "errors"

var (
	// ErrBadArg indicates an invalid argument: nil reader/writer, or a buf
	// whose length is not a multiple of the configured channel count.
	ErrBadArg = errors.New("mp3: bad argument")

	// ErrBadSampleRate indicates a sample rate outside [MinSampleRate,
	// MaxSampleRate].
	ErrBadSampleRate = errors.New("mp3: bad sample rate")

	// ErrBadChannels indicates a channel count outside [1, MaxChannels].
	ErrBadChannels = errors.New("mp3: bad channel count")

	// ErrBadBitRate indicates a bit rate outside [MinBitRate, MaxBitRate].
	ErrBadBitRate = errors.New("mp3: bad bit rate")

	// ErrInvalidStream indicates a corrupted or malformed MP3 stream (bad
	// frame sync, truncated frame, etc.).
	ErrInvalidStream = errors.New("mp3: invalid stream")

	// ErrUnsupportedStream indicates a stream feature the wrapper does not
	// handle (e.g. a free-format frame or an unsupported MPEG layer).
	ErrUnsupportedStream = errors.New("mp3: unsupported stream")

	// ErrNotImplemented indicates a requested feature the active backend does
	// not yet support. Currently this is the pure-Go LAME port's VBR encode
	// path: it implements CBR/ABR, but the VBR iteration loops are not yet
	// ported, so a VBR encode surfaces this rather than mis-encoding. The cgo
	// libmp3lame backend supports VBR and never returns it.
	ErrNotImplemented = errors.New("mp3: not implemented")

	// ErrEncoderRequiresLAME indicates the MP3 encoder was requested from a
	// build that did not opt into the LGPL-licensed LAME-derived encoder. The
	// encoder (both the cgo libmp3lame backend and the pure-Go 1:1 LAME port)
	// is a derivative of LAME and is fenced behind the mp3lame build tag so a
	// default build links no LGPL code. Rebuild with -tags mp3lame to enable
	// it. Decoding (minimp3, CC0) is always available.
	ErrEncoderRequiresLAME = errors.New("mp3: encoder requires the mp3lame build tag (LGPL)")

	// ErrInternal indicates an unexpected internal codec error.
	ErrInternal = errors.New("mp3: internal error")

	// ErrClosed indicates the Decoder or Encoder has already been closed.
	ErrClosed = errors.New("mp3: already closed")
)
