// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Parity-only exports for the perceptual-entropy (line_pe.cpp) port. These thin
// wrappers let the cgo parity slice under internal/parity_tests/enc-adj-thr/
// drive the unexported prepareSfbPe / calcSfbPe against the genuine vendored
// FDKaacEnc_prepareSfbPe / FDKaacEnc_calcSfbPe and compare bit-for-bit. Not part
// of the production API.

// ParityPrepareSfbPe runs prepareSfbPe over flat ld-domain inputs and returns
// the resulting sfbNLines (length maxGroupedSFB == 60), matching the
// PE_CHANNEL_DATA.sfbNLines layout the C oracle fills.
func ParityPrepareSfbPe(
	sfbEnergyLdData, sfbThresholdLdData, sfbFormFactorLdData, sfbOffset []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int) []int32 {
	var pc peChannelData
	prepareSfbPe(&pc, sfbEnergyLdData, sfbThresholdLdData, sfbFormFactorLdData,
		sfbOffset, sfbCnt, sfbPerGroup, maxSfbPerGroup)
	out := make([]int32, maxGroupedSFB)
	copy(out, pc.sfbNLines[:])
	return out
}

// ParityCalcSfbPe seeds a peChannelData with the given sfbNLines, runs calcSfbPe
// over the flat ld-domain energies/thresholds, and returns the resulting
// (sfbPe, sfbConstPart, sfbNActiveLines) arrays (each length maxGroupedSFB) and
// the channel sums (pe, constPart, nActiveLines) — exactly the PE_CHANNEL_DATA
// fields the C oracle produces.
func ParityCalcSfbPe(
	sfbNLines, sfbEnergyLdData, sfbThresholdLdData []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int,
	isBook, isScale []int32) (sfbPe, sfbConstPart, sfbNActiveLines []int32, pe, constPart, nActiveLines int32) {
	var pc peChannelData
	copy(pc.sfbNLines[:], sfbNLines)
	calcSfbPe(&pc, sfbEnergyLdData, sfbThresholdLdData,
		sfbCnt, sfbPerGroup, maxSfbPerGroup, isBook, isScale)
	sfbPe = append([]int32(nil), pc.sfbPe[:]...)
	sfbConstPart = append([]int32(nil), pc.sfbConstPart[:]...)
	sfbNActiveLines = append([]int32(nil), pc.sfbNActiveLines[:]...)
	return sfbPe, sfbConstPart, sfbNActiveLines, pc.pe, pc.constPart, pc.nActiveLines
}

// ParityCalcInvLdData / ParityCalcLdInt / ParityFMultNorm / ParityFMultI export
// the LD-domain helper kernels for direct bit-exact comparison against the
// vendored CalcInvLdData / CalcLdInt / fMultNorm / fMultI.
func ParityCalcInvLdData(x int32) int32 { return calcInvLdData(x) }

// ParityCalcLdInt wraps calcLdInt.
func ParityCalcLdInt(i int32) int32 { return calcLdInt(i) }

// ParityFMultNorm wraps fMultNorm, returning (mantissa, exponent).
func ParityFMultNorm(f1, f2 int32) (int32, int32) { return fMultNorm(f1, f2) }

// ParityFMultI wraps fMultI.
func ParityFMultI(a, b int32) int32 { return fMultI(a, b) }
