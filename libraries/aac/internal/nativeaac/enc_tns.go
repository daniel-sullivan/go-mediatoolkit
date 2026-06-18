// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// TNS (Temporal Noise Shaping) encode — the reflection-coefficient quantization
// kernels, ported 1:1 from libAACenc/src/aacEnc_tns.cpp. These are the
// non-linear scalar quantizers that turn the LeRoux-Gueguen ParCor (reflection)
// coefficients into the on-wire 3-bit / 4-bit TNS coefficient indices, and the
// inverse map used when the encoder re-derives the LPC for filtering.
//
// The ParCor coefficients are FIXP_LPC, which on the build target (aarch64,
// LPC_TNS_COEF_RES default) is FIXP_SGL == int16 Q1.15 (FDK_lpc.h:128,130 ->
// FX_DBL2FXCONST_LPC == FX_DBL2FXCONST_SGL). The border / coefficient ROM tables
// are the int16-narrowed forms of the int32 hex constants in aacEnc_rom.cpp
// (FX_DBL2FXCONST_SGL narrowing, common_fix.h:160). The search and the inverse
// lookup are pure integer comparisons / indexing — no float, no transcendental —
// so they are bit-identical regardless of vectorization and are NOT gated by
// aac_strict.
//
// Scope: this file ports the quantization stage of TNS encode
// (FDKaacEnc_Search3/Search4, FDKaacEnc_Parcor2Index, FDKaacEnc_Index2Parcor).
// The autocorrelation / LeRoux-Gueguen LPC analysis (FDKaacEnc_TnsDetect ->
// FDKaacEnc_MergedAutoCorrelation -> CLpc_AutoToParcor) and the lattice filter
// (FDKaacEnc_TnsEncode -> CLpc_ParcorToLpc / CLpc_Analysis) pull in the entire
// FDK_lpc module plus the fixed-point invSqrtNorm2 / fPow / fDivNorm helpers and
// are a separate, larger area — intentionally not in this slice.

// FDKaacEnc_tnsEncCoeff3 ports FDKaacEnc_tnsEncCoeff3[8] (aacEnc_rom.cpp:818):
// the 3-bit-resolution TNS reflection-coefficient dequantization table.
// FIXP_LPC == FIXP_SGL on the build target, so these are the int16-narrowed
// (FX_DBL2FXCONST_SGL) forms of the int32 hex constants. Indexed by index[i]+4.
var fdkaacEncTnsEncCoeff3 = [8]int16{
	-32270, -28378, -21063, -11207, 0, 14218, 25619, 31946,
}

// FDKaacEnc_tnsCoeff3Borders ports FDKaacEnc_tnsCoeff3Borders[8]
// (aacEnc_rom.cpp:823): the 3-bit-resolution ParCor quantization decision
// borders. int16-narrowed FIXP_LPC. Used by FDKaacEnc_Search3.
var fdkaacEncTnsCoeff3Borders = [8]int16{
	-32768, -30792, -25102, -16384, -5690, 7292, 20431, 29523,
}

// FDKaacEnc_tnsEncCoeff4 ports FDKaacEnc_tnsEncCoeff4[16] (aacEnc_rom.cpp:837):
// the 4-bit-resolution TNS reflection-coefficient dequantization table.
// int16-narrowed FIXP_LPC. Indexed by index[i]+8.
var fdkaacEncTnsEncCoeff4 = [16]int16{
	-32628, -31517, -29333, -26149, -22076, -17250, -11837, -6021,
	0, 6813, 13328, 19261, 24351, 28378, 31164, 32588,
}

// FDKaacEnc_tnsCoeff4Borders ports FDKaacEnc_tnsCoeff4Borders[16]
// (aacEnc_rom.cpp:846): the 4-bit-resolution ParCor quantization decision
// borders. int16-narrowed FIXP_LPC. Used by FDKaacEnc_Search4.
var fdkaacEncTnsCoeff4Borders = [16]int16{
	-32768, -32210, -30555, -27860, -24216, -19747, -14606, -8967,
	-3023, 3425, 10126, 16384, 21926, 26510, 29935, 32052,
}

// fdkaacEncSearch3 ports the static FDKaacEnc_Search3 (aacEnc_tns.cpp:1141):
// quantize a ParCor coefficient to a 3-bit signed index in [-4,3] by finding
// the highest border it exceeds. parcor is FIXP_LPC (int16).
func fdkaacEncSearch3(parcor int16) int {
	index := 0
	for i := 0; i < 8; i++ {
		if parcor > fdkaacEncTnsCoeff3Borders[i] {
			index = i
		}
	}
	return index - 4
}

// fdkaacEncSearch4 ports the static FDKaacEnc_Search4 (aacEnc_tns.cpp:1150):
// quantize a ParCor coefficient to a 4-bit signed index in [-8,7] by finding
// the highest border it exceeds. parcor is FIXP_LPC (int16).
func fdkaacEncSearch4(parcor int16) int {
	index := 0
	for i := 0; i < 16; i++ {
		if parcor > fdkaacEncTnsCoeff4Borders[i] {
			index = i
		}
	}
	return index - 8
}

// fdkaacEncParcor2Index ports the static FDKaacEnc_Parcor2Index
// (aacEnc_tns.cpp:1164): non-linear quantization of `order` ParCor coefficients
// to signed indices using either the 3-bit (bitsPerCoeff==3) or 4-bit search.
// index must have length >= order.
func fdkaacEncParcor2Index(parcor []int16, index []int, order, bitsPerCoeff int) {
	for i := 0; i < order; i++ {
		if bitsPerCoeff == 3 {
			index[i] = fdkaacEncSearch3(parcor[i])
		} else {
			index[i] = fdkaacEncSearch4(parcor[i])
		}
	}
}

// fdkaacEncIndex2Parcor ports the static FDKaacEnc_Index2Parcor
// (aacEnc_tns.cpp:1185): inverse quantization of `order` TNS coefficient indices
// back to ParCor (reflection) coefficients via the dequantization table. The
// 4-bit table is offset by +8, the 3-bit table by +4. parcor must have length
// >= order.
func fdkaacEncIndex2Parcor(index []int, parcor []int16, order, bitsPerCoeff int) {
	for i := 0; i < order; i++ {
		if bitsPerCoeff == 4 {
			parcor[i] = fdkaacEncTnsEncCoeff4[index[i]+8]
		} else {
			parcor[i] = fdkaacEncTnsEncCoeff3[index[i]+4]
		}
	}
}
