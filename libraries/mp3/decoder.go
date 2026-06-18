//go:build !cgo

package mp3

import "io"

// newDecoder routes to the pure-Go port when cgo is unavailable. The
// cgo-backed minimp3 path lives in decoder_cgo.go (//go:build cgo).
func newDecoder(r io.Reader, cfg decoderConfig) (Decoder, error) {
	return newNativeDecoder(r, cfg)
}
