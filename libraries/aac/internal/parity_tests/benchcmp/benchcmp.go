// Package benchcmp benchmarks the pure-Go nativeaac encoder/decoder against the
// vendored Fraunhofer FDK-AAC C reference (compiled inline via cgo), mirroring
// the libraries/opus and libraries/flac benchcmp suites.
//
// The benchmark files (bench_test.go) and the cgo FDK reference (cgo_fdk.go and
// the fdk_tu_*.cpp wrappers) are gated behind `//go:build cgo && aacfdk` — the
// same tag the rest of the AAC engine uses — so a default `CGO_ENABLED=0 go
// build` / `go vet` of the tree sees only this (untagged) package doc and links
// zero FDK-AAC. Run the benchmarks with the tag:
//
//	go test -tags 'cgo aacfdk' -bench . -benchmem ./libraries/aac/internal/parity_tests/benchcmp/
//
// The cgo column is the production-style fdk encoder (TRANSMUX 0 raw AAC-LC,
// afterburner on, -O2) and the production fdk decoder with the PCM peak limiter
// disabled (so the output is the bare fixed-point decode chain the pure-Go port
// mirrors). It is therefore a native-vs-production-C comparison.
//
// The native column drives internal/nativeaac directly rather than importing
// libraries/aac: under -tags aacfdk that package compiles its own copy of the
// FDK C, which would duplicate-symbol-clash with the FDK reference TUs this
// package links. Driving nativeaac directly is the same amalgamation-split the
// flac benchcmp suite and the decode-e2e / encode-e2e parity slices document.
package benchcmp
