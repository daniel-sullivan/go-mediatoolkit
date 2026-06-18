// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk && !cgo

package aac

// newEncoder is the tag-routed constructor for [NewEncoder] in the
// aacfdk-without-cgo build. The vendored Fraunhofer FDK-AAC backend
// (encoder_cgo.go) needs cgo to link the C reference; with cgo disabled but
// the aacfdk fence opted in, the only available AAC engine is the pure-Go
// 1:1 port under internal/nativeaac, so [NewEncoder] routes there.
//
// The default build (no aacfdk tag) instead surfaces [ErrEngineRequiresFDK]
// from encoder.go; the cgo+aacfdk build uses the C backend in encoder_cgo.go.
func newEncoder(sampleRate, channels int, cfg encoderConfig) (Encoder, error) {
	return newNativeEncoder(sampleRate, channels, cfg)
}
