//go:build flac_strict

package nativeflac

// StrictMode is true in the parity build. Code that has a fast path
// gated on absence of `flac_strict` (e.g., compile-time SIMD or FMA
// fusing) can branch on this constant — but prefer file-level build
// tags for code that compiles to entirely different instructions.
const StrictMode = true
