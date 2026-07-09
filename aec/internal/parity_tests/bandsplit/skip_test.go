//go:build !cgo || !aec_oracle

// Package bandsplit is the bit-exact parity slice for
// aec/internal/aec3's bandsplit.go against the fetched AEC3 C++
// oracle. This build (no cgo, or the aec_oracle tag not set) excludes
// the real tests at the Go build-constraint level — see the smoke
// slice's cgo.go doc comment for the full rationale.
package bandsplit

import "testing"

// TestBandsplitParitySkipped documents why the real parity tests
// aren't present in this build and how to enable them.
func TestBandsplitParitySkipped(t *testing.T) {
	t.Skip("AEC3 band-split parity tests require cgo and the fetched+built C++ oracle: " +
		"run `mise run //aec:oracle:fetch` then `mise run //aec:parity` " +
		"(see aec/oracle/VERSION for what gets fetched/built)")
}
