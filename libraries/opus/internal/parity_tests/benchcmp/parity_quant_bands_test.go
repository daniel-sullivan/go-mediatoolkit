//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_Amp2Log2 — celt_log2_db + mean subtraction over the band
// energies.
func TestParity_Amp2Log2(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	r := rand.New(rand.NewSource(71))

	for _, C_ := range []int{1, 2} {
		for run := 0; run < 5; run++ {
			effEnd := 5 + r.Intn(nb-5)
			end := nb
			bandE := make([]float32, C_*nb)
			for i := range bandE {
				bandE[i] = 0.01 + r.Float32()*1000
			}
			cLog := make([]float32, C_*nb)
			gLog := make([]float32, C_*nb)
			cAmp2Log2(cm, effEnd, end, append([]float32(nil), bandE...), cLog, C_)
			nativeopus.ExportTestAmp2Log2(gm, effEnd, end, append([]float32(nil), bandE...), gLog, C_)
			for i := 0; i < C_*nb; i++ {
				if cLog[i] != gLog[i] {
					t.Errorf("amp2Log2 C=%d run=%d [%d]: C=%g Go=%g (%d ULP)",
						C_, run, i, cLog[i], gLog[i], ulpDiffF32(cLog[i], gLog[i]))
					break
				}
			}
		}
	}
}

// TestParity_QuantCoarseEnergy — encode coarse energies with both
// sides; assert identical oldEBands, error, and byte stream.
func TestParity_QuantCoarseEnergy(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	r := rand.New(rand.NewSource(79))

	for _, C_ := range []int{1, 2} {
		for _, LM := range []int{0, 1, 2, 3} {
			for run := 0; run < 3; run++ {
				start := 0
				end := nb
				effEnd := nb
				budget := uint32(2000 + r.Intn(8000))
				nbAvailable := int(budget >> 6)
				forceIntra := 0
				if r.Intn(3) == 0 {
					forceIntra = 1
				}
				lossRate := r.Intn(50)

				eBands := make([]float32, C_*nb)
				oldE0 := make([]float32, C_*nb)
				for i := range eBands {
					eBands[i] = -8 + r.Float32()*24
					oldE0[i] = -8 + r.Float32()*24
				}
				delayed := r.Float32() * 20

				// C side.
				oldEC := append([]float32(nil), oldE0...)
				errC := make([]float32, C_*nb)
				cBuf := make([]byte, 4096)
				cE := cEcEncNew(cBuf)
				diC := delayed
				cQuantCoarseEnergy(cm, start, end, effEnd,
					append([]float32(nil), eBands...), oldEC, budget,
					errC, cE, C_, LM, nbAvailable, forceIntra,
					&diC, 1, lossRate, 0)

				// Go side.
				oldEG := append([]float32(nil), oldE0...)
				errG := make([]float32, C_*nb)
				goBuf := make([]byte, 4096)
				gE := nativeopus.ExportTestEcEncNew(goBuf)
				diG := delayed
				nativeopus.ExportTestQuantCoarseEnergy(gm, start, end, effEnd,
					append([]float32(nil), eBands...), oldEG, budget,
					errG, gE, C_, LM, nbAvailable, forceIntra,
					&diG, 1, lossRate, 0)

				if diC != diG {
					t.Errorf("C=%d LM=%d run=%d delayedIntra C=%g Go=%g",
						C_, LM, run, diC, diG)
				}
				for i := 0; i < C_*nb; i++ {
					if oldEC[i] != oldEG[i] {
						t.Errorf("C=%d LM=%d run=%d oldEBands[%d] C=%g Go=%g",
							C_, LM, run, i, oldEC[i], oldEG[i])
						break
					}
					if errC[i] != errG[i] {
						t.Errorf("C=%d LM=%d run=%d error[%d] C=%g Go=%g",
							C_, LM, run, i, errC[i], errG[i])
						break
					}
				}
				if cE.Rng() != nativeopus.ExportTestEcRng(gE) ||
					cE.Val() != nativeopus.ExportTestEcVal(gE) {
					t.Errorf("C=%d LM=%d run=%d: ec state diverged", C_, LM, run)
				}
				cE.EncDone()
				nativeopus.ExportTestEcEncDone(gE)
				if !bytes.Equal(cBuf, goBuf) {
					t.Errorf("C=%d LM=%d run=%d: ec bytes differ", C_, LM, run)
				}
				cE.Free()
			}
		}
	}
}

// TestParity_QuantFineEnergy — fine-energy encode parity.
func TestParity_QuantFineEnergy(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	r := rand.New(rand.NewSource(83))

	for _, C_ := range []int{1, 2} {
		for run := 0; run < 5; run++ {
			extra := make([]int, nb)
			for i := range extra {
				extra[i] = r.Intn(5)
			}
			oldE0 := make([]float32, C_*nb)
			err0 := make([]float32, C_*nb)
			for i := range oldE0 {
				oldE0[i] = -4 + r.Float32()*8
				err0[i] = -0.5 + r.Float32()
			}

			oldEC := append([]float32(nil), oldE0...)
			errC := append([]float32(nil), err0...)
			oldEG := append([]float32(nil), oldE0...)
			errG := append([]float32(nil), err0...)
			cBuf := make([]byte, 4096)
			goBuf := make([]byte, 4096)
			cE := cEcEncNew(cBuf)
			gE := nativeopus.ExportTestEcEncNew(goBuf)

			cQuantFineEnergy(cm, 0, nb, oldEC, errC, nil, extra, cE, C_)
			nativeopus.ExportTestQuantFineEnergy(gm, 0, nb, oldEG, errG, nil, extra, gE, C_)

			for i := 0; i < C_*nb; i++ {
				if oldEC[i] != oldEG[i] || errC[i] != errG[i] {
					t.Errorf("C=%d run=%d [%d]: oldE C=%g Go=%g err C=%g Go=%g",
						C_, run, i, oldEC[i], oldEG[i], errC[i], errG[i])
					break
				}
			}
			cE.EncDone()
			nativeopus.ExportTestEcEncDone(gE)
			if !bytes.Equal(cBuf, goBuf) {
				t.Errorf("C=%d run=%d: fine bytes differ", C_, run)
			}
			cE.Free()
		}
	}
}

// TestParity_CoarseRoundTrip — encode with C, decode with both and
// compare oldEBands output.
func TestParity_CoarseRoundTrip(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	r := rand.New(rand.NewSource(89))

	for _, C_ := range []int{1, 2} {
		for _, LM := range []int{0, 1, 2, 3} {
			for run := 0; run < 2; run++ {
				eBands := make([]float32, C_*nb)
				oldE0 := make([]float32, C_*nb)
				for i := range eBands {
					eBands[i] = -8 + r.Float32()*24
					oldE0[i] = -8 + r.Float32()*24
				}
				budget := uint32(3000 + r.Intn(6000))

				// Encode with C.
				oldEEnc := append([]float32(nil), oldE0...)
				errEnc := make([]float32, C_*nb)
				cBuf := make([]byte, 4096)
				cE := cEcEncNew(cBuf)
				di := float32(1.0)
				cQuantCoarseEnergy(cm, 0, nb, nb,
					append([]float32(nil), eBands...), oldEEnc, budget,
					errEnc, cE, C_, LM, int(budget>>6), 0, &di, 1, 20, 0)
				cE.EncDone()
				cE.Free()

				// Decode with both.
				oldEDecC := append([]float32(nil), oldE0...)
				oldEDecG := append([]float32(nil), oldE0...)
				cD := cEcDecNew(cBuf)
				gD := nativeopus.ExportTestEcDecNew(cBuf)
				cUnquantCoarseEnergy(cm, 0, nb, oldEDecC, 0, cD, C_, LM)
				nativeopus.ExportTestUnquantCoarseEnergy(gm, 0, nb, oldEDecG, 0, gD, C_, LM)
				cD.Free()

				for i := 0; i < C_*nb; i++ {
					if oldEDecC[i] != oldEDecG[i] {
						t.Errorf("C=%d LM=%d run=%d [%d]: oldE decoded C=%g Go=%g",
							C_, LM, run, i, oldEDecC[i], oldEDecG[i])
						break
					}
				}
			}
		}
	}
}
