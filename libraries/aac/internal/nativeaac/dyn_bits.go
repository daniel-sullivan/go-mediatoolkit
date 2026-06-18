// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Pure-Go 1:1 port of the Fraunhofer FDK-AAC encoder noiseless section coder —
// libAACenc/src/dyn_bits.cpp's FDKaacEnc_dynBitCount and its full static helper
// chain (getSideInfoBits, buildBitLookUp, findBestBook, findMinMergeBits,
// mergeBitLookUp, findMaxMerge, CalcMergeGain, gmStage0/1/2, noiselessCounter,
// scfCount, noiseCount).
//
// dynBitCount is the load-bearing inner-loop bit counter of the quantization /
// rate-control loop (qc_main.cpp's FDKaacEnc_QCMain): for one channel's quantized
// spectrum it groups the scalefactor bands into Huffman sections (greedy merge by
// bit gain), assigns the optimal codebook per section, and sums the resulting
// Huffman bits + sectioning side-info bits + scalefactor DPCM bits + PNS-energy
// bits. The total it returns is exactly the dynamic-bit consumption the QC loop
// drives toward the budget.
//
// Pure integer arithmetic — INT / SHORT / UINT throughout, bit-identical to the
// C regardless of vectorization. The packed Huffman length tables and the
// FDKaacEnc_huff_ltabscf scalefactor table are reused from huffman_rom.go via the
// bit_cnt_count.go kernels (bitCount / bitCountScalefactorDelta). aacfdk-fenced.

package nativeaac

// maxSfbLong is MAX_SFB_LONG (psy_const.h:142) == 51: the longest per-group sfb
// count, bounding the bitLookUp / mergeGainLookUp scratch arrays.
const maxSfbLong = 51

// noNoisePns is NO_NOISE_PNS (aacenc_pns.h:109) == FDK_INT_MIN: the sentinel a
// band's noiseNrg carries when PNS is not used in it.
const noNoisePns = fdkIntMin

// sideInfoTabLong is the 1:1 transcription of FDKaacEnc_sideInfoTabLong
// (aacEnc_rom.cpp:719): the long-window section side-info bit cost indexed by the
// section's sfbCnt (4-bit length field => 9 bits for short runs, 14 for long).
var sideInfoTabLong = [...]int{
	0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009,
	0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009,
	0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009, 0x0009,
	0x0009, 0x0009, 0x0009, 0x0009, 0x000e, 0x000e, 0x000e, 0x000e, 0x000e,
	0x000e, 0x000e, 0x000e, 0x000e, 0x000e, 0x000e, 0x000e, 0x000e, 0x000e,
	0x000e, 0x000e, 0x000e, 0x000e, 0x000e, 0x000e, 0x000e,
}

// sideInfoTabShort is the 1:1 transcription of FDKaacEnc_sideInfoTabShort
// (aacEnc_rom.cpp:727): the short-window section side-info bit cost (3-bit length
// field).
var sideInfoTabShort = [...]int{
	0x0007, 0x0007, 0x0007, 0x0007, 0x0007, 0x0007, 0x0007, 0x000a,
	0x000a, 0x000a, 0x000a, 0x000a, 0x000a, 0x000a, 0x000d, 0x000d,
}

// bitCntrState is the 1:1 port of struct BITCNTR_STATE (dyn_bits.h:141): the
// scratch lookup tables the noiseless counter reuses across calls. The C
// allocates bitLookUp as a flat INT[MAX_SFB_LONG * (CODE_BOOK_ESC_NDX+1)] and
// casts it to lookUpTable (INT(*)[CODE_BOOK_ESC_NDX+1]); here it is the
// equivalent 2-D array, and mergeGainLookUp an INT[MAX_SFB_LONG].
type bitCntrState struct {
	bitLookUp       [maxSfbLong][codeBookEscNdx + 1]int
	mergeGainLookUp [maxSfbLong]int
}

// getSideInfoBits is the 1:1 port of FDKaacEnc_getSideInfoBits
// (dyn_bits.cpp:112): the section side-info bits for one huffsection — 5 bits for
// HCR-escaped books under AC_ER_VCB11, else sideInfoTab[sfbCnt].
func getSideInfoBits(huffsection *SectionInfo, sideInfoTab []int, useHCR int) int {
	if useHCR != 0 && (huffsection.CodeBook == 11 || huffsection.CodeBook >= 16) {
		return 5
	}
	return sideInfoTab[huffsection.SfbCnt]
}

// buildBitLookUp is the 1:1 port of FDKaacEnc_buildBitLookUp (dyn_bits.cpp:128):
// initialise one huffsection per sfb (sfbCnt 1, sectionBits INVALID, codeBook
// -1) and fill bitLookUp[i] with every codebook's bit cost for that band.
func buildBitLookUp(quantSpectrum []int16, maxSfb int, sfbOffset []int,
	sfbMax []uint, bitLookUp *[maxSfbLong][codeBookEscNdx + 1]int,
	huffsection []SectionInfo) {
	for i := 0; i < maxSfb; i++ {
		huffsection[i].SfbCnt = 1
		huffsection[i].SfbStart = i
		huffsection[i].SectionBits = invalidBitcount
		huffsection[i].CodeBook = -1
		sfbWidth := sfbOffset[i+1] - sfbOffset[i]
		bitCount(quantSpectrum[sfbOffset[i]:], sfbWidth, int(sfbMax[i]), bitLookUp[i][:])
	}
}

// findBestBook is the 1:1 port of FDKaacEnc_findBestBook (dyn_bits.cpp:147): the
// cheapest codebook in a bitLookUp row and its bit cost. useVCB11 is unused (the
// callers pass 0 for the cost decision, matching the C comment).
func findBestBook(bc []int, book *int) int {
	minBits := invalidBitcount
	for j := 0; j <= codeBookEscNdx; j++ {
		if bc[j] < minBits {
			minBits = bc[j]
			*book = j
		}
	}
	return minBits
}

// findMinMergeBits is the 1:1 port of FDKaacEnc_findMinMergeBits
// (dyn_bits.cpp:162): the minimum combined bit cost over all codebooks of merging
// two bitLookUp rows.
func findMinMergeBits(bc1, bc2 []int) int {
	minBits := invalidBitcount
	for j := 0; j <= codeBookEscNdx; j++ {
		minBits = fixMin(minBits, bc1[j]+bc2[j])
	}
	return minBits
}

// mergeBitLookUp is the 1:1 port of FDKaacEnc_mergeBitLookUp
// (dyn_bits.cpp:176): accumulate bc2 into bc1 per codebook, clamping to
// INVALID_BITCOUNT.
func mergeBitLookUp(bc1, bc2 []int) {
	for j := 0; j <= codeBookEscNdx; j++ {
		bc1[j] = fixMin(bc1[j]+bc2[j], invalidBitcount)
	}
}

// findMaxMerge is the 1:1 port of FDKaacEnc_findMaxMerge (dyn_bits.cpp:186): the
// section start index with the greatest merge gain, walking section-by-section.
func findMaxMerge(mergeGainLookUp []int, huffsection []SectionInfo, maxSfb int, maxNdx *int) int {
	maxMergeGain := 0
	lastMaxNdx := 0
	for i := 0; i+huffsection[i].SfbCnt < maxSfb; i += huffsection[i].SfbCnt {
		if mergeGainLookUp[i] > maxMergeGain {
			maxMergeGain = mergeGainLookUp[i]
			lastMaxNdx = i
		}
	}
	*maxNdx = lastMaxNdx
	return maxMergeGain
}

// calcMergeGain is the 1:1 port of FDKaacEnc_CalcMergeGain (dyn_bits.cpp:202):
// the bits saved by merging sections ndx1 and ndx2 (split bits minus merged
// bits). Merging a PNS/IS section is forbidden (gain forced to -1).
func calcMergeGain(huffsection []SectionInfo,
	bitLookUp *[maxSfbLong][codeBookEscNdx + 1]int,
	sideInfoTab []int, ndx1, ndx2 int) int {
	mergeBits := sideInfoTab[huffsection[ndx1].SfbCnt+huffsection[ndx2].SfbCnt] +
		findMinMergeBits(bitLookUp[ndx1][:], bitLookUp[ndx2][:])
	splitBits := huffsection[ndx1].SectionBits + huffsection[ndx2].SectionBits
	mergeGain := splitBits - mergeBits

	if huffsection[ndx1].CodeBook == codeBookPnsNo ||
		huffsection[ndx2].CodeBook == codeBookPnsNo ||
		huffsection[ndx1].CodeBook == codeBookIsOutOfPhaseNo ||
		huffsection[ndx2].CodeBook == codeBookIsOutOfPhaseNo ||
		huffsection[ndx1].CodeBook == codeBookIsInPhaseNo ||
		huffsection[ndx2].CodeBook == codeBookIsInPhaseNo {
		mergeGain = -1
	}

	return mergeGain
}

// gmStage0 is the 1:1 port of FDKaacEnc_gmStage0 (dyn_bits.cpp:230): assign the
// minimum codebook (or the pre-allocated PNS / intensity book) to each
// not-yet-costed section.
func gmStage0(huffsection []SectionInfo,
	bitLookUp *[maxSfbLong][codeBookEscNdx + 1]int, maxSfb int,
	noiseNrg, isBook []int) {
	for i := 0; i < maxSfb; i++ {
		if huffsection[i].SectionBits == invalidBitcount {
			if noiseNrg[i] != noNoisePns {
				huffsection[i].CodeBook = codeBookPnsNo
				huffsection[i].SectionBits = 0
			} else if isBook[i] != 0 {
				huffsection[i].CodeBook = isBook[i]
				huffsection[i].SectionBits = 0
			} else {
				huffsection[i].SectionBits =
					findBestBook(bitLookUp[i][:], &huffsection[i].CodeBook)
			}
		}
	}
}

// gmStage1 is the 1:1 port of FDKaacEnc_gmStage1 (dyn_bits.cpp:259): merge all
// adjacent runs sharing a codebook and add the section side-info bits.
func gmStage1(huffsection []SectionInfo,
	bitLookUp *[maxSfbLong][codeBookEscNdx + 1]int, maxSfb int,
	sideInfoTab []int, useVCB11 int) {
	mergeStart := 0
	for {
		var mergeEnd int
		for mergeEnd = mergeStart + 1; mergeEnd < maxSfb; mergeEnd++ {
			if huffsection[mergeStart].CodeBook != huffsection[mergeEnd].CodeBook {
				break
			}
			huffsection[mergeStart].SfbCnt++
			huffsection[mergeStart].SectionBits += huffsection[mergeEnd].SectionBits
			mergeBitLookUp(bitLookUp[mergeStart][:], bitLookUp[mergeEnd][:])
		}

		huffsection[mergeStart].SectionBits +=
			getSideInfoBits(&huffsection[mergeStart], sideInfoTab, useVCB11)
		huffsection[mergeEnd-1].SfbStart = huffsection[mergeStart].SfbStart

		mergeStart = mergeEnd
		if mergeStart >= maxSfb {
			break
		}
	}
}

// gmStage2 is the 1:1 port of FDKaacEnc_gmStage2 (dyn_bits.cpp:294): greedy merge
// — repeatedly merge the adjacent section pair with the largest positive bit gain
// until no merge helps, updating the merge-gain lookup incrementally.
func gmStage2(huffsection []SectionInfo, mergeGainLookUp []int,
	bitLookUp *[maxSfbLong][codeBookEscNdx + 1]int, maxSfb int,
	sideInfoTab []int) {
	for i := 0; i+huffsection[i].SfbCnt < maxSfb; i += huffsection[i].SfbCnt {
		mergeGainLookUp[i] =
			calcMergeGain(huffsection, bitLookUp, sideInfoTab, i, i+huffsection[i].SfbCnt)
	}

	for {
		var maxNdx int
		maxMergeGain := findMaxMerge(mergeGainLookUp, huffsection, maxSfb, &maxNdx)

		if maxMergeGain <= 0 {
			break
		}

		maxNdxNext := maxNdx + huffsection[maxNdx].SfbCnt

		huffsection[maxNdx].SfbCnt += huffsection[maxNdxNext].SfbCnt
		huffsection[maxNdx].SectionBits +=
			huffsection[maxNdxNext].SectionBits - maxMergeGain

		mergeBitLookUp(bitLookUp[maxNdx][:], bitLookUp[maxNdxNext][:])

		if maxNdx != 0 {
			maxNdxLast := huffsection[maxNdx-1].SfbStart
			mergeGainLookUp[maxNdxLast] =
				calcMergeGain(huffsection, bitLookUp, sideInfoTab, maxNdxLast, maxNdx)
		}
		maxNdxNext = maxNdx + huffsection[maxNdx].SfbCnt

		huffsection[maxNdxNext-1].SfbStart = huffsection[maxNdx].SfbStart

		if maxNdxNext < maxSfb {
			mergeGainLookUp[maxNdx] =
				calcMergeGain(huffsection, bitLookUp, sideInfoTab, maxNdx, maxNdxNext)
		}
	}
}

// noiselessCounter is the 1:1 port of FDKaacEnc_noiselessCounter
// (dyn_bits.cpp:343): run the three sectioning stages per group, set the optimal
// codebook on each surviving section, and accumulate sectionData.huffmanBits /
// sideInfoBits + the compacted section list.
func noiselessCounter(sectionData *SectionData, mergeGainLookUp []int,
	bitLookUp *[maxSfbLong][codeBookEscNdx + 1]int,
	quantSpectrum []int16, maxValueInSfb []uint, sfbOffset []int,
	blockType int, noiseNrg, isBook []int, useVCB11 int) {
	var sideInfoTab []int

	switch blockType {
	case ShortWindow:
		sideInfoTab = sideInfoTabShort[:]
	default: // LONG_WINDOW, START_WINDOW, STOP_WINDOW
		sideInfoTab = sideInfoTabLong[:]
	}

	sectionData.NoOfSections = 0
	sectionData.HuffmanBits = 0
	sectionData.SideInfoBits = 0

	if sectionData.MaxSfbPerGroup == 0 {
		return
	}

	for grpNdx := 0; grpNdx < sectionData.SfbCnt; grpNdx += sectionData.SfbPerGroup {
		huffsection := sectionData.HuffSection[sectionData.NoOfSections:]

		buildBitLookUp(quantSpectrum, sectionData.MaxSfbPerGroup,
			sfbOffset[grpNdx:], maxValueInSfb[grpNdx:], bitLookUp, huffsection)

		gmStage0(huffsection, bitLookUp, sectionData.MaxSfbPerGroup,
			noiseNrg[grpNdx:], isBook[grpNdx:])

		gmStage1(huffsection, bitLookUp, sectionData.MaxSfbPerGroup,
			sideInfoTab, useVCB11)

		gmStage2(huffsection, mergeGainLookUp, bitLookUp,
			sectionData.MaxSfbPerGroup, sideInfoTab)

		for i := 0; i < sectionData.MaxSfbPerGroup; i += huffsection[i].SfbCnt {
			if huffsection[i].CodeBook == codeBookPnsNo ||
				huffsection[i].CodeBook == codeBookIsOutOfPhaseNo ||
				huffsection[i].CodeBook == codeBookIsInPhaseNo {
				huffsection[i].SectionBits = 0
			} else {
				findBestBook(bitLookUp[i][:], &huffsection[i].CodeBook)

				sectionData.HuffmanBits +=
					huffsection[i].SectionBits -
						getSideInfoBits(&huffsection[i], sideInfoTab, useVCB11)
			}

			huffsection[i].SfbStart += grpNdx

			sectionData.SideInfoBits +=
				getSideInfoBits(&huffsection[i], sideInfoTab, useVCB11)
			sectionData.HuffSection[sectionData.NoOfSections] = huffsection[i]
			sectionData.NoOfSections++
		}
	}
}

// scfCount is the 1:1 port of FDKaacEnc_scfCount (dyn_bits.cpp:467): count the
// DPCM scalefactor (and intensity-scale) bits. Empty bands repeat the previous
// scalefactor (saving bits) only when the delta to the next non-empty band stays
// within CODE_BOOK_SCF_LAV; otherwise the delta is transmitted. scalefacGain nil
// (C NULL) means no scalefactors, so 0 bits.
func scfCount(scalefacGain []int, maxValueInSfb []uint, sectionData *SectionData, isScale []int) {
	lastValScf := 0
	deltaScf := 0
	found := 0
	scfSkipCounter := 0
	lastValIs := 0

	sectionData.ScalefacBits = 0

	if scalefacGain == nil {
		return
	}

	sectionData.FirstScf = 0

	for i := 0; i < sectionData.NoOfSections; i++ {
		if sectionData.HuffSection[i].CodeBook != codeBookZeroNo {
			sectionData.FirstScf = sectionData.HuffSection[i].SfbStart
			lastValScf = scalefacGain[sectionData.FirstScf]
			break
		}
	}

	for i := 0; i < sectionData.NoOfSections; i++ {
		if sectionData.HuffSection[i].CodeBook == codeBookIsOutOfPhaseNo ||
			sectionData.HuffSection[i].CodeBook == codeBookIsInPhaseNo {
			for j := sectionData.HuffSection[i].SfbStart; j < sectionData.HuffSection[i].SfbStart+sectionData.HuffSection[i].SfbCnt; j++ {
				deltaIs := isScale[j] - lastValIs
				lastValIs = isScale[j]
				sectionData.ScalefacBits += bitCountScalefactorDelta(deltaIs)
			}
		} else if sectionData.HuffSection[i].CodeBook != codeBookZeroNo &&
			sectionData.HuffSection[i].CodeBook != codeBookPnsNo {
			tmp := sectionData.HuffSection[i].SfbStart + sectionData.HuffSection[i].SfbCnt
			for j := sectionData.HuffSection[i].SfbStart; j < tmp; j++ {
				if maxValueInSfb[j] == 0 {
					found = 0
					if scfSkipCounter == 0 {
						if j == (tmp - 1) {
							found = 0
						} else {
							for k := j + 1; k < tmp; k++ {
								if maxValueInSfb[k] != 0 {
									found = 1
									if fixpAbsInt(scalefacGain[k]-lastValScf) <= codeBookScfLav {
										deltaScf = 0
									} else {
										deltaScf = lastValScf - scalefacGain[j]
										lastValScf = scalefacGain[j]
										scfSkipCounter = 0
									}
									break
								}
								scfSkipCounter++
							}
						}

						for m := i + 1; m < sectionData.NoOfSections && found == 0; m++ {
							if sectionData.HuffSection[m].CodeBook != codeBookZeroNo &&
								sectionData.HuffSection[m].CodeBook != codeBookPnsNo {
								end := sectionData.HuffSection[m].SfbStart + sectionData.HuffSection[m].SfbCnt
								for n := sectionData.HuffSection[m].SfbStart; n < end; n++ {
									if maxValueInSfb[n] != 0 {
										found = 1
										if fixpAbsInt(scalefacGain[n]-lastValScf) <= codeBookScfLav {
											deltaScf = 0
										} else {
											deltaScf = lastValScf - scalefacGain[j]
											lastValScf = scalefacGain[j]
											scfSkipCounter = 0
										}
										break
									}
									scfSkipCounter++
								}
							}
						}
						if found == 0 {
							deltaScf = 0
							scfSkipCounter = 0
						}
					} else {
						deltaScf = 0
						scfSkipCounter--
					}
				} else {
					deltaScf = lastValScf - scalefacGain[j]
					lastValScf = scalefacGain[j]
				}
				sectionData.ScalefacBits += bitCountScalefactorDelta(deltaScf)
			}
		}
	}
}

// noiseCount is the 1:1 port of FDKaacEnc_noiseCount (dyn_bits.cpp:589): count
// the PNS energy bits — PNS_PCM_BITS for the first noise band, DPCM thereafter.
func noiseCount(sectionData *SectionData, noiseNrg []int) {
	noisePCMFlag := true
	lastValPns := 0

	sectionData.NoiseNrgBits = 0

	for i := 0; i < sectionData.NoOfSections; i++ {
		if sectionData.HuffSection[i].CodeBook == codeBookPnsNo {
			sfbStart := sectionData.HuffSection[i].SfbStart
			sfbEnd := sfbStart + sectionData.HuffSection[i].SfbCnt
			for j := sfbStart; j < sfbEnd; j++ {
				if noisePCMFlag {
					sectionData.NoiseNrgBits += PnsPCMBits
					lastValPns = noiseNrg[j]
					noisePCMFlag = false
				} else {
					deltaPns := noiseNrg[j] - lastValPns
					lastValPns = noiseNrg[j]
					sectionData.NoiseNrgBits += bitCountScalefactorDelta(deltaPns)
				}
			}
		}
	}
}

// dynBitCount is the 1:1 port of FDKaacEnc_dynBitCount (dyn_bits.cpp:617): for one
// channel set up sectionData (block/group geometry) then run noiselessCounter +
// scfCount + noiseCount, returning the total dynamic-bit consumption
// (huffmanBits + sideInfoBits + scalefacBits + noiseNrgBits). syntaxFlags selects
// the VCB11 side-info escape via AC_ER_VCB11.
func dynBitCount(hBC *bitCntrState, quantSpectrum []int16, maxValueInSfb []uint,
	scalefac []int, blockType, sfbCnt, maxSfbPerGroup, sfbPerGroup int,
	sfbOffset []int, sectionData *SectionData, noiseNrg, isBook, isScale []int,
	syntaxFlags uint) int {
	sectionData.BlockType = blockType
	sectionData.SfbCnt = sfbCnt
	sectionData.SfbPerGroup = sfbPerGroup
	sectionData.NoOfGroups = sfbCnt / sfbPerGroup
	sectionData.MaxSfbPerGroup = maxSfbPerGroup

	useVCB11 := 0
	if syntaxFlags&acErVCB11 != 0 {
		useVCB11 = 1
	}

	noiselessCounter(sectionData, hBC.mergeGainLookUp[:], &hBC.bitLookUp,
		quantSpectrum, maxValueInSfb, sfbOffset, blockType, noiseNrg, isBook, useVCB11)

	scfCount(scalefac, maxValueInSfb, sectionData, isScale)

	noiseCount(sectionData, noiseNrg)

	return sectionData.HuffmanBits + sectionData.SideInfoBits +
		sectionData.ScalefacBits + sectionData.NoiseNrgBits
}
