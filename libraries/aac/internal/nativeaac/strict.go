// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk && aac_strict

package nativeaac

// StrictMode is true in the parity build. The fixed-point AAC kernels are
// exact-integer in both builds, so this flag does not switch arithmetic; it
// un-skips the in-package integer-parity assertions and the strict-gated unit
// tests (`if !nativeaac.StrictMode { t.Skip(...) }`).
const StrictMode = true
