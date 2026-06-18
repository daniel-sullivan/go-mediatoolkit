// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// TNS-filter scale-head-room computation for the AAC decoder, ported 1:1 from
// the TNS branch of CBlock_ScaleSpectralData in block.cpp:247-294.
//
// This is the only TNS-filter-specific code in block.cpp: before the spectrum
// is down-shifted to a common per-window block exponent, the TNS bands need
// extra mantissa head room so the lattice predictor (applied later by
// CTns_Apply / CLpc_SynthesisLattice) cannot overflow. The computation is pure
// integer (max/min of SHORT exponents plus a count-leading-bits head-room
// query over the FIXP_DBL spectrum); parity is EXACT integer equality, as for
// the rest of the fixed-point AAC port (see nativeaac.go).
//
// The surrounding loop structure of CBlock_ScaleSpectralData (the window/group
// iteration and the post-headroom down-shift) belongs to the broader
// block-scaling area; this function isolates the TNS branch exactly as written
// in the C, taking the per-window context the C reads from the channel info as
// explicit parameters:
//
//   - tnsData       — pAacDecoderChannelInfo->pDynData->TnsData
//   - window        — the current window index
//   - specScaleIn   — SpecScale_window on entry to the TNS branch (the max sfb
//                     scale already folded in for [0, maxSfbs))
//   - sfbScale      — pAacDecoderChannelInfo->pDynData->aSfbScale, the flat
//                     [window*16 + band] SHORT exponent array
//   - bandOffsets   — GetScaleFactorBandOffsets(...) result (SHORT line
//                     offsets, indexed by scalefactor band)
//   - spectrum      — SPEC(pSpectralCoefficient, window, granuleLength), the
//                     FIXP_DBL MDCT lines for this window
//   - maxTnsBands   — GetMaximumTnsBands(icsInfo, samplingRateIndex)
//
// It returns the updated SpecScale_window. When the TNS branch is inactive (no
// active filters), it returns specScaleIn unchanged, mirroring the C's guard.

// cBlockScaleSpectralDataTnsHeadroom adds the TNS mantissa head room into the
// per-window block exponent. Ported 1:1 from block.cpp:247-294.
func cBlockScaleSpectralDataTnsHeadroom(
	tnsData *CTnsData,
	window int,
	specScaleIn int32,
	sfbScale []int16,
	bandOffsets []int16,
	spectrum []int32,
	maxTnsBands uint8,
) int32 {
	specScaleWindow := specScaleIn

	if tnsData.Active != 0 && tnsData.NumberOfFilters[window] > 0 {
		// Find max scale of TNS bands.
		var specScaleWindowTns int32 = 0
		tnsStart := int32(maxTnsBands)
		var tnsStop int32 = 0

		for filterIndex := 0; filterIndex < int(tnsData.NumberOfFilters[window]); filterIndex++ {
			for band := tnsData.Filter[window][filterIndex].StartBand; band < tnsData.Filter[window][filterIndex].StopBand; band++ {
				specScaleWindowTns = fMax(specScaleWindowTns, int32(sfbScale[window*16+int(band)]))
			}
			// Find TNS line boundaries for all TNS filters.
			tnsStart = fMin(tnsStart, int32(tnsData.Filter[window][filterIndex].StartBand))
			tnsStop = fMax(tnsStop, int32(tnsData.Filter[window][filterIndex].StopBand))
		}
		specScaleWindowTns = specScaleWindowTns + int32(tnsData.GainLd)
		// FDK_ASSERT(tns_stop >= tns_start); — preserved as a non-fatal
		// expectation; the C build drops the assert in release.

		// Consider existing head room of all MDCT lines inside the TNS bands.
		start := int32(bandOffsets[tnsStart])
		stop := int32(bandOffsets[tnsStop])
		specScaleWindowTns -= getScalefactor(spectrum[start:], stop-start)
		if specScaleWindow <= 17 {
			specScaleWindowTns++
		}
		// Add enough mantissa head room such that the spectrum is still
		// representable after applying TNS.
		specScaleWindow = fMax(specScaleWindow, specScaleWindowTns)
	}

	return specScaleWindow
}
