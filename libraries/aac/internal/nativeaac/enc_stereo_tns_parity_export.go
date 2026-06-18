// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Thin exported wrappers around the ENCODE M/S-stereo decision (enc_ms_stereo.go)
// and the TNS-encode reflection-coefficient quantizers (enc_tns.go) so the cgo
// parity oracle in internal/parity_tests/enc-stereo-tns can drive them without
// being in-package. They forward 1:1; the M/S wrapper additionally publishes the
// in-place-mutated MsStereoData arrays + msMask + msDigest back to the caller for
// element-for-element comparison. All values are int32 FIXP_DBL / int16 FIXP_LPC
// fixed-point — no float — so the oracle asserts EXACT integer equality.

// EncMsStereoProcessing forwards to MsStereoProcessing (enc_ms_stereo.go). It
// mutates d and msMask in place and returns msDigest, exactly as the C
// FDKaacEnc_MsStereoProcessing. Re-exported only for the parity harness.
func EncMsStereoProcessing(d *MsStereoData, isBook, msMask []int32,
	allowMS, sfbCnt, sfbPerGroup, maxSfbPerGroup int, sfbOffset []int32) int {
	return MsStereoProcessing(d, isBook, msMask, allowMS, sfbCnt, sfbPerGroup,
		maxSfbPerGroup, sfbOffset)
}

// EncTnsParcor2Index forwards to fdkaacEncParcor2Index (enc_tns.go): non-linear
// quantization of ParCor coefficients to signed TNS coefficient indices.
func EncTnsParcor2Index(parcor []int16, order, bitsPerCoeff int) []int {
	index := make([]int, order)
	fdkaacEncParcor2Index(parcor, index, order, bitsPerCoeff)
	return index
}

// EncTnsIndex2Parcor forwards to fdkaacEncIndex2Parcor (enc_tns.go): inverse
// quantization of TNS coefficient indices back to ParCor coefficients.
func EncTnsIndex2Parcor(index []int, order, bitsPerCoeff int) []int16 {
	parcor := make([]int16, order)
	fdkaacEncIndex2Parcor(index, parcor, order, bitsPerCoeff)
	return parcor
}

// EncTnsRom publishes the ported TNS-encode reflection-coefficient ROM tables
// (3-bit/4-bit dequant tables + their decision borders) so the parity oracle
// verifies the int16-narrowed transcription bit-for-bit against the genuine
// vendored FDKaacEnc_tnsEncCoeff*/tnsCoeff*Borders.
func EncTnsRom() (encCoeff3, coeff3Borders, encCoeff4, coeff4Borders []int16) {
	encCoeff3 = fdkaacEncTnsEncCoeff3[:]
	coeff3Borders = fdkaacEncTnsCoeff3Borders[:]
	encCoeff4 = fdkaacEncTnsEncCoeff4[:]
	coeff4Borders = fdkaacEncTnsCoeff4Borders[:]
	return
}

// MsStereoArrays bundles all per-band MsStereoData slices as a flat exported
// struct so the oracle can both seed the inputs and read back every mutated
// output for comparison. Field order mirrors MsStereoData.
type MsStereoArrays struct {
	SfbEnergyLeft, SfbEnergyRight           []int32
	SfbEnergyMid, SfbEnergySide             []int32
	SfbThresholdLeft, SfbThresholdRight     []int32
	SfbSpreadEnLeft, SfbSpreadEnRight       []int32
	SfbEnergyLeftLd, SfbEnergyRightLd       []int32
	SfbEnergyMidLd, SfbEnergySideLd         []int32
	SfbThresholdLeftLd, SfbThresholdRightLd []int32
	MdctSpectrumLeft, MdctSpectrumRight     []int32
}

// NewMsStereoData builds an MsStereoData backed by the given MsStereoArrays
// (no copy — the arrays are mutated in place by MsStereoProcessing).
func NewMsStereoData(a *MsStereoArrays) *MsStereoData {
	return &MsStereoData{
		SfbEnergyLeft:       a.SfbEnergyLeft,
		SfbEnergyRight:      a.SfbEnergyRight,
		SfbEnergyMid:        a.SfbEnergyMid,
		SfbEnergySide:       a.SfbEnergySide,
		SfbThresholdLeft:    a.SfbThresholdLeft,
		SfbThresholdRight:   a.SfbThresholdRight,
		SfbSpreadEnLeft:     a.SfbSpreadEnLeft,
		SfbSpreadEnRight:    a.SfbSpreadEnRight,
		SfbEnergyLeftLd:     a.SfbEnergyLeftLd,
		SfbEnergyRightLd:    a.SfbEnergyRightLd,
		SfbEnergyMidLd:      a.SfbEnergyMidLd,
		SfbEnergySideLd:     a.SfbEnergySideLd,
		SfbThresholdLeftLd:  a.SfbThresholdLeftLd,
		SfbThresholdRightLd: a.SfbThresholdRightLd,
		MdctSpectrumLeft:    a.MdctSpectrumLeft,
		MdctSpectrumRight:   a.MdctSpectrumRight,
	}
}
