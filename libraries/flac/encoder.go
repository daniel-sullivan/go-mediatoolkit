//go:build !cgo

package flac

import "io"

func newEncoder(w io.Writer, info StreamInfo, cfg encoderConfig) (Encoder, error) {
	return newNativeEncoder(w, info, cfg)
}
