//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// loadGoMode mirrors the fields of a C-built CELTMode into a Go
// OpusCustomMode so that both sides see identical inputs.
func loadGoMode(t *testing.T, Fs, frameSize int) (cMode, nativeopus.CeltModeHandle) {
	t.Helper()
	cm := cModeGet(Fs, frameSize)
	if cm.p == nil {
		t.Fatalf("opus_custom_mode_create(%d,%d) failed", Fs, frameSize)
	}
	nb := cm.NbEBands()
	eBands := make([]int16, nb+1)
	for i := range eBands {
		eBands[i] = cm.EBand(i)
	}
	logN := make([]int16, nb)
	for i := range logN {
		logN[i] = cm.LogN(i)
	}
	av := make([]byte, cm.AllocVecLen())
	for i := range av {
		av[i] = cm.AllocVec(i)
	}
	cacheBits := make([]byte, cm.CacheSize())
	for i := range cacheBits {
		cacheBits[i] = cm.CacheBit(i)
	}
	cacheIdx := make([]int16, cm.CacheIdxLen())
	for i := range cacheIdx {
		cacheIdx[i] = cm.CacheIdx(i)
	}
	// caps length = (maxLM+1) * 2 * nbEBands.
	capsLen := (cm.MaxLM() + 1) * 2 * cm.NbEBands()
	cacheCaps := make([]byte, capsLen)
	for i := range cacheCaps {
		cacheCaps[i] = cm.CacheCap(i)
	}
	gm := nativeopus.NewCeltModeFromData(
		int32(cm.Fs()), cm.Overlap(),
		cm.NbEBands(), cm.EffEBands(),
		eBands,
		cm.MaxLM(), cm.NbShortMdcts(), cm.ShortMdctSize(),
		cm.NbAllocVectors(), av,
		logN,
		cm.CacheSize(), cacheIdx, cacheBits, cacheCaps,
	)
	return cm, gm
}

func TestParity_GetPulses(t *testing.T) {
	for i := 0; i < 64; i++ {
		c := cGetPulses(i)
		g := nativeopus.ExportTestGetPulses(i)
		if c != g {
			t.Errorf("get_pulses(%d): C=%d Go=%d", i, c, g)
		}
	}
}

func TestParity_Bits2Pulses_Pulses2Bits(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	// LM range 0..3 for 48kHz standard mode (2.5 / 5 / 10 / 20 ms).
	for LM := 0; LM <= 3; LM++ {
		for band := 0; band < cm.NbEBands(); band++ {
			for bits := 0; bits <= 512; bits += 7 {
				c := cBits2Pulses(cm, band, LM, bits)
				g := nativeopus.ExportTestBits2Pulses(gm, band, LM, bits)
				if c != g {
					t.Errorf("bits2pulses band=%d LM=%d bits=%d: C=%d Go=%d",
						band, LM, bits, c, g)
				}
			}
			// pulses2bits iterates up to cache[0].
			maxP := nativeopus.ExportTestBits2Pulses(gm, band, LM, 1<<20)
			for p := 0; p <= maxP; p++ {
				c := cPulses2Bits(cm, band, LM, p)
				g := nativeopus.ExportTestPulses2Bits(gm, band, LM, p)
				if c != g {
					t.Errorf("pulses2bits band=%d LM=%d p=%d: C=%d Go=%d",
						band, LM, p, c, g)
				}
			}
		}
	}
}

// makeCaps mirrors the per-band cap formula used by celt_encoder.c
// and celt_decoder.c:
//
//	cap[i] = (cache.caps[nbEBands*(2*LM+C-1) + i] + 64)*C*N >> 2
//
// where N = (eBands[i+1]-eBands[i]) << LM.
func makeCaps(m cMode, C, LM int) []int {
	nb := m.NbEBands()
	out := make([]int, nb)
	for i := 0; i < nb; i++ {
		N := int(m.EBand(i+1)-m.EBand(i)) << LM
		idx := nb*(2*LM+C-1) + i
		out[i] = (int(m.CacheCap(idx)) + 64) * C * N >> 2
	}
	return out
}

// TestParity_ComputeAllocation_Encode — run the bisection + fine-bit
// split on the encoder side with random (C, LM, total, alloc_trim,
// offsets) and assert identical outputs + identical ec_enc state.
func TestParity_ComputeAllocation_Encode(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()

	r := rand.New(rand.NewSource(17))
	for run := 0; run < 40; run++ {
		C_ := 1 + r.Intn(2)
		LM := r.Intn(4)
		start := 0
		end := nb
		allocTrim := r.Intn(11)
		prev := r.Intn(nb)
		signalBW := r.Intn(nb)
		// Realistic packet sizes: 1..120 bytes per channel per frame.
		total := int32(64+r.Intn(8000)) << 3
		offsets := make([]int, nb)
		for i := range offsets {
			if r.Intn(4) == 0 {
				offsets[i] = (r.Intn(9) - 4) << 3
			}
		}
		cap_ := makeCaps(cm, C_, LM)
		intensity := r.Intn(nb + 1)
		dualStereo := r.Intn(2)

		cBuf := make([]byte, 4096)
		goBuf := make([]byte, 4096)
		cE := cEcEncNew(cBuf)
		gE := nativeopus.ExportTestEcEncNew(goBuf)

		pulsesC := make([]int, nb)
		ebitsC := make([]int, nb)
		fpC := make([]int, nb)
		pulsesG := make([]int, nb)
		ebitsG := make([]int, nb)
		fpG := make([]int, nb)

		intC := intensity
		intG := intensity
		dsC := dualStereo
		dsG := dualStereo
		balC := int32(0)
		balG := int32(0)

		cbC := cComputeAllocation(cm, start, end, offsets, cap_, allocTrim,
			&intC, &dsC, total, &balC,
			pulsesC, ebitsC, fpC, C_, LM, cE, 1, prev, signalBW)
		cbG := nativeopus.ExportTestCltComputeAllocation(gm, start, end,
			offsets, cap_, allocTrim, &intG, &dsG, total, &balG,
			pulsesG, ebitsG, fpG, C_, LM, gE, 1, prev, signalBW)

		if cbC != cbG {
			t.Errorf("run %d: codedBands C=%d Go=%d (C=%d LM=%d trim=%d total=%d)",
				run, cbC, cbG, C_, LM, allocTrim, total)
		}
		if intC != intG || dsC != dsG || balC != balG {
			t.Errorf("run %d: intensity/ds/balance C=(%d,%d,%d) Go=(%d,%d,%d)",
				run, intC, dsC, balC, intG, dsG, balG)
		}
		for i := 0; i < nb; i++ {
			if pulsesC[i] != pulsesG[i] || ebitsC[i] != ebitsG[i] || fpC[i] != fpG[i] {
				t.Errorf("run %d band %d: pulses/ebits/fp C=(%d,%d,%d) Go=(%d,%d,%d)",
					run, i, pulsesC[i], ebitsC[i], fpC[i],
					pulsesG[i], ebitsG[i], fpG[i])
				break
			}
		}
		if cE.Rng() != nativeopus.ExportTestEcRng(gE) ||
			cE.Val() != nativeopus.ExportTestEcVal(gE) {
			t.Errorf("run %d: ec state diverged", run)
		}
		cE.EncDone()
		nativeopus.ExportTestEcEncDone(gE)
		if !bytes.Equal(cBuf, goBuf) {
			t.Errorf("run %d: ec bytes differ", run)
		}
		cE.Free()
	}
}

// TestParity_ComputeAllocation_RoundTrip — encode with C, decode with
// both and confirm both sides agree on the allocation produced by the
// decoded ec state.
func TestParity_ComputeAllocation_RoundTrip(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()

	r := rand.New(rand.NewSource(23))
	for run := 0; run < 20; run++ {
		C_ := 1 + r.Intn(2)
		LM := r.Intn(4)
		allocTrim := r.Intn(11)
		prev := r.Intn(nb)
		signalBW := r.Intn(nb)
		total := int32(64+r.Intn(8000)) << 3
		offsets := make([]int, nb)
		for i := range offsets {
			if r.Intn(4) == 0 {
				offsets[i] = (r.Intn(9) - 4) << 3
			}
		}
		cap_ := makeCaps(cm, C_, LM)
		intensity := r.Intn(nb + 1)
		dualStereo := r.Intn(2)

		cBuf := make([]byte, 4096)
		cE := cEcEncNew(cBuf)
		pulsesX := make([]int, nb)
		ebitsX := make([]int, nb)
		fpX := make([]int, nb)
		intX := intensity
		dsX := dualStereo
		balX := int32(0)
		cComputeAllocation(cm, 0, nb, offsets, cap_, allocTrim,
			&intX, &dsX, total, &balX, pulsesX, ebitsX, fpX,
			C_, LM, cE, 1, prev, signalBW)
		cE.EncDone()
		cE.Free()

		// Decode with both.
		cD := cEcDecNew(cBuf)
		gD := nativeopus.ExportTestEcDecNew(cBuf)
		pulsesC := make([]int, nb)
		ebitsC := make([]int, nb)
		fpC := make([]int, nb)
		pulsesG := make([]int, nb)
		ebitsG := make([]int, nb)
		fpG := make([]int, nb)
		intC, intG := intensity, intensity
		dsC, dsG := dualStereo, dualStereo
		balC, balG := int32(0), int32(0)
		cbC := cComputeAllocation(cm, 0, nb, offsets, cap_, allocTrim,
			&intC, &dsC, total, &balC,
			pulsesC, ebitsC, fpC, C_, LM, cD, 0, prev, signalBW)
		cbG := nativeopus.ExportTestCltComputeAllocation(gm, 0, nb,
			offsets, cap_, allocTrim, &intG, &dsG, total, &balG,
			pulsesG, ebitsG, fpG, C_, LM, gD, 0, prev, signalBW)
		if cbC != cbG || intC != intG || dsC != dsG || balC != balG {
			t.Errorf("run %d: decode header C=(cb=%d,int=%d,ds=%d,bal=%d) Go=(cb=%d,int=%d,ds=%d,bal=%d)",
				run, cbC, intC, dsC, balC, cbG, intG, dsG, balG)
		}
		for i := 0; i < nb; i++ {
			if pulsesC[i] != pulsesG[i] || ebitsC[i] != ebitsG[i] || fpC[i] != fpG[i] {
				t.Errorf("run %d band %d: pulses/ebits/fp mismatch", run, i)
				break
			}
		}
		cD.Free()
	}
}
