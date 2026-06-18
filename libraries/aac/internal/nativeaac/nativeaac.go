// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Package nativeaac is the pure-Go 1:1 port of the vendored Fraunhofer
// FDK-AAC reference (libraries/aac/libfdk), used by
// [go-mediatoolkit/libraries/aac] when cgo is disabled or when the native
// path is requested explicitly via NewNativeDecoder / NewNativeEncoder.
//
// FDK-AAC is the only AAC engine, so this entire port is derived from it and
// is fenced behind the opt-in aacfdk build tag: a default `go build ./...`
// links none of it. Each ported function carries a doc comment referencing
// its C counterpart as `file:line`, organised by feature, interface-first —
// do not "improve" the algorithm; the parity gate compares output against the
// vendored C oracle.
//
// # Integer parity convention
//
// FDK-AAC is a FIXED-POINT codec: both AAC-LC decode and the ported encoder
// kernels operate exclusively on int32 Q-format fixed-point values (FIXP_SGL /
// FIXP_DBL), never on floats. The whole pipeline — the bit-unpacking, the
// Huffman/spectrum decode, inverse quantization, the TNS/stereo tools, the
// fixed-point FFT/DCT/MDCT filterbank with its per-stage scale headroom
// (CShift), and the encoder quantizer/rate-control and bitstream syntax — is
// integer arithmetic. The reproducibility contract is therefore EXACT integer
// equality (and, for the encode path, a byte-identical bitstream).
//
// AAC is thus pure fixed-point: there is no floating-point arithmetic, no FP
// variant of any kernel, and no ULP/FMA-fusion concern to manage anywhere in
// the port. Do not introduce float shims, FMA-free helpers, or
// *_fp_strict/*_fp_default splits here — those exist for the FP codecs and
// have no analogue in AAC.
//
// The cgo parity oracle still compiles with `-O2 -ffp-contract=off
// -fno-vectorize -fno-slp-vectorize -fno-unroll-loops` (set via CGO_CFLAGS in
// the mise tasks, since Go's cgo flag allowlist rejects `-ffp-contract=off`);
// for these integer kernels that flag set is belt-and-suspenders, not a
// correctness requirement.
//
// # The aac_strict build tag
//
// aac_strict flips the [StrictMode] constant to true. It does NOT select a
// different arithmetic build (there is no FP path to make deterministic, and
// the integer kernels produce bit-identical output in either build); instead
// it un-skips the in-package integer-parity assertions and the strict-gated
// unit tests, which guard with `if !nativeaac.StrictMode { t.Skip(...) }` so a
// bare `go test` stays clean while the strict run asserts exact equality
// against the oracle. The real gate is
// `MISE_EXPERIMENTAL=1 mise run //libraries/aac:parity` (built with
// `-tags 'aac_strict aacfdk'`), which compiles and runs the cgo parity slices
// against the vendored FDK-AAC.
package nativeaac
