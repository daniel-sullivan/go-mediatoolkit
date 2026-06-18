// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Joint-stereo side-info read (the ms element): CJointStereo_Read restricted to
// the AAC-LC paths, ported 1:1 from libAACdec/src/stereo.cpp. AAC-LC never
// activates complex prediction (cplxPredictionActiv == 0), so MsMaskPresent
// cases 0/1/2 are handled and case 3 (USAC cplx-pred) returns a parse error,
// mirroring the C `return -1` when cplx prediction is signalled but inactive.

// jointStereoRead ports CJointStereo_Read (stereo.cpp) for AAC-LC: read the
// 2-bit ms_mask_present, clear MsUsed, then per case fill the per-band M/S flags.
func jointStereoRead(bs *bitStream, jsd *JointStereoData, windowGroups int,
	scaleFactorBandsTransmitted int, flags uint32) aacDecoderError {
	jsd.MsMaskPresent = uint8(bs.readBits(2))

	// FDKmemclear(MsUsed, scaleFactorBandsTransmitted).
	for i := 0; i < scaleFactorBandsTransmitted; i++ {
		jsd.MsUsed[i] = 0
	}

	switch jsd.MsMaskPresent {
	case 0:
		// no M/S — all flags already cleared.
	case 1:
		// read ms_used
		for group := 0; group < windowGroups; group++ {
			for band := 0; band < scaleFactorBandsTransmitted; band++ {
				jsd.MsUsed[band] |= uint8(bs.readBits(1)) << uint(group)
			}
		}
	case 2:
		// full spectrum M/S
		for band := 0; band < scaleFactorBandsTransmitted; band++ {
			jsd.MsUsed[band] = 255
		}
	case 3:
		// M/S disabled, complex stereo prediction enabled — USAC only. AAC-LC
		// (flags without USAC/RSVD) has cplxPredictionActiv == 0, so the C
		// returns -1 (parse error).
		if flags&(acUSAC|acRSVD50|acRSV603DA) == 0 {
			return aacDecParseError
		}
		return aacDecParseError
	}

	return aacDecOK
}
