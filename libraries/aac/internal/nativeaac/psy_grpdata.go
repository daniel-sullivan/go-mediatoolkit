// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Short-block grouping for the psychoacoustic model — a 1:1 port of
// grp_data.cpp (FDKaacEnc_groupShortData, line 118). After the per-window
// short-block analysis, FDKaacEnc_psyMain regroups the MDCT spectrum and sums
// the per-window SFB thresholds / energies (L/R, M/S, spread) into the grouped
// ".Long" arrays according to the block-switch grouping, also deriving the
// grouped SFB offsets, grouped minSnr, and maxSfbPerGroup. Pure fixed-point:
// FIXP_DBL fractions with a saturating add — bit-identical regardless of build
// tag.

// maxSfbShort is MAX_SFB_SHORT (psy_const.h:145) — the per-short-window SFB
// count of the SFB_ENERGY / SFB_THRESHOLD Short arrays.
const maxSfbShort = 15

// maxGroupedSfb is MAX_GROUPED_SFB (psy_const.h:151) ==
// max(MAX_NO_OF_GROUPS*MAX_SFB_SHORT, MAX_SFB_LONG) == max(60, 51) == 60.
const maxGroupedSfb = 60

// sfbGroupedCells is the C union footprint: max(MAX_GROUPED_SFB,
// TRANS_FAC*MAX_SFB_SHORT) == max(60, 8*15=120) == 120 FIXP_DBL cells. The C
// type is `union { FIXP_DBL Long[60]; FIXP_DBL Short[8][15]; }` (psy_data.h:
// 117-121, shouldBeUnion == union, common_fix.h:168), so Long[] and Short[][]
// OVERLAP the same storage.
const sfbGroupedCells = TransFac * maxSfbShort // 120

// sfbGrouped ports the C union shared by SFB_THRESHOLD / SFB_ENERGY /
// SFB_SPREAD_ENERGY (psy_data.h:116-127). It is a genuine UNION in the C: the
// per-short-window Short[wnd][sfb] inputs and the regrouped Long[i] outputs
// alias the SAME storage. groupShortData writes Long[i] while still reading
// Short[wnd][sfb] from the same buffer, and the resulting in-place overwrite
// IS observable in the output (verified: a non-aliased model diverges). The
// port therefore models the union exactly with one flat backing array, with
// Long(i)/SetLong(i,v) and Short(w,b)/SetShort(w,b,v) accessors mapping to the
// C memory layout: Long index i -> cell i; Short[w][b] -> cell w*MAX_SFB_SHORT+b.
type sfbGrouped struct {
	cells [sfbGroupedCells]int32
}

// Long returns the union's Long[i] (cell i).
func (s *sfbGrouped) Long(i int) int32 { return s.cells[i] }

// SetLong sets the union's Long[i] (cell i), aliasing Short storage as in C.
func (s *sfbGrouped) SetLong(i int, v int32) { s.cells[i] = v }

// Short returns the union's Short[w][b] (cell w*MAX_SFB_SHORT+b).
func (s *sfbGrouped) Short(w, b int) int32 { return s.cells[w*maxSfbShort+b] }

// SetShort sets the union's Short[w][b] (cell w*MAX_SFB_SHORT+b).
func (s *sfbGrouped) SetShort(w, b int, v int32) { s.cells[w*maxSfbShort+b] = v }

// Slice returns the backing union storage from cell `from` onward, modelling
// the C pointer `&union.Long[from]` / `union.Short[w]` (cell w*maxSfb). The
// leaf kernels take FIXP_DBL* arguments; psyMain passes these sub-slices to
// reproduce the `pSfb...[ch] + w*maxSfb` pointer arithmetic exactly.
func (s *sfbGrouped) Slice(from int) []int32 { return s.cells[from:] }

// Cells returns the whole backing union storage (cell 0 onward).
func (s *sfbGrouped) Cells() []int32 { return s.cells[:] }

// nrgAddSaturate adds two FIXP_DBL energies with saturation to MAXVAL_DBL,
// keeping one bit more accuracy than fAddSaturate2. C counterpart:
// nrgAddSaturate (grp_data.cpp:114).
//
//	static inline FIXP_DBL nrgAddSaturate(const FIXP_DBL a, const FIXP_DBL b) {
//	  return ((a >= (FIXP_DBL)MAXVAL_DBL - b) ? (FIXP_DBL)MAXVAL_DBL : (a + b));
//	}
func nrgAddSaturate(a, b int32) int32 {
	if a >= maxvalDBL-b {
		return maxvalDBL
	}
	return a + b
}

// groupShortData regroups the short-block spectrum and groups energies and
// thresholds according to the block-switch grouping. It is NOT in-place for
// the spectrum (it uses a scratch buffer, matching the C). C counterpart:
// FDKaacEnc_groupShortData, grp_data.cpp:118.
//
// mdctSpectrum is in-out (granuleLength entries). sfbThreshold/sfbEnergy/
// sfbEnergyMS/sfbSpreadEnergy carry Short (in) and Long (out). groupedSfbOffset
// and groupedSfbMinSnrLdData are outputs; maxSfbPerGroup is returned.
func groupShortData(
	mdctSpectrum []int32,
	sfbThreshold, sfbEnergy, sfbEnergyMS, sfbSpreadEnergy *sfbGrouped,
	sfbCnt, sfbActive int, sfbOffset []int,
	sfbMinSnrLdData []int32,
	groupedSfbOffset []int,
	groupedSfbMinSnrLdData []int32,
	noOfGroups int, groupLen []int, granuleLength int,
) (maxSfbPerGroup int) {
	granuleLengthShort := granuleLength / TransFac

	tmpSpectrum := make([]int32, 1024)

	// calculate maxSfbPerGroup
	highestSfb := 0
	for wnd := 0; wnd < TransFac; wnd++ {
		var sfb int
		for sfb = sfbActive - 1; sfb >= highestSfb; sfb-- {
			var line int
			for line = sfbOffset[sfb+1] - 1; line >= sfbOffset[sfb]; line-- {
				if mdctSpectrum[wnd*granuleLengthShort+line] != 0 {
					break // this band is not completely zero
				}
			}
			if line >= sfbOffset[sfb] {
				break // this band was not completely zero
			}
		}
		if sfb > highestSfb {
			highestSfb = sfb
		}
	}
	if highestSfb < 0 {
		highestSfb = 0
	}
	maxSfbPerGroup = highestSfb + 1

	// calculate groupedSfbOffset
	i := 0
	offset := 0
	for grp := 0; grp < noOfGroups; grp++ {
		var sfb int
		for sfb = 0; sfb < sfbActive+1; sfb++ {
			groupedSfbOffset[i] = offset + sfbOffset[sfb]*groupLen[grp]
			i++
		}
		i += sfbCnt - sfb
		offset += groupLen[grp] * granuleLengthShort
	}
	groupedSfbOffset[i] = granuleLength
	i++

	// calculate groupedSfbMinSnr
	i = 0
	for grp := 0; grp < noOfGroups; grp++ {
		var sfb int
		for sfb = 0; sfb < sfbActive; sfb++ {
			groupedSfbMinSnrLdData[i] = sfbMinSnrLdData[sfb]
			i++
		}
		i += sfbCnt - sfb
	}

	// sum up sfbThresholds (union aliasing: writes to Long alias Short storage)
	wnd := 0
	i = 0
	for grp := 0; grp < noOfGroups; grp++ {
		var sfb int
		for sfb = 0; sfb < sfbActive; sfb++ {
			thresh := sfbThreshold.Short(wnd, sfb)
			for j := 1; j < groupLen[grp]; j++ {
				thresh = nrgAddSaturate(thresh, sfbThreshold.Short(wnd+j, sfb))
			}
			sfbThreshold.SetLong(i, thresh)
			i++
		}
		i += sfbCnt - sfb
		wnd += groupLen[grp]
	}

	// sum up sfbEnergies left/right
	wnd = 0
	i = 0
	for grp := 0; grp < noOfGroups; grp++ {
		var sfb int
		for sfb = 0; sfb < sfbActive; sfb++ {
			energy := sfbEnergy.Short(wnd, sfb)
			for j := 1; j < groupLen[grp]; j++ {
				energy = nrgAddSaturate(energy, sfbEnergy.Short(wnd+j, sfb))
			}
			sfbEnergy.SetLong(i, energy)
			i++
		}
		i += sfbCnt - sfb
		wnd += groupLen[grp]
	}

	// sum up sfbEnergies mid/side
	wnd = 0
	i = 0
	for grp := 0; grp < noOfGroups; grp++ {
		var sfb int
		for sfb = 0; sfb < sfbActive; sfb++ {
			energy := sfbEnergyMS.Short(wnd, sfb)
			for j := 1; j < groupLen[grp]; j++ {
				energy = nrgAddSaturate(energy, sfbEnergyMS.Short(wnd+j, sfb))
			}
			sfbEnergyMS.SetLong(i, energy)
			i++
		}
		i += sfbCnt - sfb
		wnd += groupLen[grp]
	}

	// sum up sfbSpreadEnergies
	wnd = 0
	i = 0
	for grp := 0; grp < noOfGroups; grp++ {
		var sfb int
		for sfb = 0; sfb < sfbActive; sfb++ {
			energy := sfbSpreadEnergy.Short(wnd, sfb)
			for j := 1; j < groupLen[grp]; j++ {
				energy = nrgAddSaturate(energy, sfbSpreadEnergy.Short(wnd+j, sfb))
			}
			sfbSpreadEnergy.SetLong(i, energy)
			i++
		}
		i += sfbCnt - sfb
		wnd += groupLen[grp]
	}

	// re-group spectrum
	wnd = 0
	i = 0
	for grp := 0; grp < noOfGroups; grp++ {
		var sfb int
		for sfb = 0; sfb < sfbActive; sfb++ {
			width := sfbOffset[sfb+1] - sfbOffset[sfb]
			base := sfbOffset[sfb] + wnd*granuleLengthShort
			for j := 0; j < groupLen[grp]; j++ {
				src := base + j*granuleLengthShort
				for line := 0; line < width; line++ {
					tmpSpectrum[i] = mdctSpectrum[src+line]
					i++
				}
			}
		}
		i += groupLen[grp] * (sfbOffset[sfbCnt] - sfbOffset[sfb])
		wnd += groupLen[grp]
	}

	copy(mdctSpectrum[:granuleLength], tmpSpectrum[:granuleLength])

	return maxSfbPerGroup
}
