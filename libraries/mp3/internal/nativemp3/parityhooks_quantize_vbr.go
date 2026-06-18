// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Exported test hooks for the vbr-iteration-loop parity oracle
// (internal/parity_tests/vbr-iteration-loop).
//
// The VBR iteration drivers (quantize_encode_vbr.go: vbrNewIterationLoop /
// vbrOldIterationLoop and their static prepares / bitpressure_strategy /
// vbrEncodeGranule) are a 1:1 translation of LAME's quantize.c VBR_*_iteration_-
// loop. They are normally reached through FrameEncodeStages (stages.go); these
// hooks add the thin entry the parity suite needs to drive the genuine loops over
// a LameInternalFlags the test populates field-by-field (cfg / sv_qnt / ATH /
// reservoir / scalefac_band / l3_side.tt geometry + xr + ratio), mirroring the C
// oracle's handle. mp3lame-gated like the slice.

// VBRNewIterationLoopParity drives vbrNewIterationLoop over gfc, for the parity
// oracle. The caller populates gfc exactly as the C oracle populates its handle.
func (gfc *LameInternalFlags) VBRNewIterationLoopParity(pe *[2][2]float32,
	msEnerRatio *[2]float32, ratio *[2][2]III_psy_ratio) {
	gfc.vbrNewIterationLoop(pe, msEnerRatio, ratio)
}

// VBROldIterationLoopParity drives vbrOldIterationLoop over gfc, for the parity
// oracle.
func (gfc *LameInternalFlags) VBROldIterationLoopParity(pe *[2][2]float32,
	msEnerRatio *[2]float32, ratio *[2][2]III_psy_ratio) {
	gfc.vbrOldIterationLoop(pe, msEnerRatio, ratio)
}
