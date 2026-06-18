//go:build !cgo

package flac

import "io"

func newDecoder(r io.Reader, cfg decoderConfig) (Decoder, error) {
	return newNativeDecoder(r, cfg)
}
