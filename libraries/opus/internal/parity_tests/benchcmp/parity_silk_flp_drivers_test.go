//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// Random but range-valid payload for silk_process_gains_FLP.
func randProcessGainsPayload(r *rand.Rand) nativeopus.SilkEncoderStateFLPPayload {
	var p nativeopus.SilkEncoderStateFLPPayload
	// signalType: 0 (NB_INACTIVE), 1 (NB_UNVOICED), 2 (TYPE_VOICED)
	p.SignalType = int8(r.Intn(3) * 1) // 0,1,2
	// quantOffsetType: 0 or 1.
	p.QuantOffsetType = int8(r.Intn(2))
	// nb_subfr: 2 or 4.
	if r.Intn(2) == 0 {
		p.NbSubfr = 2
	} else {
		p.NbSubfr = 4
	}
	// subfr_length: typical SILK values (40, 80, 96).
	choices := []int{40, 80, 96, 60}
	p.SubfrLength = choices[r.Intn(len(choices))]
	// SNR_dB_Q7: typical ~10–35 dB → Q7 = 1280..4480.
	p.SNR_dB_Q7 = 1280 + r.Intn(3200)
	// nStatesDelayedDecision: 1–4.
	p.NStatesDelayedDecision = 1 + r.Intn(4)
	// input_tilt_Q15: signed ~±Q15.
	p.InputTiltQ15 = r.Intn(20001) - 10000
	// speech_activity_Q8: [0,256].
	p.SpeechActivityQ8 = r.Intn(257)
	// LastGainIndex: [0, N_LEVELS_QGAIN-1]=63.
	p.LastGainIndex = int8(r.Intn(64))
	// Gains: positive, plausible range e.g. 0.1..10.
	for i := 0; i < p.NbSubfr; i++ {
		p.Gains[i] = 0.1 + r.Float32()*20.0
		// ResNrg: positive, plausible range.
		p.ResNrg[i] = 0.0 + r.Float32()*1e5
	}
	// LTPredCodGain: ~0..20.
	p.LTPredCodGain = r.Float32() * 20.0
	// input_quality, coding_quality: [0,1].
	p.InputQuality = r.Float32()
	p.CodingQuality = r.Float32()
	// condCoding: 0 (CODE_INDEPENDENTLY) or 2 (CODE_CONDITIONALLY).
	if r.Intn(2) == 0 {
		p.CondCoding = 0
	} else {
		p.CondCoding = 2
	}
	return p
}

func TestParity_SilkProcessGainsFLP(t *testing.T) {
	r := rand.New(rand.NewSource(20260418))
	for run := 0; run < 200; run++ {
		p := randProcessGainsPayload(r)
		co := cSilkProcessGainsFLP(p)
		go_ := nativeopus.ExportTestSilkProcessGainsFLP(p)
		if co != go_ {
			t.Errorf("run=%d mismatch:\n C: %+v\nGo: %+v", run, co, go_)
			return
		}
	}
}

// -------------------- process_NLSFs_FLP --------------------

// Generate a valid sorted NLSF vector at Q15 — the codebook expects
// monotonic increasing values in [0, 32768).
func randSortedNLSF(r *rand.Rand, n int) []int16 {
	raw := make([]int16, n)
	// Uniformly spaced with jitter.
	step := int32(32768 / (n + 1))
	for i := 0; i < n; i++ {
		base := step*int32(i+1) + int32(r.Intn(int(step/2))-int(step/4))
		if base < 1 {
			base = 1
		}
		if base > 32767 {
			base = 32767
		}
		raw[i] = int16(base)
	}
	return raw
}

func TestParity_SilkProcessNLSFsFLP(t *testing.T) {
	r := rand.New(rand.NewSource(20260418 + 1))
	for _, wb := range []bool{false, true} {
		order := 10
		if wb {
			order = 16
		}
		for _, interpCoef := range []int{0, 1, 2, 3, 4} {
			for _, uiNLSF := range []int{0, 1} {
				// Skip illegal combination: celt_assert requires
				// useInterpolatedNLSFs==1 OR NLSFInterpCoef_Q2==4.
				if uiNLSF == 0 && interpCoef != 4 {
					continue
				}
				for _, sigType := range []int{0, 1, 2} {
					for _, nb := range []int{2, 4} {
						for run := 0; run < 2; run++ {
							nlsf := randSortedNLSF(r, order)
							prev := randSortedNLSF(r, order)
							speech := r.Intn(257)
							survivors := 2 + r.Intn(8)
							co := cSilkProcessNLSFsFLP(wb, speech, uiNLSF, interpCoef,
								sigType, nb, survivors, nlsf, prev)
							predA, predB, nlsfOut, idx :=
								nativeopus.ExportTestSilkProcessNLSFsFLP(wb, speech, uiNLSF, interpCoef,
									sigType, nb, survivors, nlsf, prev)
							// Compare predA/predB only up to order (trailing positions
							// are uninitialized stack garbage in C).
							for i := 0; i < order; i++ {
								if co.PredA[i] != predA[i] {
									t.Errorf("wb=%v ic=%d ui=%d st=%d nb=%d run=%d predA[%d]: C=%g Go=%g",
										wb, interpCoef, uiNLSF, sigType, nb, run, i, co.PredA[i], predA[i])
									return
								}
								if co.PredB[i] != predB[i] {
									t.Errorf("wb=%v ic=%d ui=%d st=%d nb=%d run=%d predB[%d]: C=%g Go=%g",
										wb, interpCoef, uiNLSF, sigType, nb, run, i, co.PredB[i], predB[i])
									return
								}
							}
							for i := 0; i < 16; i++ {
								if co.NLSFOut[i] != nlsfOut[i] {
									t.Errorf("wb=%v ic=%d ui=%d nlsf[%d]: C=%d Go=%d",
										wb, interpCoef, uiNLSF, i, co.NLSFOut[i], nlsfOut[i])
									return
								}
							}
							for i := 0; i < 17; i++ {
								if co.Indices[i] != idx[i] {
									t.Errorf("wb=%v ic=%d ui=%d idx[%d]: C=%d Go=%d",
										wb, interpCoef, uiNLSF, i, co.Indices[i], idx[i])
									return
								}
							}
						}
					}
				}
			}
		}
	}
}

// -------------------- quant_LTP_gains_FLP --------------------

func TestParity_SilkQuantLTPGainsFLP(t *testing.T) {
	r := rand.New(rand.NewSource(20260418 + 2))
	for _, nb := range []int{2, 4} {
		for _, sl := range []int{40, 60, 80, 96} {
			for run := 0; run < 10; run++ {
				XX := randF32(r, nb*5*5, 20.0)
				xX := randF32(r, nb*5, 5.0)
				slog := int32(r.Intn(1000))
				co := cSilkQuantLTPGainsFLP(XX, xX, sl, nb, slog)
				go_ := nativeopus.ExportTestSilkQuantLTPGainsFLP(XX, xX, sl, nb, slog)
				if co.PeriodicityIdx != go_.PeriodicityIdx {
					t.Errorf("nb=%d sl=%d run=%d periodicity: C=%d Go=%d", nb, sl, run, co.PeriodicityIdx, go_.PeriodicityIdx)
					return
				}
				if co.SumLogGainQ7 != go_.SumLogGainQ7 {
					t.Errorf("nb=%d sl=%d run=%d sumLogGain: C=%d Go=%d", nb, sl, run, co.SumLogGainQ7, go_.SumLogGainQ7)
					return
				}
				if co.PredGainDB != go_.PredGainDB {
					t.Errorf("nb=%d sl=%d run=%d predGainDB: C=%g Go=%g", nb, sl, run, co.PredGainDB, go_.PredGainDB)
					return
				}
				// B and cbk_index are only written for the first nb_subfr entries.
				for i := 0; i < nb*5; i++ {
					if co.B[i] != go_.B[i] {
						t.Errorf("nb=%d sl=%d run=%d B[%d]: C=%g Go=%g", nb, sl, run, i, co.B[i], go_.B[i])
						return
					}
				}
				for i := 0; i < nb; i++ {
					if co.CbkIndex[i] != go_.CbkIndex[i] {
						t.Errorf("nb=%d sl=%d run=%d cbk[%d]: C=%d Go=%d", nb, sl, run, i, co.CbkIndex[i], go_.CbkIndex[i])
						return
					}
				}
			}
		}
	}
}

// -------------------- NSQ_wrapper_FLP --------------------

func randNSQWrapperPayload(r *rand.Rand) (NSQWrapperPayload, nativeopus.SilkNSQWrapperFLPPayload) {
	var cP NSQWrapperPayload
	var gP nativeopus.SilkNSQWrapperFLPPayload
	// fs_kHz in {8,12,16} with subfr_length = 5*fs_kHz; SILK couples
	// NB/MB (8/12 kHz) → predictLPC=10, WB (16 kHz) → predictLPC=16.
	fsKHz := []int{8, 12, 16}[r.Intn(3)]
	lpcOrder := 16
	if fsKHz != 16 {
		lpcOrder = 10
	}
	shapeOrder := 16
	if lpcOrder == 10 {
		shapeOrder = 12
	}
	nb := 2
	if r.Intn(2) == 1 {
		nb = 4
	}
	sl := 5 * fsKHz
	frameLen := sl * nb
	ltpMem := 20 * fsKHz // LTP_MEM_LENGTH_MS * fs_kHz
	// Delayed-decision vs simple NSQ path: driven by
	// nStatesDelayedDecision>1 or warping_Q16>0.
	var nStates int
	var warping int
	if r.Intn(2) == 0 {
		nStates = 1
		warping = 0
	} else {
		nStates = 2 + r.Intn(3)
		warping = r.Intn(3000)
	}
	// Signal type.
	sigType := int8(r.Intn(3))
	// LTP_scaleIndex must be in [0,2] since silk_LTPScales_table_Q14 has 3 entries.
	ltpIdx := int8(r.Intn(3))
	seed := int8(r.Intn(4))
	// NLSFInterpCoef_Q2 can be any 0..4.
	nlInt := int8(r.Intn(5))
	perIdx := int8(r.Intn(3))

	setBoth := func(f func(c *NSQWrapperPayload, g *nativeopus.SilkNSQWrapperFLPPayload)) {
		f(&cP, &gP)
	}

	setBoth(func(c *NSQWrapperPayload, g *nativeopus.SilkNSQWrapperFLPPayload) {
		c.SignalType, g.SignalType = sigType, sigType
		c.QuantOffsetType, g.QuantOffsetType = 0, 0
		c.LTPScaleIndex, g.LTPScaleIndex = ltpIdx, ltpIdx
		c.Seed, g.Seed = seed, seed
		c.NLSFInterpCoefQ2, g.NLSFInterpCoefQ2 = nlInt, nlInt
		c.PERIndex, g.PERIndex = perIdx, perIdx
		c.NbSubfr, g.NbSubfr = nb, nb
		c.FrameLength, g.FrameLength = frameLen, frameLen
		c.SubfrLength, g.SubfrLength = sl, sl
		c.LtpMemLength, g.LtpMemLength = ltpMem, ltpMem
		c.ShapingLPCOrder, g.ShapingLPCOrder = shapeOrder, shapeOrder
		c.PredictLPCOrder, g.PredictLPCOrder = lpcOrder, lpcOrder
		c.NStatesDelayedDecision, g.NStatesDelayedDecision = nStates, nStates
		c.WarpingQ16, g.WarpingQ16 = warping, warping
		c.Arch, g.Arch = 0, 0
	})

	for i := 0; i < 4*24; i++ {
		v := (r.Float32()*2 - 1) * 0.5
		cP.AR[i] = v
		gP.AR[i] = v
	}
	for i := 0; i < 4; i++ {
		v := (r.Float32()*2 - 1) * 0.5
		cP.LFMAShp[i], gP.LFMAShp[i] = v, v
		v = (r.Float32()*2 - 1) * 0.5
		cP.LFARShp[i], gP.LFARShp[i] = v, v
		v = (r.Float32()*2 - 1) * 0.2
		cP.Tilt[i], gP.Tilt[i] = v, v
		v = r.Float32() * 0.5
		cP.HarmShapeGain[i], gP.HarmShapeGain[i] = v, v
		v = 0.5 + r.Float32()*10.0
		cP.Gains[i], gP.Gains[i] = v, v
	}
	// Pitch lag generation mirroring real SILK pitch-analyzer output:
	// a single base lag refined per-subframe via codebook offsets from
	// silk_CB_lags_stage2/3 (range ≈ ±10 samples). We use base + uniform
	// ±pitchJitter and clamp lag(k) <= lag(0) so subfr 0's rewhitened
	// sLTP_Q15 write range always covers later subframes' gain_adj
	// reads — the same invariant real SILK pitch tracking upholds and
	// that libopus NSQ implicitly relies on (violating it reads
	// uninitialized stack in silk_NSQ_c, which is upstream C UB and
	// not something we should synthesize in fuzz inputs).
	const pitchJitter = 10
	minLag := sl + lpcOrder + 4
	maxLag := ltpMem - lpcOrder - 2 - 4
	if maxLag <= minLag+pitchJitter {
		maxLag = minLag + pitchJitter + 1
	}
	baseLag := minLag + pitchJitter + r.Intn(maxLag-minLag-pitchJitter)
	cP.PitchL[0], gP.PitchL[0] = int32(baseLag), int32(baseLag)
	for i := 1; i < 4; i++ {
		// offset ∈ [-pitchJitter, 0] so lag(k) ≤ lag(0).
		pl := baseLag - r.Intn(pitchJitter+1)
		if pl < minLag {
			pl = minLag
		}
		cP.PitchL[i], gP.PitchL[i] = int32(pl), int32(pl)
	}
	lam := 0.1 + r.Float32()*1.9
	cP.Lambda, gP.Lambda = lam, lam
	for i := 0; i < 5*4; i++ {
		v := (r.Float32()*2 - 1) * 0.5
		cP.LTPCoef[i], gP.LTPCoef[i] = v, v
	}
	for j := 0; j < 2; j++ {
		for i := 0; i < 16; i++ {
			v := (r.Float32()*2 - 1) * 0.5
			cP.PredCoef[j*16+i] = v
			gP.PredCoef[j][i] = v
		}
	}

	// NSQ state: zero — matches a freshly-initialized NSQ. The wrapper
	// path cares about sLTP_buf_idx / sLTP_shp_buf_idx in bounds, and
	// those are written by the NSQ kernel before being read again.
	cP.NSQRandSeed = 0
	gP.NSQRandSeed = 0
	cP.NSQLagPrev = 0
	gP.NSQLagPrev = 0
	cP.NSQPrevGainQ16 = 65536 // 1.0 in Q16
	gP.NSQPrevGainQ16 = 65536

	// Input signal — int-valued range to avoid out-of-int16 silk_float2int.
	cP.X = make([]float32, frameLen)
	gP.X = make([]float32, frameLen)
	for i := 0; i < frameLen; i++ {
		v := float32(r.Intn(32767) - 16383)
		cP.X[i], gP.X[i] = v, v
	}
	cP.Pulses = make([]int8, frameLen)
	gP.Pulses = make([]int8, frameLen)
	return cP, gP
}

func TestParity_SilkNSQWrapperFLP(t *testing.T) {
	r := rand.New(rand.NewSource(20260418 + 3))
	for run := 0; run < 200; run++ {
		cP, gP := randNSQWrapperPayload(r)
		co := cSilkNSQWrapperFLP(cP)
		go_ := nativeopus.ExportTestSilkNSQWrapperFLP(gP)
		for i := range co.Pulses {
			if co.Pulses[i] != go_.Pulses[i] {
				t.Errorf("run=%d pulses[%d]: C=%d Go=%d", run, i, co.Pulses[i], go_.Pulses[i])
				return
			}
		}
		if co.SignalType != go_.SignalType ||
			co.QuantOffsetType != go_.QuantOffsetType ||
			co.LTPScaleIndex != go_.LTPScaleIndex ||
			co.Seed != go_.Seed {
			t.Errorf("run=%d index mismatch\n C: st=%d qo=%d ltp=%d seed=%d\nGo: st=%d qo=%d ltp=%d seed=%d",
				run, co.SignalType, co.QuantOffsetType, co.LTPScaleIndex, co.Seed,
				go_.SignalType, go_.QuantOffsetType, go_.LTPScaleIndex, go_.Seed)
			return
		}
		if co.OutNSQLagPrev != go_.OutNSQLagPrev ||
			co.OutNSQPrevGainQ16 != go_.OutNSQPrevGainQ16 ||
			co.OutNSQSLTPBufIdx != go_.OutNSQSLTPBufIdx ||
			co.OutNSQSLTPShpBufIdx != go_.OutNSQSLTPShpBufIdx ||
			co.OutNSQRewhiteFlag != go_.OutNSQRewhiteFlag ||
			co.OutNSQSLFARShpQ14 != go_.OutNSQSLFARShpQ14 ||
			co.OutNSQSDiffShpQ14 != go_.OutNSQSDiffShpQ14 {
			t.Errorf("run=%d NSQ bookkeeping mismatch\n C: %+v\nGo: %+v", run, co, go_)
			return
		}
		// rand_seed can be compared too — it's deterministic.
		if co.OutNSQRandSeed != go_.OutNSQRandSeed {
			t.Errorf("run=%d NSQ rand_seed mismatch: C=%d Go=%d", run, co.OutNSQRandSeed, go_.OutNSQRandSeed)
			return
		}
	}
}
