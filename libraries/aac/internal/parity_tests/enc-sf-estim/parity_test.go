// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encsfestim

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// toIntSlice converts an int32 boundary buffer to the []int the Go port expects.
func toIntSlice(s []int32) []int {
	out := make([]int, len(s))
	for i, v := range s {
		out[i] = int(v)
	}
	return out
}

// specRand returns a pseudo-random FIXP_DBL MDCT line. The form-factor sqrt and
// the quantizer work on Q1.31 spectral lines across the full magnitude range.
func specRand(r *rand.Rand) int32 { return int32(r.Uint32()) }

// ldRand returns a pseudo-random ld64-domain FIXP_DBL (energies/thresholds/form
// factors are CalcLdData outputs — negative-ish small fractions).
func ldRand(r *rand.Rand) int32 {
	return int32(r.Int63n(0x50000000)) - 0x40000000
}

// TestInvSqrtNorm2Parity asserts invSqrtNorm2 (mantissa+shift) == genuine
// invSqrtNorm2 over the full positive input range plus the zero special case.
func TestInvSqrtNorm2Parity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	edge := []int32{0, 1, 2, 0x40000000, 0x7FFFFFFF, 0x00000003, 0x10000000}
	for _, x := range edge {
		cm, cs := cInvSqrtNorm2(x)
		gm, gs := nativeaac.ParityInvSqrtNorm2(x)
		assert.Equalf(t, cm, gm, "invSqrtNorm2(%#x) mantissa", uint32(x))
		assert.Equalf(t, cs, gs, "invSqrtNorm2(%#x) shift", uint32(x))
	}
	r := rand.New(rand.NewSource(0x59A7))
	for i := 0; i < 300000; i++ {
		x := int32(r.Uint32()) & 0x7FFFFFFF // op must be > 0
		if x == 0 {
			continue
		}
		cm, cs := cInvSqrtNorm2(x)
		gm, gs := nativeaac.ParityInvSqrtNorm2(x)
		require.Equalf(t, cm, gm, "invSqrtNorm2(%#x) mantissa", uint32(x))
		require.Equalf(t, cs, gs, "invSqrtNorm2(%#x) shift", uint32(x))
	}
}

// TestSqrtFixpParity asserts sqrtFixp == genuine sqrtFixp over the non-negative
// input range.
func TestSqrtFixpParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	for _, x := range []int32{0, 1, 0x40000000, 0x7FFFFFFF} {
		assert.Equalf(t, cSqrtFixp(x), nativeaac.ParitySqrtFixp(x), "sqrtFixp(%#x)", uint32(x))
	}
	r := rand.New(rand.NewSource(0x5417))
	for i := 0; i < 300000; i++ {
		x := int32(r.Uint32()) & 0x7FFFFFFF
		require.Equalf(t, cSqrtFixp(x), nativeaac.ParitySqrtFixp(x), "sqrtFixp(%#x)", uint32(x))
	}
}

// TestBitCountScfDeltaParity asserts bitCountScalefactorDelta == genuine over
// the valid delta range [-CODE_BOOK_SCF_LAV, CODE_BOOK_SCF_LAV].
func TestBitCountScfDeltaParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	for d := -60; d <= 60; d++ {
		assert.Equalf(t, cBitCountScfDelta(d), nativeaac.ParityBitCountScalefactorDelta(d),
			"bitCountScalefactorDelta(%d)", d)
	}
}

// makeSpectrum builds a randomized long-block channel: an ascending sfbOffsets
// with widths that are multiples of 4 (the estimator's maxSpec loop unrolls by 4
// and reads sfbOffsets[..+1] in steps of 4 — a width not divisible by 4 would
// over-read, which never happens in a real AAC band layout), the MDCT spectrum,
// and ld-domain energies/thresholds/form factors. sfbPerGroup == sfbCnt,
// maxSfbPerGroup == sfbCnt (one long-block group).
func makeSpectrum(r *rand.Rand, sfbCnt int) (mdct []int32, energy, thr, formFac []int32, offset []int32) {
	mdct = make([]int32, 1024)
	energy = make([]int32, maxGroupedSFB)
	thr = make([]int32, maxGroupedSFB)
	formFac = make([]int32, maxGroupedSFB)
	offset = make([]int32, maxGroupedSFB+1)

	pos := int32(0)
	for i := 0; i < sfbCnt; i++ {
		offset[i] = pos
		w := int32(1+r.Intn(8)) * 4 // width 4..32, multiple of 4
		if pos+w > 1024 {
			w = 1024 - pos
			w -= w % 4
		}
		for j := pos; j < pos+w; j++ {
			mdct[j] = specRand(r)
		}
		pos += w
		energy[i] = ldRand(r)
		thr[i] = ldRand(r)
		formFac[i] = ldRand(r)
	}
	offset[sfbCnt] = pos
	return mdct, energy, thr, formFac, offset
}

// TestCalcFormFactorParity asserts calcFormFactorChannel == genuine
// FDKaacEnc_CalcFormFactor over randomized long-block spectra.
func TestCalcFormFactorParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xF0F0))
	for iter := 0; iter < 3000; iter++ {
		sfbCnt := 1 + r.Intn(maxGroupedSFB)
		mdct, _, _, _, offset := makeSpectrum(r, sfbCnt)

		cFF := cCalcFormFactor(mdct, offset, sfbCnt, sfbCnt, sfbCnt)
		gFF := nativeaac.ParityCalcFormFactorChannel(mdct, toIntSlice(offset), sfbCnt, sfbCnt, sfbCnt)
		require.Equalf(t, cFF, gFF, "calcFormFactor iter=%d sfbCnt=%d", iter, sfbCnt)
	}
}

// estimateCase runs the full FDKaacEnc_EstimateScaleFactors in both the genuine C
// and the Go port for a given invQuant / dZoneQuantEnable and asserts every output
// (scf, globalGain, quantSpec, the zeroed mdct spectrum) is bit-identical. The
// form factor is first produced by the genuine FDKaacEnc_CalcFormFactor so the
// whole CalcFormFactor -> EstimateScaleFactors chain is exercised.
func estimateCase(t *testing.T, r *rand.Rand, iters, invQuant int, dZone bool) {
	t.Helper()
	for iter := 0; iter < iters; iter++ {
		sfbCnt := 1 + r.Intn(maxGroupedSFB)
		mdct, energy, thr, _, offset := makeSpectrum(r, sfbCnt)

		// genuine form factor from the real CalcFormFactor (shared input)
		formFac := cCalcFormFactor(mdct, offset, sfbCnt, sfbCnt, sfbCnt)

		// the estimator mutates mdct in place; give each side its own copy
		mdctC := append([]int32(nil), mdct...)
		mdctG := append([]int32(nil), mdct...)

		cScf, cGg, cQs := cEstimateScaleFactors(mdctC, energy, thr, formFac,
			offset, sfbCnt, sfbCnt, sfbCnt, invQuant, dZone)
		gScfI, gGg, gQs := nativeaac.ParityEstimateScaleFactorsChannel(mdctG,
			energy, thr, formFac, toIntSlice(offset), sfbCnt, sfbCnt, sfbCnt,
			invQuant, dZone)
		gScf := make([]int32, len(gScfI))
		for i, v := range gScfI {
			gScf[i] = int32(v)
		}

		require.Equalf(t, cScf, gScf, "scf iter=%d invQuant=%d dZone=%v", iter, invQuant, dZone)
		require.Equalf(t, cGg, gGg, "globalGain iter=%d invQuant=%d dZone=%v", iter, invQuant, dZone)
		require.Equalf(t, cQs, gQs, "quantSpec iter=%d invQuant=%d dZone=%v", iter, invQuant, dZone)
		require.Equalf(t, mdctC, mdctG, "mdct(zeroed) iter=%d invQuant=%d dZone=%v", iter, invQuant, dZone)
	}
}

// TestEstimateScaleFactorsParity_NoInvQuant asserts the initial-estimate path
// (invQuant == 0: no analysis-by-synthesis, no assimilate passes).
func TestEstimateScaleFactorsParity_NoInvQuant(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	estimateCase(t, rand.New(rand.NewSource(0xE571)), 2000, 0, false)
}

// TestEstimateScaleFactorsParity_InvQuant1 asserts the analysis-by-synthesis path
// with the single-scf assimilate pass (invQuant == 1).
func TestEstimateScaleFactorsParity_InvQuant1(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	estimateCase(t, rand.New(rand.NewSource(0x10001)), 1500, 1, true)
	estimateCase(t, rand.New(rand.NewSource(0x10002)), 1500, 1, false)
}

// TestEstimateScaleFactorsParity_InvQuant2 asserts the full path including both
// multiple-scf assimilate passes (invQuant == 2).
func TestEstimateScaleFactorsParity_InvQuant2(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	estimateCase(t, rand.New(rand.NewSource(0x20001)), 1500, 2, true)
	estimateCase(t, rand.New(rand.NewSource(0x20002)), 1500, 2, false)
}

// TestCalcFormFactorDriverParity asserts the top-level CalcFormFactor wrapper
// (nativeaac.ParityCalcFormFactorDriver) == genuine FDKaacEnc_CalcFormFactor over
// 1- and 2-channel cases, so the wrapper's channel loop is exercised directly
// against the real driver (not just transitively through the channel kernel).
func TestCalcFormFactorDriverParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xFFD0))
	for iter := 0; iter < 1500; iter++ {
		nCh := 1 + r.Intn(2)
		sfbCnt := 1 + r.Intn(maxGroupedSFB)
		mdct := make([]int32, nCh*1024)
		// one shared band layout; per-channel MDCT spectra
		_, _, _, _, offset := makeSpectrum(r, sfbCnt)
		for ch := 0; ch < nCh; ch++ {
			m, _, _, _, _ := makeSpectrum(r, sfbCnt)
			copy(mdct[ch*1024:], m)
		}

		c := cCalcFormFactorMulti(nCh, mdct, offset, sfbCnt, sfbCnt, sfbCnt)
		g := nativeaac.ParityCalcFormFactorDriver(nCh, mdct, toIntSlice(offset), sfbCnt, sfbCnt, sfbCnt)
		require.Equalf(t, c, g, "CalcFormFactor driver iter=%d nCh=%d", iter, nCh)
	}
}

// TestEstimateScaleFactorsDriverParity asserts the top-level EstimateScaleFactors
// wrapper (nativeaac.ParityEstimateScaleFactorsDriver) == genuine
// FDKaacEnc_EstimateScaleFactors over 1- and 2-channel cases (all invQuant
// modes), exercising the wrapper's channel loop directly.
func TestEstimateScaleFactorsDriverParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xE5D0))
	for _, invQuant := range []int{0, 1, 2} {
		for iter := 0; iter < 700; iter++ {
			nCh := 1 + r.Intn(2)
			sfbCnt := 1 + r.Intn(maxGroupedSFB)
			_, _, _, _, offset := makeSpectrum(r, sfbCnt)

			mdct := make([]int32, nCh*1024)
			energy := make([]int32, nCh*maxGroupedSFB)
			thr := make([]int32, nCh*maxGroupedSFB)
			for ch := 0; ch < nCh; ch++ {
				m, en, th, _, _ := makeSpectrum(r, sfbCnt)
				copy(mdct[ch*1024:], m)
				copy(energy[ch*maxGroupedSFB:], en)
				copy(thr[ch*maxGroupedSFB:], th)
			}
			// genuine form factor per channel (shared layout)
			formFac := make([]int32, nCh*maxGroupedSFB)
			ffMulti := cCalcFormFactorMulti(nCh, mdct, offset, sfbCnt, sfbCnt, sfbCnt)
			copy(formFac, ffMulti)

			mdctC := append([]int32(nil), mdct...)
			mdctG := append([]int32(nil), mdct...)

			cScf, cGg, cQs := cEstimateScaleFactorsMulti(nCh, mdctC, energy, thr, formFac,
				offset, sfbCnt, sfbCnt, sfbCnt, invQuant, false)
			gScf, gGg, gQs, _ := nativeaac.ParityEstimateScaleFactorsDriver(nCh, mdctG,
				energy, thr, formFac, toIntSlice(offset), sfbCnt, sfbCnt, sfbCnt, invQuant, false)

			require.Equalf(t, cScf, gScf, "EstimateScaleFactors driver scf invQuant=%d iter=%d nCh=%d", invQuant, iter, nCh)
			require.Equalf(t, cGg, gGg, "EstimateScaleFactors driver globalGain invQuant=%d iter=%d", invQuant, iter)
			require.Equalf(t, cQs, gQs, "EstimateScaleFactors driver quantSpec invQuant=%d iter=%d", invQuant, iter)
			require.Equalf(t, mdctC, mdctG, "EstimateScaleFactors driver mdct invQuant=%d iter=%d", invQuant, iter)
		}
	}
}
