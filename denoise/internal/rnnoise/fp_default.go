//go:build !rnnoise_strict

package rnnoise

// Default (non-parity) floating-point composites. These are plain
// expressions with no inlining barrier, so the Go compiler is free to
// contract the multiply into the add/sub (a single fused FMADD/FMSUB on
// arm64), which is faster but rounds once instead of twice. This build
// is therefore NOT a bit-exact parity target; the rnnoise_strict build
// (fp_strict.go) is the one the parity slices compare against the
// -ffp-contract=off C oracle. Same convention as the aec3, opus, and
// flac ports.

func mul32(a, b float32) float32 { return a * b }

func add32(a, b float32) float32 { return a + b }

func sub32(a, b float32) float32 { return a - b }

// mla returns a + b*c.
func mla(a, b, c float32) float32 { return a + b*c }

// muladd returns a*b + c*d.
func muladd(a, b, c, d float32) float32 { return a*b + c*d }

// mulsub returns a*b - c*d.
func mulsub(a, b, c, d float32) float32 { return a*b - c*d }

// Float64 strict primitives (default build: plain, fusible).

func mul64(a, b float64) float64 { return a * b }

func add64(a, b float64) float64 { return a + b }

func sub64(a, b float64) float64 { return a - b }

// mulsub64 returns a*b - c*d in float64.
func mulsub64(a, b, c, d float64) float64 { return a*b - c*d }
