//go:build cgo

package lpc_encode

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// windowedSignal fabricates a float32 windowed signal of length n the way
// the encoder feeds FLAC__lpc_compute_autocorrelation: a random source
// signal multiplied by a Welch-ish window, all in float32 so the rounding
// matches FLAC__real (== float). Centred near zero so autoc[0] != 0.
func windowedSignal(r *rand.Rand, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		// Random source sample in [-1, 1).
		s := float32(r.Float64()*2 - 1)
		// Apply a window so the signal tapers — exercises the float32
		// rounding the real encoder produces. Window computed in float64
		// then stored to float32, mirroring window.c.
		w := 1.0 - math.Pow((float64(i)-float64(n-1)/2)/(float64(n-1)/2), 2)
		out[i] = s * float32(w)
	}
	// Guarantee a non-zero lag-0 autocorrelation.
	if out[0] == 0 {
		out[0] = 0.5
	}
	return out
}

// ── Autocorrelation (float64, strict-mode bit-exact) ────────────────

func TestParityComputeAutocorrelation(t *testing.T) {
	// The default (non-strict) arm64 build computes autocorrelation with a
	// float64x2 NEON kernel (8 independent FMADDD accumulators), which
	// reassociates the float64 reduction and is therefore intentionally NOT
	// bit-exact against the -ffp-contract=off oracle. Bit-exactness is only
	// asserted under flac_strict, where the scalar f64-helper body runs; the
	// default build's correctness is covered by the lossless encode
	// round-trip test instead.
	if !nativeflac.StrictMode {
		t.Skip("autocorrelation is bit-exact only under flac_strict; default build uses the reassociated NEON kernel")
	}
	r := rand.New(rand.NewPCG(2101, 2102))
	// Cover both branches of FLAC__lpc_compute_autocorrelation: the
	// locality path (data_len < FLAC__MAX_LPC_ORDER || lag > 16) and the
	// MAX_LAG include path (lag in {<=8, <=12, <=16}) with data_len >= 32.
	for _, dataLen := range []int{16, 31, 32, 64, 256, 4096} {
		for _, lag := range []uint32{1, 2, 8, 9, 12, 13, 16, 17, 24, 33} {
			if int(lag) > dataLen {
				continue
			}
			data := windowedSignal(r, dataLen)
			cAutoc := cgoComputeAutocorrelation(data, uint32(dataLen), lag)
			// LPCComputeAutocorrelation's MAX_LAG path writes up to its
			// rounded-up bucket (8/12/16), so size the destination the
			// way the real encoder does (FLAC__MAX_LPC_ORDER+1) and
			// compare only the first `lag` meaningful entries.
			gAutoc := make([]float64, nativeflac.MaxLPCOrder+1)
			nativeflac.LPCComputeAutocorrelation(data, uint32(dataLen), lag, gAutoc)
			require.Equal(t, cAutoc, gAutoc[:lag],
				"autocorrelation dataLen=%d lag=%d", dataLen, lag)
		}
	}
}

// ── Levinson-Durbin (float64, strict-mode bit-exact) ────────────────

func TestParityComputeLPCoefficients(t *testing.T) {
	// The Levinson-Durbin recursion accumulates through the default build's
	// fused math.FMA helpers (lpc_fp_default.go), which single-round a*b+c and
	// therefore diverge from the -ffp-contract=off oracle. Bit-exactness is
	// asserted only under flac_strict (the scalar f64-helper body); the default
	// build's correctness is covered by the lossless encode round-trip test.
	if !nativeflac.StrictMode {
		t.Skip("Levinson-Durbin is bit-exact only under flac_strict; default build fuses FMA")
	}
	r := rand.New(rand.NewPCG(2201, 2202))
	for _, dataLen := range []int{64, 256, 4096} {
		for _, maxOrder := range []uint32{1, 4, 8, 12, 16, 32} {
			lag := maxOrder + 1
			if int(lag) > dataLen {
				continue
			}
			data := windowedSignal(r, dataLen)
			autoc := cgoComputeAutocorrelation(data, uint32(dataLen), lag)
			require.NotZero(t, autoc[0], "autoc[0] must be non-zero for Levinson")

			cFlat, cErr, cMaxOrder := cgoComputeLPCoefficients(autoc, maxOrder)

			gLP := make([][]float32, nativeflac.MaxLPCOrder)
			for i := range gLP {
				gLP[i] = make([]float32, nativeflac.MaxLPCOrder)
			}
			gErr := make([]float64, maxOrder)
			gMaxOrder := nativeflac.LPCComputeLPCoefficients(autoc, maxOrder, gLP, gErr)

			require.Equal(t, cMaxOrder, gMaxOrder,
				"max_order dataLen=%d maxOrder=%d", dataLen, maxOrder)
			require.Equal(t, cErr, gErr,
				"per-order error dataLen=%d maxOrder=%d", dataLen, maxOrder)
			// Compare only the populated rows/cols: row i holds coeffs
			// [0..i]; the rest of the [MaxLPCOrder][MaxLPCOrder] table is
			// uninitialised in C, so don't compare it.
			for i := uint32(0); i < cMaxOrder; i++ {
				for j := uint32(0); j <= i; j++ {
					cv := cFlat[int(i)*MaxLPCOrder+int(j)]
					require.Equal(t, cv, gLP[i][j],
						"lp_coeff[%d][%d] dataLen=%d maxOrder=%d", i, j, dataLen, maxOrder)
				}
			}
		}
	}
}

// ── Quantise coefficients (integer, bit-exact) ──────────────────────

func TestParityQuantizeCoefficients(t *testing.T) {
	r := rand.New(rand.NewPCG(2301, 2302))
	for _, dataLen := range []int{256, 4096} {
		for _, order := range []uint32{1, 2, 4, 8, 12, 16, 32} {
			lag := order + 1
			if int(lag) > dataLen {
				continue
			}
			for _, precision := range []uint32{5, 10, 12, 14, 15} {
				data := windowedSignal(r, dataLen)
				autoc := cgoComputeAutocorrelation(data, uint32(dataLen), lag)
				if autoc[0] == 0 {
					continue
				}
				// Derive real LP coefficients to feed the quantiser.
				cFlat, _, mo := cgoComputeLPCoefficients(autoc, order)
				if mo < order {
					continue
				}
				lp := make([]float32, order)
				for i := uint32(0); i < order; i++ {
					lp[i] = cFlat[int(order-1)*MaxLPCOrder+int(i)]
				}

				cQlp, cShift, cStatus := cgoQuantizeCoefficients(lp, order, precision)

				gQlp := make([]int32, order)
				var gShift int
				gStatus := nativeflac.LPCQuantizeCoefficients(lp, order, precision, gQlp, &gShift)

				require.Equal(t, cStatus, gStatus,
					"quantize status order=%d precision=%d", order, precision)
				if cStatus != 0 {
					continue
				}
				require.Equal(t, cShift, gShift,
					"quantize shift order=%d precision=%d", order, precision)
				require.Equal(t, cQlp, gQlp,
					"quantize qlp order=%d precision=%d", order, precision)
			}
		}
	}
}

// ── Residual (integer, bit-exact) ───────────────────────────────────

func fillRandom32(r *rand.Rand, out []int32, bits int) {
	half := int32(1) << (bits - 1)
	for i := range out {
		out[i] = int32(r.Uint32()&uint32(2*half-1)) - half
	}
}

func TestParityComputeResidual(t *testing.T) {
	r := rand.New(rand.NewPCG(2401, 2402))
	for _, dataLen := range []int{1, 16, 4096} {
		for _, order := range []uint32{1, 2, 4, 8, 12, 13, 16, 32} {
			for _, shift := range []int{0, 5, 12} {
				n := int(order) + dataLen
				cData := make([]int32, n)
				qlp := make([]int32, order)
				fillRandom32(r, qlp, 12)
				fillRandom32(r, cData, 14)

				cRes := cgoComputeResidual(cData, uint32(dataLen), qlp, order, shift)
				gRes := make([]int32, dataLen)
				nativeflac.LPCComputeResidualFromQLPCoefficients(cData, uint32(dataLen), qlp, order, shift, gRes)
				require.Equal(t, cRes, gRes,
					"residual order=%d shift=%d dataLen=%d", order, shift, dataLen)
			}
		}
	}
}

func TestParityComputeResidualWide(t *testing.T) {
	r := rand.New(rand.NewPCG(2501, 2502))
	for _, dataLen := range []int{1, 16, 4096} {
		for _, order := range []uint32{1, 2, 4, 8, 12, 13, 16, 32} {
			for _, shift := range []int{0, 5, 12} {
				n := int(order) + dataLen
				cData := make([]int32, n)
				qlp := make([]int32, order)
				fillRandom32(r, qlp, 14)
				fillRandom32(r, cData, 24)

				cRes := cgoComputeResidualWide(cData, uint32(dataLen), qlp, order, shift)
				gRes := make([]int32, dataLen)
				nativeflac.LPCComputeResidualFromQLPCoefficientsWide(cData, uint32(dataLen), qlp, order, shift, gRes)
				require.Equal(t, cRes, gRes,
					"residual wide order=%d shift=%d dataLen=%d", order, shift, dataLen)
			}
		}
	}
}

// ── Limit-residual (int64 accumulator + overflow return, bit-exact) ──
//
// FLAC__lpc_compute_residual_from_qlp_coefficients_limit_residual[_33bit]
// (lpc.c:832 / :886) accumulate into int64 and bail out returning false
// the moment a residual would not fit in int32 (residual_to_check <=
// INT32_MIN || > INT32_MAX). This is the path the encoder uses in subset
// mode, and the path the blocker bug lived in — so both the per-sample
// residual values AND the boolean return must match the oracle bit-for-bit,
// across both an in-range regime (ok == true) and a forced-overflow regime
// (ok == false).

func fillRandom64(r *rand.Rand, out []int64, bits int) {
	half := int64(1) << (bits - 1)
	for i := range out {
		out[i] = int64(r.Uint64()&uint64(2*half-1)) - half
	}
}

func TestParityComputeResidualLimitResidual(t *testing.T) {
	r := rand.New(rand.NewPCG(2601, 2602))
	for _, dataLen := range []int{1, 16, 4096} {
		for _, order := range []uint32{1, 2, 4, 8, 12, 13, 16, 32} {
			for _, shift := range []int{0, 5, 12} {
				// Two regimes: "narrow" keeps every residual comfortably
				// inside int32 (expect ok == true); "wide" uses near-max
				// coefficients and 24-bit data with shift 0 so the int64
				// sum routinely exceeds int32, forcing the false return.
				for _, regime := range []struct {
					name             string
					qlpBits, datBits int
				}{
					{"narrow", 8, 8},
					{"wide", 15, 24},
				} {
					n := int(order) + dataLen
					cData := make([]int32, n)
					qlp := make([]int32, order)
					fillRandom32(r, qlp, regime.qlpBits)
					fillRandom32(r, cData, regime.datBits)

					cRes, cOK := cgoComputeResidualLimitResidual(cData, uint32(dataLen), qlp, order, shift)
					gRes := make([]int32, dataLen)
					gOK := nativeflac.LPCComputeResidualFromQLPCoefficientsLimitResidual(cData, uint32(dataLen), qlp, order, shift, gRes)

					require.Equal(t, cOK, gOK,
						"limit-residual ok %s order=%d shift=%d dataLen=%d", regime.name, order, shift, dataLen)
					require.Equal(t, cRes, gRes,
						"limit-residual values %s order=%d shift=%d dataLen=%d", regime.name, order, shift, dataLen)
				}
			}
		}
	}
}

func TestParityComputeResidualLimitResidual33Bit(t *testing.T) {
	r := rand.New(rand.NewPCG(2701, 2702))
	for _, dataLen := range []int{1, 16, 4096} {
		for _, order := range []uint32{1, 2, 4, 8, 12, 13, 16, 32} {
			for _, shift := range []int{0, 5, 12} {
				for _, regime := range []struct {
					name             string
					qlpBits, datBits int
				}{
					{"narrow", 8, 8},
					{"wide", 15, 33},
				} {
					n := int(order) + dataLen
					cData := make([]int64, n)
					qlp := make([]int32, order)
					fillRandom32(r, qlp, regime.qlpBits)
					fillRandom64(r, cData, regime.datBits)

					cRes, cOK := cgoComputeResidualLimitResidual33Bit(cData, uint32(dataLen), qlp, order, shift)
					gRes := make([]int32, dataLen)
					gOK := nativeflac.LPCComputeResidualFromQLPCoefficientsLimitResidual33Bit(cData, uint32(dataLen), qlp, order, shift, gRes)

					require.Equal(t, cOK, gOK,
						"limit-residual-33bit ok %s order=%d shift=%d dataLen=%d", regime.name, order, shift, dataLen)
					require.Equal(t, cRes, gRes,
						"limit-residual-33bit values %s order=%d shift=%d dataLen=%d", regime.name, order, shift, dataLen)
				}
			}
		}
	}
}
