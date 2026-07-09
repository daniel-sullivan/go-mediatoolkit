//go:build !aec_strict

package aec3

// Default (non-parity) floating-point composites. These are plain
// expressions with no inlining barrier, so the Go compiler is free to
// contract the multiply into the add/sub (a single fused FMADD/FMSUB
// on arm64), which is faster but rounds once instead of twice. This
// build is therefore NOT a bit-exact parity target; the aec_strict
// build (fp_strict.go) is the one the parity slices compare against
// the -ffp-contract=off C++ oracle. Same convention as the opus
// (fma_default.go/fma_strict.go) and flac (lpc_fp_default.go) ports.

func mul32(a, b float32) float32 { return a * b }

func add32(a, b float32) float32 { return a + b }

func sub32(a, b float32) float32 { return a - b }

// mla returns a + b*c.
func mla(a, b, c float32) float32 { return a + b*c }

// muladd returns a*b + c*d.
func muladd(a, b, c, d float32) float32 { return a*b + c*d }

// mulsub returns a*b - c*d.
func mulsub(a, b, c, d float32) float32 { return a*b - c*d }
