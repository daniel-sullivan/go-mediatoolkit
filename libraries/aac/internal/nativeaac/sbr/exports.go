// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Exported entry points for the HE-AAC v1 SBR decode path, used by the AAC-LC
// core integration (internal/nativeaac/decode_sbr.go) and the sbr-dec-e2e parity
// oracle. They wrap the package-private orchestration (SbrDecoderOpen /
// SbrDecoderInitElement / SbrDecoderApply / SbrDecoderParse) so callers never
// need to name the private qmfDomain / sbrError types.

// SbrdecOK is the exported SBRDEC_OK success sentinel.
const SbrdecOK = sbrdecOK

// NewDecoderInstance builds a ready-to-use HE-AAC v1 SBR decoder instance: it
// allocates the QMF domain, opens the decoder, and initialises one element. It
// mirrors the InitElement the AAC decoder runs on SBR signalling. coreCodec is
// AOT_AAC_LC (2) or AOT_SBR (5); elementID is ID_SCE (0) or ID_CPE (1);
// sampleRateOut == 0 requests implicit dual-rate. Returns nil on bad config.
func NewDecoderInstance(sampleRateIn, sampleRateOut, coreFrameLen, coreCodec, elementID, elementIndex int) *SbrDecoderInstance {
	qd := new(qmfDomain)
	inst := SbrDecoderOpen(qd)
	if SbrDecoderInitElement(inst, sampleRateIn, sampleRateOut, coreFrameLen, coreCodec, elementID, elementIndex) != sbrdecOK {
		return nil
	}
	return inst
}

// SbrDecoderApplyExt is the exported wrapper over SbrDecoderApply for the HE-AAC
// v1 path (no PS). input holds the AAC-LC core int32 time signal
// (numChannels*coreFrameLen, channel-blocked); timeData receives the interleaved
// SBR output. *numChannels / *sampleRate are updated. Returns true on success.
func SbrDecoderApplyExt(self *SbrDecoderInstance, input, timeData []int32, numChannels, sampleRate *int,
	coreDecodedOk bool, inDataHeadroom int) bool {
	psDecoded := 0
	return SbrDecoderApply(self, input, timeData, numChannels, sampleRate, coreDecodedOk, inDataHeadroom, &psDecoded) == sbrdecOK
}

// SbrDecoderApplyPS is the exported PS-aware wrapper over SbrDecoderApply. It is
// identical to SbrDecoderApplyExt but threads the psPossible/psDecoded in/out
// flag: on entry *psDecoded != 0 requests PS (psPossible) for a single mono SCE;
// on return *psDecoded reports whether PS was actually applied and *numChannels is
// 2 for a PS stereo output. input is the mono core (coreFrameLen int32); timeData
// must hold 2*coreFrameLen*2 int32 for the interleaved stereo SBR output.
func SbrDecoderApplyPS(self *SbrDecoderInstance, input, timeData []int32, numChannels, sampleRate *int,
	coreDecodedOk bool, inDataHeadroom int, psDecoded *int) bool {
	return SbrDecoderApply(self, input, timeData, numChannels, sampleRate, coreDecodedOk, inDataHeadroom, psDecoded) == sbrdecOK
}

// SbrDecoderParseAU drives sbrDecoder_Parse over buf at the absolute bit position
// startBitPos (bits from the buffer origin). It builds its own SBR bit reader,
// advances to startBitPos, parses the sbr_extension_data, and returns the number
// of bits consumed. *countBits is updated to the remaining payload-bit count by
// SbrDecoderParse. crcFlag must be 0 (EXT_SBR_DATA). prevElement is the preceding
// core element ID (ID_SCE / ID_CPE).
func SbrDecoderParseAU(self *SbrDecoderInstance, buf []byte, bufSize uint32, startBitPos uint32,
	countBits *int, crcFlag, prevElement, elementIndex int) int {
	bs := newSbrBitStream(buf, bufSize, bufSize*8)
	bs.pushFor(startBitPos)
	before := int(bs.getValidBits())
	SbrDecoderParse(self, bs, countBits, *countBits, crcFlag, prevElement, elementIndex)
	after := int(bs.getValidBits())
	return before - after
}
