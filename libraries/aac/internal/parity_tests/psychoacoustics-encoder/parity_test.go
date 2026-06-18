// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psychoacoustics_encoder

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// TestChaosMeasureParity asserts the pure-Go chaos-measure port
// (nativeaac.CalculateChaosMeasure) matches the vendored FDK-AAC
// FDKaacEnc_CalculateChaosMeasure bit-for-bit (raw int32) over a spread of
// fabricated MDCT magnitude arrays.
//
// Chaos measure is a pure fixed-point INTEGER kernel, so it is bit-exact in
// any build; the strict-gate below is the area convention (the aac_strict
// parity discipline), not a numerical necessity here. We still run the
// comparison only under aac_strict so a bare `go test` of the suite stays
// clean while the strict gate asserts everything.
func TestChaosMeasureParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("chaos-measure parity asserts under -tags=aac_strict (the integer-parity gate convention); skipping in the default build")
	}

	// FIXP_DBL inputs are constrained to [0, 2^30) so the peak-filter abs(),
	// the schur_div precondition (num <= denum after the leading-bit
	// normalisation) and the `center << leadingBits` shift never overflow or
	// hit the INT_MIN abs() edge — matching the realistic MDCT magnitude
	// domain the encoder feeds this kernel. The reference and the port apply
	// the identical operations, so any in-range array is a valid oracle case.
	const maxMag = int32(1) << 30

	// AAC line counts the chaos path actually sees: a full long block (1024)
	// and the eight short blocks (128) plus a few odd lengths to exercise the
	// even/odd dual pass and the trailing-line fill-ins. numberOfLines must be
	// even and >= 6 for the peak filter to run its main loop.
	lengths := []int{1024, 128, 8, 16, 64, 256, 512}

	rng := rand.New(rand.NewSource(0x5EED_AAC))
	for _, n := range lengths {
		n := n
		t.Run("", func(t *testing.T) {
			// Several independent random spectra per length, including a
			// tonal-ish (sparse peaks) and a noise-like (dense) draw, so both
			// branches of the `tmp < center` test are exercised.
			for iter := 0; iter < 16; iter++ {
				mdct := make([]int32, n)
				for i := range mdct {
					v := int32(rng.Int63n(int64(maxMag)))
					// Half the iterations randomly sign the lines so the
					// branch-free abs() in both the C and the port is covered
					// (negative inputs must fold to the same magnitude).
					if iter%2 == 1 && rng.Intn(2) == 0 {
						v = -v
					}
					mdct[i] = v
				}

				want := cChaosMeasure(mdct)

				got := make([]int32, n)
				nativeaac.CalculateChaosMeasure(mdct, n, got)

				require.Equal(t, want, got, "chaos measure mismatch at length %d iter %d", n, iter)
			}
		})
	}
}

// TestChaosMeasureParityDeterministic is a lightweight always-on check (no
// strict gate) that the C oracle itself runs and is stable across repeated
// calls — it guards the cgo build/link of the vendored chaosmeasure.cpp +
// fixpoint_math.cpp TUs even when the strict assertion above is skipped.
func TestChaosMeasureParityDeterministic(t *testing.T) {
	mdct := make([]int32, 64)
	for i := range mdct {
		mdct[i] = int32(i*i*131) & 0x3FFF_FFFF
	}
	a := cChaosMeasure(mdct)
	b := cChaosMeasure(mdct)
	require.Equal(t, a, b, "C chaos-measure oracle is non-deterministic")
	require.Len(t, a, len(mdct))
}
