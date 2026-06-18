// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build !cgo && mp3lame

package mp3

import "io"

// newEncoder routes to the pure-Go LAME-derived port when cgo is unavailable
// but the mp3lame tag is set. The cgo-backed libmp3lame path lives in
// encoder_cgo.go (//go:build cgo && mp3lame); the !mp3lame default returns
// ErrEncoderRequiresLAME from encoder_disabled.go.
func newEncoder(w io.Writer, info StreamInfo, cfg encoderConfig) (Encoder, error) {
	return newNativeEncoder(w, info, cfg)
}
