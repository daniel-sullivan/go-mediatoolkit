// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Exported test hook for the vbrquantize-frame parity oracle
// (internal/parity_tests/vbrquantize-frame).
//
// The VBR bit-search orchestration tier (vbrquantize_frame.go: tryGlobalStepsize
// .. outOfBitsStrategy, reduce_bit_usage and VBR_encode_frame) is a 1:1
// translation of LAME's `static` vbrquantize.c drivers plus the non-static
// VBR_encode_frame entry. VBRencodeFrame is already exported (it is the encoder's
// per-frame VBR entry point); this hook only adds the thin input/output plumbing
// the parity suite needs so it can drive the genuine VBR_encode_frame and the Go
// port over byte-identical state and assert the per-granule scalefactors,
// quantized spectrum and bit usage match. mp3lame-gated like the slice.

// VBRencodeFrameParity drives VBRencodeFrame over gfc and returns its bit-usage
// result, for the parity oracle. The caller populates gfc (cfg, scalefac_band,
// HuffmanInit, the 2x2 l3_side.tt gr_info geometry) and the per-granule inputs
// exactly as the C oracle populates its handle. It exists only so the
// parity_tests package — which compiles the vendored vbrquantize.c +
// takehiro.c + tables.c oracle and cannot reach unexported helpers — has a
// single call shaped like the C trampoline.
func (gfc *LameInternalFlags) VBRencodeFrameParity(xr34orig *[2][2][576]float32,
	l3Xmin *[2][2][SFBMAX]float32, maxBits *[2][2]int) int {
	return VBRencodeFrame(gfc, xr34orig, l3Xmin, maxBits)
}
