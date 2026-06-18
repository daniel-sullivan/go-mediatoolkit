// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.

package nativeaac

// This file exposes thin exported wrappers around the unexported ics-parse
// kernels (ics_parse.go) so the cgo parity oracle in
// internal/parity_tests/ics-parse can drive them without being in-package. The
// wrappers add no logic: each forwards 1:1 to the ported kernel under test.

// IcsParseResult is the flattened, exported view of the cIcsInfo + cSectionData a
// raw_data_block ics-parse produces, plus the return code. All fields are the
// int32-comparable shapes the parity oracle asserts against the C struct.
type IcsParseResult struct {
	WindowGroupLength   [8]uint8
	WindowGroups        uint8
	Valid               uint8
	WindowShape         uint8
	WindowSequence      uint8
	MaxSfBands          uint8
	ScaleFactorGrouping uint8
	TotalSfBands        uint8
	CodeBook            [8 * 16]uint8
	NumberSection       int
	ErrorCode           int
}

// ParseIcsAndSectionData runs the AAC-LC ics-parse stage over pBuffer: it
// reconstructs the bit reader, resolves the sampling-rate info, runs icsRead
// then (on success) readSectionData, and returns the flattened result.
//
// samplesPerFrame / samplingRateIndex / samplingRate select the scalefactor-band
// ROM (getSamplingRateInfo); commonWindow gates the intensity-codebook legality
// check; flags is the AC_* bitmask (0 for AAC-LC). bufSize must be a power of
// two (the FDK_BITBUF invariant) and validBits the number of valid bits.
//
// This mirrors exactly what CChannelElement_Read does for the ics_info +
// section_data element items: IcsRead followed by CBlock_ReadSectionData on the
// same bit reader, with no intervening reads.
func ParseIcsAndSectionData(pBuffer []byte, bufSize, validBits uint32,
	samplesPerFrame, samplingRateIndex, samplingRate uint32,
	commonWindow uint8, flags uint32) IcsParseResult {

	var bs bitStream
	initBitStream(&bs, pBuffer, bufSize, validBits)

	var sri samplingRateInfo
	getSamplingRateInfo(&sri, samplesPerFrame, samplingRateIndex, samplingRate)

	var ics cIcsInfo
	err := icsRead(&bs, &ics, &sri, flags)

	var sd cSectionData
	if err == aacDecOK {
		err = readSectionData(&bs, &ics, &sri, commonWindow, flags, &sd)
	}

	return IcsParseResult{
		WindowGroupLength:   ics.windowGroupLength,
		WindowGroups:        ics.windowGroups,
		Valid:               ics.valid,
		WindowShape:         ics.windowShape,
		WindowSequence:      uint8(ics.windowSequence),
		MaxSfBands:          ics.maxSfBands,
		ScaleFactorGrouping: ics.scaleFactorGrouping,
		TotalSfBands:        ics.totalSfBands,
		CodeBook:            sd.aCodeBook,
		NumberSection:       sd.numberSection,
		ErrorCode:           int(err),
	}
}

// BitPosition returns the absolute bit-consumption position of a bit reader
// initialised over pBuffer after running the ics-parse stage — used by the
// oracle to cross-check that both sides consumed identical bit counts.
func ParseIcsAndSectionDataBitPos(pBuffer []byte, bufSize, validBits uint32,
	samplesPerFrame, samplingRateIndex, samplingRate uint32,
	commonWindow uint8, flags uint32) uint32 {

	var bs bitStream
	initBitStream(&bs, pBuffer, bufSize, validBits)

	var sri samplingRateInfo
	getSamplingRateInfo(&sri, samplesPerFrame, samplingRateIndex, samplingRate)

	var ics cIcsInfo
	if err := icsRead(&bs, &ics, &sri, flags); err == aacDecOK {
		var sd cSectionData
		readSectionData(&bs, &ics, &sri, commonWindow, flags, &sd)
	}

	// FDKgetBitCnt == bitNdx - BitsInCache (the unconsumed cache bits).
	return bs.bitBuf.bitNdx - bs.bitsInCache
}
