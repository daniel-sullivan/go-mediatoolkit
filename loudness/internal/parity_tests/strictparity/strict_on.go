//go:build loudness_strict

// Package strictparity exposes a single build-tag-selected constant,
// Enabled, that every loudness parity slice consults to choose its
// assertion mode. It replaces the per-slice strict_on.go / strict_off.go
// pairs that previously each redeclared an identical, unexported
// `strictParity` const.
//
// Two assertion modes:
//
//   - Enabled == true (this file, built with -tags=loudness_strict, as
//     the //loudness:parity and //loudness:test mise tasks do): the C
//     oracle is compiled with CGO_CFLAGS including -ffp-contract=off
//     (plus the vectorization/unroll-suppressing flags), forcing its
//     arithmetic to scalar, FMA-free code that matches the pure-Go r128
//     port bit-for-bit. Parity assertions on C-arithmetic-derived
//     float64 values are therefore checked BIT-EXACT (or bounded to a
//     handful of documented libm-only ULP exceptions).
//   - Enabled == false (strict_off.go, any build without the tag — e.g. a
//     plain `go test ./...` in CI): the oracle may be built with whatever
//     FMA/vectorization the default toolchain chooses, so the same
//     assertions widen to a relative+absolute tolerance loose enough to
//     absorb that drift.
//
// The build tags carry NO cgo term deliberately: this package imports no
// C, so it compiles in every configuration and the parity slices (which
// DO carry cgo) can import it unconditionally without dragging a second
// ebur128.c compilation into their binary.
package strictparity

// Enabled reports whether the parity slices should assert against the
// bit-exact (strict) bound rather than the widened tolerance. See the
// package doc.
const Enabled = true
