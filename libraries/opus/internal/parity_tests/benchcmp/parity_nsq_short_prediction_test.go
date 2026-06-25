//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestNSQShortPrediction_SoAvsScalar asserts that the 4-lane SoA
// short_prediction kernel is bit-exact with four independent scalar
// calls to silk_noise_shape_quantizer_short_prediction_c for every
// combination of (order, nStates, seed, base) we sweep.
//
// The SoA kernel is always given 4 lanes of state, but the caller
// populates only the leading nStates lanes with random data and the
// remaining lanes are guaranteed zero by nsqDelDecAoStoSoA's zero-out
// contract. The zero-lane property (step 2) pins this contract at
// the short_prediction boundary: every zero-state lane must
// deterministically produce the bias (order >> 1), because each
// SMLAWB(acc, 0, c) is acc, and the initial acc is the bias.
func TestNSQShortPrediction_SoAvsScalar(t *testing.T) {
	const maxLanes = 4

	// base must satisfy base - (order-1) >= 0 to avoid underflow into
	// negative indices of sLPC_Q14. The scalar caller uses
	//   base = NSQ_LPC_BUF_LENGTH - 1 + i
	// with i in [0, MAX_SUB_FRAME_LENGTH) and NSQ_LPC_BUF_LENGTH=16,
	// so base ranges [15, 15 + MAX_SUB_FRAME_LENGTH - 1]. base >= 15
	// is safe for both order=10 and order=16.
	const baseLo = 15
	// A small handful of bases is enough; the kernel is tap-symmetric.
	bases := []int{baseLo, baseLo + 1, baseLo + 7, baseLo + 37}

	var zeroLaneChecked bool

	for _, order := range []int{10, 16} {
		for _, nStates := range []int{1, 2, 3, 4} {
			for _, seed := range []int64{1, 2, 3} {
				r := rand.New(rand.NewSource(seed*10_000 + int64(order)*100 + int64(nStates)))

				// Build AoS with leading nStates lanes populated from
				// the deterministic rand source; remaining lanes are
				// left at their zero value so the SoA zero-out
				// contract can be exercised.
				aos := make([]nativeopus.NSQ_del_dec_struct, maxLanes)
				for k := 0; k < nStates; k++ {
					aos[k] = nativeopus.ExportTestNSQDelDecFillRandom(r)
				}

				// Random coef16 of length `order`. opus_int16 is a
				// type alias for int16, so []int16 flows through
				// without conversion.
				coef := make([]int16, order)
				for i := range coef {
					coef[i] = int16(r.Int31())
				}

				// AoS -> SoA.
				soa := nativeopus.ExportTestNSQDelDecAoStoSoA(aos, nStates)

				for _, base := range bases {
					simd := nativeopus.ExportTestShortPredictionSoA(soa, base, coef, order)

					// Scalar parity check for every active lane.
					for k := 0; k < nStates; k++ {
						want := nativeopus.ExportTestShortPredictionScalar(
							nativeopus.ExportTestNSQDelDecSLPCQ14(&aos[k]), base, coef, order,
						)
						if simd[k] != want {
							t.Errorf("SoA != scalar: nStates=%d base=%d order=%d seed=%d lane=%d got=%d want=%d",
								nStates, base, order, seed, k, simd[k], want)
						}
					}

					// arm64 NEON asm kernel — identical signature,
					// should match the pure-Go SoA bit-for-bit.
					if nativeopus.ExportTestNSQSIMDAvailable() {
						asm := nativeopus.ExportTestShortPredictionSoASIMD(soa, base, coef, order)
						for k := 0; k < maxLanes; k++ {
							if asm[k] != simd[k] {
								t.Errorf("asm != pureGo SoA: nStates=%d base=%d order=%d seed=%d lane=%d asm=%d pureGo=%d",
									nStates, base, order, seed, k, asm[k], simd[k])
							}
						}
					}

					// Zero-lane property: every lane k >= nStates
					// reads from all-zero sLPC_Q14 slots, so the
					// result is the initial bias (order >> 1).
					//
					// We only need to check this once to confirm the
					// SoA zeroing contract survives through the
					// kernel — do it on the first case encountered.
					if !zeroLaneChecked && nStates < maxLanes {
						wantBias := int32(order >> 1)
						for k := nStates; k < maxLanes; k++ {
							if simd[k] != wantBias {
								t.Errorf("zero-lane leak: nStates=%d base=%d order=%d lane=%d got=%d want=%d",
									nStates, base, order, k, simd[k], wantBias)
							}
						}
						zeroLaneChecked = true
					}
				}
			}
		}
	}
}
