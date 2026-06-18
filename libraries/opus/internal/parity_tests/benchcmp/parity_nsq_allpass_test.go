//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestNSQAllpass_SoAvsScalar asserts that the 4-lane SoA noise-shape
// allpass kernel is bit-exact with four independent scalar copies of
// the loop body at silk_NSQ_del_dec.go:403-421 for every combination
// of (nStates, shapingLPCOrder, seed) we sweep.
//
// Checks:
//  1. The per-lane n_AR_Q14 returned by the SoA kernel matches the
//     scalar computation for every active lane.
//  2. The mutated sAR2_Q14 per active lane in the SoA matches the
//     scalar's mutated psDD.sAR2_Q14.
//  3. Lanes k >= nStates (zero-initialised per the SoA zero-out
//     contract) deterministically produce the bias (shapingLPCOrder
//     >> 1) for n_AR_Q14 and keep sAR2_Q14 all zero — because every
//     SMLAWB(0, 0, *) and SUB32_ovflw(0, 0) is zero.
//  4. If the arm64 NEON asm kernel is linked in, its output and its
//     mutations are bit-for-bit identical to the pure-Go SoA.
func TestNSQAllpass_SoAvsScalar(t *testing.T) {
	const maxLanes = 4

	// Only even orders: the scalar loop iterates j=2,4,...,order-2, so
	// odd orders don't reach the terminal tmp1 store correctly. The
	// real encoder also uses even shapingLPCOrder (10, 14, 16, 20, 22,
	// 24 are all SILK-reachable values).
	orders := []int{10, 14, 16, 20, 22, 24}

	for _, order := range orders {
		for _, nStates := range []int{1, 2, 3, 4} {
			for _, seed := range []int64{1, 2, 3, 4, 5} {
				r := rand.New(rand.NewSource(seed*100_000 + int64(order)*100 + int64(nStates)))

				// Build AoS with leading nStates lanes populated from
				// the deterministic rand source; remaining lanes left
				// at their zero value.
				aos := make([]nativeopus.NSQ_del_dec_struct, maxLanes)
				for k := 0; k < nStates; k++ {
					aos[k] = nativeopus.ExportTestNSQDelDecFillRandom(r)
				}

				// Random warping_Q16 in [-32768, 32767] — only the
				// low 16 bits are consumed by silk_SMLAWB anyway, but
				// staying in int16 range keeps the fixed-point trap
				// (INT32_MIN × INT16_MIN through SQDMULH) out of
				// reach for any future asm kernel comparison.
				warping := int32(r.Intn(1<<16) - (1 << 15))

				// Random AR_shp_Q13 of length `order`. Bound to
				// (INT16_MIN, INT16_MAX] for the same SQDMULH
				// saturation-corner reason.
				ARshp := make([]int16, order)
				for i := range ARshp {
					v := r.Intn(1<<16) - (1 << 15)
					if v == -(1 << 15) {
						v++
					}
					ARshp[i] = int16(v)
				}

				// Scalar reference: run the loop body on a fresh copy
				// per lane, capturing the mutated sAR2_Q14 and the
				// n_AR_Q14 result. The copy is a value copy of the
				// NSQ_del_dec_struct so the original aos entry stays
				// pristine for use by the SoA path below.
				wantNAR := make([]int32, maxLanes)
				wantSAR2 := make([][]int32, maxLanes) // length = order per lane
				for k := 0; k < nStates; k++ {
					scalarCopy := aos[k]
					n, sar := nativeopus.ExportTestNSQAllpassScalar(&scalarCopy, warping, ARshp, order)
					wantNAR[k] = int32(n)
					tmp := make([]int32, order)
					for i := 0; i < order; i++ {
						tmp[i] = int32(sar[i])
					}
					wantSAR2[k] = tmp
				}

				// AoS -> SoA, then run the SoA kernel once on all 4
				// lanes simultaneously.
				soa := nativeopus.ExportTestNSQDelDecAoStoSoA(aos, nStates)
				gotNAR := nativeopus.ExportTestNSQAllpassSoA(soa, warping, ARshp, order)

				// Project the mutated SoA back to AoS so we can read
				// per-lane sAR2_Q14 via the public export surface.
				gotAoS := nativeopus.ExportTestNSQDelDecSoAtoAoS(soa, maxLanes)

				// Active-lane parity: n_AR_Q14 and the resulting
				// sAR2_Q14 must match the scalar reference exactly.
				for k := 0; k < nStates; k++ {
					if int32(gotNAR[k]) != wantNAR[k] {
						t.Errorf("SoA n_AR_Q14 mismatch: order=%d nStates=%d seed=%d lane=%d got=%d want=%d",
							order, nStates, seed, k, gotNAR[k], wantNAR[k])
					}
					laneSAR2 := nativeopus.ExportTestNSQDelDecSAR2Q14(&gotAoS[k])
					for i := 0; i < order; i++ {
						if int32(laneSAR2[i]) != wantSAR2[k][i] {
							t.Errorf("SoA sAR2_Q14[%d][lane=%d] mismatch: order=%d nStates=%d seed=%d got=%d want=%d",
								i, k, order, nStates, seed, laneSAR2[i], wantSAR2[k][i])
							break
						}
					}
				}

				// Zero-lane property: lanes k >= nStates start at
				// all-zero sAR2_Q14 and Diff_Q14 (by the SoA zero-out
				// contract), so every SMLAWB and SUB reduces to the
				// bias / zero and the mutated sAR2 stays all zero.
				wantBias := int32(order >> 1)
				for k := nStates; k < maxLanes; k++ {
					if int32(gotNAR[k]) != wantBias {
						t.Errorf("zero-lane n_AR_Q14 mismatch: order=%d nStates=%d seed=%d lane=%d got=%d want=%d (bias)",
							order, nStates, seed, k, gotNAR[k], wantBias)
					}
					laneSAR2 := nativeopus.ExportTestNSQDelDecSAR2Q14(&gotAoS[k])
					for i := 0; i < order; i++ {
						if laneSAR2[i] != 0 {
							t.Errorf("zero-lane sAR2_Q14[%d][lane=%d] leak: order=%d nStates=%d seed=%d got=%d",
								i, k, order, nStates, seed, laneSAR2[i])
							break
						}
					}
				}

				// If the arm64 NEON asm kernel is linked, its output
				// and mutations must match the pure-Go SoA bit-for-
				// bit. Rebuild the SoA so the asm kernel starts from
				// the same pristine state as the pure-Go one.
				if nativeopus.ExportTestNSQAllpassSIMDAvailable() {
					soaAsm := nativeopus.ExportTestNSQDelDecAoStoSoA(aos, nStates)
					asmNAR := nativeopus.ExportTestNSQAllpassSIMD(soaAsm, warping, ARshp, order)
					asmAoS := nativeopus.ExportTestNSQDelDecSoAtoAoS(soaAsm, maxLanes)
					for k := 0; k < maxLanes; k++ {
						if asmNAR[k] != gotNAR[k] {
							t.Errorf("asm n_AR_Q14 != pureGo SoA: order=%d nStates=%d seed=%d lane=%d asm=%d pureGo=%d",
								order, nStates, seed, k, asmNAR[k], gotNAR[k])
						}
						pgLaneSAR2 := nativeopus.ExportTestNSQDelDecSAR2Q14(&gotAoS[k])
						asLaneSAR2 := nativeopus.ExportTestNSQDelDecSAR2Q14(&asmAoS[k])
						for i := 0; i < order; i++ {
							if asLaneSAR2[i] != pgLaneSAR2[i] {
								t.Errorf("asm sAR2_Q14[%d][lane=%d] != pureGo SoA: order=%d nStates=%d seed=%d asm=%d pureGo=%d",
									i, k, order, nStates, seed, asLaneSAR2[i], pgLaneSAR2[i])
								break
							}
						}
					}
				}
			}
		}
	}
}
