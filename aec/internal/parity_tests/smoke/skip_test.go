//go:build !cgo || !aec_oracle

// Package smoke is a Phase-A parity slice proving the fetched AEC3
// C++ oracle links, runs, and produces deterministic, substantially
// echo-reduced output. This build (no cgo, or the aec_oracle tag not
// set) has neither the oracle fetched nor cgo enabled, so the actual
// smoke test in cgo.go/parity_test.go is excluded entirely at the Go
// build-constraint level (a missing C++ library/headers fails at
// compile/link time, not something a runtime t.Skip could catch) —
// see cgo.go's doc comment for the full rationale.
package smoke

import "testing"

// TestSmokeSkipped documents why the real smoke test isn't present in
// this build and how to enable it.
func TestSmokeSkipped(t *testing.T) {
	t.Skip("AEC3 oracle smoke parity test requires cgo and the fetched+built C++ oracle: " +
		"run `mise run //aec:oracle:fetch` then `mise run //aec:parity` " +
		"(see aec/oracle/VERSION for what gets fetched/built)")
}
