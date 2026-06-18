//go:build !aacfdk

package aac

// newEncoder is the tag-routed constructor for [NewEncoder] in the default
// build (the aacfdk tag is absent). FDK-AAC is the only AAC engine and it
// is fenced behind the opt-in aacfdk build tag, so the default build links
// zero FDK-AAC and surfaces [ErrEngineRequiresFDK] at use. Build with
// `-tags aacfdk` (cgo enabled) to select the vendored Fraunhofer FDK-AAC
// backend in encoder_cgo.go.
//
// Callers that want the pure-Go port regardless of tags use
// [NewNativeEncoder], which routes to newNativeEncoder unconditionally.
func newEncoder(sampleRate, channels int, cfg encoderConfig) (Encoder, error) {
	return nil, ErrEngineRequiresFDK
}
