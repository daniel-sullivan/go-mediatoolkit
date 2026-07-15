//go:build rnnoise_strict

package rnnoise

// Strict floating-point composites for bit-exact parity against the C
// oracle, which is compiled with -ffp-contract=off (see
// libraries/rnnoise/mise.toml). The Go spec allows the compiler to fuse
// `a + b*c` into a single-rounding FMA (and the arm64 backend does),
// which diverges from the oracle's separately-rounded multiply and add
// by 1 ulp. Routing the multiply and the add/sub through //go:noinline
// primitives forces a rounding boundary between them, exactly like the
// aec3, opus, and flac ports.
//
// Cross-statement discipline (project_fp_cross_statement_fma): Go may
// also contract a raw product computed in an earlier statement into a
// later +/-. Any raw float product that later feeds an add/sub must go
// through mul32 (or an explicit float32() barrier), not just the
// composites below.

//go:noinline
func mul32(a, b float32) float32 { return a * b }

//go:noinline
func add32(a, b float32) float32 { return a + b }

//go:noinline
func sub32(a, b float32) float32 { return a - b }

// mla returns a + b*c, separately rounded.
func mla(a, b, c float32) float32 { return add32(a, mul32(b, c)) }

// muladd returns a*b + c*d, separately rounded.
func muladd(a, b, c, d float32) float32 { return add32(mul32(a, b), mul32(c, d)) }

// mulsub returns a*b - c*d, separately rounded.
func mulsub(a, b, c, d float32) float32 { return sub32(mul32(a, b), mul32(c, d)) }

// Float64 strict primitives. RNNoise's rnn_biquad accumulates in double
// (`b[0]*(double)xi - a[0]*(double)yi`) while storing float32 state; the
// oracle's -ffp-contract=off applies to double too, so the two double
// multiplies must not fuse into an FMSUB. Same //go:noinline barrier.

//go:noinline
func mul64(a, b float64) float64 { return a * b }

//go:noinline
func add64(a, b float64) float64 { return a + b }

//go:noinline
func sub64(a, b float64) float64 { return a - b }

// mulsub64 returns a*b - c*d in float64, separately rounded.
func mulsub64(a, b, c, d float64) float64 { return sub64(mul64(a, b), mul64(c, d)) }
