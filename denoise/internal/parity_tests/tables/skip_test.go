//go:build !cgo || !rnnoise_strict

package tables

import "testing"

// TestParitySkipped documents why the RNNoise parity slice is absent from
// this build (no cgo, or the rnnoise_strict tag not set) and how to enable
// it. The cgo oracle compiles the vendored C on vec.h's generic branch
// (-DDISABLE_NEON, -ffp-contract=off) and, for the network/e2e slices,
// needs the fetched rnnoise_data.c — all wired by the mise task below.
func TestParitySkipped(t *testing.T) {
	t.Skip("RNNoise parity requires cgo and -tags=rnnoise_strict: run " +
		"'mise run //libraries/rnnoise:fetch' then 'mise run //libraries/rnnoise:parity'")
}
