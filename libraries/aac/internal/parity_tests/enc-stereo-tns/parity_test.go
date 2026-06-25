// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encstereotns

import (
	"math/rand/v2"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These are pure INTEGER fixed-point kernels (FIXP_DBL == int32, FIXP_SGL ==
// int16): fixMin/fixMax + arithmetic shifts (ms_stereo) and integer border
// comparisons + table indexing (TNS quantizers) are bit-identical regardless of
// -ffp-contract / vectorization, with no transcendental and no float. So the
// assertions run unconditionally (like the sibling enc-quantize / enc-psy-model
// oracle), NOT gated on nativeaac.StrictMode — there is no FP path to gate. The
// canonical gate still runs under -tags 'aac_strict aacfdk' for consistency.

// longBandOffset is a faithful AAC-LC long-window scalefactor-band offset layout
// (the 49-band 44.1 kHz table): a strictly increasing partition of the 1024-line
// spectrum into SFBs.
var longBandOffset = []int32{
	0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 48, 56, 64, 72, 80, 88, 96, 108,
	120, 132, 144, 160, 176, 196, 216, 240, 264, 292, 320, 352, 384, 416, 448,
	480, 512, 544, 576, 608, 640, 672, 704, 736, 768, 800, 832, 864, 896, 928,
	1024,
}

func numLong() int { return len(longBandOffset) - 1 }

// ldKind enumerates ld-domain energy/threshold shapes driving the M/S decision
// across its branches (forcing useMS true/false in different mixes).
type ldKind int

const (
	kindRandom ldKind = iota // mixed energies/thresholds
	kindMSWins               // mid/side cheaper -> mostly useMS
	kindLRWins               // L/R cheaper -> mostly !useMS
	kindFlat                 // equal energies/thresholds
)

var allKinds = []ldKind{kindRandom, kindMSWins, kindLRWins, kindFlat}

func kindName(k ldKind) string {
	return [...]string{"random", "ms-wins", "lr-wins", "flat"}[k]
}

// makeMsIO builds a deterministically-seeded msIO over `nbands` bands and the
// matching number of MDCT lines, for the given ld-shape. ld-domain values are
// negative FIXP_DBL (CalcLdData of a fraction < 1 is negative); MDCT lines are
// full-range int32.
func makeMsIO(k ldKind, nbands int, sfbOffset []int32, seed int) *msIO {
	rng := rand.New(rand.NewPCG(uint64(seed)+1, 0x5e7e))
	nlines := int(sfbOffset[nbands])

	ld := func(lo, hi int32) []int32 {
		s := make([]int32, nbands)
		for i := range s {
			s[i] = lo + int32(rng.IntN(int(hi-lo+1)))
		}
		return s
	}
	lines := func() []int32 {
		s := make([]int32, nlines)
		for i := range s {
			s[i] = int32(rng.Uint32())
		}
		return s
	}

	io := &msIO{
		mdctSpectrumLeft:  lines(),
		mdctSpectrumRight: lines(),
		msMask:            make([]int32, nbands),
	}

	// Energies (linear FIXP_DBL, non-negative) and thresholds.
	posE := func() []int32 {
		s := make([]int32, nbands)
		for i := range s {
			s[i] = int32(rng.Uint32() >> 1)
		}
		return s
	}
	io.sfbEnergyLeft = posE()
	io.sfbEnergyRight = posE()
	io.sfbEnergyMid = posE()
	io.sfbEnergySide = posE()
	io.sfbThresholdLeft = posE()
	io.sfbThresholdRight = posE()
	io.sfbSpreadEnLeft = posE()
	io.sfbSpreadEnRight = posE()

	// ld-domain forms: CalcLdData(x) is in roughly [-2^31, 0); use a wide
	// negative range so fixMin/fixMax and the >>1 headroom math exercise.
	switch k {
	case kindRandom:
		io.sfbEnergyLeftLd = ld(-0x40000000, -1)
		io.sfbEnergyRightLd = ld(-0x40000000, -1)
		io.sfbEnergyMidLd = ld(-0x40000000, -1)
		io.sfbEnergySideLd = ld(-0x40000000, -1)
		io.sfbThresholdLeftLd = ld(-0x40000000, -1)
		io.sfbThresholdRightLd = ld(-0x40000000, -1)
	case kindMSWins:
		// Make mid/side energies tiny (very negative ld) and thresholds high
		// (near 0) so pnms > pnlr -> useMS.
		io.sfbEnergyLeftLd = ld(-0x08000000, -1)
		io.sfbEnergyRightLd = ld(-0x08000000, -1)
		io.sfbEnergyMidLd = ld(-0x7f000000, -0x40000000)
		io.sfbEnergySideLd = ld(-0x7f000000, -0x40000000)
		io.sfbThresholdLeftLd = ld(-0x04000000, -1)
		io.sfbThresholdRightLd = ld(-0x04000000, -1)
	case kindLRWins:
		io.sfbEnergyLeftLd = ld(-0x7f000000, -0x40000000)
		io.sfbEnergyRightLd = ld(-0x7f000000, -0x40000000)
		io.sfbEnergyMidLd = ld(-0x08000000, -1)
		io.sfbEnergySideLd = ld(-0x08000000, -1)
		io.sfbThresholdLeftLd = ld(-0x04000000, -1)
		io.sfbThresholdRightLd = ld(-0x04000000, -1)
	case kindFlat:
		v := ld(-0x20000000, -0x20000000)
		io.sfbEnergyLeftLd = append([]int32(nil), v...)
		io.sfbEnergyRightLd = append([]int32(nil), v...)
		io.sfbEnergyMidLd = append([]int32(nil), v...)
		io.sfbEnergySideLd = append([]int32(nil), v...)
		io.sfbThresholdLeftLd = append([]int32(nil), v...)
		io.sfbThresholdRightLd = append([]int32(nil), v...)
	}
	return io
}

// clone deep-copies an msIO so the C and Go runs see identical inputs.
func (io *msIO) clone() *msIO {
	cp := func(s []int32) []int32 { return append([]int32(nil), s...) }
	return &msIO{
		sfbEnergyLeft: cp(io.sfbEnergyLeft), sfbEnergyRight: cp(io.sfbEnergyRight),
		sfbEnergyMid: cp(io.sfbEnergyMid), sfbEnergySide: cp(io.sfbEnergySide),
		sfbThresholdLeft: cp(io.sfbThresholdLeft), sfbThresholdRight: cp(io.sfbThresholdRight),
		sfbSpreadEnLeft: cp(io.sfbSpreadEnLeft), sfbSpreadEnRight: cp(io.sfbSpreadEnRight),
		sfbEnergyLeftLd: cp(io.sfbEnergyLeftLd), sfbEnergyRightLd: cp(io.sfbEnergyRightLd),
		sfbEnergyMidLd: cp(io.sfbEnergyMidLd), sfbEnergySideLd: cp(io.sfbEnergySideLd),
		sfbThresholdLeftLd: cp(io.sfbThresholdLeftLd), sfbThresholdRightLd: cp(io.sfbThresholdRightLd),
		mdctSpectrumLeft: cp(io.mdctSpectrumLeft), mdctSpectrumRight: cp(io.mdctSpectrumRight),
		msMask: cp(io.msMask),
	}
}

// goMsArrays adapts an msIO into the nativeaac.MsStereoArrays the Go port
// mutates in place.
func (io *msIO) goArrays() *nativeaac.MsStereoArrays {
	return &nativeaac.MsStereoArrays{
		SfbEnergyLeft: io.sfbEnergyLeft, SfbEnergyRight: io.sfbEnergyRight,
		SfbEnergyMid: io.sfbEnergyMid, SfbEnergySide: io.sfbEnergySide,
		SfbThresholdLeft: io.sfbThresholdLeft, SfbThresholdRight: io.sfbThresholdRight,
		SfbSpreadEnLeft: io.sfbSpreadEnLeft, SfbSpreadEnRight: io.sfbSpreadEnRight,
		SfbEnergyLeftLd: io.sfbEnergyLeftLd, SfbEnergyRightLd: io.sfbEnergyRightLd,
		SfbEnergyMidLd: io.sfbEnergyMidLd, SfbEnergySideLd: io.sfbEnergySideLd,
		SfbThresholdLeftLd: io.sfbThresholdLeftLd, SfbThresholdRightLd: io.sfbThresholdRightLd,
		MdctSpectrumLeft: io.mdctSpectrumLeft, MdctSpectrumRight: io.mdctSpectrumRight,
	}
}

// assertEq compares the two io arrays element-for-element plus msDigest.
func assertEqMsIO(t *testing.T, cIO, goIO *msIO, cDigest, goDigest int) {
	t.Helper()
	assert.Equal(t, cDigest, goDigest, "msDigest")
	assert.Equal(t, cIO.msMask, goIO.msMask, "msMask")
	assert.Equal(t, cIO.sfbEnergyLeft, goIO.sfbEnergyLeft, "sfbEnergyLeft")
	assert.Equal(t, cIO.sfbEnergyRight, goIO.sfbEnergyRight, "sfbEnergyRight")
	assert.Equal(t, cIO.sfbThresholdLeft, goIO.sfbThresholdLeft, "sfbThresholdLeft")
	assert.Equal(t, cIO.sfbThresholdRight, goIO.sfbThresholdRight, "sfbThresholdRight")
	assert.Equal(t, cIO.sfbSpreadEnLeft, goIO.sfbSpreadEnLeft, "sfbSpreadEnLeft")
	assert.Equal(t, cIO.sfbSpreadEnRight, goIO.sfbSpreadEnRight, "sfbSpreadEnRight")
	assert.Equal(t, cIO.sfbEnergyLeftLd, goIO.sfbEnergyLeftLd, "sfbEnergyLeftLd")
	assert.Equal(t, cIO.sfbEnergyRightLd, goIO.sfbEnergyRightLd, "sfbEnergyRightLd")
	assert.Equal(t, cIO.sfbThresholdLeftLd, goIO.sfbThresholdLeftLd, "sfbThresholdLeftLd")
	assert.Equal(t, cIO.sfbThresholdRightLd, goIO.sfbThresholdRightLd, "sfbThresholdRightLd")
	assert.Equal(t, cIO.mdctSpectrumLeft, goIO.mdctSpectrumLeft, "mdctSpectrumLeft")
	assert.Equal(t, cIO.mdctSpectrumRight, goIO.mdctSpectrumRight, "mdctSpectrumRight")
}

// TestMsStereoProcessing exercises FDKaacEnc_MsStereoProcessing across ld-shapes,
// allowMS on/off, and isBook nil / sparse-intensity, asserting EXACT integer
// equality of every mutated array + msDigest vs the genuine vendored C.
func TestMsStereoProcessing(t *testing.T) {
	nb := numLong()
	for _, k := range allKinds {
		for _, allowMS := range []int{0, 1} {
			for _, isBookMode := range []int{0, 1, 2} { // 0=nil, 1=all-zero, 2=sparse
				for seed := 0; seed < 6; seed++ {
					base := makeMsIO(k, nb, longBandOffset, seed*13+isBookMode*7+allowMS*3)

					var isBook []int32
					switch isBookMode {
					case 1:
						isBook = make([]int32, nb) // all zero
					case 2:
						isBook = make([]int32, nb)
						rng := rand.New(rand.NewPCG(uint64(seed)+99, 0x15))
						for i := range isBook {
							if rng.IntN(4) == 0 {
								isBook[i] = 15 // INTENSITY_HCB-ish (non-zero)
								base.msMask[i] = int32(rng.IntN(2))
							}
						}
					}

					cIO := base.clone()
					goIO := base.clone()

					cDigest := cMsStereo(cIO, isBook, longBandOffset, allowMS, nb, nb, nb)

					ga := goIO.goArrays()
					gd := nativeaac.NewMsStereoData(ga)
					goDigest := nativeaac.EncMsStereoProcessing(gd, isBook, goIO.msMask,
						allowMS, nb, nb, nb, longBandOffset)

					name := kindName(k)
					t.Run(name, func(t *testing.T) {
						assertEqMsIO(t, cIO, goIO, cDigest, goDigest)
					})
				}
			}
		}
	}
}

// TestMsStereoGroups exercises the grouped-short geometry (sfbPerGroup>1,
// maxSfbPerGroup<sfbPerGroup) so the outer group stride + the inner band loop +
// the MS_MASK_ALL promotion predicate (numMsMaskFalse < maxSfbPerGroup) are
// covered.
func TestMsStereoGroups(t *testing.T) {
	// 4 groups of 16 sfbs each; only the first 14 bands per group are active.
	const sfbPerGroup = 15
	const maxSfbPerGroup = 14
	const nGroups = 4
	sfbCnt := sfbPerGroup * nGroups

	// Build a monotone offset over sfbCnt bands of 8 lines each.
	off := make([]int32, sfbCnt+1)
	for i := range off {
		off[i] = int32(i * 8)
	}

	for _, k := range allKinds {
		for _, allowMS := range []int{0, 1} {
			for seed := 0; seed < 5; seed++ {
				base := makeMsIO(k, sfbCnt, off, seed*17+allowMS)
				cIO := base.clone()
				goIO := base.clone()

				cDigest := cMsStereo(cIO, nil, off, allowMS, sfbCnt, sfbPerGroup, maxSfbPerGroup)

				ga := goIO.goArrays()
				gd := nativeaac.NewMsStereoData(ga)
				goDigest := nativeaac.EncMsStereoProcessing(gd, nil, goIO.msMask,
					allowMS, sfbCnt, sfbPerGroup, maxSfbPerGroup, off)

				t.Run(kindName(k), func(t *testing.T) {
					assertEqMsIO(t, cIO, goIO, cDigest, goDigest)
				})
			}
		}
	}
}

// TestTnsRom verifies the Go int16-narrowed TNS-encode reflection-coefficient
// ROM tables match the genuine vendored FDKaacEnc_tnsEncCoeff3/4 +
// tnsCoeff3/4Borders bit-for-bit.
func TestTnsRom(t *testing.T) {
	cEnc3, cB3, cEnc4, cB4 := cTnsRom()
	gEnc3, gB3, gEnc4, gB4 := nativeaac.EncTnsRom()
	require.Equal(t, cEnc3, gEnc3, "tnsEncCoeff3")
	require.Equal(t, cB3, gB3, "tnsCoeff3Borders")
	require.Equal(t, cEnc4, gEnc4, "tnsEncCoeff4")
	require.Equal(t, cB4, gB4, "tnsCoeff4Borders")
}

// TestTnsParcorQuant exercises FDKaacEnc_Parcor2Index over random ParCor
// coefficients (3-bit and 4-bit resolutions), asserting EXACT index equality vs
// the genuine static C, then round-trips through FDKaacEnc_Index2Parcor and
// checks the dequantized ParCor matches too.
func TestTnsParcorQuant(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 0x70c))
	for _, bits := range []int{3, 4} {
		for order := 1; order <= 12; order++ {
			for trial := 0; trial < 200; trial++ {
				parcor := make([]int16, order)
				for i := range parcor {
					parcor[i] = int16(rng.Uint32())
				}
				cIdx := cParcor2Index(parcor, order, bits)
				gIdx := nativeaac.EncTnsParcor2Index(parcor, order, bits)
				// Go returns []int; widen the C []int32 for compare.
				gIdx32 := make([]int32, order)
				for i, v := range gIdx {
					gIdx32[i] = int32(v)
				}
				require.Equal(t, cIdx, gIdx32, "parcor2index bits=%d order=%d", bits, order)

				// Round-trip dequant.
				gIdxInt := make([]int, order)
				for i, v := range cIdx {
					gIdxInt[i] = int(v)
				}
				cPar := cIndex2Parcor(cIdx, order, bits)
				gPar := nativeaac.EncTnsIndex2Parcor(gIdxInt, order, bits)
				require.Equal(t, cPar, gPar, "index2parcor bits=%d order=%d", bits, order)
			}
		}
	}
}
