//go:build !cgo

package opus

func newEncoder(sampleRate int, channels int, cfg encoderConfig) (Encoder, error) {
	return newNativeEncoder(sampleRate, channels, cfg)
}
