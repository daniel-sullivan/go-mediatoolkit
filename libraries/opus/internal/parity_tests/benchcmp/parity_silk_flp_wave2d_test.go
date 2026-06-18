//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_SilkFindPredCoefsFLP exercises silk_find_pred_coefs_FLP
// across voiced/unvoiced paths, NB/MB (fs=8/12 kHz, predictLPCOrder=10)
// and WB (fs=16 kHz, predictLPCOrder=16), short (2) and long (4)
// subframe counts, and both condCoding modes.
//
// Input-domain invariants mirrored here:
//   * predictLPCOrder couples to fs_kHz (NB/MB→10, WB→16).
//   * subfr_length = 5*fs_kHz (40/60/80 samples).
//   * ltp_mem_length >= predictLPCOrder + max(pitchL) + LTP_ORDER/2
//     (voiced-branch celt_assert).
//   * Pitch lags follow a base-lag ± jitter pattern with lag(k) ≤ lag(0).
//   * ResPitch layout: resPitchOff must leave >= max(lag)+LTP_ORDER/2
//     pre-roll behind it, and resPitchOff + (nb_subfr-1)*subfr_length +
//     subfr_length + LTP_ORDER samples of tail for silk_energy_FLP.
//   * X layout: xOff - predictLPCOrder must be >= 0, and
//     xOff + nb_subfr*subfr_length samples of tail. The LTP filter
//     reads x at offsets down to xOff - predictLPCOrder - max(pitchL).
//   * find_LPC_FLP Burg constraints: (subfr_length+predictLPCOrder)*nb_subfr
//     <= silkBurgMaxFrameSize (384), and 2*(subfr_length+predictLPCOrder)
//     <= MAX_FRAME_LENGTH + MAX_NB_SUBFR*MAX_LPC_ORDER (384) for the
//     interpolation filter call.
//   * NLSF preamble: celt_assert requires useInterpolatedNLSFs==1 OR
//     NLSFInterpCoef_Q2==4.

func randFindPredCoefsPayload(r *rand.Rand, voiced bool, wb bool, nb int, condCoding int, firstAfter int, useInterp int) nativeopus.SilkFindPredCoefsFLPPayload {
	var p nativeopus.SilkFindPredCoefsFLPPayload

	fsKHz := 8
	if wb {
		fsKHz = 16
	} else if r.Intn(2) == 1 {
		fsKHz = 12
	}
	predictOrder := 10
	if wb {
		predictOrder = 16
	}
	sl := 5 * fsKHz // 40, 60, or 80.

	// Skip configurations that violate the downstream Burg/interp sizing.
	// (subfr_length + predictLPCOrder) * nb must be <= 384.
	if (sl+predictOrder)*nb > 384 {
		// reduce nb to 2 if possible; skip if still too big.
		if (sl+predictOrder)*2 <= 384 {
			nb = 2
		} else {
			// fall back to NB to shrink.
			fsKHz = 8
			predictOrder = 10
			sl = 40
			wb = false
		}
	}
	// 2 * (sl + predictOrder) <= 384 for LPC_res scratch in find_LPC.
	if 2*(sl+predictOrder) > 384 {
		sl = (384 / 2) - predictOrder
	}

	// LTP mem length needs to cover predictLPCOrder + baseLag + LTP_ORDER/2.
	// Standard SILK sets ltp_mem_length = 20 * fs_kHz. Lags range ~minLag..maxLag.
	ltpMem := 20 * fsKHz // 160/240/320

	// Pitch lag pattern (see randNSQWrapperPayload).
	const pitchJitter = 10
	minLag := sl + predictOrder + 4
	maxLag := ltpMem - predictOrder - 2 - 4
	if maxLag <= minLag+pitchJitter {
		maxLag = minLag + pitchJitter + 1
	}
	baseLag := minLag + pitchJitter + r.Intn(maxLag-minLag-pitchJitter)
	var pitchL [4]int32
	pitchL[0] = int32(baseLag)
	for i := 1; i < 4; i++ {
		pl := baseLag - r.Intn(pitchJitter+1)
		if pl < minLag {
			pl = minLag
		}
		pitchL[i] = int32(pl)
	}

	// Assemble payload.
	sigType := int8(2) // TYPE_VOICED
	if !voiced {
		sigType = int8(r.Intn(2)) // 0 or 1 (unvoiced / inactive)
	}
	p.SignalType = sigType
	p.NbSubfr = nb
	p.SubfrLength = sl
	p.PredictLPCOrder = predictOrder
	p.LtpMemLength = ltpMem
	p.FirstFrameAfterReset = firstAfter
	p.SumLogGainQ7In = int32(r.Intn(1000))
	p.SNRdBQ7 = 1280 + r.Intn(3200)
	p.PacketLossPerc = r.Intn(40)
	p.NFramesPerPacket = 1 + r.Intn(3)
	if r.Intn(2) == 1 {
		p.LBRRFlag = 1
	}
	p.UseInterpolatedNLSFs = useInterp
	p.SpeechActivityQ8 = r.Intn(257)
	p.NLSFMSVQSurvivors = 2 + r.Intn(8)
	p.WB = wb

	// NLSFInterpCoef_Q2 initial — must be 4 when useInterp==0 per celt_assert
	// inside silk_process_NLSFs (see existing wave 2b test). When useInterp==1
	// any value 0..4 is valid but 4 is the default.
	if useInterp == 0 {
		p.NLSFInterpCoefQ2In = 4
	} else {
		p.NLSFInterpCoefQ2In = int8(r.Intn(5))
	}

	// Generate plausible prev NLSFs from a random stable AR.
	A := stableARwave2b(r, predictOrder)
	prevNLSF := cSilkA2NLSFFLP(A)
	for i := 0; i < predictOrder && i < 16; i++ {
		p.PrevNLSFqQ15[i] = prevNLSF[i]
	}
	p.Arch = 0
	p.CondCoding = condCoding

	// Gains: positive reasonable range.
	for i := 0; i < nb; i++ {
		p.Gains[i] = 0.5 + r.Float32()*5.0
	}
	// For unused subframes, leave positive too — silk_assert requires > 0.
	for i := nb; i < 4; i++ {
		p.Gains[i] = 1.0
	}
	for i := 0; i < 4; i++ {
		p.PitchL[i] = pitchL[i]
	}
	p.CodingQuality = r.Float32()

	// --- res_pitch buffer. ---
	// silk_find_LTP_FLP at subframe k reads:
	//   - corrMatrix_FLP(lagPtr, L=subfr_length, Order=5): reads L+Order-1 = sl+4 samples at lagPtr.
	//   - corrVector_FLP(lagPtr, rPtr, L=subfr_length, Order=5): reads lagPtr[0..sl+Order-2] and rPtr[0..sl-1].
	//   - silk_energy_FLP(rPtr, subfr_length + LTP_ORDER): reads sl+5 at rPtr.
	// lagPtr = rPtr - (lag[k] + LTP_ORDER/2). At subframe k, rPtr = resPitch + k*sl.
	// So we need resPitchOff - maxLag - LTP_ORDER/2 >= 0, and tail = (nb-1)*sl + max(sl+4, sl+5) = (nb-1)*sl + sl+5 = nb*sl + 5.
	resPitchOff := baseLag + 2 + 16 // some extra pre-roll.
	resPitchLen := resPitchOff + nb*sl + 16
	p.ResPitch = make([]float32, resPitchLen)
	// Fill with moderate values; make sure not to overflow.
	for i := range p.ResPitch {
		p.ResPitch[i] = (r.Float32()*2 - 1) * 0.5
	}
	p.ResPitchOff = resPitchOff

	// --- x buffer. ---
	// Accessed at xOff - predictLPCOrder (scale_copy / LTP filter pre-roll)
	// down to xOff - predictLPCOrder - max(pitchL) (LTP filter history read),
	// up to xOff + nb*sl (main frame). Keep extra head/tail padding.
	xOff := predictOrder + baseLag + 16
	xLen := xOff + nb*sl + 16
	p.X = make([]float32, xLen)
	for i := range p.X {
		p.X[i] = (r.Float32()*2 - 1) * 0.5
	}
	p.XOff = xOff

	return p
}

func TestParity_SilkFindPredCoefsFLP(t *testing.T) {
	r := rand.New(rand.NewSource(20260419))
	// Cover:
	//   - voiced & unvoiced
	//   - WB (predictLPCOrder=16) & NB/MB (predictLPCOrder=10)
	//   - nb_subfr 2 and 4
	//   - condCoding independent (0) and conditional (2)
	//   - first_frame_after_reset 0/1
	//   - useInterpolatedNLSFs 0/1
	configs := 0
	runs := 0
	for _, voiced := range []bool{true, false} {
		for _, wb := range []bool{false, true} {
			for _, nb := range []int{2, 4} {
				for _, cc := range []int{0, 2} {
					for _, fa := range []int{0, 1} {
						for _, ui := range []int{0, 1} {
							configs++
							for run := 0; run < 3; run++ {
								p := randFindPredCoefsPayload(r, voiced, wb, nb, cc, fa, ui)
								co := cSilkFindPredCoefsFLP(p)
								go_ := nativeopus.ExportTestSilkFindPredCoefsFLP(p)
								runs++

								label := func(field string) string {
									return field
								}
								_ = label

								// Compare all salient outputs.
								if co.SumLogGainQ7 != go_.SumLogGainQ7 {
									t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d SumLogGainQ7: C=%d Go=%d",
										voiced, wb, nb, cc, fa, ui, run, co.SumLogGainQ7, go_.SumLogGainQ7)
								}
								if co.PERIndex != go_.PERIndex {
									t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d PERIndex: C=%d Go=%d",
										voiced, wb, nb, cc, fa, ui, run, co.PERIndex, go_.PERIndex)
								}
								if co.LTPScaleIndex != go_.LTPScaleIndex {
									t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d LTPScaleIndex: C=%d Go=%d",
										voiced, wb, nb, cc, fa, ui, run, co.LTPScaleIndex, go_.LTPScaleIndex)
								}
								if co.LTPScale != go_.LTPScale {
									t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d LTPScale: C=%g Go=%g",
										voiced, wb, nb, cc, fa, ui, run, co.LTPScale, go_.LTPScale)
								}
								if co.LTPredCodGain != go_.LTPredCodGain {
									t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d LTPredCodGain: C=%g Go=%g",
										voiced, wb, nb, cc, fa, ui, run, co.LTPredCodGain, go_.LTPredCodGain)
								}
								if co.NLSFInterpCoefQ2 != go_.NLSFInterpCoefQ2 {
									t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d NLSFInterpCoef_Q2: C=%d Go=%d",
										voiced, wb, nb, cc, fa, ui, run, co.NLSFInterpCoefQ2, go_.NLSFInterpCoefQ2)
								}
								for i := 0; i < 4; i++ {
									if co.LTPIndex[i] != go_.LTPIndex[i] {
										t.Fatalf("voiced=%v nb=%d run=%d LTPIndex[%d]: C=%d Go=%d",
											voiced, nb, run, i, co.LTPIndex[i], go_.LTPIndex[i])
									}
									if co.ResNrg[i] != go_.ResNrg[i] {
										t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d ResNrg[%d]: C=%g Go=%g (%d ULP)",
											voiced, wb, nb, cc, fa, ui, run, i, co.ResNrg[i], go_.ResNrg[i], ulpDiffF32(co.ResNrg[i], go_.ResNrg[i]))
									}
								}
								for i := 0; i < 5*4; i++ {
									if co.LTPCoef[i] != go_.LTPCoef[i] {
										t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d LTPCoef[%d]: C=%g Go=%g",
											voiced, wb, nb, cc, fa, ui, run, i, co.LTPCoef[i], go_.LTPCoef[i])
									}
								}
								for i := 0; i < 16; i++ {
									if co.PrevNLSFqOut[i] != go_.PrevNLSFqOut[i] {
										t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d PrevNLSFq[%d]: C=%d Go=%d",
											voiced, wb, nb, cc, fa, ui, run, i, co.PrevNLSFqOut[i], go_.PrevNLSFqOut[i])
									}
									if co.PredCoefA[i] != go_.PredCoefA[i] {
										t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d PredCoefA[%d]: C=%g Go=%g",
											voiced, wb, nb, cc, fa, ui, run, i, co.PredCoefA[i], go_.PredCoefA[i])
									}
									if co.PredCoefB[i] != go_.PredCoefB[i] {
										t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d PredCoefB[%d]: C=%g Go=%g",
											voiced, wb, nb, cc, fa, ui, run, i, co.PredCoefB[i], go_.PredCoefB[i])
									}
								}
								for i := 0; i < 17; i++ {
									if co.NLSFIndices[i] != go_.NLSFIndices[i] {
										t.Fatalf("voiced=%v wb=%v nb=%d cc=%d fa=%d ui=%d run=%d NLSFIndices[%d]: C=%d Go=%d",
											voiced, wb, nb, cc, fa, ui, run, i, co.NLSFIndices[i], go_.NLSFIndices[i])
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if runs == 0 {
		t.Fatalf("no runs executed (configs=%d)", configs)
	}
	t.Logf("ran %d parity configurations", runs)
}
