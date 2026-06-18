// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Thin exported driver for the PS decorrelator cgo parity oracle: open + init
// DECORR_PS (isLegacyPS, 71 hybrid bands) then run per-slot FDKdecorrelateApply,
// returning the in-place-modified left (direct) + the decorrelated right hybrid
// data. Mirrors exactly what the C bridge's qparity_decorr does.

// PsDecorrRun drives the PS decorrelator over nSlots slots of 71 hybrid bands.
func PsDecorrRun(nSlots int, inRe, inImg []int32) (leftRe, leftImg, rightRe, rightImg []int32) {
	var dec decorrDec
	buf := make([]int32, 2*(825+373))
	fdkDecorrelateOpen(&dec, buf)
	fdkDecorrelateInit(&dec, 71, decorrPs, duckerAutomatic, 0, 0, 0, 0, 1, 1)

	const nb = 71
	leftRe = make([]int32, nSlots*nb)
	leftImg = make([]int32, nSlots*nb)
	rightRe = make([]int32, nSlots*nb)
	rightImg = make([]int32, nSlots*nb)

	for s := 0; s < nSlots; s++ {
		lRe := make([]int32, nb)
		lImg := make([]int32, nb)
		rRe := make([]int32, nb)
		rImg := make([]int32, nb)
		copy(lRe, inRe[s*nb:(s+1)*nb])
		copy(lImg, inImg[s*nb:(s+1)*nb])
		fdkDecorrelateApply(&dec, lRe, lImg, rRe, rImg, 0)
		copy(leftRe[s*nb:(s+1)*nb], lRe)
		copy(leftImg[s*nb:(s+1)*nb], lImg)
		copy(rightRe[s*nb:(s+1)*nb], rRe)
		copy(rightImg[s*nb:(s+1)*nb], rImg)
	}
	return leftRe, leftImg, rightRe, rightImg
}
