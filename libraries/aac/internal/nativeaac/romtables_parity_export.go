// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// This file exposes thin exported wrappers around the unexported sfb-offset /
// sampling-rate-info ROM (aac_rom_sfb.go) so the cgo parity oracle in
// internal/parity_tests/rom-tables can drive them without being in-package. The
// wrappers add no logic: each forwards 1:1 to the ported lookup under test
// against its vendored C counterpart (libAACdec/src/channelinfo.cpp +
// aac_rom.cpp). They exist solely for the parity harness — the production decode
// path uses the unexported forms directly.

// SamplingRateInfo is the exported mirror of the ported SamplingRateInfo
// (channelinfo.h:152), holding the result of GetSamplingRateInfo so the parity
// harness can compare every field against the vendored C struct.
type SamplingRateInfo struct {
	ScaleFactorBandsLong          []int16
	ScaleFactorBandsShort         []int16
	NumberOfScaleFactorBandsLong  uint8
	NumberOfScaleFactorBandsShort uint8
	SamplingRateIndex             uint32
	SamplingRate                  uint32
}

// GetSamplingRateInfo wraps getSamplingRateInfo (channelinfo.cpp:225): resolve
// the scalefactor-band ROM for the given frame length / sampling-rate index /
// rate. It returns the filled SamplingRateInfo and the raw C error code
// (0 == AAC_DEC_OK, 0x2003 == AAC_DEC_UNSUPPORTED_FORMAT). On the unsupported
// frame-length path the C function returns before touching the struct, so the
// returned info mirrors that (only the fields set so far are populated).
func GetSamplingRateInfo(samplesPerFrame, samplingRateIndex, samplingRate uint32) (SamplingRateInfo, int) {
	var t samplingRateInfo
	err := getSamplingRateInfo(&t, samplesPerFrame, samplingRateIndex, samplingRate)
	return SamplingRateInfo{
		ScaleFactorBandsLong:          t.scaleFactorBandsLong,
		ScaleFactorBandsShort:         t.scaleFactorBandsShort,
		NumberOfScaleFactorBandsLong:  t.numberOfScaleFactorBandsLong,
		NumberOfScaleFactorBandsShort: t.numberOfScaleFactorBandsShort,
		SamplingRateIndex:             t.samplingRateIndex,
		SamplingRate:                  t.samplingRate,
	}, int(err)
}
