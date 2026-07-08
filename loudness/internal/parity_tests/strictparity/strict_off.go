//go:build !loudness_strict

package strictparity

// Enabled reports whether the parity slices should assert against the
// bit-exact (strict) bound rather than the widened tolerance. See the
// package doc (strict_on.go).
const Enabled = false
