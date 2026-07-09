//go:build !cgo || !aec_oracle

// Package erle is the standalone bit-exact parity slice for
// aec/internal/aec3's ErleEstimator (and its constituents
// FullBandErleEstimator, SubbandErleEstimator and
// SignalDependentErleEstimator) against the fetched AEC3 C++ oracle.
// This build (no cgo, or the aec_oracle tag not set) excludes the
// real tests at the Go build-constraint level -- see the subtractor
// slice's skip_test.go doc comment for the full rationale.
package erle

import "testing"

// TestErleParitySkipped documents why the real parity tests aren't
// present in this build and how to enable them.
func TestErleParitySkipped(t *testing.T) {
	t.Skip("AEC3 erle parity tests require cgo and the fetched+built C++ oracle: " +
		"run `mise run //aec:oracle:fetch` then `mise run //aec:parity` " +
		"(see aec/oracle/VERSION for what gets fetched/built)")
}
