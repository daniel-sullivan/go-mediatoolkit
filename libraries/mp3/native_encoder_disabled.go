//go:build !mp3lame

package mp3

import "io"

// newNativeEncoder is the default (non-mp3lame) stub for the pure-Go encoder
// seam. The pure-Go encoder is a 1:1 translation of LAME (LGPL), so it is
// compiled only under the mp3lame build tag (native_encoder.go). Without that
// tag both NewEncoder and NewNativeEncoder return ErrEncoderRequiresLAME and no
// LGPL code is linked. This file carries no LAME-derived code.
func newNativeEncoder(io.Writer, StreamInfo, encoderConfig) (Encoder, error) {
	return nil, ErrEncoderRequiresLAME
}
