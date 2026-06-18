package ogg

import "errors"

var (
	ErrNoStreams       = errors.New("ogg: no logical streams found")
	ErrUnknownStream   = errors.New("ogg: serial number not found")
	ErrAlreadyClosed   = errors.New("ogg: writer already closed")
	ErrNoOpusStream    = errors.New("ogg: no Opus stream found")
	ErrBadOpusHead     = errors.New("ogg: OpusHead header is malformed")
	ErrBadOpusTags     = errors.New("ogg: OpusTags header is malformed")
	ErrNoFLACStream    = errors.New("ogg: no FLAC stream found")
	ErrBadFLACHead     = errors.New("ogg: Ogg-FLAC mapping header is malformed")
	ErrBadFLACMetadata = errors.New("ogg: Ogg-FLAC metadata block is malformed")
)
