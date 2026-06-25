//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// stableARwave2b generates stable AR coefficients from reflection
// coefficients in (-0.85, 0.85) via the C k2a FLP helper (matches the
// one in parity_silk_flp_test.go).
func stableARwave2b(r *rand.Rand, order int) []float32 {
	rc := make([]float32, order)
	for i := range rc {
		rc[i] = (r.Float32()*2 - 1) * 0.85
	}
	return cSilkK2aFLP(rc)
}

// ---------- A2NLSF_FLP / NLSF2A_FLP (helpers exercised by find_LPC) ----------

func TestParity_SilkA2NLSFFLP(t *testing.T) {
	r := rand.New(rand.NewSource(20201))
	// silk_NLSF2A asserts d==10 || d==16; silk_A2NLSF is order-agnostic
	// but for symmetry with NLSF2A we restrict to the same set.
	for _, order := range []int{10, 16} {
		for run := 0; run < 8; run++ {
			A := stableARwave2b(r, order)
			co := cSilkA2NLSFFLP(A)
			go_ := nativeopus.ExportTestSilkA2NLSFFLP(A)
			for i := 0; i < order; i++ {
				if co[i] != go_[i] {
					t.Errorf("A2NLSF order=%d run=%d [%d]: C=%d Go=%d", order, run, i, co[i], go_[i])
					break
				}
			}
		}
	}
}

func TestParity_SilkNLSF2AFLP(t *testing.T) {
	// Generate realistic NLSFs by A2NLSF on a stable AR, then round-trip.
	// silk_NLSF2A asserts d==10 || d==16.
	r := rand.New(rand.NewSource(20203))
	for _, order := range []int{10, 16} {
		for run := 0; run < 8; run++ {
			A := stableARwave2b(r, order)
			NLSF := cSilkA2NLSFFLP(A)
			co := cSilkNLSF2AFLP(NLSF)
			go_ := nativeopus.ExportTestSilkNLSF2AFLP(NLSF)
			for i := 0; i < order; i++ {
				if co[i] != go_[i] {
					t.Errorf("NLSF2A order=%d run=%d [%d]: C=%g Go=%g (%d ULP)",
						order, run, i, co[i], go_[i], ulpDiffF32(co[i], go_[i]))
					break
				}
			}
		}
	}
}

// ---------- silk_find_LTP_FLP ----------

func TestParity_SilkFindLTPFLP(t *testing.T) {
	r := rand.New(rand.NewSource(20207))
	// Lag must satisfy rOff - lag[k] - LTP_ORDER/2 >= 0, where rOff
	// advances by subfr_length each iteration. The slice read by
	// corrMatrix/Vector has L+Order-1 = subfr_length+4 elements starting
	// at rBuf[rOff - lag[k] - 2], and silk_energy_FLP reads
	// subfr_length+LTP_ORDER samples starting at rBuf[rOff].
	// Budget: pre-roll for maximal lag + subfr_length*nb_subfr + tail.
	const maxLag = 200
	for _, nb := range []int{1, 2, 4} {
		for _, sl := range []int{40, 60, 80} {
			for run := 0; run < 4; run++ {
				rOff := maxLag + 8
				total := rOff + nb*sl + sl + 16 // tail for the silk_energy read
				rBuf := randF32(r, total, 0.9)
				lag := make([]int, nb)
				for i := range lag {
					lag[i] = 20 + r.Intn(maxLag-20)
				}
				cXX, cxX := cSilkFindLTPFLP(rBuf, rOff, lag, sl, nb)
				gXX, gxX := nativeopus.ExportTestSilkFindLTPFLP(rBuf, rOff, lag, sl, nb)
				for i := 0; i < len(cXX); i++ {
					if cXX[i] != gXX[i] {
						t.Errorf("find_LTP nb=%d sl=%d run=%d XX[%d]: C=%g Go=%g (%d ULP)",
							nb, sl, run, i, cXX[i], gXX[i], ulpDiffF32(cXX[i], gXX[i]))
						break
					}
				}
				for i := 0; i < len(cxX); i++ {
					if cxX[i] != gxX[i] {
						t.Errorf("find_LTP nb=%d sl=%d run=%d xX[%d]: C=%g Go=%g (%d ULP)",
							nb, sl, run, i, cxX[i], gxX[i], ulpDiffF32(cxX[i], gxX[i]))
						break
					}
				}
			}
		}
	}
}

// ---------- silk_find_LPC_FLP ----------

func TestParity_SilkFindLPCFLP(t *testing.T) {
	r := rand.New(rand.NewSource(20211))

	// subfr_length + predictLPCOrder is the Burg subframe length;
	// subfr_length*nb_subfr is the total input samples passed to x.
	// Also LPC_res[] is sized MAX_FRAME_LENGTH + MAX_NB_SUBFR*MAX_LPC_ORDER
	// in C (= 320 + 64 = 384). The interpolation branch calls
	// silk_LPC_analysis_filter_FLP with `2 * subfr_length` output samples,
	// so 2*subfr_length <= 384, i.e. subfr_length <= 192.
	//
	// Burg has silkBurgMaxFrameSize = 384 (subfr_length*nb_subfr<=384).
	// silk_NLSF2A asserts d==10 || d==16; use the SILK predictLPCOrder set.
	for _, order := range []int{10, 16} {
		for _, nb := range []int{1, 2, 4} {
			for _, slBase := range []int{40, 60, 80} {
				// subfr_length passed to Burg is (sl_cmn + order),
				// which must be <= 192 for the LPC_res constraint and
				// (sl_cmn + order)*nb <= 384 for Burg.
				sl := slBase + order
				if sl > 192 {
					continue
				}
				if sl*nb > 384 {
					continue
				}
				for _, useInterp := range []int{0, 1} {
					for _, firstAfter := range []int{0, 1} {
						for run := 0; run < 3; run++ {
							// The Burg call operates on nb*sl samples,
							// but the interp branch also needs x up to
							// (MAX_NB_SUBFR/2)*sl + 2*sl (via the second Burg
							// call at offset (MAX_NB_SUBFR/2)*sl, and the
							// LPC_analysis_filter over 2*sl samples). When
							// nb < 4, interp branch is gated off by the
							// `nb_subfr == MAX_NB_SUBFR` check, so we only
							// need nb*sl samples.
							xLen := nb * sl
							x := randF32(r, xLen, 0.5)
							minInvGain := float32(1.0 / 16384.0)

							// Generate plausible prev NLSFs from a random
							// stable AR.
							A := stableARwave2b(r, order)
							prevNLSF := cSilkA2NLSFFLP(A)
							// Pad to MAX_LPC_ORDER.
							pNL := make([]int16, 16)
							copy(pNL, prevNLSF)

							in := nativeopus.FindLPCInput{
								PredictLPCOrder:         order,
								NbSubfr:                 nb,
								SubfrLength:             slBase, // s.subfr_length + predictLPCOrder == sl
								UseInterpolatedNLSFs:    useInterp,
								FirstFrameAfterReset:    firstAfter,
								PrevNLSFqQ15:            pNL,
								InitialNLSFInterpCoefQ2: 4,
								X:                       x,
								MinInvGain:              minInvGain,
							}
							cIn := cFindLPCInput{
								PredictLPCOrder:         order,
								NbSubfr:                 nb,
								SubfrLength:             slBase,
								UseInterpolatedNLSFs:    useInterp,
								FirstFrameAfterReset:    firstAfter,
								PrevNLSFqQ15:            pNL,
								InitialNLSFInterpCoefQ2: 4,
								X:                       x,
								MinInvGain:              minInvGain,
							}
							co := cSilkFindLPCFLP(cIn)
							go_ := nativeopus.ExportTestSilkFindLPCFLP(in)

							tag := func() string {
								return "find_LPC"
							}
							_ = tag
							if co.NLSFInterpCoefQ2 != go_.NLSFInterpCoefQ2 {
								t.Errorf("find_LPC order=%d nb=%d sl=%d ui=%d fa=%d run=%d InterpCoef: C=%d Go=%d",
									order, nb, slBase, useInterp, firstAfter, run,
									co.NLSFInterpCoefQ2, go_.NLSFInterpCoefQ2)
							}
							for i := 0; i < 16; i++ {
								if co.NLSFQ15[i] != go_.NLSFQ15[i] {
									t.Errorf("find_LPC order=%d nb=%d sl=%d ui=%d fa=%d run=%d NLSF[%d]: C=%d Go=%d",
										order, nb, slBase, useInterp, firstAfter, run, i,
										co.NLSFQ15[i], go_.NLSFQ15[i])
									break
								}
							}
						}
					}
				}
			}
		}
	}
}
