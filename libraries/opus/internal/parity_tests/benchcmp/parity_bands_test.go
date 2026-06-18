//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func TestParity_BitexactCos(t *testing.T) {
	for x := int16(0); x <= 16384; x++ {
		c := cBitexactCos(x)
		g := nativeopus.ExportTestBitexactCos(x)
		if c != g {
			t.Errorf("bitexact_cos(%d): C=%d Go=%d", x, c, g)
			break
		}
	}
}

func TestParity_BitexactLog2Tan(t *testing.T) {
	// isin, icos are "positive" inputs from bitexact_cos output.
	for isin := 1; isin < 32768; isin += 31 {
		for icos := 1; icos < 32768; icos += 37 {
			c := cBitexactLog2Tan(isin, icos)
			g := nativeopus.ExportTestBitexactLog2Tan(isin, icos)
			if c != g {
				t.Errorf("log2tan(%d,%d): C=%d Go=%d", isin, icos, c, g)
				return
			}
		}
	}
}

func TestParity_CeltLcgRand(t *testing.T) {
	var seed uint32 = 0xdeadbeef
	for i := 0; i < 100; i++ {
		c := cCeltLcgRand(seed)
		g := nativeopus.ExportTestCeltLcgRand(seed)
		if c != g {
			t.Errorf("lcg_rand(%x): C=%x Go=%x", seed, c, g)
			return
		}
		seed = c
	}
}

func TestParity_HysteresisDecision(t *testing.T) {
	thresholds := []float32{-0.5, 0.0, 0.5, 1.0}
	hysteresis := []float32{0.1, 0.1, 0.1, 0.1}
	for _, val := range []float32{-1.0, -0.5, -0.4, 0.0, 0.05, 0.5, 0.6, 1.0, 1.5} {
		for prev := 0; prev <= 4; prev++ {
			c := cHysteresisDecision(val, thresholds, hysteresis, 4, prev)
			g := nativeopus.ExportTestHysteresisDecision(val, thresholds, hysteresis, 4, prev)
			if c != g {
				t.Errorf("hysteresis(%g,prev=%d): C=%d Go=%d", val, prev, c, g)
			}
		}
	}
}

func TestParity_ComputeBandEnergies(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	r := rand.New(rand.NewSource(131))
	for _, LM := range []int{0, 1, 2, 3} {
		for _, C_ := range []int{1, 2} {
			N := cm.ShortMdctSize() << LM
			X := make([]float32, C_*N)
			for i := range X {
				X[i] = (r.Float32()*2 - 1) * 100
			}
			bC := make([]float32, C_*nb)
			bG := make([]float32, C_*nb)
			cComputeBandEnergies(cm, X, bC, nb, C_, LM)
			nativeopus.ExportTestComputeBandEnergies(gm, X, bG, nb, C_, LM)
			for i := 0; i < C_*nb; i++ {
				if bC[i] != bG[i] {
					t.Errorf("LM=%d C=%d [%d]: C=%g Go=%g (%d ULP)",
						LM, C_, i, bC[i], bG[i], ulpDiffF32(bC[i], bG[i]))
					break
				}
			}
		}
	}
}

func TestParity_NormaliseBands(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	r := rand.New(rand.NewSource(137))
	for _, LM := range []int{0, 1, 2, 3} {
		for _, C_ := range []int{1, 2} {
			M := 1 << LM
			N := M * cm.ShortMdctSize()
			freq := make([]float32, C_*N)
			for i := range freq {
				freq[i] = (r.Float32()*2 - 1) * 10
			}
			bandE := make([]float32, C_*nb)
			for i := range bandE {
				bandE[i] = 0.1 + r.Float32()*20
			}
			xC := make([]float32, C_*N)
			xG := make([]float32, C_*N)
			cNormaliseBands(cm, freq, xC, bandE, nb, C_, M)
			nativeopus.ExportTestNormaliseBands(gm, freq, xG, bandE, nb, C_, M)
			for i := 0; i < C_*N; i++ {
				if xC[i] != xG[i] {
					t.Errorf("LM=%d C=%d [%d]: C=%g Go=%g", LM, C_, i, xC[i], xG[i])
					break
				}
			}
		}
	}
}

func TestParity_DenormaliseBands(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	r := rand.New(rand.NewSource(139))
	nb := cm.NbEBands()
	for _, LM := range []int{0, 1, 2, 3} {
		M := 1 << LM
		N := M * cm.ShortMdctSize()
		X := make([]float32, N)
		for i := range X {
			X[i] = (r.Float32()*2 - 1) * 2
		}
		bandLogE := make([]float32, nb)
		for i := range bandLogE {
			bandLogE[i] = -4 + r.Float32()*8
		}
		fC := make([]float32, N)
		fG := make([]float32, N)
		cDenormaliseBands(cm, X, fC, bandLogE, 0, nb, M, 1, 0)
		nativeopus.ExportTestDenormaliseBands(gm, X, fG, bandLogE, 0, nb, M, 1, 0)
		for i := 0; i < N; i++ {
			if fC[i] != fG[i] {
				t.Errorf("LM=%d [%d]: C=%g Go=%g (%d ULP)",
					LM, i, fC[i], fG[i], ulpDiffF32(fC[i], fG[i]))
				break
			}
		}
	}
}

func TestParity_StereoSplit(t *testing.T) {
	r := rand.New(rand.NewSource(143))
	for _, N := range []int{4, 8, 16, 32, 64} {
		X := make([]float32, N)
		Y := make([]float32, N)
		for i := 0; i < N; i++ {
			X[i] = r.Float32()*2 - 1
			Y[i] = r.Float32()*2 - 1
		}
		xC := append([]float32(nil), X...)
		yC := append([]float32(nil), Y...)
		xG := append([]float32(nil), X...)
		yG := append([]float32(nil), Y...)
		cStereoSplit(xC, yC, N)
		nativeopus.ExportTestStereoSplit(xG, yG, N)
		for i := 0; i < N; i++ {
			if xC[i] != xG[i] || yC[i] != yG[i] {
				t.Errorf("N=%d [%d]: X C=%g Go=%g  Y C=%g Go=%g",
					N, i, xC[i], xG[i], yC[i], yG[i])
				break
			}
		}
	}
}

func TestParity_Haar1(t *testing.T) {
	r := rand.New(rand.NewSource(149))
	for _, N0 := range []int{4, 8, 16, 32, 64} {
		for _, stride := range []int{1, 2, 4} {
			X := make([]float32, N0*stride)
			for i := range X {
				X[i] = r.Float32()*2 - 1
			}
			xC := append([]float32(nil), X...)
			xG := append([]float32(nil), X...)
			cHaar1(xC, N0, stride)
			nativeopus.ExportTestHaar1(xG, N0, stride)
			for i := range xC {
				if xC[i] != xG[i] {
					t.Errorf("N0=%d stride=%d [%d]: C=%g Go=%g",
						N0, stride, i, xC[i], xG[i])
					break
				}
			}
		}
	}
}

func TestParity_SpreadingDecision(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	r := rand.New(rand.NewSource(151))
	spreadWeight := make([]int, nb)
	for i := range spreadWeight {
		spreadWeight[i] = 1 + r.Intn(3)
	}
	for _, LM := range []int{0, 1, 2, 3} {
		M := 1 << LM
		for _, C_ := range []int{1, 2} {
			for run := 0; run < 3; run++ {
				N := M * cm.ShortMdctSize()
				X := make([]float32, C_*N)
				for i := range X {
					X[i] = (r.Float32()*2 - 1) * 0.1
				}
				avgC, hfC, tapC := 0, 0, 0
				avgG, hfG, tapG := 0, 0, 0
				cRes := cSpreadingDecision(cm, X, &avgC, 2, &hfC, &tapC, 1, nb, C_, M, spreadWeight)
				gRes := nativeopus.ExportTestSpreadingDecision(gm, X, &avgG, 2, &hfG, &tapG, 1, nb, C_, M, spreadWeight)
				if cRes != gRes || avgC != avgG || hfC != hfG || tapC != tapG {
					t.Errorf("LM=%d C=%d run=%d: C=(%d,%d,%d,%d) Go=(%d,%d,%d,%d)",
						LM, C_, run, cRes, avgC, hfC, tapC, gRes, avgG, hfG, tapG)
				}
			}
		}
	}
}

// TestParity_AntiCollapse — inject noise into collapsed blocks.
func TestParity_AntiCollapse(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	r := rand.New(rand.NewSource(163))

	for _, LM := range []int{1, 2, 3} {
		for _, C_ := range []int{1, 2} {
			M := 1 << LM
			size := M * cm.ShortMdctSize()
			X := make([]float32, C_*size)
			for i := range X {
				X[i] = r.Float32()*2 - 1
			}
			cm_mask := make([]byte, nb*C_)
			// Simulate partial collapse — set some bits low.
			for i := range cm_mask {
				cm_mask[i] = byte(r.Intn(1 << uint(LM+1)))
			}
			pulses := make([]int, nb)
			for i := range pulses {
				pulses[i] = r.Intn(64)
			}
			logE := make([]float32, 2*nb)
			prev1 := make([]float32, 2*nb)
			prev2 := make([]float32, 2*nb)
			for i := range logE {
				logE[i] = -4 + r.Float32()*8
				prev1[i] = -4 + r.Float32()*8
				prev2[i] = -4 + r.Float32()*8
			}
			seed := uint32(0xfeedface)
			xC := append([]float32(nil), X...)
			xG := append([]float32(nil), X...)
			cmC := append([]byte(nil), cm_mask...)
			cmG := append([]byte(nil), cm_mask...)
			cAntiCollapse(cm, xC, cmC, LM, C_, size, 0, nb,
				logE, prev1, prev2, pulses, seed, 1)
			nativeopus.ExportTestAntiCollapse(gm, xG, cmG, LM, C_, size, 0, nb,
				logE, prev1, prev2, pulses, seed, 1)
			for i := 0; i < C_*size; i++ {
				if xC[i] != xG[i] {
					t.Errorf("LM=%d C=%d [%d]: C=%g Go=%g (%d ULP)",
						LM, C_, i, xC[i], xG[i], ulpDiffF32(xC[i], xG[i]))
					break
				}
			}
		}
	}
}

// TestParity_QuantAllBands_Mono — encode+decode round-trip via
// quant_all_bands using a real bit-allocation.
func TestParity_QuantAllBands_Mono(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	r := rand.New(rand.NewSource(157))

	for _, LM := range []int{0, 1, 2, 3} {
		for run := 0; run < 2; run++ {
			M := 1 << LM
			N := M * cm.ShortMdctSize()
			// Pre-normalise input X (unit-energy per band).
			freq := make([]float32, N)
			for i := range freq {
				freq[i] = r.Float32()*2 - 1
			}
			bandE := make([]float32, nb)
			cComputeBandEnergies(cm, freq, bandE, nb, 1, LM)
			X := make([]float32, N)
			cNormaliseBands(cm, freq, X, bandE, nb, 1, M)

			// Build an allocation with cap from the mode.
			C_ := 1
			cap_ := makeCaps(cm, C_, LM)
			offsets := make([]int, nb)
			// Budget scaled so each band gets a few bits.
			total := int32(4000 + r.Intn(6000))
			pulses := make([]int, nb)
			ebits := make([]int, nb)
			fp := make([]int, nb)
			tfRes := make([]int, nb)
			intensity := 0
			dualStereo := 0
			balance := int32(0)

			// Shared ec_enc for encode side and a header-free bitstream.
			cBuf := make([]byte, 2048)
			cE := cEcEncNew(cBuf)
			codedBands := cComputeAllocation(cm, 0, nb, offsets, cap_, 5,
				&intensity, &dualStereo, total, &balance, pulses, ebits, fp,
				C_, LM, cE, 1, 0, nb-1)

			// Clone pulses/tfRes for the Go side (the C side consumed the
			// ec_enc header already; we'll run quant_all_bands with a
			// continuation of that stream).
			pulsesC := append([]int(nil), pulses...)
			tfResC := append([]int(nil), tfRes...)
			tfResG := append([]int(nil), tfRes...)

			// Pulses only — no extra header. Use simple settings.
			seedC := uint32(0x12345678)
			seedG := seedC
			xC := append([]float32(nil), X...)
			xG := append([]float32(nil), X...)
			cmaskC := make([]byte, nb)
			cmaskG := make([]byte, nb)

			cQuantAllBands(1, cm, 0, nb, xC, nil, cmaskC, bandE, pulsesC,
				0, 2, 0, intensity, tfResC, total, balance, cE,
				LM, codedBands, &seedC, 5, 0)
			cE.EncDone()

			// Encode with Go using a fresh allocation on a Go ec_enc.
			goBuf := make([]byte, 2048)
			gE := nativeopus.ExportTestEcEncNew(goBuf)
			intG := 0
			dsG := 0
			balG := int32(0)
			pulsesG2 := make([]int, nb)
			ebitsG := make([]int, nb)
			fpG := make([]int, nb)
			cbG := nativeopus.ExportTestCltComputeAllocation(gm, 0, nb,
				offsets, cap_, 5, &intG, &dsG, total, &balG,
				pulsesG2, ebitsG, fpG, C_, LM, gE, 1, 0, nb-1)
			if cbG != codedBands {
				t.Errorf("LM=%d run=%d: codedBands C=%d Go=%d", LM, run, codedBands, cbG)
				cE.Free()
				continue
			}
			nativeopus.ExportTestQuantAllBands(1, gm, 0, nb, xG, nil, cmaskG,
				bandE, pulsesG2, 0, 2, 0, intG, tfResG, total, balG, gE,
				LM, cbG, &seedG, 5, 0)
			nativeopus.ExportTestEcEncDone(gE)

			if !bytes.Equal(cBuf, goBuf) {
				t.Errorf("LM=%d run=%d: ec bytes differ", LM, run)
			}
			for i := 0; i < nb; i++ {
				if cmaskC[i] != cmaskG[i] {
					t.Errorf("LM=%d run=%d cmask[%d]: C=%x Go=%x",
						LM, run, i, cmaskC[i], cmaskG[i])
					break
				}
			}
			if seedC != seedG {
				t.Errorf("LM=%d run=%d: seed C=%x Go=%x", LM, run, seedC, seedG)
			}
			cE.Free()
		}
	}
}

// TestParity_QuantAllBands_Stereo — stereo quant_all_bands parity.
// Uses theta_rdo=off (complexity<8) so both sides take the same non-RDO
// path without requiring the ec-save/restore bookkeeping path.
func TestParity_QuantAllBands_Stereo(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	r := rand.New(rand.NewSource(167))

	for _, LM := range []int{0, 1, 2, 3} {
		for run := 0; run < 2; run++ {
			M := 1 << LM
			N := M * cm.ShortMdctSize()
			// Build stereo input, normalise per-band.
			freq := make([]float32, 2*N)
			for i := range freq {
				freq[i] = r.Float32()*2 - 1
			}
			bandE := make([]float32, 2*nb)
			cComputeBandEnergies(cm, freq, bandE, nb, 2, LM)
			XY := make([]float32, 2*N)
			cNormaliseBands(cm, freq, XY, bandE, nb, 2, M)

			C_ := 2
			cap_ := makeCaps(cm, C_, LM)
			offsets := make([]int, nb)
			total := int32(6000 + r.Intn(8000))
			pulses := make([]int, nb)
			ebits := make([]int, nb)
			fp := make([]int, nb)
			tfRes := make([]int, nb)
			intensity := 0
			dualStereo := 0
			balance := int32(0)

			cBuf := make([]byte, 2048)
			cE := cEcEncNew(cBuf)
			codedBands := cComputeAllocation(cm, 0, nb, offsets, cap_, 5,
				&intensity, &dualStereo, total, &balance, pulses, ebits, fp,
				C_, LM, cE, 1, 0, nb-1)

			// Split XY into X,Y slices (stride-2).
			pulsesC := append([]int(nil), pulses...)
			tfResC := append([]int(nil), tfRes...)
			tfResG := append([]int(nil), tfRes...)

			seedC := uint32(0xabcd1234)
			seedG := seedC
			xC := append([]float32(nil), XY[:N]...)
			yC := append([]float32(nil), XY[N:]...)
			xG := append([]float32(nil), xC...)
			yG := append([]float32(nil), yC...)
			cmaskC := make([]byte, nb*2)
			cmaskG := make([]byte, nb*2)

			// complexity=5 → theta_rdo off.
			cQuantAllBands(1, cm, 0, nb, xC, yC, cmaskC, bandE, pulsesC,
				0, 2, dualStereo, intensity, tfResC, total, balance, cE,
				LM, codedBands, &seedC, 5, 0)
			cE.EncDone()

			goBuf := make([]byte, 2048)
			gE := nativeopus.ExportTestEcEncNew(goBuf)
			intG := 0
			dsG := 0
			balG := int32(0)
			pulsesG2 := make([]int, nb)
			ebitsG := make([]int, nb)
			fpG := make([]int, nb)
			cbG := nativeopus.ExportTestCltComputeAllocation(gm, 0, nb,
				offsets, cap_, 5, &intG, &dsG, total, &balG,
				pulsesG2, ebitsG, fpG, C_, LM, gE, 1, 0, nb-1)
			if cbG != codedBands {
				t.Errorf("LM=%d run=%d: codedBands C=%d Go=%d", LM, run, codedBands, cbG)
				cE.Free()
				continue
			}
			nativeopus.ExportTestQuantAllBands(1, gm, 0, nb, xG, yG, cmaskG,
				bandE, pulsesG2, 0, 2, dsG, intG, tfResG, total, balG, gE,
				LM, cbG, &seedG, 5, 0)
			nativeopus.ExportTestEcEncDone(gE)

			if !bytes.Equal(cBuf, goBuf) {
				t.Errorf("LM=%d run=%d: stereo ec bytes differ", LM, run)
			}
			for i := 0; i < 2*nb; i++ {
				if cmaskC[i] != cmaskG[i] {
					t.Errorf("LM=%d run=%d cmask[%d]: C=%x Go=%x",
						LM, run, i, cmaskC[i], cmaskG[i])
					break
				}
			}
			cE.Free()
		}
	}
}

// TestParity_QuantAllBands_Decode — round-trip: encode with C, then
// decode with both sides and compare the recovered normalised
// spectrum and collapse_masks.
func TestParity_QuantAllBands_Decode(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	r := rand.New(rand.NewSource(173))

	for _, LM := range []int{0, 1, 2, 3} {
		M := 1 << LM
		N := M * cm.ShortMdctSize()
		freq := make([]float32, N)
		for i := range freq {
			freq[i] = r.Float32()*2 - 1
		}
		bandE := make([]float32, nb)
		cComputeBandEnergies(cm, freq, bandE, nb, 1, LM)
		X := make([]float32, N)
		cNormaliseBands(cm, freq, X, bandE, nb, 1, M)

		C_ := 1
		cap_ := makeCaps(cm, C_, LM)
		offsets := make([]int, nb)
		total := int32(5000)
		pulses := make([]int, nb)
		ebits := make([]int, nb)
		fp := make([]int, nb)
		tfRes := make([]int, nb)
		intensity := 0
		dualStereo := 0
		balance := int32(0)

		// Encode with C, write a full frame bitstream.
		cBuf := make([]byte, 2048)
		cE := cEcEncNew(cBuf)
		codedBands := cComputeAllocation(cm, 0, nb, offsets, cap_, 5,
			&intensity, &dualStereo, total, &balance, pulses, ebits, fp,
			C_, LM, cE, 1, 0, nb-1)
		seed := uint32(0xc0ffee)
		xEnc := append([]float32(nil), X...)
		cmask := make([]byte, nb)
		cQuantAllBands(1, cm, 0, nb, xEnc, nil, cmask, bandE, pulses,
			0, 2, 0, intensity, tfRes, total, balance, cE,
			LM, codedBands, &seed, 5, 0)
		cE.EncDone()
		cE.Free()

		// Decode with both sides from the same bitstream.
		cD := cEcDecNew(cBuf)
		gD := nativeopus.ExportTestEcDecNew(cBuf)
		// Allocation on the decode side too.
		pulsesC := make([]int, nb)
		ebitsC := make([]int, nb)
		fpC := make([]int, nb)
		pulsesG := make([]int, nb)
		ebitsG := make([]int, nb)
		fpG := make([]int, nb)
		intC, intG2 := 0, 0
		dsC, dsG2 := 0, 0
		balC, balG := int32(0), int32(0)
		cbC := cComputeAllocation(cm, 0, nb, offsets, cap_, 5,
			&intC, &dsC, total, &balC, pulsesC, ebitsC, fpC,
			C_, LM, cD, 0, 0, nb-1)
		cbG := nativeopus.ExportTestCltComputeAllocation(gm, 0, nb,
			offsets, cap_, 5, &intG2, &dsG2, total, &balG,
			pulsesG, ebitsG, fpG, C_, LM, gD, 0, 0, nb-1)
		if cbC != cbG {
			t.Errorf("LM=%d: dec codedBands C=%d Go=%d", LM, cbC, cbG)
			continue
		}
		// Now run quant_all_bands in decode mode on both.
		seedC := uint32(0xc0ffee)
		seedG := uint32(0xc0ffee)
		xC := make([]float32, N)
		xG := make([]float32, N)
		cmaskC := make([]byte, nb)
		cmaskG := make([]byte, nb)
		cQuantAllBands(0, cm, 0, nb, xC, nil, cmaskC, bandE, pulsesC,
			0, 2, 0, intC, tfRes, total, balC, cD,
			LM, cbC, &seedC, 5, 0)
		nativeopus.ExportTestQuantAllBands(0, gm, 0, nb, xG, nil, cmaskG,
			bandE, pulsesG, 0, 2, 0, intG2, tfRes, total, balG, gD,
			LM, cbG, &seedG, 5, 0)
		for i := 0; i < N; i++ {
			if xC[i] != xG[i] {
				t.Errorf("LM=%d [%d]: dec X C=%g Go=%g (%d ULP)",
					LM, i, xC[i], xG[i], ulpDiffF32(xC[i], xG[i]))
				break
			}
		}
		for i := 0; i < nb; i++ {
			if cmaskC[i] != cmaskG[i] {
				t.Errorf("LM=%d [%d]: dec cmask C=%x Go=%x",
					LM, i, cmaskC[i], cmaskG[i])
				break
			}
		}
		cD.Free()
	}
}
