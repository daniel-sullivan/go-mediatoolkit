// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// This file exposes a thin exported wrapper around the unexported TNS synthesis
// lattice kernel (tns_apply.go) so the cgo parity oracle in
// internal/parity_tests/tns-decode can drive it without being in-package. The
// wrapper adds no logic: it forwards 1:1 to clpcSynthesisLatticeDBL. It exists
// solely for the parity harness — the production decode path (CTns_Apply) uses
// the unexported form directly.

// ClpcSynthesisLatticeDBL applies the all-pole TNS synthesis lattice in place
// over signal, the FIXP_DBL coefficient overload (FDK_lpc.cpp:168). signalE /
// signalEOut are the in/out exponents, inc is +1/-1 (forward/backward), coeff
// holds `order` FIXP_DBL lattice reflection coefficients, and state holds
// `order` accumulators the caller must zero (CTns_Apply clears state before
// each filter). Wraps clpcSynthesisLatticeDBL.
func ClpcSynthesisLatticeDBL(signal []int32, signalSize, signalE, signalEOut, inc int, coeff []int32, order int, state []int32) {
	clpcSynthesisLatticeDBL(signal, signalSize, signalE, signalEOut, inc, coeff, order, state)
}
