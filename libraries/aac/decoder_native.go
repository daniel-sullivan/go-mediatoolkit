// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk && !cgo

package aac

// newDecoder is the tag-routed constructor for [NewDecoder] in the
// aacfdk-without-cgo build. The vendored Fraunhofer FDK-AAC backend
// (decoder_cgo.go) needs cgo to link the C reference; with cgo disabled but
// the aacfdk fence opted in, the only available AAC engine is the pure-Go
// 1:1 port under internal/nativeaac, so [NewDecoder] routes there. This is
// the seam the CGO_ENABLED=0 -tags aacfdk decode-parity gate exercises.
//
// The default build (no aacfdk tag) instead surfaces [ErrEngineRequiresFDK]
// from decoder.go; the cgo+aacfdk build uses the C backend in decoder_cgo.go.
func newDecoder(asc AudioSpecificConfig, cfg decoderConfig) (Decoder, error) {
	return newNativeDecoder(asc, cfg)
}
