// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of libSBRenc/src/env_bit.cpp — the remaining SBR bit-writing routines
// that frame the SBR extension payload:
//   - FDKsbrEnc_InitSbrBitstream     (env_bit.cpp:153-178)
//   - FDKsbrEnc_AssembleSbrBitstream (env_bit.cpp:191-257)
//   - crcAdvance                     (env_bit.cpp:128-140)
//
// HE-AAC v1 only: the SBR_SYNTAX_CRC / SBR_SYNTAX_DRM_CRC (DRM) and
// SBR_SYNTAX_LOW_DELAY (ELD) branches are EXCLUDED — a GA HE-AAC stream sets
// none of those flags, so InitSbrBitstream just resets the SBR bitbuffer and
// AssembleSbrBitstream does only the GA fill-bit byte-alignment (the crcAdvance
// helper is ported 1:1 but only reachable from the excluded CRC path). fdk-aac
// SBR is FIXED-POINT — byte-identical.
package sbr

// crcAdvance is the 1:1 port of crcAdvance (env_bit.cpp:128-140): updates the
// CRC register for bBits of bValue. Reachable only from the excluded DRM/CRC
// path; ported for completeness.
func crcAdvance(crcPoly, crcMask uint16, crc *uint16, bValue uint32, bBits int) {
	for i := bBits - 1; i >= 0; i-- {
		var flag uint16
		if (*crc)&crcMask != 0 {
			flag = 1
		}
		if bValue&(1<<uint(i)) != 0 {
			flag ^= 1
		}
		*crc <<= 1
		if flag != 0 {
			*crc ^= crcPoly
		}
	}
}

// InitSbrBitstream is the 1:1 port of FDKsbrEnc_InitSbrBitstream
// (env_bit.cpp:153-178): resets the SBR bit-buffer and reserves space for the
// (absent, in v1) CRC. Returns the CRC region handle (always 0 for GA streams).
//
// HE-AAC v1: sbrSyntaxFlags carries neither SBR_SYNTAX_CRC nor
// SBR_SYNTAX_DRM_CRC, so no CRC bits are reserved.
func InitSbrBitstream(hCmonData *EncCommonData, sbrSyntaxFlags uint) int {
	hCmonData.SbrBitbuf.Reset()
	// CRC path excluded (sbrSyntaxFlags & SBR_SYNTAX_CRC == 0 for GA HE-AAC v1).
	return 0
}

// AssembleSbrBitstream is the 1:1 port of FDKsbrEnc_AssembleSbrBitstream
// (env_bit.cpp:191-257): byte-aligns the SBR payload with the 4-bit offset
// defined as part of sbr_extension_data (ISO/IEC 14496-3:2005 page 39) and
// appends the fill bits.
//
// HE-AAC v1: the DRM-CRC and the SBR_SYNTAX_CRC CRC-write branches are excluded.
func AssembleSbrBitstream(hCmonData *EncCommonData, sbrSyntaxFlags uint) {
	if hCmonData == nil {
		return
	}

	hCmonData.SbrFillBits = 0

	// !(SBR_SYNTAX_LOW_DELAY): GA byte-alignment with a 4-bit offset.
	if sbrSyntaxFlags&sbrSyntaxLowDelay == 0 {
		sbrLoad := hCmonData.SbrHdrBits + hCmonData.SbrDataBits
		// SBR_SYNTAX_CRC excluded (no SI_SBR_CRC_BITS added).
		sbrLoad += 4 // 4-bit offset before byte align.

		hCmonData.SbrFillBits = (8 - (sbrLoad % 8)) % 8

		hCmonData.SbrBitbuf.WriteBits(0, uint32(hCmonData.SbrFillBits))
	}

	// CRC calculation/write excluded (no SBR_SYNTAX_CRC).
}
