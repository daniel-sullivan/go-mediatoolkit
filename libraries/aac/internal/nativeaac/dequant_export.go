// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.

package nativeaac

// This file exposes thin exported wrappers around the unexported dequant
// orchestration kernels (dequant.go) so the cgo parity oracle in
// internal/parity_tests/dequant can drive them without being in-package. The
// wrappers add no logic: each forwards 1:1 to the ported driver under test
// against its vendored C counterpart (libAACdec/src/block.cpp + aacdec_pns.cpp).
// They exist solely for the parity harness — the production decode path uses the
// unexported forms directly.

// DequantInput carries the AAC-LC dequant context the three drivers read: the
// parsed window/grouping structure, the sampling-rate ROM selection, the global
// gain, and the per-band section codebook layout. All fields are the
// int32-comparable shapes the parity oracle fabricates identically on the C
// side. flags is the AC_* bitmask (0 for AAC-LC).
type DequantInput struct {
	SamplesPerFrame   uint32
	SamplingRateIndex uint32
	SamplingRate      uint32
	GlobalGain        uint8
	Flags             uint32

	// Window/grouping fields mirroring cIcsInfo (already parsed upstream).
	WindowSequence      uint8
	WindowGroups        uint8
	WindowGroupLength   [8]uint8
	ScaleFactorGrouping uint8
	MaxSfBands          uint8

	// CodeBook is the flat per-(group*16+band) section codebook array
	// (pDynData->aCodeBook) produced by the section-data read.
	CodeBook [8 * 16]uint8
}

// DequantResult is the flattened, exported view of the dequant outputs the
// parity oracle asserts bit-for-bit against the C: the scaled spectral
// coefficients, the per-(window,sfb) and per-window block exponents, the
// per-band scalefactors, and the two driver return codes.
type DequantResult struct {
	Spectrum    []int32       // scaled FIXP_DBL MDCT lines, window-major
	ScaleFactor [8 * 16]int16 // aScaleFactor[group*16+band]
	SfbScale    [8 * 16]int16 // aSfbScale[window*16+band]
	SpecScale   [8]int16      // per-window block exponent
	ReadSfErr   int           // CBlock_ReadScaleFactorData return code
	InvQuantErr int           // CBlock_InverseQuantizeSpectralData return code
}

// granuleLengthFor returns the per-window spectrum stride: samplesPerFrame for a
// long block, samplesPerFrame/8 for a short block. Mirrors
// pAacDecoderChannelInfo->granuleLength (channel.cpp), the SPEC() stride.
func granuleLengthFor(samplesPerFrame uint32, windowSequence uint8) int {
	if blockType(windowSequence) == blockShort {
		return int(samplesPerFrame) / 8
	}
	return int(samplesPerFrame)
}

// RunDequant runs the full AAC-LC dequant stage over a fabricated channel
// context: it reconstructs the bit reader for the scalefactor data, resolves the
// sampling-rate info, rebuilds the cIcsInfo from the supplied window/grouping
// fields, then runs readScaleFactorData → inverseQuantizeSpectralData →
// scaleSpectralData on the supplied raw quantized spectrum (which it copies, so
// the caller's input is preserved). TNS is inactive (no-TNS path). Returns the
// flattened result.
//
// scaleFactorBuf is the raw bitstream the scalefactor read consumes (bufSize a
// power of two, validBits its valid-bit count). rawSpectrum is the quantized
// MDCT buffer laid out window-major with stride granuleLength(samplesPerFrame,
// windowSequence); it is copied into the result before inverse quantization.
func RunDequant(in DequantInput, scaleFactorBuf []byte, bufSize, validBits uint32, rawSpectrum []int32) DequantResult {
	var bs bitStream
	initBitStream(&bs, scaleFactorBuf, bufSize, validBits)

	var sri samplingRateInfo
	getSamplingRateInfo(&sri, in.SamplesPerFrame, in.SamplingRateIndex, in.SamplingRate)

	ics := cIcsInfo{
		windowGroupLength:   in.WindowGroupLength,
		windowGroups:        in.WindowGroups,
		valid:               1,
		windowSequence:      blockType(in.WindowSequence),
		maxSfBands:          in.MaxSfBands,
		scaleFactorGrouping: in.ScaleFactorGrouping,
	}
	if isLongBlock(&ics) {
		ics.totalSfBands = sri.numberOfScaleFactorBandsLong
	} else {
		ics.totalSfBands = sri.numberOfScaleFactorBandsShort
	}

	granuleLength := granuleLengthFor(in.SamplesPerFrame, in.WindowSequence)

	var res DequantResult
	res.Spectrum = append([]int32(nil), rawSpectrum...)

	codeBook := in.CodeBook
	var scaleFactor [8 * 16]int16
	var pns cPnsData

	readErr := readScaleFactorData(&bs, &ics, &sri, codeBook[:], scaleFactor[:], in.GlobalGain, &pns, in.Flags)

	var sfbScale [8 * 16]int16
	invErr := inverseQuantizeSpectralData(&ics, &sri, codeBook[:], sfbScale[:], scaleFactor[:], res.Spectrum, granuleLength, nil, 0)

	var specScale [8]int16
	var tnsData CTnsData // inactive: Active == 0, no filters
	scaleSpectralData(&ics, &sri, ics.maxSfBands, sfbScale[:], specScale[:], res.Spectrum, granuleLength, &tnsData, 0)

	res.ScaleFactor = scaleFactor
	res.SfbScale = sfbScale
	res.SpecScale = specScale
	res.ReadSfErr = int(readErr)
	res.InvQuantErr = int(invErr)
	return res
}
