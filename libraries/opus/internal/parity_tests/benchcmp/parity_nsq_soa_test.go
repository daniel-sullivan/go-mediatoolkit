//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestNSQDelDecSoA_Roundtrip verifies that the AoS <-> SoA conversion
// helpers in nativeopus are bijective over the lanes [0, nStates), and
// that lanes [nStates, MAX_DEL_DEC_STATES) are zero in the SoA after
// population, even when the destination SoA arrived with nonzero junk.
func TestNSQDelDecSoA_Roundtrip(t *testing.T) {
	const maxLanes = 4

	for _, nStates := range []int{1, 2, 3, 4} {
		for seed := int64(1); seed <= 5; seed++ {
			r := rand.New(rand.NewSource(seed*1000 + int64(nStates)))

			// Build AoS slice of fixed length maxLanes. Entries at
			// index >= nStates stay at their zero value.
			aos := make([]nativeopus.NSQ_del_dec_struct, maxLanes)
			for k := 0; k < nStates; k++ {
				aos[k] = nativeopus.ExportTestNSQDelDecFillRandom(r)
			}

			// Roundtrip 1: fresh SoA via the simple export.
			soa := nativeopus.ExportTestNSQDelDecAoStoSoA(aos, nStates)
			got := nativeopus.ExportTestNSQDelDecSoAtoAoS(soa, nStates)

			for k := 0; k < nStates; k++ {
				if aos[k] != got[k] {
					t.Errorf("NSQ_del_dec_struct roundtrip mismatch: nStates=%d seed=%d lane=%d",
						nStates, seed, k)
				}
			}

			// Verify lanes >= nStates in the fresh SoA are zero.
			for k := nStates; k < maxLanes; k++ {
				if !nativeopus.ExportTestNSQDelDecSoALaneZero(soa, k) {
					t.Errorf("fresh SoA lane %d not zero after AoS->SoA with nStates=%d seed=%d",
						k, nStates, seed)
				}
			}

			// Roundtrip 2: pre-garbage the SoA, then verify that
			// AoStoSoA zeroes lanes >= nStates even with nonzero
			// initial contents.
			garbage := new(nativeopus.NSQDelDecSoA)
			for k := 0; k < maxLanes; k++ {
				// Fill every lane with a distinct nonzero pattern.
				nativeopus.ExportTestNSQDelDecSoAFillLane(garbage, k, int32(0x5A5A0000|(k+1)))
			}
			nativeopus.ExportTestNSQDelDecAoStoSoAInto(garbage, aos, nStates)

			// Live lanes must match the reference AoS.
			got2 := nativeopus.ExportTestNSQDelDecSoAtoAoS(garbage, nStates)
			for k := 0; k < nStates; k++ {
				if aos[k] != got2[k] {
					t.Errorf("pre-garbaged roundtrip mismatch: nStates=%d seed=%d lane=%d",
						nStates, seed, k)
				}
			}

			// Unused lanes must be zeroed by the helper, overriding
			// the pre-existing garbage.
			for k := nStates; k < maxLanes; k++ {
				if !nativeopus.ExportTestNSQDelDecSoALaneZero(garbage, k) {
					t.Errorf("pre-garbaged SoA lane %d not zero after AoS->SoA with nStates=%d seed=%d",
						k, nStates, seed)
				}
			}
		}
	}
}
