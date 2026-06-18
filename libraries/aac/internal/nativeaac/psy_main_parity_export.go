// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Parity exports for the psy-main driver leaf kernels (spreading, pre-echo,
// tonality, short-block grouping). These thin wrappers let the
// internal/parity_tests/enc-psy-main slice drive the unexported 1:1 ports and
// compare them bit-for-bit against the genuine vendored FDK-AAC functions
// (FDKaacEnc_SpreadingMax, FDKaacEnc_PreEchoControl /
// FDKaacEnc_InitPreEchoControl, FDKaacEnc_CalculateFullTonality,
// FDKaacEnc_groupShortData). Not part of the shipping decode/encode surface.

// SpreadingMax forwards to spreadingMax (psy_spreading.go), the 1:1 port of
// FDKaacEnc_SpreadingMax (spreading.cpp:105).
func SpreadingMax(pbCnt int, maskLowFactor, maskHighFactor, pbSpreadEnergy []int32) {
	spreadingMax(pbCnt, maskLowFactor, maskHighFactor, pbSpreadEnergy)
}

// InitPreEchoControl forwards to initPreEchoControl (psy_preecho.go), the 1:1
// port of FDKaacEnc_InitPreEchoControl (pre_echo_control.cpp:106). Returns
// (mdctScalenm1, calcPreEcho).
func InitPreEchoControl(pbThresholdNm1, sfbPcmQuantThreshold []int32, numPb int) (int, int) {
	return initPreEchoControl(pbThresholdNm1, sfbPcmQuantThreshold, numPb)
}

// PreEchoControl forwards to preEchoControl (psy_preecho.go), the 1:1 port of
// FDKaacEnc_PreEchoControl (pre_echo_control.cpp:117). Returns the updated
// mdctScalenm1.
func PreEchoControl(
	pbThresholdNm1 []int32, calcPreEcho, numPb, maxAllowedIncreaseFactor int,
	minRemainingThresholdFactor int16, pbThreshold []int32,
	mdctScale, mdctScalenm1 int,
) int {
	return preEchoControl(pbThresholdNm1, calcPreEcho, numPb, maxAllowedIncreaseFactor,
		minRemainingThresholdFactor, pbThreshold, mdctScale, mdctScalenm1)
}

// CalculateFullTonality forwards to calculateFullTonality (psy_tonality.go),
// the 1:1 port of FDKaacEnc_CalculateFullTonality (tonality.cpp:121). Writes
// sfbTonality[0:sfbCnt].
func CalculateFullTonality(
	spectrum []int32, sfbMaxScaleSpec []int, sfbEnergyLD64 []int32,
	sfbTonality []int16, sfbCnt int, sfbOffset []int, usePns int,
) {
	calculateFullTonality(spectrum, sfbMaxScaleSpec, sfbEnergyLD64, sfbTonality, sfbCnt, sfbOffset, usePns)
}

// SfbGrouped is the exported view of the grp_data SFB union (sfbGrouped), so
// the parity slice can populate the Short[wnd][sfb] inputs and read the Long[]
// outputs.
type SfbGrouped = sfbGrouped

// GroupShortData forwards to groupShortData (psy_grpdata.go), the 1:1 port of
// FDKaacEnc_groupShortData (grp_data.cpp:118). Returns maxSfbPerGroup.
func GroupShortData(
	mdctSpectrum []int32,
	sfbThreshold, sfbEnergy, sfbEnergyMS, sfbSpreadEnergy *SfbGrouped,
	sfbCnt, sfbActive int, sfbOffset []int,
	sfbMinSnrLdData []int32,
	groupedSfbOffset []int,
	groupedSfbMinSnrLdData []int32,
	noOfGroups int, groupLen []int, granuleLength int,
) int {
	return groupShortData(mdctSpectrum, sfbThreshold, sfbEnergy, sfbEnergyMS, sfbSpreadEnergy,
		sfbCnt, sfbActive, sfbOffset, sfbMinSnrLdData, groupedSfbOffset,
		groupedSfbMinSnrLdData, noOfGroups, groupLen, granuleLength)
}
