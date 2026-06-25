// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encadjthr

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// ldRand returns a pseudo-random ld64-domain FIXP_DBL. The PE engine works on
// CalcLdData outputs, which are negative-ish small fractions (energies/thresholds
// in ld64), so bias the distribution toward [-1.0, ~0.5) of the Q1.31 range.
func ldRand(r *rand.Rand) int32 {
	// roughly [-0x40000000, 0x10000000)
	return int32(r.Int63n(0x50000000)) - 0x40000000
}

// TestCalcInvLdDataParity asserts calcInvLdData == genuine CalcInvLdData for the
// full fractional input range plus the documented edge cases (0, ±31/64).
func TestCalcInvLdDataParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	edge := []int32{
		0, 1, -1,
		int32(31.0 / 64.0 * 2147483648.0), int32(-31.0/64.0*2147483648.0) - 1,
		0x7FFFFFFF, -0x80000000, 0x40000000, -0x40000000,
	}
	for _, x := range edge {
		assert.Equalf(t, cCalcInvLdData(x), nativeaac.ParityCalcInvLdData(x),
			"CalcInvLdData(%#x)", uint32(x))
	}
	r := rand.New(rand.NewSource(0xA11CE))
	for i := 0; i < 200000; i++ {
		x := int32(r.Uint32())
		assert.Equalf(t, cCalcInvLdData(x), nativeaac.ParityCalcInvLdData(x),
			"CalcInvLdData(%#x)", uint32(x))
	}
}

// TestCalcLdIntParity asserts calcLdInt == genuine CalcLdInt over the table
// range and beyond.
func TestCalcLdIntParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	for i := int32(-5); i < 300; i++ {
		assert.Equalf(t, cCalcLdInt(i), nativeaac.ParityCalcLdInt(i), "CalcLdInt(%d)", i)
	}
}

// TestFMultNormParity asserts fMultNorm (mantissa+exponent) == genuine fMultNorm.
func TestFMultNormParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	pairs := [][2]int32{
		{0, 0}, {0, 5}, {7, 0},
		{-0x80000000, -0x80000000}, {0x7FFFFFFF, 0x7FFFFFFF},
		{-0x80000000, 0x7FFFFFFF}, {1, -1},
	}
	for _, p := range pairs {
		cm, ce := cFMultNorm(p[0], p[1])
		gm, ge := nativeaac.ParityFMultNorm(p[0], p[1])
		assert.Equalf(t, cm, gm, "fMultNorm(%#x,%#x) mantissa", uint32(p[0]), uint32(p[1]))
		assert.Equalf(t, ce, ge, "fMultNorm(%#x,%#x) exp", uint32(p[0]), uint32(p[1]))
	}
	r := rand.New(rand.NewSource(0xBEEF))
	for i := 0; i < 100000; i++ {
		f1, f2 := int32(r.Uint32()), int32(r.Uint32())
		cm, ce := cFMultNorm(f1, f2)
		gm, ge := nativeaac.ParityFMultNorm(f1, f2)
		require.Equalf(t, cm, gm, "fMultNorm(%#x,%#x) mantissa", uint32(f1), uint32(f2))
		require.Equalf(t, ce, ge, "fMultNorm(%#x,%#x) exp", uint32(f1), uint32(f2))
	}
}

// TestFMultIParity asserts fMultI == genuine fMultI.
func TestFMultIParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xF00D))
	for i := 0; i < 100000; i++ {
		a := int32(r.Uint32())
		b := int32(r.Int31n(1 << 16)) // a line count
		assert.Equalf(t, cFMultI(a, b), nativeaac.ParityFMultI(a, b),
			"fMultI(%#x,%d)", uint32(a), b)
	}
}

// makeCase builds a randomized long-block PE input set: sfbCnt sfbs in one group
// (sfbPerGroup == sfbCnt, maxSfbPerGroup == sfbCnt), ascending sfbOffsets so the
// per-sfb width is positive, ld-domain energies/thresholds and a form factor.
func makeCase(r *rand.Rand, sfbCnt int) (energy, thr, formFac, offset, isBook, isScale []int32) {
	energy = make([]int32, maxGroupedSFB)
	thr = make([]int32, maxGroupedSFB)
	formFac = make([]int32, maxGroupedSFB)
	offset = make([]int32, maxGroupedSFB+1)
	isBook = make([]int32, maxGroupedSFB)
	isScale = make([]int32, maxGroupedSFB)
	pos := int32(0)
	lastIs := int32(0)
	for i := 0; i < sfbCnt; i++ {
		energy[i] = ldRand(r)
		thr[i] = ldRand(r)
		formFac[i] = ldRand(r)
		offset[i] = pos
		pos += 1 + r.Int31n(32) // sfb width 1..32
		// occasional intensity band. The intensity branch in calcSfbPe DPCM-codes
		// isScale deltas through FDKaacEnc_bitCountScalefactorDelta, which asserts
		// |delta| <= CODE_BOOK_SCF_LAV (60); keep the walk bounded so the genuine
		// C oracle stays within its huffman scalefactor table (and matches Go).
		if r.Intn(8) == 0 {
			isBook[i] = 15 // any non-zero book
			// Force en <= thr so calcSfbPe takes the intensity (else-if isBook)
			// branch, which is where lastValIs is updated. This keeps Go's lastIs
			// walk in lock-step with the C oracle's lastValIs, so the DPCM delta
			// stays bounded (|delta| <= 20 < CODE_BOOK_SCF_LAV).
			thr[i] = energy[i]
			lastIs += r.Int31n(41) - 20
			isScale[i] = lastIs
		}
	}
	offset[sfbCnt] = pos
	return energy, thr, formFac, offset, isBook, isScale
}

// TestPrepareSfbPeParity asserts prepareSfbPe (sfbNLines) == genuine
// FDKaacEnc_prepareSfbPe over randomized long-block cases.
func TestPrepareSfbPeParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0x5FB))
	for iter := 0; iter < 4000; iter++ {
		sfbCnt := 1 + r.Intn(maxGroupedSFB)
		energy, thr, formFac, offset, _, _ := makeCase(r, sfbCnt)

		cN := cPrepareSfbPe(energy, thr, formFac, offset, sfbCnt, sfbCnt, sfbCnt)
		gN := nativeaac.ParityPrepareSfbPe(energy, thr, formFac, offset, sfbCnt, sfbCnt, sfbCnt)
		require.Equalf(t, cN, gN, "prepareSfbPe sfbNLines iter=%d sfbCnt=%d", iter, sfbCnt)
	}
}

// TestCalcSfbPeParity asserts calcSfbPe (all three per-sfb arrays + the three
// channel sums) == genuine FDKaacEnc_calcSfbPe, seeded with the real sfbNLines
// from the genuine prepareSfbPe (full driver chain).
func TestCalcSfbPeParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xCA1C))
	for iter := 0; iter < 4000; iter++ {
		sfbCnt := 1 + r.Intn(maxGroupedSFB)
		energy, thr, formFac, offset, isBook, isScale := makeCase(r, sfbCnt)

		// real sfbNLines from genuine prepareSfbPe
		nLines := cPrepareSfbPe(energy, thr, formFac, offset, sfbCnt, sfbCnt, sfbCnt)

		cPe, cCp, cNa, cP, cC, cN := cCalcSfbPe(nLines, energy, thr, sfbCnt, sfbCnt, sfbCnt, isBook, isScale)
		gPe, gCp, gNa, gP, gC, gN := nativeaac.ParityCalcSfbPe(nLines, energy, thr, sfbCnt, sfbCnt, sfbCnt, isBook, isScale)

		require.Equalf(t, cPe, gPe, "calcSfbPe sfbPe iter=%d", iter)
		require.Equalf(t, cCp, gCp, "calcSfbPe sfbConstPart iter=%d", iter)
		require.Equalf(t, cNa, gNa, "calcSfbPe sfbNActiveLines iter=%d", iter)
		require.Equalf(t, cP, gP, "calcSfbPe pe iter=%d", iter)
		require.Equalf(t, cC, gC, "calcSfbPe constPart iter=%d", iter)
		require.Equalf(t, cN, gN, "calcSfbPe nActiveLines iter=%d", iter)
	}
}

// toIntSlice converts an int32 boundary buffer to the []int the Go port expects.
func toIntSlice(s []int32) []int {
	out := make([]int, len(s))
	for i, v := range s {
		out[i] = int(v)
	}
	return out
}

// nrgRand returns a pseudo-random non-negative FIXP_DBL energy (Q1.31). The
// weighting / avoid-hole / chaos kernels work on linear energies plus their ld64
// counterparts, so cover the full non-negative magnitude range.
func nrgRand(r *rand.Rand) int32 { return int32(r.Uint32() & 0x7FFFFFFF) }

// normPow returns a NORMALISED non-negative FIXP_DBL in [0x40000000, 0x7FFFFFFF]
// (fNorm in {0,1}), modelling the CalcRedValPower mantissa / thrExp values the
// reduceThresholdsCBR reduction formula operates on in the real encoder.
func normPow(r *rand.Rand) int32 { return int32(r.Uint32()&0x3FFFFFFF) | 0x40000000 }

// TestPreparePeParity asserts preparePe (sfbNLines + stamped offset) == genuine
// FDKaacEnc_preparePe over randomized long-block cases.
func TestPreparePeParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0x9E11))
	for iter := 0; iter < 3000; iter++ {
		sfbCnt := 1 + r.Intn(maxGroupedSFB)
		energy, thr, formFac, offset, _, _ := makeCase(r, sfbCnt)
		peOffset := r.Intn(4000)

		cN, cOff := cPreparePe(energy, thr, formFac, offset, sfbCnt, sfbCnt, sfbCnt, peOffset)
		gN, gOff := nativeaac.ParityPreparePe(energy, thr, formFac, toIntSlice(offset),
			sfbCnt, sfbCnt, sfbCnt, peOffset)
		require.Equalf(t, cN, gN, "preparePe sfbNLines iter=%d", iter)
		require.Equalf(t, cOff, gOff, "preparePe offset iter=%d", iter)
	}
}

// TestCalcWeightingParity asserts calcWeighting (sfbEnFacLd + chaosMeasureEnFac +
// lastEnFacPatch) == genuine FDKaacEnc_calcWeighting. sfbNLines are seeded from
// the genuine prepareSfbPe (so the fDivNorm precondition denom >= num holds), and
// the no-short-window path (all long blocks) is exercised — the only path that
// produces a non-trivial weighting.
func TestCalcWeightingParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xCA7))
	for iter := 0; iter < 2500; iter++ {
		nCh := 1 + r.Intn(2)
		sfbCnt := 1 + r.Intn(maxGroupedSFB)
		_, _, _, offset, _, _ := makeCase(r, sfbCnt)

		flatEnLd := make([]int32, nCh*maxGroupedSFB)
		flatEn := make([]int32, nCh*maxGroupedSFB)
		flatNLines := make([]int32, nCh*maxGroupedSFB)
		lastWnd := make([]int32, nCh)
		var chaosIn [2]int32
		var patchIn [2]int
		for ch := 0; ch < nCh; ch++ {
			en, thr, ff, _, _, _ := makeCase(r, sfbCnt)
			base := ch * maxGroupedSFB
			for i := 0; i < maxGroupedSFB; i++ {
				flatEnLd[base+i] = en[i]
				flatEn[base+i] = nrgRand(r)
			}
			// genuine sfbNLines (denom >= num precondition)
			nLines := cPrepareSfbPe(en, thr, ff, offset, sfbCnt, sfbCnt, sfbCnt)
			copy(flatNLines[base:], nLines)
			lastWnd[ch] = 0 // LONG_WINDOW -> noShortWindowInFrame
			chaosIn[ch] = ldRand(r)
			patchIn[ch] = r.Intn(2)
		}
		msMask := make([]int32, maxGroupedSFB)
		for i := 0; i < sfbCnt; i++ {
			if r.Intn(2) == 0 {
				msMask[i] = 1
			}
		}

		cChaos := append([]int32(nil), chaosIn[:]...)
		cPatch := []int32{int32(patchIn[0]), int32(patchIn[1])}
		cEnFac := cCalcWeighting(nCh, flatEnLd, flatEn, flatNLines,
			offset, lastWnd, msMask, sfbCnt, sfbCnt, sfbCnt,
			cChaos, cPatch)

		gEnFac, gChaos, gPatch := nativeaac.ParityCalcWeighting(nCh, flatEnLd, flatEn,
			flatNLines, toIntSlice(offset), toIntSlice(lastWnd), msMask,
			sfbCnt, sfbCnt, sfbCnt, chaosIn, patchIn)

		require.Equalf(t, cEnFac, gEnFac, "calcWeighting sfbEnFacLd iter=%d nCh=%d", iter, nCh)
		require.Equalf(t, cChaos[:nCh], gChaos[:nCh], "calcWeighting chaosMeasureEnFac iter=%d", iter)
		require.Equalf(t, int32(cPatch[0]), int32(gPatch[0]), "calcWeighting lastEnFacPatch[0] iter=%d", iter)
		if nCh == 2 {
			require.Equalf(t, int32(cPatch[1]), int32(gPatch[1]), "calcWeighting lastEnFacPatch[1] iter=%d", iter)
		}
	}
}

// TestCalcPeParity asserts calcPe (element pe/constPart/nActiveLines) == genuine
// FDKaacEnc_calcPe, with sfbNLines from the genuine prepareSfbPe.
func TestCalcPeParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xCA1CBE))
	for iter := 0; iter < 3000; iter++ {
		nCh := 1 + r.Intn(2)
		sfbCnt := 1 + r.Intn(maxGroupedSFB)
		peOffset := r.Intn(4000)

		flatWEn := make([]int32, nCh*maxGroupedSFB)
		flatThr := make([]int32, nCh*maxGroupedSFB)
		flatNLines := make([]int32, nCh*maxGroupedSFB)
		flatIsBook := make([]int32, nCh*maxGroupedSFB)
		flatIsScale := make([]int32, nCh*maxGroupedSFB)
		for ch := 0; ch < nCh; ch++ {
			en, thr, ff, offset, isBook, isScale := makeCase(r, sfbCnt)
			base := ch * maxGroupedSFB
			nLines := cPrepareSfbPe(en, thr, ff, offset, sfbCnt, sfbCnt, sfbCnt)
			for i := 0; i < maxGroupedSFB; i++ {
				flatWEn[base+i] = en[i]
				flatThr[base+i] = thr[i]
				flatNLines[base+i] = nLines[i]
				flatIsBook[base+i] = isBook[i]
				flatIsScale[base+i] = isScale[i]
			}
		}

		cP, cC, cN := cCalcPe(nCh, flatWEn, flatThr, flatNLines, flatIsBook, flatIsScale,
			sfbCnt, sfbCnt, sfbCnt, peOffset)
		gP, gC, gN := nativeaac.ParityCalcPe(nCh, flatWEn, flatThr, flatNLines,
			toIntSlice(flatIsBook), toIntSlice(flatIsScale), sfbCnt, sfbCnt, sfbCnt, peOffset)
		require.Equalf(t, cP, gP, "calcPe pe iter=%d", iter)
		require.Equalf(t, cC, gC, "calcPe constPart iter=%d", iter)
		require.Equalf(t, cN, gN, "calcPe nActiveLines iter=%d", iter)
	}
}

// TestInitAvoidHoleFlagParity asserts initAvoidHoleFlag (ahFlag + mutated
// sfbSpreadEnergy + sfbMinSnrLdData) == genuine FDKaacEnc_initAvoidHoleFlag over
// long/short windows, modifyMinSnr on/off and 1/2 channels.
func TestInitAvoidHoleFlagParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xA40E))
	for iter := 0; iter < 3000; iter++ {
		nCh := 1 + r.Intn(2)
		sfbCnt := 1 + r.Intn(maxGroupedSFB)
		modifyMinSnr := r.Intn(2)
		_, _, _, offset, _, _ := makeCase(r, sfbCnt)

		flatSpread := make([]int32, nCh*maxGroupedSFB)
		flatEn := make([]int32, nCh*maxGroupedSFB)
		flatEnLd := make([]int32, nCh*maxGroupedSFB)
		flatMinSnr := make([]int32, nCh*maxGroupedSFB)
		lastWnd := make([]int32, nCh)
		for ch := 0; ch < nCh; ch++ {
			base := ch * maxGroupedSFB
			for i := 0; i < maxGroupedSFB; i++ {
				flatSpread[base+i] = nrgRand(r)
				flatEn[base+i] = nrgRand(r)
				flatEnLd[base+i] = ldRand(r)
				flatMinSnr[base+i] = ldRand(r)
			}
			lastWnd[ch] = int32(r.Intn(4)) // any window sequence
		}
		msMask := make([]int32, maxGroupedSFB)
		for i := 0; i < sfbCnt; i++ {
			if r.Intn(2) == 0 {
				msMask[i] = 1
			}
		}

		cAh, cSpread, cMinSnr := cInitAvoidHoleFlag(nCh, flatSpread, flatEn, flatEnLd,
			flatMinSnr, offset, lastWnd, msMask,
			sfbCnt, sfbCnt, sfbCnt, modifyMinSnr)
		gAh, gSpread, gMinSnr := nativeaac.ParityInitAvoidHoleFlag(nCh, flatSpread, flatEn,
			flatEnLd, flatMinSnr, toIntSlice(offset), toIntSlice(lastWnd), msMask,
			sfbCnt, sfbCnt, sfbCnt, modifyMinSnr)

		require.Equalf(t, cAh, gAh, "initAvoidHoleFlag ahFlag iter=%d nCh=%d mod=%d", iter, nCh, modifyMinSnr)
		require.Equalf(t, cSpread, gSpread, "initAvoidHoleFlag sfbSpreadEnergy iter=%d", iter)
		require.Equalf(t, cMinSnr, gMinSnr, "initAvoidHoleFlag sfbMinSnrLdData iter=%d", iter)
	}
}

// TestReduceThresholdsCBRParity asserts reduceThresholdsCBR (reduced
// sfbThresholdLdData + mutated ahFlag) == genuine FDKaacEnc_reduceThresholdsCBR.
func TestReduceThresholdsCBRParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0x4ED0))
	for iter := 0; iter < 4000; iter++ {
		nCh := 1 + r.Intn(2)
		sfbCnt := 1 + r.Intn(maxGroupedSFB)
		// redVal_m / thrExp are NORMALISED non-negative powers in the real encoder
		// (CalcRedValPower mantissa, fNorm in {0,1}); redVal_e is a small block
		// exponent. Random arbitrary int32 would drive minScale into shift counts
		// >= 32 where C (arm `shift & 31`) and Go (defined-zero) intentionally
		// diverge — a domain the genuine caller never reaches. Stay in-domain.
		redValM := normPow(r)
		redValE := r.Intn(12) - 2 // small block exponent

		flatWEn := make([]int32, nCh*maxGroupedSFB)
		flatThr := make([]int32, nCh*maxGroupedSFB)
		flatMinSnr := make([]int32, nCh*maxGroupedSFB)
		flatThrExp := make([]int32, nCh*maxGroupedSFB)
		ahIn := make([]uint8, nCh*maxGroupedSFB)
		for ch := 0; ch < nCh; ch++ {
			base := ch * maxGroupedSFB
			for i := 0; i < maxGroupedSFB; i++ {
				flatWEn[base+i] = ldRand(r)
				flatThr[base+i] = ldRand(r)
				flatMinSnr[base+i] = ldRand(r)
				flatThrExp[base+i] = normPow(r)
				ahIn[base+i] = uint8(r.Intn(3)) // NO_AH / AH_INACTIVE / AH_ACTIVE
			}
		}

		cThr, cAh := cReduceThresholdsCBR(nCh, flatWEn, flatThr, flatMinSnr, ahIn,
			flatThrExp, sfbCnt, sfbCnt, sfbCnt, redValM, redValE)
		gThr, gAh := nativeaac.ParityReduceThresholdsCBR(nCh, flatWEn, flatThr, flatMinSnr,
			ahIn, flatThrExp, sfbCnt, sfbCnt, sfbCnt, redValM, int32(redValE))

		require.Equalf(t, cThr, gThr, "reduceThresholdsCBR thr iter=%d nCh=%d", iter, nCh)
		require.Equalf(t, cAh, gAh, "reduceThresholdsCBR ahFlag iter=%d", iter)
	}
}

// TestCalcChaosMeasureParity asserts calcChaosMeasure == genuine
// FDKaacEnc_calcChaosMeasure over randomized audible-band cases (including the
// no-audible-band total-chaos path).
func TestCalcChaosMeasureParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xC4A05))
	for iter := 0; iter < 4000; iter++ {
		sfbCnt := 1 + r.Intn(maxGroupedSFB)
		_, _, formFac, offset, _, _ := makeCase(r, sfbCnt)
		enLd := make([]int32, maxGroupedSFB)
		thrLd := make([]int32, maxGroupedSFB)
		en := make([]int32, maxGroupedSFB)
		for i := 0; i < maxGroupedSFB; i++ {
			enLd[i] = ldRand(r)
			thrLd[i] = ldRand(r)
			en[i] = nrgRand(r)
		}

		c := cCalcChaosMeasure(enLd, thrLd, en, formFac, offset, sfbCnt, sfbCnt, sfbCnt)
		g := nativeaac.ParityCalcChaosMeasure(enLd, thrLd, en, formFac,
			toIntSlice(offset), sfbCnt, sfbCnt, sfbCnt)
		require.Equalf(t, c, g, "calcChaosMeasure iter=%d sfbCnt=%d", iter, sfbCnt)
	}
}

// --- A-leaves parity (calcThreshExp / adaptMinSnr / resetAHFlags / calcPeNoAH /
//     calcBitSave / calcBitSpend / adjustPeMinMax) ---------------------------

// randLd64Slice fills n cells with ld64-domain FIXP_DBL values, zero past n.
func randLd64Slice(r *rand.Rand, n int) []int32 {
	out := make([]int32, maxGroupedSFB)
	for i := 0; i < n; i++ {
		out[i] = int32(r.Int63n(0x50000000)) - 0x40000000
	}
	return out
}

// TestCalcThreshExpParity asserts calcThreshExp == genuine FDKaacEnc_calcThreshExp.
func TestCalcThreshExpParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xC0FFEE))
	for iter := 0; iter < 3000; iter++ {
		for _, nCh := range []int{1, 2} {
			maxSfb := 1 + r.Intn(30)
			sfbCnt := []int{maxSfb, maxSfb}
			sfbPer := []int{maxSfb, maxSfb} // single group
			maxPer := []int{maxSfb, maxSfb}
			thr := make([]int32, nCh*maxGroupedSFB)
			for ch := 0; ch < nCh; ch++ {
				copy(thr[ch*maxGroupedSFB:], randLd64Slice(r, maxSfb))
			}
			c := cCalcThreshExp(nCh, thr, sfbCnt, sfbPer, maxPer)
			thrRows := make([][]int32, nCh)
			for ch := 0; ch < nCh; ch++ {
				thrRows[ch] = thr[ch*maxGroupedSFB : (ch+1)*maxGroupedSFB]
			}
			g := nativeaac.ParityCalcThreshExp(thrRows, sfbCnt[:nCh], sfbPer[:nCh], maxPer[:nCh], nCh)
			for ch := 0; ch < nCh; ch++ {
				assert.Equalf(t, c[ch*maxGroupedSFB:(ch+1)*maxGroupedSFB], g[ch][:maxGroupedSFB],
					"calcThreshExp iter=%d ch=%d", iter, ch)
			}
		}
	}
}

// TestAdaptMinSnrParity asserts adaptMinSnr == genuine FDKaacEnc_adaptMinSnr.
func TestAdaptMinSnrParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0x5A1AD))
	for iter := 0; iter < 4000; iter++ {
		for _, nCh := range []int{1, 2} {
			maxSfb := 1 + r.Intn(30)
			sfbCnt := []int{maxSfb, maxSfb}
			sfbPer := []int{maxSfb, maxSfb}
			maxPer := []int{maxSfb, maxSfb}
			en := make([]int32, nCh*maxGroupedSFB)
			enLd := make([]int32, nCh*maxGroupedSFB)
			minSnr := make([]int32, nCh*maxGroupedSFB)
			for ch := 0; ch < nCh; ch++ {
				for i := 0; i < maxSfb; i++ {
					en[ch*maxGroupedSFB+i] = int32(r.Int63n(0x7FFFFFFF)) // positive energy
				}
				copy(enLd[ch*maxGroupedSFB:], randLd64Slice(r, maxSfb))
				copy(minSnr[ch*maxGroupedSFB:], randLd64Slice(r, maxSfb))
			}
			// MINSNR_ADAPT_PARAM values: ld64-domain small fractions.
			maxRed := int32(r.Int63n(0x10000000)) - 0x8000000
			startRatio := int32(r.Int63n(0x20000000)) - 0x10000000
			redRatioFac := int32(r.Int63n(0x7FFFFFFF))
			redOffs := int32(r.Int63n(0x10000000)) - 0x8000000

			c := cAdaptMinSnr(nCh, en, enLd, minSnr, sfbCnt, sfbPer, maxPer, maxRed, startRatio, redRatioFac, redOffs)

			enRows := splitRows(en, nCh)
			enLdRows := splitRows(enLd, nCh)
			minSnrRows := splitRows(minSnr, nCh)
			g := nativeaac.ParityAdaptMinSnr(enRows, enLdRows, minSnrRows,
				sfbCnt[:nCh], sfbPer[:nCh], maxPer[:nCh], maxRed, startRatio, redRatioFac, redOffs, nCh)
			for ch := 0; ch < nCh; ch++ {
				assert.Equalf(t, c[ch*maxGroupedSFB:(ch+1)*maxGroupedSFB], g[ch][:maxGroupedSFB],
					"adaptMinSnr iter=%d ch=%d", iter, ch)
			}
		}
	}
}

// TestResetAHFlagsParity asserts resetAHFlags == genuine FDKaacEnc_resetAHFlags.
func TestResetAHFlagsParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xA4F1A))
	for iter := 0; iter < 3000; iter++ {
		for _, nCh := range []int{1, 2} {
			maxSfb := 1 + r.Intn(30)
			sfbCnt := []int{maxSfb, maxSfb}
			sfbPer := []int{maxSfb, maxSfb}
			maxPer := []int{maxSfb, maxSfb}
			ah := make([]uint8, nCh*maxGroupedSFB)
			for i := range ah {
				ah[i] = uint8(r.Intn(3)) // NO_AH/AH_INACTIVE/AH_ACTIVE
			}
			c := cResetAHFlags(nCh, ah, sfbCnt, sfbPer, maxPer)
			ahRows := splitU8Rows(ah, nCh)
			g := nativeaac.ParityResetAHFlags(ahRows, sfbCnt[:nCh], sfbPer[:nCh], maxPer[:nCh], nCh)
			for ch := 0; ch < nCh; ch++ {
				assert.Equalf(t, c[ch*maxGroupedSFB:(ch+1)*maxGroupedSFB], g[ch][:maxGroupedSFB],
					"resetAHFlags iter=%d ch=%d", iter, ch)
			}
		}
	}
}

// TestCalcPeNoAHParity asserts calcPeNoAH == genuine FDKaacEnc_FDKaacEnc_calcPeNoAH.
func TestCalcPeNoAHParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xBEEF))
	for iter := 0; iter < 4000; iter++ {
		for _, nCh := range []int{1, 2} {
			maxSfb := 1 + r.Intn(30)
			sfbCnt := []int{maxSfb, maxSfb}
			sfbPer := []int{maxSfb, maxSfb}
			maxPer := []int{maxSfb, maxSfb}
			offset := int32(r.Int31n(1 << 20))
			sfbPe := make([]int32, nCh*maxGroupedSFB)
			sfbCP := make([]int32, nCh*maxGroupedSFB)
			sfbNAL := make([]int32, nCh*maxGroupedSFB)
			ah := make([]uint8, nCh*maxGroupedSFB)
			for ch := 0; ch < nCh; ch++ {
				for i := 0; i < maxSfb; i++ {
					sfbPe[ch*maxGroupedSFB+i] = r.Int31n(1 << 24)
					sfbCP[ch*maxGroupedSFB+i] = r.Int31n(1 << 24)
					sfbNAL[ch*maxGroupedSFB+i] = r.Int31n(1 << 10)
					ah[ch*maxGroupedSFB+i] = uint8(r.Intn(3))
				}
			}
			cpe, ccp, cnal := cCalcPeNoAH(nCh, offset, sfbPe, sfbCP, sfbNAL, ah, sfbCnt, sfbPer, maxPer)
			gpe, gcp, gnal := nativeaac.ParityCalcPeNoAH(offset, splitRows(sfbPe, nCh), splitRows(sfbCP, nCh),
				splitRows(sfbNAL, nCh), splitU8Rows(ah, nCh), sfbCnt[:nCh], sfbPer[:nCh], maxPer[:nCh], nCh)
			assert.Equalf(t, cpe, gpe, "calcPeNoAH pe iter=%d", iter)
			assert.Equalf(t, ccp, gcp, "calcPeNoAH constPart iter=%d", iter)
			assert.Equalf(t, cnal, gnal, "calcPeNoAH nActiveLines iter=%d", iter)
		}
	}
}

// TestCalcBitSaveSpendParity asserts calcBitSave / calcBitSpend == genuine.
func TestCalcBitSaveSpendParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xB175A))
	for i := 0; i < 200000; i++ {
		fill := int32(r.Int63n(0x100000000) - 0x80000000)
		clipLow := int32(r.Int63n(0x100000000) - 0x80000000)
		clipHigh := int32(r.Int63n(0x100000000) - 0x80000000)
		minB := int32(r.Int63n(0x100000000) - 0x80000000)
		maxB := int32(r.Int63n(0x100000000) - 0x80000000)
		slope := int32(r.Int63n(0x100000000) - 0x80000000)
		assert.Equalf(t, cCalcBitSave(fill, clipLow, clipHigh, minB, maxB, slope),
			nativeaac.ParityCalcBitSave(fill, clipLow, clipHigh, minB, maxB, slope),
			"calcBitSave i=%d", i)
		assert.Equalf(t, cCalcBitSpend(fill, clipLow, clipHigh, minB, maxB, slope),
			nativeaac.ParityCalcBitSpend(fill, clipLow, clipHigh, minB, maxB, slope),
			"calcBitSpend i=%d", i)
	}
}

// TestAdjustPeMinMaxParity asserts adjustPeMinMax == genuine FDKaacEnc_adjustPeMinMax.
func TestAdjustPeMinMaxParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xAD105))
	for i := 0; i < 300000; i++ {
		currPe := r.Intn(1 << 22)
		peMin := r.Intn(1 << 22)
		peMax := peMin + r.Intn(1<<22)
		cMin, cMax := cAdjustPeMinMax(currPe, peMin, peMax)
		gMin, gMax := nativeaac.ParityAdjustPeMinMax(currPe, peMin, peMax)
		assert.Equalf(t, cMin, gMin, "adjustPeMinMax peMin i=%d currPe=%d peMin=%d peMax=%d", i, currPe, peMin, peMax)
		assert.Equalf(t, cMax, gMax, "adjustPeMinMax peMax i=%d currPe=%d peMin=%d peMax=%d", i, currPe, peMin, peMax)
	}
}

// TestAdjustThresholdsParity asserts the TOP-ENTRY FDKaacEnc_AdjustThresholds (the
// CBR / INTRA-element threshold-adjustment driver, exercising the full
// adaptThresholdsToPe two-guess + correctThresh / reduceMinSnr / allowMoreHoles
// refinement chain) produces a bit-identical sfbThresholdLdData vs the genuine
// vendored top entry over representative SCE/CPE long-block frames.
//
// The per-sfb peData arrays (sfbNLines/sfbPe/sfbConstPart/sfbNActiveLines) and the
// element pe/constPart/nActiveLines are derived from the genuine
// prepareSfbPe/calcSfbPe so they stay self-consistent and in-domain. thrExp inputs
// (the reduceThresholdsCBR reduction formula and correctThresh both build redVal
// via CalcRedValPower on in-range pe sums) avoid the shift-count UB region the
// sibling reduceThresholdsCBR slice documents by keeping ld-domain energies and
// the per-element pe figures in the realistic encoder range.
func TestAdjustThresholdsParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xAD105ADD))
	for iter := 0; iter < 4000; iter++ {
		nCh := 1 + r.Intn(2)
		elType := 0 // ID_SCE
		if nCh == 2 {
			elType = 1 // ID_CPE
		}
		sfbCnt := 1 + r.Intn(48) // long-block sfb count (<= MAX_SFB_LONG=51)

		flatEn := make([]int32, nCh*maxGroupedSFB)
		flatEnLd := make([]int32, nCh*maxGroupedSFB)
		flatThr := make([]int32, nCh*maxGroupedSFB)
		flatWEn := make([]int32, nCh*maxGroupedSFB)
		flatSpread := make([]int32, nCh*maxGroupedSFB)
		flatMinSnr := make([]int32, nCh*maxGroupedSFB)
		flatFF := make([]int32, nCh*maxGroupedSFB)
		flatEnFac := make([]int32, nCh*maxGroupedSFB)
		flatSfbPe := make([]int32, nCh*maxGroupedSFB)
		flatSfbCP := make([]int32, nCh*maxGroupedSFB)
		flatSfbNAL := make([]int32, nCh*maxGroupedSFB)
		flatNLines := make([]int32, nCh*maxGroupedSFB)
		lastWnd := make([]int32, nCh)

		var pe, constPart, nActiveLines int32
		for ch := 0; ch < nCh; ch++ {
			en, thr, ff, offset, isBook, isScale := makeCase(r, sfbCnt)
			base := ch * maxGroupedSFB
			// genuine prepareSfbPe -> sfbNLines, then calcSfbPe -> per-sfb pe arrays
			nLines := cPrepareSfbPe(en, thr, ff, offset, sfbCnt, sfbCnt, sfbCnt)
			sPe, sCP, sNAL, cP, cC, cN := cCalcSfbPe(nLines, en, thr, sfbCnt, sfbCnt, sfbCnt, isBook, isScale)
			pe += cP
			constPart += cC
			nActiveLines += cN
			for i := 0; i < maxGroupedSFB; i++ {
				flatEn[base+i] = nrgRand(r)
				flatEnLd[base+i] = en[i]
				flatThr[base+i] = thr[i]
				flatWEn[base+i] = en[i] // weighted == energy when sfbEnFacLd seeded 0
				flatSpread[base+i] = nrgRand(r)
				flatMinSnr[base+i] = ldRand(r)
				flatFF[base+i] = ff[i]
				flatEnFac[base+i] = 0
				flatSfbPe[base+i] = sPe[i]
				flatSfbCP[base+i] = sCP[i]
				flatSfbNAL[base+i] = sNAL[i]
				flatNLines[base+i] = nLines[i]
			}
			lastWnd[ch] = 0 // LONG_WINDOW
		}

		// shared sfbOffset for the element (single group)
		_, _, _, offset, _, _ := makeCase(r, sfbCnt)
		offsetI := offset // already int32

		msMask := make([]int32, maxGroupedSFB)
		if nCh == 2 {
			for i := 0; i < sfbCnt; i++ {
				if r.Intn(2) == 0 {
					msMask[i] = 1
				}
			}
		}

		// grantedPeCorr spread across the regimes so the full Part II/III/IV chain
		// (reduceThresholdsCBR guesses + correctThresh / reduceMinSnr / allowMoreHoles)
		// is exercised: deep below pe forces the Part IV holes; just below pe stays in
		// the two-guess loop; above pe hits the no-reduction gate.
		var grantedPeCorr int
		switch r.Intn(4) {
		case 0:
			grantedPeCorr = r.Intn(int(pe)/8 + 1) // very low -> reduceMinSnr/allowMoreHoles
		case 1:
			grantedPeCorr = int(pe) - r.Intn(int(pe)/2+1)
		case 2:
			grantedPeCorr = int(pe) - r.Intn(int(pe)/8+1)
		default:
			grantedPeCorr = int(pe) + r.Intn(1000) // exercise the "no reduction" gate too
		}
		maxIter := 1
		if r.Intn(2) == 0 {
			maxIter = 3
		}

		p := nativeaac.AdjThrParams{
			PeOffset:        0,
			ModifyMinSnr:    r.Intn(2),
			StartSfbL:       15,
			StartSfbS:       3,
			MaxRed:          nativeaac.ParityFL2FXConstDBLf(0.00390625),
			StartRatio:      nativeaac.ParityFL2FXConstDBLf(0.05190512648),
			RedRatioFac:     nativeaac.ParityFL2FXConstDBLf(-0.375),
			RedOffs:         nativeaac.ParityFL2FXConstDBL(0.021484375),
			MaxIter2ndGuess: maxIter,
			GrantedPeCorr:   grantedPeCorr,
			Pe:              pe,
			ConstPart:       constPart,
			NActiveLines:    nActiveLines,
		}
		cp := adjThrCParams{
			peOffset: p.PeOffset, modifyMinSnr: p.ModifyMinSnr,
			startSfbL: p.StartSfbL, startSfbS: p.StartSfbS,
			maxRed: p.MaxRed, startRatio: p.StartRatio,
			redRatioFac: p.RedRatioFac, redOffs: p.RedOffs,
			maxIter2ndGuess: p.MaxIter2ndGuess, grantedPeCorr: p.GrantedPeCorr,
			pe: p.Pe, constPart: p.ConstPart, nActiveLines: p.NActiveLines,
		}

		c := cAdjustThresholds(nCh, elType, flatEn, flatEnLd, flatThr, flatWEn,
			flatSpread, flatMinSnr, flatFF, flatEnFac, flatSfbPe, flatSfbCP, flatSfbNAL,
			flatNLines, offsetI, lastWnd, msMask, sfbCnt, sfbCnt, sfbCnt, cp)

		g := nativeaac.ParityAdjustThresholds(nCh, elType, flatEn, flatEnLd, flatThr,
			flatWEn, flatSpread, flatMinSnr, flatFF, flatEnFac, flatSfbPe, flatSfbCP,
			flatSfbNAL, flatNLines, toIntSlice(offsetI), toIntSlice(lastWnd), msMask,
			sfbCnt, sfbCnt, sfbCnt, p)

		require.Equalf(t, c, g, "AdjustThresholds sfbThresholdLdData iter=%d nCh=%d sfbCnt=%d grantedPeCorr=%d pe=%d",
			iter, nCh, sfbCnt, grantedPeCorr, pe)
	}
}

// splitRows splits a flat nCh*maxGroupedSFB int32 slice into per-channel rows.
func splitRows(flat []int32, nCh int) [][]int32 {
	rows := make([][]int32, nCh)
	for ch := 0; ch < nCh; ch++ {
		rows[ch] = flat[ch*maxGroupedSFB : (ch+1)*maxGroupedSFB]
	}
	return rows
}

// splitU8Rows splits a flat nCh*maxGroupedSFB uint8 slice into per-channel rows.
func splitU8Rows(flat []uint8, nCh int) [][]uint8 {
	rows := make([][]uint8, nCh)
	for ch := 0; ch < nCh; ch++ {
		rows[ch] = flat[ch*maxGroupedSFB : (ch+1)*maxGroupedSFB]
	}
	return rows
}
