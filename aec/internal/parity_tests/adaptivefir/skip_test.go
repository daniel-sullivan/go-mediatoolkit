//go:build !cgo || !aec_oracle

// Package adaptivefir is the bit-exact parity slice for
// aec/internal/aec3's adaptive_fir_filter.go (AdaptiveFirFilter,
// scalar path) and adaptive_fir_filter_erl.go (ComputeErl) against the
// fetched AEC3 C++ oracle. This build (no cgo, or the aec_oracle tag
// not set) excludes the real tests at the Go build-constraint level --
// see the smoke slice's cgo.go doc comment for the full rationale.
package adaptivefir

import "testing"

// TestAdaptiveFirFilterParitySkipped documents why the real parity
// tests aren't present in this build and how to enable them.
func TestAdaptiveFirFilterParitySkipped(t *testing.T) {
	t.Skip("AEC3 adaptive-FIR-filter parity tests require cgo and the fetched+built C++ oracle: " +
		"run `mise run //aec:oracle:fetch` then `mise run //aec:parity` " +
		"(see aec/oracle/VERSION for what gets fetched/built)")
}
