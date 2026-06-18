//go:build !flac_strict

package nativeflac

import "math"

// Default-mode float64 helpers for the LPC Levinson-Durbin recursion.
//
// The production build inlines the plain float64 operators and lets the
// backend fuse multiply-adds (FMADD/FNMSUB) where it can; f64fma uses a
// genuine fused math.FMA. This build is NOT a bit-exact parity target —
// the strict build (lpc_fp_strict.go) is what the parity suite asserts
// against the -ffp-contract=off oracle.

func f64mul(a, b float64) float64 { return a * b }
func f64add(a, b float64) float64 { return a + b }
func f64sub(a, b float64) float64 { return a - b }

// f64fma computes a fused multiply-add (single rounding).
func f64fma(a, b, c float64) float64 { return math.FMA(a, b, c) }
