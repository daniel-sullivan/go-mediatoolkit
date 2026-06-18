// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Thin exported driver for the PS apply (full synthesis) cgo parity oracle
// (internal/parity_tests/ps-dec-apply). It mirrors exactly what the C bridge
// does: CreatePsDec, parse the ps_data payload (ReadPsData), DecodePs, then run
// the per-slot loop (initSlotBasedRotation at each envelope border, ApplyPsSlot
// for every column) over the supplied left/mono QMF buffer, returning the
// in-place-modified left channel + the synthesised right channel.

// PsApplyRun drives the full PS mono->stereo synthesis. lowBandReal/lowBandImag
// are (noCol + HYBRID_FILTER_DELAY) rows of 64 QMF bands for the left/mono input
// (the extra HYBRID_FILTER_DELAY rows are the look-ahead the hybrid analysis
// needs). The PS payload (pBuffer, validBits) is parsed and decoded first. lsb /
// usb are the SBR low/high subband split. The scale factors are passed through to
// ApplyPsSlot unchanged. Returns the left (in-place modified) and right output
// QMF channels, noCol rows of 64 bands each, flattened.
func PsApplyRun(aacSamplesPerFrame int, pBuffer []byte, bufSize, validBits uint32,
	noCol, lsb, usb, scaleFactorLowBandNoOv, scaleFactorLowBand, scaleFactorHighBand, highSubband int,
	lowBandRealFlat, lowBandImagFlat []int32) (outLeftRe, outLeftImg, outRightRe, outRightImg []int32, psProcess int) {

	h := new(psDec)
	if createPsDec(h, aacSamplesPerFrame) != 0 {
		return nil, nil, nil, nil, -1
	}
	// Single-slot delay line (no frame delay): read + process slot 0.
	h.BsLastSlot = 0
	h.BsReadSlot = 0
	h.ProcessSlot = 0
	// procFrameBased starts at -1 (set by createPsDec); the e2e path runs slot
	// based directly, so leave the warm-up (PreparePsProcessing) out — match by
	// setting procFrameBased to 0 so the hybrid delay buffer is filled purely by
	// the per-slot analysis (the C oracle does the same).
	h.ProcFrameBased = 0

	bs := newSbrBitStream(pBuffer, bufSize, validBits)
	readPsData(h, bs, int(validBits))

	var coef psDecCoefficients
	psProcess = decodePs(h, 0, &coef)
	h.PsDecodedPrv = uint8(psProcess)

	const bands = 64
	totalRows := noCol + psHybridFilterDelay

	// Build the [row][band] pointer view of the left QMF buffer.
	left := make([][]int32, totalRows)
	leftImg := make([][]int32, totalRows)
	for r := 0; r < totalRows; r++ {
		left[r] = lowBandRealFlat[r*bands : (r+1)*bands]
		leftImg[r] = lowBandImagFlat[r*bands : (r+1)*bands]
	}

	outRightRe = make([]int32, noCol*bands)
	outRightImg = make([]int32, noCol*bands)

	env := 0
	for i := 0; i < noCol; i++ {
		if psProcess != 0 {
			if i == int(h.BsData[h.ProcessSlot].AEnvStartStop[env]) {
				initSlotBasedRotation(h, env, highSubband)
				env++
			}
			rRight := outRightRe[i*bands : (i+1)*bands]
			iRight := outRightImg[i*bands : (i+1)*bands]
			applyPsSlot(h, left[i:], leftImg[i:], rRight, iRight,
				scaleFactorLowBandNoOv, scaleFactorLowBand, scaleFactorHighBand, lsb, usb)
		}
	}

	// The left output is rows 0..noCol-1 (modified in place by hybrid synthesis).
	outLeftRe = make([]int32, noCol*bands)
	outLeftImg = make([]int32, noCol*bands)
	for r := 0; r < noCol; r++ {
		copy(outLeftRe[r*bands:(r+1)*bands], left[r])
		copy(outLeftImg[r*bands:(r+1)*bands], leftImg[r])
	}
	return outLeftRe, outLeftImg, outRightRe, outRightImg, psProcess
}
