//go:build !aacfdk

package aac

// newDecoder is the tag-routed constructor for [NewDecoder] in the default
// build (the aacfdk tag is absent). FDK-AAC is the only AAC engine and it
// is fenced behind the opt-in aacfdk build tag, so the default build links
// zero FDK-AAC and surfaces [ErrEngineRequiresFDK] at use. Build with
// `-tags aacfdk` (cgo enabled) to select the vendored Fraunhofer FDK-AAC
// backend in decoder_cgo.go.
//
// Callers that want the pure-Go port regardless of tags use
// [NewNativeDecoder], which routes to newNativeDecoder unconditionally.
func newDecoder(asc AudioSpecificConfig, cfg decoderConfig) (Decoder, error) {
	return nil, ErrEngineRequiresFDK
}
