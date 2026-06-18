//go:build !mp3lame

package mp3

import "io"

// newEncoder is the default (non-mp3lame) stub. The MP3 encoder — both the cgo
// libmp3lame backend (encoder_cgo.go) and the pure-Go 1:1 LAME port
// (internal/nativemp3, reached via encoder.go) — is a derivative work of LAME
// and is LGPL-licensed, so it is fenced behind the mp3lame build tag. A build
// without that tag links no LGPL code; requesting an encoder returns a clear
// sentinel. Rebuild with -tags mp3lame to enable encoding. This file carries
// no LGPL code and is excluded whenever the encoder is compiled in.
func newEncoder(io.Writer, StreamInfo, encoderConfig) (Encoder, error) {
	return nil, ErrEncoderRequiresLAME
}
