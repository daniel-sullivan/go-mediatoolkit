//go:build !opus_strict

package nativeopus

// Float32 multiply-add helpers — FMA-fused default variant.
//
// This file is built by default. Each helper is a plain inline
// expression with no //go:noinline barrier, so the Go compiler is free
// to fuse `a + b*c` patterns into a single-rounded FMADDS (arm64) or
// VFMADD (amd64) instruction, matching what clang produces for upstream
// libopus compiled with default `-ffp-contract=on`.
//
// Quality vs the bit-exact `opus_strict` build: PSNR 117 dB+ on int16
// output, 152 dB+ on int24 — the same level of drift that exists
// between different C toolchain builds (see the 4-way PSNR matrix in
// the README). Inaudible by a wide margin.

func mul_f32(a, b float32) float32     { return a * b }
func add_f32(a, b float32) float32     { return a + b }
func sub_f32(a, b float32) float32     { return a - b }
func neg_mul_f32(a, b float32) float32 { return -(a * b) }
func fma_add(a, b, c float32) float32  { return a + b*c }
func fma_sub(a, b, c float32) float32  { return a - b*c }
func fma_rsub(a, b, c float32) float32 { return b*c - a }
func fneg_mul(a, b float32) float32    { return -(a * b) }

func mul_f64(a, b float64) float64      { return a * b }
func add_f64(a, b float64) float64      { return a + b }
func sub_f64(a, b float64) float64      { return a - b }
func fma_add64(a, b, c float64) float64 { return a + b*c }
func fma_sub64(a, b, c float64) float64 { return a - b*c }
