//go:build !cgo || !aec_oracle

// Package ec3 is the top-level integration parity slice for
// aec/internal/aec3.EchoCanceller3 against the fetched AEC3 C++
// oracle's real webrtc::EchoCanceller3. This build (no cgo, or the
// aec_oracle tag not set) has neither the oracle fetched nor cgo
// enabled, so the actual parity tests in cgo.go/parity_test.go are
// excluded entirely at the Go build-constraint level -- see cgo.go's
// doc comment for the full rationale (mirrors every other slice under
// aec/internal/parity_tests).
package ec3

import "testing"

// TestEc3ParitySkipped documents why the real parity tests aren't
// present in this build and how to enable them.
func TestEc3ParitySkipped(t *testing.T) {
	t.Skip("AEC3 top-level EchoCanceller3 parity tests require cgo and the fetched+built C++ oracle: " +
		"run `mise run //aec:oracle:fetch` then `mise run //aec:parity` " +
		"(see aec/oracle/VERSION for what gets fetched/built)")
}
