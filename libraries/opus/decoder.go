//go:build !cgo

package opus

func newDecoder(sampleRate, channels int, cfg decoderConfig) (Decoder, error) {
	return newNativeDecoder(sampleRate, channels, cfg)
}
