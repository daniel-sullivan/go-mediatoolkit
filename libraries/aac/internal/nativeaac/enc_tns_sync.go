// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDKaacEnc_TnsSync, ported 1:1 from libAACenc/src/aacenc_tns.cpp (:961-1034).
// It synchronises the TNS filters between two channels of a channel pair when
// their higher-filter ParCor coefficients are similar enough, so the pair can
// share one filter set. Pure integer (fAbs over int coef indices, comparisons),
// aacfdk-fenced, no aac_strict split.
//
// The C selects the per-window TNS_SUBBLOCK_INFO via dataRaw.Long.subBlockInfo
// (nWindows==1, w==0) for long blocks or dataRaw.Short.subBlockInfo[w]
// (nWindows==8) for short blocks. The Go TNSData mirrors this with LongSubBlock
// and ShortSubBlock[encTransFac]; tnsInfo arrays are indexed by window w.

// tnsSync ports FDKaacEnc_TnsSync(TNS_DATA *dest, const TNS_DATA *src, TNS_INFO
// *infoDest, TNS_INFO *infoSrc, INT blockTypeDest, INT blockTypeSrc, const
// TNS_CONFIG *tC). dest/infoDest are mutated in place; src/infoSrc are read.
func tnsSync(tnsDataDest, tnsDataSrc *TNSData, tnsInfoDest, tnsInfoSrc *TNSInfo,
	blockTypeDest, blockTypeSrc int, tC *TNSConfig) {

	// if one channel contains short blocks and the other not, do not synchronize
	if (blockTypeSrc == encShortWindow && blockTypeDest != encShortWindow) ||
		(blockTypeDest == encShortWindow && blockTypeSrc != encShortWindow) {
		return
	}

	var nWindows int
	if blockTypeDest != encShortWindow {
		nWindows = 1
	} else {
		nWindows = 8
	}

	// subBlockInfo accessors that mirror the C `sbInfoDest + w` / `sbInfoSrc + w`.
	sbDest := func(w int) *TNSSubblockInfo {
		if blockTypeDest != encShortWindow {
			return &tnsDataDest.LongSubBlock
		}
		return &tnsDataDest.ShortSubBlock[w]
	}
	sbSrc := func(w int) *TNSSubblockInfo {
		if blockTypeDest != encShortWindow {
			return &tnsDataSrc.LongSubBlock
		}
		return &tnsDataSrc.ShortSubBlock[w]
	}

	for w := 0; w < nWindows; w++ {
		pSbInfoSrcW := sbSrc(w)
		pSbInfoDestW := sbDest(w)
		doSync := 1
		absDiffSum := 0

		// if TNS is active in at least one channel, check if ParCor coefficients
		// of higher filter are similar
		if pSbInfoDestW.TnsActive[hifilt] != 0 || pSbInfoSrcW.TnsActive[hifilt] != 0 {
			for i := 0; i < tC.MaxOrder; i++ {
				absDiff := fixpAbsInt(tnsInfoDest.Coef[w][hifilt][i] - tnsInfoSrc.Coef[w][hifilt][i])
				absDiffSum += absDiff
				// if coefficients diverge too much between channels, do not
				// synchronize
				if absDiff > 1 || absDiffSum > 2 {
					doSync = 0
					break
				}
			}

			if doSync != 0 {
				// if no significant difference was detected, synchronize
				// coefficient sets
				if pSbInfoSrcW.TnsActive[hifilt] != 0 {
					// no dest filter, or more dest than source filters: use one
					// dest filter
					if pSbInfoDestW.TnsActive[hifilt] == 0 ||
						(pSbInfoDestW.TnsActive[hifilt] != 0 &&
							tnsInfoDest.NumOfFilters[w] > tnsInfoSrc.NumOfFilters[w]) {
						pSbInfoDestW.TnsActive[hifilt] = 1
						tnsInfoDest.NumOfFilters[w] = 1
					}
					tnsDataDest.FiltersMerged = tnsDataSrc.FiltersMerged
					tnsInfoDest.Order[w][hifilt] = tnsInfoSrc.Order[w][hifilt]
					tnsInfoDest.Length[w][hifilt] = tnsInfoSrc.Length[w][hifilt]
					tnsInfoDest.Direction[w][hifilt] = tnsInfoSrc.Direction[w][hifilt]
					tnsInfoDest.CoefCompress[w][hifilt] = tnsInfoSrc.CoefCompress[w][hifilt]

					for i := 0; i < tC.MaxOrder; i++ {
						tnsInfoDest.Coef[w][hifilt][i] = tnsInfoSrc.Coef[w][hifilt][i]
					}
				} else {
					pSbInfoDestW.TnsActive[hifilt] = 0
					tnsInfoDest.NumOfFilters[w] = 0
				}
			}
		}
	}
}
